// 本文件收敛窗口层（Wails v2）在 Linux 上的选项，避免这些「平台事实」散落在 main 的
// 组装代码里：策略取值被 Wails 的 Go 枚举与 C 实现错位影响，集中在一处才能被测试固定住。
package app

import "github.com/wailsapp/wails/v2/pkg/options/linux"

// webviewGpuPolicyOnDemand 是「按需开启硬件加速」在 Wails 2.15.0 里真正生效的取值。
//
// 为什么写数值而不是用常量名：Wails 的 Go 枚举次序是
// WebviewGpuPolicyOnDemand=0、WebviewGpuPolicyAlways=1、WebviewGpuPolicyNever=2，
// 而它自己的 C 侧 switch（internal/frontend/desktop/linux/window.c）是
// 0→WEBKIT_HARDWARE_ACCELERATION_POLICY_ALWAYS、1→ON_DEMAND、2→NEVER，两者不一致：
// 按名字传 Always 实际得到「按需」，按名字传 OnDemand 实际得到「始终开启」。
// 这里要的是 WebKitGTK 自己的默认语义（按需），对应数值 1。
//
// 升级 Wails 后若上游修好这处错位，window_test.go 里固定数值的用例会失败——那时应重新
// 核对映射并改成用常量名，而不是让策略悄无声息地换档。
const webviewGpuPolicyOnDemand = linux.WebviewGpuPolicy(1)

// WailsLinuxOptions 返回 launcher 传给 wails.Run 的 Linux 选项。
//
// 必须返回非 nil：Wails 在 options.Linux 为 nil 时会把 WebviewGpuPolicy 强制设为
// WebviewGpuPolicyNever（对 wails#2977 的规避），等效于
// WEBKIT_HARDWARE_ACCELERATION_POLICY_NEVER——WebKitGTK 的加速合成被整体关闭，页面退回
// CPU 绘制。薄壳版此前「复杂界面卡顿、CPU 高、内嵌页面没有 GPU 合成」正是这条造成的：
// 同一台机器、同一份 webkit2gtk，MiniBrowser 能拿到硬件合成缓冲，launcher 却始终拿不到。
//
// 需要彻底关掉加速合成时不要改这里，用既有的 DSH_DESKTOP_WEBKIT_DISABLE_COMPOSITING=1
// （见 packaging.ConfigureWebKitRendering），它按机器生效，不必重新发版。
func WailsLinuxOptions() *linux.Options {
	return &linux.Options{WebviewGpuPolicy: webviewGpuPolicyOnDemand}
}
