package tray

import (
	"github.com/godbus/dbus/v5"
	"github.com/godbus/dbus/v5/prop"

	"github.com/deepseek-ai/deepseek-harness/apps/linyaps/desktop-dual/internal/i18n"
)

// render 是一次渲染出的可视内容：图标、提示与 SNI 状态。
type render struct {
	pixmaps []Pixmap
	tooltip toolTip
	status  string
}

// properties 声明托盘项的 SNI 属性。
//
// Menu 指向 dbusmenu 对象、ItemIsMenu 为真：宿主据此在右键时展开我们导出的菜单，
// 而不是要求我们弹一个窗口（托盘进程没有窗口可用）。
// @returns 按接口分组的属性表。
func (it *item) properties() prop.Map {
	return prop.Map{
		sniInterface: {
			"Category":   {Value: "ApplicationStatus", Emit: prop.EmitFalse},
			"Id":         {Value: "dsh-desktop-dual", Emit: prop.EmitFalse},
			"Title":      {Value: "DeepSeek Harness", Emit: prop.EmitTrue},
			"Status":     {Value: "Active", Emit: prop.EmitTrue},
			"IconName":   {Value: "", Emit: prop.EmitFalse},
			"IconPixmap": {Value: []Pixmap{}, Emit: prop.EmitTrue},
			"ToolTip":    {Value: toolTip{}, Emit: prop.EmitTrue},
			"ItemIsMenu": {Value: true, Emit: prop.EmitFalse},
			"Menu":       {Value: menuPath, Emit: prop.EmitFalse},
		},
	}
}

// refreshLocked 按切换器最新状态重绘托盘项。调用方必须持有 client.mu。
func (it *item) refreshLocked() {
	state := trayStateOf(it.client.opts.Board, it.client.opts.Locale)
	if it.rendered.pixmaps != nil && state == it.last {
		return
	}
	next := state.render(it.client.opts.IconPrefix)

	// 属性变更由 prop 包发 PropertiesChanged；SNI 另外约定 NewIcon/NewToolTip 信号，
	// 两者都发，宿主听哪一路都能刷新。
	it.props.SetMust(sniInterface, "Status", next.status)
	it.props.SetMust(sniInterface, "IconPixmap", next.pixmaps)
	it.props.SetMust(sniInterface, "ToolTip", next.tooltip)
	_ = it.client.conn.Emit(itemPath, sniInterface+".NewIcon")
	_ = it.client.conn.Emit(itemPath, sniInterface+".NewToolTip")

	it.menu.setState(state)
	it.last = state
	it.rendered = next
}

// Activate 是左键单击：切换到另一个客户端形态。
//
// SNI 没有「单击即切换」的专用语义，但 Activate 就是主操作，把它定义为切换与菜单里
// 第一项的语义一致。
// @param x - 宿主给的鼠标横坐标，本实现不使用。
// @param y - 宿主给的鼠标纵坐标，本实现不使用。
// @returns 恒为 nil；失败通过通知与提示表达，不让宿主看到 D-Bus 错误。
func (it *item) Activate(x, y int32) *dbus.Error {
	it.client.perform(it.client.opts.Board.Switch)
	return nil
}

// SecondaryActivate 是中键单击：与主操作同义，避免中键无反应。
// @param x - 宿主给的鼠标横坐标，本实现不使用。
// @param y - 宿主给的鼠标纵坐标，本实现不使用。
// @returns 恒为 nil。
func (it *item) SecondaryActivate(x, y int32) *dbus.Error {
	it.client.perform(it.client.opts.Board.Switch)
	return nil
}

// ContextMenu 是宿主请求我们弹出菜单。菜单由 dbusmenu 托管（ItemIsMenu 为真），
// 宿主通常不会调用它；这里不做事，避免出现两个菜单。
// @param x - 宿主给的鼠标横坐标，本实现不使用。
// @param y - 宿主给的鼠标纵坐标，本实现不使用。
// @returns 恒为 nil。
func (it *item) ContextMenu(x, y int32) *dbus.Error {
	return nil
}

// Scroll 是滚轮事件。切换器没有可滚动的内容，忽略。
// @param delta - 滚动量，本实现不使用。
// @param orientation - 滚动方向，本实现不使用。
// @returns 恒为 nil。
func (it *item) Scroll(delta int32, orientation string) *dbus.Error {
	return nil
}

// perform 执行一次切换器动作：失败时记录并发通知，最后刷新托盘。
// @param action - 要执行的动作。
func (c *Client) perform(action func() error) {
	if err := action(); err != nil {
		c.logf("switcher action failed: %v", err)
		c.NotifyFailure(i18n.T(c.opts.Locale, "tray.switchFailed"), err.Error())
	}
	c.Refresh()
}
