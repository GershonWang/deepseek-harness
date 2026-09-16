package supervisor

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/deepseek-ai/deepseek-harness/apps/desktop-launcher/internal/domain"
)

func TestReadyRegex_Match(t *testing.T) {
	line := "dsh web: http://127.0.0.1:34567"
	match := readyPattern.FindStringSubmatch(line)
	if match == nil {
		t.Fatal("expected match")
	}
	if match[1] != "http://127.0.0.1:34567" {
		t.Errorf("expected http://127.0.0.1:34567, got %s", match[1])
	}
}

func TestReadyRegex_WithLAN(t *testing.T) {
	line := "dsh web: http://127.0.0.1:34567 (LAN: http://192.168.1.100:34567)"
	match := readyPattern.FindStringSubmatch(line)
	if match == nil {
		t.Fatal("expected match")
	}
	if match[1] != "http://127.0.0.1:34567" {
		t.Errorf("expected http://127.0.0.1:34567, got %s", match[1])
	}
}

func TestReadyRegex_WithToken(t *testing.T) {
	line := "dsh web: http://127.0.0.1:34567/?token=abc123XYZ"
	match := readyPattern.FindStringSubmatch(line)
	if match == nil {
		t.Fatal("expected match")
	}
	if match[1] != "http://127.0.0.1:34567/?token=abc123XYZ" {
		t.Errorf("expected URL with token, got %s", match[1])
	}
}

func TestReadyRegex_WithTokenAndLAN(t *testing.T) {
	line := "dsh web: http://127.0.0.1:34567/?token=abc123 (LAN: http://192.168.1.100:34567/?token=abc123)"
	match := readyPattern.FindStringSubmatch(line)
	if match == nil {
		t.Fatal("expected match")
	}
	if match[1] != "http://127.0.0.1:34567/?token=abc123" {
		t.Errorf("expected URL with token (without LAN suffix), got %s", match[1])
	}
}

func TestReadyRegex_NoMatch(t *testing.T) {
	lines := []string{
		"Hello world",
		"dsh: starting...",
		"[INFO] Server listening on port 3000",
	}
	for _, line := range lines {
		if readyPattern.MatchString(line) {
			t.Errorf("unexpected match for: %s", line)
		}
	}
}

