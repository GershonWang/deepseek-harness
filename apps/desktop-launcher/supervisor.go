package main

import (
	"bytes"
	"context"
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
	s.mu.Unlock()

	if cmd == nil || cmd.Process == nil {
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

		// 等待就绪或超时
		select {
		case <-s.ready:
			attempt = 0 // 就绪后重置退避计数
		case <-time.After(time.Duration(s.options.StartupTimeoutMs) * time.Millisecond):
			// 启动超时，杀死子进程走退避
			s.mu.Lock()
			cmd := s.cmd
			s.mu.Unlock()
			if cmd != nil && cmd.Process != nil {
				killProcessGroup(cmd, syscall.SIGKILL)
				cmd.Wait()
			}
		}

		// 等待子进程退出
		s.mu.Lock()
		cmd := s.cmd
		s.mu.Unlock()
		if cmd != nil {
			cmd.Wait()
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
		time.Sleep(time.Duration(delay) * time.Millisecond)
	}
}

func (s *Supervisor) spawn() {
	logPath := filepath.Join(s.env.LogDir, "harness.log")
	os.MkdirAll(s.env.LogDir, 0o755)
	logFile, _ := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	s.logFile = logFile

	ctx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(ctx, s.env.Command, s.env.Args...)
	_ = cancel // 由 Stop/killProcessGroup 显式终止；保留 cancel 以抑制 vet 告警

	// 创建独立进程组，确保 Stop 能终止整个进程树
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	cmd.Stdout = io.MultiWriter(logFile, &readyScanner{sup: s})
	cmd.Stderr = logFile

	s.mu.Lock()
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
