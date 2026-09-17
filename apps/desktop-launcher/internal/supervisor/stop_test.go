package supervisor

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/deepseek-ai/deepseek-harness/apps/desktop-launcher/internal/domain"
)

// TestSupervisor_LogsExitReason 回归：harness 正常退出后，supervisor 应把
// 退出原因（code/signal）写入 harness.log，这是诊断静默死亡的关键。
func TestSupervisor_LogsExitReason(t *testing.T) {
	cfg := Config{Command: "sh", Args: []string{"testdata/mock-clean-exit.sh"}, LogDir: t.TempDir()}
	sup := NewSupervisor(cfg, DefaultOptions())
	sup.Start()

	select {
	case <-sup.Ready():
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for ready")
	}

	time.Sleep(2 * time.Second) // harness 已 exit 0，等 run() 回收并写日志

	data, err := os.ReadFile(filepath.Join(cfg.LogDir, "harness.log"))
	if err != nil {
		t.Fatalf("read log: %v", err)
	}
	if !strings.Contains(string(data), "[supervisor] harness exited code=0") {
		t.Fatalf("日志缺少退出原因记录:\n%s", data)
	}
}

// TestSupervisor_StopBoundedWithEscapedGrandchild 回归：孙进程逃逸到独立
// 进程组并持有 stdout 管道时，cmd.Wait() 可能永不返回；Stop() 必须在
// 有界时间内返回，否则窗口关闭后 launcher 无法退出（进程泄漏）。
func TestSupervisor_StopBoundedWithEscapedGrandchild(t *testing.T) {
	cfg := Config{Command: "sh", Args: []string{"testdata/mock-orphan-alive.sh"}, LogDir: t.TempDir()}
	opts := DefaultOptions()
	opts.KillTimeoutMs = 300 // 缩小超时，加快测试
	sup := NewSupervisor(cfg, opts)
	sup.Start()

	select {
	case <-sup.Ready():
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for ready")
	}

	done := make(chan struct{})
	go func() {
		sup.Stop()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Stop() 未在 5 秒内有界返回 —— launcher 将无法退出")
	}
}

// TestSupervisor_ClearsExitedCmd 回归：子进程退出并被 Wait 回收后，supervisor
// 必须清掉 s.cmd。Restart 与 StopHarness 都会对 s.cmd 的进程组发 SIGTERM/SIGKILL，
// 留着已回收的 cmd 会在 PID 回绕后把信号打到无关进程组上。
func TestSupervisor_ClearsExitedCmd(t *testing.T) {
	cfg := Config{Command: "sh", Args: []string{"testdata/mock-clean-exit.sh"}, LogDir: t.TempDir()}
	opts := DefaultOptions()
	// 把退避拉长：子进程退出后 run() 会在这个窗口里等待，不会立刻 spawn 新的，
	// 断言才不会被下一轮的 cmd 覆盖。
	opts.RestartDelayMs = 60000
	opts.MaxRestartDelayMs = 60000
	sup := NewSupervisor(cfg, opts)
	defer sup.Stop()
	sup.Start()

	select {
	case <-sup.Ready():
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for ready")
	}

	// 阶段一：先等子进程被真正拉起。否则"cmd 为 nil"可能只是还没 spawn，
	// 用例会在什么都没验证的情况下通过。
	if !waitForCondition(5*time.Second, func() bool {
		sup.mu.Lock()
		defer sup.mu.Unlock()
		return sup.cmd != nil
	}) {
		t.Fatal("子进程未在 5 秒内拉起")
	}

	// 阶段二：子进程退出并被 Wait 回收后，cmd 引用必须一并清掉。
	deadline := time.Now().Add(5 * time.Second)
	for {
		sup.mu.Lock()
		cmd, pid, state := sup.cmd, sup.pid, sup.state
		sup.mu.Unlock()
		if cmd == nil {
			return // 已清理
		}
		if pid == 0 && state == domain.StateStopped {
			t.Fatalf("子进程已退出并被回收（state=%v pid=0）但 supervisor 仍持有 cmd：Restart/StopHarness 会按它发信号，PID 回绕后误杀无关进程组", state)
		}
		if time.Now().After(deadline) {
			t.Fatal("等待子进程退出超时")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// waitForCondition 轮询 cond 直到为真或超时，返回是否在超时前成立。
func waitForCondition(timeout time.Duration, cond func() bool) bool {
	deadline := time.Now().Add(timeout)
	for {
		if cond() {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(5 * time.Millisecond)
	}
}
