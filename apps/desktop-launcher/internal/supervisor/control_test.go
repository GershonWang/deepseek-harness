package supervisor

import (
	"strings"
	"testing"
	"time"

	"github.com/deepseek-ai/deepseek-harness/apps/desktop-launcher/internal/domain"
)

// mockCfg 返回以 testdata 脚本为子进程的配置。
func mockCfg(t *testing.T, script string) Config {
	t.Helper()
	return Config{Command: "sh", Args: []string{script}, LogDir: t.TempDir()}
}

// waitState 轮询直到 Status 达到指定状态，超时失败。
func waitState(t *testing.T, sup *Supervisor, want domain.HarnessState) domain.HarnessStatus {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		st := sup.Status()
		if st.State == want {
			return st
		}
		if time.Now().After(deadline) {
			t.Fatalf("等待状态 %v 超时,当前 %v (%+v)", want, st.State, st)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestSupervisor_StatusRunningThenStopped(t *testing.T) {
	sup := NewSupervisor(mockCfg(t, "testdata/mock-dsh-web.sh"), DefaultOptions())
	sup.Start()
	st := waitState(t, sup, domain.StateRunning)
	if !strings.Contains(st.URL, "127.0.0.1:18080") {
		t.Fatalf("URL 应为 mock 端口,got %q", st.URL)
	}
	sup.StopHarness()
	st = waitState(t, sup, domain.StateStopped)
	if !strings.Contains(st.LastExit, "signal=terminated") {
		t.Fatalf("LastExit 应为 SIGTERM 信号,got %q", st.LastExit)
	}
	sup.Stop()
	sup.Wait()
}

func TestSupervisor_LastExitRecordsExitCode(t *testing.T) {
	sup := NewSupervisor(mockCfg(t, "testdata/mock-exit-3.sh"), DefaultOptions())
	sup.Start()
	st := waitState(t, sup, domain.StateStopped)
	if !strings.Contains(st.LastExit, "exited code=3") {
		t.Fatalf("LastExit 应为 exited code=3,got %q", st.LastExit)
	}
	sup.Stop()
	sup.Wait()
}

func TestSupervisor_ManualStopPausesAutoRestart(t *testing.T) {
	sup := NewSupervisor(mockCfg(t, "testdata/mock-dsh-web.sh"), DefaultOptions())
	sup.Start()
	waitState(t, sup, domain.StateRunning)
	sup.StopHarness()
	waitState(t, sup, domain.StateStopped)
	// 超过首个退避(500ms)后仍应保持 stopped,不被自动重启。
	time.Sleep(1500 * time.Millisecond)
	if st := sup.Status(); st.State != domain.StateStopped {
		t.Fatalf("手动停止后不应自动重启,state=%v", st.State)
	}
	// Start() 恢复运行。
	sup.Start()
	waitState(t, sup, domain.StateRunning)
	sup.Stop()
	sup.Wait()
}

func TestSupervisor_RestartRespawns(t *testing.T) {
	sup := NewSupervisor(mockCfg(t, "testdata/mock-dsh-web.sh"), DefaultOptions())
	sup.Start()
	old := waitState(t, sup, domain.StateRunning)
	sup.Restart()
	deadline := time.Now().Add(5 * time.Second)
	for {
		st := sup.Status()
		if st.State == domain.StateRunning && st.PID != old.PID {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("Restart 后 PID 未变化,old=%d current=%d", old.PID, sup.Status().PID)
		}
		time.Sleep(20 * time.Millisecond)
	}
	sup.Stop()
	sup.Wait()
}

func TestSupervisor_FailedSpawnMarksStopped(t *testing.T) {
	// 持久启动失败(如二进制缺失)必须落在 StateStopped 并记录原因，
	// 否则状态卡在 StateStarting 会死锁手动停止路径、Start() 无法恢复。
	cfg := Config{Command: "nonexistent-binary-xyz", LogDir: t.TempDir()}
	sup := NewSupervisor(cfg, DefaultOptions())
	st := waitState(t, sup, domain.StateStopped)
	if !strings.Contains(st.LastExit, "start failed") {
		t.Fatalf("LastExit 应含 start failed,got %q", st.LastExit)
	}
	sup.Stop()
	sup.Wait()
}

func TestSupervisor_StartWhileRunningIsNoop(t *testing.T) {
	sup := NewSupervisor(mockCfg(t, "testdata/mock-dsh-web.sh"), DefaultOptions())
	sup.Start()
	old := waitState(t, sup, domain.StateRunning)
	sup.Start()
	time.Sleep(200 * time.Millisecond)
	if st := sup.Status(); st.PID != old.PID {
		t.Fatalf("运行中 Start() 不应换进程,old=%d current=%d", old.PID, st.PID)
	}
	sup.Stop()
	sup.Wait()
}

// TestSupervisor_StartupFailureGivesUp 回归：harness 持续启动失败（从未就绪）累计
// 超过 StartupTimeoutMs 后应进入失败态停止自动重试，而不是"启动中"无限卡死；
// Start() 可从失败态唤醒重试。
func TestSupervisor_StartupFailureGivesUp(t *testing.T) {
	opts := DefaultOptions()
	opts.StartupTimeoutMs = 100 // 缩小超时，加快测试
	sup := NewSupervisor(mockCfg(t, "testdata/mock-fail-start.sh"), opts)

	st := waitState(t, sup, domain.StateFailed)
	if st.LastExit == "" {
		t.Fatalf("失败态应保留退出原因,got %q", st.LastExit)
	}
	// 失败态不再自动重启。
	time.Sleep(1500 * time.Millisecond)
	if got := sup.Status(); got.State != domain.StateFailed {
		t.Fatalf("失败态不应自动重启,state=%v", got.State)
	}
	// Start() 唤醒重试：状态应短暂离开 Failed（进入 Starting/Stopped）。
	sup.Start()
	deadline := time.Now().Add(3 * time.Second)
	for {
		if got := sup.Status(); got.State != domain.StateFailed {
			break // 已重新尝试
		}
		if time.Now().After(deadline) {
			t.Fatal("Start() 后未重新尝试（状态仍为 Failed）")
		}
		time.Sleep(5 * time.Millisecond)
	}
	sup.Stop()
	sup.Wait()
}

// TestSupervisor_FatalLoadFailsFast 回归：stderr 出现确定性加载失败特征时，
// 即使 StartupTimeoutMs 未到也应立即进入失败态，而不是继续退避重试——
// 坏插件每轮加载整个插件树耗时数秒，等待熔断会让用户看到"启动几秒后
// 停止、重复数次"的卡顿。
func TestSupervisor_FatalLoadFailsFast(t *testing.T) {
	// 默认 StartupTimeoutMs=30000:若快速失败路径未生效,2 秒内不可能进入 Failed。
	sup := NewSupervisor(mockCfg(t, "testdata/mock-fail-plugin.sh"), DefaultOptions())
	st := waitState(t, sup, domain.StateFailed)
	if st.LastExit == "" {
		t.Fatalf("失败态应保留退出原因,got %q", st.LastExit)
	}
	sup.Stop()
	sup.Wait()
}

// TestSupervisor_NoFatalFeatureKeepsRetrying 对照：stderr 无失败特征时快速失败
// 路径不触发——默认熔断期内仍在退避重试（尚未进入 Failed），保证快速失败只
// 针对确定性特征，不误伤一般的启动失败。
func TestSupervisor_NoFatalFeatureKeepsRetrying(t *testing.T) {
	sup := NewSupervisor(mockCfg(t, "testdata/mock-fail-start.sh"), DefaultOptions())
	// mock-fail-start.sh 立即退出(无特征),默认熔断 30s;快速失败仅在有特征时生效,
	// 因此短暂观察期后状态不应是 Failed。
	deadline := time.Now().Add(1500 * time.Millisecond)
	for time.Now().Before(deadline) {
		if got := sup.Status(); got.State == domain.StateFailed {
			t.Fatalf("无失败特征不应快速进入 Failed,state=%v", got.State)
		}
		time.Sleep(20 * time.Millisecond)
	}
	sup.Stop()
	sup.Wait()
}
