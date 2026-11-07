// 本文件覆盖独立「关于」窗口的可判定部分：窗口形态选项与主题读取。
//
// 为什么钉这些字段：尺寸、URL 与不可缩放写错不会编译失败，只会表现为内容被裁掉、
// 窗口拖成一片空白，或页面 404；而这些都要真机打开窗口才看得见。
package app

import (
	"testing"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// TestAboutWindowOptionsFixWindowShape 断言关于窗口的形态字段。
func TestAboutWindowOptionsFixWindowShape(t *testing.T) {
	const title = "关于 DeepSeek Harness"
	opts := aboutWindowOptions(title, 30, 31, 32)

	if opts.Name != aboutWindowName {
		t.Fatalf("Name = %q，期望 %q（按名字找回已开窗口，改名会每点一次多一个窗口）", opts.Name, aboutWindowName)
	}
	if opts.Title != title {
		t.Fatalf("Title = %q，期望 %q", opts.Title, title)
	}
	if opts.URL != "/about.html" {
		t.Fatalf("URL = %q，期望 /about.html（main.go 的嵌入清单提供该页面）", opts.URL)
	}
	if opts.Width != aboutWindowWidth || opts.Height != aboutWindowHeight {
		t.Fatalf("尺寸 = %dx%d，期望 %dx%d", opts.Width, opts.Height, aboutWindowWidth, aboutWindowHeight)
	}
	if opts.MinWidth != aboutWindowWidth || opts.MinHeight != aboutWindowHeight {
		t.Fatalf("最小尺寸 = %dx%d，期望与初始尺寸一致", opts.MinWidth, opts.MinHeight)
	}
	if !opts.DisableResize {
		t.Fatal("关于窗口应禁止缩放：内容按固定宽度排版，缩放后字段网格会折行")
	}
	if opts.InitialPosition != application.WindowCentered {
		t.Fatalf("InitialPosition = %v，期望居中", opts.InitialPosition)
	}
	if opts.BackgroundColour != application.NewRGB(30, 31, 32) {
		t.Fatalf("首帧底色 = %#v，期望与传入分量一致", opts.BackgroundColour)
	}
}

// TestThemeReadsSavedConfig 断言关于窗口的首帧底色取自上次记录的主题。
//
// 主题真源是系统、由前端回推；关于窗口必须在页面绘制前就有底色，只能读这份记录。
func TestThemeReadsSavedConfig(t *testing.T) {
	home := t.TempDir()
	cfg := DefaultAppConfig()
	cfg.Theme = ThemeLight
	if err := SaveAppConfig(home, cfg); err != nil {
		t.Fatalf("写配置失败: %v", err)
	}

	a := &App{home: home}
	if got := a.theme(); got != ThemeLight {
		t.Fatalf("theme() = %q，期望 %q", got, ThemeLight)
	}
}

// TestThemeFallsBackWhenConfigMissing 断言没有配置记录时不报错、返回空主题。
//
// 首次启动就是这种状态：FirstFrameBackground 会把空主题落到暗色，与主窗口首帧一致。
func TestThemeFallsBackWhenConfigMissing(t *testing.T) {
	a := &App{home: t.TempDir()}
	if got := a.theme(); got != "" {
		t.Fatalf("theme() = %q，期望空串（无配置记录）", got)
	}
}
