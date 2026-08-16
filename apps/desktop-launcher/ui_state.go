package main

import "fmt"

// statusBarText 生成状态栏左侧指示文本(状态 + 端口/退出原因)。
func statusBarText(st HarnessStatus) string {
	switch st.State {
	case StateRunning:
		return "● 运行中 " + st.URL
	case StateStarting:
		return "● 启动中"
	default:
		if st.LastExit != "" {
			return "● 已停止 (" + st.LastExit + ")"
		}
		return "● 已停止"
	}
}

// ServerDialogState 是服务器状态弹框的一次刷新内容。
type ServerDialogState struct {
	State                         string
	Detail                        string
	CanStart, CanRestart, CanStop bool
}

// serverDialogState 由当前状态推导弹框文本与按钮可用性。
func serverDialogState(st HarnessStatus) ServerDialogState {
	switch st.State {
	case StateRunning:
		return ServerDialogState{
			State:      "运行中",
			Detail:     fmt.Sprintf("地址: %s\nPID: %d", st.URL, st.PID),
			CanStart:   false,
			CanRestart: true,
			CanStop:    true,
		}
	case StateStarting:
		return ServerDialogState{
			State:      "启动中",
			Detail:     "harness 正在启动…",
			CanStart:   false,
			CanRestart: true,
			CanStop:    true,
		}
	default:
		return ServerDialogState{
			State:      "已停止",
			Detail:     "上次退出: " + st.LastExit,
			CanStart:   true,
			CanRestart: false,
			CanStop:    false,
		}
	}
}

// externalStatusBarText 生成外部模式的状态栏文本。
func externalStatusBarText(connector *Connector) string {
	if u := connector.ExternalURL(); u != "" {
		return "● 外部服务 " + u
	}
	return "● 外部模式"
}

// ExternalDialogState 是外部模式弹框状态区的文本与按钮可用性。
type ExternalDialogState struct {
	State, Detail string
	CanConnect    bool
	CanDisconnect bool
}

// externalDialogState 由连接器状态推导弹框文本与按钮可用性。
func externalDialogState(connector *Connector, busy bool) ExternalDialogState {
	connected := connector.Mode() == ModeExternal
	return ExternalDialogState{
		State:         map[bool]string{true: "已连接", false: "未连接"}[connected],
		Detail:        "外部地址: " + connector.ExternalURL(),
		CanConnect:    !connected && !busy,
		CanDisconnect: connected && !busy,
	}
}
