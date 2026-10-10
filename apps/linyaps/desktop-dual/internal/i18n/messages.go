package i18n

// messages 是托盘的文案字典：每种语言同键集，缺键由 T 回退英文。
//
// 为什么形态名（薄壳版／官方版）进字典而不再像悬浮球那样用 `web`／`desktop` 标记：
// 标记当年是给球下方的技术标识用的，两种语言写法相同因而不进字典；托盘只有菜单与提示，
// 这里显示的是给用户看的名字，必须本地化。
var messages = map[Locale]map[string]string{
	Zh: {
		"tray.switchToShell":    "切换到薄壳版",
		"tray.switchToOfficial": "切换到官方版",
		"tray.modeShell":        "薄壳版",
		"tray.modeOfficial":     "官方版",
		"tray.noClient":         "客户端未运行",
		"tray.defaultShell":     "默认启动薄壳版",
		"tray.defaultOfficial":  "默认启动官方版",
		"tray.quit":             "退出切换器",
		"tray.switchFailed":     "切换客户端失败",
		"tray.tooltip":          "DeepSeek Harness 切换器",
	},
	En: {
		"tray.switchToShell":    "Switch to thin shell",
		"tray.switchToOfficial": "Switch to official client",
		"tray.modeShell":        "Thin shell",
		"tray.modeOfficial":     "Official client",
		"tray.noClient":         "No client running",
		"tray.defaultShell":     "Start the thin shell by default",
		"tray.defaultOfficial":  "Start the official client by default",
		"tray.quit":             "Quit the switcher",
		"tray.switchFailed":     "Switching the client failed",
		"tray.tooltip":          "DeepSeek Harness switcher",
	},
}
