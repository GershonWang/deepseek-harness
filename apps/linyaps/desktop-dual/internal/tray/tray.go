// Package tray 把切换器暴露成系统托盘入口：一个 StatusNotifierItem（SNI）加一棵
// dbusmenu 菜单，没有窗口。
//
// 为什么用 SNI 而不是 DDE 的托盘插件接口：dde-tray-loader 的 PluginsItemInterfaceV2
// 是桌面进程内加载的 C++ 插件，要求把 .so 装进宿主插件目录并匹配 ABI，玲珑包只读且
// 无提权，做不到。SNI 是跨桌面的 D-Bus 标准，DDE 的托盘宿主（dde-shell 经
// org.kde.StatusNotifierWatcher）本来就托管第三方进程注册的图标——本机实测容器内可连
// 宿主会话总线、可申请 well-known name，因此这条路可行。
//
// 单实例由本包自己的 well-known name 保证（SNI 的 org.kde.StatusNotifierItem-<pid>-<n>
// 名字随进程号变化，不能当互斥锁）。
package tray

import (
	"errors"
	"fmt"
	"os"
	"sync"

	"github.com/godbus/dbus/v5"
	"github.com/godbus/dbus/v5/prop"

	"github.com/deepseek-ai/deepseek-harness/apps/linyaps/desktop-dual/internal/i18n"
	"github.com/deepseek-ai/deepseek-harness/apps/linyaps/desktop-dual/internal/switchboard"
)

// ErrAlreadyRunning 表示已经有一个切换器实例持有单实例名。
var ErrAlreadyRunning = errors.New("tray: another switcher instance is already running")

// ErrWatcherUnavailable 表示当前会话没有 SNI 宿主（没有进程提供
// org.kde.StatusNotifierWatcher）。调用方据此走「托盘不可用」的降级路径。
var ErrWatcherUnavailable = errors.New("tray: no StatusNotifierWatcher on the session bus")

const (
	// watcherName 是 SNI 宿主在会话总线上的固定名字。
	watcherName = "org.kde.StatusNotifierWatcher"
	// watcherPath 是该宿主对象的固定路径。
	watcherPath = dbus.ObjectPath("/StatusNotifierWatcher")
	// itemPath 是本进程的托盘项对象路径，SNI 约定用这个名字。
	itemPath = dbus.ObjectPath("/StatusNotifierItem")
	// menuPath 是 dbusmenu 对象路径；SNI 的 Menu 属性指向它。
	menuPath = dbus.ObjectPath("/MenuBar")
	// sniInterface 是托盘项接口名。
	sniInterface = "org.kde.StatusNotifierItem"
	// menuInterface 是菜单接口名。
	menuInterface = "com.canonical.dbusmenu"
)

// Pixmap 是 SNI 的 (iiay) 位图：宽、高，以及 ARGB32（网络字节序）像素。
//
// 为什么自己画而不是给宿主一个图标名：宿主进程在容器外，按名字解析要经过宿主图标主题，
// 而玲珑包安装的图标不在宿主的主题目录里；直接传位图则完全自包含。
type Pixmap struct {
	Width  int32
	Height int32
	Data   []byte
}

// toolTip 是 SNI 的 ToolTip 属性 (sa(iiay)ss)：图标名、位图、标题、正文。
type toolTip struct {
	IconName   string
	IconPixmap []Pixmap
	Title      string
	Text       string
}

// Options 是注册托盘项所需的外部依赖。
type Options struct {
	// Board 是切换器本体；托盘只读取状态并触发它的动作。
	Board *switchboard.Switchboard
	// Locale 决定菜单与提示的语言。
	Locale i18n.Locale
	// IconPrefix 是安装前缀（<PREFIX>），用于定位随包图标；缺失时回退到内置图形。
	IconPrefix string
	// Version 是玲珑包版本，展示在「关于」里。
	Version string
	// About 弹出「关于」对话框，入参是本地化的附加信息；为 nil 时该菜单项不做事。
	About func(comments string) error
	// Quit 是菜单「退出」的回调。
	Quit func()
	// Logf 记录降级与失败，便于真机排查；为 nil 时不记录。
	Logf func(format string, args ...any)
}

// Client 持有会话总线连接与单实例名，并按需注册/注销托盘项。
//
// 单实例名在 Connect 时申请并一直持有到进程结束；托盘项则可能在宿主缺失时注册失败，
// 之后由调用方周期调用 EnsureRegistered 收敛（宿主重启是真实场景：DDE 崩溃重启后托盘
// 应当自动回来）。
type Client struct {
	mu        sync.Mutex
	conn      *dbus.Conn
	mutexName string
	item      *item
	opts      Options

	// watcherOwner 是注册成功时宿主的连接唯一名（形如 :1.30），未注册时为空串。
	// 宿主每次重启都会换唯一名，因此它同时是「我们注册到的那个宿主还在不在」的判据。
	watcherOwner string
}

// item 是一次已注册的托盘项。
type item struct {
	client   *Client
	name     string
	props    *prop.Properties
	menu     *menuObject
	rendered render

	// 最近一次渲染的状态，用于抑制重复的 D-Bus 属性变更。
	last trayState
}

