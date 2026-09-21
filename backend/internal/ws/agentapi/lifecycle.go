package agentapi

import (
	"sync"
	"time"

	"github.com/askie/grix/backend/internal/pkg/logger"
	"github.com/gorilla/websocket"
)

// defaultAgentDrainTimeout is a fixed wall-clock budget for planned shutdown.
// It covers normal long-running agent turns without turning shutdown into an
// unbounded wait; work still active after the deadline is failed closed by the
// connector when this node forces the socket down.
const defaultAgentDrainTimeout = 3 * time.Minute

// shutdownWait 是强制关闭连接并广播 stopping() 后，Shutdown 留给
// 后台工作收尾的上限。local_action、session_bind、tailnet 等长等待都会
// 被 stopping() 取消；这里只保留短收尾窗口，超时告警但不让它们越过
// Kubernetes 的整体终止宽限。
const shutdownWait = 5 * time.Second

// backgroundGroup 汇总 Manager 派生的全部后台工作：正在服务的 agent 连接、
// 后台协程、以及超时定时器。Manager 关停时据此断连接、停定时器并等协程退出。
//
// 存在的意义：ServeWS 与其派生协程（writePump / 积压重放 / agent_invoke）、
// 落库协程、pending 超时定时器都会读写全局资源（DB、logger）。没有统一的
// 生命周期，进程关停（或测试收尾）后它们还在跑，轻则日志报 "database is closed"，
// 重则把已经不该发生的写入落回库里。
//
// 用自己的计数而不是 sync.WaitGroup：关停期间仍会有新的后台工作产生
// （断开连接会触发一大批终态收尾），WaitGroup 在 Wait 期间从 0 涨到 1 会 panic，
// 计数 + 轮询等待则天然允许「边关停边接活，等到全部归零为止」。
type backgroundGroup struct {
	mu      sync.Mutex
	active  int
	closing bool
	// stop 在关停时关闭，作为广播信号：长时间阻塞等待的后台工作（跨节点
	// local_action 等回包 20s、tailnet 传输编排等对端 5 分钟）据此提前收手。
	// 只计数不取消是不够的——那样 Shutdown 要么干等到它们自己超时，
	// 要么到上限后把它承诺等待的协程丢下不管。
	stop chan struct{}
	// conns 是正在服务的连接：键是底层 ws 连接（鉴权前就存在），
	// 值是鉴权后建立的 agentConn（鉴权阶段为 nil）。
	// 关停时对 agentConn 走 close()（置终止标志、关 done、关 ws），
	// 只有裸连接的直接关 ws——不能只关 ws：那样 done 还没关，
	// 生产者会以为连接还活着，继续把消息投进永远不会被发出的缓冲区。
	conns  map[*websocket.Conn]*agentConn
	timers map[*trackedTimer]struct{}
}

// tryBegin atomically admits background work into the shutdown counter.
// FinalizeBackground seals new roots, but work that was already admitted may
// still enqueue its own asynchronous tail while active is non-zero. Once the
// counter reaches zero after sealing, it can never rise again, so
// waitBackground cannot observe zero and then be escaped by a late task.
func (g *backgroundGroup) tryBegin() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.closing && g.active == 0 {
		return false
	}
	g.active++
	return true
}

// stopChanLocked 返回关停广播通道（惰性创建）；调用方须持锁。
func (g *backgroundGroup) stopChanLocked() chan struct{} {
	if g.stop == nil {
		g.stop = make(chan struct{})
	}
	return g.stop
}

// stopping 返回关停广播通道：关停开始时它会被关闭。
// 长时间阻塞的后台工作应当 select 它，收到即收手。
func (m *Manager) stopping() <-chan struct{} {
	m.bg.mu.Lock()
	defer m.bg.mu.Unlock()
	return m.bg.stopChanLocked()
}

func (g *backgroundGroup) end() {
	g.mu.Lock()
	g.active--
	g.mu.Unlock()
}

// beginConn 在同一个临界区内完成「登记工作 + 登记连接」。
// 两步必须原子：若分成两次加锁，Shutdown 可能在中间抢到锁——连接已计入
// 等待却还没进 conns 快照，于是它不会被关掉，读循环一直阻塞在 ReadMessage，
// Shutdown 白等到超时，收尾（关库）之后它还活着。
// 返回 false 表示已进入关停，调用方应立即断开这条新连接。
func (g *backgroundGroup) beginConn(conn *websocket.Conn) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.closing {
		return false
	}
	g.active++
	if g.conns == nil {
		g.conns = make(map[*websocket.Conn]*agentConn)
	}
	g.conns[conn] = nil
	return true
}

