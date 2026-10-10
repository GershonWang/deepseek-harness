package app

import (
	"os"
	"strings"
	"testing"
)

// SetTheme 只服务下次启动的首帧底色：记录取值，且只在变化时落盘（审计 28）。
//
// 「只在变化时落盘」用一个 json 往返会丢掉的未知字段做哨兵来断言——同值调用后文件
// 必须原样保留，取值变化后必须被重写。比对比 mtime 更可靠（同秒内的两次写入区分不出来）。
func TestSetThemePersistsOnlyOnChange(t *testing.T) {
	home := t.TempDir()
	a := &App{home: home}

	a.SetTheme(true)
	cfg, err := LoadAppConfig(home)
	if err != nil || cfg.Theme != ThemeDark {
		t.Fatalf("深色主题未落盘: cfg=%+v err=%v", cfg, err)
	}

	sentinel := `{"theme":"dark","unknown":"keep"}`
	if err := os.WriteFile(AppConfigFilePath(home), []byte(sentinel), 0o600); err != nil {
		t.Fatal(err)
	}
	a.SetTheme(true)
	raw, err := os.ReadFile(AppConfigFilePath(home))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"unknown":"keep"`) {
		t.Fatalf("同值重复调用不应重写配置文件: %s", raw)
	}

	a.SetTheme(false)
	raw, err = os.ReadFile(AppConfigFilePath(home))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "unknown") {
		t.Fatalf("取值变化时应重写配置文件: %s", raw)
	}
	cfg, _ = LoadAppConfig(home)
	if cfg.Theme != ThemeLight {
		t.Fatalf("浅色主题未落盘: %+v", cfg)
	}
}

// 首帧底色必须与 frontend/styles.css 的 --bg 一致（暗 #1e1e1e、亮 #f5f5f5）：
// 不一致时页面绘制之前会闪一下与当前主题相反的底色。
func TestFirstFrameBackground(t *testing.T) {
	if r, g, b := FirstFrameBackground(ThemeLight); r != 245 || g != 245 || b != 245 {
		t.Fatalf("浅色首帧底色应等于 --bg #f5f5f5, got #%02x%02x%02x", r, g, b)
	}
	if r, g, b := FirstFrameBackground(ThemeDark); r != 30 || g != 30 || b != 30 {
		t.Fatalf("深色首帧底色应等于 --bg #1e1e1e, got #%02x%02x%02x", r, g, b)
	}
	// 尚未记录（首次启动）时保持旧行为：暗色。
	if r, g, b := FirstFrameBackground(""); r != 30 || g != 30 || b != 30 {
		t.Fatalf("主题未知时应回退暗色, got #%02x%02x%02x", r, g, b)
	}
}
