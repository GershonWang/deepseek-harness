// 本文件收敛「关窗之后如何把窗口唤回来」这一组动作：窗口层的注入点、供托盘触发的
// ShowWindow，以及把信号接到动作上的接线。
//
// 为什么单独成文件：关窗行为现在由 closeAction 分叉（见 app.go 的 OnBeforeClose），
// 而「隐藏之后还能被叫回来」这件事跨了两个进程——切换器发 SIGUSR1，本进程收下并显示。
// 把这一来一回的约定集中在一处，改的时候不容易只改一半。
package app

import (
	"os"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// Wails 的窗口动作以包级变量暴露，供单测替换：runtime 是一组包级函数，没有接口可注入，
// 而 OnBeforeClose 与 ShowWindow 的分支行为必须能被断言。同 internal/clipboard 里
// socketDial 的做法。
//
// 读取类的三个动作（尺寸/位置/最大化）也在这里收口，理由不只是可测：窗口一旦销毁就不该
// 再被读——非主线程发起时要排队等 GTK 主线程执行（Wails 的 window.Size() 是
// invokeOnMainThread + wg.Wait），主循环退出后没人再排空那个队列，等待无人唤醒；即便调用方
// 正好在主线程上、绕过了等待，也只是对已释放的窗口对象取值。谁在什么时机读窗口因此是一条
// 需要被断言的不变式，见 app.go 的 Shutdown 与 saveWindowState。
var (
	windowHide        = runtime.WindowHide
	windowShow        = runtime.WindowShow
	windowUnminimise  = runtime.WindowUnminimise
	windowGetSize     = runtime.WindowGetSize
	windowGetPosition = runtime.WindowGetPosition
	windowIsMaximised = runtime.WindowIsMaximised
)

// HideOnCloseEnv 是切换器用来指定「关窗只隐藏」的环境变量名。
//
// 名字在 desktop-dual 的 switchboard 里另有一份字面量：两个模块是各自独立的 Go 模块，
// 切换器无法 import 本包（internal 也不允许跨模块），共享常量需要一个第三方模块，代价
// 大于一个字符串。两边各写一份并互相点名，各自再有一个用例把这个字面量钉死——改名时
// 至少会有一个模块的测试失败，而不是两边悄悄错开、表现为「托盘点了没反应」。
const HideOnCloseEnv = "DSH_DESKTOP_HIDE_ON_CLOSE"

// CloseActionFromEnv 解析关窗行为：只有切换器注入的那一个值会启用隐藏。
//
// 其余一切取值（变量缺失、空串、别的值）都保持「关窗即退出」——本进程没有单实例机制，
// 也没有自己的托盘，藏起来的窗口没有第二个入口能叫回来。
// @returns 关窗行为。
func CloseActionFromEnv() CloseAction {
	if os.Getenv(HideOnCloseEnv) == "1" {
		return CloseHides
	}
	return CloseQuits
}

// ShowWindow 把窗口显示出来并取消最小化，是「关窗后唤回」的落点。
//
// 窗口尚未就绪时（OnStartup 还没跑过，showCtx 仍为 nil）静默返回：切换器可以在拉起
// 客户端之后立刻被点击，那次唤回比窗口先到是可能的；这种情况让用户再点一次即可，
// 比让进程崩溃好。
func (a *App) ShowWindow() {
	a.showCtxMu.RLock()
	ctx := a.showCtx
	a.showCtxMu.RUnlock()
	if ctx == nil {
		return
	}
	// 先取消最小化再显示：窗口既可能是被关窗隐藏的，也可能是被用户最小化的，
	// 两种状态都要回到可见。
	windowUnminimise(ctx)
	windowShow(ctx)
}

// WatchShowSignal 把「显示窗口」信号接到动作上，直到通道关闭。
//
// 为什么注册信号的调用点必须在 main 里而不是本函数里：signal.Notify 要在任何一次隐藏
// 之前生效，否则未处理的 SIGUSR1 会按默认动作终止进程——表现为「点托盘显示，客户端被
// 杀」。main 在 wails.Run 之前注册，覆盖了窗口可能出现之前的整个窗口期；本函数只负责
// 「收到就执行」，因此可以在没有 Wails 上下文的环境里被断言。
// @param signals - 已由 signal.Notify 注册的通知通道。
// @param show - 收到信号时执行的动作；为 nil 时只消费信号，不做任何事。
func WatchShowSignal(signals <-chan os.Signal, show func()) {
	for range signals {
		if show != nil {
			show()
		}
	}
}
