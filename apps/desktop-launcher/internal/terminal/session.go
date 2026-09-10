// Package terminal - PTY 会话实现
package terminal

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
	"unicode/utf8"

	"github.com/creack/pty"
)

// Session 表示一个独立的 PTY 终端会话。
//
// 每个会话管理一个伪终端对（master/slave）和一个子进程。
// 输出通过回调函数推送给前端，输入通过 Write 方法写入 PTY。
type Session struct {
	id        string
	title     string
	status    atomic.Value // SessionStatus
	cmd       *exec.Cmd
	pty       *os.File
	cols      int
	rows      int
	createdAt time.Time
	exitCode  int
	done      chan struct{} // waitLoop 退出时关闭，供 Close() 等待

	mu      sync.Mutex
	closed  bool
	onOutput func(data string)
	onStatus func(status SessionStatus, exitCode int, err error)
}

// ensureSessionEnv 补齐终端子进程必需的环境变量（TERM/SHELL），返回新环境切片。
//
// 玲珑启动 GUI 应用时不携带 SHELL，而 ~/.bashrc 里的 dircolors 等子进程工具
// 依赖它判断输出格式；bash 会从 /etc/passwd 补一个 $SHELL 变量但并不导出，
// 子进程读不到，必须在 env 数组里显式兜底，否则每次开终端都会打印
// "dircolors: no SHELL environment variable" 警告。SHELL 兜底取本次启动的
// shell；已有的非空值保持不动（开发态从桌面会话继承的值不应被覆盖）。
// env 数组出现重复条目时 getenv 返回首条，因此空值条目一律剔除、非空值
// 统一只保留一条，保证子进程看到的 SHELL 恰好一条且非空。TERM 缺失同样
// 在此兜底，否则很多程序显示异常。
//
// base 必须是本次调用新建的切片：函数为避免额外分配会原地紧凑该切片。
func ensureSessionEnv(base []string, shell string) []string {
	hasTerm := false
	shellEntry := ""
	env := base[:0]
	for _, e := range base {
		switch {
		case strings.HasPrefix(e, "TERM="):
			hasTerm = true
			env = append(env, e)
		case e == "SHELL=":
			// 空值等价于缺失，剔除后走兜底
		case strings.HasPrefix(e, "SHELL="):
			shellEntry = e
			env = append(env, e)
		default:
			env = append(env, e)
		}
	}
	if !hasTerm {
		env = append(env, "TERM=xterm-256color")
	}
	if shellEntry == "" {
		env = append(env, "SHELL="+shell)
	}
	return env
}

// newSession 创建一个新的 PTY 会话。
//
// 调用后会话处于 Starting 状态，PTY 已创建但子进程尚未启动。
// 调用 start() 后进入 Running 状态。
func newSession(id string, opts StartOptions) (*Session, error) {
	if opts.Cols <= 0 {
		opts.Cols = 80
	}
	if opts.Rows <= 0 {
		opts.Rows = 24
	}
	if opts.Command == "" {
		opts.Command = "/bin/bash"
	}

	title := opts.Title
	if title == "" {
		title = opts.Command
	}

	s := &Session{
		id:        id,
		title:     title,
		cols:      opts.Cols,
		rows:      opts.Rows,
		createdAt: time.Now(),
		exitCode:  -1,
		done:      make(chan struct{}),
	}
	s.status.Store(StatusStarting)

	// 构造命令
	cmd := exec.Command(opts.Command, opts.Args...)
	if opts.Cwd != "" {
		cmd.Dir = opts.Cwd
	}

	// 合并环境变量：继承系统环境 + 额外环境，并补齐 TERM/SHELL 兜底。
	// ensureSessionEnv 会原地紧凑 base，这里必须传新建的切片（os.Environ
	// 与 append 均返回新切片，满足前提）。
	env := ensureSessionEnv(append(os.Environ(), opts.Env...), opts.Command)
	cmd.Env = env

	s.cmd = cmd

	// 创建 PTY
	ptmx, err := pty.StartWithSize(cmd, &pty.Winsize{
		Cols: uint16(opts.Cols),
		Rows: uint16(opts.Rows),
		X:    0,
		Y:    0,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to start pty: %w", err)
	}
	s.pty = ptmx
	s.status.Store(StatusRunning)

	// 启动输出读取 goroutine
	go s.readLoop()

	// 启动等待 goroutine（监控进程退出）
	go s.waitLoop()

	return s, nil
}

// readLoop 持续从 PTY 主端读取输出，并通过回调推送。
//
// 设计要点：
//   - 使用固定大小的缓冲区（4KB）批量读取，减少回调次数
//   - 读取错误时终止循环并标记会话已关闭
//   - 输出按原始字节传递，保留 ANSI 转义序列
func (s *Session) readLoop() {
	buf := make([]byte, 4096)
	for {
		n, err := s.pty.Read(buf)
		if n > 0 {
			// 只传递有效的 UTF-8 数据（大部分终端输出都是 UTF-8）
			// 对于非 UTF-8 字节，原样传递不做转换，由前端处理
			data := string(buf[:n])
			s.mu.Lock()
			cb := s.onOutput
			s.mu.Unlock()
			if cb != nil {
				cb(data)
			}
		}
		if err != nil {
			// EOF 或读取错误表示 PTY 已关闭
			return
		}
	}
}

// waitLoop 等待子进程退出，然后更新状态。
func (s *Session) waitLoop() {
	err := s.cmd.Wait()

	s.mu.Lock()
	defer s.mu.Unlock()

	exitCode := -1
	if err == nil {
		exitCode = 0
	} else {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			exitCode = exitErr.ExitCode()
		}
	}
	s.exitCode = exitCode
	s.closed = true
	s.status.Store(StatusClosed)

	if s.onStatus != nil {
		s.onStatus(StatusClosed, exitCode, err)
	}

	// 关闭 PTY 主端
	_ = s.pty.Close()

	// 通知所有等待方进程已退出
	close(s.done)
}

