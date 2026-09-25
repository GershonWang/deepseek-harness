//go:build linux

package webviewperm

/*
#cgo linux pkg-config: gtk+-3.0
#cgo !webkit2_41 pkg-config: webkit2gtk-4.0
#cgo webkit2_41 pkg-config: webkit2gtk-4.1

// 由 permission_linux.c 实现：遍历顶层窗口找到内嵌 WebKitWebView 并连接
// permission-request 信号。返回 1 表示已挂载，0 表示未找到视图。
int dshInstallPermissionHandler(void);
*/
import "C"

import "log"

// Install 挂载内嵌 WebView 的麦克风权限处理。
//
// 必须在 GTK 主线程调用。Wails 的 OnDomReady 满足该前提：它由 GTK 主循环内的 C
// 回调直接进入（wails/v2 internal/frontend/desktop/linux/frontend.go 的
// processMessage，由 window.c 的 load-changed 处理器调用），因此这里直接访问
// 控件树，不做线程切换。此时页面已就绪，WebView 必然已经创建并放入窗口。
//
// 找不到 WebView 时只记一条日志：界面已经可用，麦克风权限缺失不该影响启动。
func Install() {
	if C.dshInstallPermissionHandler() == 0 {
		log.Printf("未找到内嵌 WebKitWebView，麦克风权限未挂载")
		return
	}
	log.Printf("麦克风权限处理已挂载")
}

// dshShouldAllowPermission 是 C 侧回调的决策入口：把三个设备标志映射成
// PermissionFacts 后交给 AllowPermission，返回 1 表示放行。
//
// 参数与返回值都用 C.int 而非 bool，因为该函数由 cgo 导出给 C 调用，签名必须与
// _cgo_export.h 中的声明一致。
//
//export dshShouldAllowPermission
func dshShouldAllowPermission(isUserMedia, isAudio, isVideo, isDisplay C.int) C.int {
	allowed := AllowPermission(PermissionFacts{
		IsUserMedia: isUserMedia != 0,
		IsAudio:     isAudio != 0,
		IsVideo:     isVideo != 0,
		IsDisplay:   isDisplay != 0,
	})
	if allowed {
		return 1
	}
	return 0
}
