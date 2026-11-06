// 关窗行为与托盘唤回的用例。
//
// 这一组行为跨了两个进程（切换器连唤回 socket，本进程收下并显示窗口）与一次部署选择
// （DSH_DESKTOP_HIDE_ON_CLOSE），因此测试分三层钉住：环境变量到 closeAction 的解析、
// BeforeClose 两条分支、以及唤回报文本身的安全性。
package app

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// stubWindowActions 替换窗口层注入点并记录调用序列。
// @param t - 测试上下文，用于在收尾时还原。
// @returns 调用序列的读取入口。
func stubWindowActions(t *testing.T) func() []string {
	t.Helper()
	var calls []string
	prevHide, prevShow, prevUnminimise := windowHide, windowShow, windowUnminimise
	windowHide = func() { calls = append(calls, "hide") }
	windowShow = func() { calls = append(calls, "show") }
	windowUnminimise = func() { calls = append(calls, "unminimise") }
	t.Cleanup(func() {
		windowHide, windowShow, windowUnminimise = prevHide, prevShow, prevUnminimise
	})
	return func() []string { return calls }
}

// TestHideOnCloseEnvNameIsPinned 钉死环境变量名。
//
// 名字在 desktop-dual 的 switchboard 里另有一份字面量（两个模块互相无法 import），
// 改一边而另一边没跟上的表现是「托盘点了没反应」——既没有报错也没有日志。这个用例让
// 改名必须同时改两处，且各自模块的测试会失败一次。
func TestHideOnCloseEnvNameIsPinned(t *testing.T) {
	if HideOnCloseEnv != "DSH_DESKTOP_HIDE_ON_CLOSE" {
		t.Fatalf("环境变量名 = %q，期望 %q；改名必须同步 desktop-dual 的 switchboard",
			HideOnCloseEnv, "DSH_DESKTOP_HIDE_ON_CLOSE")
	}
}

// TestResolveCloseAction 覆盖关窗语义矩阵。
//
// 这组取值直接决定「窗口关掉之后用户还能不能找回来」：托盘可用时才允许隐藏，
// 否则一旦隐藏就没有第二个入口。强制退出的开关必须在最前面，便于排障。
func TestResolveCloseAction(t *testing.T) {
	cases := []struct {
		name        string
		hideOnClose bool
		forceQuit   bool
		tray        bool
		want        CloseAction
	}{
		{"默认 + 托盘可用 → 隐藏", false, false, true, CloseHides},
		{"默认 + 无托盘 → 退出", false, false, false, CloseQuits},
		{"显式隐藏（dual 约定）", true, false, false, CloseHides},
		{"强制退出优先于托盘", false, true, true, CloseQuits},
		{"强制退出优先于显式隐藏", true, true, true, CloseQuits},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := ResolveCloseAction(c.hideOnClose, c.forceQuit, c.tray); got != c.want {
				t.Fatalf("ResolveCloseAction(%v,%v,%v) = %v，期望 %v",
					c.hideOnClose, c.forceQuit, c.tray, got, c.want)
			}
		})
	}
}

// TestCloseActionFromEnv 覆盖环境变量解析：返回值里的 bool 表示「环境变量是否显式指定」。
func TestCloseActionFromEnv(t *testing.T) {
	cases := []struct {
		name    string
		env     map[string]string
		want    CloseAction
		wantSet bool
	}{
		{"变量缺失", nil, CloseQuits, false},
		{"隐藏", map[string]string{HideOnCloseEnv: "1"}, CloseHides, true},
		{"强制退出", map[string]string{CloseQuitsEnv: "1"}, CloseQuits, true},
		{"两者都给时退出优先", map[string]string{HideOnCloseEnv: "1", CloseQuitsEnv: "1"}, CloseQuits, true},
		{"其它取值不启用", map[string]string{HideOnCloseEnv: "true"}, CloseQuits, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			for k, v := range c.env {
				t.Setenv(k, v)
			}
			got, set := CloseActionFromEnv()
			if got != c.want || set != c.wantSet {
				t.Fatalf("CloseActionFromEnv() = (%v,%v)，期望 (%v,%v)", got, set, c.want, c.wantSet)
			}
		})
	}
}

// TestBeforeCloseHidesOnlyWhenAsked 覆盖关窗两条分支：CloseHides 下拦下这次关闭并
// 隐藏窗口（返回 true 才能取消 Wails 的 Quit），CloseQuits 下放行且不碰窗口。
// 程序化退出（托盘菜单「退出」）必须绕过「关窗隐藏」，否则进程退不掉。
func TestBeforeCloseAllowsProgrammaticQuit(t *testing.T) {
	calls := stubWindowActions(t)
	a := &App{closeAction: CloseHides}
	a.quitting.Store(true)
	if a.BeforeClose() {
		t.Fatal("退出意图下必须放行关闭，否则进程无法退出")
	}
	if got := calls(); len(got) != 0 {
		t.Fatalf("退出意图下不应再隐藏窗口，实际调用 = %v", got)
	}
}

