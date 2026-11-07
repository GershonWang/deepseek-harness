// 本文件收敛「关窗之后如何把窗口唤回来」这一组动作：窗口层的注入点、供托盘触发的
// ShowWindow，以及切换器用来触发它的本地通道。
//
// 为什么单独成文件：关窗行为现在由 closeAction 分叉（见 app.go 的 BeforeClose），
// 而「隐藏之后还能被叫回来」这件事跨了两个进程——切换器连唤回 socket，本进程收下并显示。
// 把这一来一回的约定集中在一处，改的时候不容易只改一半。
package app

import (
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// Wails 的窗口动作以包级变量暴露，供单测替换：runtime 是一组包级函数，没有接口可注入，
// 而 BeforeClose 与 ShowWindow 的分支行为必须能被断言。同 internal/clipboard 里
// socketDial 的做法。
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
// 需要一个可替换的注入点（与 internal/clipboard 里 socketDial 的做法一致）。
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

// HideOnCloseEnv 是切换器用来指定「关窗只隐藏」的环境变量名。
//
// 名字在 desktop-dual 的 switchboard 里另有一份字面量：两个模块是各自独立的 Go 模块，
// 切换器无法 import 本包（internal 也不允许跨模块），共享常量需要一个第三方模块，代价
// 大于一个字符串。两边各写一份并互相点名，各自再有一个用例把这个字面量钉死——改名时
// 至少会有一个模块的测试失败，而不是两边悄悄错开、表现为「托盘点了没反应」。
const HideOnCloseEnv = "DSH_DESKTOP_HIDE_ON_CLOSE"

// CloseQuitsEnv 强制「关窗即退出」，即使托盘可用。
//
// 存在理由：托盘常驻对大多数用户是期望行为，但排障或刻意单窗口使用时需要让关窗
// 真的结束进程，否则用户会以为窗口关掉了、进程却还在。
const CloseQuitsEnv = "DSH_DESKTOP_CLOSE_QUITS"

// ResolveCloseAction 依据环境变量与托盘可用性决定关窗行为。
//
// 优先级：
//  1. DSH_DESKTOP_CLOSE_QUITS=1：强制退出（排障/单窗口用法）；
//  2. DSH_DESKTOP_HIDE_ON_CLOSE=1：隐藏窗口（dual 包的既有约定，保持兼容）；
//  3. 托盘可用：隐藏窗口——有托盘就有第二个入口把窗口叫回来；
//  4. 其余（含托盘创建失败）：关窗即退出——藏起来的窗口没有入口，比直接退出更糟。
//
// @param hideOnClose - DSH_DESKTOP_HIDE_ON_CLOSE 是否为 "1"。
// @param forceQuit - DSH_DESKTOP_CLOSE_QUITS 是否为 "1"。
// @param trayAvailable - 托盘是否创建成功。
// @returns 关窗行为。
func ResolveCloseAction(hideOnClose, forceQuit, trayAvailable bool) CloseAction {
	if forceQuit {
		return CloseQuits
	}
	if hideOnClose || trayAvailable {
		return CloseHides
	}
	return CloseQuits
}

// CloseActionFromEnv 解析显式的环境变量覆盖（不含托盘可用性这一维）。
//
// 调用方在托盘创建完成后用 ResolveCloseAction 合成最终行为；保留本函数是为了让
// 「环境变量怎么解析」这件事能被单独断言。
// @returns 环境变量给出的覆盖：显式退出、显式隐藏，或未指定。
func CloseActionFromEnv() (CloseAction, bool) {
	if os.Getenv(CloseQuitsEnv) == "1" {
		return CloseQuits, true
	}
	if os.Getenv(HideOnCloseEnv) == "1" {
		return CloseHides, true
	}
	return CloseQuits, false
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

// ShowWindow 把窗口显示出来并取消最小化，是「关窗后唤回」的落点。
//
// 窗口尚未就绪时（ServiceStartup 还没跑过，showCtx 仍为 nil）静默返回：切换器可以在拉起
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
	windowUnminimise()
	windowShow()
}

// ShowSocketRel 是唤回通道在用户家目录下的相对路径。
//
// 为什么不用信号：SIGUSR1 在 WebKit 里已经被 JavaScriptCore 拿走做 GC 线程的挂起与恢复
// （Wails 的 Linux 前端在 install_signal_handlers 里专门注明：一旦 JSC 装上自己的
// SIGUSR1 处理器，Go 就收不到它了）。发出去的那一下因此不是「显示窗口」，而是打进 JSC 的
// GC 机制——实测客户端进程当场消失、没有任何日志，harness 变成孤儿，用户看到的是
// 「点一次没反应、点第二次才开，而且开的是新进程」。换成另一个信号只是把同一个赌注再押
// 一次：第三方库会占用哪些信号，是我们无法枚举的。
//
// 名字在 desktop-dual 的 switchboard 里另有一份字面量：两个模块各自独立，切换器无法
// import 本包，共享常量要引入第三方模块。两边各写一份并互相点名，各自再有一个用例把
// 字面量钉死——改名时至少有一个模块的测试失败，而不是两边悄悄错开、表现为「点托盘没反应」。
const ShowSocketRel = ".cache/dsh-desktop/show.sock"

// showRequestTimeout 是单个唤回请求从连上到读完的超时。
//
// 设上限只为防御：切换器连上就写一行然后关闭，正常路径立刻 EOF。没有它，一个连上却不
// 发数据也不关闭的客户端会把 accept 循环堵在这里，后续唤回全部排队。
const showRequestTimeout = 2 * time.Second

// acceptRetryDelay 是 accept 遇到瞬时错误后的退避。
//
// 存在的理由与「不能直接 return」相同：句柄耗尽这类错误会反复出现，不退避就变成忙等，
// 而退出则让通道假活（见 ServeShowRequests 里的说明）。
const acceptRetryDelay = 100 * time.Millisecond

// ShowSocketPath 把唤回通道的相对路径拼成绝对路径。
// @param home - 用户主目录。
// @returns 唤回通道的绝对路径。
func ShowSocketPath(home string) string {
	return filepath.Join(home, filepath.FromSlash(ShowSocketRel))
}

// ServeShowRequests 在 path 上监听唤回请求：每收到一次请求就执行 show，直到 stop 被调用。
//
// 协议刻意做到最简——连上、写任意内容、关闭即表示唤回，因此没有版本协商，也没有解析
// 失败的分支要维护。服务端会读到 EOF 再动作，这样客户端「写成功」才是有意义的确认，
// 而不是在服务端提前关闭时拿到一个假的 EPIPE。
//
// 监听建立失败不拦启动：客户端照常可用，只是这一次运行的唤回会退化为「切换器报错」，
// 而不是悄悄再拉起一个客户端。调用方据此决定是记日志还是放弃。
// @param path - socket 绝对路径；父目录不存在时会创建。
// @param show - 收到请求时执行的动作；为 nil 时只消费请求。
// @returns stop - 停止监听（幂等）；err - 监听建立失败的原因。
func ServeShowRequests(path string, show func()) (stop func(), err error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	// 先删掉同名文件再 bind：进程被 SIGKILL 或走 os.Exit 收尾时不会执行 stop，上一次运行
	// 留下的 socket 文件会让 bind 直接 EADDRINUSE，而那种状态表现为「客户端从此唤不回来」，
	// 所以清理是常规路径而不是异常补救。
	//
	// 代价是「后启动的那个客户端会抢走通道」：若真有另一个客户端同时在跑（本进程没有单实例
	// 机制），它原有的监听还在、文件已被换成新的，托盘从此只唤得到新的那个。这种情形本身
	// 就是另一个缺陷（见 docs/AUDIT.md 的第 36 条），两条路里选「最新启动的客户端可达」比
	// 「谁都连不上」好。
	_ = os.Remove(path)
	ln, err := net.Listen("unix", path)
	if err != nil {
		return nil, err
	}
	// 关掉 Go 自带的「Close 时删路径」：它删的是路径而不是自己那个 inode。若另一个客户端
	// 后来抢占了同一路径（见上面对抢占的说明），本进程退出就会把别人的通道文件删掉，
	// 结果是两个都唤不回来。陈旧文件统一交给下次 bind 之前的那次删除处理。
	if ul, ok := ln.(*net.UnixListener); ok {
		ul.SetUnlinkOnClose(false)
	}
	// 家目录通常已是 0700，这里再收一次：唤回通道能让窗口弹出来，不该开放给别的本地用户。
	_ = os.Chmod(path, 0o600)

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				// 只有「监听被 stop 关掉」才是收工。其余都是瞬时错误（句柄耗尽、连接在
				// accept 之前被对端放弃等）；直接退出会让通道变成假活——listener 还开着、
				// 文件还在，拨号因此仍然成功，切换器据此认为唤回成功，而窗口纹丝不动，
				// 用户看到的就是「点了没反应，也没有任何报错」。
				if errors.Is(err, net.ErrClosed) {
					return
				}
				time.Sleep(acceptRetryDelay)
				continue
			}
			_ = conn.SetDeadline(time.Now().Add(showRequestTimeout))
			_, _ = io.Copy(io.Discard, conn)
			_ = conn.Close()
			if show != nil {
				show()
			}
		}
	}()

	var once sync.Once
	return func() {
		once.Do(func() { _ = ln.Close() })
	}, nil
}
