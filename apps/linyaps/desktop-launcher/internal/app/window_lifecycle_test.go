// 关窗行为与托盘唤回的用例。
//
// 这一组行为跨了两个进程（切换器发 SIGUSR1，本进程收下并显示窗口）与一次部署选择
// （DSH_DESKTOP_HIDE_ON_CLOSE），因此测试分三层钉住：环境变量到 closeAction 的解析、
// OnBeforeClose 两条分支、以及唤回报文本身的安全性。
package app

import (
	"context"
	"os"
	"syscall"
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

// TestWatchShowSignalInvokesAction 覆盖信号到动作的接线，以及通道关闭后消费协程退出。
func TestWatchShowSignalInvokesAction(t *testing.T) {
	signals := make(chan os.Signal, 1)
	called := make(chan struct{})
	go WatchShowSignal(signals, func() { close(called) })
	signals <- syscall.SIGUSR1

	select {
	case <-called:
	case <-time.After(2 * time.Second):
		t.Fatal("收到 SIGUSR1 后未执行唤回动作")
	}

	// 没有动作时也必须继续消费：否则信号会堆在通道里，看起来「注册了却不生效」。
	closed := make(chan os.Signal, 1)
	done := make(chan struct{})
	go func() { WatchShowSignal(closed, nil); close(done) }()
	closed <- syscall.SIGUSR1
	close(closed)
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("通道关闭后 WatchShowSignal 应退出")
	}
}
