//go:build linux && cgo && !gtk3 && !purego

// 本文件是 GTK4 + WebKitGTK 6.0 下的权限钩子入口：把挂载动作调度到 GTK 主循环，
// 并把挂载结果与每次权限请求记录成可写进报告的状态。
//
// 为什么不在调用点直接做遍历：v3 里没有 v2 的 OnDomReady（由 GTK 主循环的 C 回调
// 直接进入），OnWindowEvent 的回调线程取决于框架实现。g_timeout_add 只要求主循环
// 在跑，且天然串行，因此无论从哪个线程调用都安全。
package webviewperm

/*
#cgo linux pkg-config: gtk4 webkitgtk-6.0

#include <gtk/gtk.h>
#include <webkit/webkit.h>

// 由 permission_gtk4_linux.c 实现：调度到 GTK 主循环，在若干次内重试直到找到
// WebKitWebView。返回 1 表示已调度（不代表已挂载）。
int dshInstallPermissionHandlerGTK4(void);
*/
import "C"

// Install 请求在 GTK 主循环上安装权限钩子。可重复调用，重复无害。
//
// 不返回是否挂载成功：查找发生在主循环回调里，结果通过 CurrentStatus 读取。
func Install() {
	state.Lock()
	state.status.Scheduled = true
	state.Unlock()
	C.dshInstallPermissionHandlerGTK4()
}

// 注意：这些 //export 函数的签名会生成进 _cgo_export.h，C 侧的调用点必须同步。
// GOOS=windows 的交叉检查不编译 cgo 文件，因此这里的不一致只有真机构建能发现。

// dshReportInstall 由 C 侧回调：报告第几次尝试、是否挂上，以及遍历时看到的
// 顶层窗口数与 WebView 数（用于区分「窗口没建出来」与「窗口里找不到视图」）。
//
//export dshReportInstall
func dshReportInstall(attempts, installed, toplevels, views C.int) {
	state.Lock()
	defer state.Unlock()
	state.status.Attempts = int(attempts)
	state.status.Installed = installed != 0
	state.status.Toplevels = int(toplevels)
	state.status.Views = int(views)
}

// dshRecordPermissionRequest 由 C 侧回调：记录一次权限请求与判定结果。
//
//export dshRecordPermissionRequest
func dshRecordPermissionRequest(isUserMedia, isAudio, isVideo, isDisplay, allowed C.int) {
	state.Lock()
	defer state.Unlock()
	state.status.Requests++
	if allowed != 0 {
		state.status.Allowed++
	} else {
		state.status.Denied++
	}
	state.status.LastKind = describeRequest(isUserMedia != 0, isAudio != 0, isVideo != 0, isDisplay != 0)
}

// dshShouldAllowPermission 是 C 侧回调的决策入口：把四个设备标志映射成
// PermissionFacts 后交给 AllowPermission，返回 1 表示放行。
//
// 参数与返回值都用 C.int 而非 bool，因为该函数由 cgo 导出给 C 调用，签名必须与
// _cgo_export.h 中的声明一致。
//
//export dshShouldAllowPermission
func dshShouldAllowPermission(isUserMedia, isAudio, isVideo, isDisplay C.int) C.int {
	if AllowPermission(PermissionFacts{
		IsUserMedia: isUserMedia != 0,
		IsAudio:     isAudio != 0,
		IsVideo:     isVideo != 0,
		IsDisplay:   isDisplay != 0,
	}) {
		return 1
	}
	return 0
}

// describeRequest 生成请求类型的可读描述。
func describeRequest(userMedia, audio, video, display bool) string {
	if !userMedia {
		return "non-usermedia"
	}
	kind := "usermedia"
	if audio {
		kind += "/audio"
	}
	if video {
		kind += "/video"
	}
	if display {
		kind += "/display"
	}
	if !audio && !video && !display {
		kind += "/unknown"
	}
	return kind
}
