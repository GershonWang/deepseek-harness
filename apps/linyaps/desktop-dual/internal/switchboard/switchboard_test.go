package switchboard

import (
	"errors"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// TestNormalizeKeepsKnownModes 覆盖形态合法性：只有两种客户端形态被承认，
// 其余（含空串，即配置缺字段）一律回退薄壳版——切换器不能去启动一个不存在的形态。
// 这里传空的可用形态：探测不到包内客户端时只做合法性收敛，保留用户的选择。
func TestNormalizeKeepsKnownModes(t *testing.T) {
	cases := map[Mode]Mode{
		ModeShell:    ModeShell,
		ModeOfficial: ModeOfficial,
		"":           ModeShell,
		"wat":        ModeShell,
	}
	for input, want := range cases {
		if got := normalize(Config{DefaultMode: input}, nil).DefaultMode; got != want {
			t.Errorf("normalize(%q) = %q，期望 %q", input, got, want)
		}
	}
}

// TestNormalizeFallsBackToShippedMode 覆盖跨包保留的配置：配置文件住在宿主家目录，
// 用户可能先装过含官方客户端的包、再换成只含薄壳的包。旧的 defaultMode=official 必须
// 回退到包内确实存在的形态——单客户端形态的菜单里没有「默认形态」入口可以改回来，
// 这里是唯一能自愈的地方。
func TestNormalizeFallsBackToShippedMode(t *testing.T) {
	if got := normalize(Config{DefaultMode: ModeOfficial}, []Mode{ModeShell}).DefaultMode; got != ModeShell {
		t.Fatalf("默认形态不在包内时应回退薄壳版，得到 %q", got)
	}
	if got := normalize(Config{DefaultMode: ModeShell}, []Mode{ModeOfficial}).DefaultMode; got != ModeOfficial {
		t.Fatalf("应回退到包内实际随带的形态，得到 %q", got)
	}
	// 一个客户端都探测不到（开发态或安装异常）时不改写配置：启动失败会带着路径报出来，
	// 比在这里猜一个形态更好诊断。
	if got := normalize(Config{DefaultMode: ModeOfficial}, nil).DefaultMode; got != ModeOfficial {
		t.Fatalf("探测不到形态时不应改写配置，得到 %q", got)
	}
}

// TestAvailableModesReportsShippedClients 覆盖「包里随带了哪些客户端」的探测：
// 托盘菜单结构由它决定，判据必须与启动客户端时的解析规则一致——两处各写一份 os.Stat
// 迟早会漂移，而漂移的结果是菜单给出一个点了必然失败的入口。
func TestAvailableModesReportsShippedClients(t *testing.T) {
	prefix := t.TempDir()
	bin := filepath.Join(prefix, "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatalf("准备目录失败: %v", err)
	}
	exe := filepath.Join(bin, "dsh-desktop-switcher")

	if got := availableModes(exe); len(got) != 0 {
		t.Fatalf("两个客户端都不在时不应报告可用形态，得到 %q", got)
	}

	// 只装薄壳：不含 Electron 的那个包。
	if err := os.WriteFile(filepath.Join(bin, shellExecutable), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatalf("准备薄壳客户端失败: %v", err)
	}
	if got := availableModes(exe); !slices.Equal(got, []Mode{ModeShell}) {
		t.Fatalf("只随带薄壳时应报告 [shell]，得到 %q", got)
	}

	// 再补上官方客户端：双客户端包。薄壳仍排在前，即菜单优先级。
	electron := filepath.Join(prefix, "electron")
	if err := os.MkdirAll(electron, 0o755); err != nil {
		t.Fatalf("准备目录失败: %v", err)
	}
	if err := os.WriteFile(filepath.Join(electron, officialExecutable), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatalf("准备官方客户端失败: %v", err)
	}
	if got := availableModes(exe); !slices.Equal(got, []Mode{ModeShell, ModeOfficial}) {
		t.Fatalf("两个客户端都在时应报告 [shell official]，得到 %q", got)
	}
}

// TestStatusSwitchableFollowsShippedClients 覆盖对外事实：托盘据此决定菜单给不给
// 「切换」与「默认启动」。
func TestStatusSwitchableFollowsShippedClients(t *testing.T) {
	home := t.TempDir()
	if newSwitchboard(home, []Mode{ModeShell}).Status().Switchable {
		t.Fatal("只随带一个客户端时不应报告可切换")
	}
	if !newSwitchboard(home, []Mode{ModeShell, ModeOfficial}).Status().Switchable {
		t.Fatal("两个客户端都在时应报告可切换")
	}
}

// TestConfigRoundTrip 覆盖默认形态的持久化：切换器每次启动都是新进程，
// 用户的选择只能靠配置文件跨启动保留。
func TestConfigRoundTrip(t *testing.T) {
	home := t.TempDir()
	// 显式声明两个形态都在包里：本用例断言的是持久化本身，不该依赖探测结果——
	// 探测在测试环境里恒为空，会把「形态是否随包」这层语义混进来。
	both := []Mode{ModeShell, ModeOfficial}
	board := newSwitchboard(home, both)
	if err := board.SetDefault(ModeOfficial); err != nil {
		t.Fatalf("写入默认形态失败: %v", err)
	}

	reopened := newSwitchboard(home, both)
	if got := reopened.Status().DefaultMode; got != ModeOfficial {
		t.Fatalf("默认形态未持久化: %q", got)
	}
	if want := filepath.Join(home, ".config", "dsh-desktop", "switch.json"); reopened.ConfigPath() != want {
		t.Fatalf("配置路径 = %q，期望 %q", reopened.ConfigPath(), want)
	}
}

// TestConfigIgnoresLegacyBubbleFields 覆盖从悬浮球形态升级：旧配置里的悬浮球坐标
// 必须被忽略而不是让解码失败——否则升级后用户会莫名其妙退回默认形态。
func TestConfigIgnoresLegacyBubbleFields(t *testing.T) {
	home := t.TempDir()
	path := filepath.Join(home, ".config", "dsh-desktop", "switch.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("准备配置目录失败: %v", err)
	}
	legacy := []byte("{\n  \"defaultMode\": \"official\",\n  \"bubbleX\": 120,\n  \"bubbleY\": 240,\n  \"bubbleSet\": true\n}\n")
	if err := os.WriteFile(path, legacy, 0o644); err != nil {
		t.Fatalf("写入旧配置失败: %v", err)
	}

	// 两个形态都随包，因此旧配置里的 official 是可用值，必须原样保留。
	if got := newSwitchboard(home, []Mode{ModeShell, ModeOfficial}).Status().DefaultMode; got != ModeOfficial {
		t.Fatalf("旧配置的默认形态应保留，得到 %q", got)
	}
}

// TestCorruptConfigFallsBack 覆盖配置损坏：读不动就回默认值，不拦启动。
// 切换器不可用比配置丢失严重得多。
func TestCorruptConfigFallsBack(t *testing.T) {
	home := t.TempDir()
	board := New(home)
	if err := board.SetDefault(ModeOfficial); err != nil {
		t.Fatalf("准备配置失败: %v", err)
	}
	if err := os.WriteFile(board.ConfigPath(), []byte("{ 不是 JSON"), 0o644); err != nil {
		t.Fatalf("写入损坏配置失败: %v", err)
	}
	if got := New(home).Status().DefaultMode; got != ModeShell {
		t.Fatalf("损坏配置应回退薄壳版，得到 %q", got)
	}
}

// TestSetDefaultRejectsUnknownMode 覆盖越界入参：形态来自菜单回调，
// 这里必须自己把关，否则会把垃圾写进配置。
func TestSetDefaultRejectsUnknownMode(t *testing.T) {
	if err := New(t.TempDir()).SetDefault("wat"); err == nil {
		t.Fatal("未知形态应被拒绝")
	}
}

// TestStatusWithoutClient 覆盖「没有客户端在跑」的初始快照：
// 此时 Mode 为空串，托盘据此显示未运行态。
func TestStatusWithoutClient(t *testing.T) {
	status := New(t.TempDir()).Status()
	if status.Mode != "" {
		t.Fatalf("初始 Mode 应为空串，得到 %q", status.Mode)
	}
	if status.DefaultMode != ModeShell {
		t.Fatalf("初始默认形态应为薄壳版，得到 %q", status.DefaultMode)
	}
	if status.Switching || status.Error != "" {
		t.Fatalf("初始不应处于切换中或带错误: %+v", status)
	}
}

// TestShutdownWithoutClientIsSafe 覆盖幂等：没有子进程时停止不应 panic，
// 托盘退出与信号两路都会调到它。
func TestShutdownWithoutClientIsSafe(t *testing.T) {
	board := New(t.TempDir())
	board.Shutdown()
	board.Shutdown()
}

// TestRunningLockedUsesExitedChannel 覆盖「是否真在跑」的判据：进程结束后 reap 要先关
// exited、再抢锁复位字段，两者之间 s.child 仍非 nil。只看 s.child 会把一个僵尸当成在跑，
// 于是 Present 走「唤回」分支，真的再起一个实例却不进入状态跟踪。
func TestRunningLockedUsesExitedChannel(t *testing.T) {
	board := New(t.TempDir())
	if board.runningLocked() {
		t.Fatal("没有子进程时不应算在跑")
	}
	exited := make(chan struct{})
	board.child = &exec.Cmd{}
	board.exited = exited
	if !board.runningLocked() {
		t.Fatal("子进程仍在运行时应算在跑")
	}
	close(exited)
	if board.runningLocked() {
		t.Fatal("子进程已结束（exited 已关闭）时不应算在跑")
	}
}

// TestPresentStartsDefaultClientWhenNoneRuns 覆盖「客户端关闭后点击显示」的主场景：
// 没有客户端时按默认形态启动。这里用缺失的安装布局触发解析错误，从而断言它确实走了
// 启动分支（错误里是薄壳客户端的路径），而不是唤回分支。
func TestPresentStartsDefaultClientWhenNoneRuns(t *testing.T) {
	err := New(t.TempDir()).Present()
	if err == nil {
		t.Fatal("客户端缺失时应报错")
	}
	if !strings.Contains(err.Error(), shellExecutable) {
		t.Fatalf("应尝试启动默认形态的薄壳客户端，错误为 %v", err)
	}
}

// TestPresentFocusesRunningOfficialClient 覆盖官方版「关窗只是隐藏」的情形：客户端仍在
// 运行时不再启动新进程，而是走唤回分支（错误里是官方客户端的路径），且不得改动正在跟踪
// 的那个子进程。
func TestPresentFocusesRunningOfficialClient(t *testing.T) {
	board := New(t.TempDir())
	board.child = &exec.Cmd{}
	board.exited = make(chan struct{})
	board.current = ModeOfficial

	err := board.Present()
	if err == nil {
		t.Fatal("官方客户端缺失时应报错")
	}
	if !strings.Contains(err.Error(), officialExecutable) {
		t.Fatalf("应走唤回官方客户端的分支，错误为 %v", err)
	}
	if board.current != ModeOfficial || board.child == nil {
		t.Fatal("唤回不应改动正在跟踪的客户端状态")
	}
}

// TestClientCommandRejectsUnknownMode 覆盖未知形态不会退化成「启动自己」：
// 返回错误而不是一个看似可用的命令。
func TestClientCommandRejectsUnknownMode(t *testing.T) {
	if _, _, err := clientCommand("/opt/apps/x/files/bin/dsh-desktop-switcher", "wat"); err == nil {
		t.Fatal("未知形态应返回错误")
	}
}

// TestClientCommandShellIsSiblingBinary 覆盖薄壳客户端的解析：它就是与切换器同目录的
// dsh-desktop-launcher，且不带参数——客户端形态由二进制本身决定，不再是启动参数。
func TestClientCommandShellIsSiblingBinary(t *testing.T) {
	prefix := t.TempDir()
	bin := filepath.Join(prefix, "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatalf("准备目录失败: %v", err)
	}
	shell := filepath.Join(bin, shellExecutable)
	if err := os.WriteFile(shell, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatalf("准备薄壳客户端失败: %v", err)
	}

	path, args, err := clientCommand(filepath.Join(bin, "dsh-desktop-switcher"), ModeShell)
	if err != nil {
		t.Fatalf("解析薄壳客户端失败: %v", err)
	}
	if path != shell {
		t.Fatalf("薄壳客户端路径 = %q，期望 %q", path, shell)
	}
	if len(args) != 0 {
		t.Fatalf("薄壳客户端不应带参数，得到 %q", args)
	}
}

// TestClientCommandOfficialUsesPackageLayout 覆盖官方客户端的解析：electron/ 与 bin/
// 同在 <PREFIX> 下，且必须带 --no-sandbox（玲珑容器内 Chromium 沙箱不可用）。
func TestClientCommandOfficialUsesPackageLayout(t *testing.T) {
	prefix := t.TempDir()
	bin := filepath.Join(prefix, "bin")
	electron := filepath.Join(prefix, "electron")
	for _, dir := range []string{bin, electron} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("准备目录失败: %v", err)
		}
	}
	official := filepath.Join(electron, officialExecutable)
	if err := os.WriteFile(official, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatalf("准备官方客户端失败: %v", err)
	}

	path, args, err := clientCommand(filepath.Join(bin, "dsh-desktop-switcher"), ModeOfficial)
	if err != nil {
		t.Fatalf("解析官方客户端失败: %v", err)
	}
	if path != official {
		t.Fatalf("官方客户端路径 = %q，期望 %q", path, official)
	}
	if len(args) != 1 || args[0] != "--no-sandbox" {
		t.Fatalf("官方客户端参数 = %q，期望 --no-sandbox", args)
	}
}

