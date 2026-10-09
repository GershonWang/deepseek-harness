// 主入口：组装 Wails 应用（内嵌前端 + 绑定 App 控制器），并在进程被外部
// 信号终止时停掉 harness 子进程。
package main

import (
	"context"
	"embed"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/linux"

	"github.com/deepseek-ai/deepseek-harness/apps/desktop-launcher/internal/app"
	"github.com/deepseek-ai/deepseek-harness/apps/desktop-launcher/internal/appenv"
	"github.com/deepseek-ai/deepseek-harness/apps/desktop-launcher/internal/linglonghost"
	"github.com/deepseek-ai/deepseek-harness/apps/desktop-launcher/internal/packaging"
	"github.com/deepseek-ai/deepseek-harness/apps/desktop-launcher/internal/switchboard"
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
//go:embed frontend/index.html frontend/app.js frontend/styles.css frontend/i18n.js frontend/bubble.html frontend/bubble.js frontend/bubble.css frontend/locales frontend/vendor
var assets embed.FS

// 运行模式：无参数或 --mode=shell 是客户端窗口（薄壳版包的既有行为），
// --mode=bubble 是切换器悬浮球（双客户端包的唯一入口）。两者必须是独立进程：
// Wails v2 只提供单窗口 API，常驻的悬浮球与客户端主窗口无法共存于一个进程。
func main() {
	if runMode(os.Args[1:]) == modeBubble {
		runBubble()
		return
	}
	runClient()
}

// runClient 启动客户端窗口：内嵌 dsh web 的 Go + Wails 薄壳客户端。
func runClient() {
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
	// 宿主玲珑工具链：容器里没有 ll-builder/ll-cli，宿主的玲珑守护进程、层仓库
	// (/var/lib/linglong) 与可写状态目录也只在宿主侧，因此把它们以透传包装的形式放到
	// ~/.dsh-linglong/bin，紧随其后的 ConfigureChildEnv 把该目录注入子进程 PATH——
	// 模型用既有的 Bash 工具就能构建与打包玲珑应用。
	// 非玲珑容器（开发态直接在宿主上跑）或宿主未装玲珑时探测结果为空，旧包装会被清掉，
	// 不拦启动。
	if fact, ok := appenv.HostEscape(); ok {
		if n, err := linglonghost.Ensure(home, fact.Rootfs); err != nil {
			log.Printf("玲珑宿主工具链包装未启用: %v", err)
		} else if n > 0 {
			log.Printf("玲珑宿主工具链: %d 条命令可在容器内直接调用", n)
		}
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

	// 读取上次保存的窗口几何与主题（尺寸、最大化、位置、主题），读取失败时静默回退默认值。
	cfg, _ := app.LoadAppConfig(home)
	windowState := cfg.Window

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

	// 首帧窗口底色由上次记录的主题决定：窗口在页面绘制之前就存在，那一刻 Go 侧读不到
	// 系统主题，只能沿用前端上次回推的值（见 app.FirstFrameBackground）。
	frameR, frameG, frameB := app.FirstFrameBackground(cfg.Theme)

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
		BackgroundColour: &options.RGBA{R: frameR, G: frameG, B: frameB, A: 255},
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

// 两种运行模式。客户端模式是薄壳版包的既有行为（command 不带参数即落到这里），
// 悬浮球模式只由双客户端包使用。
const (
	// modeClient 是客户端窗口模式。
	modeClient = "client"
	// modeBubble 是切换器悬浮球模式。
	modeBubble = "bubble"
)

// bubbleSize 是悬浮球窗口的边长（像素）。窗口不可缩放，边长即命中区域。
const bubbleSize = 72

// runMode 解析命令行里的运行模式。只认 --mode=bubble，其余一律按客户端处理，
// 因此薄壳版包不带参数的启动行为与改动前完全一致。
// @param args - 不含程序名的命令行参数。
// @returns 要启动的运行模式。
func runMode(args []string) string {
	for _, arg := range args {
		if value, ok := strings.CutPrefix(arg, "--mode="); ok && value == modeBubble {
			return modeBubble
		}
	}
	return modeClient
}

// runBubble 启动切换器：一个常驻的置顶悬浮球，单击切换客户端形态，
// 右键菜单设置默认形态。
//
// 窗口无边框且背景透明：悬浮球自己画成圆形，窗口矩形不该可见。
func runBubble() {
	home, _ := os.UserHomeDir()
	// 悬浮球也是 webkit 窗口，需要与客户端相同的三项渲染期配置。
	packaging.ConfigureWebKitHelperPath()
	packaging.ConfigureWebKitRendering()
	packaging.ConfigureFontConfig()

	board := switchboard.New(home)
	if err := board.Start(); err != nil {
		// 默认客户端拉不起来时切换器仍然启动：悬浮球会显示错误，用户点一下即可重试，
		// 直接退出反而让人无从下手。
		log.Printf("切换器: 默认客户端启动失败: %v", err)
	}

	// 外部终止时先停客户端：切换器没了，用户就再也管不到那个后台客户端。
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigCh
		board.Shutdown()
		os.Exit(0)
	}()

	err := wails.Run(&options.App{
		Title:            "DeepSeek Harness",
		Width:            bubbleSize,
		Height:           bubbleSize,
		MinWidth:         bubbleSize,
		MinHeight:        bubbleSize,
		MaxWidth:         bubbleSize,
		MaxHeight:        bubbleSize,
		DisableResize:    true,
		Frameless:        true,
		AlwaysOnTop:      true,
		BackgroundColour: &options.RGBA{R: 0, G: 0, B: 0, A: 0},
		// Linux 后端只有在 WindowIsTranslucent 打开时才把窗口背景的 alpha 归零
		// （Wails 的 window.c：windowIsTranslucent 为真时 colour.alpha = 0）。只设
		// BackgroundColour 的 alpha 不够——那样窗口仍画成不透明黑底，悬浮球会顶着一个
		// 黑色方块，而 CSS 的圆角只在方块内部生效。
		Linux: &linux.Options{WindowIsTranslucent: true},
		AssetServer: &assetserver.Options{
			Assets:     assets,
			Middleware: bubbleEntrypoint,
		},
		OnShutdown: func(context.Context) { board.Shutdown() },
		Bind:       []interface{}{board},
	})
	if err != nil {
		log.Fatalf("dsh-desktop switchboard: %v", err)
	}
}

// bubbleEntrypoint 把悬浮球窗口的根请求改写到 bubble.html，其余请求保持默认链。
// 悬浮球与客户端共用同一份 embed.FS，靠这一层区分入口，不必把两套界面塞进一个页面。
// @param next - AssetServer 的默认处理器。
// @returns 完成路由改写的处理器。
func bubbleEntrypoint(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" || r.URL.Path == "/index.html" {
			r.URL.Path = "/bubble.html"
		}
		next.ServeHTTP(w, r)
	})
}