func TestSupervisor_MockChild(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	cfg := Config{
		Command: "sh",
		Args:    []string{"testdata/mock-dsh-web.sh"},
		LogDir:  t.TempDir(),
	}
	sup := NewSupervisor(cfg, DefaultOptions())
	sup.Start()

	select {
	case url := <-sup.Ready():
		if url != "http://127.0.0.1:18080" {
			t.Errorf("expected http://127.0.0.1:18080, got %s", url)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for ready")
	}

	sup.Stop()
}

// 进度上报只在 stderr 上以固定前缀出现；解析结果与「已收到输出」的时刻一起决定
// 加载页给用户看什么。用真实子进程跑一遍，避免只测正则而漏掉 writer 接线。
func TestSupervisor_StartupProgressFromChild(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	cfg := Config{
		Command: "sh",
		Args:    []string{"testdata/mock-progress.sh"},
		LogDir:  t.TempDir(),
	}
	sup := NewSupervisor(cfg, DefaultOptions())

	notified := make(chan struct{}, 4)
	sup.SetStartupProgressListener(func() {
		select {
		case notified <- struct{}{}:
		default:
		}
	})
	sup.Start()

	select {
	case url := <-sup.Ready():
		if url != "http://127.0.0.1:18081" {
			t.Errorf("expected http://127.0.0.1:18081, got %s", url)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for ready")
	}
	// 就绪行与进度行走的是两条管道，顺序没有保证（生产中同样实测到就绪先于最后
	// 几条进度到达），因此这里等最终计数落地而不是假定它随就绪一起到。
	var got domain.StartupProgress
	deadline := time.Now().Add(2 * time.Second)
	for {
		got = sup.StartupProgress()
		if got.Reported && got.Loaded == 7 && got.Total == 7 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("StartupProgress = %+v, want Reported with 7/7", got)
		}
		time.Sleep(10 * time.Millisecond)
	}
	if got.OutputAt.IsZero() {
		t.Error("expected OutputAt to be recorded")
	}
	if got.StartedAt.IsZero() || got.OutputAt.Before(got.StartedAt) {
		t.Errorf("timeline inconsistent: started=%v output=%v", got.StartedAt, got.OutputAt)
	}
	if len(notified) == 0 {
		t.Error("expected the progress listener to be notified")
	}

	sup.Stop()
}

// 解析器边界：不带完整格式的行、以及插件自身写的其它行，都不能当成进度。
func TestStartupProgressPattern(t *testing.T) {
	cases := []struct {
		line    string
		matched bool
		loaded  string
		total   string
	}{
		{"dsh-desktop: startup 12/127", true, "12", "127"},
		{"dsh-desktop: startup 0/0", true, "0", "0"},
		{"dsh-desktop: startup", false, "", ""},
		{"dsh-desktop: startup 12", false, "", ""},
		{"dsh-desktop: startup 12/", false, "", ""},
		{"dsh-desktop: startup 12/127 extra", false, "", ""},
		{"dsh-desktop: startup unavailable boom", false, "", ""},
		{"dsh web: http://127.0.0.1:1", false, "", ""},
		{"prefix dsh-desktop: startup 1/2", false, "", ""},
	}
	for _, c := range cases {
		match := startupProgressPattern.FindStringSubmatch(c.line)
		if !c.matched {
			if match != nil {
				t.Errorf("%q: unexpected match %v", c.line, match)
			}
			continue
		}
		if match == nil {
			t.Fatalf("%q: expected match", c.line)
		}
		if match[1] != c.loaded || match[2] != c.total {
			t.Errorf("%q: got %s/%s, want %s/%s", c.line, match[1], match[2], c.loaded, c.total)
		}
	}
}

// 行切分是三处扫描器共用的实现：跨写入边界的半行必须拼起来再回调。
func TestLineSink_SplitsAcrossWrites(t *testing.T) {
	var lines []string
	sink := newLineSink(func(line string) { lines = append(lines, line) })
	if _, err := sink.Write([]byte("dsh-desktop: star")); err != nil {
		t.Fatal(err)
	}
	if len(lines) != 0 {
		t.Fatalf("expected no complete line yet, got %v", lines)
	}
	if _, err := sink.Write([]byte("tup 1/2\ndsh-desktop: startup 2/2\n")); err != nil {
		t.Fatal(err)
	}
	want := []string{"dsh-desktop: startup 1/2", "dsh-desktop: startup 2/2"}
	if len(lines) != len(want) {
		t.Fatalf("lines = %v, want %v", lines, want)
	}
	for i := range want {
		if lines[i] != want[i] {
			t.Errorf("line %d = %q, want %q", i, lines[i], want[i])
		}
	}
}

// TestBackoffDelay 固定重启退避的取值：指数增长到上限为止。
// 曾经的写法 base * (1 << (attempt-1)) 在 attempt 累加到 56 次以上时溢出为负数，
// 上限判断对负值不成立，time.After(负值) 立即触发，监护循环退化成无退避的
// spawn 风暴。本用例覆盖增长、封顶、超过位移宽度的 attempt 以及非法配置。
func TestBackoffDelay(t *testing.T) {
	cases := []struct {
		name    string
		base    int
		max     int
		attempt int
		want    int
	}{
		{"首次重启用基础延迟", 500, 10000, 1, 500},
		{"第二次翻倍", 500, 10000, 2, 1000},
		{"未到上限继续翻倍", 500, 10000, 5, 8000},
		{"越过上限即封顶", 500, 10000, 6, 10000},
		{"远超位移宽度仍封顶", 500, 10000, 200, 10000},
		{"attempt 极大也不为负", 500, 10000, 1 << 20, 10000},
		{"上限低于基础延迟时取上限", 500, 100, 1, 100},
		{"base 等于上限", 500, 500, 3, 500},
		{"未配置延迟时保持 0", 0, 10000, 3, 0},
		{"attempt 为 0 按首次处理", 500, 10000, 0, 500},
		{"非法上限退回基础延迟", 500, 0, 4, 500},
		{"接近 int 上限也不溢出", 1 << 40, 1<<62 + 1, 40, 1<<62 + 1},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := backoffDelay(c.base, c.max, c.attempt); got != c.want {
				t.Fatalf("backoffDelay(%d, %d, %d) = %d, want %d",
					c.base, c.max, c.attempt, got, c.want)
			}
		})
	}
}

