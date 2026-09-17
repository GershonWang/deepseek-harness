// Package supervisor 负责 harness 子进程的监护：spawn、进程树终止、退避重启。
// 纯 Go、无 GUI 依赖；进程组/信号的平台差异收敛在 process_unix.go 与
// process_windows.go。
package supervisor

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"sync"
	"time"

	"github.com/deepseek-ai/deepseek-harness/apps/desktop-launcher/internal/domain"
)

// readyPattern 匹配 dsh web 的就绪行。
// v0.1.2-alpha.1 起 URL 带认证 token（?token=xxx），需捕获到空白前的完整 URL。
var readyPattern = regexp.MustCompile(`^dsh web:\s+(https?://127\.0\.0\.1:\d+[^\s]*)`)

// fatalLoadPattern 匹配 harness 打印的确定性启动失败特征：插件树无法加载
// （缺失依赖、语法错误、模块解析失败）。出现即表示重试无意义，supervisor
// 直接进入失败态而非等待 StartupTimeoutMs 熔断——否则用户会看到"启动
// 几秒后停止、重复数次"的卡顿（坏插件下每轮加载整个插件树耗时数秒）。
var fatalLoadPattern = regexp.MustCompile(
	`plugin tree failed to load|host preparation failed|ERR_MODULE_NOT_FOUND`,
)

// startupProgressPattern 匹配 launcher 注入的启动进度上报插件写入 stderr 的进度行。
// 前缀与行格式是壳与插件的约定，插件源码见 internal/appenv/startup_progress.mjs；
// 只接受形如 "dsh-desktop: startup 12/127" 的整行，避免把插件的其它输错当进度。
var startupProgressPattern = regexp.MustCompile(`^dsh-desktop: startup (\d+)/(\d+)$`)

// Options 监护参数。
type Options struct {
	RestartDelayMs    int
	MaxRestartDelayMs int
	KillTimeoutMs     int
	StartupTimeoutMs  int
}

// DefaultOptions 返回默认监护参数。
func DefaultOptions() Options {
	return Options{
		RestartDelayMs:    500,
		MaxRestartDelayMs: 10000,
		KillTimeoutMs:     5000,
		StartupTimeoutMs:  30000,
	}
}

// Config 描述要监护的子进程（由 appenv 解析后注入）。
type Config struct {
	Command string
	Args    []string
	LogDir  string
	// Env 是子进程的完整环境（os/exec 语义：nil 表示继承 launcher 当前环境）。
	// 预检降级路径用它在 spawn 时注入 DSH_HOME 指向全新运行时目录；普通路径
	// 保持 nil，让子进程与 launcher 共享同一份环境快照。
	Env []string
}

// Supervisor 管理 harness 子进程的生命周期。构造即启动唯一的 run() 监护循环。
type Supervisor struct {
	cfg             Config
	options         Options
	ready           chan string
	logSink         *logSink     // 日志落盘端（带轮转）
	stdoutLog       *timedWriter // 带时间戳的 stdout 日志 writer
	stderrLog       *timedWriter // 带时间戳的 stderr 日志 writer
	cancel          context.CancelFunc
	mu              sync.Mutex
	cmd             *exec.Cmd
	exited          chan struct{}
	stopping        bool
	state           domain.HarnessState
	url             string
	pid             int
	lastExit        string
	manuallyStopped bool
	startCh         chan struct{}
	gated           bool // 首次 spawn 前等待 Release（预检编排用）；消耗后不再生效
	sawReady        bool // 当前 spawn 周期是否已匹配就绪行
	sawFatalLoad    bool // 当前 spawn 周期是否已出现确定性加载失败特征
	// startup 是本轮 spawn 的启动进度事实（见 domain.StartupProgress）；随
	// 每次 spawn 重置。onStartupProgress 是进度变化回调，在锁外调用，供 app
	// 层即时推送前端事件；为 nil 时只有 1s 状态轮询会看到新值。
	startup           domain.StartupProgress
	onStartupProgress func()
}

