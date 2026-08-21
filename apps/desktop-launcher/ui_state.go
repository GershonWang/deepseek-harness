package main

import (
	"fmt"
	"strings"
)

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
	State             string
	Detail            string
	CanStart, CanStop bool
}

// serverDialogState 由当前状态推导弹框文本与按钮可用性。
func serverDialogState(st HarnessStatus) ServerDialogState {
	switch st.State {
	case StateRunning:
		return ServerDialogState{
			State:    "运行中",
			Detail:   fmt.Sprintf("地址: %s\nPID: %d", st.URL, st.PID),
			CanStart: false,
			CanStop:  true,
		}
	case StateStarting:
		return ServerDialogState{
			State:    "启动中",
			Detail:   "harness 正在启动…",
			CanStart: false,
			CanStop:  true,
		}
	default:
		return ServerDialogState{
			State:    "已停止",
			Detail:   "上次退出: " + st.LastExit,
			CanStart: true,
			CanStop:  false,
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

// ToolPanelState 是设置弹框"工具"分区的渲染数据。
type ToolPanelState struct {
	Checks      []ToolCheck
	Installed   []string
	Installable []string
}

// toolPanelState 由探测结果与已安装列表推导面板状态。
func toolPanelState(checks []ToolCheck, installed []string) ToolPanelState {
	return ToolPanelState{Checks: checks, Installed: installed, Installable: []string{"go", "ripgrep"}}
}

// toolPanelText 把工具分区渲染成结构化文本,供 C 侧 dsh_populate_tool_list
// 组装成表格:每工具一行 "名称\tok\t值"(ok=1 时值为版本号,0 时值为缺失原因);
// 末行 "INSTALL\t已安装\t可安装" 描述启动器按需安装的工具。版本号从探测输出
// 提取纯数字段(如 "git version 2.34.1" -> "2.34.1"),与工具名分列,不再混排。
func toolPanelText(s ToolPanelState) string {
	var b strings.Builder
	san := func(s string) string {
		return strings.NewReplacer("\t", " ", "\n", " ", "\r", " ").Replace(s)
	}
	for _, c := range s.Checks {
		b.WriteString(c.Name)
		b.WriteString("\t")
		if c.OK {
			b.WriteString("1\t")
			b.WriteString(san(versionNumber(c.Version)))
		} else {
			b.WriteString("0\t")
			b.WriteString(san(c.Err))
		}
		b.WriteString("\n")
	}
	installed := "无"
	if len(s.Installed) > 0 {
		installed = strings.Join(s.Installed, ",")
	}
	b.WriteString("INSTALL\t" + installed + "\t" + strings.Join(s.Installable, ","))
	return b.String()
}

// versionNumber 从探测输出里提取首个数字段并去掉前导非数字字符,把
// "git version 2.34.1" / "Python 3.10.12" / "v18.19.0" / "jq-1.6"
// 归一为 "2.34.1" / "3.10.12" / "18.19.0" / "1.6"。
func versionNumber(v string) string {
	v = strings.TrimSpace(v)
	for i := 0; i < len(v); i++ {
		if v[i] >= '0' && v[i] <= '9' {
			return v[i:]
		}
	}
	return v
}

// CredentialPanelState 是设置弹框"Git 凭据"分区的渲染数据。
type CredentialPanelState struct {
	HasToken    bool
	User        string
	StoragePath string
}

// credentialPanelState 读取当前凭据并给出存储位置展示。
func credentialPanelState(home, storagePath string) CredentialPanelState {
	user, _, found := ReadGitCredentials(home)
	return CredentialPanelState{HasToken: found, User: user, StoragePath: storagePath}
}

// credentialStatusText 生成凭据分区状态行,永不包含令牌明文。
// HasToken 时标注已保存的用户名,否则提示未保存;两行都附存储位置。
func credentialStatusText(s CredentialPanelState) string {
	if s.HasToken {
		return "✓ 已保存 (" + s.User + ")\n存储位置: " + s.StoragePath
	}
	return "未保存\n存储位置: " + s.StoragePath
}
