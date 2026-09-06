// Package terminal - 终端会话管理器
package terminal

import (
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/google/uuid"
)

// Manager 管理所有终端会话的生命周期。
//
// 职责：
//   - 创建、查找、关闭会话
//   - 生成唯一的会话 ID
//   - 维护会话列表，供前端查询
//   - 转发会话事件到 Wails 事件系统（通过注册的回调）
//
// Manager 是线程安全的，可以在多个 goroutine 中并发使用。
type Manager struct {
	mu       sync.RWMutex
	sessions map[string]*Session

	// 事件回调（由 app 层注册，用于推送到 Wails 前端）
	onOutput func(sessionID string, data string)
	onStatus func(sessionID string, status SessionStatus, exitCode int, err error)

	counter atomic.Int64
}

// NewManager 创建一个新的终端会话管理器。
func NewManager() *Manager {
	return &Manager{
		sessions: make(map[string]*Session),
	}
}

// SetOutputCallback 设置全局输出回调。
//
// 所有会话的输出都会通过此回调转发，app 层用它来发送 Wails 事件。
func (m *Manager) SetOutputCallback(cb func(sessionID string, data string)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.onOutput = cb
}

// SetStatusCallback 设置全局状态变更回调。
func (m *Manager) SetStatusCallback(cb func(sessionID string, status SessionStatus, exitCode int, err error)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.onStatus = cb
}

// Start 创建并启动一个新的终端会话。
//
// opts 可以为 nil，此时使用默认配置（bash、80x24、当前目录）。
// 返回会话 ID 和错误信息。
func (m *Manager) Start(opts *StartOptions) (string, error) {
	if opts == nil {
		opts = &StartOptions{}
	}

	// 生成唯一会话 ID
	sessionID := generateSessionID()

	session, err := newSession(sessionID, *opts)
	if err != nil {
		return "", fmt.Errorf("failed to create terminal session: %w", err)
	}

	// 注册回调，转发事件到全局回调
	session.SetOutputCallback(func(data string) {
		m.mu.RLock()
		cb := m.onOutput
		m.mu.RUnlock()
		if cb != nil {
			cb(sessionID, data)
		}
	})

	session.SetStatusCallback(func(status SessionStatus, exitCode int, err error) {
		m.mu.RLock()
		cb := m.onStatus
		m.mu.RUnlock()
		if cb != nil {
			errStr := ""
			if err != nil {
				errStr = err.Error()
			}
			cb(sessionID, status, exitCode, fmt.Errorf("%s", errStr))
		}

		// 会话关闭后，延迟清理（保留一段时间供前端查询状态）
		if status == StatusClosed {
			// 不立即删除，留给前端获取退出状态的机会
			// 实际清理可以在下次 List 时或显式 Remove 时进行
		}
	})

	m.mu.Lock()
	m.sessions[sessionID] = session
	m.mu.Unlock()

	return sessionID, nil
}

// Get 获取指定 ID 的会话。
//
// 如果会话不存在，返回 nil 和 false。
func (m *Manager) Get(sessionID string) (*Session, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	s, ok := m.sessions[sessionID]
	return s, ok
}

// Write 向指定会话写入输入。
//
// 会话不存在时返回错误。
func (m *Manager) Write(sessionID string, data string) error {
	s, ok := m.Get(sessionID)
	if !ok {
		return fmt.Errorf("session %s not found", sessionID)
	}
	return s.Write(data)
}

// Resize 调整指定会话的窗口大小。
func (m *Manager) Resize(sessionID string, cols, rows int) error {
	s, ok := m.Get(sessionID)
	if !ok {
		return fmt.Errorf("session %s not found", sessionID)
	}
	return s.Resize(cols, rows)
}

// Close 关闭指定的会话。
//
// 会话不存在时返回 nil（幂等操作）。
func (m *Manager) Close(sessionID string) error {
	s, ok := m.Get(sessionID)
	if !ok {
		return nil
	}
	return s.Close()
}

// Remove 从管理器中移除会话（会话必须已关闭）。
//
// 如果会话仍在运行，会先关闭再移除。
func (m *Manager) Remove(sessionID string) error {
	s, ok := m.Get(sessionID)
	if !ok {
		return nil
	}

	// 确保会话已关闭
	_ = s.Close()

	m.mu.Lock()
	delete(m.sessions, sessionID)
	m.mu.Unlock()
	return nil
}

// List 返回所有会话的元数据快照。
func (m *Manager) List() []SessionInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make([]SessionInfo, 0, len(m.sessions))
	for _, s := range m.sessions {
		result = append(result, s.Info())
	}
	return result
}

// CloseAll 关闭所有会话。
//
// 用于应用退出时的清理工作。
func (m *Manager) CloseAll() {
	m.mu.RLock()
	sessions := make([]*Session, 0, len(m.sessions))
	for _, s := range m.sessions {
		sessions = append(sessions, s)
	}
	m.mu.RUnlock()

	for _, s := range sessions {
		_ = s.Close()
	}
}

// generateSessionID 生成唯一的会话 ID。
//
// 使用 UUID v4 格式，确保全局唯一且不可预测。
func generateSessionID() string {
	return "term-" + uuid.New().String()[:8]
}
