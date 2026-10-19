// 前端事件落盘的契约测试：一行一事件、凭据不入盘、超限轮转、并发安全。
// 这些断言之所以重要，是因为 launcher.log 是排查"闪屏/白屏"类偶发问题的唯一界面侧
// 证据：格式被破坏或 token 漏进日志，都会让这条通道失去价值（或直接造成泄露）。
package app

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// readLog 读取测试目录下的 launcher.log。
func readLog(t *testing.T, dir string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(dir, uiLogFileName))
	if err != nil {
		t.Fatalf("读取 launcher.log 失败: %v", err)
	}
	return string(raw)
}

// TestAppendUiEventLog_WritesTimestampedLine 一行事件带时间戳与 [ui] 标记，格式与
// harness.log 一致，便于按时间对齐。
func TestAppendUiEventLog_WritesTimestampedLine(t *testing.T) {
	dir := t.TempDir()
	if err := appendUiEventLog(dir, "reload", "reason=watchdog budget=1/2"); err != nil {
		t.Fatalf("写入失败: %v", err)
	}

	lines := strings.Split(strings.TrimSuffix(readLog(t, dir), "\n"), "\n")
	if len(lines) != 1 {
		t.Fatalf("应只写入一行,实得 %d 行: %q", len(lines), lines)
	}
	if !strings.Contains(lines[0], "[ui] reload reason=watchdog budget=1/2") {
		t.Fatalf("事件内容不符: %q", lines[0])
	}
	// 时间戳形如 [2026-10-19 14:03:59.123]，与 harness.log 的格式一致。
	if !strings.HasPrefix(lines[0], "[20") || !strings.Contains(lines[0][:23], ".") {
		t.Fatalf("时间戳格式不符: %q", lines[0])
	}
}

// TestAppendUiEventLog_RedactsToken 凭据类查询串必须被抹掉：iframe 地址带 ?token=…，
// 漏进日志就等于把凭据写到了磁盘上。脱敏在写入侧强制生效，不依赖前端。
func TestAppendUiEventLog_RedactsToken(t *testing.T) {
	dir := t.TempDir()
	const secret = "http://127.0.0.1:36943/?token=v_HIdOn1QoPRmiyGHDtdr1EpB-6BPytCWBr"
	if err := appendUiEventLog(dir, "target-changed", "next="+secret); err != nil {
		t.Fatalf("写入失败: %v", err)
	}

	got := readLog(t, dir)
	if strings.Contains(got, "v_HIdOn1QoPRmiyGHDtdr1EpB") {
		t.Fatalf("token 不得落盘: %q", got)
	}
	if !strings.Contains(got, "token=<redacted>") {
		t.Fatalf("应留下脱敏标记: %q", got)
	}
}

// TestAppendUiEventLog_FlattensAndTruncates 多行内容压成一行并截断：事件日志按行解析，
// 多行 detail 会破坏与 harness.log 的时间戳对齐。
func TestAppendUiEventLog_FlattensAndTruncates(t *testing.T) {
	dir := t.TempDir()
	long := strings.Repeat("x", uiLogMaxDetail+50)
	if err := appendUiEventLog(dir, "bridge-stall", "第一行\n第二行\r\n"+long); err != nil {
		t.Fatalf("写入失败: %v", err)
	}

	lines := strings.Split(strings.TrimSuffix(readLog(t, dir), "\n"), "\n")
	if len(lines) != 1 {
		t.Fatalf("多行 detail 必须压成一行,实得 %d 行", len(lines))
	}
	if strings.ContainsAny(lines[0], "\r\n") {
		t.Fatalf("不得残留换行: %q", lines[0])
	}
	if !strings.HasSuffix(lines[0], "…") {
		t.Fatalf("超长 detail 应被截断并留下省略号: %q", lines[0])
	}
	// 只断言 detail 部分：时间戳与事件名不属于 detail 预算。
	const marker = "bridge-stall "
	detail := lines[0][strings.Index(lines[0], marker)+len(marker):]
	if n := len([]rune(detail)); n > uiLogMaxDetail+1 {
		t.Fatalf("detail 截断后仍超限: %d", n)
	}
}

// TestAppendUiEventLog_RotatesAtLimit 超过上限时轮转为 .1，新文件只留最新事件：
// 诊断日志不允许无限增长。
func TestAppendUiEventLog_RotatesAtLimit(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, uiLogFileName)
	if err := os.WriteFile(path, []byte(strings.Repeat("y", uiLogMaxBytes)), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := appendUiEventLog(dir, "reload", "reason=iframe-error budget=2/2"); err != nil {
		t.Fatalf("写入失败: %v", err)
	}

	if _, err := os.Stat(path + ".1"); err != nil {
		t.Fatalf("超过上限应轮转为 .1: %v", err)
	}
	got := readLog(t, dir)
	if strings.Contains(got, "yyyy") {
		t.Fatalf("轮转后新文件不应保留旧内容: %q", got)
	}
	if !strings.Contains(got, "reason=iframe-error") {
		t.Fatalf("轮转后应写入新事件: %q", got)
	}
}

// TestAppendUiEventLog_ConcurrentWritesKeepOneLinePerEvent 并发写入不丢事件、不串行：
// 前端可能从一个 tick 里连续上报多条。
func TestAppendUiEventLog_ConcurrentWritesKeepOneLinePerEvent(t *testing.T) {
	dir := t.TempDir()
	const writers, perWriter = 8, 5

	var wg sync.WaitGroup
	for i := 0; i < writers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < perWriter; j++ {
				_ = appendUiEventLog(dir, "stage", "loading-page→harness")
			}
		}()
	}
	wg.Wait()

	lines := strings.Split(strings.TrimSuffix(readLog(t, dir), "\n"), "\n")
	if want := writers * perWriter; len(lines) != want {
		t.Fatalf("事件数不符: 期望 %d, 实得 %d", want, len(lines))
	}
	for _, line := range lines {
		if strings.Count(line, "[ui]") != 1 {
			t.Fatalf("单行只应含一条事件: %q", line)
		}
	}
}

// TestLogUiEvent_UsesRuntimeDir 绑定方法写到 appenv.RuntimeDir()，因此
// DSH_DESKTOP_LOG_DIR 能把它重定向到临时目录——与 harness.log 共用同一处解析。
func TestLogUiEvent_UsesRuntimeDir(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("DSH_DESKTOP_LOG_DIR", dir)

	if err := (&App{}).LogUiEvent("mounted", ""); err != nil {
		t.Fatalf("写入失败: %v", err)
	}
	if !strings.Contains(readLog(t, dir), "[ui] mounted") {
		t.Fatalf("应写入 RuntimeDir 下的 launcher.log")
	}
}
