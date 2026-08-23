// Package domain 定义桌面客户端各层共享的领域模型。
// 只含纯类型，不依赖任何其它包，供 supervisor/connector/toolchain/app 等层引用。
package domain

// HarnessState 描述 harness 进程生命周期状态。
type HarnessState int

const (
	StateStarting HarnessState = iota
	StateRunning
	StateStopped
	// StateFailed 表示持续启动失败后已停止自动重试，等待用户 Start()/Restart()。
	StateFailed
)

// HarnessStatus 是 harness 进程的只读快照。
type HarnessStatus struct {
	State    HarnessState
	URL      string
	PID      int
	LastExit string
}

// Mode 表示当前连接的服务来源。
type Mode int

const (
	ModeContainer Mode = iota
	ModeExternal
)

// ToolCheck 记录一次工具探测结果。
type ToolCheck struct {
	Name    string
	OK      bool
	Version string
	Err     string
}

// CredentialInfo 描述 Git 凭据的展示信息，绝不携带令牌明文。
type CredentialInfo struct {
	HasToken    bool
	User        string
	StoragePath string
}
