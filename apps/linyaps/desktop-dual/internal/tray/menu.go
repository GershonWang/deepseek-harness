package tray

import (
	"sync"

	"github.com/godbus/dbus/v5"
	"github.com/godbus/dbus/v5/prop"

	"github.com/deepseek-ai/deepseek-harness/apps/linyaps/desktop-dual/internal/about"
	"github.com/deepseek-ai/deepseek-harness/apps/linyaps/desktop-dual/internal/i18n"
	"github.com/deepseek-ai/deepseek-harness/apps/linyaps/desktop-dual/internal/switchboard"
)

// 菜单项 ID。dbusmenu 用整数标识项，宿主回调时只给 ID，因此这组常量是菜单与动作之间
// 唯一约定。
const (
	idSwitch          int32 = 1
	idSeparatorTop    int32 = 2
	idSettings        int32 = 3
	idDefaultShell    int32 = 4
	idDefaultOfficial int32 = 5
	idSeparatorBottom int32 = 6
	idAbout           int32 = 7
	idQuit            int32 = 8
)

// toggle 是菜单项的勾选形态。零值表示该项不是单选项，宿主不会画标记。
type toggle int

const (
	// toggleNone 表示该项没有勾选标记。
	toggleNone toggle = iota
	// toggleOff 表示该项是未选中的单选项。
	toggleOff
	// toggleOn 表示该项是已选中的单选项。
	toggleOn
)

// menuEntry 是菜单中的一项（与 D-Bus 无关的纯数据）。Children 非空表示这一项是子菜单
// 入口：它自身不带动作，宿主展开后显示子项。
type menuEntry struct {
	ID        int32
	Label     string
	Enabled   bool
	Separator bool
	Toggle    toggle
	Children  []menuEntry
}

// menuEntries 按当前状态生成菜单。
//
// 结构固定为「切换 / 设置▸ / 关于 / 退出」：切换是破坏性动作，保留在首项但必须显式点；
// 默认形态属于设置而不是常用动作，收进设置子菜单；关于与退出各自独立成项。
//
// 「默认启动」两项是单选项（dbusmenu 的 toggle-type=radio）：当前默认那一项带勾选标记、
// 另一项空白，两项都保持可点。为什么不用「当前项置灰」：置灰看起来像功能不可用，而它
// 表达的只是一个状态；单选标记既说清状态，又不牺牲可点性。
// @param state - 当前状态。
// @returns 菜单项，顺序即显示顺序。
func menuEntries(state trayState) []menuEntry {
	return []menuEntry{
		{ID: idSwitch, Label: i18n.T(state.locale, switchLabelKey(state.nextMode())), Enabled: !state.switching},
		{ID: idSeparatorTop, Separator: true},
		{ID: idSettings, Label: i18n.T(state.locale, "tray.settings"), Enabled: true, Children: []menuEntry{
			{ID: idDefaultShell, Label: i18n.T(state.locale, "tray.defaultShell"),
				Enabled: true, Toggle: toggleFor(state.defaultMode, switchboard.ModeShell)},
			{ID: idDefaultOfficial, Label: i18n.T(state.locale, "tray.defaultOfficial"),
				Enabled: true, Toggle: toggleFor(state.defaultMode, switchboard.ModeOfficial)},
		}},
		{ID: idSeparatorBottom, Separator: true},
		{ID: idAbout, Label: i18n.T(state.locale, "tray.about"), Enabled: true},
		{ID: idQuit, Label: i18n.T(state.locale, "tray.quit"), Enabled: true},
	}
}

// toggleFor 给出某一形态在「默认启动」里的勾选态。
// @param defaultMode - 当前默认形态。
// @param mode - 本项代表的形态。
// @returns 已选中或未选中。
func toggleFor(defaultMode, mode switchboard.Mode) toggle {
	if defaultMode == mode {
		return toggleOn
	}
	return toggleOff
}

// switchLabelKey 给出「切换」菜单项在目标形态下该用的字典键。
// @param target - 切换的目标形态。
// @returns 字典键。
func switchLabelKey(target switchboard.Mode) string {
	if target == switchboard.ModeOfficial {
		return "tray.switchToOfficial"
	}
	return "tray.switchToShell"
}

// dbusmenuLayout 是 dbusmenu 的 (ia{sv}av)：ID、属性、子项。
type dbusmenuLayout struct {
	ID       int32
	Props    map[string]dbus.Variant
	Children []dbus.Variant
}

// dbusmenuProperty 是 dbusmenu 的 (ia{sv})，用于 GetGroupProperties 与
// ItemsPropertiesUpdated。
type dbusmenuProperty struct {
	ID    int32
	Props map[string]dbus.Variant
}

