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
// Menu 指向 dbusmenu 对象；ItemIsMenu 必须为 false，宿主才会把左键当动作（Activate）、
// 把右键当菜单：为 true 时宿主认为本项只有菜单，左键也会直接展开它。本机 DDE 实测两种
// 取值的差异与此一致：false 时左键收到 Activate，右键取菜单树。
// @returns 按接口分组的属性表。
func (it *item) properties() prop.Map {
	return prop.Map{
		sniInterface: {
			"Category":   {Value: "ApplicationStatus", Emit: prop.EmitFalse},
			"Id":         {Value: "dsh-desktop", Emit: prop.EmitFalse},
			"Title":      {Value: "DeepSeek Harness", Emit: prop.EmitTrue},
			"Status":     {Value: "Active", Emit: prop.EmitTrue},
			"IconName":   {Value: "", Emit: prop.EmitFalse},
			"IconPixmap": {Value: []Pixmap{}, Emit: prop.EmitTrue},
			"ToolTip":    {Value: toolTip{}, Emit: prop.EmitTrue},
			"ItemIsMenu": {Value: false, Emit: prop.EmitFalse},
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

// Activate 是左键单击：把客户端显示出来。
//
// 「显示」而不是「切换」：切换是破坏性的（停掉当前客户端、中断正在进行的会话），不该
// 由最容易误触的左键承担；而用户关掉客户端窗口之后，托盘是唯一能把它叫回来的入口。
// 没有客户端在跑时这会启动默认形态，官方版已在跑（窗口被关掉只是隐藏）时由它自己的
// 单实例锁把窗口唤回——两种情形都由 switchboard.Present 收敛。
// @param x - 宿主给的鼠标横坐标，本实现不使用。
// @param y - 宿主给的鼠标纵坐标，本实现不使用。
// @returns 恒为 nil；失败通过通知与提示表达，不让宿主看到 D-Bus 错误。
func (it *item) Activate(x, y int32) *dbus.Error {
	it.client.perform("tray.showFailed", it.client.opts.Board.Present)
	return nil
}

// SecondaryActivate 是中键单击：与左键同义，避免中键无反应。
// @param x - 宿主给的鼠标横坐标，本实现不使用。
// @param y - 宿主给的鼠标纵坐标，本实现不使用。
// @returns 恒为 nil。
func (it *item) SecondaryActivate(x, y int32) *dbus.Error {
	it.client.perform("tray.showFailed", it.client.opts.Board.Present)
	return nil
}

// ContextMenu 是宿主请求我们弹出菜单。菜单由 dbusmenu 托管，宿主通常不会调用它；
// 这里不做事，避免出现两个菜单。
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

// perform 执行一次托盘动作：失败时记录、按动作名发桌面通知，最后刷新托盘。
// @param failureKey - 失败通知标题的字典键，让用户知道是哪一步没成。
// @param action - 要执行的动作。
func (c *Client) perform(failureKey string, action func() error) {
	if err := action(); err != nil {
		c.logf("switcher action failed: %v", err)
		c.NotifyFailure(i18n.T(c.opts.Locale, failureKey), err.Error())
	}
	c.Refresh()
}