// NewSupervisor 创建监护器并启动监护循环（初始态为 StateStarting，首次
// Start() 由"仅停止态生效"守卫拦下，避免重复 spawn）。
func NewSupervisor(cfg Config, options Options) *Supervisor {
	s := &Supervisor{
		cfg:     cfg,
		options: options,
		ready:   make(chan string, 1),
		startCh: make(chan struct{}, 1),
	}
	go s.run()
	return s
}

// Ready 返回就绪通道，收到 URL 后关闭（仅供首次启动等待使用）。
func (s *Supervisor) Ready() <-chan string {
	return s.ready
}

// Stop 终止当前子进程并停监护循环，用于 launcher 退出。
// 两段等待都有界，避免被卡住的 cmd.Wait() 拖死退出。
func (s *Supervisor) Stop() {
	s.mu.Lock()
	s.stopping = true
	cmd := s.cmd
	cancel := s.cancel
	exited := s.exited
	s.mu.Unlock()

	// 唤醒 run()（可能阻塞在手动停止等待），让其检查 stopping 后退出。
	select {
	case s.startCh <- struct{}{}:
	default:
	}

	if cmd == nil || cmd.Process == nil {
		if cancel != nil {
			cancel()
		}
		return
	}

	terminateTree(cmd)
	select {
	case <-exited:
	case <-time.After(time.Duration(s.options.KillTimeoutMs) * time.Millisecond):
		killTree(cmd)
		select {
		case <-exited:
		case <-time.After(time.Duration(s.options.KillTimeoutMs) * time.Millisecond):
			s.logf("[supervisor] stop: harness wait stuck after kill; giving up")
		}
	}

	if cancel != nil {
		cancel()
	}
}

// Start 手动启动：仅停止态/失败态生效，恢复崩溃自动重启。
func (s *Supervisor) Start() {
	s.mu.Lock()
	if s.stopping || (s.state != domain.StateStopped && s.state != domain.StateFailed) {
		s.mu.Unlock()
		return
	}
	s.manuallyStopped = false
	s.mu.Unlock()
	select {
	case s.startCh <- struct{}{}:
	default:
	}
}

// Gate 要求首次 spawn 前等待 Release：启动前预检需要先于 harness 启动完成，
// 门控让监护循环构造后挂起，直到预检给出放行/降级决策。必须在 NewSupervisor
// 之后、监护循环消费 startCh 之前调用（App 构造内紧接着完成）。
func (s *Supervisor) Gate() {
	s.mu.Lock()
	s.gated = true
	s.mu.Unlock()
}

// Release 放行门控的首次 spawn；未门控时是无害 no-op，因此预检通过路径与
// 现有 StartServer 手动恢复路径可以共用同一调用点。
func (s *Supervisor) Release() {
	s.mu.Lock()
	gated := s.gated
	s.mu.Unlock()
	if !gated {
		return
	}
	select {
	case s.startCh <- struct{}{}:
	default:
	}
}

// SetEnv 原子替换后续 spawn 的子进程环境（nil 恢复继承当前环境）。替换只影响
// 下一次 spawn，不触碰正在运行的进程；与 spawn 的读取同受 mu 保护。
func (s *Supervisor) SetEnv(env []string) {
	s.mu.Lock()
	s.cfg.Env = env
	s.mu.Unlock()
}

// Env 返回当前注入的子进程环境（nil 表示继承）。
func (s *Supervisor) Env() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cfg.Env
}

// Restart 手动重启：停止态直接唤醒 spawn，运行态先优雅终止再唤醒。
func (s *Supervisor) Restart() {
	s.mu.Lock()
	if s.stopping {
		s.mu.Unlock()
		return
	}
	s.manuallyStopped = false
	cmd := s.cmd
	s.mu.Unlock()
	if cmd != nil && cmd.Process != nil {
		terminateTree(cmd)
	}
	select {
	case s.startCh <- struct{}{}:
	default:
	}
}

// StopHarness 手动停止：终止当前 harness 并暂停自动重启，直到 Start()。
func (s *Supervisor) StopHarness() {
	s.mu.Lock()
	if s.stopping {
		s.mu.Unlock()
		return
	}
	s.manuallyStopped = true
	cmd := s.cmd
	s.mu.Unlock()
	if cmd != nil && cmd.Process != nil {
		terminateTree(cmd)
	}
}

