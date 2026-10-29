// 托盘项注册生命周期的测试：覆盖「宿主重启后图标自己回来」这条真实失效路径。
//
// 用例自己拉起一条私有会话总线（dbus-daemon），因此既不需要外部运行器，也不会碰到开发者
// 桌面上那条真实总线——直接跑 go test 时若连到真实会话，第一步「会话里还没有宿主」就不
// 成立，而且会在用户的托盘里留下一个探针图标。
//
// 桩宿主与切换器客户端都在测试进程内，宿主「重启」就是换一条总线连接——与真实宿主的
// 崩溃重启等价：唯一名变了、已注册项丢了，而 SNI 不会通知客户端。
package tray

import (
	"bufio"
	"errors"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/godbus/dbus/v5"

	"github.com/deepseek-ai/deepseek-harness/apps/linyaps/desktop-dual/internal/i18n"
	"github.com/deepseek-ai/deepseek-harness/apps/linyaps/desktop-dual/internal/switchboard"
)

// testMutexName 是测试客户端的单实例名，与真实入口区分开，避免在同一台机器上误判。
const testMutexName = "org.deepseek.dsh.DesktopDualSwitcherTest"

// 桌面通知服务在会话总线上的固定名字与路径：切换器动作失败时经它上报，用例据此确认
// 「第二次启动的请求确实被执行了」。
const (
	notificationsName = "org.freedesktop.Notifications"
	notificationsPath = dbus.ObjectPath("/org/freedesktop/Notifications")
)

// startPrivateBus 拉起一条只属于本次测试的会话总线，并把地址写进环境变量，之后的
// dbus.SessionBus 与 SessionBusPrivate 都连到它。
// @param t - 测试上下文。
func startPrivateBus(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("dbus-daemon"); err != nil {
		t.Skip("需要 dbus-daemon 才能拉起私有会话总线")
	}
	socket := filepath.Join(t.TempDir(), "bus")
	cmd := exec.Command("dbus-daemon", "--session", "--nofork",
		"--print-address=1", "--address=unix:path="+socket)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatalf("读取 dbus-daemon 地址失败: %v", err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatalf("启动 dbus-daemon 失败: %v", err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	})
	address, err := bufio.NewReader(stdout).ReadString('\n')
	if err != nil {
		t.Fatalf("读取会话总线地址失败: %v", err)
	}
	t.Setenv("DBUS_SESSION_BUS_ADDRESS", strings.TrimSpace(address))
	// godbus 会缓存上一次的会话总线连接：上一个用例的私有总线已随它自己的 daemon 退出
	// 而失效，但缓存未必立刻察觉。先主动关掉缓存，保证后续 SessionBus() 连到本次这条。
	if conn, err := dbus.SessionBus(); err == nil {
		_ = conn.Close()
	}
}

// fakeWatcher 是测试用的 SNI 宿主：用独立的总线连接持有 org.kde.StatusNotifierWatcher，
// 并记录收到的每一个注册请求。
type fakeWatcher struct {
	conn *dbus.Conn

	mu    sync.Mutex
	items []string
}

// RegisterStatusNotifierItem 记录注册请求；用例断言的就是「宿主到底有没有收到」。
// @param service - 宿主收到的服务名，本实现只记录、不校验。
// @returns 恒为 nil。
func (w *fakeWatcher) RegisterStatusNotifierItem(service string) *dbus.Error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.items = append(w.items, service)
	return nil
}

// registered 返回已收到的注册请求副本。
// @returns 注册请求列表，顺序与收到顺序一致。
func (w *fakeWatcher) registered() []string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return append([]string(nil), w.items...)
}

// stop 关掉连接：总线随即回收 well-known 名，等价于宿主进程崩溃。
func (w *fakeWatcher) stop() {
	_ = w.conn.Close()
}

