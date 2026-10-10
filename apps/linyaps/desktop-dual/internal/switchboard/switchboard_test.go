package switchboard

import (
	"os"
	"path/filepath"
	"testing"
)

// TestNormalizeKeepsKnownModes 覆盖形态合法性：只有两种客户端形态被承认，
// 其余（含空串，即配置缺字段）一律回退薄壳版——切换器不能去启动一个不存在的形态。
func TestNormalizeKeepsKnownModes(t *testing.T) {
	cases := map[Mode]Mode{
		ModeShell:    ModeShell,
		ModeOfficial: ModeOfficial,
		"":           ModeShell,
		"wat":        ModeShell,
	}
	for input, want := range cases {
		if got := normalize(Config{DefaultMode: input}).DefaultMode; got != want {
			t.Errorf("normalize(%q) = %q，期望 %q", input, got, want)
		}
	}
}

// TestConfigRoundTrip 覆盖默认形态的持久化：切换器每次启动都是新进程，
// 用户的选择只能靠配置文件跨启动保留。
func TestConfigRoundTrip(t *testing.T) {
	home := t.TempDir()
	board := New(home)
	if err := board.SetDefault(ModeOfficial); err != nil {
		t.Fatalf("写入默认形态失败: %v", err)
	}

	reopened := New(home)
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

	if got := New(home).Status().DefaultMode; got != ModeOfficial {
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
