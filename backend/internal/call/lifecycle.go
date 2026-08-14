package call

import (
	"time"
)

// 通话定时器（响铃超时、通话最长时长、AI 通话超时）的生命周期。
//
// 这些回调会读写 DB 并往外推 WS 事件。Timer.Stop() 只能拦住还没触发的定时器——
// 已经触发、正在跑的那个回调没人等：进程关停时它会被硬打断（通话状态卡在中间态），
// 测试里则会活过用例，去读下一个用例的全局 DB/logger（-race 必红）。
//
// 这里给 Controller 补上「等回调跑完」的能力，用计数而不是 WaitGroup：
// 关停期间仍可能有定时器刚好触发，WaitGroup 在 Wait 期间从 0 涨到 1 会 panic。
const callShutdownWait = 10 * time.Second

// trackedAfterFunc 起一个受 Controller 生命周期约束的定时器。
//
// ⛔ 计数必须在「决定要排这个定时器」的这一刻加，不能等到回调真正触发才加——
// 检查 closing 和调用 time.AfterFunc 之间哪怕只有几行代码的间隙，Shutdown 也可能
// 恰好在这个间隙里完成「等待 + 退出」，回调后面照样会跑，却活过了 Shutdown 的等待。
//
// 关停中不再新建定时器（新建了也没人停它，返回 nil）；但【已经触发】的回调一律
// 放行、只做计数——Shutdown 的职责是等它跑完，不是把它掐断。回调里判 closing
// 直接返回，会让一通已经超时的通话既没被超时收尾、也没被 Shutdown 结束，状态就
// 悬着了。
//
// 返回裸 *time.Timer：调用方原样存进 callEntry.timer，Reset 直接用标准库语义。
// 只有 Stop 需要配对减计数，统一走 stopCallTimer——不为此单独包一层类型。
func (c *Controller) trackedAfterFunc(d time.Duration, fn func()) *time.Timer {
	c.bgMu.Lock()
	if c.bgClosing {
		c.bgMu.Unlock()
		return nil
	}
	c.bgActive++
	c.bgMu.Unlock()

	return time.AfterFunc(d, func() {
		defer func() {
			c.bgMu.Lock()
			c.bgActive--
			c.bgMu.Unlock()
		}()
		fn()
	})
}

// stopCallTimer 停掉一个由 trackedAfterFunc 排定的定时器，语义与 time.Timer.Stop
// 一致：返回是否成功拦住了「还没触发」的定时器。
//
// 只有真拦住了才代它减计数——拦不住（已经触发/已经被停过）说明回调要么正在跑、
// 要么已经跑完，减计数已经由回调自己的 defer 做了，这里再减一次就会把计数减穿。
func (c *Controller) stopCallTimer(t *time.Timer) bool {
	if t == nil {
		return false
	}
	stopped := t.Stop()
	if stopped {
		c.bgMu.Lock()
		c.bgActive--
		c.bgMu.Unlock()
	}
	return stopped
}

// resetCallTimer 把一个已经被 stopCallTimer 拦下的定时器重新排上（响铃超时
// 的回滚路径：接管失败要把通话打回 RINGING，继续等原来的超时）。
//
// ⛔ 必须重新计数，不能假设「反正之前 Stop 过、计数还留着」——stopCallTimer
// 在真拦住时已经把计数还回去了，Reset 相当于重新打开了这个定时器「还会触发」
// 的窗口，等价于又 trackedAfterFunc 了一次，必须照它的口径重新 +1。不然这个
// 定时器改天真触发时，回调自己的 defer 照样会减一次计数——凭空多减一次，
// 计数被减穿，Shutdown 会在它还活着的时候就误判「没有在途工作」提前放行。
func (c *Controller) resetCallTimer(t *time.Timer, d time.Duration) bool {
	if t == nil {
		return false
	}
	c.bgMu.Lock()
	c.bgActive++
	c.bgMu.Unlock()
	return t.Reset(d)
}

// waitCallBackground 等正在跑的定时器回调退出；超时只是不再干等，避免关停被拖死。
func (c *Controller) waitCallBackground() {
	deadline := time.Now().Add(callShutdownWait)
	for {
		c.bgMu.Lock()
		active := c.bgActive
		c.bgMu.Unlock()
		if active == 0 {
			return
		}
		if time.Now().After(deadline) {
			callLogInfof("call trace: controller shutdown timed out, %d timer callbacks still running", active)
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
}