// dbusmenuRemoved 是 ItemsPropertiesUpdated 第二个参数 (ias) 的项：被移除的属性名。
// 本实现从不移除属性（只有值在变），因此始终传空列表，类型仍需存在以对齐签名。
type dbusmenuRemoved struct {
	ID    int32
	Props []string
}

// menuChange 是一次菜单变化的通知内容。
type menuChange struct {
	// StructureChanged 为真表示菜单形状变了（增删项、改层级），宿主必须重取整树。
	StructureChanged bool
	// Updated 是属性发生变化（文案、可用性、勾选态）的项及其新属性。
	Updated []dbusmenuProperty
}

// diffEntries 比较两份菜单内容，决定该发哪种通知。
//
// 为什么区分两种通知：属性变化若发整树 LayoutUpdated，宿主会重建整个弹窗；用户正在交互
// 的弹窗被销毁重建，实测会把 DDE 的托盘弹窗打进「读到 null 的 x/y」的坏状态，此后所有
// 应用的托盘菜单都弹不出来（只能重启 dde-shell 恢复）。dbusmenu 为此提供了
// ItemsPropertiesUpdated：只报告变化的项，宿主原地更新即可。
// @param previous - 变更前的菜单内容。
// @param current - 变更后的菜单内容。
// @returns 需要发出的通知；两者皆空表示无需通知。
func diffEntries(previous, current []menuEntry) menuChange {
	if !sameStructure(previous, current) {
		return menuChange{StructureChanged: true}
	}
	var updated []dbusmenuProperty
	for _, entry := range flattenEntries(current) {
		old, ok := findEntry(previous, entry.ID)
		if !ok {
			continue
		}
		if old.Label == entry.Label && old.Enabled == entry.Enabled && old.Toggle == entry.Toggle {
			continue
		}
		updated = append(updated, dbusmenuProperty{ID: entry.ID, Props: entryLayout(entry).Props})
	}
	return menuChange{Updated: updated}
}

// sameStructure 比较两份菜单的形状：项 ID、分隔线、子项层级与顺序。
// 形状相同意味着宿主不必重建弹窗，只更新变化的属性即可。
// @param a - 旧内容。
// @param b - 新内容。
// @returns 形状一致为真。
func sameStructure(a, b []menuEntry) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].ID != b[i].ID || a[i].Separator != b[i].Separator {
			return false
		}
		if !sameStructure(a[i].Children, b[i].Children) {
			return false
		}
	}
	return true
}

// menuObject 实现 com.canonical.dbusmenu。宿主在展开菜单时按需调用它，因此这里始终以
// 最新状态回应，而不是推送整棵树。
type menuObject struct {
	mu      sync.Mutex
	parent  *item
	entries []menuEntry
}

// newMenuObject 构造菜单对象并写入初始内容。
// @param it - 所属托盘项。
// @returns 菜单对象。
func newMenuObject(it *item) *menuObject {
	m := &menuObject{parent: it}
	// 初始内容按「无客户端、默认薄壳版」渲染；Register 紧接的第一次刷新会覆盖它，
	// 但两帧之间宿主可能已经来取布局，给出一棵完整菜单比给空树安全。
	m.entries = menuEntries(trayState{
		defaultMode: switchboard.ModeShell,
		locale:      it.client.opts.Locale,
	})
	return m
}

// properties 声明 dbusmenu 的对象属性。
// @returns 按接口分组的属性表。
func (m *menuObject) properties() prop.Map {
	return prop.Map{
		menuInterface: {
			"Version":       {Value: uint32(3), Emit: prop.EmitFalse},
			"Status":        {Value: "normal", Emit: prop.EmitFalse},
			"TextDirection": {Value: "ltr", Emit: prop.EmitFalse},
			"IconThemePath": {Value: []string{}, Emit: prop.EmitFalse},
		},
	}
}

// setState 更新菜单内容，并按变化的种类通知宿主。
// @param state - 当前状态。
func (m *menuObject) setState(state trayState) {
	entries := menuEntries(state)
	m.mu.Lock()
	previous := m.entries
	m.entries = entries
	m.mu.Unlock()

	change := diffEntries(previous, entries)
	switch {
	case change.StructureChanged:
		_ = m.parent.client.conn.Emit(menuPath, menuInterface+".LayoutUpdated", uint32(0), int32(0))
	case len(change.Updated) > 0:
		_ = m.parent.client.conn.Emit(menuPath, menuInterface+".ItemsPropertiesUpdated",
			change.Updated, []dbusmenuRemoved{})
	}
}

