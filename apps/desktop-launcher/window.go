package main

/*
#cgo pkg-config: gtk+-3.0
#include <gtk/gtk.h>

// dshDeleteEvent 处理窗口关闭按钮（WM_DELETE_WINDOW）。返回 FALSE 让 GTK
// 默认处理器运行（gtk_widget_destroy），从而触发 webkit 已连接的 destroy ->
// terminate -> gtk_main_quit 链；否则（如 webview_go 默认）delete-event 只
// 隐藏窗口不销毁，w.Run() 永不返回，进程无法退出。
gboolean dshDeleteEvent(GtkWidget *widget, GdkEvent *event, gpointer data) {
  (void)event; (void)data;
  // 明确销毁窗口：webview_go 未处理 delete-event，GTK 默认行为在此
  // 环境下不销毁窗口。主动调用 gtk_widget_destroy 触发 webkit 的
  // destroy -> terminate -> gtk_main_quit 关闭链。
  if (widget != NULL) {
    gtk_widget_destroy(widget);
  }
  return TRUE; // 已处理，阻止 GTK 默认
}
*/
import "C"

import (
	"os"
	"path/filepath"

	"github.com/webview/webview_go"
)

// openWindow 创建 webkit2gtk 窗口并加载 URL。
// w.Run() 会阻塞直到窗口关闭，退出后触发 sup.Stop()。
func openWindow(url string, sup *Supervisor) {
	configureWebKitHelperPath()
	w := webview.New(false)
	defer w.Destroy()

	w.SetTitle("DeepSeek Harness")
	w.SetSize(1280, 800, webview.HintNone)

	// webview_go 未处理窗口 delete-event（点关闭按钮/Alt+F4），窗口只隐藏
	// 不销毁，w.Run() 不会返回。手动连接 delete-event 并在处理器里主动
	// gtk_widget_destroy，触发 webkit 的 destroy -> terminate 关闭链。
	connectDeleteEvent(w)

	// 底部状态栏、服务器/关于按钮、状态轮询、窗口居中与外部连接导航
	// Navigate 闭包只在 GTK 主线程被触发(弹框回调与 idle 回调),线程安全。
	installDesktopUI(w.Window(), sup, func(u string) {
		w.Navigate(u)
	})

	w.Navigate(url)

	// w.Run() 阻塞直到用户关闭窗口
	w.Run()

	// 窗口已关闭，停止子进程
	sup.Stop()
}

// connectDeleteEvent 给 GTK 窗口连接 delete-event 处理器。
// w.Window() 返回 GtkWidget*；空指针时静默跳过（不影响后续）。
func connectDeleteEvent(w webview.WebView) {
	win := w.Window()
	if win == nil {
		return
	}
	// g_signal_connect 是宏，cgo 用其展开后的实际函数 g_signal_connect_data
	C.g_signal_connect_data(
		C.gpointer(win),
		C.CString("delete-event"),
		C.GCallback(C.dshDeleteEvent),
		nil,
		nil,
		0,
	)
}

// configureWebKitHelperPath 让 webkit2gtk 找到其辅助进程
// （WebKitNetworkProcess / WebKitWebProcess / WebKitGPUProcess）。
//
// 背景：Debian 正式构建的 webkit2gtk 把 helper 目录硬编码在
// libwebkit2gtk-4.1.so.0 里（/usr/lib/x86_64-linux-gnu/webkit2gtk-4.1），
// 运行时 /usr 只读、玲珑 layer 不导出 /usr 写入。打包时已用
// patch-webkit-exec-path.sh 把该字符串字节替换为 /tmp/dsh-webkit-4.1，
// 这里在启动时把 /tmp/dsh-webkit-4.1 建为指向 ${PREFIX} 下真实 helper
// 目录的符号链接。注入 bundle 用 WEBKIT_INJECTED_BUNDLE_PATH 覆盖
// （该变量在正式构建有效，与 WEBKIT_EXEC_PATH 不同）。
// 开发态（未打包）不动：helper 在系统标准路径，默认即正确。
func configureWebKitHelperPath() {
	exe, err := os.Executable()
	if err != nil {
		return
	}
	prefix := filepath.Dir(filepath.Dir(exe)) // .../files/bin -> .../files
	helperDir := filepath.Join(prefix, "lib", "x86_64-linux-gnu", "webkit2gtk-4.1")
	network := filepath.Join(helperDir, "WebKitNetworkProcess")
	if _, statErr := os.Stat(network); statErr != nil {
		return // 开发态：helper 在系统路径，无需处理
	}
	// 打包态：/tmp 可写，建符号链接 /tmp/dsh-webkit-4.1 -> 真实 helper 目录
	const shortPath = "/tmp/dsh-webkit-4.1"
	if _, statErr := os.Lstat(shortPath); statErr != nil {
		_ = os.Symlink(helperDir, shortPath)
	}
	// injected-bundle 路径正式构建支持环境变量覆盖
	_ = os.Setenv("WEBKIT_INJECTED_BUNDLE_PATH", filepath.Join(helperDir, "injected-bundle"))
}
