package tray

import (
	"sync/atomic"
	"testing"
	"time"
)

// waitFor 轮询等待条件成立，超时即失败。合并语义本身是异步的，用例只能等状态而非等固定时长。
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("等待超时: %s", what)
}

// TestCoalescerDropsTriggersWhileInFlight 覆盖合并语义：在途期间重复触发不再叠加执行。
//
// 这条是本次改动的要点——托盘刷新每秒一次，若每次都无条件起 goroutine，切换期间锁被占着
// 的那二十秒会堆出二十个并发刷新，锁一放开就一起轰同一个 D-Bus 连接。
func TestCoalescerDropsTriggersWhileInFlight(t *testing.T) {
	var calls, running atomic.Int32
	release := make(chan struct{})
	done := make(chan struct{})
	c := NewCoalescer(func() {
		calls.Add(1)
		running.Add(1)
		<-release
		running.Add(-1)
	})

	c.Trigger()
	waitFor(t, "第一次执行进入", func() bool { return running.Load() == 1 })

	for i := 0; i < 5; i++ {
		c.Trigger()
	}
	// 在途期间触发的都不该执行：既没有第二个在途，也没有排队等着补跑。
	if got := running.Load(); got != 1 {
		t.Fatalf("在途执行数 = %d，期望 1（重复触发应被合并）", got)
	}

	close(release)
	go func() { waitFor(t, "第一次执行结束", func() bool { return running.Load() == 0 }); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("第一次执行未结束")
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("执行次数 = %d，期望 1（在途期间的触发应被丢弃而不是排队）", got)
	}
}

// TestCoalescerRunsAgainAfterCompletion 覆盖空闲后的再次触发：合并不能变成「只跑一次」。
func TestCoalescerRunsAgainAfterCompletion(t *testing.T) {
	var calls atomic.Int32
	ran := make(chan struct{}, 4)
	c := NewCoalescer(func() { calls.Add(1); ran <- struct{}{} })

	c.Trigger()
	select {
	case <-ran:
	case <-time.After(2 * time.Second):
		t.Fatal("第一次触发未执行")
	}
	// 第一次已经返回，inFlight 由 defer 复位；短暂等待后再次触发必须能跑。
	waitFor(t, "在途标志复位", func() bool { return !c.inFlight.Load() })
	c.Trigger()
	select {
	case <-ran:
	case <-time.After(2 * time.Second):
		t.Fatal("空闲后的触发未执行：合并退化成了只跑一次")
	}
	if got := calls.Load(); got != 2 {
		t.Fatalf("执行次数 = %d，期望 2", got)
	}
}

// TestCoalescerNilIsNoop 覆盖可选组件未装配时的空操作语义。
func TestCoalescerNilIsNoop(t *testing.T) {
	var c *Coalescer
	c.Trigger()                 // 不应 panic
	NewCoalescer(nil).Trigger() // 动作缺失时同样不应 panic
}
