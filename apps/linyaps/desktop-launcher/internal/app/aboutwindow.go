// 本文件负责「关于」独立窗口：创建、唤回与回退。
//
// 为什么独立成窗口：关窗隐藏或最小化后，主窗口 webview 里的壳内模态框不可见——
// 它属于主窗口 DOM，无法脱离主窗口显示。独立窗口由 Wails 的窗口管理器持有，
// 主窗口处于任何状态都能单独出现，且不把主界面拉回前台。
package app

import (
	"errors"

	"github.com/wailsapp/wails/v3/pkg/application"
)

const (
	// aboutWindowName 是独立关于窗口的名字：重复点击托盘「关于」时按名字找回
	// 已打开的窗口，避免每点一次多一个窗口。
	aboutWindowName = "about"
	// aboutWindowWidth 与 aboutWindowHeight 固定跟随内容：窗口不可缩放。
	// 宽度是 500 的卡片（与壳内 .modal-about 一致，按仓库地址单行展示确定）加两侧
	// 各 10px 留白；高度按卡片实际内容（logo、版本、包 ID 与四行字段，实测约 392px）
	// 加 body 上下 10px 内边距，再留约 14px 余量——520 时内容居中后上下各空出 60px
	// 以上，观感上像一张空窗。
	aboutWindowWidth  = 520
	aboutWindowHeight = 420
)

// ShowAbout 展示「关于」窗口。
//
// 唯一入口是托盘菜单（见 main.go）。壳运行时尚未绑定、或独立窗口建不出来时，
// 退回 v3 的 Info 对话框——那种情况下没有页面可承载数据，对话框是唯一能显示
// 内容的载体。主窗口可见与否都不影响这条路径：窗口不会因此被唤回。
func (a *App) ShowAbout() {
	if a.app == nil {
		return
	}
	if !a.bound {
		a.showAboutDialog()
		return
	}
	if err := a.presentAboutWindow(); err != nil {
		a.logf("关于窗口打开失败，退回 Info 对话框: %v", err)
		a.showAboutDialog()
	}
}

// presentAboutWindow 打开或唤回独立关于窗口。
// @returns 创建窗口失败的原因；已有窗口被唤回时返回 nil。
func (a *App) presentAboutWindow() error {
	if existing, ok := a.app.Window.GetByName(aboutWindowName); ok && existing != nil {
		existing.UnMinimise()
		existing.Show()
		existing.Focus()
		return nil
	}

	r, g, b := FirstFrameBackground(a.theme())
	window := a.app.Window.NewWithOptions(aboutWindowOptions(a.t("about.title"), r, g, b))
	if window == nil {
		return errors.New("窗口管理器返回空窗口")
	}
	window.Show()
	window.Focus()
	return nil
}

// theme 返回上次记录的界面主题，供关于窗口的首帧底色使用。
//
// 读配置失败按「未记录」处理（FirstFrameBackground 会落到暗色），与主窗口首帧的
// 降级一致：首帧底色只影响页面绘制前的极短一瞬，不值得为它拦下窗口创建。
// @returns 配置里的主题取值，空串表示尚未记录。
func (a *App) theme() string {
	cfg, err := LoadAppConfig(a.home)
	if err != nil {
		return ""
	}
	return cfg.Theme
}

// aboutWindowOptions 组装关于窗口的静态选项。
//
// 抽成函数是为了让尺寸、URL 与不可缩放这些决定窗口形态的字段可被断言：它们写错
// 不会编译失败，只会表现为内容被裁掉或窗口被拖成一片空白。
// @param title - 窗口标题（已本地化）。
// @param r/g/b - 首帧底色分量。
// @returns 窗口选项。
func aboutWindowOptions(title string, r, g, b uint8) application.WebviewWindowOptions {
	return application.WebviewWindowOptions{
		Name:             aboutWindowName,
		Title:            title,
		URL:              "/about.html",
		Width:            aboutWindowWidth,
		Height:           aboutWindowHeight,
		MinWidth:         aboutWindowWidth,
		MinHeight:        aboutWindowHeight,
		DisableResize:    true,
		InitialPosition:  application.WindowCentered,
		BackgroundColour: application.NewRGB(r, g, b),
		Linux:            WailsLinuxOptions(),
	}
}
