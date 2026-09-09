// Package terminal - 终端会话测试
package terminal

import (
	"os"
	"syscall"
	"testing"
	"time"
)

func TestSession_Close_KillsProcessGroup(t *testing.T) {
	// 启动一个 sleep 进程作为子进程（代替 bash），验证 Close 能杀掉它
	session, err := newSession("test-close", StartOptions{
		Command: "/bin/sleep",
		Args:    []string{"30"},
		Cols:    80,
		Rows:    24,
	})
	if err != nil {
		t.Fatalf("创建会话失败: %v", err)
	}

	pid := session.cmd.Process.Pid

	// 确认进程存在且在运行
	if err := syscall.Kill(pid, 0); err != nil {
		t.Fatalf("进程应该在运行: %v", err)
	}

	// 调用 Close，应该在 3 秒内返回（SIGTERM 不响应 sleep，
	// 3 秒后 SIGKILL 杀掉）
	start := time.Now()
	if err := session.Close(); err != nil {
		t.Fatalf("Close 返回错误: %v", err)
	}
	elapsed := time.Since(start)

	if elapsed > 6*time.Second {
		t.Fatalf("Close 耗时过长: %v", elapsed)
	}

	// 确认进程已不存在
	if err := syscall.Kill(pid, 0); err == nil {
		t.Fatal("进程应该已经退出，但仍然存活")
	}

	// 确认 status 是 closed
	if session.Status() != StatusClosed {
		t.Fatalf("状态应为 closed, got %v", session.Status())
	}
}

func TestSession_Close_Idempotent(t *testing.T) {
	session, err := newSession("test-idem", StartOptions{
		Command: "/bin/sleep",
		Args:    []string{"30"},
		Cols:    80,
		Rows:    24,
	})
	if err != nil {
		t.Fatalf("创建会话失败: %v", err)
	}

	// 调用两次 Close，第二次应该立即返回且不报错
	if err := session.Close(); err != nil {
		t.Fatalf("第一次 Close 失败: %v", err)
	}
	start := time.Now()
	if err := session.Close(); err != nil {
		t.Fatalf("第二次 Close 失败: %v", err)
	}
	if time.Since(start) > 500*time.Millisecond {
		t.Fatal("第二次 Close 应该立即返回")
	}
}

func TestManager_CloseAll(t *testing.T) {
	m := NewManager()

	// 创建 3 个会话
	var ids []string
	for i := 0; i < 3; i++ {
		id, err := m.Start(&StartOptions{
			Command: "/bin/sleep",
			Args:    []string{"30"},
			Cols:    80,
			Rows:    24,
		})
		if err != nil {
			t.Fatalf("创建会话 %d 失败: %v", i, err)
		}
		ids = append(ids, id)
	}

	// 确认所有会话都在运行
	for _, id := range ids {
		s, ok := m.Get(id)
		if !ok || s.Status() != StatusRunning {
			t.Fatalf("会话 %s 应该在运行", id)
		}
	}

	// CloseAll 应该能把所有进程杀掉
	start := time.Now()
	m.CloseAll()
	elapsed := time.Since(start)

	if elapsed > 8*time.Second {
		t.Fatalf("CloseAll 耗时过长: %v", elapsed)
	}

	// 验证进程不存在（通过 pid 检查）
	for _, id := range ids {
		s, ok := m.Get(id)
		if !ok {
			continue
		}
		if s.cmd.Process != nil {
			pid := s.cmd.Process.Pid
			if err := syscall.Kill(pid, 0); err == nil {
				t.Fatalf("会话 %s 的进程应该已退出", id)
			}
		}
	}
}

// 测试环境：确保有 /bin/sleep
func TestMain(m *testing.M) {
	if _, err := os.Stat("/bin/sleep"); err != nil {
		os.Exit(0) // 没有 sleep 就跳过测试
	}
	os.Exit(m.Run())
}
