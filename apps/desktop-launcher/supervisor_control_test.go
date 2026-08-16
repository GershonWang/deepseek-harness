package main

import (
	"strings"
	"testing"
	"time"
)

// waitState 轮询直到 Status 达到指定状态,超时失败。
func waitState(t *testing.T, sup *Supervisor, want HarnessState) HarnessStatus {
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
	env := DesktopEnv{Command: "sh", Args: []string{"testdata/mock-dsh-web.sh"}, LogDir: t.TempDir(), Port: "0"}
	sup := NewSupervisor(env, DefaultSupervisorOptions())
	sup.Start()
	st := waitState(t, sup, StateRunning)
	if !strings.Contains(st.URL, "127.0.0.1:18080") {
		t.Fatalf("URL 应为 mock 端口,got %q", st.URL)
	}
	sup.StopHarness()
	st = waitState(t, sup, StateStopped)
	if !strings.Contains(st.LastExit, "signal=terminated") {
		t.Fatalf("LastExit 应为 SIGTERM 信号,got %q", st.LastExit)
	}
	sup.Stop()
	sup.Wait()
}

func TestSupervisor_LastExitRecordsExitCode(t *testing.T) {
	env := DesktopEnv{Command: "sh", Args: []string{"testdata/mock-exit-3.sh"}, LogDir: t.TempDir(), Port: "0"}
	sup := NewSupervisor(env, DefaultSupervisorOptions())
	sup.Start()
	st := waitState(t, sup, StateStopped)
	if !strings.Contains(st.LastExit, "exited code=3") {
		t.Fatalf("LastExit 应为 exited code=3,got %q", st.LastExit)
	}
	sup.Stop()
	sup.Wait()
}

func TestSupervisor_ManualStopPausesAutoRestart(t *testing.T) {
	env := DesktopEnv{Command: "sh", Args: []string{"testdata/mock-dsh-web.sh"}, LogDir: t.TempDir(), Port: "0"}
	sup := NewSupervisor(env, DefaultSupervisorOptions())
	sup.Start()
	waitState(t, sup, StateRunning)
	sup.StopHarness()
	waitState(t, sup, StateStopped)
	// 超过首个退避(500ms)后仍应保持 stopped,不被自动重启
	time.Sleep(1500 * time.Millisecond)
	if st := sup.Status(); st.State != StateStopped {
		t.Fatalf("手动停止后不应自动重启,state=%v", st.State)
	}
	// Start() 恢复运行
	sup.Start()
	waitState(t, sup, StateRunning)
	sup.Stop()
	sup.Wait()
}

func TestSupervisor_RestartRespawns(t *testing.T) {
	env := DesktopEnv{Command: "sh", Args: []string{"testdata/mock-dsh-web.sh"}, LogDir: t.TempDir(), Port: "0"}
	sup := NewSupervisor(env, DefaultSupervisorOptions())
	sup.Start()
	old := waitState(t, sup, StateRunning)
	sup.Restart()
	deadline := time.Now().Add(5 * time.Second)
	for {
		st := sup.Status()
		if st.State == StateRunning && st.PID != old.PID {
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

func TestSupervisor_StartWhileRunningIsNoop(t *testing.T) {
	env := DesktopEnv{Command: "sh", Args: []string{"testdata/mock-dsh-web.sh"}, LogDir: t.TempDir(), Port: "0"}
	sup := NewSupervisor(env, DefaultSupervisorOptions())
	sup.Start()
	old := waitState(t, sup, StateRunning)
	sup.Start()
	time.Sleep(200 * time.Millisecond)
	if st := sup.Status(); st.PID != old.PID {
		t.Fatalf("运行中 Start() 不应换进程,old=%d current=%d", old.PID, st.PID)
	}
	sup.Stop()
	sup.Wait()
}
