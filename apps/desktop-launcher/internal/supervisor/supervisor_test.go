package supervisor

import (
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
