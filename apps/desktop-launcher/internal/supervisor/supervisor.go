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
	logFile         *os.File
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
		delay := s.options.RestartDelayMs * (1 << (attempt - 1))
		if delay > s.options.MaxRestartDelayMs {
			delay = s.options.MaxRestartDelayMs
		}
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
	if s.logFile != nil {
		if s.stdoutLog != nil {
			s.stdoutLog.flush()
		}
		if s.stderrLog != nil {
			s.stderrLog.flush()
		}
		s.logFile.Close()
		s.logFile = nil
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

	logFile := openLogFile(filepath.Join(s.cfg.LogDir, "harness.log"))
	// stdout/stderr 分别走带时间戳的 writer，便于排查问题时
	// 直接定位每行的产生时间与来源。
	var stdoutLog, stderrLog *timedWriter
	if logFile != nil {
		stdoutLog = newTimedWriter(logFile, "stdout")
		stderrLog = newTimedWriter(logFile, "stderr")
	} else {
		stdoutLog = newTimedWriter(io.Discard, "stdout")
		stderrLog = newTimedWriter(io.Discard, "stderr")
	}

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
	cmd.Stdout = io.MultiWriter(stdoutLog, &readyScanner{sup: s})
	cmd.Stderr = io.MultiWriter(stderrLog, &failScanner{sup: s})

	exited := make(chan struct{})
	s.mu.Lock()
	s.logFile = logFile
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
		s.lastExit = reason
		s.mu.Unlock()
		close(exited)
	}()
}

// openLogFile 打开（必要时创建）harness 日志文件；失败时返回 io.Discard，
// 保证子进程输出永不落到 nil writer 上。
func openLogFile(path string) *os.File {
	_ = os.MkdirAll(filepath.Dir(path), 0o755)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil
	}
	return f
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

// readyScanner 逐行扫描 stdout，匹配就绪行。
type readyScanner struct {
	sup *Supervisor
	buf []byte
}

func (r *readyScanner) Write(p []byte) (n int, err error) {
	r.buf = append(r.buf, p...)
	for {
		idx := bytes.IndexByte(r.buf, '\n')
		if idx < 0 {
			break
		}
		line := string(r.buf[:idx])
		r.buf = r.buf[idx+1:]
		if match := readyPattern.FindStringSubmatch(line); match != nil {
			r.sup.markReady(match[1])
			select {
			case r.sup.ready <- match[1]:
			default:
			}
		}
	}
	return len(p), nil
}

// markReady 记录就绪地址并进入运行态。
func (s *Supervisor) markReady(url string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.state = domain.StateRunning
	s.sawReady = true
	s.url = url
}

// failScanner 逐行扫描 stderr，匹配确定性加载失败特征（插件树无法加载）。
// 匹配即标记，run() 在进程退出后据此直接进入失败态，不再等待熔断。
type failScanner struct {
	sup *Supervisor
	buf []byte
}

func (f *failScanner) Write(p []byte) (n int, err error) {
	f.buf = append(f.buf, p...)
	for {
		idx := bytes.IndexByte(f.buf, '\n')
		if idx < 0 {
			break
		}
		line := string(f.buf[:idx])
		f.buf = f.buf[idx+1:]
		if fatalLoadPattern.MatchString(line) {
			f.sup.markFatalLoad()
		}
	}
	return len(p), nil
}

// markFatalLoad 记录本次 spawn 已出现确定性加载失败特征。
func (s *Supervisor) markFatalLoad() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sawFatalLoad = true
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
