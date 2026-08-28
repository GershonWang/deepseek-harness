package app

import (
	"testing"
	"time"

	"github.com/deepseek-ai/deepseek-harness/apps/desktop-launcher/internal/connector"
	"github.com/deepseek-ai/deepseek-harness/apps/desktop-launcher/internal/domain"
)

// testApp 构造一个不会真正执行 dsh 的 App：RunDoctor 命令不存在，
// exec 立即失败返回错误报告，便于在测试中同步验证自动诊断标志的转换。
func testApp() *App {
	return &App{conn: connector.New(), dshCmd: "dsh-doctor-no-such-bin"}
}

func TestMaybeStartStartupDoctor(t *testing.T) {
	cases := []struct {
		name        string
		prev, cur   domain.HarnessState
		mode        domain.Mode
		wantTrigger bool
	}{
		{"starting to failed triggers", domain.StateStarting, domain.StateFailed, domain.ModeContainer, true},
		{"running to failed triggers", domain.StateRunning, domain.StateFailed, domain.ModeContainer, true},
		{"stopped to failed triggers", domain.StateStopped, domain.StateFailed, domain.ModeContainer, true},
		{"failed to failed does not repeat", domain.StateFailed, domain.StateFailed, domain.ModeContainer, false},
		{"failed to starting is exit edge, not trigger", domain.StateFailed, domain.StateStarting, domain.ModeContainer, false},
		{"external never triggers on failed edge", domain.StateStarting, domain.StateFailed, domain.ModeExternal, false},
		{"external failed to failed no trigger", domain.StateFailed, domain.StateFailed, domain.ModeExternal, false},
		{"no state change no trigger", domain.StateStopped, domain.StateStopped, domain.ModeContainer, false},
	}
	for _, c := range cases {
		if got := maybeStartStartupDoctor(c.prev, c.cur, c.mode); got != c.wantTrigger {
			t.Errorf("%s: maybeStartStartupDoctor(%v,%v,%v) = %v, want %v",
				c.name, c.prev, c.cur, c.mode, got, c.wantTrigger)
		}
	}
}

func TestExitFailedEdge(t *testing.T) {
	cases := []struct {
		name      string
		prev, cur domain.HarnessState
		want      bool
	}{
		{"failed to starting resets", domain.StateFailed, domain.StateStarting, true},
		{"failed to running resets", domain.StateFailed, domain.StateRunning, true},
		{"failed to failed stays", domain.StateFailed, domain.StateFailed, false},
		{"starting to failed is trigger edge, not reset", domain.StateStarting, domain.StateFailed, false},
		{"stopped to stopped nothing", domain.StateStopped, domain.StateStopped, false},
	}
	for _, c := range cases {
		if got := exitFailedEdge(c.prev, c.cur); got != c.want {
			t.Errorf("%s: exitFailedEdge(%v,%v) = %v, want %v", c.name, c.prev, c.cur, got, c.want)
		}
	}
}

// doctorFlags 带锁一次性读取四个自动诊断标志。
func doctorFlags(a *App) (running, ready, doneOnce bool, errText string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.startupDoctorRunning, a.startupDoctorReady, a.startupDoctorDoneOnce, a.startupDoctorError
}

// waitStartupDoctor 轮询直到自动诊断完成（ready 且不在运行），超时失败。
func waitStartupDoctor(t *testing.T, a *App) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for {
		running, ready, _, _ := doctorFlags(a)
		if ready && !running {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("等待自动诊断完成超时: running=%v ready=%v", running, ready)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestTrackStartupDoctor_TriggerOnFailedEdge(t *testing.T) {
	a := testApp()
	a.trackStartupDoctor(domain.StateStarting, domain.StateFailed)
	// 触发是同步置位的：调用返回后 doneOnce 必为 true。
	if _, _, doneOnce, _ := doctorFlags(a); !doneOnce {
		t.Fatalf("触发后 doneOnce 应为 true")
	}
	// 后台诊断最终完成并落盘 ready。
	waitStartupDoctor(t, a)
}

func TestTrackStartupDoctor_NoRepeatWhileFailed(t *testing.T) {
	a := testApp()
	a.trackStartupDoctor(domain.StateStarting, domain.StateFailed)
	waitStartupDoctor(t, a)
	// failed→failed 不应重新触发诊断：标志保持完成态（错误触发会先置 running）。
	a.trackStartupDoctor(domain.StateFailed, domain.StateFailed)
	running, ready, doneOnce, _ := doctorFlags(a)
	if running {
		t.Fatalf("failed→failed 不应重新启动诊断, running=%v", running)
	}
	if !ready || !doneOnce {
		t.Fatalf("failed→failed 后应保持完成态: ready=%v doneOnce=%v", ready, doneOnce)
	}
}

func TestTrackStartupDoctor_ResetOnExitFailed(t *testing.T) {
	a := testApp()
	a.trackStartupDoctor(domain.StateStarting, domain.StateFailed)
	waitStartupDoctor(t, a)
	// 用户 Restart:failed→starting 应清空本周期全部诊断标志。
	a.trackStartupDoctor(domain.StateFailed, domain.StateStarting)
	running, ready, doneOnce, errText := doctorFlags(a)
	if running || ready || doneOnce || errText != "" {
		t.Fatalf("退出失败态后应重置全部诊断标志: running=%v ready=%v doneOnce=%v err=%q",
			running, ready, doneOnce, errText)
	}
}
