package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 会话 id 的形状校验：它来自页面（wire 边界），随后会被写进 overlay 的插件 config，
// 因此只放行一个字符集。空串合法——含义是「当前没有会话」，由调用方解释为清除。
func TestValidSessionID(t *testing.T) {
	cases := []struct {
		name string
		id   string
		want bool
	}{
		{"empty means no Session", "", true},
		{"current form", "session-ce872d03-4f5d-4762-b645-735d7967d775", true},
		{"legacy form", "42696681-0879-4b77-b87f-9844e2525f8b", true},
		{"at the length limit", strings.Repeat("a", maxSessionIDLength), true},
		{"over the length limit", strings.Repeat("a", maxSessionIDLength+1), false},
		{"space", "session-bad id", false},
		{"newline", "session-bad\nid", false},
		{"markup", "session-<script>", false},
		{"quote", "session-\"x\"", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := validSessionID(c.id); got != c.want {
				t.Errorf("validSessionID(%q) = %v, want %v", c.id, got, c.want)
			}
		})
	}
}

// 记忆往返且不动同文件里的其他字段；空串清除记录。
func TestSaveLastSession_RoundTripAndClear(t *testing.T) {
	home := t.TempDir()
	if err := SaveWebPort(home, 40321); err != nil {
		t.Fatal(err)
	}
	if err := SaveLastSession(home, "session-abc"); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadAppConfig(home)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.LastSessionID != "session-abc" {
		t.Fatalf("LastSessionID = %q, want session-abc", cfg.LastSessionID)
	}
	if cfg.WebPort != 40321 {
		t.Fatalf("WebPort = %d, want 40321 (other fields must survive)", cfg.WebPort)
	}

	if err := SaveLastSession(home, ""); err != nil {
		t.Fatal(err)
	}
	cfg, err = LoadAppConfig(home)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.LastSessionID != "" {
		t.Fatalf("LastSessionID = %q, want cleared", cfg.LastSessionID)
	}
}

// 配置文件被手工改坏（形状非法的 id）时按「没有记录」处理：这个值会被写进 overlay 的
// 插件 config，宁可这次不还原，也不让一次手改污染下次启动的启动参数。
func TestLoadAppConfig_RejectsInvalidLastSessionID(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, ".config", "dsh-desktop")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config.json"),
		[]byte(`{"window":{"width":1280,"height":800},"lastSessionId":"session-<script>"}`), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadAppConfig(home)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.LastSessionID != "" {
		t.Fatalf("LastSessionID = %q, want dropped", cfg.LastSessionID)
	}
}

// 绑定只落盘形状合法的 id：非法值丢弃且不改动已有记录，空串清除（用户离开了那段对话）。
func TestRememberSession_StoresOnlyValidIDs(t *testing.T) {
	home := t.TempDir()
	a := &App{home: home}

	a.RememberSession("session-ok")
	if cfg, _ := LoadAppConfig(home); cfg.LastSessionID != "session-ok" {
		t.Fatalf("LastSessionID = %q, want session-ok", cfg.LastSessionID)
	}

	a.RememberSession("session-bad id")
	if cfg, _ := LoadAppConfig(home); cfg.LastSessionID != "session-ok" {
		t.Fatalf("invalid id changed the record: %q", cfg.LastSessionID)
	}

	a.RememberSession("")
	if cfg, _ := LoadAppConfig(home); cfg.LastSessionID != "" {
		t.Fatalf("LastSessionID = %q, want cleared", cfg.LastSessionID)
	}
}