// findEntry 在菜单树里按 ID 找一项（含子项）。
// @param entries - 待搜索的层级。
// @param id - 目标 ID。
// @returns 找到的项与是否找到。
func findEntry(entries []menuEntry, id int32) (menuEntry, bool) {
	for _, entry := range entries {
		if entry.ID == id {
			return entry, true
		}
		if found, ok := findEntry(entry.Children, id); ok {
			return found, true
		}
	}
	return menuEntry{}, false
}

// flattenEntries 把菜单树摊平成一层，供按 ID 查询属性的调用使用。
// @param entries - 待摊平的层级。
// @returns 摊平后的项，顺序为父项先于其子项。
func flattenEntries(entries []menuEntry) []menuEntry {
	out := make([]menuEntry, 0, len(entries))
	for _, entry := range entries {
		out = append(out, entry)
		out = append(out, flattenEntries(entry.Children)...)
	}
	return out
}

// GetLayout 返回菜单树。dbusmenu 要求根项 ID 为 0，子项挂在它下面；子菜单通过
// parentID 再取一次，因此这里按 parentID 在树里定位层级，而不是只支持一层。
// @param parentID - 请求的父项 ID；0 表示根。
// @param recursionDepth - 递归深度；-1 表示取全部。
// @param propertyNames - 需要的属性名；为空表示全部。
// @returns 修订号、布局与错误。
func (m *menuObject) GetLayout(parentID int32, recursionDepth int32, propertyNames []string) (uint32, dbusmenuLayout, *dbus.Error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	entries := m.entries
	if parentID != 0 {
		parent, ok := findEntry(m.entries, parentID)
		if !ok {
			// 未知父项按空树回答：dbusmenu 约定未知 ID 给空布局而不是错误。
			return 0, dbusmenuLayout{ID: parentID, Props: map[string]dbus.Variant{}}, nil
		}
		entries = parent.Children
	}
	children := make([]dbus.Variant, 0, len(entries))
	for _, entry := range entries {
		children = append(children, dbus.MakeVariant(entryLayout(entry)))
	}
	return 0, dbusmenuLayout{
		ID:       parentID,
		Props:    map[string]dbus.Variant{"children-display": dbus.MakeVariant("submenu")},
		Children: children,
	}, nil
}

// GetGroupProperties 返回若干项的属性。子项与父项共用同一套 ID 空间，因此查询前先把
// 菜单树摊平，否则宿主问子项属性时会得到空结果、子菜单显示为空白。
// @param ids - 请求的项 ID。
// @param propertyNames - 需要的属性名；本实现忽略它，始终返回全部属性。
// @returns 每项的属性集合与错误。
func (m *menuObject) GetGroupProperties(ids []int32, propertyNames []string) ([]dbusmenuProperty, *dbus.Error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	wanted := make(map[int32]bool, len(ids))
	for _, id := range ids {
		wanted[id] = true
	}
	var out []dbusmenuProperty
	for _, entry := range flattenEntries(m.entries) {
		// 空 ids 按 dbusmenu 约定表示「全部」。
		if len(ids) > 0 && !wanted[entry.ID] {
			continue
		}
		out = append(out, dbusmenuProperty{ID: entry.ID, Props: entryLayout(entry).Props})
	}
	return out, nil
}

// Event 处理菜单项被点击。
// @param id - 项 ID。
// @param eventID - 事件名；只处理 clicked。
// @param data - 事件数据，本实现不使用。
// @param timestamp - 宿主给的时间戳，本实现不使用。
// @returns 恒为 nil。
func (m *menuObject) Event(id int32, eventID string, data dbus.Variant, timestamp uint32) *dbus.Error {
	if eventID != "clicked" {
		return nil
	}
	board := m.parent.client.opts.Board
	switch id {
	case idSwitch:
		m.parent.client.perform("tray.switchFailed", board.Switch)
	case idDefaultShell:
		m.parent.client.perform("tray.setDefaultFailed", func() error { return board.SetDefault(switchboard.ModeShell) })
	case idDefaultOfficial:
		m.parent.client.perform("tray.setDefaultFailed", func() error { return board.SetDefault(switchboard.ModeOfficial) })
	case idAbout:
		// 关于只负责起一个弹窗进程，不等待用户关闭，因此可以同步做；失败由 perform
		// 统一记录并发通知。idSettings 是子菜单入口，宿主不会给它发 clicked。
		if show := m.parent.client.opts.About; show != nil {
			dialog := aboutDialog(m.parent.client.opts.Locale, board.Status().Mode, m.parent.client.opts.IconPrefix)
			m.parent.client.perform("tray.aboutFailed", func() error { return show(dialog) })
		}
	case idQuit:
		// 退出走独立 goroutine：回调要停客户端并可能等优雅退出（最长 stopGrace），
		// 在 D-Bus 方法里同步做会把宿主的调用一起堵住。
		if quit := m.parent.client.opts.Quit; quit != nil {
			go quit()
		}
	}
	return nil
}

