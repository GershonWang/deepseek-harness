// Package webviewperm 让桌面壳内嵌的 WebKitGTK 视图放行麦克风采集权限。
//
// 存在的理由：Wails v2 的 Linux 后端不连接 WebKitWebView::permission-request，
// 而 WebKitGTK 对该信号的默认处理是拒绝，于是内嵌界面里的 getUserMedia 一定抛
// NotAllowedError——语音输入在桌面版不可用，同一个 Web 版用浏览器打开却正常。
// Wails 把 WebKitWebView 指针放在 internal/ 包内，应用层拿不到，因此改为从 GTK
// 侧遍历顶层窗口取回该实例并自行连接信号（见 permission_linux.c）。
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
func AllowPermission(facts PermissionFacts) bool {
	return facts.IsUserMedia && facts.IsAudio && !facts.IsVideo && !facts.IsDisplay
}
