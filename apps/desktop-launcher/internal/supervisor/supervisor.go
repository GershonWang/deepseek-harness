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
var readyPattern = regexp.MustCompile(`^dsh web:\s+(https?://127\.0\.0\.1:\d+)`)

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
}

// Supervisor 管理 harness 子进程的生命周期。构造即启动唯一的 run() 监护循环。
type Supervisor struct {
	cfg             Config
	options         Options
	ready           chan string
	logFile         *os.File
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
	sawReady        bool // 当前 spawn 周期是否已匹配就绪行
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

// logf 向 harness.log 追加一行；日志未打开时静默丢弃。
func (s *Supervisor) logf(format string, args ...any) {
	s.mu.Lock()
	f := s.logFile
	s.mu.Unlock()
	if f == nil {
		return
	}
	_, _ = fmt.Fprintf(f, format+"\n", args...)
}

// run 是唯一的监护循环：手动停止等待、spawn、等退出、退避重启。
func (s *Supervisor) run() {
	attempt := 0
	var failStart time.Time
	for {
		s.mu.Lock()
		if s.stopping {
			s.mu.Unlock()
			return
		}
		manuallyStopped := s.manuallyStopped
		state := s.state
		s.mu.Unlock()

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
		s.mu.Unlock()
		if manuallyStopped {
			continue // 回到顶部,进入手动停止等待
		}

		// 启动失败判定：本次 spawn 从未匹配就绪行（start failed 或就绪前退出）。
		// 累计超过 StartupTimeoutMs 则进入失败态停止重试，避免"启动中"无限卡死。
		if !sawReady {
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
		s.logFile.Close()
		s.logFile = nil
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
	s.mu.Unlock()

	logFile := openLogFile(filepath.Join(s.cfg.LogDir, "harness.log"))
	var out io.Writer = logFile
	if logFile == nil {
		out = io.Discard
	}

	ctx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(ctx, s.cfg.Command, s.cfg.Args...)
	setProcessGroupAttr(cmd)
	// WaitDelay：harness 退出但孙进程仍持有 stdout/stderr 管道时，cmd.Wait()
	// 会卡在 EOF 上；WaitDelay 到期强制关闭管道并触发 Cancel 清理残留孙进程。
	cmd.WaitDelay = 5 * time.Second
	cmd.Cancel = func() error {
		killTree(cmd)
		return nil
	}
	cmd.Stdout = io.MultiWriter(out, &readyScanner{sup: s})
	cmd.Stderr = out

	exited := make(chan struct{})
	s.mu.Lock()
	s.logFile = logFile
	s.cancel = cancel
	s.cmd = cmd
	s.exited = exited
	s.state = domain.StateStarting
	s.sawReady = false
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
