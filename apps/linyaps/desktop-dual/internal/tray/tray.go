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
	// Quit 是菜单「退出切换器」的回调。
	Quit func()
	// Logf 记录降级与失败，便于真机排查；为 nil 时不记录。
	Logf func(format string, args ...any)
}

// Client 持有会话总线连接与单实例名，并按需注册/注销托盘项。
//
// 单实例名在 Connect 时申请并一直持有到进程结束；托盘项则可能在宿主缺失时注册失败，
// 之后由调用方重试（宿主重启是真实场景：DDE 崩溃重启后托盘应当自动回来）。
type Client struct {
	mu        sync.Mutex
	conn      *dbus.Conn
	mutexName string
	item      *item
	opts      Options
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
// @param opts - 注册所需的依赖；可在 Register 时再补，但这里先存下来供重试使用。
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

// Registered 报告托盘项是否已注册。
// @returns 已注册为真。
func (c *Client) Registered() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.item != nil
}

// Register 注册托盘项（SNI + dbusmenu）。重复调用在已注册时直接返回成功，因此可以
// 在重试循环里无脑调用。
// @returns 宿主缺失时返回 ErrWatcherUnavailable；其余为连接或导出失败的原因。
func (c *Client) Register() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.item != nil {
		return nil
	}
	hasWatcher, err := c.hasWatcher()
	if err != nil {
		return fmt.Errorf("tray: probing %s: %w", watcherName, err)
	}
	if !hasWatcher {
		return ErrWatcherUnavailable
	}

	name := fmt.Sprintf("%s-%d-1", sniInterface, os.Getpid())
	if reply, err := c.conn.RequestName(name, dbus.NameFlagDoNotQueue); err != nil {
		return fmt.Errorf("tray: requesting %s: %w", name, err)
	} else if reply != dbus.RequestNameReplyPrimaryOwner {
		return fmt.Errorf("tray: %s is already owned", name)
	}

	it := &item{client: c, name: name}
	it.menu = newMenuObject(it)
	if err := c.conn.Export(it, itemPath, sniInterface); err != nil {
		return fmt.Errorf("tray: exporting %s: %w", sniInterface, err)
	}
	if err := c.conn.Export(it.menu, menuPath, menuInterface); err != nil {
		return fmt.Errorf("tray: exporting %s: %w", menuInterface, err)
	}

	props, err := prop.Export(c.conn, itemPath, it.properties())
	if err != nil {
		return fmt.Errorf("tray: exporting SNI properties: %w", err)
	}
	it.props = props
	if _, err := prop.Export(c.conn, menuPath, it.menu.properties()); err != nil {
		return fmt.Errorf("tray: exporting dbusmenu properties: %w", err)
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
	c.logf("tray item registered as %s", name)
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

// releaseLocked 注销托盘项并释放 SNI 名。调用方必须持有 c.mu。
func (c *Client) releaseLocked() {
	if c.item == nil {
		return
	}
	_, _ = c.conn.ReleaseName(c.item.name)
	c.item = nil
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
		"DeepSeek Harness", uint32(0), "dsh-desktop-dual", summary, body,
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