// Status 返回当前状态快照。
func (s *Supervisor) Status() domain.HarnessStatus {
	s.mu.Lock()
	defer s.mu.Unlock()
	return domain.HarnessStatus{State: s.state, URL: s.url, PID: s.pid, LastExit: s.lastExit}
}

// Wait 等待子进程结束，与 Stop() 同理只等 s.exited，避免被卡住的 Wait 拖死。
func (s *Supervisor) Wait() {
	s.mu.Lock()
	exited := s.exited
	s.mu.Unlock()
	if exited == nil {
		return
	}
	select {
	case <-exited:
	case <-time.After(time.Duration(s.options.KillTimeoutMs) * time.Millisecond):
		s.logf("[supervisor] wait: harness wait stuck; giving up")
	}
}

// logf 向 harness.log 追加一行（带时间戳和 supervisor 标记）；日志未打开时静默丢弃。
func (s *Supervisor) logf(format string, args ...any) {
	s.mu.Lock()
	w := s.stdoutLog
	s.mu.Unlock()
	if w == nil {
		return
	}
	// supervisor 内部日志统一走 stdoutLog 的 writer，
	// 标记为 supervisor，格式与 stdout/stderr 一致。
	ts := time.Now().Format("2006-01-02 15:04:05.000")
	_, _ = fmt.Fprintf(w.out, "[%s] [supervisor] "+format+"\n", append([]any{ts}, args...)...)
}

// run 是唯一的监护循环：门控等待、手动停止等待、spawn、等退出、退避重启。
func (s *Supervisor) run() {
	attempt := 0
	var failStart time.Time
	for {
		s.mu.Lock()
		if s.stopping {
			s.mu.Unlock()
			return
		}
		gated := s.gated
		manuallyStopped := s.manuallyStopped
		state := s.state
		s.mu.Unlock()

		if gated {
			<-s.startCh
			s.mu.Lock()
			s.gated = false
			stop := s.stopping
			s.mu.Unlock()
			if stop {
				return
			}
		}

		if manuallyStopped {
			<-s.startCh
			s.mu.Lock()
			s.manuallyStopped = false
			attempt = 0
			stop := s.stopping
			s.mu.Unlock()
			if stop {
				return
			}
		}

		// 启动失败态：停止自动重试，等 Start()/Restart() 唤醒后重新尝试。
		if state == domain.StateFailed {
			<-s.startCh
			failStart = time.Time{}
			attempt = 0
			s.mu.Lock()
			stop := s.stopping
			s.mu.Unlock()
			if stop {
				return
			}
		}

		s.spawn()

		s.mu.Lock()
		exited := s.exited
		s.mu.Unlock()
		if exited != nil {
			<-exited
		}

		s.mu.Lock()
		if s.stopping {
			s.mu.Unlock()
			return
		}
		manuallyStopped = s.manuallyStopped
		sawReady := s.sawReady
		sawFatalLoad := s.sawFatalLoad
		s.mu.Unlock()
		if manuallyStopped {
			continue // 回到顶部,进入手动停止等待
		}

		// 启动失败判定：本次 spawn 从未匹配就绪行（start failed 或就绪前退出）。
		// 若 stderr 出现确定性加载失败特征（插件树无法加载），立即进入失败态，
		// 重试没有意义——坏插件每次都会在加载阶段崩掉，等待熔断只会让用户
		// 反复看到"启动几秒后停止、重复数次"。其余情况累计超过
		// StartupTimeoutMs 才进入失败态，避免"启动中"无限卡死。
		if !sawReady {
			if sawFatalLoad {
				s.mu.Lock()
				s.state = domain.StateFailed
				s.mu.Unlock()
				s.logf("[supervisor] harness plugin load failed; giving up immediately")
				continue
			}
			if failStart.IsZero() {
				failStart = time.Now()
			}
			if time.Since(failStart) >= time.Duration(s.options.StartupTimeoutMs)*time.Millisecond {
				s.mu.Lock()
				s.state = domain.StateFailed
				s.mu.Unlock()
				s.logf("[supervisor] harness startup failed; giving up after %dms", s.options.StartupTimeoutMs)
				continue
			}
		} else {
			failStart = time.Time{}
		}

		attempt++
		delay := backoffDelay(s.options.RestartDelayMs, s.options.MaxRestartDelayMs, attempt)
		s.logf("[supervisor] restarting harness in %dms (attempt %d)", delay, attempt)
		select {
		case <-time.After(time.Duration(delay) * time.Millisecond):
		case <-s.startCh:
			attempt = 0
		}
	}
}

