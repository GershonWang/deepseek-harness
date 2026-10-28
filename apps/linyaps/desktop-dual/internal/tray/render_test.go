package tray

import (
	"os"
	"path/filepath"
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

// TestRenderTooltipTitleIsProductName 覆盖提示的标题行：标题只写产品名，当前形态由
// 正文承载。两种语言都断言，避免中文去掉后缀、英文仍留着「switcher」。
func TestRenderTooltipTitleIsProductName(t *testing.T) {
	for _, locale := range []i18n.Locale{i18n.Zh, i18n.En} {
		got := trayState{locale: locale, mode: switchboard.ModeShell}.render("").tooltip.Title
		if got != "DeepSeek Harness" {
			t.Errorf("render(%s, shell).tooltip.Title = %q，期望产品名", locale, got)
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
	entries := menuEntries(trayState{locale: i18n.Zh, mode: switchboard.ModeShell, defaultMode: switchboard.ModeShell, switchable: true})
	if len(entries) != 6 {
		t.Fatalf("菜单项数 = %d，期望 6", len(entries))
	}
	wantLabels := []string{"切换到官方版", "", "设置", "", "关于", "退出"}
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
	entries := menuEntries(trayState{locale: i18n.Zh, mode: switchboard.ModeOfficial, defaultMode: switchboard.ModeOfficial, switchable: true})
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

// TestMenuEntriesCollapseToSingleClient 覆盖只随带薄壳客户端的包：包里没有第二种形态，
// 切换没有目标、默认形态也没有第二个可选，菜单必须收敛为「显示 / 关于 / 退出」。
// 留着切换项会是一个点了必然失败的入口——clientCommand 以「可执行文件不存在」拒绝，
// 用户看到的是托盘通知而不是一个能用的功能。
func TestMenuEntriesCollapseToSingleClient(t *testing.T) {
	entries := menuEntries(trayState{locale: i18n.Zh, mode: switchboard.ModeShell})
	if len(entries) != 4 {
		t.Fatalf("单客户端菜单项数 = %d，期望 4", len(entries))
	}
	if entries[0].ID != idShow || entries[0].Label != "显示" {
		t.Fatalf("首项 = %+v，期望「显示」", entries[0])
	}
	// 「显示」必须可点：SNI 宿主缺失或未被宿主接管时左键到不了 Activate，
	// 菜单里这一项是该形态下唯一的客户端入口。
	if !entries[0].Enabled {
		t.Error("「显示」应保持可点")
	}
	if !entries[1].Separator {
		t.Error("第 2 项应为分隔线")
	}
	if entries[2].Label != "关于" || entries[3].Label != "退出" {
		t.Fatalf("后两项 = %q/%q，期望「关于」「退出」", entries[2].Label, entries[3].Label)
	}
	for _, entry := range entries {
		if entry.ID == idSwitch || entry.ID == idSettings || len(entry.Children) > 0 {
			t.Fatalf("单客户端菜单不应出现切换或默认形态入口: %+v", entry)
		}
	}
	// 文案两个语言都要有：缺键时 T 回落到键名本身，用户会看到 "tray.show"。
	if got := menuEntries(trayState{locale: i18n.En})[0].Label; got != "Show" {
		t.Fatalf("英文首项 = %q，期望 Show", got)
	}
}

// TestDiffEntriesReportsPropertyChange 覆盖通知种类的选择：只有勾选态变化时发
// ItemsPropertiesUpdated（宿主原地更新那两项），形状变化才发 LayoutUpdated（宿主重建整树）。
// 分错种类会出两种事故：发整树会重建用户正在交互的弹窗（实测能把 DDE 的托盘弹窗打进
// 坏状态），漏发则出现「配置改了、菜单里的圆点没动」。
func TestDiffEntriesReportsPropertyChange(t *testing.T) {
	shell := menuEntries(trayState{locale: i18n.Zh, defaultMode: switchboard.ModeShell, switchable: true})
	official := menuEntries(trayState{locale: i18n.Zh, defaultMode: switchboard.ModeOfficial, switchable: true})

	change := diffEntries(shell, official)
	if change.StructureChanged {
		t.Fatal("只有默认形态变化时不应判定为形状变化（那会让宿主重建弹窗）")
	}
	// 三项：切换项的文案跟着翻转，两个单选项的勾选态对调。
	if len(change.Updated) != 3 {
		t.Fatalf("应报告三项变化，得到 %d 项", len(change.Updated))
	}
	states := make(map[int32]int32, 2)
	for _, item := range change.Updated {
		if item.ID == idSwitch {
			if label, _ := item.Props["label"].Value().(string); label != "切换到薄壳版" {
				t.Fatalf("切换项文案 = %q，期望「切换到薄壳版」", label)
			}
			continue
		}
		state, ok := item.Props["toggle-state"].Value().(int32)
		if !ok {
			t.Fatalf("toggle-state 类型不是 int32: %v", item.Props["toggle-state"].Value())
		}
		states[item.ID] = state
	}
	if states[idDefaultShell] != 0 || states[idDefaultOfficial] != 1 {
		t.Fatalf("报告的状态 = %v，期望薄壳版 0、官方版 1", states)
	}

	if same := diffEntries(shell, shell); same.StructureChanged || len(same.Updated) != 0 {
		t.Fatalf("内容相同时不应产生任何通知: %+v", same)
	}
}

// TestDiffEntriesSeesStructureChange 覆盖形状变化：项数或层级变了必须让宿主重取整树，
// 否则宿主会继续用旧的那棵树。
func TestDiffEntriesSeesStructureChange(t *testing.T) {
	full := menuEntries(trayState{locale: i18n.Zh, switchable: true})
	if change := diffEntries(full, full[:2]); !change.StructureChanged {
		t.Fatal("项数变化应判定为形状变化")
	}
	if change := diffEntries(full, full); change.StructureChanged {
		t.Fatal("形状未变时不应判定为形状变化")
	}
}

// TestMenuEntriesDisableSwitchWhileSwitching 覆盖防重复触发：切换期间禁用首项，
// 避免排队出第二次切换（切换会中断会话，误触代价高）。
func TestMenuEntriesDisableSwitchWhileSwitching(t *testing.T) {
	entries := menuEntries(trayState{locale: i18n.Zh, switching: true, switchable: true})
	if entries[0].Enabled {
		t.Fatal("切换进行中首项应置灰")
	}
}

// TestMenuLayoutServesSubmenu 覆盖子菜单的 dbusmenu 协议面：父项标成 submenu，宿主用
// 父项 ID 再取一次布局时返回它的子项，子项属性也要能按 ID 查到（否则子菜单显示为空白）。
func TestMenuLayoutServesSubmenu(t *testing.T) {
	m := &menuObject{entries: menuEntries(trayState{locale: i18n.Zh, defaultMode: switchboard.ModeShell, switchable: true})}

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

// TestAboutDialogCarriesFacts 覆盖「关于」的内容与分组：窗口标题是本地化的「关于」；当前
// 形态与包 ID 是分隔线上方的居中行，随语言与运行状态变化，其中包 ID 是唯一带一键复制标记
// 的值（形态名不需要抄给别人）；DSH 版本、两个仓库与封装作者是分隔线下方的事实行，两个仓库
// 地址必须是 http(s)——助手只按这个前缀决定渲染成可点击链接还是纯文本。两组的顺序也要钉住，
// 它决定用户看到的排列。
func TestAboutDialogCarriesFacts(t *testing.T) {
	prefix := t.TempDir()
	harnessDir := filepath.Join(prefix, "harness")
	if err := os.MkdirAll(harnessDir, 0o755); err != nil {
		t.Fatalf("准备目录失败: %v", err)
	}
	if err := os.WriteFile(filepath.Join(harnessDir, "package.json"),
		[]byte(`{"name":"dsh","version":"0.2.0-rc.2"}`), 0o644); err != nil {
		t.Fatalf("准备 package.json 失败: %v", err)
	}

	zh := aboutDialog(i18n.Zh, switchboard.ModeOfficial, prefix)
	if zh.Title != "关于" {
		t.Errorf("窗口标题 = %q，期望 关于", zh.Title)
	}
	wantCenter := []string{"当前形态", "包 ID"}
	if len(zh.CenterFields) != len(wantCenter) {
		t.Fatalf("居中行数 = %d，期望 %d", len(zh.CenterFields), len(wantCenter))
	}
	for i, want := range wantCenter {
		if zh.CenterFields[i].Label != want {
			t.Errorf("居中第 %d 行标签 = %q，期望 %q", i, zh.CenterFields[i].Label, want)
		}
	}
	if !strings.Contains(zh.CenterFields[0].Value, "官方版") {
		t.Errorf("居中形态 = %q，应含当前形态名", zh.CenterFields[0].Value)
	}
	if zh.CenterFields[1].Value != about.PackageID {
		t.Errorf("居中包 ID = %q，期望 %q", zh.CenterFields[1].Value, about.PackageID)
	}
	if !zh.CenterFields[1].Copy {
		t.Error("包 ID 没有复制标记：值不再可选中，没有复制入口就只能手抄")
	}
	if zh.CenterFields[0].Copy {
		t.Error("形态名不该带复制按钮：它不是需要原样转述的标识")
	}
	if zh.CopyLabel == "" || zh.CopiedLabel == "" {
		t.Errorf("复制按钮文案 = %q / %q，都不能为空", zh.CopyLabel, zh.CopiedLabel)
	}
	if zh.OpenFailedLabel == "" {
		t.Error("打开失败提示的前缀为空：链接打不开时用户只会看到一句技术原因")
	}

	wantLabels := []string{"DSH版本", "DSH仓库", "玲珑封装作者", "玲珑封装仓库"}
	if len(zh.Fields) != len(wantLabels) {
		t.Fatalf("事实行数 = %d，期望 %d", len(zh.Fields), len(wantLabels))
	}
	for i, want := range wantLabels {
		if zh.Fields[i].Label != want {
			t.Errorf("事实第 %d 行标签 = %q，期望 %q", i, zh.Fields[i].Label, want)
		}
	}
	if zh.Fields[0].Value != "0.2.0-rc.2" {
		t.Errorf("DSH 版本 = %q，期望 0.2.0-rc.2", zh.Fields[0].Value)
	}
	if zh.Fields[1].Value != about.UpstreamRepo || zh.Fields[3].Value != about.GithubRepo {
		t.Errorf("仓库地址 = %q / %q", zh.Fields[1].Value, zh.Fields[3].Value)
	}
	if zh.Fields[2].Value != about.Packager {
		t.Errorf("封装作者 = %q，期望 %q", zh.Fields[2].Value, about.Packager)
	}
	// fork 与上游是两个不同的责任方，填成同一个地址会让用户把上游问题报到这里。
	if about.GithubRepo == about.UpstreamRepo {
		t.Error("两个仓库地址相同，必须指向不同仓库")
	}

	en := aboutDialog(i18n.En, "", prefix)
	if en.Title != "About" {
		t.Errorf("英文窗口标题 = %q，期望 About", en.Title)
	}
	if en.Fields[1].Label != "DSH repository" {
		t.Errorf("英文 DSH 仓库标签 = %q", en.Fields[1].Label)
	}
	if en.CopyLabel != "Copy" || en.CopiedLabel != "Copied" {
		t.Errorf("英文复制文案 = %q / %q，期望 Copy / Copied", en.CopyLabel, en.CopiedLabel)
	}
	if !en.CenterFields[1].Copy {
		t.Error("英文包 ID 没有复制标记")
	}
	if !strings.Contains(en.CenterFields[0].Value, "No client running") {
		t.Errorf("英文形态 = %q，应含未运行态", en.CenterFields[0].Value)
	}
}
