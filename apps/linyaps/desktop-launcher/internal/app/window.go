// 本文件收敛窗口层（Wails v3）在 Linux 上的选项，避免这些「平台事实」散落在 main 的
// 组装代码里。
package app

import "github.com/wailsapp/wails/v3/pkg/application"

// WailsLinuxOptions 返回窗口的 Linux 选项。
//
// WebviewGpuPolicy 在 v3 的 GTK4 后端只有两种实际效果：Never 关闭加速合成，
// 其余取值一律映射为 Always（WebKitGTK 6.0 已删除 ON_DEMAND 策略）。这里仍显式传入
// OnDemand 表达「按页面需要」的意图，但不要指望它在 GTK4 上与原语义一致——
// 需要彻底关掉加速合成时，用既有的 DSH_DESKTOP_WEBKIT_DISABLE_COMPOSITING=1
// （见 packaging.ConfigureWebKitRendering），它按机器生效，不必重新发版。
//
// v2 时代这里要写死数值 1 来绕开 Go 枚举与 C 实现的错位；v3 的枚举与 C 侧一致，
// 直接使用常量即可（见 window_test.go）。
// @returns 窗口的 Linux 选项。
func WailsLinuxOptions() application.LinuxWindow {
	return application.LinuxWindow{WebviewGpuPolicy: application.WebviewGpuPolicyOnDemand}
}
