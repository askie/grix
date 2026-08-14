package agentapi

import (
	"sync"
	"testing"
	"time"
)

// 连接已终止时，sendPayload 必须稳定报失败——调用方据此把事件落到离线队列。
//
// 这里刻意构造「closed 标志尚未可见、done 已关闭」的状态：并发下真实存在，
// 就是生产者读到 closed==false 之后 close() 才跑完的那一瞬。此时若把
// <-c.done 和 c.send <- data 放进同一个 select，两者同时就绪，Go 随机选，
// 于是对一条已死的连接随机返回「发送成功」，调用方跳过离线入队，消息静默丢失。
func TestSendPayloadAfterDoneClosedAlwaysReportsFailure(t *testing.T) {
	conn := &agentConn{send: make(chan []byte, 256), done: make(chan struct{})}
	close(conn.done) // 只关终止信号，模拟 closed 标志还没被这个生产者看到

	for i := 0; i < 1000; i++ {
		if conn.sendPayload("delegate_event", int64(i), map[string]int{"i": i}) {
			t.Fatalf("sendPayload reported success on a terminated conn (iteration %d)", i)
		}
	}
}

// 走完整 close() 的连接同样必须稳定报失败。
func TestSendPayloadOnClosedConnAlwaysReportsFailure(t *testing.T) {
	conn := &agentConn{send: make(chan []byte, 256), done: make(chan struct{})}
	conn.close()

	for i := 0; i < 1000; i++ {
		if conn.sendPayload("delegate_event", int64(i), map[string]int{"i": i}) {
			t.Fatalf("sendPayload reported success on a closed conn (iteration %d)", i)
		}
	}
}

// send 通道打满仍按既有语义处理：断开连接并报失败。
func TestSendPayloadFullChannelClosesConn(t *testing.T) {
	conn := &agentConn{send: make(chan []byte, 1), done: make(chan struct{})}

	if !conn.sendPayload("a", 1, map[string]int{"i": 1}) {
		t.Fatal("first send should succeed into an empty buffer")
	}
	if conn.sendPayload("b", 2, map[string]int{"i": 2}) {
		t.Fatal("send into a full channel should report failure")
	}
	if !conn.closed.Load() {
		t.Fatal("a full send channel must close the conn")
	}
}

// 「报告已送达」必须蕴含「当时连接还活着」：调用发起时 close() 已经完成的，
// 一次都不许报成功。（调用中途才被关闭是合法的——包已入队，由 writePump 负责。）
//
// 若「判活」与「入队」不是原子的（判活通过 → close 插进来 → 再入队），
// 包就进了没人消费的缓冲区却被报成已送达：调用方跳过离线入队，
// event_edit 重放路径还会顺手删掉队列记录，消息就永久丢了。
// 这里让 close 与大量生产者高强度并发，逐条核对这个不变量。
func TestSendPayloadNeverReportsSuccessAfterClose(t *testing.T) {
	for round := 0; round < 500; round++ {
		conn := &agentConn{send: make(chan []byte, 4096), done: make(chan struct{})}

		var producers sync.WaitGroup
		start := make(chan struct{})
		closed := make(chan struct{})

		for p := 0; p < 8; p++ {
			producers.Add(1)
			go func() {
				defer producers.Done()
				<-start
				for i := 0; i < 40; i++ {
					conn.sendPayload("delegate_event", int64(i), map[string]int{"i": i})
				}
			}()
		}
		go func() {
			<-start
			conn.close()
			close(closed)
		}()
		close(start)

		// close() 一返回，发送缓冲就必须「封口」：此后不允许再有包进来。
		// 无锁时会出现「判活通过 → close 插入 → 才入队」，那个包永远没人消费，
		// 却被 sendPayload 报成了「已送达」。
		<-closed
		buffered := len(conn.send)
		producers.Wait()
		if grew := len(conn.send) - buffered; grew > 0 {
			t.Fatalf("round %d: %d packet(s) were enqueued after close() returned — "+
				"they will never be written, yet sendPayload reported them as delivered", round, grew)
		}
	}
}

// 多生产者与 close 并发时不得 panic（send 通道永不关闭）。
func TestSendPayloadConcurrentWithCloseDoesNotPanic(t *testing.T) {
	for round := 0; round < 500; round++ {
		conn := &agentConn{send: make(chan []byte, 256), done: make(chan struct{})}
		var wg sync.WaitGroup
		start := make(chan struct{})
		for p := 0; p < 8; p++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				for i := 0; i < 20; i++ {
					conn.sendPayload("x", int64(i), map[string]int{"i": i})
				}
			}()
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			conn.close()
		}()
		close(start)
		wg.Wait()
	}
}

// 关停期间产生的后台工作（run 终态落库等）不能被丢掉：此时 DB 还活着，
// 丢了会让会话状态永远停在 running。也不能改成同步内联执行——调用方可能是
// WS 读循环或 Redis 订阅的单线程分发，内联会把它们阻塞住（agent 自派自时即死锁）。
// 正确语义：照常异步执行，且仍计入 Shutdown 的等待。
func TestGoBackgroundStillRunsAsyncAfterShutdown(t *testing.T) {
	mgr := NewManager("", time.Second, nil, nil, nil, nil)
	mgr.Shutdown()

	done := make(chan struct{})
	mgr.goBackground(func() { close(done) })

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("background work must still run after shutdown, not be dropped")
	}
}

