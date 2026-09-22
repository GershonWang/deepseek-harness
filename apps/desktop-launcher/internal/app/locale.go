package app

import (
	"github.com/deepseek-ai/deepseek-harness/apps/desktop-launcher/internal/i18n"
)

// 壳语言的读写。语言真源在 iframe 内 harness GUI 的 <html lang>（见
// docs/i18n.md 第五节），这里只是它在壳进程内的镜像：
//
//   - 前端 applyLocale 时经 SetLocale 回推，是唯一写入方；
//   - 前端起来之前（加载页、预检页、启动失败页、Wails 原生对话框）用 New 里按
//     环境语言解析出的初值，与前端按 navigator 兜底同源；
//   - 不做持久化：真源是 GUI 的设置，壳重启后重新解析即可，多存一份只会多一处漂移。

// SetLocale 记录前端回推的生效语言，由 Wails 绑定给前端调用。
//
// 识别不了的取值直接忽略并保持原值：真源在 GUI 侧，这里收到未知语言只可能是三方
// 语言包或协议演进，猜一个内置语言会让界面显示与用户所见不符。
//
// @param id 形如 zh-CN、zh_CN.UTF-8、en 的语言标签。
func (a *App) SetLocale(id string) {
	locale := i18n.Normalize(id)
	if locale == "" {
		return
	}
	a.localeMu.Lock()
	defer a.localeMu.Unlock()
	a.locale = locale
}

// GetLocale 返回当前生效的壳语言，由 Wails 绑定给前端（供调试与断言）。
//
// @returns 内置语言 id：zh 或 en。
func (a *App) GetLocale() string {
	a.localeMu.RLock()
	defer a.localeMu.RUnlock()
	return string(a.locale)
}
