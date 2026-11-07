// 本文件收敛「关窗之后如何把窗口唤回来」这一组动作：关窗行为的选择，以及托盘触发的
// PresentWindow。
//
// 为什么单独成文件：关窗行为由 closeAction 分叉（见 app.go 的 BeforeClose），而
// 「隐藏之后还能被叫回来」的窗口动作集中在这里；两者共用同一份窗口状态快照，
// 改的时候不容易只改一半。
package app

import (
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// Wails 的窗口动作以包级变量暴露，供单测替换：runtime 是一组包级函数，没有接口可注入，
// 而 BeforeClose 与 PresentWindow 的分支行为必须能被断言。
//
// 读取类的三个动作（尺寸/位置/最大化）也在这里收口，理由不只是可测：窗口一旦销毁就不该
// 再被读——非主线程发起时要排队等 GTK 主线程执行（Wails 的 window.Size() 是
// invokeOnMainThread + wg.Wait），主循环退出后没人再排空那个队列，等待无人唤醒；即便调用方
// 正好在主线程上、绕过了等待，也只是对已释放的窗口对象取值。谁在什么时机读窗口因此是一条
// 需要被断言的不变式，见 app.go 的 Shutdown 与 saveWindowState。
var (
	windowHide        = func() {}
	windowShow        = func() {}
	windowUnminimise  = func() {}
	windowUnmaximise  = func() {}
	windowGetSize     = func() (int, int) { return 0, 0 }
	windowGetPosition = func() (int, int) { return 0, 0 }
	windowIsMaximised = func() bool { return false }
	windowSetSize     = func(int, int) {}
	windowSetPosition = func(int, int) {}
	windowMaximise    = func() {}
	windowFocus       = func() {}
)

// bindWindowOps 把窗口动作接到 v3 的窗口对象上。
//
// 保留这层包级变量而不是直接调用 a.window.X：窗口动作在单测里没有真实 GTK 窗口，
// 需要一个可替换的注入点。
// @param w - 主窗口。
func bindWindowOps(w application.Window) {
	windowHide = func() { w.Hide() }
	windowShow = func() { w.Show() }
	windowUnminimise = func() { w.UnMinimise() }
	windowUnmaximise = func() { w.UnMaximise() }
	windowGetSize = func() (int, int) { return w.Size() }
	windowGetPosition = func() (int, int) { return w.Position() }
	windowIsMaximised = func() bool { return w.IsMaximised() }
	windowSetSize = func(width, height int) { w.SetSize(width, height) }
	windowSetPosition = func(x, y int) { w.SetPosition(x, y) }
	windowMaximise = func() { w.Maximise() }
	windowFocus = func() { w.Focus() }
}

// CloseQuitsEnv 强制「关窗即退出」，即使托盘可用。
//
// 存在理由：托盘常驻对大多数用户是期望行为，但排障或刻意单窗口使用时需要让关窗
// 真的结束进程，否则用户会以为窗口关掉了、进程却还在。
const CloseQuitsEnv = "DSH_DESKTOP_CLOSE_QUITS"

// ResolveCloseAction 依据环境变量与托盘可用性决定关窗行为。
//
// 优先级：
//  1. DSH_DESKTOP_CLOSE_QUITS=1：强制退出（排障/单窗口用法）；
//  2. 托盘可用：隐藏窗口——有托盘就有第二个入口把窗口叫回来；
//  3. 其余（含托盘创建失败）：关窗即退出——藏起来的窗口没有入口，比直接退出更糟。
//
// @param forceQuit - DSH_DESKTOP_CLOSE_QUITS 是否为 "1"。
// @param trayAvailable - 托盘是否创建成功。
// @returns 关窗行为。
func ResolveCloseAction(forceQuit, trayAvailable bool) CloseAction {
	if forceQuit {
		return CloseQuits
	}
	if trayAvailable {
		return CloseHides
	}
	return CloseQuits
}

// PresentWindow 把窗口叫到前面：先恢复隐藏前的几何，再显示，最后请求聚焦。
//
// 单实例的第二进程被拦下、以及托盘左键点击，都走这里——它们要的是同一件事，
// 没必要各自拼一遍窗口动作。
func (a *App) PresentWindow() {
	if !a.ready() {
		return
	}
	// 只有「确实被我们隐藏过」才还原几何：窗口可见（或仅被最小化）时用户点图标，
	// 期望的只是把它提到前面，强制拉回旧尺寸反而是退步。
	wasHidden := a.windowHidden.Swap(false)

	windowUnminimise()
	// 几何必须在 Show 之前恢复，不能反过来。
	//
	// GTK4 的窗口隐藏后，最大化状态会随窗口 unmapped 交给窗口管理器丢弃；重新 map
	// 时先以还原尺寸（实测 1280x695）出现约一帧，随后 maximize 才把它拉回全屏——用户
	// 看到的就是「全屏 → 小尺寸 → 全屏」的抖动，两次尺寸变化之间 WebKit 的合成层
	// 还会留下未重绘区域，要等鼠标事件才补画（真机采样：22:21:02.677 先是
	// 1280x695，133ms 后才是 1920x1043 最大化）。
	//
	// GTK4 允许在窗口显示前调用 gtk_window_maximize：窗口首次出现时即为最大化。
	// 因此这里先把隐藏前的状态设回去，让窗口直接以正确的几何出现。
	if wasHidden {
		// v3 的 Maximise() 在 IsMaximised() 为真时直接返回。窗口隐藏后 GTK 仍可能
		// 记着最大化状态，直接调用会被这个守卫吞掉，pending 最大化没设上，窗口于是
		// 仍以还原尺寸 map —— 修复就落空了。先显式取消再请求，保证状态机真的前进到
		// 「显示前最大化」。
		_, _, _, _, _, maximised := a.visibleSnapshot()
		if maximised {
			windowUnmaximise()
			windowMaximise()
		} else {
			a.restoreVisibleGeometry()
		}
	}
	windowShow()
	if wasHidden {
		w, h, x, y, hasPos, maximised := a.visibleSnapshot()
		a.logf("窗口唤回: 目标 %dx%d @(%d,%d) 有位置=%v 最大化=%v", w, h, x, y, hasPos, maximised)
	}
	windowFocus()

	// 映射完成后兜底校正一次：个别窗口管理器会忽略显示前设置的 pending 状态，
	// 延迟足够短，不会让用户觉得拖动/缩放无效。
	if !wasHidden {
		return
	}
	time.AfterFunc(150*time.Millisecond, func() {
		if !a.ready() || a.windowHidden.Load() {
			return
		}
		a.restoreVisibleGeometry()
	})
}

// visibleSnapshot 返回隐藏前记录的可见状态快照。
// @returns width - 可见宽度；height - 可见高度；x/y - 可见位置；
// hasPos - 位置是否可用；maximised - 隐藏前是否最大化。
func (a *App) visibleSnapshot() (width, height, x, y int, hasPos, maximised bool) {
	a.winMu.Lock()
	defer a.winMu.Unlock()
	return a.winVisibleWidth, a.winVisibleHeight, a.winVisibleX, a.winVisibleY,
		a.winVisibleHasPos, a.winVisibleMaximised
}

// restoreVisibleGeometry 按隐藏前的快照还原窗口几何。
//
// 为什么必须显式还原：GTK 的 hide→present 往返不保留最大化标记与位置——X11 的 WM 在
// 重新映射时把窗口当新窗口摆放，实测窗口会退回还原尺寸并跑到左上角。尺寸下限与位置
// 合理性都先校验：隐藏期间的读数不可信，宁可不动也不要恢复出一组坏几何。
func (a *App) restoreVisibleGeometry() {
	w, h, x, y, hasPos, maximised := a.visibleSnapshot()
	if maximised {
		windowMaximise()
		return
	}
	if w >= 400 && h >= 300 {
		windowSetSize(w, h)
	}
	if hasPos && a.windowPositionPlausible(x, y) {
		windowSetPosition(x, y)
	}
}
