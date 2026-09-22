package app

import (
	"testing"

	"github.com/deepseek-ai/deepseek-harness/apps/desktop-launcher/internal/i18n"
)

// TestLocaleRoundTrip 覆盖前端回推的完整链路：归一化、存储、读回。
func TestLocaleRoundTrip(t *testing.T) {
	tests := []struct {
		name string
		id   string
		want string
	}{
		{"带地区与编码的中文", "zh_CN.UTF-8", "zh"},
		{"文档语言标签", "zh-CN", "zh"},
		{"多段子标签的中文", "zh-Hant-TW", "zh"},
		{"英文", "en", "en"},
		{"大小写不敏感", "EN", "en"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := &App{locale: i18n.Fallback}
			a.SetLocale(tt.id)
			if got := a.GetLocale(); got != tt.want {
				t.Fatalf("SetLocale(%q) 后 GetLocale() = %q, 期望 %q", tt.id, got, tt.want)
			}
		})
	}
}

// TestSetLocaleIgnoresUnknown 覆盖未知语言：真源在 GUI 侧，收到识别不了的取值时
// 保持原值，而不是猜一个内置语言——猜错会让壳的文案与用户在 GUI 里所见不符。
func TestSetLocaleIgnoresUnknown(t *testing.T) {
	for _, id := range []string{"ja_JP.UTF-8", "C", "", "POSIX"} {
		t.Run(id, func(t *testing.T) {
			a := &App{locale: i18n.Zh}
			a.SetLocale(id)
			if got := a.GetLocale(); got != "zh" {
				t.Fatalf("SetLocale(%q) 不应改变语言，GetLocale() = %q, 期望保持 zh", id, got)
			}
		})
	}
}

// TestRenderedCopyFollowsLocale 钉住「Go 侧文案跟着语言走」依赖的机制：渲染读的是
// 调用那一刻的语言，而不是启动时的快照。
//
// 这是 SetLocale 里重推快照之所以有效的原因——旧快照不会自己变，但重新渲染一次就会
// 用上新语言。没有 Wails ctx 时测不了事件本身，因此测机制。
func TestRenderedCopyFollowsLocale(t *testing.T) {
	a := testApp()
	a.SetLocale("zh")
	if got := joinOrNone(a.t, nil); got != "无" {
		t.Fatalf("中文下 joinOrNone = %q，期望 无", got)
	}
	a.SetLocale("en")
	if got := joinOrNone(a.t, nil); got != "None" {
		t.Fatalf("切到英文后 joinOrNone = %q，期望 None", got)
	}
	if got := updateNotice(a.t, []string{"go 1.25 → 1.26"}, 1); got != "Updated 1 tool(s): go 1.25 → 1.26; 1 failed. Older versions stay on disk; switch or uninstall them from the version dropdown on the card" {
		t.Fatalf("切到英文后 updateNotice = %q", got)
	}
}
