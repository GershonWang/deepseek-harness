// Package terminal - 终端会话测试
package terminal

import (
	"os"
	"strings"
	"syscall"
	"testing"
	"time"
)

// TestEnsureSessionEnv 覆盖终端子进程环境的兜底规则：
// 缺失时注入、非空时保留、空值剔除，且 SHELL/TERM 各恰好一条。
func TestEnsureSessionEnv(t *testing.T) {
	cases := []struct {
		name      string
		base      []string
		shell     string
		wantShell string // 期望的唯一 SHELL 条目
		wantTerm  string // 期望的唯一 TERM 条目
	}{
		{
			// 玲珑启动 GUI 应用不携带 SHELL/TERM，两者都应兜底
			name:      "玲珑环境缺 SHELL 与 TERM 时都注入",
			base:      []string{"PATH=/bin", "HOME=/root"},
			shell:     "/bin/bash",
			wantShell: "SHELL=/bin/bash",
			wantTerm:  "TERM=xterm-256color",
		},
		{
			// 开发态从桌面会话继承的非空值不应被覆盖
			name:      "已有非空 SHELL 与 TERM 时保持不动",
			base:      []string{"PATH=/bin", "SHELL=/usr/bin/fish", "TERM=foot"},
			shell:     "/bin/bash",
			wantShell: "SHELL=/usr/bin/fish",
			wantTerm:  "TERM=foot",
		},
		{
			// 空值条目在重复时会让 getenv 兜底失效，必须替换为可用值
			name:      "空值 SHELL 条目被兜底替换",
			base:      []string{"SHELL=", "PATH=/bin"},
			shell:     "/bin/bash",
			wantShell: "SHELL=/bin/bash",
			wantTerm:  "TERM=xterm-256color",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ensureSessionEnv(tc.base, tc.shell)
			countPrefixed := func(prefix string) int {
				n := 0
				for _, e := range got {
					if strings.HasPrefix(e, prefix) {
						n++
					}
				}
				return n
			}
			if n := countPrefixed("SHELL="); n != 1 {
				t.Fatalf("SHELL 应恰好一条, got %d: %v", n, got)
			}
			if n := countPrefixed("TERM="); n != 1 {
				t.Fatalf("TERM 应恰好一条, got %d: %v", n, got)
			}
			for _, e := range got {
				if e == tc.wantShell || e == tc.wantTerm {
					continue
				}
				if strings.HasPrefix(e, "SHELL=") || strings.HasPrefix(e, "TERM=") {
					t.Fatalf("SHELL/TERM 值不符: got %v, want %s / %s", got, tc.wantShell, tc.wantTerm)
				}
			}
		})
	}
}

// TestSession_EnvHasSingleShell 把纯函数规则钉在真实启动路径上：
// 无论宿主环境是否携带 SHELL，子进程 env 数组里都恰好一条非空 SHELL。
func TestSession_EnvHasSingleShell(t *testing.T) {
	session, err := newSession("test-shell-env", StartOptions{
		Command: "/bin/true",
		Cols:    80,
		Rows:    24,
	})
	if err != nil {
		t.Fatalf("创建会话失败: %v", err)
	}
	defer session.Close()

	count := 0
	for _, e := range session.cmd.Env {
		if strings.HasPrefix(e, "SHELL=") {
			count++
			if len(e) <= len("SHELL=") {
				t.Fatalf("SHELL 值不应为空: %q", e)
			}
		}
	}
	if count != 1 {
		t.Fatalf("子进程 env 应恰好一条 SHELL, got %d", count)
	}
}

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
