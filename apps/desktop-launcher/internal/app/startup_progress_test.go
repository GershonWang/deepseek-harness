package app

import (
	"testing"
	"time"

	"github.com/deepseek-ai/deepseek-harness/apps/desktop-launcher/internal/domain"
)

// 阶段判定是加载页唯一的真相来源：事件与 1s 快照都走它。这里逐档锁定"哪些事实
// 组合出哪个阶段"，尤其是"没有上报时不得给出计数"这条——否则就是假进度。
func TestStartupView(t *testing.T) {
	now := time.Now()
	output := now.Add(time.Second)
	cases := []struct {
		name  string
		state domain.HarnessState
		p     domain.StartupProgress
		want  StartupView
	}{
		{
			name:  "非启动态一律零值",
			state: domain.StateRunning,
			p:     domain.StartupProgress{Reported: true, Loaded: 7, Total: 7, StartedAt: now, OutputAt: output},
			want:  StartupView{},
		},
		{
			name:  "停止态即使是零值也不显示加载页",
			state: domain.StateStopped,
			want:  StartupView{},
		},
		{
			name:  "已拉起但还没有输出",
			state: domain.StateStarting,
			p:     domain.StartupProgress{StartedAt: now},
			want:  StartupView{Phase: StartupPhaseStarting},
		},
		{
			name:  "已有输出但还没有计数",
			state: domain.StateStarting,
			p:     domain.StartupProgress{StartedAt: now, OutputAt: output},
			want:  StartupView{Phase: StartupPhaseLoading},
		},
		{
			name:  "有计数：进入带数字的挂载阶段",
			state: domain.StateStarting,
			p:     domain.StartupProgress{StartedAt: now, OutputAt: output, Reported: true, Loaded: 12, Total: 127},
			want:  StartupView{Phase: StartupPhasePlugins, Loaded: 12, Total: 127},
		},
		{
			name:  "计数到齐：进入启动服务端口阶段",
			state: domain.StateStarting,
			p:     domain.StartupProgress{StartedAt: now, OutputAt: output, Reported: true, Loaded: 127, Total: 127},
			want:  StartupView{Phase: StartupPhaseServing, Loaded: 127, Total: 127},
		},
		{
			name:  "上报 0/0 视为没有有效分母，不进入 serving",
			state: domain.StateStarting,
			p:     domain.StartupProgress{StartedAt: now, OutputAt: output, Reported: true},
			want:  StartupView{Phase: StartupPhasePlugins},
		},
	}
	for _, c := range cases {
		if got := startupView(c.state, c.p); got != c.want {
			t.Errorf("%s: startupView = %+v, want %+v", c.name, got, c.want)
		}
	}
}

func TestStartupViewEqual(t *testing.T) {
	a := StartupView{Phase: StartupPhasePlugins, Loaded: 3, Total: 7}
	if !a.equal(a) {
		t.Error("same view must be equal")
	}
	if a.equal(StartupView{Phase: StartupPhasePlugins, Loaded: 4, Total: 7}) {
		t.Error("count change must be visible to change detection")
	}
	if a.equal(StartupView{Phase: StartupPhaseServing, Loaded: 3, Total: 7}) {
		t.Error("phase change must be visible to change detection")
	}
	// 零值可比较：running 态的零值视图与 plugins 视图不得被判为相同。
	if (StartupView{}).equal(a) {
		t.Error("zero view must differ from an active view")
	}
}

// 快照必须把启动阶段一并带给前端：加载页首帧就靠它决定文案与进度块；
// 少了这个字段的比较，进度变化就不会触发状态推送。
func TestFrontendStatus_EqualComparesStartup(t *testing.T) {
	base := FrontendStatus{State: "starting"}
	same := base
	if !base.equal(same) {
		t.Fatal("identical snapshots must compare equal")
	}
	changed := base
	changed.Startup = StartupView{Phase: StartupPhasePlugins, Loaded: 1, Total: 2}
	if base.equal(changed) {
		t.Error("FrontendStatus.equal must compare the Startup view")
	}
}