// TestClientCommandReportsMissingClients 覆盖客户端缺失：能安装却点不动的包比构建期
// 报错难查得多，这里必须给出带路径的错误而不是一个空命令。
func TestClientCommandReportsMissingClients(t *testing.T) {
	exe := filepath.Join(t.TempDir(), "bin", "dsh-desktop-switcher")
	for _, mode := range []Mode{ModeShell, ModeOfficial} {
		path, _, err := clientCommand(exe, mode)
		if err == nil {
			t.Fatalf("%s 形态在客户端缺失时应报错，却返回 %q", mode, path)
		}
	}
}

// TestStopGraceCoversLauncherTeardown 钉死终止宽限期的下界。
//
// stopGrace 是按启动器自身有界的收尾时间推导出来的（Supervisor.Stop() 最长 10s、
// stopDoctor() 最长 2s；terminal.CloseAll() 每会话最长 5s、串行且会话数不定，上界不可
// 预估，所以只钉得下界）。调小它就会在启动器收尾中途强杀，留下占着 dsh 端口的孤儿
// harness——表现为「退出不干净 / ll-cli ps 里还有进程」，且没有任何报错或日志。这个用例
// 让调小必须回 switchboard.go 的 stopGrace 注释重新推导一次启动器侧的上界。
func TestStopGraceCoversLauncherTeardown(t *testing.T) {
	const minStopGrace = 20 * time.Second
	if stopGrace < minStopGrace {
		t.Fatalf("stopGrace = %v，低于下界 %v；调小必须重新推导启动器收尾上界（见 switchboard.go 中 stopGrace 的说明）",
			stopGrace, minStopGrace)
	}
}

