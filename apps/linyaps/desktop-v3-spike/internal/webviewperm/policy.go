// Package webviewperm 是环境贴权限钩子的 GTK4 原型：接管内嵌 WebKitWebView 的
// permission-request 信号，并按最小权限原则放行音频采集。
//
// 为什么要在 v3 spike 里先做原型：客户端现有的实现（apps/linyaps/desktop-launcher/
// internal/webviewperm）用的是 GTK3 的 gtk_window_list_toplevels 与
// gtk_container_get_children，这两个 API 在 GTK4 已被删除，且 WebKitGTK 6.0 换了
// 头文件与部分 GObject 类型。策略判断（policy.go）与 GTK 侧解耦，可以原样移植；
// 真正需要重验的是「GTK4 下还能不能从控件树找回内嵌视图并挂上信号」。
package webviewperm

// PermissionFacts 是一次权限请求的可判定事实。
//
// 单独建模而不直接传 WebKit 类型，是为了让放行策略能脱离 GTK 单测：C 侧只负责从
// WebKitPermissionRequest 提取事实，判断逻辑留在 Go。
type PermissionFacts struct {
	// IsUserMedia 表示这是 getUserMedia 一类的媒体采集请求。
	IsUserMedia bool
	// IsAudio 表示请求包含音频采集（麦克风）。
	IsAudio bool
	// IsVideo 表示请求包含视频采集（摄像头）。
	IsVideo bool
	// IsDisplay 表示请求包含屏幕采集。
	IsDisplay bool
}

// AllowPermission 判定是否放行一次权限请求。
//
// 只放行「音频且不含视频/屏幕」的采集：语音输入只需要麦克风，按最小权限原则拒绝
// 摄像头与屏幕共享。其余类型的权限请求（通知、地理位置、指针锁定等）同样拒绝——
// 内嵌界面没有需要它们的产品功能，放行只会扩大暴露面。
//
// 这段与 desktop-launcher 的 policy.go 保持一致；原型阶段复制而非跨模块引用，
// 正式迁移时直接把原文件搬过来即可。
func AllowPermission(facts PermissionFacts) bool {
	return facts.IsUserMedia && facts.IsAudio && !facts.IsVideo && !facts.IsDisplay
}