// spawn 启动一个子进程并注册唯一调用 cmd.Wait() 的 goroutine。
func (s *Supervisor) spawn() {
	s.mu.Lock()
	if s.logSink != nil {
		if s.stdoutLog != nil {
			s.stdoutLog.flush()
		}
		if s.stderrLog != nil {
			s.stderrLog.flush()
		}
		s.logSink.Close()
		s.logSink = nil
		s.stdoutLog = nil
		s.stderrLog = nil
	}
	if s.cancel != nil {
		s.cancel()
		s.cancel = nil
	}
	for {
		select {
		case <-s.ready:
		default:
			goto drained
		}
	}
drained:
	childEnv := s.cfg.Env
	s.mu.Unlock()

	logSink := newLogSink(filepath.Join(s.cfg.LogDir, "harness.log"))
	// stdout/stderr 分别走带时间戳的 writer，便于排查问题时
	// 直接定位每行的产生时间与来源。
	stdoutLog := newTimedWriter(logSink, "stdout")
	stderrLog := newTimedWriter(logSink, "stderr")

	ctx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(ctx, s.cfg.Command, s.cfg.Args...)
	// Env 为 nil 时保持 os/exec 默认的继承语义；预检降级路径注入的环境在此生效。
	cmd.Env = childEnv
	setProcessGroupAttr(cmd)
	// WaitDelay：harness 退出但孙进程仍持有 stdout/stderr 管道时，cmd.Wait()
	// 会卡在 EOF 上；WaitDelay 到期强制关闭管道并触发 Cancel 清理残留孙进程。
	cmd.WaitDelay = 5 * time.Second
	cmd.Cancel = func() error {
		killTree(cmd)
		return nil
	}
	cmd.Stdout = io.MultiWriter(stdoutLog, newOutputMarker(s), newReadyScanner(s))
	cmd.Stderr = io.MultiWriter(stderrLog, newOutputMarker(s), newFailScanner(s), newProgressScanner(s))

	exited := make(chan struct{})
	s.mu.Lock()
	s.logSink = logSink
	s.stdoutLog = stdoutLog
	s.stderrLog = stderrLog
	s.cancel = cancel
	s.cmd = cmd
	s.exited = exited
	s.state = domain.StateStarting
	s.sawReady = false
	s.sawFatalLoad = false
	s.url = ""
	s.pid = 0
	s.lastExit = ""
	// 启动进度按周期重置：上一轮的数字/时刻不能泄漏到这一轮，否则加载页会先
	// 显示旧进度。StartedAt 取 spawn 开始时刻，让加载页的"已等待"从拉起算起。
	s.startup = domain.StartupProgress{StartedAt: time.Now()}
	s.mu.Unlock()

	if err := cmd.Start(); err != nil {
		s.mu.Lock()
		s.state = domain.StateStopped
		s.lastExit = fmt.Sprintf("start failed: %v", err)
		s.mu.Unlock()
		s.logf("[supervisor] harness start failed: %v", err)
		close(exited)
		return
	}

	s.mu.Lock()
	s.pid = cmd.Process.Pid
	s.mu.Unlock()

	go func() {
		err := cmd.Wait()
		reason := exitReason(cmd, err)
		if reason != "" {
			s.logf("[supervisor] harness %s", reason)
		}
		if err != nil {
			s.logf("[supervisor] harness wait error: %v", err)
		}
		s.mu.Lock()
		s.state = domain.StateStopped
		s.pid = 0
		// 清掉已回收进程的 cmd 引用：Restart/StopHarness 会按 s.cmd 对进程组
		// 发信号，留着它就会在 PID 回绕后打到无关进程组上。只清仍属本次的那个，
		// 因为退避窗口内 run() 可能已经 spawn 了新的子进程。
		if s.cmd == cmd {
			s.cmd = nil
		}
		s.lastExit = reason
		s.mu.Unlock()
		close(exited)
	}()
}