// TestLineSink_DropsOverlongLine 覆盖 S4：子进程长时间不输出换行时，扫描器的行
// 缓冲必须有界。超长行整行丢弃，换行之后恢复正常切分。
func TestLineSink_DropsOverlongLine(t *testing.T) {
	var lines []string
	l := newLineSink(func(line string) { lines = append(lines, line) })

	if _, err := l.Write(bytes.Repeat([]byte("x"), maxLogLineBytes+1024)); err != nil {
		t.Fatal(err)
	}
	if len(l.buf) != 0 {
		t.Fatalf("超长行的内容仍留在缓冲里: %d 字节", len(l.buf))
	}
	// 仍在同一行内：换行前的后续片段同样丢弃。
	if _, err := l.Write([]byte("tail-of-huge-line")); err != nil {
		t.Fatal(err)
	}
	if len(lines) != 0 {
		t.Fatalf("超长行不应产生整行回调: %v", lines)
	}
	// 换行之后恢复正常。
	if _, err := l.Write([]byte("\nok\nnext\n")); err != nil {
		t.Fatal(err)
	}
	if len(lines) != 2 || lines[0] != "ok" || lines[1] != "next" {
		t.Fatalf("换行后应恢复正常切行, got %v", lines)
	}
}

// TestTimedWriter_TruncatesOverlongLine 覆盖 S4 的另一半：这里的缓冲就是日志内容，
// 超长行必须带标记落盘而不是丢弃，内存有界的同时不丢字节。
func TestTimedWriter_TruncatesOverlongLine(t *testing.T) {
	var out bytes.Buffer
	w := newTimedWriter(&out, "stdout")

	if _, err := w.Write(bytes.Repeat([]byte("y"), maxLogLineBytes+1024)); err != nil {
		t.Fatal(err)
	}
	if len(w.buf) != 0 {
		t.Fatalf("超长行落盘后缓冲应清空: %d 字节", len(w.buf))
	}
	if !strings.Contains(out.String(), "[truncated]") {
		t.Fatal("被截断的记录应带 [truncated] 标记")
	}
	if got := strings.Count(out.String(), "\n"); got != 1 {
		t.Fatalf("应只落盘一条被截断的记录, got %d 行", got)
	}
	// 同一行的剩余部分继续正常落盘。
	if _, err := w.Write([]byte("rest-of-line\n")); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "rest-of-line") {
		t.Fatal("换行后剩余部分应正常落盘")
	}
}

// TestLogSink_RotatesAtThreshold 覆盖 S4 的磁盘一侧：单次运行内累计写入到达阈值
// 即轮转，只保留一份历史，且不丢字节。
func TestLogSink_RotatesAtThreshold(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "harness.log")
	s := newLogSink(path)

	chunk := bytes.Repeat([]byte("z"), 64<<10)
	const writes = 100 // 6.4 MiB，超过 5 MiB 阈值
	for i := 0; i < writes; i++ {
		if _, err := s.Write(chunk); err != nil {
			t.Fatal(err)
		}
	}
	s.Close()

	rotated, err := os.Stat(path + ".1")
	if err != nil {
		t.Fatalf("到达阈值应生成轮转文件: %v", err)
	}
	cur, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if cur.Size() >= maxLogBytes {
		t.Fatalf("轮转后的当前文件应小于阈值: %d", cur.Size())
	}
	// 轮转不丢字节：两份文件之和等于实际写入量。
	if got, want := rotated.Size()+cur.Size(), int64(writes*len(chunk)); got != want {
		t.Fatalf("轮转后总字节 = %d, want %d", got, want)
	}
}

// TestLogSink_RotatesPreexistingOversizeFile 覆盖 S4 的跨重启一侧：上次运行留下的
// 超大日志会在本次运行的首次写入时被挪走，新内容不接在它后面继续追加。
func TestLogSink_RotatesPreexistingOversizeFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "harness.log")
	if err := os.WriteFile(path, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Truncate(path, maxLogBytes+1); err != nil { // 稀疏文件，不必真写
		t.Fatal(err)
	}

	s := newLogSink(path)
	if _, err := s.Write([]byte("fresh\n")); err != nil {
		t.Fatal(err)
	}
	s.Close()

	if _, err := os.Stat(path + ".1"); err != nil {
		t.Fatalf("超大日志应在打开时被轮转: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "fresh\n" {
		t.Fatalf("轮转后当前文件应只含本次内容, got %q", data)
	}
}

// TestLogSink_UnavailableIsNoop 覆盖日志不可用时的降级：写不出去也不能拦住子进程
// 输出（Write 返回成功，调用方不视为错误）。
func TestLogSink_UnavailableIsNoop(t *testing.T) {
	dir := t.TempDir()
	blocker := filepath.Join(dir, "not-a-dir")
	if err := os.WriteFile(blocker, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	s := newLogSink(filepath.Join(blocker, "harness.log"))
	if s.f != nil {
		t.Fatal("父路径是文件时不应打开成功")
	}
	n, err := s.Write([]byte("anything\n"))
	if err != nil || n != len("anything\n") {
		t.Fatalf("日志不可用时 Write 应静默成功, got n=%d err=%v", n, err)
	}
	s.Close() // 幂等，不应 panic
}