// 关停中产生的后台工作必须被 Shutdown 等到——否则它会活过关库，
// 正是本次要根治的竞态。
func TestShutdownWaitsForWorkStartedWhileClosing(t *testing.T) {
	mgr := NewManager("", time.Second, nil, nil, nil, nil)

	started := make(chan struct{})
	finished := make(chan struct{})
	mgr.goBackground(func() {
		close(started)
		time.Sleep(150 * time.Millisecond)
		close(finished)
	})
	<-started

	mgr.Shutdown()

	select {
	case <-finished:
	default:
		t.Fatal("Shutdown returned while background work was still running")
	}
}

// goBackground 绝不能在调用方协程上同步执行：那会阻塞 WS 读循环 / Redis 分发。
func TestGoBackgroundNeverRunsOnCallerGoroutine(t *testing.T) {
	mgr := NewManager("", time.Second, nil, nil, nil, nil)
	defer mgr.Shutdown()
	mgr.Shutdown() // 进入关停态后同样不得内联

	release := make(chan struct{})
	entered := make(chan struct{})
	mgr.goBackground(func() {
		close(entered)
		<-release // 若是内联执行，调用方会被卡在这里，下面的断言永远到不了
	})

	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("background work never started")
	}
	close(release)
}

// 关停后不再新建定时器：超时判定在关停时触发没有意义，跑它反而会写出误导性的失败状态。
func TestAfterFuncAfterShutdownDoesNotFire(t *testing.T) {
	mgr := NewManager("", time.Second, nil, nil, nil, nil)
	mgr.Shutdown()

	fired := make(chan struct{})
	timer := mgr.afterFunc(time.Millisecond, func() { close(fired) })
	if timer == nil {
		t.Fatal("afterFunc must still return a usable handle after shutdown")
	}
	timer.Stop() // 调用方照常 Stop，不得 panic

	select {
	case <-fired:
		t.Fatal("timer scheduled after shutdown must not fire")
	case <-time.After(50 * time.Millisecond):
	}
}

// 关停必须能叫醒长等待：跨节点 local_action 正常要等 20s 回包、tailnet 传输
// 最长等 5 分钟。若工作组只计数不广播停止，Shutdown 要么干等，要么到上限后把
// 它承诺等待的协程丢下——那就是活过关停的野协程，正是本次要根治的东西。
func TestShutdownInterruptsLongBackgroundWait(t *testing.T) {
	mgr := NewManager("", time.Second, nil, nil, nil, nil)

	entered := make(chan struct{})
	woke := make(chan struct{})
	mgr.goBackground(func() {
		close(entered)
		select {
		case <-time.After(20 * time.Second): // 正常路径的等待时长
		case <-mgr.stopping(): // 关停广播
		}
		close(woke)
	})
	<-entered

	start := time.Now()
	mgr.Shutdown()
	elapsed := time.Since(start)

	select {
	case <-woke:
	default:
		t.Fatal("Shutdown returned while the long wait was still parked")
	}
	if elapsed > 3*time.Second {
		t.Fatalf("Shutdown blocked %v on a long background wait; the stop broadcast did not wake it", elapsed)
	}
}

// 关停信号必须对所有观察者可见，且在 Shutdown 之后调用 stopping() 也立刻可读。
func TestStoppingBroadcastVisibleToAllWaitersAndAfterShutdown(t *testing.T) {
	mgr := NewManager("", time.Second, nil, nil, nil, nil)

	const waiters = 8
	woke := make(chan struct{}, waiters)
	ready := make(chan struct{}, waiters)
	for i := 0; i < waiters; i++ {
		mgr.goBackground(func() {
			ready <- struct{}{}
			<-mgr.stopping()
			woke <- struct{}{}
		})
	}
	for i := 0; i < waiters; i++ {
		<-ready
	}

	mgr.Shutdown()

	for i := 0; i < waiters; i++ {
		select {
		case <-woke:
		default:
			t.Fatalf("only %d of %d waiters observed the stop broadcast", i, waiters)
		}
	}

	// 关停之后再问一次，必须立刻可读（不能永远阻塞）。
	select {
	case <-mgr.stopping():
	case <-time.After(time.Second):
		t.Fatal("stopping() must be readable after Shutdown")
	}
}

// Shutdown 幂等，重复调用不得 panic 或卡住。
func TestShutdownIsIdempotent(t *testing.T) {
	mgr := NewManager("", time.Second, nil, nil, nil, nil)
	done := make(chan struct{})
	go func() {
		defer close(done)
		mgr.Shutdown()
		mgr.Shutdown()
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("repeated Shutdown must not block")
	}
}

// 被提前 Stop 的定时器要从工作组注销，否则长跑进程里会越堆越多。
func TestTrackedTimerStopUnregisters(t *testing.T) {
	mgr := NewManager("", time.Second, nil, nil, nil, nil)
	defer mgr.Shutdown()

	timer := mgr.afterFunc(time.Hour, func() {})
	mgr.bg.mu.Lock()
	got := len(mgr.bg.timers)
	mgr.bg.mu.Unlock()
	if got != 1 {
		t.Fatalf("timer should be tracked, got %d", got)
	}

	timer.Stop()
	mgr.bg.mu.Lock()
	got = len(mgr.bg.timers)
	mgr.bg.mu.Unlock()
	if got != 0 {
		t.Fatalf("stopped timer must be unregistered, got %d", got)
	}
}
