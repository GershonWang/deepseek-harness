package i18n

import "testing"

// withTestMessages 临时替换字典：P0 阶段真实字典为空（文案自 P2 起迁入），回退链与
// 占位符替换只能靠注入的键验证。用例串行执行（不使用 t.Parallel），退出时恢复原表。
func withTestMessages(t *testing.T, dict map[Locale]map[string]string) {
	t.Helper()
	original := messages
	messages = dict
	t.Cleanup(func() { messages = original })
}

func TestNormalize(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want Locale
	}{
		{"纯语言", "zh", Zh},
		{"连字符地区", "zh-CN", Zh},
		{"下划线加编码", "zh_CN.UTF-8", Zh},
		{"文字子标签", "zh-Hans", Zh},
		{"大小写与空白", "  ZH  ", Zh},
		{"英文", "en", En},
		{"英文地区", "en_US.UTF-8", En},
		{"未注册语言", "ja_JP.UTF-8", ""},
		{"无语言信息", "C", ""},
		{"POSIX 占位", "POSIX", ""},
		{"空串", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Normalize(tt.raw); got != tt.want {
				t.Fatalf("Normalize(%q) = %q, 期望 %q", tt.raw, got, tt.want)
			}
		})
	}
}

func TestFromEnv(t *testing.T) {
	tests := []struct {
		name  string
		lcAll string
		lcMsg string
		lang  string
		want  Locale
	}{
		{"LC_ALL 优先", "en_US.UTF-8", "zh_CN.UTF-8", "zh_CN.UTF-8", En},
		{"LC_ALL 为空时看 LC_MESSAGES", "", "zh_CN.UTF-8", "en_US.UTF-8", Zh},
		{"前两者为空时看 LANG", "", "", "zh_CN.UTF-8", Zh},
		{"全部为空", "", "", "", Fallback},
		{"首个非空变量识别不了即回退", "ja_JP.UTF-8", "", "zh_CN.UTF-8", Fallback},
		{"C 回退", "C", "", "", Fallback},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("LC_ALL", tt.lcAll)
			t.Setenv("LC_MESSAGES", tt.lcMsg)
			t.Setenv("LANG", tt.lang)
			if got := FromEnv(); got != tt.want {
				t.Fatalf("FromEnv() = %q, 期望 %q", got, tt.want)
			}
		})
	}
}

func TestT(t *testing.T) {
	withTestMessages(t, map[Locale]map[string]string{
		Zh: {
			"demo.greet":   "你好 %s",
			"demo.count":   "共 %d 项",
			"demo.percent": "100% 完成",
			"demo.onlyzh":  "仅中文",
		},
		En: {
			"demo.greet":   "hello %s",
			"demo.count":   "total %d",
			"demo.percent": "100% complete",
		},
	})
	tests := []struct {
		name   string
		locale Locale
		key    string
		args   []any
		want   string
	}{
		{"英文取本语言", En, "demo.greet", []any{"world"}, "hello world"},
		{"中文取本语言", Zh, "demo.greet", []any{"世界"}, "你好 世界"},
		{"整数占位符", En, "demo.count", []any{3}, "total 3"},
		{"英文缺键回退中文", En, "demo.onlyzh", nil, "仅中文"},
		{"缺键返回键名", En, "demo.missing", nil, "demo.missing"},
		{"缺键不渲染参数", En, "demo.missing", []any{1}, "demo.missing"},
		{"无参数时不解释百分号", Zh, "demo.percent", nil, "100% 完成"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := T(tt.locale, tt.key, tt.args...); got != tt.want {
				t.Fatalf("T(%q, %q) = %q, 期望 %q", tt.locale, tt.key, got, tt.want)
			}
		})
	}
}