func TestBeforeCloseHidesOnlyWhenAsked(t *testing.T) {
	calls := stubWindowActions(t)

	hiding := &App{closeAction: CloseHides}
	if !hiding.BeforeClose() {
		t.Fatal("CloseHides 下应返回 true 以取消本次关闭")
	}
	if got := calls(); len(got) != 1 || got[0] != "hide" {
		t.Fatalf("CloseHides 下窗口调用 = %v，期望只有一次 hide", got)
	}

	quitting := &App{closeAction: CloseQuits}
	if quitting.BeforeClose() {
		t.Fatal("CloseQuits 下应返回 false 放行关闭")
	}
	if got := calls(); len(got) != 1 {
		t.Fatalf("CloseQuits 下不应再产生窗口调用，实际 = %v", got)
	}
}

// TestShowWindowWaitsForAReadyWindow 覆盖唤回的安全性：窗口未就绪时（OnStartup 尚未
// 发布上下文）静默返回，不能对 nil 上下文调用 Wails 的窗口 API。
func TestShowWindowWaitsForAReadyWindow(t *testing.T) {
	calls := stubWindowActions(t)

	(&App{}).ShowWindow()
	if got := calls(); len(got) != 0 {
		t.Fatalf("窗口未就绪时不应调用任何窗口动作，实际 = %v", got)
	}

	ready := &App{}
	ready.showCtxMu.Lock()
	ready.showCtx = context.Background()
	ready.showCtxMu.Unlock()
	ready.ShowWindow()
	got := calls()
	if len(got) != 2 || got[0] != "unminimise" || got[1] != "show" {
		t.Fatalf("唤回调用序列 = %v，期望先 unminimise 再 show（窗口可能只是被最小化）", got)
	}
}

// TestShowSocketRelIsPinned 钉死唤回通道的相对路径。
//
// 名字在 desktop-dual 的 switchboard 里另有一份字面量（两个模块互相无法 import）。改一边
// 而另一边没跟上的表现是「点托盘没反应」，而切换器那边会把拨号失败当成客户端已死、再拉起
// 一个——既不报错也没有明显日志。这个用例让改名必须同时改两处，各自失败一次。
func TestShowSocketRelIsPinned(t *testing.T) {
	if ShowSocketRel != ".cache/dsh-desktop/show.sock" {
		t.Fatalf("唤回通道相对路径 = %q，期望 %q；改名必须同步 desktop-dual 的 switchboard",
			ShowSocketRel, ".cache/dsh-desktop/show.sock")
	}
}

// TestServeShowRequestsInvokesShow 覆盖唤回通道的主路径：连上并写一行即触发显示。
func TestServeShowRequestsInvokesShow(t *testing.T) {
	path := filepath.Join(t.TempDir(), "show.sock")
	called := make(chan struct{}, 1)
	stop, err := ServeShowRequests(path, func() { called <- struct{}{} })
	if err != nil {
		t.Fatalf("建立唤回通道失败: %v", err)
	}
	defer stop()

	conn, err := net.Dial("unix", path)
	if err != nil {
		t.Fatalf("连接唤回通道失败: %v", err)
	}
	if _, err := conn.Write([]byte("show\n")); err != nil {
		t.Fatalf("写唤回请求失败: %v", err)
	}
	_ = conn.Close()

	select {
	case <-called:
	case <-time.After(2 * time.Second):
		t.Fatal("收到唤回请求后未执行显示动作")
	}
}

// TestServeShowRequestsReplacesStaleSocket 覆盖上一次运行遗留 socket 文件的情形。
//
// 进程被 SIGKILL 或走 os.Exit 收尾时不会执行 stop，文件会留在原地；此时 bind 会因
// EADDRINUSE 失败，而那正是「客户端从此唤不回来」的静默故障。
func TestServeShowRequestsReplacesStaleSocket(t *testing.T) {
	path := filepath.Join(t.TempDir(), "show.sock")
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	stop, err := ServeShowRequests(path, nil)
	if err != nil {
		t.Fatalf("已存在的陈旧 socket 文件不应阻止监听: %v", err)
	}
	stop()
}

// TestServeShowRequestsStopRefusesConnections 覆盖停止后的契约：通道不再接受请求。
//
// 判据是「拨号必须失败」而不是「文件消失」：切换器把拨号成功当作唤回成功，若通道关了却
// 仍能拨通，用户会看到一次「点了没反应，也没有任何报错」。文件本身故意留着（见
// ServeShowRequests 里关于删除归属的说明），由下次启动 bind 之前清掉。
func TestServeShowRequestsStopRefusesConnections(t *testing.T) {
	path := filepath.Join(t.TempDir(), "show.sock")
	stop, err := ServeShowRequests(path, nil)
	if err != nil {
		t.Fatalf("建立唤回通道失败: %v", err)
	}
	stop()
	stop() // 幂等

	conn, err := net.DialTimeout("unix", path, time.Second)
	if err == nil {
		_ = conn.Close()
		t.Fatal("停止后仍能连上唤回通道：切换器会把这次假成功当成唤回成功")
	}
}

