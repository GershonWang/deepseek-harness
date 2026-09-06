// Package terminal 提供基于伪终端（PTY）的交互式 Shell 会话管理。
//
// 设计目标：
//   - 轻量独立：不依赖 DSH 运行时，可在 desktop-launcher 中直接使用
//   - 线程安全：所有公开方法并发安全
//   - 事件驱动：输出通过回调/事件推送给前端
//
// 架构：
//
//	Manager -- owns --> Session(PTY)
//	     |                    |
//	     v                    v
//	 Wails Events        输入/输出流
package terminal

import "time"

// SessionStatus 描述终端会话的运行状态。
type SessionStatus string

const (
	// StatusStarting 会话正在启动中。
	StatusStarting SessionStatus = "starting"
	// StatusRunning 会话正常运行。
	StatusRunning SessionStatus = "running"
	// StatusClosed 会话已关闭（正常退出或异常终止）。
	StatusClosed SessionStatus = "closed"
)

// SessionInfo 是前端可用的会话元数据快照。
type SessionInfo struct {
	ID        string         `json:"id"`
	Title     string         `json:"title"`
	Status    SessionStatus `json:"status"`
	Cols      int            `json:"cols"`
	Rows      int            `json:"rows"`
	CreatedAt time.Time      `json:"createdAt"`
	ExitCode  int            `json:"exitCode,omitempty"`
}

// OutputEvent 是推送到前端的终端输出事件载荷。
//
// Data 字段是原始字节流，可能包含 ANSI 转义序列，
// 前端终端模拟器负责解析和渲染。
type OutputEvent struct {
	SessionID string `json:"sessionId"`
	Data      string `json:"data"`
}

// StatusEvent 是推送到前端的会话状态变更事件载荷。
type StatusEvent struct {
	SessionID string         `json:"sessionId"`
	Status    SessionStatus `json:"status"`
	ExitCode  int            `json:"exitCode,omitempty"`
	Error     string         `json:"error,omitempty"`
}

// StartOptions 是创建新终端会话的选项。
type StartOptions struct {
	// Command 要执行的 shell 命令，为空时使用默认 shell（/bin/bash）。
	Command string
	// Args 命令参数。
	Args []string
	// Cwd 工作目录，为空时使用当前工作目录。
	Cwd string
	// Env 额外的环境变量，会与继承的系统环境合并。
	Env []string
	// Cols 初始列数，默认 80。
	Cols int
	// Rows 初始行数，默认 24。
	Rows int
	// Title 会话标题，用于前端标签显示。
	Title string
}
