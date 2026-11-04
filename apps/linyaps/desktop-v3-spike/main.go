// 本文件是 Wails v3 技术验证 spike 的入口：把一个最小但真实的 v3 客户端组装起来，
// 在目标栈（Deepin 25 / GTK4 / WebKitGTK 6.0）上回答升级前必须先有答案的几个问题。
//
// 为什么单独成一个 Go module 而不是直接改 desktop-launcher：v3 与 v2 的模块路径、图形栈
// （GTK3 → GTK4）、前端运行时模型（全局 window.go → ES module）都不同，两套栈塞进同一棵
// go.mod 既无法并存，也会把「验证结论」和「真实迁移」搅在一起。验证通过后，本模块的组装
// 方式就是升级版客户端的骨架：服务化绑定、窗口事件钩子、无打包器的静态壳、按环境选择的
// GPU 策略。验证矩阵、判定标准与已知证据见 README.zh.md 与 docs/linyaps-runtime-evidence.md。
package main

import (
	"embed"
	"log/slog"
	"os"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"

	"github.com/deepseek-ai/deepseek-harness/apps/linyaps/desktop-v3-spike/internal/probe"
	"github.com/deepseek-ai/deepseek-harness/apps/linyaps/desktop-v3-spike/internal/webviewperm"
)

// 前端资源。与 desktop-launcher 一样逐项列出而不整体嵌入：spike 的 frontend/ 下只允许
// 出现真正随包的文件，漏登记或误登记都能在评审时一眼看出。
//
//go:embed frontend/index.html frontend/app.js frontend/styles.css
var assets embed.FS

// main 组装并运行 spike 窗口。
func main() {
	started := time.Now()

	// 没有会话就一定会停在 GTK 初始化失败上，而且那行报错与「显卡/驱动问题」极易混淆。
	// 先在 Go 侧拦住并给出可执行的提示，避免把一个环境问题误读成 v3 的兼容性结论。
	if err := probe.Preflight(); err != nil {
		slog.Error("spike 前置检查未通过", "err", err)
		os.Exit(2)
	}

	app := application.New(application.Options{
		Name:        "dsh-v3-spike",
		Description: "Wails v3 技术验证（GTK4 + WebKitGTK 6.0）",
		// BundledAssetFileServer 与 AssetFileServerFS 的唯一实质差别是前者额外在
		// /wails/runtime.js 提供 runtime。静态壳没有打包器，必须走这条路才能在纯
		// HTML/JS 页面里拿到 runtime（拖拽区域也依赖 runtime 初始化）。
		Assets: application.AssetOptions{
			Handler: application.BundledAssetFileServer(assets),
		},
		Linux: application.LinuxOptions{
			ProgramName: "dsh-v3-spike",
		},
	})

	// 服务在 app.Run 之前注册即可。依赖直接传 application 实例：退出、发事件、订阅事件
	// 本来就是 App 的能力，不拆成可空回调（第一版曾因漏传其中一个回调在真机上空指针崩溃）。
	svc := probe.New(app, probe.ReportPath(), probe.AutoClose(), started)
	app.RegisterService(application.NewService(svc))

	// 窗口在 Run 之前创建：v3 会在 startup 阶段运行待启动窗口，这与 v2 的
	// 「Run 里的 options 就是唯一窗口」是同一个时机的两种写法。
	window := app.Window.NewWithOptions(application.WebviewWindowOptions{
		Title:            "DSH · Wails v3 spike",
		Width:            1000,
		Height:           700,
		MinWidth:         720,
		MinHeight:        480,
		Frameless:        true,
		StartState:       application.WindowStateNormal,
		InitialPosition:  application.WindowCentered,
		BackgroundColour: application.NewRGB(24, 26, 30),
		Linux: application.LinuxWindow{
			// GTK4 后端的 WebKitGTK 6.0 已删除 ON_DEMAND 策略，只认 ALWAYS/NEVER；
			// 这里用环境变量切换，才能在同一台机器上对比两种渲染路径的差异。
			WebviewGpuPolicy: probe.GPUPolicy(),
		},
	})

	// WindowRuntimeReady 是 v3 里「前端 runtime 已初始化、可以安全收发事件」的唯一信号。
	// v2 的 OnDomReady 在 v3.1.0 中已不存在（application.Options 没有该字段），
	// 所以壳里所有「页面就绪后再做」的动作都必须挂在这个事件上。
	window.OnWindowEvent(events.Common.WindowRuntimeReady, func(*application.WindowEvent) {
		probe.Mark("runtime-ready", time.Since(started))
		app.Event.Emit("spike:announce", map[string]any{
			"sinceStartMs": time.Since(started).Milliseconds(),
			"backend":      probe.BuildBackend,
			"stack":        probe.RuntimeStackVersion(),
		})
		// 只能在这里挂：更早调用时 GTK 尚未初始化，主循环上的 g_timeout 回调会抢先
		// 触碰控件树，实测在窗口创建阶段直接 SIGSEGV。页面侧的权限探测已延后 2.5s，
		// 足够让这次挂载先生效。
		webviewperm.Install()
	})

	// 关窗钩子必须用 RegisterHook 才能拦截；spike 只记录不拦截，用来确认钩子在
	// 无边框窗口 + 自绘标题栏下确实会触发（托盘常驻的客户端要靠它保存几何）。
	window.RegisterHook(events.Common.WindowClosing, func(*application.WindowEvent) {
		probe.Mark("window-closing", time.Since(started))
		svc.Flush("窗口关闭前补写报告")
	})

	if err := app.Run(); err != nil {
		slog.Error("spike 运行失败", "err", err)
		os.Exit(1)
	}
}
