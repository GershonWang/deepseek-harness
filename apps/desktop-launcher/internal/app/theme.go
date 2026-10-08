// Package app - 壳界面主题的持久化
//
// 主题真源是系统：前端用 prefers-color-scheme 读到的就是它，界面本身也一直跟着系统走。
// 这里只把最近一次观测到的取值存下来，供**下次启动的首帧窗口底色**使用（审计 28）——
// 首帧必须早于前端，Go 侧那一刻读不到系统主题，只能沿用上次的值。
package app

// ThemeDark 与 ThemeLight 是配置里允许的两个主题取值。
const (
	ThemeDark  = "dark"
	ThemeLight = "light"
)

// FirstFrameBackground 返回窗口首帧底色的 RGB 分量。
//
// 取值与 frontend/styles.css 的 --bg 一致（暗 #1e1e1e、亮 #f5f5f5）：不一致时页面绘制
// 之前会闪一下与当前主题相反的底色。主题未知（首次启动，前端还没回推过）时用暗色，
// 与旧行为一致；前端起来后会回推实际主题，下次启动即跟上（审计 28）。
//
// 放在这里而不是 main 包：main 依赖 cgo 与 GTK 开发库，普通机器上编译不了，
// 这条判定没有理由跟着一起失去可测性。
//
// @param theme 配置里记录的主题，空串表示尚未记录。
// @returns 首帧底色的红、绿、蓝分量。
func FirstFrameBackground(theme string) (r, g, b uint8) {
	if theme == ThemeLight {
		return 245, 245, 245
	}
	return 30, 30, 30
}

// SetTheme 记录前端回推的界面主题，由 Wails 绑定给前端调用。
//
// 只影响下次启动的首帧窗口底色，本次界面由页面自己的 color-scheme 决定，因此这里
// 不推任何事件、也不重绘。只在取值变化时落盘：前端每次启动、以及系统主题每次切换
// 都会调用它。
//
// @param dark 系统当前是否处于深色主题。
func (a *App) SetTheme(dark bool) {
	theme := ThemeLight
	if dark {
		theme = ThemeDark
	}
	cfg, err := LoadAppConfig(a.home)
	if err != nil || cfg.Theme == theme {
		return
	}
	cfg.Theme = theme
	_ = SaveAppConfig(a.home, cfg)
}
