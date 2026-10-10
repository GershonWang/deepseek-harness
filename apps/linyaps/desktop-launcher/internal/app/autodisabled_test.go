// 自动禁用提示的读取与确认。
//
// 这些用例同时固定 doctor 与壳之间的跨进程约定：留痕文件的位置、字段名与"按包名
// 确认"的语义。doctor 侧写出这些字段（packages/support/doctor 的 auto-disabled.ts），
// 任何一边改动都会在这里先失败。

package app

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// writeLedger 写入 doctor 的留痕文件：路径 <home>/.dsh/doctor/auto-disabled.json。
func writeLedger(t *testing.T, home string, records []autoDisabledRecord) {
	t.Helper()
	dir := filepath.Join(home, ".dsh", "doctor")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(records)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "auto-disabled.json"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
}

// autoDisabledTestApp 构造只带 home 的 App：这两个绑定不碰 harness、不碰 supervisor。
// 已提示标记落在 DSH_DESKTOP_LOG_DIR 指向的临时目录，测试不写真实 ~/.cache。
func autoDisabledTestApp(t *testing.T) *App {
	t.Helper()
	t.Setenv("DSH_DESKTOP_LOG_DIR", t.TempDir())
	return &App{home: t.TempDir()}
}

func TestPendingAutoDisabled_ReportsLedgerRecords(t *testing.T) {
	a := autoDisabledTestApp(t)
	writeLedger(t, a.home, []autoDisabledRecord{
		{At: "2026-09-18T10:00:00.000Z", Bundle: "third-party-bad", Reason: "探测到该插件与当前版本不兼容（加载失败）", BackupDir: "/tmp/backups/doctor-1"},
	})

	notices := a.PendingAutoDisabled()

	if len(notices) != 1 {
		t.Fatalf("应返回一条待提示记录, got %+v", notices)
	}
	if notices[0].Bundle != "third-party-bad" {
		t.Errorf("包名应原样带出, got %q", notices[0].Bundle)
	}
	if notices[0].Reason == "" || notices[0].At == "" {
		t.Errorf("原因与时刻都应带出, got %+v", notices[0])
	}
}

func TestPendingAutoDisabled_DedupesByBundleKeepingNewest(t *testing.T) {
	a := autoDisabledTestApp(t)
	writeLedger(t, a.home, []autoDisabledRecord{
		{At: "2026-09-18T10:00:00.000Z", Bundle: "bad-a", Reason: "旧"},
		{At: "2026-09-18T11:00:00.000Z", Bundle: "bad-b", Reason: "另一个"},
		{At: "2026-09-18T12:00:00.000Z", Bundle: "bad-a", Reason: "新"},
	})

	notices := a.PendingAutoDisabled()

	// 同一插件被禁用过多轮只提示一次（最新的那条），另一插件不受影响。
	if len(notices) != 2 {
		t.Fatalf("应按包名去重, got %+v", notices)
	}
	if notices[0].Bundle != "bad-a" || notices[0].Reason != "新" {
		t.Errorf("应保留最新一条记录, got %+v", notices[0])
	}
	if notices[1].Bundle != "bad-b" {
		t.Errorf("第二个插件应保留, got %+v", notices[1])
	}

	// 确认后连被折叠掉的旧记录一起算作已提示，下一轮不再冒出来。
	if err := a.AckAutoDisabled([]string{"bad-a"}); err != "" {
		t.Fatalf("确认应成功, got %q", err)
	}
	rest := a.PendingAutoDisabled()
	if len(rest) != 1 || rest[0].Bundle != "bad-b" {
		t.Fatalf("确认 bad-a 后只剩 bad-b, got %+v", rest)
	}
}

