package app

import (
	"os"
	"path/filepath"
	"testing"
)

// 端口必须随配置一起持久化：它是 harness Web GUI 的 origin 的一部分，而 GUI 的
// 界面状态（含「上次会话」）按 origin 存在 localStorage 里。丢了这个字段，下次启动
// 就会换端口、换 origin，用户看到的就是「每次都开一个新会话」。
func TestWebPort_RoundTripKeepsOtherFields(t *testing.T) {
	home := t.TempDir()
	if err := SaveAppConfig(home, AppConfig{
		ExternalURL: "http://127.0.0.1:3456",
		Window:      WindowState{Width: 1280, Height: 800},
		Theme:       "dark",
	}); err != nil {
		t.Fatal(err)
	}

	if err := SaveWebPort(home, 40321); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadAppConfig(home)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.WebPort != 40321 {
		t.Fatalf("WebPort = %d, want 40321", cfg.WebPort)
	}
	// 端口写在同一个文件里，读改写不能顺手清掉别的字段。
	if cfg.ExternalURL != "http://127.0.0.1:3456" || cfg.Theme != "dark" || cfg.Window.Width != 1280 {
		t.Fatalf("其他字段被覆盖: %+v", cfg)
	}
}

// 本次没拿到具体端口（0）时不写文件：既不该留下空配置，也不该把「没有端口」
// 变成一个待复用的端口。
func TestWebPort_ZeroIsNotPersisted(t *testing.T) {
	home := t.TempDir()
	if err := SaveWebPort(home, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(AppConfigFilePath(home)); !os.IsNotExist(err) {
		t.Fatalf("port 0 不应写配置: err=%v", err)
	}
}

// 配置文件被手工改坏（越界端口）时按「没有记录」处理，交给 appenv 另挑一个；
// 直接把非法值透传给 harness 会让它启动失败。
func TestLoadAppConfig_RejectsOutOfRangeWebPort(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, ".config", "dsh-desktop")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config.json"),
		[]byte(`{"window":{"width":1280,"height":800},"webPort":70000}`), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadAppConfig(home)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.WebPort != 0 {
		t.Fatalf("WebPort = %d, want 0 for out-of-range input", cfg.WebPort)
	}
}
