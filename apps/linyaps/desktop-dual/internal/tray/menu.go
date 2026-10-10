package tray

import (
	"sync"

	"github.com/godbus/dbus/v5"
	"github.com/godbus/dbus/v5/prop"

	"github.com/deepseek-ai/deepseek-harness/apps/linyaps/desktop-dual/internal/i18n"
	"github.com/deepseek-ai/deepseek-harness/apps/linyaps/desktop-dual/internal/switchboard"
)

// 菜单项 ID。dbusmenu 用整数标识项，宿主回调时只给 ID，因此这组常量是菜单与动作之间
// 唯一约定。
const (
	idSwitch          int32 = 1
	idSeparatorTop    int32 = 2
	idDefaultShell    int32 = 3
	idDefaultOfficial int32 = 4
	idSeparatorBottom int32 = 5
	idQuit            int32 = 6
)

// menuEntry 是菜单中的一项（与 D-Bus 无关的纯数据）。
type menuEntry struct {
	ID        int32
	Label     string
	Enabled   bool
	Separator bool
}

// menuEntries 按当前状态生成菜单。
//
// 「默认启动」两项在已经是默认形态时置灰：托盘菜单没有勾选框，置灰是唯一不额外引入
// dbusmenu 单选语义就能表达「当前就是这个」的手段，而单选语义在不同宿主上的表现并不一致。
// @param state - 当前状态。
// @returns 菜单项，顺序即显示顺序。
func menuEntries(state trayState) []menuEntry {
	return []menuEntry{
		{ID: idSwitch, Label: i18n.T(state.locale, switchLabelKey(state.nextMode())), Enabled: !state.switching},
		{ID: idSeparatorTop, Separator: true},
		{ID: idDefaultShell, Label: i18n.T(state.locale, "tray.defaultShell"),
			Enabled: state.defaultMode != switchboard.ModeShell},
		{ID: idDefaultOfficial, Label: i18n.T(state.locale, "tray.defaultOfficial"),
			Enabled: state.defaultMode != switchboard.ModeOfficial},
		{ID: idSeparatorBottom, Separator: true},
		{ID: idQuit, Label: i18n.T(state.locale, "tray.quit"), Enabled: true},
	}
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

// dbusmenuProperty 是 dbusmenu 的 (ia{sv})，用于 GetGroupProperties。
type dbusmenuProperty struct {
	ID    int32
	Props map[string]dbus.Variant
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

// setState 更新菜单内容并通知宿主重取布局。
// @param state - 当前状态。
func (m *menuObject) setState(state trayState) {
	entries := menuEntries(state)
	m.mu.Lock()
	changed := !sameEntries(m.entries, entries)
	m.entries = entries
	m.mu.Unlock()
	if !changed {
		// 内容没变就不发信号：宿主每收到一次 LayoutUpdated 都会重新拉取整棵树。
		return
	}
	_ = m.parent.client.conn.Emit(menuPath, menuInterface+".LayoutUpdated", uint32(0), int32(0))
}

// sameEntries 比较两份菜单内容是否一致（标签与可用性）。
// @param a - 旧内容。
// @param b - 新内容。
// @returns 完全一致为真。
func sameEntries(a, b []menuEntry) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// GetLayout 返回菜单树。dbusmenu 要求根项 ID 为 0，子项挂在它下面。
// @param parentID - 请求的父项 ID；本实现只有一层菜单，非 0 一律按空树回答。
// @param recursionDepth - 递归深度；-1 表示取全部。
// @param propertyNames - 需要的属性名；为空表示全部。
// @returns 修订号、布局与错误。
func (m *menuObject) GetLayout(parentID int32, recursionDepth int32, propertyNames []string) (uint32, dbusmenuLayout, *dbus.Error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if parentID != 0 {
		return 0, dbusmenuLayout{ID: parentID, Props: map[string]dbus.Variant{}}, nil
	}
	children := make([]dbus.Variant, 0, len(m.entries))
	for _, entry := range m.entries {
		children = append(children, dbus.MakeVariant(entryLayout(entry)))
	}
	return 0, dbusmenuLayout{
		ID:       0,
		Props:    map[string]dbus.Variant{"children-display": dbus.MakeVariant("submenu")},
		Children: children,
	}, nil
}

// GetGroupProperties 返回若干项的属性。
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
	for _, entry := range m.entries {
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
		m.parent.client.perform(board.Switch)
	case idDefaultShell:
		m.parent.client.perform(func() error { return board.SetDefault(switchboard.ModeShell) })
	case idDefaultOfficial:
		m.parent.client.perform(func() error { return board.SetDefault(switchboard.ModeOfficial) })
	case idQuit:
		// 退出走独立 goroutine：回调要停客户端并可能等优雅退出（最长 stopGrace），
		// 在 D-Bus 方法里同步做会把宿主的调用一起堵住。
		if quit := m.parent.client.opts.Quit; quit != nil {
			go quit()
		}
	}
	return nil
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
	if entry.Separator {
		props["type"] = dbus.MakeVariant("separator")
	} else {
		props["type"] = dbus.MakeVariant("standard")
	}
	return dbusmenuLayout{ID: entry.ID, Props: props}
}
