package agentapi

import (
	"context"
	"testing"
	"time"

	"github.com/askie/grix/backend/internal/store"
	"github.com/askie/grix/backend/internal/ws/protocol"
	"github.com/gorilla/websocket"
)

func trackDrainTestConn(m *Manager, conn *agentConn) {
	m.bg.mu.Lock()
	if m.bg.conns == nil {
		m.bg.conns = make(map[*websocket.Conn]*agentConn)
	}
	m.bg.conns[nil] = conn
	m.bg.mu.Unlock()
}

func TestBeginDrainClosesIdleConnImmediately(t *testing.T) {
	mgr := NewManager("", time.Second, nil, nil, nil, nil)
	defer mgr.cancelDrainDeadline()
	conn := &agentConn{agentID: 11, ownerID: 21, send: make(chan []byte, 4), done: make(chan struct{})}
	trackDrainTestConn(mgr, conn)

	mgr.BeginDrain()

	if !conn.closed.Load() {
		t.Fatal("idle connection must close immediately when drain begins")
	}
	if mgr.trackServe(nil) {
		t.Fatal("new websocket must not be admitted while draining")
	}
}

func TestDrainKeepsActiveConnUntilTerminalAck(t *testing.T) {
	withoutDurableStores(t)
	mgr := NewManager("", time.Second, nil, nil, nil, nil)
	conn := &agentConn{
		agentID:      12,
		ownerID:      22,
		capabilities: []string{"event_result_ack"},
		send:         make(chan []byte, 4),
		done:         make(chan struct{}),
	}
	trackDrainTestConn(mgr, conn)
	mgr.runs["active-event"] = &activeAgentRun{
		EventID: "active-event", AgentID: conn.agentID, OwnerID: conn.ownerID,
		SessionID: "session-active", State: protocol.AgentOutputStateReceived,
	}

	mgr.BeginDrain()
	if conn.closed.Load() {
		t.Fatal("active connection closed before its terminal result")
	}

	mgr.handleEventResult(conn, makePacket(t, protocol.CmdEventResult, 7, EventResultPayload{
		EventID: "active-event",
		Status:  protocol.AgentEventResultResponded,
	}))
	packet := requireDurablePacket(t, conn.send, protocol.CmdSendAck)
	if packet.Seq != 7 {
		t.Fatalf("terminal ACK seq=%d want=7", packet.Seq)
	}
	if !conn.closed.Load() {
		t.Fatal("connection must close after its last run is terminal and ACK is queued")
	}
	// The production ServeWS defer removes the closed socket from bg.conns.
	// This focused test uses a synthetic entry, so remove it before Shutdown and
	// wait for terminal side-effect goroutines before store cleanup runs.
	mgr.bg.mu.Lock()
	delete(mgr.bg.conns, nil)
	mgr.bg.mu.Unlock()
	mgr.Shutdown()
}

func TestDrainDoesNotCloseBetweenRunSettlementAndTerminalAck(t *testing.T) {
	mgr := NewManager("", time.Second, nil, nil, nil, nil)
	defer mgr.cancelDrainDeadline()
	conn := &agentConn{
		agentID: 15, ownerID: 25, capabilities: []string{"event_result_ack"},
		send: make(chan []byte, 4), done: make(chan struct{}),
	}
	trackDrainTestConn(mgr, conn)

	finishTerminal := mgr.beginTerminalResult(conn)
	mgr.BeginDrain()
	if conn.closed.Load() {
		t.Fatal("terminal processing was misclassified as idle before its ACK")
	}
	mgr.sendEventResultAck(conn, makePacket(t, protocol.CmdEventResult, 8, EventResultPayload{}), EventResultPayload{
		EventID: "settled-event",
		Status:  protocol.AgentEventResultResponded,
	})
	if conn.closed.Load() {
		t.Fatal("connection closed before terminal handler finished queuing its ACK")
	}
	finishTerminal()
	if !conn.closed.Load() {
		t.Fatal("connection did not close after terminal ACK handling finished")
	}
	requireDurablePacket(t, conn.send, protocol.CmdSendAck)
}

func TestBeginDrainDoesNotWaitForReservedExternalIO(t *testing.T) {
	mgr := NewManager("", time.Second, nil, nil, nil, nil)
	defer mgr.cancelDrainDeadline()
	conn := &agentConn{agentID: 16, ownerID: 26, send: make(chan []byte, 4), done: make(chan struct{})}
	trackDrainTestConn(mgr, conn)

	finishAdmission, admitted := mgr.beginDrainWork(conn, true)
	if !admitted {
		t.Fatal("pre-drain dispatch admission was rejected")
	}
	ioRelease := make(chan struct{})
	ioDone := make(chan struct{})
	go func() {
		defer close(ioDone)
		<-ioRelease // stand in for blocked durable registration / authority I/O.
		mgr.runsMu.Lock()
		mgr.runs["reserved-event"] = &activeAgentRun{
			EventID: "reserved-event", AgentID: conn.agentID, OwnerID: conn.ownerID,
			SessionID: "session-reserved", State: protocol.AgentOutputStateReceived,
		}
		mgr.runsMu.Unlock()
		finishAdmission()
	}()

	started := time.Now()
	mgr.BeginDrain()
	if elapsed := time.Since(started); elapsed > 100*time.Millisecond {
		t.Fatalf("BeginDrain waited %v for reserved external I/O", elapsed)
	}
	if conn.closed.Load() {
		t.Fatal("reserved dispatch was misclassified as idle")
	}
	close(ioRelease)
	<-ioDone
	if conn.closed.Load() {
		t.Fatal("connection with a run committed by reserved admission was closed")
	}
}

