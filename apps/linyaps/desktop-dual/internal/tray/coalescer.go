// 本文件提供托盘事件的合并执行器：把重复触发收敛成「至多一个在途执行」。
//
// 为什么需要它：托盘的定时刷新与宿主核对都要读切换器状态，而切换期间那把锁会被占住
// （最长一个 stopGrace）。在事件循环里同步做，会把同一循环里的退出与信号分支一起堵住，
// 表现为「点退出后托盘二十秒没反应」。
package tray

import "sync/atomic"

// Coalescer 把重复触发合并成至多一个在途执行。
//
// 放后台执行就不能让 1s 的节拍堆出几十个并发调用，所以采用「上一次还没回来就跳过本次」的
// 合并语义：每次触发要的都是「拿到最新状态」，多触发一次没有额外价值；反过来，丢掉中间
// 某一次也不会让状态停在旧值上——在途的那次读到的只会比它更新。
type Coalescer struct {
	inFlight atomic.Bool
	run      func()
}

// NewCoalescer 构造一个把 run 合并执行的触发器。
// @param run - 实际要执行的动作；为 nil 时触发器退化为空操作。
// @returns 就绪的触发器。
func NewCoalescer(run func()) *Coalescer {
	return &Coalescer{run: run}
}

// Trigger 请求执行一次；已有在途执行时直接返回。
//
// 调用方可以认为它永不阻塞：真正的动作在独立 goroutine 里跑。nil 接收者按空操作处理，
// 让调用点不必为「这个可选组件没装上」写分支。
func (c *Coalescer) Trigger() {
	if c == nil || c.run == nil {
		return
	}
	if !c.inFlight.CompareAndSwap(false, true) {
		return
	}
	go func() {
		defer c.inFlight.Store(false)
		c.run()
	}()
}
