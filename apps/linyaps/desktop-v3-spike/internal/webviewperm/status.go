// 本文件是权限钩子的共享状态：与构建标签无关，保证「不支持」的构建也能读出状态。

package webviewperm

import "sync"

// Status 是挂载与权限请求的当前状态，写进 spike 报告。
//
// 字段全部导出且带 JSON 标签：报告是给人看的证据，键名与含义必须在文档里对得上，
// 因此不在外层再包一层匿名结构。
type Status struct {
	// Unsupported 表示当前构建不含 GTK4/WebKitGTK 6.0 权限实现（非 Linux 或未启用 cgo）。
	Unsupported bool `json:"unsupported"`
	// Scheduled 表示 Install 已被调用并交给了 GTK 主循环。
	Scheduled bool `json:"scheduled"`
	// Installed 表示已在控件树里找到 WebKitWebView 并挂上信号。
	Installed bool `json:"installed"`
	// Attempts 是主循环上重新查找视图的次数。
	Attempts int `json:"attempts"`
	// SignalConnected 表示 g_signal_connect("permission-request") 是否返回了有效 handler id。
	// 返回 0 说明信号名在 WebKitGTK 6.0 上不匹配——那是「钩子挂上了但永远不会被调用」。
	SignalConnected bool `json:"signalConnected"`
	// Toplevels 是最近一次尝试时进程内的顶层窗口数量。
	// 「顶层窗口 > 0 但视图找不到」与「连窗口都没有」是两种完全不同的失败。
	Toplevels int `json:"toplevels"`
	// Views 是最近一次遍历中遇到的 WebKitWebView 数量。
	Views int `json:"views"`
	// Requests/Allowed/Denied 统计 permission-request 的触发次数与结果。
	Requests int `json:"requests"`
	Allowed  int `json:"allowed"`
	Denied   int `json:"denied"`
	// LastKind 记录最近一次请求的类型描述，便于区分「信号挂着但没被触发」与
	// 「触发的是别的权限」。
	LastKind string `json:"lastKind"`
}

var state = struct {
	sync.Mutex
	status Status
}{}

// CurrentStatus 返回状态快照。
func CurrentStatus() Status {
	state.Lock()
	defer state.Unlock()
	return state.status
}