// TestHideOnCloseEnvNameIsPinned 钉死环境变量名。
//
// 名字在 desktop-launcher 的 internal/app 里另有一份字面量（两个模块互相无法 import），
// 改一边而另一边没跟上的表现是「托盘点了没反应」——既没有报错也没有日志。这个用例让改名
// 必须同时改两处，且各自模块的测试都会失败一次。
func TestHideOnCloseEnvNameIsPinned(t *testing.T) {
	if hideOnCloseEnv != "DSH_DESKTOP_HIDE_ON_CLOSE" {
		t.Fatalf("环境变量名 = %q，期望 %q；改名必须同步 desktop-launcher 的 HideOnCloseEnv",
			hideOnCloseEnv, "DSH_DESKTOP_HIDE_ON_CLOSE")
	}
}

// TestClientEnvEnablesHideOnCloseForTheShellOnly 覆盖环境注入：只有薄壳客户端需要被告知
// 「关窗只隐藏」（托盘是隐藏之后唯一能把它叫回来的入口）；官方客户端自带 Electron 的
// 关窗隐藏语义，多带一份用不上的变量只会让排查时多一个干扰项。
func TestClientEnvEnablesHideOnCloseForTheShellOnly(t *testing.T) {
	if shell := clientEnv(ModeShell); !slices.Contains(shell, hideOnCloseEnv+"=1") {
		t.Fatalf("薄壳客户端的环境里应含 %s=1，实际 %v", hideOnCloseEnv, shell)
	}
	if official := clientEnv(ModeOfficial); official != nil {
		t.Fatalf("官方客户端应继承本进程环境（nil），实际 %v", official)
	}
}