// bindConn 在鉴权完成、agentConn 建好后回填，让 Shutdown 能对它走完整的 close()。
func (g *backgroundGroup) bindConn(ws *websocket.Conn, conn *agentConn) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if _, ok := g.conns[ws]; ok {
		g.conns[ws] = conn
	}
}

func (g *backgroundGroup) endConn(ws *websocket.Conn) {
	g.mu.Lock()
	defer g.mu.Unlock()
	delete(g.conns, ws)
	g.active--
}

// trackedTimer 是登记在后台工作组里的超时定时器。
// Stop 时自动从工作组注销（否则被提前 Stop 的定时器会永远堆在集合里）；
// Manager 关停时工作组统一把它们停掉。
type trackedTimer struct {
	group *backgroundGroup
	timer *time.Timer // 只在 group.mu 保护下读写
}

// Stop 停掉定时器并注销；语义与 time.Timer.Stop 一致（返回是否成功阻止了触发）。
func (t *trackedTimer) Stop() bool {
	if t == nil || t.group == nil {
		return false
	}
	t.group.mu.Lock()
	defer t.group.mu.Unlock()
	delete(t.group.timers, t)
	if t.timer == nil {
		return false
	}
	return t.timer.Stop()
}

func (g *backgroundGroup) removeTimer(timer *trackedTimer) {
	g.mu.Lock()
	defer g.mu.Unlock()
	delete(g.timers, timer)
}

// trackServe 登记一条正在服务的 agent 连接；返回 false 表示 Manager 正在关停，
// ServeWS 应立即断开这条新连接。
func (m *Manager) trackServe(conn *websocket.Conn) bool {
	if m == nil {
		return false
	}
	m.drainMu.Lock()
	defer m.drainMu.Unlock()
	if m.draining {
		return false
	}
	return m.bg.beginConn(conn)
}

// bindServeConn 把鉴权后建立的 agentConn 关联到底层连接上。
func (m *Manager) bindServeConn(ws *websocket.Conn, conn *agentConn) {
	m.bg.bindConn(ws, conn)
}

// untrackServe 在 ServeWS 返回时注销连接。
func (m *Manager) untrackServe(conn *websocket.Conn) {
	m.bg.endConn(conn)
}

// goBackground 起一个受 Manager 生命周期约束的后台协程。
//
// 关停中照常异步执行、并计入等待，两个都不能省：
//   - 不能丢弃：关停会主动断开全部连接、触发大量 run 终态落库，丢了会让
//     session_agent_states 永远停在 running（此时 DB 还活着，写是该落的）。
//   - 不能改成同步内联：调用方可能是 WS 读循环（agent_invoke）或 Redis 订阅
//     的单线程分发（跨节点 local_action），内联会把它们阻塞住——agent 自派自时
//     回包要靠同一个读循环去读，内联即死锁。
func (m *Manager) goBackground(fn func()) bool {
	if fn == nil {
		return false
	}
	if !m.bg.tryBegin() {
		return false
	}
	go func() {
		defer m.bg.end()
		fn()
	}()
	return true
}

// GoBackground 是 goBackground 的导出入口，供同一进程内的 ws.Server 把它派生的、
// 会读写 DB 的协程（如启动时的 session_agent_state 对账）挂进同一套生命周期。
func (m *Manager) GoBackground(fn func()) bool {
	return m.goBackground(fn)
}