// stubWindowState 固定窗口读数，供捕获/还原逻辑断言。
// @param t - 测试上下文（负责恢复原函数）。
// @param width/height - windowGetSize 的返回值。
// @param x/y - windowGetPosition 的返回值。
// @param maximised - windowIsMaximised 的返回值。
func stubWindowState(t *testing.T, width, height, x, y int, maximised bool) {
	t.Helper()
	prevSize, prevPos, prevMax := windowGetSize, windowGetPosition, windowIsMaximised
	windowGetSize = func() (int, int) { return width, height }
	windowGetPosition = func() (int, int) { return x, y }
	windowIsMaximised = func() bool { return maximised }
	t.Cleanup(func() {
		windowGetSize, windowGetPosition, windowIsMaximised = prevSize, prevPos, prevMax
	})
}

// stubWindowWrites 记录写窗口动作，返回当前的调用序列。
// @param t - 测试上下文（负责恢复原函数）。
// @returns 读取已记录调用序列的函数。
func stubWindowWrites(t *testing.T) func() []string {
	t.Helper()
	var calls []string
	prevSetSize, prevSetPos := windowSetSize, windowSetPosition
	prevMax, prevFocus := windowMaximise, windowFocus
	windowSetSize = func(w, h int) { calls = append(calls, "size") }
	windowSetPosition = func(x, y int) { calls = append(calls, "position") }
	windowMaximise = func() { calls = append(calls, "maximise") }
	windowFocus = func() { calls = append(calls, "focus") }
	t.Cleanup(func() {
		windowSetSize, windowSetPosition = prevSetSize, prevSetPos
		windowMaximise, windowFocus = prevMax, prevFocus
	})
	return func() []string { return calls }
}

// 隐藏前的快照必须来自「关窗那一刻」的读数：只有它能在 WAILS 的 hide→present
// 往返之后把最大化状态与位置还原回去。
func TestCaptureVisibleGeometryRecordsVisibleState(t *testing.T) {
	stubWindowState(t, 1280, 700, 320, 192, false)
	a := &App{bound: true}
	a.captureVisibleGeometry()

	if a.winVisibleMaximised {
		t.Fatal("非最大化窗口不应记为最大化")
	}
	if a.winVisibleWidth != 1280 || a.winVisibleHeight != 700 {
		t.Fatalf("尺寸 = %dx%d，期望 1280x700", a.winVisibleWidth, a.winVisibleHeight)
	}
	if !a.winVisibleHasPos || a.winVisibleX != 320 || a.winVisibleY != 192 {
		t.Fatalf("位置 = (%d,%d) hasPos=%v，期望 (320,192)", a.winVisibleX, a.winVisibleY, a.winVisibleHasPos)
	}
}

// 最大化时只记标记，不把最大化尺寸当成「还原尺寸」——否则取消最大化后会得到全屏尺寸。
func TestCaptureVisibleGeometryKeepsSizeWhenMaximised(t *testing.T) {
	stubWindowState(t, 1920, 1043, 0, 0, true)
	a := &App{bound: true}
	a.captureVisibleGeometry()

	if !a.winVisibleMaximised {
		t.Fatal("最大化窗口必须被记为最大化")
	}
	if a.winVisibleWidth != 0 || a.winVisibleHeight != 0 {
		t.Fatalf("最大化时不应写入还原尺寸，得到 %dx%d", a.winVisibleWidth, a.winVisibleHeight)
	}
}

// 隐藏前最大化：唤回时只需重新最大化。
func TestRestoreVisibleGeometryMaximises(t *testing.T) {
	calls := stubWindowWrites(t)
	a := &App{bound: true, winVisibleMaximised: true}
	a.restoreVisibleGeometry()

	got := calls()
	if len(got) != 1 || got[0] != "maximise" {
		t.Fatalf("调用 = %v，期望仅 maximise", got)
	}
}

// 隐藏前非最大化：尺寸与位置都要还原，否则窗口会跑到左上角。
func TestRestoreVisibleGeometryRestoresSizeAndPosition(t *testing.T) {
	calls := stubWindowWrites(t)
	a := &App{
		bound:            true,
		winVisibleWidth:  1280,
		winVisibleHeight: 700,
		winVisibleX:      320,
		winVisibleY:      192,
		winVisibleHasPos: true,
	}
	a.restoreVisibleGeometry()

	got := calls()
	if len(got) != 2 || got[0] != "size" || got[1] != "position" {
		t.Fatalf("调用 = %v，期望先 size 再 position", got)
	}
}

// 可疑尺寸（隐藏期间读数）不得被恢复：宁可不动，也不要恢复出一组坏几何。
func TestRestoreVisibleGeometrySkipsBogusSize(t *testing.T) {
	calls := stubWindowWrites(t)
	a := &App{bound: true, winVisibleWidth: 200, winVisibleHeight: 100}
	a.restoreVisibleGeometry()

	if got := calls(); len(got) != 0 {
		t.Fatalf("可疑尺寸不应触发写动作，实际 = %v", got)
	}
}