// runningShell 造一个「薄壳客户端正在运行」的切换器状态，Present 的唤回分支据此进入。
// @param home - 用户主目录，唤回 socket 的绝对路径由它推出。
// @returns 已处于运行态的切换器。
func runningShell(home string) *Switchboard {
	board := &Switchboard{home: home}
	board.child = &exec.Cmd{Process: &os.Process{Pid: 4242}}
	board.exited = make(chan struct{})
	board.current = ModeShell
	return board
}

// TestPresentDialsARunningShellToShowItself 覆盖「左键/显示」对薄壳客户端的落点。
//
// 它在由切换器拉起时关窗只隐藏、进程仍在，因此这里不能再像以前那样空操作返回——那样
// 用户点「显示」不会有任何反应。走唤回 socket 而不是再启动一次：薄壳客户端没有单实例锁，
// 再启动会真的多出一个进程去抢同一个 dsh 端口与 ~/.dsh 状态。本用例因此同时守着
// 「不重复启动」：唤回之后跟踪的子进程必须还是原来那一个。
//
// 拨号路径必须正好由主目录推出：它和 desktop-launcher 侧的监听路径互为镜像，错开的表现
// 是「点显示没反应」，没有任何报错可查。
func TestPresentDialsARunningShellToShowItself(t *testing.T) {
	home := t.TempDir()
	board := runningShell(home)
	var gotPath string
	defer stubDialShow(func(path string) error {
		gotPath = path
		return nil
	})()

	if err := board.Present(); err != nil {
		t.Fatalf("唤回薄壳客户端失败: %v", err)
	}
	if want := filepath.Join(home, ".cache", "dsh-desktop", "show.sock"); gotPath != want {
		t.Fatalf("唤回路径 = %q，期望 %q", gotPath, want)
	}
	if board.current != ModeShell || board.child == nil {
		t.Fatal("唤回不应改动正在跟踪的客户端状态")
	}
}

