package app

import (
	"os"
	"path/filepath"
	"strings"
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

// 壳设置必须存进 AppConfig 并与其它字段共存：v3 的壳 origin 从 wails://wails 变成
// wails://localhost，localStorage 按 origin 隔离，旧值读不到；改存 Go 侧后才能跨框架、
// 跨 origin 稳定。这里固定三条不变式：空配置返回 {}、写入后能读回、读改写不丢其它字段。
func TestShellSettings_RoundTripKeepsOtherFields(t *testing.T) {
	home := t.TempDir()
	a := &App{home: home}

	if got := a.ShellSettingsJSON(); got != "{}" {
		t.Fatalf("空配置应返回 {}，得到 %q", got)
	}

	// 先放一个无关字段，验证随后的读改写不会把它抹掉。
	cfg, _ := LoadAppConfig(home)
	cfg.Theme = "dark"
	if err := SaveAppConfig(home, cfg); err != nil {
		t.Fatal(err)
	}

	if res := a.SaveShellSetting("dsh-desktop.locale", "en"); strings.HasPrefix(res, "error") {
		t.Fatalf("写入失败: %s", res)
	}
	if res := a.SaveShellSetting("dsh-desktop.terminal.fontSize", "15"); strings.HasPrefix(res, "error") {
		t.Fatalf("写入失败: %s", res)
	}

	got := a.ShellSettingsJSON()
	if !strings.Contains(got, `"dsh-desktop.locale":"en"`) || !strings.Contains(got, `"dsh-desktop.terminal.fontSize":"15"`) {
		t.Fatalf("设置未读回: %s", got)
	}

	after, _ := LoadAppConfig(home)
	if after.Theme != "dark" {
		t.Fatalf("读改写丢了其它字段: theme=%q", after.Theme)
	}

	// 空值等价于 removeItem。
	if res := a.SaveShellSetting("dsh-desktop.locale", ""); strings.HasPrefix(res, "error") {
		t.Fatalf("删除失败: %s", res)
	}
	if got := a.ShellSettingsJSON(); strings.Contains(got, "dsh-desktop.locale") {
		t.Fatalf("删除后仍存在: %s", got)
	}
}
