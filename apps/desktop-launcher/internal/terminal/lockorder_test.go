// Package terminal - 会话与管理器之间的加锁顺序回归（S2）。
//
// 两条不变式合起来排除死锁环：waitLoop 回调 app 层状态回调时不得持有会话锁，
// Manager.List 取会话快照时不得持有管理器锁。任一条被破坏，只要管理器锁上排着写者
// （Start/Remove/SetStatusCallback 都会写），三方就会互等。
package terminal

import (
	"testing"
	"time"
)

// TestSessionStatusCallbackRunsWithoutSessionLock 用 TryLock 从回调内部直接判定：
// 回调运行在 waitLoop 上，这里还能拿到会话锁才说明调用时没有持锁。
func TestSessionStatusCallbackRunsWithoutSessionLock(t *testing.T) {
	// 用 cat 让子进程停在读 stdin 上：回调注册与进程退出之间的时序因此可控。
	s, err := newSession("test-status-callback-lock", StartOptions{
		Command: "/bin/cat",
		Cols:    80,
		Rows:    24,
	})
	if err != nil {
		t.Fatalf("创建会话失败: %v", err)
	}
	defer s.Close()

	checked := make(chan bool, 1)
	s.SetStatusCallback(func(SessionStatus, int, error) {
		if s.mu.TryLock() {
			s.mu.Unlock()
			checked <- true
			return
		}
		checked <- false
	})

	if err := s.Close(); err != nil {
		t.Fatalf("关闭会话: %v", err)
	}
	select {
	case locked := <-checked:
		if !locked {
			t.Fatal("状态回调在持有会话锁时被调用：与 Manager.List 构成反向加锁（S2）")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("状态回调没有被调用")
	}
}

// TestManagerListReleasesManagerLockBeforeSessionInfo 覆盖另一侧：持住会话锁让 List
// 停在 s.Info() 上，再从外部尝试获取管理器写锁。拿得到说明 List 已经先放开了 m.mu；
// 拿不到就说明它在等会话锁时仍握着管理器锁，正是死锁环的一环。
func TestManagerListReleasesManagerLockBeforeSessionInfo(t *testing.T) {
	m := NewManager()
	id, err := m.Start(&StartOptions{Command: "/bin/cat", Cols: 80, Rows: 24})
	if err != nil {
		t.Fatalf("启动会话失败: %v", err)
	}
	defer m.CloseAll()

	s, ok := m.Get(id)
	if !ok {
		t.Fatal("会话未注册到管理器")
	}

	s.mu.Lock()
	listDone := make(chan struct{})
	go func() {
		m.List()
		close(listDone)
	}()
	// 等 List 走到 s.Info()：两种实现都停在这里，区别只在是否还握着 m.mu。等待
	// 是必要的——刚起的 goroutine 还没排上 CPU 时探测，任何实现都会显示"已放开"。
	// TryLock 失败不会排队成写者，因此这次探测不会把旧实现推进死锁。
	time.Sleep(200 * time.Millisecond)
	heldManagerLock := !m.mu.TryLock()
	if !heldManagerLock {
		m.mu.Unlock()
	}
	// 先放开会话锁并把 List 收尾，再判定：否则 defer m.CloseAll() 会卡在同一把锁上。
	s.mu.Unlock()
	<-listDone

	if heldManagerLock {
		t.Fatal("List 在等待会话锁时仍持有 m.mu：与 waitLoop 的反向加锁会死锁（S2）")
	}
}
