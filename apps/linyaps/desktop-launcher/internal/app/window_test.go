package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// 窗口位置必须随尺寸一起持久化：只有尺寸时每次启动都回到默认位置（审计 27）。
// 这里固定 JSON 契约——x/y 为 0 时省略（0,0 即「未记录」），非零时往返一致。
func TestWindowState_PositionRoundTrip(t *testing.T) {
	home := t.TempDir()
	if err := SaveWindowState(home, WindowState{Width: 1280, Height: 800, X: 120, Y: 64}); err != nil {
		t.Fatal(err)
	}
	got, err := LoadWindowState(home)
	if err != nil {
		t.Fatal(err)
	}
	if got.X != 120 || got.Y != 64 {
		t.Fatalf("位置未往返: %+v", got)
	}

	// 未记录位置时不写 x/y 键：省略与显式 0 在语义上都表示「没有记录」，
	// 但省掉键能让配置文件保持与旧版本一致。
	if err := SaveWindowState(home, WindowState{Width: 1280, Height: 800}); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(AppConfigFilePath(home))
	if err != nil {
		t.Fatal(err)
	}
	var probe map[string]map[string]any
	if err := json.Unmarshal(raw, &probe); err != nil {
		t.Fatal(err)
	}
	if _, ok := probe["window"]["x"]; ok {
		t.Fatalf("0 坐标不应写进配置: %s", raw)
	}
}

// positionWithinScreens 是恢复位置前的保守判定：Wails 的 Screen 没有原点，
// 只能用「所有屏幕拼起来的外接框」近似。明显越界时必须判否——否则副屏被拔掉后
// 窗口会恢复到屏幕外，用户看不到也点不着。
func TestPositionWithinScreens(t *testing.T) {
	// v3 的 Screen 指针语义（ScreenManager.GetAll 返回 []*Screen），构造时返回指针。
	screen := func(w, h int) *application.Screen {
		s := &application.Screen{}
		s.Size.Width, s.Size.Height = w, h
		return s
	}
	one := []*application.Screen{screen(1920, 1080)}
	two := []*application.Screen{screen(1920, 1080), screen(1280, 1024)}
	cases := []struct {
		name    string
		screens []*application.Screen
		x, y    int
		want    bool
	}{
		{"主屏内", one, 100, 100, true},
		{"主屏右下角内侧", one, 1919, 1079, true},
		{"右侧副屏内（按各屏宽度之和放宽）", two, 3000, 500, true},
		{"左侧副屏的负坐标（按一块屏宽放宽）", two, -1500, 500, true},
		{"副屏被拔掉后的旧坐标", one, 3000, 500, false},
		{"纵向越界", one, 100, 2000, false},
		{"负得过多", one, -3000, 100, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := positionWithinScreens(tc.screens, tc.x, tc.y); got != tc.want {
				t.Fatalf("positionWithinScreens(%d,%d) = %v, want %v", tc.x, tc.y, got, tc.want)
			}
		})
	}
}

// 配置损坏或字段缺失时位置回退为「未记录」，不打断启动。
func TestLoadWindowState_MissingPosition(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, ".config", "dsh-desktop")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config.json"),
		[]byte(`{"window":{"width":1000,"height":700,"maximized":false}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := LoadWindowState(home)
	if err != nil {
		t.Fatal(err)
	}
	if got.X != 0 || got.Y != 0 {
		t.Fatalf("缺失位置应为 0: %+v", got)
	}
	if got.Width != 1000 || got.Height != 700 {
		t.Fatalf("尺寸应保留: %+v", got)
	}
}

// 硬件加速策略必须显式给出：v3 的 GTK4 后端只区分 Never 与「其余一律 Always」，
// 传 Never 会把内嵌 WebView 的加速合成整体关闭、页面退回 CPU 绘制。这条用例钉住
// 「不是 Never」，并记录 GTK4 下 OnDemand 的实际语义（等价于 Always）。
func TestWailsLinuxOptionsEnablesWebviewAcceleration(t *testing.T) {
	opts := WailsLinuxOptions()
	if opts.WebviewGpuPolicy == application.WebviewGpuPolicyNever {
		t.Fatalf("策略不得为 Never（加速合成会被整体关闭），得到 %d", int(opts.WebviewGpuPolicy))
	}
	if got, want := opts.WebviewGpuPolicy, application.WebviewGpuPolicyOnDemand; got != want {
		t.Fatalf("策略 = %d，期望 OnDemand(%d)；若上游调整了常量顺序需要重新核对",
			int(got), int(want))
	}
}