// afterFunc 起一个受 Manager 生命周期约束的定时器。
// 关停时未触发的定时器会被停掉；已经触发、正在执行的回调会被 Shutdown 等待。
// 与 goBackground 不同，关停中不再新建定时器：超时判定在关停时触发没有意义
// （连接本来就是被我们主动断的），跑它反而会写出误导性的「超时失败」状态。
func (m *Manager) afterFunc(wait time.Duration, fn func()) *trackedTimer {
	tracked := &trackedTimer{group: &m.bg}

	// 建定时器与登记都在 mu 内完成：定时器可能立刻触发，回调要取同一把锁，
	// 会等到这里登记完成，因此不存在「回调读到还没写完的 tracked.timer」的竞态。
	m.bg.mu.Lock()
	defer m.bg.mu.Unlock()
	if m.bg.closing {
		// 返回一个空壳：调用方存下来照常 Stop，行为等价于「已停止」。
		return tracked
	}
	tracked.timer = time.AfterFunc(wait, func() {
		m.bg.removeTimer(tracked)
		m.bg.mu.Lock()
		if m.bg.closing {
			m.bg.mu.Unlock()
			return
		}
		m.bg.active++
		m.bg.mu.Unlock()
		defer m.bg.end()
		fn()
	})
	if m.bg.timers == nil {
		m.bg.timers = make(map[*trackedTimer]struct{})
	}
	m.bg.timers[tracked] = struct{}{}
	return tracked
}

// BeginDrain atomically closes admission for new connections and new initial
// event dispatches, fixes the wall-clock deadline, and immediately migrates
// connections that have no active run. Existing runs keep their socket and all
// of its output/terminal protocol until settlement or the fixed deadline.
func (m *Manager) BeginDrain() {
	if m == nil {
		return
	}
	requestedAt := time.Now()
	m.drainMu.Lock()
	if m.draining {
		m.drainMu.Unlock()
		return
	}
	m.draining = true
	timeout := m.drainTimeout
	if timeout <= 0 {
		timeout = defaultAgentDrainTimeout
	}
	m.drainDeadline = requestedAt.Add(timeout)
	m.drainDone = make(chan struct{})

	// trackServe and initial dispatch take drainMu too, so after draining is
	// visible this snapshot cannot race a just-admitted connection or run.
	conns := m.snapshotServeConns()
	deadline := m.drainDeadline
	done := m.drainDone
	m.drainMu.Unlock()
	m.closeIdleDrainConns(conns)
	// Enforce the deadline independently of later shutdown stages (for example
	// voice-call cleanup). Shutdown cancels this bounded waiter when drain ends
	// early, so it cannot outlive the Manager.
	m.goBackground(func() {
		timer := time.NewTimer(time.Until(deadline))
		defer timer.Stop()
		select {
		case <-timer.C:
			m.forceCloseServeConns()
		case <-done:
		}
	})
}

func (m *Manager) snapshotServeConns() map[*websocket.Conn]*agentConn {
	m.bg.mu.Lock()
	conns := make(map[*websocket.Conn]*agentConn, len(m.bg.conns))
	for ws, conn := range m.bg.conns {
		conns[ws] = conn
	}
	m.bg.mu.Unlock()
	return conns
}

// connHasDrainWorkLocked reports whether conn must remain open. The caller must
// hold drainMu; lock ordering is drainMu -> runsMu everywhere in this file.
func (m *Manager) connHasDrainWorkLocked(conn *agentConn) bool {
	if conn == nil {
		return false
	}
	if conn.drainWork > 0 {
		return true
	}
	m.runsMu.Lock()
	defer m.runsMu.Unlock()
	for _, run := range m.runs {
		if run != nil && run.AgentID == conn.agentID && run.OwnerID == conn.ownerID {
			return true
		}
	}
	return false
}

// beginDrainWork reserves a connection across external I/O without holding
// drainMu. Initial dispatch and bootstrap pass rejectWhileDraining=true;
// terminal processing is already-active work and remains admitted during drain.
func (m *Manager) beginDrainWork(conn *agentConn, rejectWhileDraining bool) (func(), bool) {
	if m == nil || conn == nil {
		return func() {}, false
	}
	m.drainMu.Lock()
	if rejectWhileDraining && m.draining {
		m.drainMu.Unlock()
		return func() {}, false
	}
	conn.drainWork++
	m.drainMu.Unlock()
	var once sync.Once
	return func() {
		once.Do(func() {
			m.drainMu.Lock()
			conn.drainWork--
			m.closeDrainedConnIfIdleLocked(conn)
			m.drainMu.Unlock()
		})
	}, true
}

func (m *Manager) beginTerminalResult(conn *agentConn) func() {
	finish, _ := m.beginDrainWork(conn, false)
	return finish
}

