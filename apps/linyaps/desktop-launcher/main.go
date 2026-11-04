// 主入口：组装 Wails v3 应用（内嵌前端 + 绑定 App 控制器），并在进程被外部信号终止时
// 停掉 harness 子进程。
//
// 本二进制只是薄壳客户端。双客户端包里的切换器是 desktop-dual 模块的独立托盘进程，
// 它把本二进制当作「薄壳客户端」拉起；两者不再是同一个可执行文件的不同运行模式。
package main

import (
	"embed"
	"log"
	"os"
	"os/signal"
	"strconv"
	"syscall"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"

	"github.com/deepseek-ai/deepseek-harness/apps/linyaps/desktop-launcher/internal/app"
	"github.com/deepseek-ai/deepseek-harness/apps/linyaps/desktop-launcher/internal/appenv"
	"github.com/deepseek-ai/deepseek-harness/apps/linyaps/desktop-launcher/internal/linglonghost"
	"github.com/deepseek-ai/deepseek-harness/apps/linyaps/desktop-launcher/internal/packaging"
	"github.com/deepseek-ai/deepseek-harness/apps/linyaps/desktop-launcher/internal/toolchain"
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
//go:embed frontend/index.html frontend/boot.js frontend/v2shim.js frontend/app.js frontend/styles.css frontend/i18n.js frontend/locales frontend/vendor
var assets embed.FS

// main 启动薄壳客户端窗口。
func main() {
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
	// ~/.config/dsh-desktop/linglong-bin，紧随其后的 ConfigureChildEnv 把该目录注入
	// 子进程 PATH——模型用既有的 Bash 工具就能构建与打包玲珑应用。
	// 非玲珑容器（开发态直接在宿主上跑）或宿主未装玲珑时探测结果为空，旧包装会被清掉，
	// 不拦启动。
	//
	// 旧落点（~/.dsh-linglong）在换点后不会再被更新或删除，无条件清理一次：宿主开发态
	// 没有宿主逃逸通道，同样不该在用户家目录里留下那份残留。
	linglonghost.PruneLegacy(home)
	if fact, ok := appenv.HostEscape(); ok {
		if n, err := linglonghost.Ensure(home, fact.Rootfs); err != nil {
			log.Printf("玲珑宿主工具链包装未启用: %v", err)
		} else if n > 0 {
			log.Printf("玲珑宿主工具链: %d 条命令可在容器内直接调用", n)
		}
	}
	appenv.ConfigureChildEnv(home)
	packaging.ConfigureBundledRuntime()
	// 同样须在 GTK/WebKit 初始化之前：做音频设备枚举的是 WebKit 的 WebProcess，它继承本
	// 进程环境，插件搜索路径要在它启动前就位。
	packaging.ConfigureGStreamerPlugins()
	// 须在 GTK/WebKit 初始化之前：webkit2gtk 只在 GTK/WebKit 初始化时读取渲染后端的开关。
	packaging.ConfigureWebKitRendering()
	// 同样须在 GTK/WebKit 初始化之前：GTK/WebKit 首次读取 FONTCONFIG_FILE 时即固定字体配置。
	packaging.ConfigureFontConfig()

	// 读取上次保存的配置：窗口几何与主题沿用旧值；WebPort 让 harness 的监听端口跨
	// 启动保持不变，LastSessionID 让 GUI 恢复上次打开的会话。读取失败时静默回退默认值。
	cfg, _ := app.LoadAppConfig(home)
	windowState := cfg.Window

	// 端口是 harness Web GUI 的 origin 的一部分，而 GUI 的界面状态（含「上次会话」）
	// 按 origin 存在 localStorage 里，所以这次实际用的端口要记回去，下次才能接着用
	// 同一个 origin。写配置失败只影响下次启动的还原，不拦本次启动。
	resolved := appenv.Resolve(appenv.Preferences{Port: cfg.WebPort, SessionID: cfg.LastSessionID})
	if resolved.RememberPort {
		// RememberPort 为真时端口必然是 appenv 自己挑出的数字串（环境变量覆盖时不置位），
		// 解析失败意味着这条不变式被打破：只留痕，不拦启动。
		port, err := strconv.Atoi(resolved.Port)
		if err != nil {
			log.Printf("无法记录端口 %q: %v", resolved.Port, err)
		} else if err := app.SaveWebPort(home, port); err != nil {
			log.Printf("端口未落盘: %v", err)
		}
	}
	controller := app.New(resolved, home, app.ExternalConfigFilePath(), app.CloseActionFromEnv())

	// 外部终止（SIGTERM/SIGINT，如桌面管理器退出）时停 harness，避免子进程残留。
	// 走 ShutdownFromSignal 而不是 Shutdown：这条路进来时窗口通常还活着，可以补存
	// 一次窗口几何；而 ServiceShutdown 那条路窗口已销毁，读它要么等不到 GTK 主线程执行、
	// 要么是在读已释放的对象（见 internal/app 的 Shutdown）。
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigCh
		controller.ShutdownFromSignal()
		os.Exit(0)
	}()

	// 托盘唤回：切换器连本进程的唤回 socket，表示「把窗口显示出来」。
	//
	// 为什么不是信号：SIGUSR1 已被 WebKit 的 JavaScriptCore 接管（Go 收不到它），发过来
	// 不是「显示窗口」而是打进 JSC 的 GC 机制——实测客户端进程当场消失、没有任何日志，
	// harness 变成孤儿。见 internal/app 的 ShowSocketRel。
	//
	// 必须在 app.Run 之前监听：窗口就绪之前到达的请求会被 ShowWindow 丢掉，但通道
	// 本身要尽早可用——晚建的那段时间里点托盘会退化成一次拨号失败（用户看到报错），
	// 而不是别的语义。
	// 建立失败不拦启动，只留痕：客户端仍然可用，那一次运行的唤回会退化为切换器报错。
	if stopShow, err := app.ServeShowRequests(app.ShowSocketPath(home), controller.ShowWindow); err != nil {
		log.Printf("唤回通道未建立: %v", err)
	} else {
		defer stopShow()
	}

	// 起始窗口状态：上次是最大化则本次也最大化
	startState := application.WindowStateNormal
	if windowState.Maximized {
		startState = application.WindowStateMaximised
	}

	// 首帧窗口底色由上次记录的主题决定：窗口在页面绘制之前就存在，那一刻 Go 侧读不到
	// 系统主题，只能沿用前端上次回推的值（见 app.FirstFrameBackground）。
	frameR, frameG, frameB := app.FirstFrameBackground(cfg.Theme)

	wailsApp := application.New(application.Options{
		Name:        "DeepSeek Harness",
		Description: "DeepSeek Harness 桌面客户端",
		// 服务在 Run 之前注册即可；v2 的 Bind 列表在这里就是 Services。
		Services: []application.Service{application.NewService(controller)},
		// BundledAssetFileServer 额外提供 /wails/runtime.js：静态壳没有打包器，
		// 前端以 ES module 方式导入它来初始化 runtime（拖拽依赖该初始化）。
		Assets: application.AssetOptions{
			Handler: application.BundledAssetFileServer(assets),
		},
		Linux: application.LinuxOptions{ProgramName: "dsh-desktop"},
	})

	// 无边框窗口：自绘标题栏（frontend/#titlebar）承载品牌/按钮/窗口控制，
	// 通过 --wails-draggable 拖拽、边缘自动 resize。
	window := wailsApp.Window.NewWithOptions(application.WebviewWindowOptions{
		Title:            "DeepSeek Harness",
		Width:            windowState.Width,
		Height:           windowState.Height,
		MinWidth:         900,
		MinHeight:        600,
		Frameless:        true,
		StartState:       startState,
		BackgroundColour: application.NewRGB(frameR, frameG, frameB),
		// 权限策略：语音输入只需要麦克风；摄像头与屏幕共享一律拒绝（屏幕共享请求带
		// video 标志，因此落到同一个 PermissionCamera）。其余权限（通知等）在 Linux 上
		// 由 WebKit 默认处理，即拒绝。
		//
		// v3 内置了这套模型，因此 v2 时代自建的 GTK 权限钩子（internal/webviewperm）
		// 已被移除——真机实测该钩子永远排在内置处理之后，收不到任何请求。
		Permissions: map[application.PermissionType]application.Permission{
			application.PermissionMicrophone: application.PermissionAllow,
			application.PermissionCamera:     application.PermissionDeny,
		},
		Linux: app.WailsLinuxOptions(),
	})

	// 控制器需要 app/window 才能发事件、读写窗口几何；v2 用 OnStartup 的 ctx 承担同一职责。
	controller.Bind(wailsApp, window)

	// 运行时观测：v3 的窗口事件是唯一能确认「前端真的起来了」的信号。
	window.OnWindowEvent(events.Common.WindowRuntimeReady, func(*application.WindowEvent) {
		log.Printf("wails: 前端 runtime 就绪")
	})

	// 关窗钩子：只有 RegisterHook 能取消关闭。CloseHides 下拦下这次关闭并隐藏窗口，
	// 与 v2 的 OnBeforeClose 返回 true 等价。
	window.RegisterHook(events.Common.WindowClosing, func(event *application.WindowEvent) {
		if controller.BeforeClose() {
			event.Cancel()
		}
	})

	// Run 只在主循环结束后返回：正常关窗时 err 为 nil，启动失败时非 nil。
	// 没有这行日志的话，「窗口一闪而过」与「没起来」在输出里完全一样。
	err := wailsApp.Run()
	log.Printf("wails: Run 返回 err=%v（主循环结束）", err)
	if err != nil {
		log.Fatalf("dsh-desktop: %v", err)
	}
}
