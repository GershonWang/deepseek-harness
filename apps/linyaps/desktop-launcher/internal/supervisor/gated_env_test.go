// 门控首次 spawn 与按次环境注入的单测：两条路径都是启动前预检的依赖——
// 门控让预检先于 harness 启动完成，SetEnv 支撑「全新运行时目录」降级启动。
package supervisor

import (
	"os"
	"testing"
	"time"

	"github.com/deepseek-ai/deepseek-harness/apps/linyaps/desktop-launcher/internal/domain"
)

// gatedOptions 返回启用构造期门控的参数：门控必须在 NewSupervisor 之前确定，
// 否则监护循环会抢在置位前读到未门控值（见 Options.GateFirstStart）。
func gatedOptions() Options {
	opts := DefaultOptions()
	opts.GateFirstStart = true
	return opts
}

// TestGateFirstStart_BlocksUntilRelease 门控语义：构造期门控让监护循环挂起、
// 不产生子进程；Release 后首次 spawn 正常发生并进入运行态。
func TestGateFirstStart_BlocksUntilRelease(t *testing.T) {
	sup := NewSupervisor(mockCfg(t, "testdata/mock-dsh-web-env.sh"), gatedOptions())

	// 门控期间不应有子进程：等待足够长（远大于正常 spawn 即刻可达的 Starting）。
	time.Sleep(300 * time.Millisecond)
	if st := sup.Status(); st.PID != 0 {
		t.Fatalf("门控期不应 spawn 子进程,pid=%d", st.PID)
	}

	sup.Release()
	waitState(t, sup, domain.StateRunning)
	sup.Stop()
	sup.Wait()
}

// TestRelease_ReportsGateState 放行返回值是预检收尾区分"只需放行"与"需要重启"
// 的唯一依据：门控期返回 true，门控被消费后返回 false。
func TestRelease_ReportsGateState(t *testing.T) {
	sup := NewSupervisor(mockCfg(t, "testdata/mock-dsh-web-env.sh"), gatedOptions())
	if !sup.Release() {
		t.Fatal("门控期 Release 应返回 true")
	}
	waitState(t, sup, domain.StateRunning)
	if sup.Release() {
		t.Fatal("门控已被消费，Release 应返回 false")
	}
	sup.Stop()
	sup.Wait()
}

// TestRelease_WithoutGateIsNoop 未门控时 Release 是无害 no-op：返回 false，
// 且不改变"监护循环直接 spawn"的既有行为。
func TestRelease_WithoutGateIsNoop(t *testing.T) {
	sup := NewSupervisor(mockCfg(t, "testdata/mock-dsh-web-env.sh"), DefaultOptions())
	if sup.Release() {
		t.Fatal("未门控时 Release 应返回 false")
	}
	waitState(t, sup, domain.StateRunning)
	sup.Stop()
	sup.Wait()
}

// TestGateFirstStart_StopDuringGateDoesNotSpawn 门控期按下停止后，预检放行不得
// 反而拉起一次 harness：停止发生在门控等待之前，若沿用它之前的状态快照，
// 放行就会先 spawn 再进入停止等待——用户明确说了停止，却看到进程被拉起。
func TestGateFirstStart_StopDuringGateDoesNotSpawn(t *testing.T) {
	sup := NewSupervisor(mockCfg(t, "testdata/mock-dsh-web-env.sh"), gatedOptions())

	// 先让监护循环进入门控等待（停止快照在等待之前读取），再模拟按下停止；
	// 只有这样"停止晚于快照"的时序才覆盖到放行即拉起的旧行为。
	time.Sleep(150 * time.Millisecond)
	sup.StopHarness()
	sup.Release() // 预检放行
	time.Sleep(300 * time.Millisecond)
	if st := sup.Status(); st.PID != 0 {
		t.Fatalf("门控期停止后放行不应 spawn 子进程,pid=%d", st.PID)
	}

	sup.Stop()
	sup.Wait()
}

// waitStatePID 轮询直到状态达到 want 且 PID 不同于 before。Restart 后旧进程
// 可能尚未退出，Status 仍短暂返回旧 Running（旧 PID），仅按状态等待会读到
// 旧进程的快照——必须用 PID 变化确认新 spawn 已发生。
func waitStatePID(t *testing.T, sup *Supervisor, want domain.HarnessState, before int) domain.HarnessStatus {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		st := sup.Status()
		if st.State == want && st.PID != before {
			return st
		}
		if time.Now().After(deadline) {
			t.Fatalf("等待状态 %v (pid≠%d) 超时,当前 %+v", want, before, st)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// TestSetEnv_AppliesToNextSpawn SetEnv 注入的环境只对下一次 spawn 生效：
// canary 经就绪行 token 带回；SetEnv(nil) 恢复继承后 canary 不再出现。
func TestSetEnv_AppliesToNextSpawn(t *testing.T) {
	sup := NewSupervisor(mockCfg(t, "testdata/mock-dsh-web-env.sh"), DefaultOptions())
	sup.Start()
	st := waitState(t, sup, domain.StateRunning)
	if st.URL != "http://127.0.0.1:18080/?token=" {
		t.Fatalf("未注入环境时 canary 应为空,got %q", st.URL)
	}

	sup.SetEnv(append(os.Environ(), "DSH_TEST_CANARY=fresh-home"))
	sup.Restart()
	st = waitStatePID(t, sup, domain.StateRunning, st.PID)
	if st.URL != "http://127.0.0.1:18080/?token=fresh-home" {
		t.Fatalf("SetEnv 后 spawn 应看到注入的 canary,got %q", st.URL)
	}

	sup.SetEnv(nil)
	sup.Restart()
	st = waitStatePID(t, sup, domain.StateRunning, st.PID)
	if st.URL != "http://127.0.0.1:18080/?token=" {
		t.Fatalf("SetEnv(nil) 后应恢复继承环境,got %q", st.URL)
	}
	sup.Stop()
	sup.Wait()
}