func (m *Manager) closeIdleDrainConns(conns map[*websocket.Conn]*agentConn) {
	for ws, conn := range conns {
		if conn == nil {
			// Authentication has not completed, so this connection cannot own an
			// active run and must not become a new epoch on the draining node.
			if ws != nil {
				_ = ws.Close()
			}
			continue
		}
		m.drainMu.Lock()
		m.closeDrainedConnIfIdleLocked(conn)
		m.drainMu.Unlock()
	}
}

func (m *Manager) closeDrainedConnIfIdleLocked(conn *agentConn) {
	if m.draining && !m.connHasDrainWorkLocked(conn) {
		conn.closeAfterShutdownFlush()
	}
}

// closeDrainedConnIfIdle is called after a terminal event_result ACK has been
// accepted into the outbound FIFO. It is a no-op outside planned drain.
func (m *Manager) closeDrainedConnIfIdle(conn *agentConn) {
	if m == nil || conn == nil {
		return
	}
	m.drainMu.Lock()
	m.closeDrainedConnIfIdleLocked(conn)
	m.drainMu.Unlock()
}

func (m *Manager) drainDeadlineSnapshot() time.Time {
	m.drainMu.Lock()
	defer m.drainMu.Unlock()
	return m.drainDeadline
}

func (m *Manager) cancelDrainDeadline() {
	if m == nil {
		return
	}
	m.drainDoneOnce.Do(func() {
		m.drainMu.Lock()
		done := m.drainDone
		m.drainMu.Unlock()
		if done != nil {
			close(done)
		}
	})
}

func (m *Manager) forceCloseServeConns() {
	for ws, conn := range m.snapshotServeConns() {
		if conn != nil {
			m.drainMu.Lock()
			conn.shutdownClose.Store(true)
			conn.close()
			m.drainMu.Unlock()
			// The graceful writer path may currently be blocked in a socket
			// write. At the fixed deadline, force the transport down rather
			// than letting its write deadline extend the drain budget.
			if conn.ws != nil {
				_ = conn.ws.Close()
			}
			continue
		}
		if ws != nil {
			_ = ws.Close()
		}
	}
}

// DrainConnections completes the websocket phase of a planned drain. It never
// moves the deadline established by BeginDrain: repeated calls, new events,
// and ACK policy cannot extend it. Background admission deliberately remains
// open because the Redis subscriber must keep consuming stale-route forwards
// until the server synchronously stops it after this method returns.
func (m *Manager) DrainConnections() {
	if m == nil {
		return
	}
	m.BeginDrain()
	deadline := m.drainDeadlineSnapshot()
	for {
		conns := m.snapshotServeConns()
		if len(conns) == 0 {
			break
		}
		m.closeIdleDrainConns(conns)
		if !time.Now().Before(deadline) {
			m.forceCloseServeConns()
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	m.cancelDrainDeadline()
}

// FinalizeBackground seals background admission, cancels waits/timers, and
// waits for all work admitted before the seal (including its asynchronous
// tail) to finish. The server calls this only after the Redis subscriber has
// stopped, so no new cross-node root can arrive after the seal.
func (m *Manager) FinalizeBackground() {
	if m == nil {
		return
	}
	m.bg.mu.Lock()
	alreadyClosing := m.bg.closing
	m.bg.closing = true
	if stop := m.bg.stopChanLocked(); !alreadyClosing {
		close(stop)
	}
	for tracked := range m.bg.timers {
		if tracked.timer != nil {
			tracked.timer.Stop()
		}
	}
	m.bg.timers = nil
	m.bg.mu.Unlock()
	m.waitBackground()
}

// Shutdown preserves the standalone Manager contract while Server uses the
// two explicit phases to stop Redis delivery between them.
func (m *Manager) Shutdown() {
	if m == nil {
		return
	}
	m.DrainConnections()
	m.FinalizeBackground()
}

// waitBackground 等到后台工作全部归零；超时只告警，不无限等。
func (m *Manager) waitBackground() {
	deadline := time.Now().Add(shutdownWait)
	for {
		m.bg.mu.Lock()
		active := m.bg.active
		m.bg.mu.Unlock()
		if active == 0 {
			return
		}
		if time.Now().After(deadline) {
			logger.L.Warnf(
				"agent api manager shutdown timed out after %s, %d background tasks still running",
				shutdownWait, active,
			)
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
}
