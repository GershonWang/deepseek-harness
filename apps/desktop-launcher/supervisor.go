package main

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
	"syscall"
	"time"
)

// readyPattern 匹配 dsh web 的就绪行。
var readyPattern = regexp.MustCompile(`^dsh web:\s+(https?://127\.0\.0\.1:\d+)`)

// HarnessState 描述 harness 生命周期状态。
type HarnessState int

const (
	StateStarting HarnessState = iota // 已 spawn、未就绪
	StateRunning                      // 就绪行已匹配,存活
	StateStopped                      // 未运行(记录上次退出原因)
)

// HarnessStatus 是 UI 轮询用的 harness 快照。
type HarnessStatus struct {
	State    HarnessState
	URL      string // StateRunning 时的就绪地址
	PID      int    // 当前子进程 PID;停止时为 0
	LastExit string // 上次退出原因,如 "exited code=3" / "killed by signal=terminated";从未退出时为空
}

// SupervisorOptions 进程监护参数。
type SupervisorOptions struct {
	RestartDelayMs    int
	MaxRestartDelayMs int
	KillTimeoutMs     int
	StartupTimeoutMs  int
}

// DefaultSupervisorOptions 默认参数。
func DefaultSupervisorOptions() SupervisorOptions {
	return SupervisorOptions{
		RestartDelayMs:    500,
		MaxRestartDelayMs: 10000,
		KillTimeoutMs:     5000,
		StartupTimeoutMs:  30000,
	}
}

// Supervisor 管理 harness 子进程的生命周期。
type Supervisor struct {
	env             DesktopEnv
	options         SupervisorOptions
	ready           chan string
	logFile         *os.File
	cancel          context.CancelFunc // 取消当前子进程上下文，在 Stop() 和重新 spawn 时调用
	mu              sync.Mutex
	cmd             *exec.Cmd
	exited          chan struct{} // 唯一的 cmd.Wait() 完成后关闭;run()/Stop()/Wait() 都等它
	stopping        bool
	state           HarnessState
	url             string
	pid             int
	lastExit        string
	manuallyStopped bool          // 手动停止后暂停自动重启,直到 Start()/Restart()
	startCh         chan struct{} // 唤醒 run():Start/Restart/Stop 解除阻塞
}

// NewSupervisor 创建监护器。
// 构造时启动唯一的 run() 监护循环(初始 state 为 StateStarting,
// 首次 Start() 因此必然被"仅停止态生效"守卫拦下、不留多余 startCh
// 令牌;run() 也恰好只有一个实例,重复 Start() 不会双 spawn)。
func NewSupervisor(env DesktopEnv, options SupervisorOptions) *Supervisor {
	s := &Supervisor{
		env:     env,
		options: options,
		ready:   make(chan string, 1),
		startCh: make(chan struct{}, 1),
	}
	go s.run()
	return s
}

// Ready 返回就绪通道，收到 URL 后关闭。
func (s *Supervisor) Ready() <-chan string {
	return s.ready
}

// Stop 优雅停止子进程：SIGTERM → 等待 → SIGKILL。
// 两段等待都有界：孙进程逃逸出进程组并持有 stdout 管道时，cmd.Wait()
// 可能被 WaitDelay 的清理路径拖住，无界等待会让 launcher 在窗口关闭后
// 无法退出。cmd.Wait() 由 spawn() 里的唯一 goroutine 调用，这里只等
// s.exited 通道，避免与 run() 并发 Wait 导致 exec 内部 ctxResult 只发
// 一次、第二个调用者永久阻塞。
func (s *Supervisor) Stop() {
	s.mu.Lock()
	s.stopping = true
	cmd := s.cmd
	cancel := s.cancel
	exited := s.exited
	s.mu.Unlock()

	// 唤醒 run()(可能阻塞在手动停止等待),让其检查 stopping 后退出
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

	// 向进程组发送 SIGTERM，确保子进程的子孙也被终止
	killProcessGroup(cmd, syscall.SIGTERM)

	// 等待退出或超时后 SIGKILL
	select {
	case <-exited:
	case <-time.After(time.Duration(s.options.KillTimeoutMs) * time.Millisecond):
		killProcessGroup(cmd, syscall.SIGKILL)
		// SIGKILL 后仍可能被卡住的 Wait 拖住，给等待加上限，
		// 超时即放弃（等待 goroutine 泄漏由进程退出兜底）。
		select {
		case <-exited:
		case <-time.After(time.Duration(s.options.KillTimeoutMs) * time.Millisecond):
			s.logf("[supervisor] stop: harness wait stuck after SIGKILL; giving up")
		}
	}

	// 释放 context 资源
	if cancel != nil {
		cancel()
	}
}

// Start 手动启动:仅停止态生效,恢复崩溃自动重启。
func (s *Supervisor) Start() {
	s.mu.Lock()
	if s.stopping || s.state != StateStopped {
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

// Restart 手动重启:停止态直接唤醒 spawn,运行态先 SIGTERM 再唤醒。
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
		killProcessGroup(cmd, syscall.SIGTERM)
	}
	select {
	case s.startCh <- struct{}{}:
	default:
	}
}

// StopHarness 手动停止:杀当前 harness 并暂停自动重启,直到 Start()。
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
		killProcessGroup(cmd, syscall.SIGTERM)
	}
}

// Status 返回当前 harness 状态快照。
func (s *Supervisor) Status() HarnessStatus {
	s.mu.Lock()
	defer s.mu.Unlock()
	return HarnessStatus{State: s.state, URL: s.url, PID: s.pid, LastExit: s.lastExit}
}

// markReady 记录就绪地址并进入运行态。
func (s *Supervisor) markReady(url string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.state = StateRunning
	s.url = url
}

