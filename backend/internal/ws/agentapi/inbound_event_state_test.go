package agentapi

import (
	"encoding/json"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/askie/grix/backend/internal/ws/protocol"
)

func TestObserveInboundPacketActivityCoversAllAgentOutputProtocols(t *testing.T) {
	testCases := []struct {
		name    string
		cmd     string
		payload any
	}{
		{"generic send", protocol.CmdSendMsg, SendMsgPayload{EventID: "evt-activity"}},
		{"generic stream", protocol.CmdClientStreamChunk, AgentStreamChunkPayload{EventID: "evt-activity"}},
		{"codex", protocol.CmdCodexEvent, CodexEventPayload{EventID: "evt-activity"}},
		{"pi", protocol.CmdPiEvent, PiEventPayload{EventID: "evt-activity"}},
		{"composing", protocol.CmdSessionActivitySet, protocol.SessionActivitySetPayload{RefEventID: "evt-activity"}},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			withoutDurableStores(t)
			mgr := NewManager("", time.Second, nil, nil, nil, nil)
			defer mgr.Shutdown()
			mgr.eventResultWait = time.Hour
			event := DelegateEventPayload{
				EventID: "evt-activity", AgentID: 101, OwnerID: 201,
				SessionID: "sess-activity", MsgID: 301,
			}
			mgr.registerPendingEventAck(event, 1)
			mgr.resolvePendingEventAck(event.EventID, time.Now().UnixMilli())

			old := time.Now().Add(-time.Hour).UnixMilli()
			mgr.acksMu.Lock()
			mgr.pending[event.EventID].selfTouchAt = old
			mgr.acksMu.Unlock()

			mgr.observeInboundPacketActivity(
				&agentConn{agentID: event.AgentID, ownerID: event.OwnerID},
				makePacket(t, tc.cmd, 1, tc.payload),
			)

			mgr.acksMu.Lock()
			got := mgr.pending[event.EventID].selfTouchAt
			mgr.acksMu.Unlock()
			if got <= old {
				t.Fatalf("event activity was not refreshed: got=%d old=%d", got, old)
			}
		})
	}
}

func TestObserveInboundPacketActivityRejectsOtherOwnerConnection(t *testing.T) {
	withoutDurableStores(t)
	mgr := NewManager("", time.Second, nil, nil, nil, nil)
	defer mgr.Shutdown()
	event := DelegateEventPayload{
		EventID: "evt-owner-a", AgentID: 101, OwnerID: 201,
		SessionID: "sess-owner-a", MsgID: 301,
	}
	mgr.registerPendingEventAck(event, 1)
	mgr.resolvePendingEventAck(event.EventID, time.Now().UnixMilli())

	old := time.Now().Add(-time.Hour).UnixMilli()
	mgr.acksMu.Lock()
	mgr.pending[event.EventID].selfTouchAt = old
	mgr.acksMu.Unlock()

	mgr.observeInboundPacketActivity(
		&agentConn{agentID: event.AgentID, ownerID: 202},
		makePacket(t, protocol.CmdPiEvent, 1, PiEventPayload{EventID: event.EventID}),
	)

	mgr.acksMu.Lock()
	got := mgr.pending[event.EventID].selfTouchAt
	mgr.acksMu.Unlock()
	if got != old {
		t.Fatalf("other owner connection refreshed event: got=%d want=%d", got, old)
	}
}

func TestEventResultFromOtherOwnerCannotSettleRun(t *testing.T) {
	withoutDurableStores(t)
	mgr := NewManager("", time.Second, nil, nil, nil, nil)
	defer mgr.Shutdown()
	event := DelegateEventPayload{
		EventID: "evt-owner-terminal", AgentID: 101, OwnerID: 201,
		SessionID: "sess-owner-terminal", MsgID: 301,
	}
	mgr.registerActiveRun(event)
	mgr.registerPendingEventAck(event, 1)
	mgr.resolvePendingEventAck(event.EventID, time.Now().UnixMilli())

	conn := &agentConn{
		agentID: event.AgentID, ownerID: 202,
		send: make(chan []byte, 1),
	}
	mgr.handleEventResult(conn, makePacket(t, protocol.CmdEventResult, 1, EventResultPayload{
		EventID: event.EventID,
		Status:  protocol.AgentEventResultFailed,
	}))

	if run := mgr.LookupActiveRun(event.EventID); run == nil {
		t.Fatal("foreign owner result settled the run")
	}
	mgr.acksMu.Lock()
	_, pending := mgr.pending[event.EventID]
	mgr.acksMu.Unlock()
	if !pending {
		t.Fatal("foreign owner result removed pending event")
	}
	select {
	case raw := <-conn.send:
		var pkt protocol.Packet
		if err := json.Unmarshal(raw, &pkt); err != nil {
			t.Fatalf("unmarshal response: %v", err)
		}
		if pkt.Cmd != "error" {
			t.Fatalf("response cmd=%q want=error", pkt.Cmd)
		}
	default:
		t.Fatal("expected ownership error response")
	}
}