// trayState 是驱动渲染的最小状态：切换器快照加语言。
type trayState struct {
	mode        switchboard.Mode
	defaultMode switchboard.Mode
	switching   bool
	err         string
	locale      i18n.Locale
}

// Connect 连接会话总线并申请单实例名。
// @param mutexName - 单实例用的 well-known name。
// @param opts - 注册与渲染所需的依赖；这里先存下来，供 EnsureRegistered 与刷新使用。
// @returns 已持有单实例名的客户端；名字已被占用时返回 ErrAlreadyRunning。
func Connect(mutexName string, opts Options) (*Client, error) {
	conn, err := dbus.SessionBus()
	if err != nil {
		return nil, fmt.Errorf("tray: connecting to the session bus: %w", err)
	}
	reply, err := conn.RequestName(mutexName, dbus.NameFlagDoNotQueue)
	if err != nil {
		return nil, fmt.Errorf("tray: requesting %s: %w", mutexName, err)
	}
	if reply != dbus.RequestNameReplyPrimaryOwner {
		return nil, ErrAlreadyRunning
	}
	return &Client{conn: conn, mutexName: mutexName, opts: opts}, nil
}

// EnsureRegistered 让托盘项与当前宿主保持一致，是注册的唯一入口：未注册时注册；
// 已注册但宿主已经换过（崩溃重启后由新进程接管这个名字）时，释放旧项并按新宿主重新
// 注册；宿主没变时无副作用。因此可以在重试循环里无脑调用。
//
// 为什么以宿主的连接唯一名作为判据，而不是读宿主的 RegisteredStatusNotifierItems：
// 宿主重启会丢掉全部已注册项，而 SNI 没有「项已被遗忘」的信号，唯一名则必然变化；
// 并且唯一名不依赖宿主如何规范化列表项字符串——本机实测同一个宿主的列表里既有
// well-known-name/path 也有 :1.x/path 两种写法，按字符串匹配有可能把自己误判成未注册，
// 从而每五秒重注册一次、图标反复闪烁。已注册项被同一进程内的 watcher 重建丢掉这种
// 情况检测不到，代价是那种宿主只能靠重启进程让图标回来。
// @returns 注册或重新注册失败的原因；会话里没有宿主时为 ErrWatcherUnavailable。
func (c *Client) EnsureRegistered() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.item != nil {
		owner, err := c.watcherOwnerLocked()
		if err != nil {
			return fmt.Errorf("tray: probing %s: %w", watcherName, err)
		}
		if owner == c.watcherOwner {
			return nil
		}
		if owner == "" {
			c.logf("tray: %s is gone; dropping the item until a host returns", watcherName)
		} else {
			c.logf("tray: %s moved to %s; re-registering the item", watcherName, owner)
		}
		c.releaseLocked()
	}
	return c.registerLocked()
}

// registerLocked 执行一次完整的注册：申请 SNI 名、导出对象、向宿主登记。
// 调用方必须持有 c.mu，并保证当前处于未注册状态。
// @returns 宿主缺失时返回 ErrWatcherUnavailable；其余为连接或导出失败的原因。
func (c *Client) registerLocked() error {
	owner, err := c.watcherOwnerLocked()
	if err != nil {
		return fmt.Errorf("tray: probing %s: %w", watcherName, err)
	}
	if owner == "" {
		return ErrWatcherUnavailable
	}

	name := fmt.Sprintf("%s-%d-1", sniInterface, os.Getpid())
	// AlreadyOwner 也算成功：上面任何一步失败后我们可能仍持有这个名字（只有所有步骤
	// 走完才由 releaseLocked 释放），此时重试必须能继续，否则重试循环会永远卡在
	// 「name is already owned」上，托盘再也不出现。
	if reply, err := c.conn.RequestName(name, dbus.NameFlagDoNotQueue); err != nil {
		return fmt.Errorf("tray: requesting %s: %w", name, err)
	} else if reply != dbus.RequestNameReplyPrimaryOwner && reply != dbus.RequestNameReplyAlreadyOwner {
		return fmt.Errorf("tray: requesting %s: unexpected reply %v", name, reply)
	}
	// 后续任一步失败都要把名字还回去：留着一个已持有但未注册生效的名字，会让下一次
	// 重试与「另一个实例正在运行」无法区分。
	fail := func(err error) error {
		_, _ = c.conn.ReleaseName(name)
		return err
	}

	it := &item{client: c, name: name}
	it.menu = newMenuObject(it)
	if err := c.conn.Export(it, itemPath, sniInterface); err != nil {
		return fail(fmt.Errorf("tray: exporting %s: %w", sniInterface, err))
	}
	if err := c.conn.Export(it.menu, menuPath, menuInterface); err != nil {
		return fail(fmt.Errorf("tray: exporting %s: %w", menuInterface, err))
	}

	props, err := prop.Export(c.conn, itemPath, it.properties())
	if err != nil {
		return fail(fmt.Errorf("tray: exporting SNI properties: %w", err))
	}
	it.props = props
	if _, err := prop.Export(c.conn, menuPath, it.menu.properties()); err != nil {
		return fail(fmt.Errorf("tray: exporting dbusmenu properties: %w", err))
	}
	c.item = it
	it.refreshLocked()

	call := c.conn.Object(watcherName, watcherPath).Call(
		watcherName+".RegisterStatusNotifierItem", 0, name)
	if call.Err != nil {
		// 注册失败就退回未注册状态：留下一个宿主不知道的对象只会让状态更难判断。
		c.releaseLocked()
		return fmt.Errorf("tray: registering with %s: %w", watcherName, call.Err)
	}
	// 记下这次登记到的是哪个宿主。登记前后宿主若又换了人，下一轮 EnsureRegistered
	// 会因为唯一名不同而重注册，因此这里不需要额外的竞态处理。
	c.watcherOwner = owner
	c.logf("tray item registered as %s with %s", name, owner)
	return nil
}