// newBusConnection 打开一条握手完成的私有会话总线连接。
//
// SessionBusPrivate 给的是「还没握手」的连接：必须先 Auth 与 Hello，否则总线不认这个
// 客户端，发出去的 RequestName 永远等不到回复。
// @param t - 测试上下文。
// @returns 已握手的连接；调用方负责关闭。
func newBusConnection(t *testing.T) *dbus.Conn {
	t.Helper()
	conn, err := dbus.SessionBusPrivate()
	if err != nil {
		t.Fatalf("打开私有会话总线连接失败: %v", err)
	}
	if err := conn.Auth(nil); err != nil {
		_ = conn.Close()
		t.Fatalf("会话总线认证失败: %v", err)
	}
	if err := conn.Hello(); err != nil {
		_ = conn.Close()
		t.Fatalf("会话总线握手失败: %v", err)
	}
	return conn
}

// startFakeWatcher 拉起一个桩宿主。
//
// 申请名字会一直等到上一个宿主的连接被总线回收为止，因为总线处理断开是异步的；这段
// 等待也正是真实宿主重启时新宿主必然要经历的窗口。
// @returns 已就绪的桩宿主。
func startFakeWatcher(t *testing.T) *fakeWatcher {
	t.Helper()
	conn := newBusConnection(t)
	deadline := time.Now().Add(5 * time.Second)
	for {
		reply, err := conn.RequestName(watcherName, dbus.NameFlagDoNotQueue)
		if err == nil && reply == dbus.RequestNameReplyPrimaryOwner {
			break
		}
		if time.Now().After(deadline) {
			_ = conn.Close()
			t.Fatalf("申请 %s 失败: reply=%v err=%v", watcherName, reply, err)
		}
		time.Sleep(20 * time.Millisecond)
	}
	w := &fakeWatcher{conn: conn}
	if err := conn.Export(w, watcherPath, watcherName); err != nil {
		_ = conn.Close()
		t.Fatalf("导出 %s 失败: %v", watcherName, err)
	}
	return w
}

// TestEnsureRegisteredFollowsTheWatcher 覆盖注册状态的四个转折：会话里还没有宿主、
// 宿主就位、宿主没变、宿主换人。
//
// 最后一条是本次修复的目标：宿主重启后若不再注册，用户会同时失去托盘图标与切换入口，
// 而重新点桌面入口又会被单实例规则挡掉。
func TestEnsureRegisteredFollowsTheWatcher(t *testing.T) {
	startPrivateBus(t)

	client, err := Connect(testMutexName, Options{
		Board:  switchboard.New(t.TempDir()),
		Locale: i18n.En,
		Logf:   t.Logf,
	})
	if err != nil {
		t.Fatalf("连接会话总线失败: %v", err)
	}
	t.Cleanup(client.Close)

	// 1) 会话里还没有宿主：如实报告宿主不可用，而不是留下一个宿主不知道的半注册状态。
	if err := client.EnsureRegistered(); !errors.Is(err, ErrWatcherUnavailable) {
		t.Fatalf("没有宿主时应返回 ErrWatcherUnavailable，得到 %v", err)
	}

	// 2) 宿主就位：注册成功，宿主收到一次登记。
	hostA := startFakeWatcher(t)
	t.Cleanup(hostA.stop)
	if err := client.EnsureRegistered(); err != nil {
		t.Fatalf("宿主就位时注册失败: %v", err)
	}
	if got := hostA.registered(); len(got) != 1 {
		t.Fatalf("宿主应收到一次登记，得到 %v", got)
	}

	// 3) 宿主没变：重复调用是空操作；否则宿主会反复重建图标，视觉上就是闪烁。
	if err := client.EnsureRegistered(); err != nil {
		t.Fatalf("宿主没变时不应报错: %v", err)
	}
	if got := hostA.registered(); len(got) != 1 {
		t.Fatalf("宿主没变时不应重复登记，得到 %v", got)
	}

	// 4) 宿主换人：新宿主必须收到登记，否则托盘入口永久消失。
	hostA.stop()
	hostB := startFakeWatcher(t)
	t.Cleanup(hostB.stop)
	if err := client.EnsureRegistered(); err != nil {
		t.Fatalf("宿主换人后重新注册失败: %v", err)
	}
	if got := hostB.registered(); len(got) != 1 {
		t.Fatalf("新宿主应收到一次登记，得到 %v", got)
	}
}

