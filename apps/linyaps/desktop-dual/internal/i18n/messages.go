package i18n

// messages 是托盘的文案字典：每种语言同键集，缺键由 T 回退英文。
//
// 为什么形态名（薄壳版／官方版）进字典而不再像悬浮球那样用 `web`／`desktop` 标记：
// 标记当年是给球下方的技术标识用的，两种语言写法相同因而不进字典；托盘只有菜单与提示，
// 这里显示的是给用户看的名字，必须本地化。
//
// 失败标题按动作分开（显示／切换／设默认／关于）：托盘形态没有窗口，桌面通知是唯一能
// 说明「刚才那一下为什么没成」的渠道，笼统的一句「操作失败」会让用户不知道是哪一步。
//
// 提示的标题行（tray.tooltip）只写产品名：当前形态由提示正文承载，标题里再补「切换器」
// 既不是用户要看的信息，也和菜单里的动作词重复。
var messages = map[Locale]map[string]string{
	Zh: {
		"tray.show":             "显示",
		"tray.switchToShell":    "切换到薄壳版",
		"tray.switchToOfficial": "切换到官方版",
		"tray.modeShell":        "薄壳版",
		"tray.modeOfficial":     "官方版",
		"tray.noClient":         "客户端未运行",
		"tray.settings":         "设置",
		"tray.defaultShell":     "默认启动薄壳版",
		"tray.defaultOfficial":  "默认启动官方版",
		"tray.about":            "关于",
		"tray.quit":             "退出",
		"tray.showFailed":       "显示客户端失败",
		"tray.switchFailed":     "切换客户端失败",
		"tray.setDefaultFailed": "设置默认形态失败",
		"tray.aboutFailed":      "打开「关于」失败",
		"tray.tooltip":          "DeepSeek Harness",
		// 「关于」的字段名沿用薄壳版原来的关于弹框：同一批事实换了位置，用词不该跟着变，
		// 否则用户会以为看到的是另一份信息。唯一例外是把「上游DSH仓库」写作「DSH仓库」：
		// 它旁边紧挨着玲珑封装仓库，「上游」二字在并列语境里是冗余的。
		"about.title":        "关于",
		"about.dshVersion":   "DSH版本",
		"about.upstreamRepo": "DSH仓库",
		"about.copy":         "复制",
		"about.copied":       "已复制",
		"about.openFailed":   "打不开这个链接",
		"about.packager":     "玲珑封装作者",
		"about.repo":         "玲珑封装仓库",
		"about.mode":         "当前形态",
		"about.package":      "包 ID",
	},
	En: {
		"tray.show":             "Show",
		"tray.switchToShell":    "Switch to thin shell",
		"tray.switchToOfficial": "Switch to official client",
		"tray.modeShell":        "Thin shell",
		"tray.modeOfficial":     "Official client",
		"tray.noClient":         "No client running",
		"tray.settings":         "Settings",
		"tray.defaultShell":     "Start the thin shell by default",
		"tray.defaultOfficial":  "Start the official client by default",
		"tray.about":            "About",
		"tray.quit":             "Quit",
		"tray.showFailed":       "Showing the client failed",
		"tray.switchFailed":     "Switching the client failed",
		"tray.setDefaultFailed": "Setting the default client failed",
		"tray.aboutFailed":      "Opening the about dialog failed",
		"tray.tooltip":          "DeepSeek Harness",
		"about.title":           "About",
		"about.dshVersion":      "DSH version",
		"about.upstreamRepo":    "DSH repository",
		"about.copy":            "Copy",
		"about.copied":          "Copied",
		"about.openFailed":      "Could not open this link",
		"about.packager":        "Linglong packager",
		"about.repo":            "Linglong packaging repo",
		"about.mode":            "Current client",
		"about.package":         "Package ID",
	},
}