// backoffDelay 返回第 attempt 次重启前应等待的毫秒数：base 起按 2 的幂增长，
// 到达 max 后不再增长。
//
// 指数不能写成 base * (1 << (attempt-1))：attempt 只在收到 startCh 时归零，
// "启动成功后崩溃"的循环会让它一直累加，移位在 int 上溢出为负数，而调用方的
// `delay > max` 判断对负值不成立，time.After(负值) 立即触发——连续约 56 次
// 就会把监护循环变成无退避的 spawn 风暴，日志疯涨、CPU 与内存被打满。
// 这里先判断再加倍，循环轮数只到"增长到 max"为止，因此与 attempt 的大小无关。
func backoffDelay(base, max, attempt int) int {
	if attempt < 1 {
		attempt = 1
	}
	delay := base
	for i := 1; i < attempt; i++ {
		if delay >= max {
			break
		}
		if delay > max/2 {
			delay = max
			break
		}
		delay *= 2
	}
	if delay > max {
		delay = max
	}
	// max <= 0 属非法配置：上面的循环会立即退出，delay 被 max 兜底成非正数，
	// 而 time.After(非正) 会立即触发。退回 base，避免"负延迟"这种无退避行为。
	if delay <= 0 {
		delay = base
	}
	return delay
}

// 日志体积上限。日志无限增长有两个来源：跨重启只追加不裁剪，以及子进程长时间
// 不输出换行时无上限的行缓冲（审计 S4）。
const (
	// maxLogBytes 是 harness.log 的单文件上限：到达即轮转为 harness.log.1
	// （只保留一份历史），因此日志占用的磁盘上限约为它的两倍。
	maxLogBytes = 5 << 20
	// maxLogLineBytes 是单行缓冲上限。子进程可能一次性打印几 MB 而不带换行
	// （例如转储整段 JSON），缓冲必须封顶。
	maxLogLineBytes = 64 << 10
)

// logSink 是 stdout/stderr 共用的日志落盘端：累计写入到达 maxLogBytes 就把当前
// 文件轮转为 <path>.1 并续写新文件。
//
// 只保留这一套轮转逻辑：打开时把已有尺寸作为起算点，因此上次运行留下的超大文件
// 会在本次运行的首次写入时被挪走，运行中的持续增长也由同一个判断兜住。日志不可用
// 时静默丢弃——日志写不出去不该拦住 harness 启动。
type logSink struct {
	mu   sync.Mutex
	path string
	f    *os.File
	size int64
}

// newLogSink 打开（必要时创建）日志文件；已有内容按追加处理，尺寸作为轮转的起算点。
func newLogSink(path string) *logSink {
	s := &logSink{path: path}
	_ = os.MkdirAll(filepath.Dir(path), 0o755)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return s // f 保持 nil，写入静默丢弃
	}
	s.f = f
	if info, statErr := f.Stat(); statErr == nil {
		s.size = info.Size()
	}
	return s
}

// Write 实现 io.Writer：写入前按累计字节数判断轮转。
func (s *logSink) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.f == nil {
		return len(p), nil
	}
	if s.size+int64(len(p)) > maxLogBytes {
		s.rotateLocked()
	}
	n, err := s.f.Write(p)
	s.size += int64(n)
	return n, err
}

// rotateLocked 调用者须持有 s.mu：关闭当前文件、挪成 <path>.1（覆盖上一份）、
// 再用新文件续写。
func (s *logSink) rotateLocked() {
	_ = s.f.Close()
	_ = os.Rename(s.path, s.path+".1")
	f, err := os.OpenFile(s.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		s.f = nil
		s.size = 0
		return
	}
	s.f = f
	s.size = 0
}

