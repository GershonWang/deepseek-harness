// Package domain 定义桌面客户端各层共享的领域模型。
// 只含纯类型，不依赖任何其它包，供 supervisor/connector/toolchain/app 等层引用。
package domain

import "time"

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

// StartupProgress 是 harness 启动期的进度事实：由 launcher 注入的上报插件写入
// 子进程 stderr，Supervisor 逐行解析后填充。
//
// 字段全部是已观测到的事实，调用方不得据此推断未上报的阶段：没有上报时就只能
// 显示粗粒度阶段，这正是加载页不出现"假进度"的前提。
type StartupProgress struct {
	// StartedAt 是本轮 spawn 的时刻；启动态期间用它计算已等待时长。
	StartedAt time.Time
	// OutputAt 是子进程第一行输出到达的时刻；零值表示还没有任何输出。
	OutputAt time.Time
	// Reported 表示本轮是否收到过条目计数上报；false 时只有上两个字段可用。
	Reported bool
	// Loaded 是已激活完成的条目数；Total 是本次启动要挂载的条目总数。
	Loaded int
	Total  int
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
	// Path 是命令在当前 PATH 中解析到的绝对路径（exec.LookPath 结果），
	// 供调用方按路径前缀归类来源（随包/宿主导入/系统）；未命中为空。
	Path string
}
