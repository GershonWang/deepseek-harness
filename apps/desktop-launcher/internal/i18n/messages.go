// 本文件是 Go 侧用户可见文案的字典。
//
// 为什么与 i18n.go 分开：i18n.go 只放机制（语言解析、查表、渲染），本文件只放内容。
// 两者的变更节奏与评审关注点不同——机制要稳，内容按区域分批迁入（见 docs/i18n.md
// 第六、七节）。键名与前端 locales/*.js 对齐（同名即同义），但占位符各随本语言惯例：
// Go 用 %s/%d，前端用 {name}，共享的是键而不是占位符写法。
//
// 只收「用户能看到」的文案。日志与诊断链（log.Printf、record(fmt.Errorf(...))）不进
// 字典：它们不随语言变化，且让每次日志措辞调整都要动字典只会增加漂移面。
//
// 分隔符（列举顿号、分句分号、列表前导冒号）与句尾标点同样进字典，而不是在拼接处
// 硬编码：它们是语言的一部分，英文用半角加空格、中文用全角，拼接处硬编码会让两处
// 说法分叉。
package i18n

// messages 按语言存放字典。Zh 是键全集真源：En 缺键时回退 Zh，再缺则返回键名本身，
// 让漏配在界面上直接可见（而不是显示空串）。
//
// 键按「区域.用途」命名；普通词（连接、取消、无）落在 common 区，供多处复用。
var messages = map[Locale]map[string]string{
	Zh: {
		"app.notReady":              "应用尚未就绪",
		"client.bootFailedNoReason": "客户端插件加载失败（未提供原因）",
		"common.cancel":             "取消",
		"common.clauseSeparator":    "；",
		"common.connect":            "连接",
		"common.listIntro":          "：",
		"common.listSeparator":      "、",
		"common.none":               "无",
		"doctor.noOutputExit":       "doctor 无输出: %s",
		"doctor.repairFailed":       "修复失败: %s",
		"hosttool.conflictWarning":  "与按需安装同名的命令，宿主挂载优先生效: %s",
		"hosttool.sandboxOnly":      "宿主挂载仅在玲珑打包环境生效（开发态宿主命令本就在 PATH）",
		"runtimeSource.bundled":     "随包",
		"runtimeSource.host":        "宿主导入",
		"runtimeSource.system":      "系统",
		"server.addressInvalid":     "地址无效: %s",
		"server.confirmMessage":     "将连接远端 harness 服务 %s，其命令在远端机器上执行，API key 等配置将发往该机器。确认连接？",
		"server.confirmTitle":       "确认连接",
		"toolchain.activateFailed":  "切换失败: %s",
		"toolchain.installDone":     "工具链 %s 已安装并设为当前版本",
		"toolchain.installFailed":   "工具链 %s 安装失败: %s",
		"toolchain.uninstallFailed": "卸载失败: %s",
		"toolchain.unknownID":       "未知工具链: %s",
		"toolchain.unknownVersion":  "工具链 %s 无版本 %s",
		"toolchain.updatedCount":    "已更新 %d 个工具",
		"toolchain.updatedFailed":   "；失败 %d 个",
		"toolchain.updatedKept":     "。旧版本保留在磁盘上，可在卡片版本下拉中切换或卸载",
	},
	En: {
		"app.notReady":              "The app is not ready yet",
		"client.bootFailedNoReason": "The client plugin failed to load (no reason given)",
		"common.cancel":             "Cancel",
		"common.clauseSeparator":    "; ",
		"common.connect":            "Connect",
		"common.listIntro":          ": ",
		"common.listSeparator":      ", ",
		"common.none":               "None",
		"doctor.noOutputExit":       "dsh doctor produced no output: %s",
		"doctor.repairFailed":       "Repair failed: %s",
		"hosttool.conflictWarning":  "Commands with the same name are also installed on demand; the host mount takes precedence: %s",
		"hosttool.sandboxOnly":      "Host mounts only apply in the Linglong packaged environment (in development the host commands are already on PATH)",
		"runtimeSource.bundled":     "Bundled",
		"runtimeSource.host":        "Host mount",
		"runtimeSource.system":      "System",
		"server.addressInvalid":     "Invalid address: %s",
		"server.confirmMessage":     "Connect to the remote harness service %s? Its commands run on that machine, and configuration such as API keys will be sent there. Confirm the connection?",
		"server.confirmTitle":       "Confirm connection",
		"toolchain.activateFailed":  "Switch failed: %s",
		"toolchain.installDone":     "Toolchain %s installed and set as the current version",
		"toolchain.installFailed":   "Toolchain %s failed to install: %s",
		"toolchain.uninstallFailed": "Uninstall failed: %s",
		"toolchain.unknownID":       "Unknown toolchain: %s",
		"toolchain.unknownVersion":  "Toolchain %s has no version %s",
		"toolchain.updatedCount":    "Updated %d tool(s)",
		"toolchain.updatedFailed":   "; %d failed",
		"toolchain.updatedKept":     ". Older versions stay on disk; switch or uninstall them from the version dropdown on the card",
	},
}
