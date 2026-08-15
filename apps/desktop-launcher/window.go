package main

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

	w.Navigate(url)

	// w.Run() 阻塞直到用户关闭窗口
	w.Run()

	// 窗口已关闭，停止子进程
	sup.Stop()
}

// configureWebKitHelperPath 让 webkit2gtk 找到其辅助进程
// （WebKitNetworkProcess / WebKitWebProcess / WebKitGPUProcess）。
//
// webkit2gtk 把辅助进程路径在编译期硬编码为
// /usr/lib/x86_64-linux-gnu/webkit2gtk-4.1/，而玲珑容器内该路径在只读
// base（无 webkit），真实 helper 随 apt depends 打进 ${PREFIX}/lib/...。
// 用 WebKitGTK 官方支持的 WEBKIT_EXEC_PATH 环境变量覆盖。
// 开发态（未打包）不动：helper 在系统标准路径，默认即正确。
func configureWebKitHelperPath() {
	exe, err := os.Executable()
	if err != nil {
		return
	}
	prefix := filepath.Dir(filepath.Dir(exe)) // .../files/bin -> .../files
	helperDir := filepath.Join(prefix, "lib", "x86_64-linux-gnu", "webkit2gtk-4.1")
	network := filepath.Join(helperDir, "WebKitNetworkProcess")
	if _, statErr := os.Stat(network); statErr == nil {
		_ = os.Setenv("WEBKIT_EXEC_PATH", helperDir)
	}
}
