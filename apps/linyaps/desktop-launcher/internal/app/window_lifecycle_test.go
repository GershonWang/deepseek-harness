// 关窗行为与托盘唤回的用例。
//
// 这一组行为由部署选择（DSH_DESKTOP_CLOSE_QUITS）与托盘可用性共同决定，因此测试分两层
// 钉住：环境变量到 closeAction 的解析、BeforeClose 两条分支，以及唤回时的调用顺序与
// 几何恢复。
package app

import "testing"

// stubWindowActions 替换窗口层注入点并记录调用序列。
//
// 同时接管写动作（最大化/尺寸/位置/聚焦）而不只是显示与隐藏：唤回的调用顺序
// 本身就是判据（几何必须先于 Show），只记录 hide/show 会让顺序回归无声通过。
// @param t - 测试上下文，用于在收尾时还原。
// @returns 调用序列的读取入口。
func stubWindowActions(t *testing.T) func() []string {
	t.Helper()
	var calls []string
	prevHide, prevShow, prevUnminimise := windowHide, windowShow, windowUnminimise
	prevUnmaximise, prevMaximise, prevSetSize, prevSetPosition, prevFocus := windowUnmaximise, windowMaximise, windowSetSize, windowSetPosition, windowFocus
	windowHide = func() { calls = append(calls, "hide") }
	windowShow = func() { calls = append(calls, "show") }
	windowUnminimise = func() { calls = append(calls, "unminimise") }
	windowUnmaximise = func() { calls = append(calls, "unmaximise") }
	windowMaximise = func() { calls = append(calls, "maximise") }
	windowSetSize = func(int, int) { calls = append(calls, "size") }
	windowSetPosition = func(int, int) { calls = append(calls, "position") }
	windowFocus = func() { calls = append(calls, "focus") }
	t.Cleanup(func() {
		windowHide, windowShow, windowUnminimise = prevHide, prevShow, prevUnminimise
		windowUnmaximise, windowMaximise, windowSetSize, windowSetPosition, windowFocus = prevUnmaximise, prevMaximise, prevSetSize, prevSetPosition, prevFocus
	})
	return func() []string { return calls }
}

// TestResolveCloseAction 覆盖关窗语义矩阵。
//
// 这组取值直接决定「窗口关掉之后用户还能不能找回来」：托盘可用时才允许隐藏，
// 否则一旦隐藏就没有第二个入口。强制退出的开关必须在最前面，便于排障。
func TestResolveCloseAction(t *testing.T) {
	cases := []struct {
		name      string
		forceQuit bool
		tray      bool
		want      CloseAction
	}{
		{"托盘可用 → 隐藏", false, true, CloseHides},
		{"无托盘 → 退出", false, false, CloseQuits},
		{"强制退出优先于托盘", true, true, CloseQuits},
		{"强制退出 + 无托盘", true, false, CloseQuits},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := ResolveCloseAction(c.forceQuit, c.tray); got != c.want {
				t.Fatalf("ResolveCloseAction(%v,%v) = %v，期望 %v",
					c.forceQuit, c.tray, got, c.want)
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

// TestPresentWindowRestoresGeometryBeforeShow 断言唤回时先恢复隐藏前的几何、再显示窗口。
//
// 顺序错了在真机上就是「全屏 → 小尺寸 → 全屏」的抖动：GTK4 隐藏后重新 map 时先以
// 还原尺寸（实测 1280x695）出现一帧，随后 maximize 才拉回全屏；两次尺寸变化之间
// WebKit 的合成层会留下未重绘区域，要等鼠标事件才补画（真机采样记录到 1280x695
// 出现 133ms 后才变成最大化 1920x1043）。GTK4 允许在显示前调用 gtk_window_maximize，
// 因此几何必须先于 Show。
func TestPresentWindowRestoresGeometryBeforeShow(t *testing.T) {
	cases := []struct {
		name      string
		maximised bool
		before    []string
	}{
		// 先 UnMaximise 再 Maximise：v3 的 Maximise 在 IsMaximised() 为真时直接返回，
		// 而隐藏后 GTK 可能仍记着最大化，只有显式取消再请求才能让 pending 最大化生效。
		{"隐藏前最大化 → 显示前先最大化", true, []string{"unmaximise", "maximise"}},
		{"隐藏前普通尺寸 → 显示前先设回尺寸", false, []string{"size"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			calls := stubWindowActions(t)
			a := &App{bound: true}
			a.windowHidden.Store(true)
			a.winVisibleMaximised = c.maximised
			a.winVisibleWidth, a.winVisibleHeight = 1280, 695
			a.winVisibleX, a.winVisibleY = 0, 0
			a.winVisibleHasPos = true

			a.PresentWindow()

			got := calls()
			indexOf := func(name string) int {
				for i, call := range got {
					if call == name {
						return i
					}
				}
				return -1
			}
			show := indexOf("show")
			if show < 0 {
				t.Fatalf("调用序列 = %v，缺少 show", got)
			}
			for _, name := range c.before {
				index := indexOf(name)
				if index < 0 || index > show {
					t.Fatalf("调用序列 = %v，期望 %q 出现在 show 之前（否则窗口先以旧尺寸出现再跳变）",
						got, name)
				}
			}
		})
	}
}

// TestPresentWindowDoesNotTouchGeometryWhenVisible 断言窗口本来就可见时只前置，不改几何。
//
// 用户点托盘图标时如果窗口只是被遮挡（没隐藏过），强制拉回旧尺寸或最大化反而是退步。
func TestPresentWindowDoesNotTouchGeometryWhenVisible(t *testing.T) {
	calls := stubWindowActions(t)
	a := &App{bound: true}
	a.winVisibleMaximised = true

	a.PresentWindow()

	for _, call := range calls() {
		if call == "maximise" || call == "size" || call == "position" {
			t.Fatalf("窗口可见时不应调整几何，实际调用 = %v", calls())
		}
	}
}
