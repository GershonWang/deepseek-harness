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
	env      DesktopEnv
	options  SupervisorOptions
	ready    chan string
	logFile  *os.File
	cancel   context.CancelFunc // 取消当前子进程上下文，在 Stop() 和重新 spawn 时调用
	mu       sync.Mutex
	cmd      *exec.Cmd
	stopping bool
}

// NewSupervisor 创建监护器。
func NewSupervisor(env DesktopEnv, options SupervisorOptions) *Supervisor {
	return &Supervisor{
		env:     env,
		options: options,
		ready:   make(chan string, 1),
	}
}

// Ready 返回就绪通道，收到 URL 后关闭。
func (s *Supervisor) Ready() <-chan string {
	return s.ready
}

// Start 开始监护（在 goroutine 中调用）。
func (s *Supervisor) Start() {
	go s.run()
}

// Stop 优雅停止子进程：SIGTERM → 等待 → SIGKILL。
func (s *Supervisor) Stop() {
	s.mu.Lock()
	s.stopping = true
	cmd := s.cmd
	cancel := s.cancel
	s.mu.Unlock()

	if cmd == nil || cmd.Process == nil {
		if cancel != nil {
			cancel()
		}
		return
	}

	// 向进程组发送 SIGTERM，确保子进程的子孙也被终止
	killProcessGroup(cmd, syscall.SIGTERM)

	// 等待退出或超时后 SIGKILL
	done := make(chan struct{})
	go func() {
		cmd.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(time.Duration(s.options.KillTimeoutMs) * time.Millisecond):
		killProcessGroup(cmd, syscall.SIGKILL)
		<-done
	}

	// 释放 context 资源
	if cancel != nil {
		cancel()
	}
}

// Wait 等待子进程结束。
func (s *Supervisor) Wait() {
	s.mu.Lock()
	cmd := s.cmd
	s.mu.Unlock()
	if cmd != nil {
		cmd.Wait()
	}
}

func (s *Supervisor) run() {
	attempt := 0
	for {
		if s.stopping {
			return
		}

		s.spawn()

		// 注意：不要在这里消费 s.ready——main() 的 <-sup.Ready() 负责
		// 接收就绪 URL 并打开窗口。本循环只负责监护：直接等子进程退出。
		// （此前这里 select s.ready 会与 main 竞争消费唯一的就绪消息，
		// main 赢了则本 goroutine 永久卡在 select，harness 死后不再重启。）

		// 等待子进程退出
		s.mu.Lock()
		cmd := s.cmd
		s.mu.Unlock()
		if cmd != nil {
			err := cmd.Wait()
			// 记录退出原因：静默死亡（SIGKILL/信号）是诊断关键，
			// 否则看起来像"莫名卡住"。
			if cmd.ProcessState != nil && cmd.ProcessState.Exited() && cmd.ProcessState.ExitCode() != -1 {
				fmt.Fprintf(s.logFile, "[supervisor] harness exited code=%d\n", cmd.ProcessState.ExitCode())
			} else if cmd.ProcessState != nil {
				// ExitCode()==-1 且已结束：被信号终止（信号名从 wait status 取）
				if ws, ok := cmd.ProcessState.Sys().(syscall.WaitStatus); ok && ws.Signaled() {
					fmt.Fprintf(s.logFile, "[supervisor] harness killed by signal=%s\n", ws.Signal())
				} else {
					fmt.Fprintf(s.logFile, "[supervisor] harness ended (no exit code)\n")
				}
			}
			if err != nil {
				fmt.Fprintf(s.logFile, "[supervisor] harness wait error: %v\n", err)
			}
		}

		if s.stopping {
			return
		}

		// 退避重启
		attempt++
		delay := s.options.RestartDelayMs * (1 << (attempt - 1))
		if delay > s.options.MaxRestartDelayMs {
			delay = s.options.MaxRestartDelayMs
		}
		fmt.Fprintf(s.logFile, "[supervisor] restarting harness in %dms (attempt %d)\n", delay, attempt)
		time.Sleep(time.Duration(delay) * time.Millisecond)
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

	s.mu.Lock()
	s.logFile = logFile
	s.cancel = cancel
	s.cmd = cmd
	s.mu.Unlock()

	cmd.Start()
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
			select {
			case r.sup.ready <- match[1]:
			default:
			}
		}
	}
	return len(p), nil
}
