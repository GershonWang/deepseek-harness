// 本文件收敛「关窗之后如何把窗口唤回来」这一组动作：窗口层的注入点、供托盘触发的
// ShowWindow，以及切换器用来触发它的本地通道。
//
// 为什么单独成文件：关窗行为现在由 closeAction 分叉（见 app.go 的 OnBeforeClose），
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
