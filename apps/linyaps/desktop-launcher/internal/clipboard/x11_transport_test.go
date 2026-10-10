package clipboard

import (
	"errors"
	"net"
	"strconv"
	"strings"
	"testing"
	"time"
)

// localTransports 是本地会话（无主机名）的期望候选顺序：抽象 socket → 文件
// socket → TCP，三者都指向同一个 display number。
func localTransports(n int) []x11Transport {
	return []x11Transport{
		{"unix", "\x00/tmp/.X11-unix/X" + strconv.Itoa(n)},
		{"unix", "/tmp/.X11-unix/X" + strconv.Itoa(n)},
		{"tcp", "127.0.0.1:" + strconv.Itoa(6000+n)},
	}
}

// TestX11Transports 钉住 DISPLAY → 候选路径的推导。
//
// display number 必须来自环境：X11 会话通常是 :0，Wayland 会话经 XWayland 通常
// 是 :1。写死 X0 会让 Wayland 会话连到无关的 X server 上，认证阶段才失败——即便
// X1 就在旁边可用，而那条路径根本不会被走到。
func TestX11Transports(t *testing.T) {
	cases := []struct {
		name    string
		display string
		want    []x11Transport
	}{
		{"X11 会话", ":0", localTransports(0)},
		{"Wayland 会话经 XWayland", ":1", localTransports(1)},
		{"带 screen 号", ":1.0", localTransports(1)},
		{"unix 协议前缀", "unix:2", localTransports(2)},
		{"local 主机名", "local:3", localTransports(3)},
		{"远端主机走 TCP", "localhost:1", []x11Transport{{"tcp", "localhost:6001"}}},
		{"protocol/host 形式", "tcp/example.com:2", []x11Transport{{"tcp", "example.com:6002"}}},
		{"socket 绝对路径", "/tmp/.X11-unix/X3", []x11Transport{
			{"unix", "\x00/tmp/.X11-unix/X3"},
			{"unix", "/tmp/.X11-unix/X3"},
		}},
		{"无 DISPLAY", "", nil},
		{"缺少冒号", "garbage", nil},
		{"display 非数字", ":abc", nil},
		{"display 为负", ":-1", nil},
		{"display 为空", ":", nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := x11Transports(c.display)
			if len(got) != len(c.want) {
				t.Fatalf("x11Transports(%q) = %v，期望 %v", c.display, got, c.want)
			}
			for i := range got {
				if got[i] != c.want[i] {
					t.Fatalf("x11Transports(%q)[%d] = %v，期望 %v", c.display, i, got[i], c.want[i])
				}
			}
		})
	}
}

// TestConnectSocketFollowsDisplay 是本次修复的核心回归：connectSocket 必须按
// DISPLAY 选 socket。
//
// 修复前它写死 X0 与 TCP 6000；Wayland 会话（DISPLAY=:1）会连上无关的 X server，
// 随后在认证阶段被拒，而真正可用的 X1 永远不会被尝试。
func TestConnectSocketFollowsDisplay(t *testing.T) {
	t.Setenv("DISPLAY", ":1")

	var tried []string
	origDial := socketDial
	socketDial = func(network, addr string, timeout time.Duration) (net.Conn, error) {
		tried = append(tried, network+"|"+addr)
		return nil, errors.New("connection refused")
	}
	t.Cleanup(func() { socketDial = origDial })

	if _, err := connectSocket(); err == nil {
		t.Fatal("所有候选都连不上时 connectSocket 应返回错误")
	}
	want := "unix|\x00/tmp/.X11-unix/X1"
	if len(tried) == 0 || tried[0] != want {
		t.Fatalf("首个候选 = %v，期望 %q", tried, want)
	}
	for _, got := range tried {
		if strings.Contains(got, "/X0") || strings.HasSuffix(got, ":6000") {
			t.Fatalf("DISPLAY=:1 时不应尝试 display 0 的候选：%v", tried)
		}
	}
}

// TestConnectSocketNoDisplay 钉住无 DISPLAY 时不发起任何连接：纯 Wayland 会话
// （合成器不带 XWayland）下候选为空，必须立刻失败而不是凭空去连 :0。
func TestConnectSocketNoDisplay(t *testing.T) {
	t.Setenv("DISPLAY", "")

	called := false
	origDial := socketDial
	socketDial = func(network, addr string, timeout time.Duration) (net.Conn, error) {
		called = true
		return nil, errors.New("should not be called")
	}
	t.Cleanup(func() { socketDial = origDial })

	if _, err := connectSocket(); err == nil {
		t.Fatal("无候选时 connectSocket 应返回错误")
	}
	if called {
		t.Fatal("无 DISPLAY 时不应尝试任何 socket")
	}
}

// TestReadImageSkipsX11WithoutDisplay 钉住策略编排：没有 DISPLAY 时 ReadImage
// 不应触碰 X11 通道，否则纯 Wayland 会话会先白等几轮 socket 连接才轮到 wl-paste。
func TestReadImageSkipsX11WithoutDisplay(t *testing.T) {
	t.Setenv("DISPLAY", "")
	// 让 Wayland 兜底也立刻返回，使 ReadImage 只走完编排本身。
	t.Setenv("WAYLAND_DISPLAY", "")

	dialed := false
	orig := connectSocket
	connectSocket = func() (net.Conn, error) {
		dialed = true
		return nil, errors.New("should not be dialed")
	}
	t.Cleanup(func() { connectSocket = orig })

	if _, err := ReadImage(); err == nil {
		t.Fatal("两条通道都无内容时应返回 errSelectionEmpty")
	}
	if dialed {
		t.Fatal("无 DISPLAY 时 ReadImage 不应建立 X11 连接")
	}
}