// Close 关闭日志文件；之后的写入静默丢弃。
func (s *logSink) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.f != nil {
		_ = s.f.Close()
		s.f = nil
	}
}

// exitReason 把进程退出状态转成诊断字符串；空表示无退出状态。
func exitReason(cmd *exec.Cmd, err error) string {
	if cmd.ProcessState != nil && cmd.ProcessState.Exited() && cmd.ProcessState.ExitCode() != -1 {
		return fmt.Sprintf("exited code=%d", cmd.ProcessState.ExitCode())
	}
	if cmd.ProcessState != nil {
		if s, ok := signalExitReason(cmd); ok {
			return s
		}
		return "ended (no exit code)"
	}
	return ""
}

// lineSink 把写入的字节按行切分后逐行回调，不完整行留到下一次写入。
// stdout/stderr 上的就绪、失败特征、启动进度、首次输出四个关注点共用同一套切分，
// 避免每个关注点各写一份行缓冲逻辑。
type lineSink struct {
	buf []byte
	// dropping 表示当前行已超过 maxLogLineBytes 且还没等到换行：后续片段在遇到
	// 换行前一律丢弃，避免无人换行的输出把扫描器的内存撑爆（审计 S4）。
	dropping bool
	on       func(line string)
}

func newLineSink(on func(line string)) *lineSink {
	return &lineSink{on: on}
}

func (l *lineSink) Write(p []byte) (n int, err error) {
	if l.dropping {
		idx := bytes.IndexByte(p, '\n')
		if idx < 0 {
			return len(p), nil
		}
		l.dropping = false
		p = p[idx+1:] // 换行之后重新开始正常缓冲
	}
	l.buf = append(l.buf, p...)
	for {
		idx := bytes.IndexByte(l.buf, '\n')
		if idx < 0 {
			break
		}
		line := string(l.buf[:idx])
		l.buf = l.buf[idx+1:]
		l.on(line)
	}
	if len(l.buf) > maxLogLineBytes {
		// 超长行整行丢弃，而不是截断后当整行喂给特征匹配：半行可能误配就绪或
		// 加载失败特征。日志文件那一支（timedWriter）仍保留原文，这里只负责
		// 让扫描器的内存有界。
		l.buf = nil
		l.dropping = true
	}
	return len(p), nil
}

// newReadyScanner 逐行扫描 stdout，匹配就绪行。
func newReadyScanner(s *Supervisor) *lineSink {
	return newLineSink(func(line string) {
		if match := readyPattern.FindStringSubmatch(line); match != nil {
			s.markReady(match[1])
			select {
			case s.ready <- match[1]:
			default:
			}
		}
	})
}

// newFailScanner 逐行扫描 stderr，匹配确定性加载失败特征（插件树无法加载）。
// 匹配即标记，run() 在进程退出后据此直接进入失败态，不再等待熔断。
func newFailScanner(s *Supervisor) *lineSink {
	return newLineSink(func(line string) {
		if fatalLoadPattern.MatchString(line) {
			s.markFatalLoad()
		}
	})
}

// newProgressScanner 逐行扫描 stderr，匹配注入插件上报的条目激活进度。
// 匹配即更新快照并回调，供加载页显示确定进度；没有上报时加载页退回粗粒度阶段。
func newProgressScanner(s *Supervisor) *lineSink {
	return newLineSink(func(line string) {
		match := startupProgressPattern.FindStringSubmatch(line)
		if match == nil {
			return
		}
		loaded, err := strconv.Atoi(match[1])
		if err != nil {
			return
		}
		total, err := strconv.Atoi(match[2])
		if err != nil {
			return
		}
		s.markStartupProgress(loaded, total)
	})
}

// newOutputMarker 在子进程第一行输出到达时记录时刻。加载页据此把"进程还没说话"
// 与"已经在加载插件"区分开：前者只能显示笼统的启动文案。
func newOutputMarker(s *Supervisor) *lineSink {
	return newLineSink(func(string) { s.markStartupOutput() })
}