func TestEventAckAfterVisibleOutputDoesNotRegressRunState(t *testing.T) {
	withoutDurableStores(t)
	mgr := NewManager("", time.Second, nil, nil, nil, nil)
	defer mgr.Shutdown()

	event := DelegateEventPayload{
		EventID: "evt-output-before-ack", AgentID: 101, OwnerID: 201,
		SessionID: "sess-output-before-ack", MsgID: 301,
	}
	mgr.registerActiveRun(event)
	mgr.registerPendingEventAck(event, 1)

	// A connector normally sends event_ack before output, but reconnect replay
	// and independently scheduled writers can expose a valid output packet
	// first. Once visible output exists, a later ack must not move the UI state
	// backwards from streaming to received.
	mgr.MarkRunStreaming(event.EventID, 9001)
	mgr.resolvePendingEventAck(event.EventID, time.Now().UnixMilli())

	run := mgr.LookupActiveRun(event.EventID)
	if run == nil {
		t.Fatal("active run unexpectedly disappeared")
	}
	if run.State != protocol.AgentOutputStateStreaming {
		t.Fatalf("late event_ack regressed run state: got=%q want=%q", run.State, protocol.AgentOutputStateStreaming)
	}
	if run.StreamMsgID != 9001 {
		t.Fatalf("late event_ack changed stream msg id: got=%d want=9001", run.StreamMsgID)
	}
}

func TestConcurrentTerminalUpdatesEmitOneAbsorbingState(t *testing.T) {
	withoutDurableStores(t)
	mgr := NewManager("", time.Second, nil, nil, nil, nil)
	defer mgr.Shutdown()

	event := DelegateEventPayload{
		EventID: "evt-concurrent-terminal", AgentID: 101, OwnerID: 201,
		SessionID: "sess-concurrent-terminal", SenderID: 999, MsgID: 301,
	}
	statuses := make(chan protocol.AgentOutputStatusPayload, 64)
	mgr.SetOutputStatusHandler(func(payload protocol.AgentOutputStatusPayload) {
		statuses <- payload
	})
	mgr.registerActiveRun(event)
	<-statuses // initial queued state

	var wg sync.WaitGroup
	for i := 0; i < 30; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			switch index % 3 {
			case 0:
				mgr.MarkRunCompleted(event.EventID)
			case 1:
				mgr.MarkRunFailed(event.EventID, "connector_failed")
			default:
				mgr.MarkRunStopped(event.EventID, "owner_requested_stop")
			}
		}(i)
	}
	wg.Wait()

	close(statuses)
	var terminal []protocol.AgentOutputStatusPayload
	for status := range statuses {
		terminal = append(terminal, status)
	}
	if len(terminal) != 1 {
		t.Fatalf("concurrent terminal reports emitted %d states, want exactly 1: %#v", len(terminal), terminal)
	}
	switch terminal[0].State {
	case protocol.AgentOutputStateCompleted,
		protocol.AgentOutputStateFailed,
		protocol.AgentOutputStateStopped:
	default:
		t.Fatalf("unexpected absorbing terminal state: %#v", terminal[0])
	}
	if run := mgr.LookupActiveRun(event.EventID); run != nil {
		t.Fatalf("terminal run still active: %+v", run)
	}
}