// Wait 等待子进程结束；与 Stop() 同理，只等 s.exited，避免被卡住的
// cmd.Wait() 拖死退出。
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

// logf 向 harness.log 追加一行；日志文件未打开（如 spawn 失败）时静默丢弃。
func (s *Supervisor) logf(format string, args ...any) {
	s.mu.Lock()
	f := s.logFile
	s.mu.Unlock()
	if f == nil {
		return
	}
	_, _ = fmt.Fprintf(f, format+"\n", args...)
}

func (s *Supervisor) run() {
	attempt := 0
	for {
		s.mu.Lock()
		if s.stopping {
			s.mu.Unlock()
			return
		}
		manuallyStopped := s.manuallyStopped
		s.mu.Unlock()

		// 手动停止态:暂停自动重启,等 Start()/Restart() 唤醒
		if manuallyStopped {
			<-s.startCh
			s.mu.Lock()
			s.manuallyStopped = false
			attempt = 0
			stop := s.stopping
			s.mu.Unlock()
			if stop {
				return // Stop() 在手动停止等待期间被调用:直接退出,不再 spawn
			}
		}

		s.spawn()

		// 注意:不要在这里消费 s.ready——main() 的 <-sup.Ready() 负责
		// 接收就绪 URL 并打开窗口。本循环只负责监护:直接等子进程退出。
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
		s.mu.Unlock()
		if manuallyStopped {
			continue // 回到顶部,进入手动停止等待
		}

		// 退避重启;startCh 可打断(Start/Restart 手动唤醒)
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

func (s *Supervisor) spawn() {
	// 关闭之前的日志文件，避免文件描述符泄漏
	s.mu.Lock()
	if s.logFile != nil {
		s.logFile.Close()
		s.logFile = nil
	}
	// 释放上一次 context 的 cancel 函数
	if s.cancel != nil {
		s.cancel()
		s.cancel = nil
	}
	// 清空就绪通道中残留的旧 URL，防止重启时读到上一次的值
	for {
		select {
		case <-s.ready:
		default:
			goto drained
		}
	}
drained:
	s.mu.Unlock()

	logPath := filepath.Join(s.env.LogDir, "harness.log")
	os.MkdirAll(s.env.LogDir, 0o755)
	logFile, _ := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)

	ctx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(ctx, s.env.Command, s.env.Args...)

	// 创建独立进程组，确保 Stop 能终止整个进程树
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	// WaitDelay：harness 退出但孙进程（如 zenity、bash 工具链）仍持有
	// stdout/stderr 管道时，cmd.Wait() 会永久卡在 EOF 上导致 supervisor
	// 不再重启（症状：GUI 永久 load failed）。WaitDelay 在子进程退出后
	// 等待该时长便强制关闭管道，让 Wait 返回、supervisor 继续重启。
	// Cancel 在 WaitDelay 触发时向进程组发 Kill，清理残留孙进程。
	cmd.WaitDelay = 5 * time.Second
	cmd.Cancel = func() error {
		killProcessGroup(cmd, syscall.SIGKILL)
		return nil
	}

	cmd.Stdout = io.MultiWriter(logFile, &readyScanner{sup: s})
	cmd.Stderr = logFile

	exited := make(chan struct{})
	s.mu.Lock()
	s.logFile = logFile
	s.cancel = cancel
	s.cmd = cmd
	s.exited = exited
	s.state = StateStarting
	s.url = ""
	s.pid = 0
	s.lastExit = ""
	s.mu.Unlock()

	if err := cmd.Start(); err != nil {
		// 启动失败(如 node 二进制缺失):置为停止态并记录原因,立即放行等待方,
		// 由 run() 退避重启。若保持 StateStarting,手动停止路径会被卡死,
		// Start() 也会因状态守卫(仅停止态生效)永远无法恢复。
		s.mu.Lock()
		s.state = StateStopped
		s.lastExit = fmt.Sprintf("start failed: %v", err)
		s.mu.Unlock()
		s.logf("[supervisor] harness start failed: %v", err)
		close(exited)
		return
	}

	s.mu.Lock()
	s.pid = cmd.Process.Pid
	s.mu.Unlock()

	// 唯一调用 cmd.Wait() 的 goroutine:记录退出原因并关闭 exited。
	// 其余等待方(run/Stop/Wait)只等通道,避免 exec.CommandContext 的
	// ctxResult 只发一次、并发 Wait 的第二个调用者永久阻塞。
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
		s.state = StateStopped
		s.pid = 0
		s.lastExit = reason
		s.mu.Unlock()
		close(exited)
	}()
}

// exitReason 把进程退出状态转成诊断字符串;空表示无退出状态。
func exitReason(cmd *exec.Cmd, err error) string {
	if cmd.ProcessState != nil && cmd.ProcessState.Exited() && cmd.ProcessState.ExitCode() != -1 {
		return fmt.Sprintf("exited code=%d", cmd.ProcessState.ExitCode())
	}
	if cmd.ProcessState != nil {
		if ws, ok := cmd.ProcessState.Sys().(syscall.WaitStatus); ok && ws.Signaled() {
			return fmt.Sprintf("killed by signal=%s", ws.Signal())
		}
		return "ended (no exit code)"
	}
	return ""
}

// killProcessGroup 向进程组发送信号。
// Setpgid: true 使子进程及其后代拥有独立 PGID（=PID），
// 负号 PID 将信号广播到整个进程组。
func killProcessGroup(cmd *exec.Cmd, sig syscall.Signal) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	syscall.Kill(-cmd.Process.Pid, sig) //nolint:errcheck // 信号失败属竞态，后续 Kill 兜底
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