// TestPresentReportsShowDialFailure 覆盖拨号失败：不能静默当作已唤回，否则用户点了
// 「显示」既没反应也没提示。
func TestPresentReportsShowDialFailure(t *testing.T) {
	board := runningShell(t.TempDir())
	defer stubDialShow(func(string) error {
		return errors.New("connection refused")
	})()

	if err := board.Present(); err == nil {
		t.Fatal("拨号失败时应返回错误")
	}
}

// TestPresentDoesNotLaunchOnShowDialFailure 覆盖关键回归：唤回失败绝不允许回退成
// 「再启动一个客户端」。
//
// 薄壳客户端没有单实例锁，回退会真的多出一个进程去抢同一个 dsh 端口与 ~/.dsh 状态，
// 也就是用户看到的「点一次显示就叠出第二个 harness」；本用例钉住原跟踪的子进程在失败后
// 仍是同一个，而不是被 launchLocked 换成了新进程。
func TestPresentDoesNotLaunchOnShowDialFailure(t *testing.T) {
	board := runningShell(t.TempDir())
	tracked := board.child
	defer stubDialShow(func(string) error {
		return errors.New("connection refused")
	})()

	err := board.Present()
	if err == nil {
		t.Fatal("拨号失败时应返回错误")
	}
	if board.child != tracked {
		t.Fatal("唤回失败不得改为启动新客户端：跟踪的子进程被换掉了")
	}
	if board.current != ModeShell {
		t.Fatalf("唤回失败不得改动当前形态，得到 %q", board.current)
	}
	if !strings.Contains(err.Error(), showSocketRel) {
		t.Fatalf("错误应带上是哪条唤回通道出的问题，得到 %v", err)
	}
}

