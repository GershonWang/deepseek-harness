package switchboard

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
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

// TestPresentIsNoOpForRunningShellClient 覆盖薄壳版：它关窗即退出，运行中说明窗口就在
// 桌面上，没有可唤回的状态；此时再启动一个会撞 dsh 端口，因此必须是空操作。
func TestPresentIsNoOpForRunningShellClient(t *testing.T) {
	board := New(t.TempDir())
	board.child = &exec.Cmd{}
	board.exited = make(chan struct{})
	board.current = ModeShell

	if err := board.Present(); err != nil {
		t.Fatalf("薄壳版运行中 Present 应为空操作，得到 %v", err)
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
