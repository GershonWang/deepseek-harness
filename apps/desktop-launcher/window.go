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