func TestDrainKeepsAttachedConnWhileBootstrapReplayIsBlocked(t *testing.T) {
	withoutDurableStores(t)
	mgr := NewManager("", time.Second, nil, nil, nil, nil)
	defer mgr.cancelDrainDeadline()
	conn := &agentConn{agentID: 17, ownerID: 27, send: make(chan []byte, 4), done: make(chan struct{})}
	trackDrainTestConn(mgr, conn)

	finishBootstrap, admitted := mgr.beginDrainWork(conn, true)
	if !admitted || !mgr.attachConnAdmitted(conn) {
		t.Fatal("pre-drain attach admission failed")
	}
	replayStarted := make(chan struct{})
	replayRelease := make(chan struct{})
	replayDone := make(chan struct{})
	go func() {
		defer close(replayDone)
		close(replayStarted)
		<-replayRelease
		mgr.replayPending(conn)
		finishBootstrap()
	}()
	<-replayStarted

	mgr.BeginDrain()
	if conn.closed.Load() {
		t.Fatal("attached connection closed before bootstrap replay completed")
	}
	close(replayRelease)
	<-replayDone
	if !conn.closed.Load() {
		t.Fatal("idle connection did not close after bootstrap replay reservation released")
	}
}

func TestDrainRejectsNewAttachAndQueuesNewEvent(t *testing.T) {
	installDurableLifecycleTestStores(t, false)
	mgr := NewManager("", time.Second, nil, nil, nil, nil)
	defer mgr.cancelDrainDeadline()
	conn := &agentConn{agentID: 13, ownerID: 23, send: make(chan []byte, 8), done: make(chan struct{})}
	trackDrainTestConn(mgr, conn)
	mgr.putConnForTest(conn)
	mgr.runs["existing-event"] = &activeAgentRun{
		EventID: "existing-event", AgentID: conn.agentID, OwnerID: conn.ownerID,
		SessionID: "session-existing", State: protocol.AgentOutputStateReceived,
	}
	mgr.BeginDrain()

	newConn := &agentConn{agentID: conn.agentID, ownerID: conn.ownerID, connectionEpoch: 99}
	if mgr.attachConn(newConn) {
		t.Fatal("new epoch must not replace the active connection while draining")
	}
	if got := mgr.lookupConnByOwner(conn.agentID, conn.ownerID); got != conn {
		t.Fatal("drain changed the active connection epoch")
	}

	event := durableLifecycleEvent("queued-during-drain", conn.agentID, conn.ownerID)
	if !mgr.PushDelegateEvent(event) {
		t.Fatal("ordinary event must fall back to the durable queue during drain")
	}
	queued, err := store.RDB.LLen(context.Background(), queuedDelegateEventListKey(event.AgentID)).Result()
	if err != nil || queued != 1 {
		t.Fatalf("event accepted during drain was not durably queued: count=%d err=%v", queued, err)
	}
	if mgr.DispatchDelegateEventWithoutQueue(durableLifecycleEvent("reject-during-drain", conn.agentID, conn.ownerID)) {
		t.Fatal("non-queueing dispatch must report unavailable during drain")
	}
	if len(conn.send) != 0 {
		t.Fatal("new event was written to the draining connection")
	}
}

func TestShutdownForcesActiveConnAtFixedDeadline(t *testing.T) {
	mgr := NewManager("", time.Second, nil, nil, nil, nil)
	mgr.drainTimeout = 35 * time.Millisecond
	conn := &agentConn{agentID: 14, ownerID: 24, send: make(chan []byte, 4), done: make(chan struct{})}
	trackDrainTestConn(mgr, conn)
	mgr.runs["long-event"] = &activeAgentRun{
		EventID: "long-event", AgentID: conn.agentID, OwnerID: conn.ownerID,
		SessionID: "session-long", State: protocol.AgentOutputStateReceived,
	}

	started := time.Now()
	mgr.BeginDrain()
	firstDeadline := mgr.drainDeadlineSnapshot()
	time.Sleep(10 * time.Millisecond)
	mgr.BeginDrain()
	if !mgr.drainDeadlineSnapshot().Equal(firstDeadline) {
		t.Fatal("repeated BeginDrain extended the fixed deadline")
	}
	mgr.Shutdown()
	elapsed := time.Since(started)

	if !conn.closed.Load() {
		t.Fatal("active connection must be forced closed at the drain deadline")
	}
	if elapsed < 30*time.Millisecond || elapsed > 500*time.Millisecond {
		t.Fatalf("shutdown elapsed=%v, fixed drain deadline was not respected", elapsed)
	}
	mgr.bg.mu.Lock()
	timers := len(mgr.bg.timers)
	activeBackground := mgr.bg.active
	mgr.bg.mu.Unlock()
	if timers != 0 {
		t.Fatalf("shutdown leaked %d tracked timers", timers)
	}
	if activeBackground != 0 {
		t.Fatalf("shutdown leaked %d background goroutines", activeBackground)
	}
}
