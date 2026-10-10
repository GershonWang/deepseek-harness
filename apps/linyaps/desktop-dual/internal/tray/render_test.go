package tray

import (
	"strings"
	"testing"

	"github.com/godbus/dbus/v5"

	"github.com/deepseek-ai/deepseek-harness/apps/linyaps/desktop-dual/internal/about"
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

// TestMenuEntriesReflectState 覆盖菜单结构：条目顺序、分隔线位置，以及收进「设置」子菜单
// 的「默认启动」两项用单选标记表达当前默认形态、且两项都保持可点。
func TestMenuEntriesReflectState(t *testing.T) {
	entries := menuEntries(trayState{locale: i18n.Zh, mode: switchboard.ModeShell, defaultMode: switchboard.ModeShell})
	if len(entries) != 6 {
		t.Fatalf("菜单项数 = %d，期望 6", len(entries))
	}
	wantLabels := []string{"切换到官方版", "", "设置", "", "关于", "退出切换器"}
	for i, want := range wantLabels {
		if entries[i].Separator {
			continue
		}
		if entries[i].Label != want {
			t.Errorf("第 %d 项标签 = %q，期望 %q", i, entries[i].Label, want)
		}
	}
	if !entries[1].Separator || !entries[3].Separator {
		t.Error("分隔线位置不符")
	}
	if entries[0].Children != nil {
		t.Error("「切换到」应是可点项，不是子菜单")
	}
	settings := entries[2]
	if len(settings.Children) != 2 {
		t.Fatalf("「设置」子项数 = %d，期望 2", len(settings.Children))
	}
	if settings.Children[0].Label != "默认启动薄壳版" || settings.Children[1].Label != "默认启动官方版" {
		t.Errorf("「设置」子项标签 = %q/%q", settings.Children[0].Label, settings.Children[1].Label)
	}
	// 两项都可点：当前默认由单选标记表达，不用置灰——置灰看起来像功能不可用。
	for i, child := range settings.Children {
		if !child.Enabled {
			t.Errorf("「设置」第 %d 项应保持可点（状态改用单选标记表达）", i)
		}
	}
	if settings.Children[0].Toggle != toggleOn || settings.Children[1].Toggle != toggleOff {
		t.Errorf("默认是薄壳版时勾选态 = %v/%v，期望 on/off",
			settings.Children[0].Toggle, settings.Children[1].Toggle)
	}
}

// TestMenuEntriesFollowRunningMode 覆盖「切换到」的文案随当前形态翻转，以及单选标记
// 跟着默认形态走。
func TestMenuEntriesFollowRunningMode(t *testing.T) {
	entries := menuEntries(trayState{locale: i18n.Zh, mode: switchboard.ModeOfficial, defaultMode: switchboard.ModeOfficial})
	if entries[0].Label != "切换到薄壳版" {
		t.Fatalf("当前跑官方版时首项 = %q，期望「切换到薄壳版」", entries[0].Label)
	}
	if entries[2].Children[0].Toggle != toggleOff {
		t.Error("默认形态是官方版时「默认启动薄壳版」应未勾选")
	}
	if entries[2].Children[1].Toggle != toggleOn {
		t.Error("默认形态是官方版时「默认启动官方版」应勾选")
	}
}

// TestSameEntriesSeesToggleChange 覆盖勾选态参与变更比较：宿主只在收到 LayoutUpdated 时
// 重取布局，漏掉勾选态就会出现「配置改了、菜单里的圆点没动」。
func TestSameEntriesSeesToggleChange(t *testing.T) {
	shell := menuEntries(trayState{locale: i18n.Zh, defaultMode: switchboard.ModeShell})
	official := menuEntries(trayState{locale: i18n.Zh, defaultMode: switchboard.ModeOfficial})
	if sameEntries(shell, official) {
		t.Fatal("只有默认形态不同时，菜单内容应判定为已变化")
	}
	if !sameEntries(shell, menuEntries(trayState{locale: i18n.Zh, defaultMode: switchboard.ModeShell})) {
		t.Fatal("状态相同时应判定为未变化")
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

// TestMenuLayoutServesSubmenu 覆盖子菜单的 dbusmenu 协议面：父项标成 submenu，宿主用
// 父项 ID 再取一次布局时返回它的子项，子项属性也要能按 ID 查到（否则子菜单显示为空白）。
func TestMenuLayoutServesSubmenu(t *testing.T) {
	m := &menuObject{entries: menuEntries(trayState{locale: i18n.Zh, defaultMode: switchboard.ModeShell})}

	_, root, dbusErr := m.GetLayout(0, -1, nil)
	if dbusErr != nil {
		t.Fatalf("取根布局失败: %v", dbusErr)
	}
	if len(root.Children) != 6 {
		t.Fatalf("根层子项数 = %d，期望 6", len(root.Children))
	}
	settings := childLayout(t, root.Children[2])
	if got := settings.Props["children-display"].Value(); got != "submenu" {
		t.Fatalf("「设置」的 children-display = %v，期望 submenu", got)
	}

	_, sub, dbusErr := m.GetLayout(idSettings, -1, nil)
	if dbusErr != nil {
		t.Fatalf("取子菜单布局失败: %v", dbusErr)
	}
	if len(sub.Children) != 2 {
		t.Fatalf("子菜单项数 = %d，期望 2", len(sub.Children))
	}
	shellItem := childLayout(t, sub.Children[0])
	if label := shellItem.Props["label"].Value(); label != "默认启动薄壳版" {
		t.Fatalf("子菜单首项标签 = %v", label)
	}
	// 单选标记走 dbusmenu 协议面：toggle-type 决定样式，toggle-state 决定勾没勾上。
	if got := shellItem.Props["toggle-type"].Value(); got != "radio" {
		t.Fatalf("子菜单首项 toggle-type = %v，期望 radio", got)
	}
	if got := shellItem.Props["toggle-state"].Value(); got != int32(1) {
		t.Fatalf("当前默认项的 toggle-state = %v，期望 1", got)
	}
	officialItem := childLayout(t, sub.Children[1])
	if got := officialItem.Props["toggle-state"].Value(); got != int32(0) {
		t.Fatalf("非默认项的 toggle-state = %v，期望 0", got)
	}

	props, dbusErr := m.GetGroupProperties([]int32{idDefaultOfficial}, nil)
	if dbusErr != nil {
		t.Fatalf("按 ID 查子项属性失败: %v", dbusErr)
	}
	if len(props) != 1 || props[0].ID != idDefaultOfficial {
		t.Fatalf("子项属性查询结果 = %+v，期望只含 idDefaultOfficial", props)
	}

	_, unknown, dbusErr := m.GetLayout(4242, -1, nil)
	if dbusErr != nil {
		t.Fatalf("未知父项应返回空布局而不是错误: %v", dbusErr)
	}
	if len(unknown.Children) != 0 {
		t.Fatalf("未知父项的子项数 = %d，期望 0", len(unknown.Children))
	}
}

// childLayout 把 dbusmenu 布局里的一个子项还原成结构体。
// @param t - 测试上下文。
// @param variant - 布局里的子项。
// @returns 还原出的布局。
func childLayout(t *testing.T, variant dbus.Variant) dbusmenuLayout {
	t.Helper()
	var layout dbusmenuLayout
	if err := variant.Store(&layout); err != nil {
		t.Fatalf("解析子项布局失败: %v", err)
	}
	return layout
}

// TestAboutCommentsCarryModeAndPackage 覆盖「关于」里的本地化信息：当前形态随语言与
// 运行状态变化，包 ID 固定——用户报问题时这两项决定了排查从哪里开始。
func TestAboutCommentsCarryModeAndPackage(t *testing.T) {
	zh := aboutComments(i18n.Zh, switchboard.ModeOfficial)
	if !strings.Contains(zh, "官方版") || !strings.Contains(zh, about.PackageID) {
		t.Fatalf("中文关于信息 = %q，应含形态名与包 ID", zh)
	}
	en := aboutComments(i18n.En, "")
	if !strings.Contains(en, "No client running") || !strings.Contains(en, about.PackageID) {
		t.Fatalf("英文关于信息 = %q，应含未运行态与包 ID", en)
	}
}
