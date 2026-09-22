// Package i18n 承载外壳 Go 侧的用户可见文案字典与语言解析。
//
// 为什么单独成包：外壳的语言真源是 iframe 内 harness GUI 的 <html lang>，由前端在
// applyLocale 时经 App.SetLocale 回推；而 Go 侧在**前端加载之前**就要产出用户可见
// 文案（预检页错误、启动失败原因、Wails 原生对话框），因此需要一个不依赖任何内部
// 包的叶子包来承载「当前语言 + 字典」，供 app 绑定层在边界渲染。
//
// 本包不含业务逻辑：领域包只报事实（枚举），文案渲染集中在 internal/app，见
// apps/desktop-launcher/docs/i18n.md 第六节。
package i18n

import (
	"fmt"
	"os"
	"strings"
)

// Locale 是外壳支持的语言。
type Locale string

const (
	// Zh 简体中文：键全集的真源语言。
	Zh Locale = "zh"
	// En 英文。
	En Locale = "en"
	// Fallback 无可用语言信息时使用的语言，与客户端 FALLBACK_LOCALE 保持一致。
	Fallback = En
)

// messages 按语言存放字典。Zh 是键全集真源：En 缺键时回退 Zh，再缺则返回键名
// 本身，让漏配在界面上直接可见，而不是显示空串。
//
// P0 只建立机制，尚未迁入任何文案（见 docs/i18n.md 第七节）；P2 起按区域分批迁入。
var messages = map[Locale]map[string]string{
	Zh: {},
	En: {},
}

// Normalize 把任意语言标签归一化为内置语言；识别不了时返回空串。
//
// 形如 zh_CN.UTF-8、zh-CN、zh-Hans 的标签对壳是同一件事：主语言子标签之外的部分
// （地区、编码、修饰）一律丢弃。无法识别时返回空串而非 Fallback，是为了让调用方
// 区分「识别到语言」与「没有语言信息」——两者的回退策略由调用方决定。
//
// @param raw 形如 zh-CN、zh_CN.UTF-8、en_US 的语言标签。
// @returns 内置语言；无法识别时为空串。
func Normalize(raw string) Locale {
	s := strings.TrimSpace(raw)
	if i := strings.IndexByte(s, '.'); i >= 0 {
		s = s[:i]
	}
	if i := strings.IndexByte(s, '@'); i >= 0 {
		s = s[:i]
	}
	if i := strings.IndexAny(s, "_-"); i >= 0 {
		s = s[:i]
	}
	switch strings.ToLower(s) {
	case string(Zh):
		return Zh
	case string(En):
		return En
	}
	return ""
}

// FromEnv 按 POSIX 优先级（LC_ALL → LC_MESSAGES → LANG）解析启动时的语言。
//
// 用于前端回推之前的初值：外壳的加载页、预检页与启动失败页都出现在 GUI 起来之前。
// 与前端按 navigator 兜底同源（都来自系统语言），因此两处初值通常一致；不一致时
// 以前端回推为准。
//
// 第一个非空变量即生效（POSIX 语义），它识别不了时直接回退 Fallback，不再看后面的
// 变量：C、POSIX 这类「无语言信息」的取值同样落在这里。
//
// @returns 内置语言，必定是 Zh 或 En。
func FromEnv() Locale {
	for _, name := range []string{"LC_ALL", "LC_MESSAGES", "LANG"} {
		raw := os.Getenv(name)
		if raw == "" {
			continue
		}
		if locale := Normalize(raw); locale != "" {
			return locale
		}
		return Fallback
	}
	return Fallback
}

// T 按语言渲染一个键。
//
// 缺键回退链与前端 i18n.js 的 lookup 一致：当前语言 → Zh → 键名本身。args 为空时
// 不经过 fmt，避免文案里出现的 % 被当作格式动词。
//
// 占位符用 Go 的 %s/%d 形式（前端用 {name}）：两侧各自遵循本语言惯例，共享的是键
// 而非占位符写法。
//
// @param locale 目标语言。
// @param key 点分命名空间的键。
// @param args 占位符取值。
// @returns 渲染后的文案；缺键时为 key 本身。
func T(locale Locale, key string, args ...any) string {
	text, ok := messages[locale][key]
	if !ok {
		text, ok = messages[Zh][key]
	}
	if !ok {
		return key
	}
	if len(args) == 0 {
		return text
	}
	return fmt.Sprintf(text, args...)
}
