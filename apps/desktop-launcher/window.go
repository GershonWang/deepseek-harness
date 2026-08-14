package main

import (
	"github.com/webview/webview_go"
)

// openWindow 创建 webkit2gtk 窗口并加载 URL。
// w.Run() 会阻塞直到窗口关闭，退出后触发 sup.Stop()。
func openWindow(url string, sup *Supervisor) {
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