// markReady 记录就绪地址并进入运行态。
func (s *Supervisor) markReady(url string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.state = domain.StateRunning
	s.sawReady = true
	s.url = url
}

// markFatalLoad 记录本次 spawn 已出现确定性加载失败特征。
func (s *Supervisor) markFatalLoad() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sawFatalLoad = true
}

// markStartupOutput 记录本 spawn 周期首行输出的时刻（只记第一次）。
func (s *Supervisor) markStartupOutput() {
	s.mu.Lock()
	if s.startup.OutputAt.IsZero() {
		s.startup.OutputAt = time.Now()
	}
	s.mu.Unlock()
}

// markStartupProgress 记录一次条目激活进度，并在锁外回调。
// 回调由读取子进程输出的 goroutine 同步执行，因此实现必须自身非阻塞：app 层只做
// 节流与事件发射。
func (s *Supervisor) markStartupProgress(loaded, total int) {
	s.mu.Lock()
	s.startup.Loaded = loaded
	s.startup.Total = total
	s.startup.Reported = true
	listener := s.onStartupProgress
	s.mu.Unlock()
	if listener != nil {
		listener()
	}
}

// StartupProgress 返回本轮 spawn 的启动进度快照。
func (s *Supervisor) StartupProgress() domain.StartupProgress {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.startup
}

// SetStartupProgressListener 注册启动进度变化回调（app 层据此即时推送前端事件）。
// 传入 nil 取消注册；回调在扫描器读取子进程输出的 goroutine 上同步执行。
func (s *Supervisor) SetStartupProgressListener(listener func()) {
	s.mu.Lock()
	s.onStartupProgress = listener
	s.mu.Unlock()
}

// timedWriter 为写入的每一行添加时间戳和来源标记前缀。
// 它内部缓冲不完整行，遇到换行符时输出带前缀的完整行。
// 同一底层 writer 上的多个 timedWriter 不保证写入原子性，
// 但 stdout/stderr 各自独立缓冲不会互相打断行结构。
type timedWriter struct {
	out io.Writer
	tag string
	buf []byte
}

// newTimedWriter 创建一个带时间戳的行写入器。
func newTimedWriter(out io.Writer, tag string) *timedWriter {
	return &timedWriter{out: out, tag: tag}
}

// Write 实现 io.Writer：按行缓冲并为每行添加 `[时间戳] [tag] ` 前缀。
//
// 与 lineSink 的处理刻意不同：这里的缓冲就是日志内容本身，超长行不能丢弃，
// 否则日志会静默缺内容；改成带截断标记先落盘再继续缓冲同一行的剩余部分，
// 内存有界的同时不丢字节（审计 S4）。
func (w *timedWriter) Write(p []byte) (int, error) {
	w.buf = append(w.buf, p...)
	for {
		idx := bytes.IndexByte(w.buf, '\n')
		if idx < 0 {
			break
		}
		line := w.buf[:idx]
		w.buf = w.buf[idx+1:]
		ts := time.Now().Format("2006-01-02 15:04:05.000")
		if _, err := fmt.Fprintf(w.out, "[%s] [%s] %s\n", ts, w.tag, line); err != nil {
			return len(p), err
		}
	}
	if len(w.buf) > maxLogLineBytes {
		ts := time.Now().Format("2006-01-02 15:04:05.000")
		if _, err := fmt.Fprintf(w.out, "[%s] [%s] %s [truncated]\n", ts, w.tag, w.buf); err != nil {
			w.buf = nil
			return len(p), err
		}
		w.buf = nil
	}
	return len(p), nil
}

// flush 输出缓冲区中可能残留的不完整行（带时间戳），用于关闭日志前兜底。
func (w *timedWriter) flush() {
	if len(w.buf) == 0 {
		return
	}
	ts := time.Now().Format("2006-01-02 15:04:05.000")
	_, _ = fmt.Fprintf(w.out, "[%s] [%s] %s\n", ts, w.tag, w.buf)
	w.buf = nil
}