func TestConcurrentEventResultsEmitOneDeliveryAndOutputTerminal(t *testing.T) {
	withoutDurableStores(t)
	mgr := NewManager("", time.Second, nil, nil, nil, nil)
	defer mgr.Shutdown()

	event := DelegateEventPayload{
		EventID: "evt-concurrent-result", AgentID: 101, OwnerID: 201,
		SessionID: "sess-concurrent-result", SenderID: 999, MsgID: 301,
	}
	mgr.registerActiveRun(event)
	mgr.registerPendingEventAck(event, 1)

	conn := &agentConn{
		agentID: event.AgentID,
		ownerID: event.OwnerID,
		send:    make(chan []byte, 64),
	}
	var deliveryCount atomic.Int32
	firstDeliveryEntered := make(chan struct{})
	releaseFirstDelivery := make(chan struct{})
	mgr.SetDeliveryStatusHandler(func(protocol.AgentDeliveryStatusPayload) {
		if deliveryCount.Add(1) == 1 {
			close(firstDeliveryEntered)
			<-releaseFirstDelivery
		}
	})
	var outputCount atomic.Int32
	mgr.SetOutputStatusHandler(func(payload protocol.AgentOutputStatusPayload) {
		switch payload.State {
		case protocol.AgentOutputStateCompleted,
			protocol.AgentOutputStateFailed,
			protocol.AgentOutputStateStopped:
			outputCount.Add(1)
		}
	})

	firstDone := make(chan struct{})
	go func() {
		defer close(firstDone)
		mgr.handleEventResult(conn, makePacket(t, protocol.CmdEventResult, 1, EventResultPayload{
			EventID: event.EventID,
			Status:  protocol.AgentEventResultResponded,
		}))
	}()
	<-firstDeliveryEntered

	var duplicates sync.WaitGroup
	for i := 0; i < 30; i++ {
		duplicates.Add(1)
		go func(index int) {
			defer duplicates.Done()
			status := protocol.AgentEventResultFailed
			if index%2 == 0 {
				status = protocol.AgentEventResultCanceled
			}
			mgr.handleEventResult(conn, makePacket(t, protocol.CmdEventResult, int64(index+2), EventResultPayload{
				EventID: event.EventID,
				Status:  status,
			}))
		}(i)
	}
	duplicates.Wait()
	close(releaseFirstDelivery)
	<-firstDone

	if got := deliveryCount.Load(); got != 1 {
		t.Fatalf("concurrent event_result emitted %d delivery terminal states, want 1", got)
	}
	if got := outputCount.Load(); got != 1 {
		t.Fatalf("concurrent event_result emitted %d output terminal states, want 1", got)
	}
}

func TestInboundOutputsRemainAcceptedWhenPendingContextIsMissing(t *testing.T) {
	withoutDurableStores(t)
	sendHandler := &mockSendMessageHandler{
		result: &SendMessageResult{MsgID: 9001, CreatedAt: time.Now().UnixMilli()},
	}
	streamHandler := &mockStreamChunkHandler{}
	mgr := NewManager("", time.Second, sendHandler.handle, streamHandler.handle, nil, nil)
	defer mgr.Shutdown()
	event := DelegateEventPayload{
		EventID: "evt-active-only", AgentID: 101, OwnerID: 201,
		SessionID: "sess-active-only", MsgID: 301,
	}
	// Simulate in-memory/durable pending loss while the active run and
	// connector execution still exist.
	mgr.registerActiveRun(event)
	conn := &agentConn{
		agentID: event.AgentID, ownerID: event.OwnerID,
		send: make(chan []byte, 4),
	}

	mgr.handleClientStreamChunk(conn, makePacket(t, protocol.CmdClientStreamChunk, 1, AgentStreamChunkPayload{
		EventID: event.EventID, SessionID: event.SessionID,
		ClientMsgID: "stream-active-only", ChunkSeq: 1, DeltaContent: "still working",
	}))
	if len(streamHandler.calls) != 1 {
		t.Fatalf("active-only stream call count=%d want=1", len(streamHandler.calls))
	}

	mgr.handleSendMsg(conn, makePacket(t, protocol.CmdSendMsg, 2, SendMsgPayload{
		EventID: event.EventID, SessionID: event.SessionID,
		ClientMsgID: "send-active-only", Content: "final details",
	}))
	if len(sendHandler.calls) != 1 {
		t.Fatalf("active-only send call count=%d want=1", len(sendHandler.calls))
	}
}
