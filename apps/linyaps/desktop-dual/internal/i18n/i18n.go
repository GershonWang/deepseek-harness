// Package i18n 提供托盘进程面向用户的文案与语言解析。
//
// 为什么在 dual 模块内自带一份而不复用 launcher 的 internal/i18n：Go 的 internal
// 可见性以模块为界，跨模块 import 不成立，而托盘只有不到十条文案，为此再抽一个共享
// 模块会引入第三个 go.mod 与版本协调成本。这里的取舍是「机制小范围重复、字典内容各管
// 各的」：语言解析沿用 POSIX 优先级（与 launcher 同一套规则，避免同一台机器上两个进程
// 显示出不同语言），键则只列托盘真正用到的。
package i18n

import (
	"os"
	"strings"
)

// Locale 是受支持的语言。
type Locale string

const (
	// Zh 是简体中文。
	Zh Locale = "zh"
	// En 是英文，也是任何识别不出来时的回退。
	En Locale = "en"
)

// FromEnv 按 POSIX 优先级解析语言：LC_ALL → LC_MESSAGES → LANG。
//
// 为什么不是直接读 LANG：用户常把 LANG 固定在某个语言，却在单个应用上通过 LC_ALL
// 覆盖；反过来只看 LC_ALL 会在未设置时给出默认值而不是用户真正的语言。
// @returns 解析出的语言，全部未设置时回退英文。
func FromEnv() Locale {
	for _, name := range []string{"LC_ALL", "LC_MESSAGES", "LANG"} {
		if locale := Normalize(os.Getenv(name)); locale != "" {
			return locale
		}
	}
	return En
}

// Normalize 把 POSIX locale 字符串收敛到受支持的语言。
// @param raw - 形如 `zh_CN.UTF-8`、`en_US`、`C` 的取值。
// @returns 匹配到的语言；无法识别时返回空串，由调用方决定回退。
func Normalize(raw string) Locale {
	value := strings.ToLower(strings.TrimSpace(raw))
	if value == "" {
		return ""
	}
	// 去掉修饰：zh_CN.UTF-8 → zh_cn，zh-Hans → zh-hans。
	if idx := strings.IndexAny(value, ".@"); idx >= 0 {
		value = value[:idx]
	}
	value = strings.ReplaceAll(value, "-", "_")
	if value == "c" || value == "posix" {
		return En
	}
	switch {
	case strings.HasPrefix(value, "zh"):
		return Zh
	case strings.HasPrefix(value, "en"):
		return En
	default:
		// 其他语言目前没有字典：显示英文比显示空白或中文更接近用户预期。
		return En
	}
}

// T 取一条文案。
// @param locale - 目标语言。
// @param key - 字典键。
// @returns 该语言下的文案；键不存在时回退英文，再回退键本身。
func T(locale Locale, key string) string {
	if text, ok := messages[locale][key]; ok {
		return text
	}
	if text, ok := messages[En][key]; ok {
		return text
	}
	return key
}