// TestShowSocketPathIsPinned 钉死唤回 socket 的相对路径。
//
// 路径在 desktop-launcher 侧另有一份字面量（两个模块互相无法 import），改一边而另一边
// 没跟上的表现是「托盘点了显示没反应」——既没有报错也没有日志。这个用例让改名必须同时改
// 两处，且各自模块的测试都会失败一次。
func TestShowSocketPathIsPinned(t *testing.T) {
	if showSocketRel != ".cache/dsh-desktop/show.sock" {
		t.Fatalf("唤回 socket 路径 = %q，期望 %q；改名必须同步 desktop-launcher 的监听路径",
			showSocketRel, ".cache/dsh-desktop/show.sock")
	}
}

// TestDialShowSocketWritesShowPayload 覆盖默认拨号实现的协议：真起一个 Unix socket
// 监听端，断言它收到约定的请求字节。服务端按「收到连接」触发唤回，因此连接本身不能拨错
// 地址，写入也不能是空字节——两者都会让客户端看不到请求。
func TestDialShowSocketWritesShowPayload(t *testing.T) {
	// 用短路径建目录：t.TempDir() 的名字带用例名，容易顶到 Unix socket 路径长度上限。
	dir, err := os.MkdirTemp("", "show")
	if err != nil {
		t.Fatalf("准备临时目录失败: %v", err)
	}
	defer func() { _ = os.RemoveAll(dir) }()
	path := filepath.Join(dir, "show.sock")
	listener, err := net.Listen("unix", path)
	if err != nil {
		t.Fatalf("监听唤回 socket 失败: %v", err)
	}
	defer func() { _ = listener.Close() }()

	received := make(chan string, 1)
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			received <- ""
			return
		}
		defer conn.Close()
		buf := make([]byte, len(showPayload))
		if _, err := io.ReadFull(conn, buf); err != nil {
			received <- ""
			return
		}
		received <- string(buf)
	}()

	if err := dialShowSocket(path); err != nil {
		t.Fatalf("拨号唤回 socket 失败: %v", err)
	}
	select {
	case payload := <-received:
		if payload != showPayload {
			t.Fatalf("唤回请求 = %q，期望 %q", payload, showPayload)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("监听端未在限时内收到唤回请求")
	}
}

// TestShowShellWithoutChildIsNoOp 覆盖子进程刚被复位的那一瞬：没有可唤回的进程时空操作，
// 而不是给用户一条没有可操作内容的失败通知。
func TestShowShellWithoutChildIsNoOp(t *testing.T) {
	defer stubDialShow(func(string) error {
		t.Fatal("没有子进程时不应拨唤回 socket")
		return nil
	})()

	if err := (&Switchboard{}).showShellLocked(); err != nil {
		t.Fatalf("没有子进程时应为空操作，得到 %v", err)
	}
}

// stubDialShow 替换拨号唤回的实现。
// @param fn - 替身实现。
// @returns 还原函数。
func stubDialShow(fn func(path string) error) func() {
	prev := dialShow
	dialShow = fn
	return func() { dialShow = prev }
}
