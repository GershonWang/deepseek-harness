// 门控首次 spawn 与按次环境注入的单测：两条路径都是启动前预检的依赖——
// 门控让预检先于 harness 启动完成，SetEnv 支撑「全新运行时目录」降级启动。
package supervisor

import (
	"os"
	"testing"
	"time"

	"github.com/deepseek-ai/deepseek-harness/apps/desktop-launcher/internal/domain"
)

// TestGate_BlocksUntilRelease 门控语义：Gate 后监护循环挂起、不产生子进程；
// Release 后首次 spawn 正常发生并进入运行态。
func TestGate_BlocksUntilRelease(t *testing.T) {
	sup := NewSupervisor(mockCfg(t, "testdata/mock-dsh-web-env.sh"), DefaultOptions())
	sup.Gate()

	// 门控期间不应有子进程：等待足够长（远大于正常 spawn 即刻可达的 Starting）。
	time.Sleep(300 * time.Millisecond)
	if st := sup.Status(); st.PID != 0 {
		t.Fatalf("gate 后不应 spawn 子进程,pid=%d", st.PID)
	}

	sup.Release()
	waitState(t, sup, domain.StateRunning)
	sup.Stop()
	sup.Wait()
}

// TestGate_ReleaseWithoutGateIsNoop 未门控时 Release 是无害 no-op：监护循环
// 直接 spawn，不因等待一个永不到来的 token 而卡死。
func TestGate_ReleaseWithoutGateIsNoop(t *testing.T) {
	sup := NewSupervisor(mockCfg(t, "testdata/mock-dsh-web-env.sh"), DefaultOptions())
	sup.Release()
	waitState(t, sup, domain.StateRunning)
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
