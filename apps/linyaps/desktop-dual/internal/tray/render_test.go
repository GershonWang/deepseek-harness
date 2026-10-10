package tray

import (
	"testing"

	"github.com/deepseek-ai/deepseek-harness/apps/linyaps/desktop-dual/internal/i18n"
	"github.com/deepseek-ai/deepseek-harness/apps/linyaps/desktop-dual/internal/switchboard"
)

// TestRenderNamesCurrentMode 覆盖提示文字：托盘没有球下方的文字标记，形态名只能出现在
// 提示与菜单里，因此这里的本地化名字是用户判断「现在跑的是哪个客户端」的唯一来源。
func TestRenderNamesCurrentMode(t *testing.T) {
	cases := []struct {
		locale i18n.Locale
		mode   switchboard.Mode
		want   string
	}{
		{i18n.Zh, switchboard.ModeShell, "薄壳版"},
		{i18n.Zh, switchboard.ModeOfficial, "官方版"},
		{i18n.Zh, "", "客户端未运行"},
		{i18n.En, switchboard.ModeShell, "Thin shell"},
		{i18n.En, "", "No client running"},
	}
	for _, tc := range cases {
		got := trayState{locale: tc.locale, mode: tc.mode}.render("").tooltip.Text
		if got != tc.want {
			t.Errorf("render(%s, %q).tooltip.Text = %q，期望 %q", tc.locale, tc.mode, got, tc.want)
		}
	}
}

// TestRenderMarksAttentionOnError 覆盖失败可见性：托盘形态没有窗口贴错误提示，
// 出错时必须用 SNI 的 NeedsAttention 让宿主标记图标，否则用户只会觉得「点了没反应」。
func TestRenderMarksAttentionOnError(t *testing.T) {
	ok := trayState{locale: i18n.Zh, mode: switchboard.ModeShell}.render("")
	if ok.status != "Active" {
		t.Fatalf("正常状态应为 Active，得到 %q", ok.status)
	}
	bad := trayState{locale: i18n.Zh, mode: switchboard.ModeShell, err: "boom"}.render("")
	if bad.status != "NeedsAttention" {
		t.Fatalf("出错状态应为 NeedsAttention，得到 %q", bad.status)
	}
}

// TestRenderAppendsEllipsisWhileSwitching 覆盖切换中的可见反馈：切换要先停后起，
// 期间可能持续数秒，提示里必须能看出正在进行。
func TestRenderAppendsEllipsisWhileSwitching(t *testing.T) {
	text := trayState{locale: i18n.Zh, mode: switchboard.ModeShell, switching: true}.render("").tooltip.Text
	if text != "薄壳版…" {
		t.Fatalf("切换中的提示 = %q，期望带省略号", text)
	}
}

// TestRenderAlwaysCarriesPixmaps 覆盖图标兜底：图标缺失时也不能上报空位图，
// 否则宿主可能整项都不显示，入口随之消失。
func TestRenderAlwaysCarriesPixmaps(t *testing.T) {
	got := trayState{locale: i18n.Zh, mode: switchboard.ModeShell}.render("")
	if len(got.pixmaps) != len(iconSizes) {
		t.Fatalf("位图数量 = %d，期望 %d", len(got.pixmaps), len(iconSizes))
	}
	for _, pixmap := range got.pixmaps {
		if len(pixmap.Data) != int(pixmap.Width*pixmap.Height)*4 {
			t.Fatalf("%dx%d 的位图数据长度不符: %d", pixmap.Width, pixmap.Height, len(pixmap.Data))
		}
	}
}

// TestNextModeIsAlwaysTheOther 覆盖切换目标：有客户端在跑时切到另一个形态；
// 没有客户端时切到默认形态之外的另一个——两种情况下用户拿到的都是「另一个」。
func TestNextModeIsAlwaysTheOther(t *testing.T) {
	cases := []struct {
		state trayState
		want  switchboard.Mode
	}{
		{trayState{mode: switchboard.ModeShell}, switchboard.ModeOfficial},
		{trayState{mode: switchboard.ModeOfficial}, switchboard.ModeShell},
		{trayState{defaultMode: switchboard.ModeShell}, switchboard.ModeOfficial},
		{trayState{defaultMode: switchboard.ModeOfficial}, switchboard.ModeShell},
	}
	for _, tc := range cases {
		if got := tc.state.nextMode(); got != tc.want {
			t.Errorf("nextMode(%+v) = %q，期望 %q", tc.state, got, tc.want)
		}
	}
}

// TestMenuEntriesReflectState 覆盖菜单内容：条目顺序、分隔线位置，以及「默认启动」
// 两项在已是默认形态时置灰——托盘菜单没有勾选框，置灰是表达当前默认形态的手段。
func TestMenuEntriesReflectState(t *testing.T) {
	entries := menuEntries(trayState{locale: i18n.Zh, mode: switchboard.ModeShell, defaultMode: switchboard.ModeShell})
	if len(entries) != 6 {
		t.Fatalf("菜单项数 = %d，期望 6", len(entries))
	}
	wantLabels := []string{"切换到官方版", "", "默认启动薄壳版", "默认启动官方版", "", "退出切换器"}
	for i, want := range wantLabels {
		if entries[i].Separator {
			continue
		}
		if entries[i].Label != want {
			t.Errorf("第 %d 项标签 = %q，期望 %q", i, entries[i].Label, want)
		}
	}
	if !entries[1].Separator || !entries[4].Separator {
		t.Error("分隔线位置不符")
	}
	if entries[2].Enabled {
		t.Error("已是默认形态的「默认启动薄壳版」应置灰")
	}
	if !entries[3].Enabled {
		t.Error("非默认形态的「默认启动官方版」应可点")
	}
}

// TestMenuEntriesFollowRunningMode 覆盖「切换到」的文案随当前形态翻转。
func TestMenuEntriesFollowRunningMode(t *testing.T) {
	entries := menuEntries(trayState{locale: i18n.Zh, mode: switchboard.ModeOfficial, defaultMode: switchboard.ModeOfficial})
	if entries[0].Label != "切换到薄壳版" {
		t.Fatalf("当前跑官方版时首项 = %q，期望「切换到薄壳版」", entries[0].Label)
	}
}

// TestMenuEntriesDisableSwitchWhileSwitching 覆盖防重复触发：切换期间禁用首项，
// 避免排队出第二次切换（切换会中断会话，误触代价高）。
func TestMenuEntriesDisableSwitchWhileSwitching(t *testing.T) {
	entries := menuEntries(trayState{locale: i18n.Zh, switching: true})
	if entries[0].Enabled {
		t.Fatal("切换进行中首项应置灰")
	}
}
