// 主入口：组装 Wails 应用（内嵌前端 + 绑定 App 控制器），并在进程被外部
// 信号终止时停掉 harness 子进程。
package main

import (
	"context"
	"embed"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"

	"github.com/deepseek-ai/deepseek-harness/apps/desktop-launcher/internal/app"
	"github.com/deepseek-ai/deepseek-harness/apps/desktop-launcher/internal/appenv"
	"github.com/deepseek-ai/deepseek-harness/apps/desktop-launcher/internal/packaging"
	"github.com/deepseek-ai/deepseek-harness/apps/desktop-launcher/internal/toolchain"
	"github.com/deepseek-ai/deepseek-harness/apps/desktop-launcher/internal/webviewperm"
)

// 前端资源。逐项列出随包文件，不用 `all:frontend` 整体嵌入：
// frontend/ 下还住着三个仅开发用的文件（test-app.cjs、test-i18n.cjs、
// tools/preview.mjs，合计约 149 KB），整体嵌入会把它们塞进二进制；而 `all:` 前缀连
// 点号或下划线开头的游离文件也一并嵌入，遇到 Go 拒绝的嵌入文件名还会让构建失败。
// 列成清单后默认从「目录下什么都进包」翻转为「只有列出的进包」。
//
// locales/ 与 vendor/ 用目录模式：新增语言字典或字体无需改这里，但写进这两个目录的
// 任何文件都会随包，所以预览产物必须留在 frontend/ 之外（见
// frontend/tools/preview.mjs 的产物守卫）。新增 frontend/ 下的顶层资源要在此登记，
// 漏登记由 main_test.go 断言点名。
//
//go:embed frontend/index.html frontend/app.js frontend/styles.css frontend/i18n.js frontend/locales frontend/vendor
var assets embed.FS

func main() {
	home, _ := os.UserHomeDir()
	// 改名工具的历史安装先搬到新 ID，再走下面的软链自愈：否则旧 ID 名下的目录与
	// current 软链会成为新清单里查不到的孤儿，用户既看不到也用不上那份安装。
	// 迁移幂等且逐项留痕，失败不拦启动。
	if migrated := toolchain.MigrateLegacyToolIDs(toolchain.InstallDir(home)); len(migrated) > 0 {
		log.Printf("工具链 ID 迁移: %v", migrated)
	}
	// 启动自愈：重建 ~/.dsh-tools/bin 软链，保证已装工具链在重装/更新/HOME 迁移后
	// 仍自动可用；随后 ConfigureChildEnv 把该目录注入子进程 PATH。
	// 索引里越界的 bin_names/bin_dirs 与越界的 current 目标会被跳过，这里留痕，
	// 不静默吞掉（自愈本身失败不该拦住启动）。
	if err := toolchain.ReconcileBinLinks(toolchain.InstallDir(home)); err != nil {
		log.Printf("工具链软链自愈有被拒绝的条目: %v", err)
	}
	appenv.ConfigureChildEnv(home)
	packaging.ConfigureWebKitHelperPath()
	// 同样须在 wails.Run 之前：做音频设备枚举的是 WebKit 的 WebProcess，它继承本
	// 进程环境，插件搜索路径要在它启动前就位。
	packaging.ConfigureGStreamerPlugins()
	// 须在 wails.Run 之前：webkit2gtk 只在 GTK/WebKit 初始化时读取渲染后端的开关。
	packaging.ConfigureWebKitRendering()
	// 同样须在 wails.Run 之前：GTK/WebKit 首次读取 FONTCONFIG_FILE 时即固定字体配置。
	packaging.ConfigureFontConfig()

	resolved := appenv.Resolve()
	controller := app.New(resolved, home, app.ExternalConfigFilePath())

	// 读取上次保存的窗口状态（尺寸、最大化等），读取失败时静默回退默认值。
	windowState, _ := app.LoadWindowState(home)

	// 外部终止（SIGTERM/SIGINT，如桌面管理器退出）时停 harness，避免子进程残留。
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigCh
		controller.Shutdown()
		os.Exit(0)
	}()

	// 起始窗口状态：上次是最大化则本次也最大化
	startState := options.Normal
	if windowState.Maximized {
		startState = options.Maximised
	}

	err := wails.Run(&options.App{
		Title:     "DeepSeek Harness",
		Width:     windowState.Width,
		Height:    windowState.Height,
		MinWidth:  900,
		MinHeight: 600,
		// 无边框窗口：自绘标题栏（frontend/#titlebar）承载品牌/按钮/窗口控制，
		// 通过 --wails-draggable 拖拽、边缘自动 resize。
		Frameless:        true,
		WindowStartState: startState,
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		BackgroundColour: &options.RGBA{R: 30, G: 30, B: 30, A: 255},
		OnStartup:        controller.OnStartup,
		// 内嵌 WebView 的麦克风权限只能在页面就绪后挂：此时 WebKit 已创建视图并把它
		// 放进窗口，GTK 侧才遍历得到（见 internal/webviewperm）；挂早了找不到视图。
		OnDomReady: func(context.Context) {
			webviewperm.Install()
		},
		OnShutdown: controller.OnShutdown,
		// 窗口关闭前保存尺寸/最大化状态：此时窗口仍存活，能读到真实值
		// （OnShutdown 时窗口已销毁，只能读到 0）。
		OnBeforeClose: controller.OnBeforeClose,
		Bind:          []interface{}{controller},
	})
	if err != nil {
		log.Fatalf("dsh-desktop: %v", err)
	}
}
