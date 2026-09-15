// 加载页的启动进度视图：把 supervisor 观测到的启动事实映射成前端可渲染的阶段。
//
// 为什么单独成文件：与 preflight.go 同构——app.go 只持有快照字段与装配，阶段判定
// （哪些事实组合成哪个阶段）集中在这里，便于单测覆盖"没有上报时不得编造进度"这条
// 约束。加载页此前只有 spinner，等待期被感知为"多了一屏空等"，本文件提供替代它的
// 真实阶段与真实计数。
package app

import (
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"github.com/deepseek-ai/deepseek-harness/apps/desktop-launcher/internal/domain"
)

// 加载页阶段（StartupView.Phase 的取值）。四个阶段各由一个可观测事实触发：
// 进程已拉起、子进程已有输出、收到条目计数、条目全部激活。没有按时间猜测的
// 中间态——宁可退到粗粒度阶段（前两个），也不给假进度。
const (
	// StartupPhaseStarting：已拉起 harness，尚未收到任何输出。
	StartupPhaseStarting = "starting"
	// StartupPhaseLoading：子进程已有输出，尚未收到条目计数上报。
	StartupPhaseLoading = "loading"
	// StartupPhasePlugins：正在挂载插件，Loaded/Total 是上报的实时计数。
	StartupPhasePlugins = "plugins"
	// StartupPhaseServing：条目全部激活，只剩监听端口与就绪行。
	StartupPhaseServing = "serving"
)

// startupProgressEventInterval 限制进度事件的推送频率：上报行约为每个条目一条
// （同一瞬间可能连续到达多条），节流到 100ms 既让进度条平滑，又不会把 Wails 的
// 事件通道打满。阶段切换到 serving 不受节流限制——那是最后一段等待的唯一反馈。
const startupProgressEventInterval = 100 * time.Millisecond

// StartupView 是加载页的启动进度视图：Phase 决定文案，Loaded/Total 决定进度条。
// 零值表示"当前不在启动态"，前端据此不渲染加载页。
type StartupView struct {
	Phase  string
	Loaded int
	Total  int
}

// equal 支持状态快照与事件的变化检测；零值可正确比较。
func (v StartupView) equal(o StartupView) bool {
	return v.Phase == o.Phase && v.Loaded == o.Loaded && v.Total == o.Total
}

// startupView 把启动事实映射成加载页阶段；非启动态返回零值。
// 纯函数便于单测：非启动态与零值输入都必须得到零值输出，否则停止/运行态会被
// 误判成"启动中"而弹出加载页。
func startupView(state domain.HarnessState, p domain.StartupProgress) StartupView {
	if state != domain.StateStarting {
		return StartupView{}
	}
	switch {
	case p.Reported && p.Total > 0 && p.Loaded >= p.Total:
		return StartupView{Phase: StartupPhaseServing, Loaded: p.Loaded, Total: p.Total}
	case p.Reported:
		return StartupView{Phase: StartupPhasePlugins, Loaded: p.Loaded, Total: p.Total}
	case !p.OutputAt.IsZero():
		return StartupView{Phase: StartupPhaseLoading}
	default:
		return StartupView{Phase: StartupPhaseStarting}
	}
}

// currentStartupView 读取当前启动进度视图（快照装配与事件推送共用同一判定）。
func (a *App) currentStartupView() StartupView {
	return startupView(a.sup.Status().State, a.sup.StartupProgress())
}

// emitStartupProgress 在上报到达时推送一次进度事件。
//
// 为什么不只依赖 1s 状态轮询：条目激活在 2-4 秒内产生上百次计数变化，1s 轮询只能
// 采到一两个点，加载页会跳变而不是走条。事件与快照共用 startupView 一个判定来源，
// 因此前端两条通道渲染出的阶段永远一致。
func (a *App) emitStartupProgress() {
	if a.ctx == nil {
		return
	}
	view := a.currentStartupView()
	a.mu.Lock()
	unchanged := view.equal(a.startupLastView)
	throttled := view.Phase != StartupPhaseServing &&
		time.Since(a.startupEmittedAt) < startupProgressEventInterval
	if !unchanged && !throttled {
		a.startupLastView = view
		a.startupEmittedAt = time.Now()
	}
	a.mu.Unlock()
	// 被节流时有意不更新 startupLastView：下一帧若仍是同一视图，仍会被推送。
	if unchanged || throttled {
		return
	}
	runtime.EventsEmit(a.ctx, StartupProgressEvent, view)
}
