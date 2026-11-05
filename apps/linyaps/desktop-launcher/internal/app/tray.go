// 本文件在客户端进程内创建系统托盘（Wails v3 的 SystemTray）。
//
// 为什么放在这里而不是继续用 dual 的独立切换器：v2 时代托盘只能由另一个进程提供，
// 于是有了「切换器 + Unix socket 唤回 + hide-on-close 跨进程约定」那一整套绕行；
// v3 把托盘做进了框架，单客户端也能常驻。合进来之后：
//   - 关窗隐藏与托盘唤回都在本进程内闭环，不再依赖 socket；
//   - 托盘菜单的「退出」直接走应用退出，harness 子进程由 ServiceShutdown 收尾；
//   - dual 包的 DSH_DESKTOP_HIDE_ON_CLOSE 约定保持兼容（见 ResolveCloseAction）。
package app

import (
	"log"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// TrayConfig 是托盘的静态配置。
type TrayConfig struct {
	// Icon 是托盘图标（PNG 原始字节）。
	Icon []byte
	// Label 是托盘提示文字，也是部分桌面环境下显示的名称。
	Label string
	// ShowLabel / AboutLabel / QuitLabel 是菜单文案，由调用方按当前语言提供。
	ShowLabel  string
	AboutLabel string
	QuitLabel  string
	// OnAbout 是「关于」菜单项的动作。
	OnAbout func()
}

// SetupTray 创建系统托盘并挂上菜单，返回是否创建成功。
//
// 失败不拦启动：没有托盘时窗口照常可用，只是关窗语义要退回「关窗即退出」
// （见 ResolveCloseAction），否则用户会关掉窗口后既看不到界面也找不回进程。
//
// @param app - v3 应用实例。
// @param window - 主窗口；托盘菜单的「显示窗口」作用于它。
// @param cfg - 图标与标签。
// @param quit - 退出动作（菜单里的「退出」调用）。
// @returns true 表示托盘已创建。
func SetupTray(app *application.App, window application.Window, cfg TrayConfig, quit func()) (ok bool) {
	if app == nil || window == nil || len(cfg.Icon) == 0 {
		return false
	}
	// 托盘依赖桌面环境的 StatusNotifier/AppIndicator 支持，个别会话下会在初始化时
	// panic；那是「没有托盘」而不是「应用该崩」，因此收敛成返回 false。
	defer func() {
		if r := recover(); r != nil {
			log.Printf("托盘初始化失败（将退回关窗即退出）: %v", r)
			ok = false
		}
	}()

	tray := app.SystemTray.New()
	if tray == nil {
		return false
	}
	tray.SetIcon(cfg.Icon)
	tray.SetLabel(cfg.Label)

	menu := app.NewMenu()
	menu.Add(cfg.ShowLabel).OnClick(func(*application.Context) {
		window.Show()
		window.Focus()
	})
	menu.Add(cfg.AboutLabel).OnClick(func(*application.Context) {
		if cfg.OnAbout != nil {
			cfg.OnAbout()
		}
	})
	menu.AddSeparator()
	menu.Add(cfg.QuitLabel).OnClick(func(*application.Context) {
		quit()
	})
	tray.SetMenu(menu)

	// 左键单击托盘图标等同于「把窗口叫回来」：隐藏状态下的主要操作就是它。
	tray.OnClick(func() {
		window.Show()
		window.Focus()
	})

	tray.Run()
	return true
}
