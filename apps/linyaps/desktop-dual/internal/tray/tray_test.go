// 托盘项注册生命周期的测试：覆盖「宿主重启后图标自己回来」这条真实失效路径。
//
// 这些用例需要一个会话总线，因此默认跳过，用私有总线显式运行：
//
//	dbus-run-session -- go test ./internal/tray
//
// 桩宿主与切换器客户端都在测试进程内，宿主「重启」就是换一条总线连接——与真实宿主的
// 崩溃重启等价：唯一名变了、已注册项丢了，而 SNI 不会通知客户端。
package tray

import (
	"errors"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/godbus/dbus/v5"

	"github.com/deepseek-ai/deepseek-harness/apps/linyaps/desktop-dual/internal/i18n"
	"github.com/deepseek-ai/deepseek-harness/apps/linyaps/desktop-dual/internal/switchboard"
)

// testMutexName 是测试客户端的单实例名，与真实入口区分开，避免在同一台机器上误判。
const testMutexName = "org.deepseek.dsh.DesktopDualSwitcherTest"

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

// startFakeWatcher 拉起一个桩宿主。
//
// SessionBusPrivate 给的是「还没握手」的连接：必须先 Auth 与 Hello，否则总线不认这个
// 客户端，发出去的 RequestName 永远等不到回复。申请名字还会一直等到上一个宿主的连接被
// 总线回收为止，因为总线处理断开是异步的；这段等待也正是真实宿主重启时新宿主必然要经历
// 的窗口。
// @returns 已就绪的桩宿主。
func startFakeWatcher(t *testing.T) *fakeWatcher {
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
	if os.Getenv("DBUS_SESSION_BUS_ADDRESS") == "" {
		t.Skip("需要会话总线，请用 dbus-run-session 运行本用例")
	}

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