// fakeNotifier 是桩通知服务：只记录收到的通知标题。
type fakeNotifier struct {
	mu        sync.Mutex
	summaries []string
}

// Notify 记录一次通知。参数与 D-Bus 签名 susssasa{sv}i 一一对应，本实现只关心标题。
//
// 返回值必须是「通知 ID + *dbus.Error」：godbus 只导出最后一个返回值是 *dbus.Error 的
// 方法，少了它这个方法不会被注册，调用方会收到「Unknown / invalid method」。
// @param summary - 通知标题，也是本桩唯一保留的字段。
// @returns 通知 ID（恒为 1）与错误（恒为 nil）。
func (n *fakeNotifier) Notify(_ string, _ uint32, _, summary, _ string, _ []string,
	_ map[string]dbus.Variant, _ int32) (uint32, *dbus.Error) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.summaries = append(n.summaries, summary)
	return 1, nil
}

// received 返回已收到的通知标题副本。
// @returns 通知标题列表，顺序与收到顺序一致。
func (n *fakeNotifier) received() []string {
	n.mu.Lock()
	defer n.mu.Unlock()
	return append([]string(nil), n.summaries...)
}

// startFakeNotifier 拉起一个桩通知服务。
// @returns 已就绪的桩服务。
func startFakeNotifier(t *testing.T) *fakeNotifier {
	t.Helper()
	conn := newBusConnection(t)
	t.Cleanup(func() { _ = conn.Close() })
	reply, err := conn.RequestName(notificationsName, dbus.NameFlagDoNotQueue)
	if err != nil || reply != dbus.RequestNameReplyPrimaryOwner {
		t.Fatalf("申请 %s 失败: reply=%v err=%v", notificationsName, reply, err)
	}
	n := &fakeNotifier{}
	if err := conn.Export(n, notificationsPath, notificationsName); err != nil {
		t.Fatalf("导出 %s 失败: %v", notificationsName, err)
	}
	return n
}

// TestSecondInstanceAsksRunningOneToPresent 覆盖桌面图标被再次点击这条路径：第二个切换器
// 申请单实例名失败后，必须把「显示客户端」的请求交给已运行的实例，而不是静默退出。
//
// 判据用桌面通知：用例环境里没有可显示、也可不经拉起就打开的客户端，运行实例的 Present
// 必然失败并经通知上报；收到这条通知即证明请求确实被运行实例执行了。
func TestSecondInstanceAsksRunningOneToPresent(t *testing.T) {
	startPrivateBus(t)
	notifier := startFakeNotifier(t)

	running, err := Connect(testMutexName, Options{
		Board:  switchboard.New(t.TempDir()),
		Locale: i18n.En,
		Logf:   t.Logf,
	})
	if err != nil {
		t.Fatalf("第一个实例应持有单实例名: %v", err)
	}
	t.Cleanup(running.Close)

	if _, err := Connect(testMutexName, Options{Locale: i18n.En}); !errors.Is(err, ErrAlreadyRunning) {
		t.Fatalf("第二个实例应被单实例名挡住，得到 %v", err)
	}

	if err := RequestPresent(testMutexName); err != nil {
		t.Fatalf("向运行实例发出显示请求失败: %v", err)
	}

	want := i18n.T(i18n.En, "tray.showFailed")
	deadline := time.Now().Add(3 * time.Second)
	for {
		for _, got := range notifier.received() {
			if got == want {
				return
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("运行实例没有执行显示请求；收到的通知 = %v", notifier.received())
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// TestRequestPresentWithoutOwnerFails 覆盖运行实例没有唤回入口的情形（旧版本切换器）：
// 请求必须失败并带上原因。静默成功会让用户以为请求生效，而窗口实际不会出现。
func TestRequestPresentWithoutOwnerFails(t *testing.T) {
	startPrivateBus(t)
	if err := RequestPresent(testMutexName); err == nil {
		t.Fatal("没有实例持有单实例名时请求应失败")
	}
}
