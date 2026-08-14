package call

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// stopCallTimer 必须在真正拦下定时器时把计数配对减掉——trackedCallTimer 包装类型
// 被拆掉后，这层配对改由 stopCallTimer 一个函数统一做，靠这条用例锁住。
func TestStopCallTimerDecrementsActiveCount(t *testing.T) {
	c := New(nil, nil, nil)

	timer := c.trackedAfterFunc(time.Hour, func() {})
	c.bgMu.Lock()
	active := c.bgActive
	c.bgMu.Unlock()
	assert.Equal(t, 1, active, "trackedAfterFunc 必须在排定的瞬间计数")

	stopped := c.stopCallTimer(timer)
	assert.True(t, stopped, "定时器还没触发，Stop 必须成功拦下")

	c.bgMu.Lock()
	active = c.bgActive
	c.bgMu.Unlock()
	assert.Equal(t, 0, active, "拦下定时器后计数必须归零，否则 Shutdown 会白等到超时")
}

// Stop 拦不住（已经触发）时不能重复减计数——回调自己的 defer 已经减过一次。
func TestStopCallTimerNoDoubleDecrementAfterFire(t *testing.T) {
	c := New(nil, nil, nil)

	fired := make(chan struct{})
	timer := c.trackedAfterFunc(5*time.Millisecond, func() { close(fired) })

	select {
	case <-fired:
	case <-time.After(2 * time.Second):
		t.Fatal("timer never fired")
	}
	// 回调跑完后其 defer 已经把计数减掉，给它一点时间落地。
	time.Sleep(20 * time.Millisecond)

	stopped := c.stopCallTimer(timer)
	assert.False(t, stopped, "定时器已触发，Stop 必须返回 false")

	c.bgMu.Lock()
	active := c.bgActive
	c.bgMu.Unlock()
	assert.Equal(t, 0, active, "已触发定时器的计数只能被回调的 defer 减一次")
}

// resetCallTimer 必须重新计数——这是审查实测出来的真实缺陷：AnswerWithAI 先
// stopCallTimer 掉响铃定时器（计数还回去了），桥接失败时把同一个定时器
// Reset 重新拉起（回滚回 RINGING 继续等超时），但没有重新计数。等这个定时器
// 改天真触发，回调自己的 defer 照样会减一次——计数被减穿成负数，Shutdown
// 会在它还活着的时候就误判"没有在途工作"提前放行。
func TestResetCallTimerRecountsAfterStop(t *testing.T) {
	c := New(nil, nil, nil)

	fired := make(chan struct{})
	timer := c.trackedAfterFunc(time.Hour, func() { close(fired) })

	stopped := c.stopCallTimer(timer)
	assert.True(t, stopped, "定时器还没触发，Stop 必须成功拦下")
	c.bgMu.Lock()
	active := c.bgActive
	c.bgMu.Unlock()
	assert.Equal(t, 0, active, "Stop 成功后计数应归零")

	// Reset 的返回值只是"调用前是否处于活跃状态"（标准库语义），这里已经被
	// Stop 掉，返回 false 是预期行为——但 Reset 本身照样会把定时器重新排上，
	// 这才是要验证的：不管返回值是什么，计数都要重新加上。
	c.resetCallTimer(timer, 5*time.Millisecond)
	c.bgMu.Lock()
	active = c.bgActive
	c.bgMu.Unlock()
	assert.Equal(t, 1, active, "Reset 重新打开了触发窗口，必须重新计数")

	select {
	case <-fired:
	case <-time.After(2 * time.Second):
		t.Fatal("timer never fired after reset")
	}
	time.Sleep(20 * time.Millisecond)

	c.bgMu.Lock()
	active = c.bgActive
	c.bgMu.Unlock()
	assert.Equal(t, 0, active, "重新计数后回调触发只应把它减回 0，不能减穿成负数")
}
