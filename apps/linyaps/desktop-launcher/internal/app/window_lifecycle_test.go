// 关窗行为与托盘唤回的用例。
//
// 这一组行为跨了两个进程（切换器连唤回 socket，本进程收下并显示窗口）与一次部署选择
// （DSH_DESKTOP_HIDE_ON_CLOSE），因此测试分三层钉住：环境变量到 closeAction 的解析、
// OnBeforeClose 两条分支、以及唤回报文本身的安全性。
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
	windowHide = func(context.Context) { calls = append(calls, "hide") }
	windowShow = func(context.Context) { calls = append(calls, "show") }
	windowUnminimise = func(context.Context) { calls = append(calls, "unminimise") }
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

// TestCloseActionFromEnv 覆盖解析：只有切换器注入的那一个值会启用隐藏，其余一切取值
// 都必须保持「关窗即退出」——本进程没有托盘，藏起来的窗口没有第二个入口能叫回来。
func TestCloseActionFromEnv(t *testing.T) {
	cases := []struct {
		name string
		set  bool
		val  string
		want CloseAction
	}{
		{"变量缺失", false, "", CloseQuits},
		{"空串", true, "", CloseQuits},
		{"切换器注入的值", true, "1", CloseHides},
		{"其它非零值不启用", true, "true", CloseQuits},
		{"0 不启用", true, "0", CloseQuits},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if c.set {
				t.Setenv(HideOnCloseEnv, c.val)
			} else {
				t.Setenv(HideOnCloseEnv, "")
				os.Unsetenv(HideOnCloseEnv)
			}
			if got := CloseActionFromEnv(); got != c.want {
				t.Fatalf("CloseActionFromEnv() = %v，期望 %v", got, c.want)
			}
		})
	}
}

// TestOnBeforeCloseHidesOnlyWhenAsked 覆盖关窗两条分支：CloseHides 下拦下这次关闭并
// 隐藏窗口（返回 true 才能取消 Wails 的 Quit），CloseQuits 下放行且不碰窗口。
func TestOnBeforeCloseHidesOnlyWhenAsked(t *testing.T) {
	calls := stubWindowActions(t)

	hiding := &App{closeAction: CloseHides}
	if !hiding.OnBeforeClose(context.Background()) {
		t.Fatal("CloseHides 下应返回 true 以取消本次关闭")
	}
	if got := calls(); len(got) != 1 || got[0] != "hide" {
		t.Fatalf("CloseHides 下窗口调用 = %v，期望只有一次 hide", got)
	}

	quitting := &App{closeAction: CloseQuits}
	if quitting.OnBeforeClose(context.Background()) {
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

// TestServeShowRequestsStopRemovesSocket 覆盖停止后通道确实消失：否则切换器会连上一个
// 没有监听者的文件，把「客户端已退出」误判成「唤回成功」，窗口不动而用户以为点了没用。
func TestServeShowRequestsStopRemovesSocket(t *testing.T) {
	path := filepath.Join(t.TempDir(), "show.sock")
	stop, err := ServeShowRequests(path, nil)
	if err != nil {
		t.Fatalf("建立唤回通道失败: %v", err)
	}
	stop()
	stop() // 幂等
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("stop 之后 socket 文件应被删除, stat err = %v", err)
	}
}