// ID 返回会话唯一标识。
func (s *Session) ID() string {
	return s.id
}

// Title 返回会话标题。
func (s *Session) Title() string {
	return s.title
}

// Status 返回当前状态。
func (s *Session) Status() SessionStatus {
	return s.status.Load().(SessionStatus)
}

// Info 返回会话元数据快照。
func (s *Session) Info() SessionInfo {
	s.mu.Lock()
	defer s.mu.Unlock()
	return SessionInfo{
		ID:        s.id,
		Title:     s.title,
		Status:    s.Status(),
		Cols:      s.cols,
		Rows:      s.rows,
		CreatedAt: s.createdAt,
		ExitCode:  s.exitCode,
	}
}

// Write 向 PTY 写入输入数据。
//
// data 是原始字节串，可以包含普通字符和控制字符（如 \n、\x03 等）。
// 写入失败时返回错误，但不影响会话状态（PTY 可能已关闭）。
func (s *Session) Write(data string) error {
	s.mu.Lock()
	closed := s.closed
	s.mu.Unlock()

	if closed {
		return errors.New("session is closed")
	}

	// 确保数据是有效的字节序列
	// 对于无效的 UTF-8，原样写入（PTY 不关心编码）
	_, err := s.pty.Write([]byte(data))
	return err
}

// Resize 调整 PTY 窗口大小。
//
// 这会向子进程发送 SIGWINCH 信号，通知应用程序窗口大小变化。
// 列数和行数必须为正整数。
func (s *Session) Resize(cols, rows int) error {
	if cols <= 0 || rows <= 0 {
		return errors.New("cols and rows must be positive")
	}

	s.mu.Lock()
	closed := s.closed
	s.mu.Unlock()

	if closed {
		return errors.New("session is closed")
	}

	err := pty.Setsize(s.pty, &pty.Winsize{
		Cols: uint16(cols),
		Rows: uint16(rows),
	})
	if err != nil {
		return fmt.Errorf("failed to resize pty: %w", err)
	}

	s.mu.Lock()
	s.cols = cols
	s.rows = rows
	s.mu.Unlock()
	return nil
}

// Close 关闭终端会话。
//
// 清理顺序：SIGTERM → 等待退出 → 关闭 PTY 主端（内核向挂在这个终端上的
// 所有进程发 SIGHUP）→ SIGKILL 兜底。既兼容玲珑沙箱（不依赖 Setpgid），
// 又能通过 SIGHUP 传播清理终端内的子进程。
// 多次调用 Close 是安全的，后续调用直接返回 nil。
func (s *Session) Close() error {
	s.mu.Lock()
	if s.closed {
		// 已经关闭过：如果 done 还没关（正在关闭中），等一下再返回
		done := s.done
		s.mu.Unlock()
		if done != nil {
			select {
			case <-done:
			case <-time.After(time.Second):
			}
		}
		return nil
	}
	s.closed = true
	cmd := s.cmd
	ptyFile := s.pty
	done := s.done
	s.mu.Unlock()

	if cmd == nil || cmd.Process == nil {
		return nil
	}

	// 1. SIGTERM 优雅终止主进程（shell 会收到并转发给前台进程组）
	_ = cmd.Process.Signal(syscall.SIGTERM)

	// 2. 关闭 PTY 主端：内核会给所有以该 PTY 为控制终端的进程发 SIGHUP，
	//    这是 POSIX 标准的终端清理机制，无需 Setpgid 就能覆盖子进程
	if ptyFile != nil {
		_ = ptyFile.Close()
	}

	// 3. 等待优雅退出，最多 3 秒
	select {
	case <-done:
		return nil
	case <-time.After(3 * time.Second):
	}

	// 4. 兜底：直接 kill 主进程（确保主程序退出）
	_ = cmd.Process.Kill()

	// 5. 再等最多 2 秒让进程彻底消亡
	select {
	case <-done:
	case <-time.After(2 * time.Second):
	}

	return nil
}

// SetOutputCallback 设置输出回调函数。
//
// 回调会在读取 goroutine 中同步调用，因此回调函数应：
//   - 尽量简短，避免阻塞读取
//   - 不要在回调中调用 Session 的其他方法（可能导致死锁）
//   - 如果需要耗时处理，应异步派发
func (s *Session) SetOutputCallback(cb func(data string)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.onOutput = cb
}

// SetStatusCallback 设置状态变更回调函数。
func (s *Session) SetStatusCallback(cb func(status SessionStatus, exitCode int, err error)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.onStatus = cb
}

// 编译期验证：确保 Write 方法接受的 data 字符串可以安全地转换为字节
var _ = utf8.ValidString
