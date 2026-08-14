package agentapi

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/askie/grix/backend/internal/ws/protocol"
)

func TestUnregisterEmitsOwnerOfflineWhileAnotherOwnerRemainsOnline(t *testing.T) {
	m := NewManager("", time.Second, nil, nil, nil, nil)
	defer m.Shutdown()

	type stateEvent struct {
		ownerID int64
		payload protocol.AgentStateSyncPayload
	}
	var (
		eventsMu sync.Mutex
		events   []stateEvent
	)
	m.SetAgentStateHandler(func(ownerID int64, payload protocol.AgentStateSyncPayload) {
		eventsMu.Lock()
		events = append(events, stateEvent{ownerID: ownerID, payload: payload})
		eventsMu.Unlock()
	})

	const (
		agentID = int64(7101)
		ownerA  = int64(8101)
		ownerB  = int64(8102)
	)
	connA := &agentConn{
		agentID:         agentID,
		ownerID:         ownerA,
		isPrimary:       true,
		connectedAt:     time.Unix(1_700_000_100, 100_000),
		connectionEpoch: 41,
		send:            make(chan []byte, 1),
		done:            make(chan struct{}),
	}
	connB := &agentConn{
		agentID: agentID,
		ownerID: ownerB,
		// Simulate another node whose wall clock is behind owner A's node.
		// State ordering must use the allocated generation, not this timestamp.
		connectedAt:     time.Unix(1_699_999_900, 200_000),
		connectionEpoch: 42,
		send:            make(chan []byte, 1),
		done:            make(chan struct{}),
	}
	m.putConnForTest(connA)
	m.putConnForTest(connB)

	m.unregister(connB)

	if got := m.lookupConnByOwner(agentID, ownerA); got != connA {
		t.Fatal("other owner connection should remain online")
	}
	eventsMu.Lock()
	defer eventsMu.Unlock()
	if len(events) != 1 {
		t.Fatalf("disconnected owner should receive one offline state, got=%d", len(events))
	}
	if events[0].ownerID != ownerB || events[0].payload.State != protocol.AgentStateOffline {
		t.Fatalf("unexpected offline state event: %#v", events[0])
	}
	var extra agentStateExtra
	if err := json.Unmarshal(events[0].payload.Extra, &extra); err != nil {
		t.Fatalf("decode state extra: %v", err)
	}
	if extra.ConnectionEpoch != connB.connectionEpoch {
		t.Fatalf(
			"connection_epoch=%d want=%d",
			extra.ConnectionEpoch,
			connB.connectionEpoch,
		)
	}
}

func TestRefreshAgentLeaseAfterUnregisterCannotResurrectOnline(t *testing.T) {
	m := NewManager("", time.Second, nil, nil, nil, nil)
	defer m.Shutdown()

	var (
		eventsMu sync.Mutex
		events   []protocol.AgentStateSyncPayload
	)
	m.SetAgentStateHandler(func(_ int64, payload protocol.AgentStateSyncPayload) {
		eventsMu.Lock()
		events = append(events, payload)
		eventsMu.Unlock()
	})

	conn := &agentConn{
		agentID:         7102,
		ownerID:         8103,
		isPrimary:       true,
		connectedAt:     time.Now(),
		connectionEpoch: 77,
		send:            make(chan []byte, 1),
		done:            make(chan struct{}),
	}
	m.putConnForTest(conn)

	m.unregister(conn)
	m.refreshAgentLease(conn)

	eventsMu.Lock()
	defer eventsMu.Unlock()
	if len(events) != 1 {
		t.Fatalf("late refresh emitted state after unregister: %#v", events)
	}
	if events[0].State != protocol.AgentStateOffline {
		t.Fatalf("final state=%s want=%s", events[0].State, protocol.AgentStateOffline)
	}
	var extra agentStateExtra
	if err := json.Unmarshal(events[0].Extra, &extra); err != nil {
		t.Fatalf("decode state extra: %v", err)
	}
	if extra.ConnectionEpoch != conn.connectionEpoch {
		t.Fatalf("offline epoch=%d want=%d", extra.ConnectionEpoch, conn.connectionEpoch)
	}
}

func TestConcurrentRefreshAndUnregisterPublishOnlineThenOffline(t *testing.T) {
	m := NewManager("", time.Second, nil, nil, nil, nil)
	defer m.Shutdown()

	var (
		eventsMu sync.Mutex
		events   []protocol.AgentStateSyncPayload
	)
	onlineStarted := make(chan struct{})
	releaseOnline := make(chan struct{})
	m.SetAgentStateHandler(func(_ int64, payload protocol.AgentStateSyncPayload) {
		eventsMu.Lock()
		events = append(events, payload)
		eventsMu.Unlock()
		if payload.State == protocol.AgentStateOnline {
			close(onlineStarted)
			<-releaseOnline
		}
	})

	conn := &agentConn{
		agentID:         7104,
		ownerID:         8105,
		isPrimary:       true,
		connectedAt:     time.Now(),
		connectionEpoch: 78,
		send:            make(chan []byte, 1),
		done:            make(chan struct{}),
	}
	m.putConnForTest(conn)

	refreshDone := make(chan struct{})
	go func() {
		m.refreshAgentLease(conn)
		close(refreshDone)
	}()
	select {
	case <-onlineStarted:
	case <-time.After(time.Second):
		t.Fatal("lease refresh did not reach online publication")
	}

	unregisterStarted := make(chan struct{})
	unregisterDone := make(chan struct{})
	go func() {
		close(unregisterStarted)
		m.unregister(conn)
		close(unregisterDone)
	}()
	<-unregisterStarted
	select {
	case <-unregisterDone:
		t.Fatal("unregister passed an in-flight online publication")
	case <-time.After(25 * time.Millisecond):
	}

	close(releaseOnline)
	select {
	case <-refreshDone:
	case <-time.After(time.Second):
		t.Fatal("lease refresh did not finish")
	}
	select {
	case <-unregisterDone:
	case <-time.After(time.Second):
		t.Fatal("unregister did not finish after online publication")
	}

	// Any callback queued after unregister must observe that conn is no longer
	// current and must not append another online state.
	m.refreshAgentLease(conn)

	eventsMu.Lock()
	defer eventsMu.Unlock()
	if len(events) != 2 {
		t.Fatalf("state event count=%d want=2 events=%#v", len(events), events)
	}
	if events[0].State != protocol.AgentStateOnline || events[1].State != protocol.AgentStateOffline {
		t.Fatalf("state ordering=%s -> %s want=online -> offline", events[0].State, events[1].State)
	}
}

func TestReserveConnectionEpochRejectsInvalidAllocatorResult(t *testing.T) {
	m := NewManager("", time.Second, nil, nil, nil, nil)
	defer m.Shutdown()
	m.SetConnectionEpochAllocator(func(context.Context, int64, int64) (int64, error) {
		return 0, nil
	})

	if epoch, err := m.reserveConnectionEpoch(context.Background(), 8104, 7103); err == nil {
		t.Fatalf("expected invalid allocation to fail, got epoch=%d", epoch)
	}
}
