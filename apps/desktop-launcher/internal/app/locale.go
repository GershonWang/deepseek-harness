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
	changed := a.locale != locale
	a.locale = locale
	a.localeMu.Unlock()
	if !changed {
		return
	}
	// 语言变了要重推已渲染的快照：Go 侧文案是在渲染那一刻取语言的，之前推出去的旧语言
	// 字符串不会自己变，而工具链弹框正开着时不会有新的 toolchain:status 到来。状态快照
	// 只读内存，同步推；工具链采集含命令探测，走既有的异步 RefreshTools。
	//
	// 没有 Wails ctx 时不推：既没有前端可刷新，采集探测也白跑（测试路径）。
	if a.ctx != nil {
		a.emitStatus()
		a.RefreshTools()
	}
}

// translate 是「键 → 当前语言文案」的渲染函数签名。
//
// 为什么要显式传它而不是让组装函数变成 App 方法：这些函数（通知文案、运行时来源、
// 列表拼接）不持有 App，测试也直接调它们断言输出；把渲染函数作为参数传入，测试里
// 传 i18n.Zh 的渲染即可固定语言，无需为一个字符串拼接构造整个 App。
type translate func(key string, args ...any) string

// t 按当前生效语言渲染一个键。
//
// 语言在渲染那一刻读取，而不是启动时快照：前端可在任意时刻经 SetLocale 回推，而
// 预检结果、安装通知这些文案都在那之后才产生。锁只覆盖读取，渲染本身不持锁。
//
// @param key 点分命名空间的键，见 internal/i18n/messages.go。
// @param args 占位符取值，按文案里的 %s/%d 顺序给出。
// @returns 当前语言下的文案；缺键时为键名本身。
func (a *App) t(key string, args ...any) string {
	a.localeMu.RLock()
	locale := a.locale
	a.localeMu.RUnlock()
	return i18n.T(locale, key, args...)
}

// GetLocale 返回当前生效的壳语言，由 Wails 绑定给前端（供调试与断言）。
//
// @returns 内置语言 id：zh 或 en。
func (a *App) GetLocale() string {
	a.localeMu.RLock()
	defer a.localeMu.RUnlock()
	return string(a.locale)
}