// aboutDialog 组织「关于」对话框的内容。
//
// 分组按「读一眼就知道现在是什么状态」与「要定位问题才看」来分：当前形态与包 ID 是状态
// 摘要，放在分隔线上方、整行居中；DSH 版本、两个仓库与封装作者是定位问题用的事实，放在
// 分隔线下方的事实网格里。这一组事实与薄壳版标题栏原来的关于信息同源（那个弹框已移除，
// 信息只在托盘这一处）。玲珑包版本不出现在任何一组里——它是对话框顶部的版本行
// （`--version`），重复一行只会让用户对不上。
//
// 各组的顺序即显示顺序；两个仓库地址是 http(s)，由助手渲染成可点击链接；包 ID 是不可选中
// 文本里唯一需要原样抄给别人的值（报障时要写清是哪个包），因此只有它带一键复制按钮。
// @param locale - 语言。
// @param mode - 当前在跑的客户端形态；空串表示没有客户端在运行。
// @param prefix - 安装前缀，用于定位随包的 harness 版本文件。
// @returns 窗口标题、居中行、事实行与复制按钮文案。
func aboutDialog(locale i18n.Locale, mode switchboard.Mode, prefix string) about.Dialog {
	return about.Dialog{
		Title: i18n.T(locale, "about.title"),
		CenterFields: []about.Field{
			{Label: i18n.T(locale, "about.mode"), Value: modeName(locale, mode)},
			{Label: i18n.T(locale, "about.package"), Value: about.PackageID, Copy: true},
		},
		Fields: []about.Field{
			{Label: i18n.T(locale, "about.dshVersion"), Value: about.HarnessVersion(prefix)},
			{Label: i18n.T(locale, "about.upstreamRepo"), Value: about.UpstreamRepo},
			{Label: i18n.T(locale, "about.packager"), Value: about.Packager},
			{Label: i18n.T(locale, "about.repo"), Value: about.GithubRepo},
		},
		CopyLabel:       i18n.T(locale, "about.copy"),
		CopiedLabel:     i18n.T(locale, "about.copied"),
		OpenFailedLabel: i18n.T(locale, "about.openFailed"),
	}
}

// AboutToShow 是宿主展开菜单前的询问。菜单内容始终是最新的，无需延迟构建。
// @param id - 项 ID，本实现不使用。
// @returns 恒为 false（没有需要延迟更新的内容）。
func (m *menuObject) AboutToShow(id int32) (bool, *dbus.Error) { return false, nil }

// AboutToShowGroup 是批量版本的 AboutToShow。
// @param ids - 项 ID，本实现不使用。
// @returns 两个空列表（无需更新、无需重新取布局）。
func (m *menuObject) AboutToShowGroup(ids []int32) ([]int32, []int32, *dbus.Error) {
	return nil, nil, nil
}

// entryLayout 把一项菜单翻译成 dbusmenu 布局。
// @param entry - 菜单项。
// @returns 布局。
func entryLayout(entry menuEntry) dbusmenuLayout {
	props := map[string]dbus.Variant{
		"label":   dbus.MakeVariant(entry.Label),
		"enabled": dbus.MakeVariant(entry.Enabled),
		"visible": dbus.MakeVariant(true),
	}
	switch {
	case entry.Separator:
		props["type"] = dbus.MakeVariant("separator")
	case len(entry.Children) > 0:
		// 子菜单入口：宿主据此画出展开箭头并在展开时用本项 ID 再取一次布局。
		props["type"] = dbus.MakeVariant("standard")
		props["children-display"] = dbus.MakeVariant("submenu")
	default:
		props["type"] = dbus.MakeVariant("standard")
	}
	// 单选项：toggle-type 决定标记样式（radio 画圆点），toggle-state 决定是否勾上。
	switch entry.Toggle {
	case toggleOn:
		props["toggle-type"] = dbus.MakeVariant("radio")
		props["toggle-state"] = dbus.MakeVariant(int32(1))
	case toggleOff:
		props["toggle-type"] = dbus.MakeVariant("radio")
		props["toggle-state"] = dbus.MakeVariant(int32(0))
	}
	return dbusmenuLayout{ID: entry.ID, Props: props}
}