func TestPendingAutoDisabled_ToleratesMissingOrCorruptLedger(t *testing.T) {
	a := autoDisabledTestApp(t)

	// 从未禁用过任何插件：留痕文件不存在。
	if notices := a.PendingAutoDisabled(); len(notices) != 0 {
		t.Fatalf("无留痕时应返回空, got %+v", notices)
	}

	// 留痕被外部改坏：放弃提示，不抛错、不阻断启动。
	dir := filepath.Join(a.home, ".dsh", "doctor")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "auto-disabled.json"), []byte("{ not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if notices := a.PendingAutoDisabled(); len(notices) != 0 {
		t.Fatalf("留痕损坏时应返回空, got %+v", notices)
	}
}

func TestAckAutoDisabled_OnlyAcknowledgesShownBundles(t *testing.T) {
	a := autoDisabledTestApp(t)
	writeLedger(t, a.home, []autoDisabledRecord{
		{At: "2026-09-18T10:00:00.000Z", Bundle: "shown", Reason: "已展示"},
		{At: "2026-09-18T11:00:00.000Z", Bundle: "later", Reason: "展示期间新增"},
	})

	// 前端只展示了 shown：later 必须留到下次启动再提示，否则用户永远看不到它。
	if err := a.AckAutoDisabled([]string{"shown"}); err != "" {
		t.Fatalf("确认应成功, got %q", err)
	}

	notices := a.PendingAutoDisabled()
	if len(notices) != 1 || notices[0].Bundle != "later" {
		t.Fatalf("未展示的插件应仍在待提示列表, got %+v", notices)
	}
}

func TestAckAutoDisabled_MirrorsLedgerKeys(t *testing.T) {
	a := autoDisabledTestApp(t)
	writeLedger(t, a.home, ledgerRecords(20))

	// 确认集合只镜像留痕里当前的键：留痕本身按 20 条封顶（doctor 侧），壳这边不另设
	// 上限，否则两边不一致时会把尚未展示的记录重新算成未提示。
	if err := a.AckAutoDisabled(bundlesOf(ledgerRecords(20))); err != "" {
		t.Fatalf("确认应成功, got %q", err)
	}
	keys := readAckKeys(t)
	if len(keys) != 20 {
		t.Fatalf("确认集合应镜像 20 条留痕, got %d", len(keys))
	}
	if notices := a.PendingAutoDisabled(); len(notices) != 0 {
		t.Fatalf("全部确认后不应再提示, got %+v", notices)
	}

	// 留痕被 doctor 裁剪（只留最后 5 条）后再确认：旧键随之从集合里消失，集合不会
	// 无限增长。
	trimmed := ledgerRecords(20)[15:]
	writeLedger(t, a.home, trimmed)
	if err := a.AckAutoDisabled(bundlesOf(trimmed)); err != "" {
		t.Fatalf("确认应成功, got %q", err)
	}
	if keys = readAckKeys(t); len(keys) != 5 {
		t.Fatalf("确认集合应随留痕裁剪, got %v", keys)
	}
	if keys[4] != "2026-09-18T10:00:00.000Z|plugin-19" {
		t.Errorf("应保留最新一条, got %q", keys[4])
	}
}

// ledgerRecords 造 n 条留痕（同一轮修复共享时刻，包名递增）。
func ledgerRecords(n int) []autoDisabledRecord {
	records := make([]autoDisabledRecord, 0, n)
	for i := 0; i < n; i++ {
		records = append(records, autoDisabledRecord{
			At:     "2026-09-18T10:00:00.000Z",
			Bundle: fmt.Sprintf("plugin-%02d", i),
			Reason: "不兼容",
		})
	}
	return records
}

// bundlesOf 取出留痕里的包名。
func bundlesOf(records []autoDisabledRecord) []string {
	bundles := make([]string, 0, len(records))
	for _, record := range records {
		bundles = append(bundles, record.Bundle)
	}
	return bundles
}

// readAckKeys 读回确认标记。
func readAckKeys(t *testing.T) []string {
	t.Helper()
	raw, err := os.ReadFile(ackFilePath())
	if err != nil {
		t.Fatalf("确认标记应落盘: %v", err)
	}
	var keys []string
	if err := json.Unmarshal(raw, &keys); err != nil {
		t.Fatalf("确认标记应为字符串数组: %v", err)
	}
	return keys
}
