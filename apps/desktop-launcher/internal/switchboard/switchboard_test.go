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

// TestConfigRoundTrip 覆盖默认形态与悬浮球位置的持久化：切换器每次启动都是新进程，
// 用户的选择只能靠配置文件跨启动保留。
func TestConfigRoundTrip(t *testing.T) {
	home := t.TempDir()
	board := New(home)
	if err := board.SetDefault(ModeOfficial); err != nil {
		t.Fatalf("写入默认形态失败: %v", err)
	}
	board.SaveBubblePosition(120, 240)

	reopened := New(home)
	if got := reopened.Status().DefaultMode; got != ModeOfficial {
		t.Fatalf("默认形态未持久化: %q", got)
	}
	if pos := reopened.BubblePosition(); !pos.Set || pos.X != 120 || pos.Y != 240 {
		t.Fatalf("悬浮球位置未持久化: %+v", pos)
	}
	if want := filepath.Join(home, ".config", "dsh-desktop", "switch.json"); reopened.ConfigPath() != want {
		t.Fatalf("配置路径 = %q，期望 %q", reopened.ConfigPath(), want)
	}
}

// TestBubblePositionUnsetByDefault 覆盖「从未拖拽过」：此时 Set 必须为假，
// 否则启动会去还原一个并不存在的原点位置，把悬浮球钉在左上角。
func TestBubblePositionUnsetByDefault(t *testing.T) {
	if pos := New(t.TempDir()).BubblePosition(); pos.Set {
		t.Fatalf("初始不应有位置记录: %+v", pos)
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

// TestSetDefaultRejectsUnknownMode 覆盖越界入参：形态来自前端字符串，
// 这里必须自己把关，否则会把垃圾写进配置。
func TestSetDefaultRejectsUnknownMode(t *testing.T) {
	if err := New(t.TempDir()).SetDefault("wat"); err == nil {
		t.Fatal("未知形态应被拒绝")
	}
}

// TestStatusWithoutClient 覆盖「没有客户端在跑」的初始快照：
// 此时 Mode 为空串，前端据此显示未运行态。
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
// 窗口关闭与信号两路都会调到它。
func TestShutdownWithoutClientIsSafe(t *testing.T) {
	board := New(t.TempDir())
	board.Shutdown()
	board.Shutdown()
}

// TestClientCommandRejectsUnknownMode 覆盖未知形态不会退化成「启动自己」：
// 返回错误而不是一个看似可用的命令。
func TestClientCommandRejectsUnknownMode(t *testing.T) {
	if _, _, err := clientCommand("wat"); err == nil {
		t.Fatal("未知形态应返回错误")
	}
}

// TestClientCommandShellIsSelf 覆盖薄壳客户端的解析结果：它就是本二进制自身，
// 以 --mode=shell 运行。
func TestClientCommandShellIsSelf(t *testing.T) {
	path, args, err := clientCommand(ModeShell)
	if err != nil {
		t.Fatalf("解析薄壳客户端失败: %v", err)
	}
	if path == "" || len(args) != 1 || args[0] != "--mode=shell" {
		t.Fatalf("薄壳客户端命令不符: %q %q", path, args)
	}
}