// Refresh 按切换器最新状态重绘图标、提示与菜单。未注册时为空操作。
func (c *Client) Refresh() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.item == nil {
		return
	}
	c.item.refreshLocked()
}

// Close 注销托盘项并释放单实例名。连接关闭后进程即将退出，这里只做尽量干净的收尾。
func (c *Client) Close() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.releaseLocked()
	if c.conn != nil {
		_ = c.conn.Close()
		c.conn = nil
	}
}

// releaseLocked 注销托盘项并释放 SNI 名，同时清掉记录的宿主：未注册状态与
// 「已登记到某个宿主」必须一起成立或一起失效，否则 EnsureRegistered 会拿一个
// 过期的宿主唯一名做判断。调用方必须持有 c.mu。
func (c *Client) releaseLocked() {
	if c.item == nil {
		return
	}
	_, _ = c.conn.ReleaseName(c.item.name)
	c.item = nil
	c.watcherOwner = ""
}

// watcherOwnerLocked 返回当前登记 org.kde.StatusNotifierWatcher 的连接唯一名。
//
// 分两步（先 NameHasOwner 再 GetNameOwner）而不是直接调 GetNameOwner：宿主不在时后者
// 返回 D-Bus 错误，而 NameHasOwner 正常回答 false；这样调用方不必解析错误名就能区分
// 「宿主不在」与「总线故障」，后者必须上报而不是被当成宿主缺失反复重注册。
// 调用方必须持有 c.mu。
// @returns 宿主的唯一名；会话里没有宿主时为空串。
func (c *Client) watcherOwnerLocked() (string, error) {
	hasWatcher, err := c.hasWatcher()
	if err != nil {
		return "", err
	}
	if !hasWatcher {
		return "", nil
	}
	var owner string
	if err := c.conn.BusObject().Call("org.freedesktop.DBus.GetNameOwner", 0, watcherName).Store(&owner); err != nil {
		return "", err
	}
	return owner, nil
}

// hasWatcher 询问会话总线上是否有 SNI 宿主。
//
// 经总线守护进程的 NameHasOwner 探测，而不是直接调用宿主：宿主可能尚未启动或正在重启，
// 直接调用会阻塞到超时，而探测只是查这个名字有没有主。
// @returns 宿主存在为真。
func (c *Client) hasWatcher() (bool, error) {
	var has bool
	if err := c.conn.BusObject().Call("org.freedesktop.DBus.NameHasOwner", 0, watcherName).Store(&has); err != nil {
		return false, err
	}
	return has, nil
}

// logf 在配置了 Logf 时记录一行。
// @param format - 格式串。
// @param args - 格式参数。
func (c *Client) logf(format string, args ...any) {
	if c.opts.Logf != nil {
		c.opts.Logf(format, args...)
	}
}

// NotifyFailure 通过桌面通知把失败原因告诉用户。
//
// 托盘形态没有窗口可以贴错误提示，而「点了没反应」是最难排查的失败；通知服务缺失时
// 只记录日志，绝不让通知本身影响切换流程。
// @param summary - 本地化的标题。
// @param body - 技术原因（英文诊断）。
func (c *Client) NotifyFailure(summary, body string) {
	c.mu.Lock()
	conn := c.conn
	c.mu.Unlock()
	if conn == nil {
		return
	}
	call := conn.Object("org.freedesktop.Notifications", "/org/freedesktop/Notifications").Call(
		"org.freedesktop.Notifications.Notify", 0,
		"DeepSeek Harness", uint32(0), "dsh-desktop", summary, body,
		[]string{}, map[string]dbus.Variant{}, int32(-1))
	if call.Err != nil {
		c.logf("sending the failure notification failed: %v", call.Err)
	}
}

// trayStateOf 把切换器快照收敛成渲染状态。
// @param board - 切换器。
// @param locale - 当前语言。
// @returns 渲染状态。
func trayStateOf(board *switchboard.Switchboard, locale i18n.Locale) trayState {
	status := board.Status()
	return trayState{
		mode:        status.Mode,
		defaultMode: status.DefaultMode,
		switching:   status.Switching,
		err:         status.Error,
		locale:      locale,
	}
}
