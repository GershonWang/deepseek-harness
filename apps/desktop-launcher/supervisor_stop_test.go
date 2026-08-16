package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestSupervisor_LogsExitReason 回归：harness 正常退出后，supervisor 应把
// 退出原因（code/signal）写入 harness.log，这是诊断静默死亡的关键。
func TestSupervisor_LogsExitReason(t *testing.T) {
	env := DesktopEnv{
		Command: "sh",
		Args:    []string{"testdata/mock-clean-exit.sh"},
		LogDir:  t.TempDir(),
		Port:    "0",
	}
	sup := NewSupervisor(env, DefaultSupervisorOptions())
	sup.Start()

	select {
	case <-sup.Ready():
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for ready")
	}

	time.Sleep(2 * time.Second) // harness 已 exit 0，等 run() 回收并写日志

	logPath := filepath.Join(env.LogDir, "harness.log")
	data, err := os.ReadFile(logPath)
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
	env := DesktopEnv{
		Command: "sh",
		Args:    []string{"testdata/mock-orphan-alive.sh"},
		LogDir:  t.TempDir(),
		Port:    "0",
	}
	opts := DefaultSupervisorOptions()
	opts.KillTimeoutMs = 300 // 缩小超时，加快测试
	sup := NewSupervisor(env, opts)
	sup.Start()

	select {
	case <-sup.Ready():
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for ready")
	}

	// harness 仍存活（mock 父进程 sleep 300），此时 Stop() 只能靠有界等待
	// 脱离卡住的 cmd.Wait()。有界上限：SIGTERM 阶段 + SIGKILL 阶段各
	// KillTimeoutMs，再加缓冲。
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
