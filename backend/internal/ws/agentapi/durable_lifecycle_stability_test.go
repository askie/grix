package agentapi

import (
	"context"
	"encoding/json"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/testutil"
	"github.com/askie/grix/backend/internal/store"
	"github.com/askie/grix/backend/internal/ws/protocol"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func installDurableLifecycleTestStores(t *testing.T, withDB bool) {
	t.Helper()
	previousDB, previousRDB := store.DB, store.RDB
	var testDB *testutil.TestDB
	if withDB {
		testDB = testutil.NewTestDB()
		store.DB = testDB.DB
	} else {
		store.DB = nil
	}
	store.RDB = testutil.NewMockRedis()
	t.Cleanup(func() {
		_ = store.RDB.Close()
		if testDB != nil {
			testDB.Close()
		}
		store.DB, store.RDB = previousDB, previousRDB
	})
}

func durableLifecycleEvent(eventID string, agentID, ownerID int64) DelegateEventPayload {
	return DelegateEventPayload{
		EventID:     eventID,
		EventType:   "user_chat",
		AgentID:     agentID,
		OwnerID:     ownerID,
		SessionID:   "session-" + eventID,
		SessionType: 1,
		MsgID:       701,
		SenderID:    ownerID,
		Content:     "durable lifecycle test",
		CreatedAt:   time.Now().UnixMilli(),
	}
}

func forceDurableLifecycleRecord(
	t *testing.T,
	record *durablePendingDelegateRecord,
) {
	t.Helper()
	require.NotNil(t, record)
	if current, ok := loadDurablePendingDelegate(context.Background(), record.Event.EventID); ok &&
		record.Version <= current.Version {
		record.Version = current.Version + 1
	}
	raw, err := json.Marshal(record)
	require.NoError(t, err)
	ctx := context.Background()
	require.NoError(t, store.RDB.Set(
		ctx,
		durablePendingDelegateRecordKey(record.Event.EventID),
		raw,
		durablePendingDelegateTTL,
	).Err())
	require.NoError(t, store.RDB.ZAdd(
		ctx,
		durablePendingDelegateIndexKey(record.Event.AgentID),
		redis.Z{Score: float64(record.Event.CreatedAt), Member: record.Event.EventID},
	).Err())
}

func requireDurablePacket(
	t *testing.T,
	ch <-chan []byte,
	wantCmd string,
) protocol.Packet {
	t.Helper()
	select {
	case raw := <-ch:
		var packet protocol.Packet
		require.NoError(t, json.Unmarshal(raw, &packet))
		require.Equal(t, wantCmd, packet.Cmd)
		return packet
	case <-time.After(time.Second):
		t.Fatalf("timed out waiting for %s packet", wantCmd)
		return protocol.Packet{}
	}
}

func requireNoDurablePacket(t *testing.T, ch <-chan []byte) {
	t.Helper()
	select {
	case raw := <-ch:
		var packet protocol.Packet
		_ = json.Unmarshal(raw, &packet)
		t.Fatalf("unexpected packet cmd=%s", packet.Cmd)
	case <-time.After(50 * time.Millisecond):
	}
}

func claimRetryAtBarrier(
	t *testing.T,
	event DelegateEventPayload,
) []durableRetryClaim {
	t.Helper()
	start := make(chan struct{})
	results := make([]durableRetryClaim, 2)
	errs := make([]error, 2)
	var ready sync.WaitGroup
	var done sync.WaitGroup
	ready.Add(2)
	done.Add(2)
	for i := range results {
		go func(index int) {
			defer done.Done()
			ready.Done()
			<-start
			results[index], errs[index] = claimDurablePendingDelegateRetry(
				context.Background(),
				event.EventID,
				event.AgentID,
				event.OwnerID,
				agentAPIDeliveryMaxAttempts,
				time.Minute,
			)
		}(i)
	}
	ready.Wait()
	close(start)
	done.Wait()
	require.NoError(t, errs[0])
	require.NoError(t, errs[1])
	return results
}

func TestDurableRetryClaimBarrierIsMonotonicAndStopsAtThree(t *testing.T) {
	installDurableLifecycleTestStores(t, false)
	event := durableLifecycleEvent("retry-barrier", 5101, 5201)
	require.True(t, persistDurablePendingDelegate(context.Background(), durablePendingDelegateRecord{
		Event:   event,
		Attempt: 1,
		Stage:   durablePendingDelegateStageAck,
	}))

	first := claimRetryAtBarrier(t, event)
	require.Equal(t, 1, boolCount(first[0].Won, first[1].Won))
	record, ok := loadDurablePendingDelegate(context.Background(), event.EventID)
	require.True(t, ok)
	require.Equal(t, 2, record.Attempt)

	record.RetryClaimUntil = time.Now().Add(-time.Second).UnixMilli()
	record.RetryDispatchedAt = 0
	forceDurableLifecycleRecord(t, record)
	second := claimRetryAtBarrier(t, event)
	require.Equal(t, 1, boolCount(second[0].Won, second[1].Won))
	record, ok = loadDurablePendingDelegate(context.Background(), event.EventID)
	require.True(t, ok)
	require.Equal(t, agentAPIDeliveryMaxAttempts, record.Attempt)

	record.RetryClaimUntil = time.Now().Add(-time.Second).UnixMilli()
	record.RetryDispatchedAt = 0
	forceDurableLifecycleRecord(t, record)
	third := claimRetryAtBarrier(t, event)
	require.Equal(t, 0, boolCount(third[0].Won, third[1].Won))
	record, ok = loadDurablePendingDelegate(context.Background(), event.EventID)
	require.True(t, ok)
	require.Equal(t, agentAPIDeliveryMaxAttempts, record.Attempt)

	advanced, _, err := advanceDurablePendingDelegateAck(
		context.Background(),
		event.EventID,
		event.AgentID,
		event.OwnerID,
		time.Now().UnixMilli(),
	)
	require.NoError(t, err)
	require.True(t, advanced)
	afterAck, err := claimDurablePendingDelegateRetry(
		context.Background(),
		event.EventID,
		event.AgentID,
		event.OwnerID,
		agentAPIDeliveryMaxAttempts,
		time.Minute,
	)
	require.NoError(t, err)
	require.False(t, afterAck.Won)
}

func boolCount(values ...bool) int {
	count := 0
	for _, value := range values {
		if value {
			count++
		}
	}
	return count
}

func TestForwardedDurableRetryIsWireOnlyAndClaimedOnce(t *testing.T) {
	installDurableLifecycleTestStores(t, false)
	event := durableLifecycleEvent("retry-forwarded", 5301, 5401)
	require.True(t, persistDurablePendingDelegate(context.Background(), durablePendingDelegateRecord{
		Event:   event,
		Attempt: 1,
		Stage:   durablePendingDelegateStageAck,
	}))

	claim, err := claimDurablePendingDelegateRetry(
		context.Background(),
		event.EventID,
		event.AgentID,
		event.OwnerID,
		agentAPIDeliveryMaxAttempts,
		time.Minute,
	)
	require.NoError(t, err)
	require.True(t, claim.Won)

	manager := NewManager("", time.Second, nil, nil, nil, nil)
	defer manager.Shutdown()
	var deliveryCount, outputCount atomic.Int32
	manager.SetDeliveryStatusHandler(func(protocol.AgentDeliveryStatusPayload) {
		deliveryCount.Add(1)
	})
	manager.SetOutputStatusHandler(func(protocol.AgentOutputStatusPayload) {
		outputCount.Add(1)
	})
	conn := &agentConn{
		agentID:  event.AgentID,
		ownerID:  event.OwnerID,
		clientID: "forwarded-retry",
		send:     make(chan []byte, 4),
	}
	manager.putConnForTest(conn)

	require.True(t, manager.HandleForwardedDelegateRetry(claim.Envelope))
	packet := requireDurablePacket(t, conn.send, "event_msg")
	var forwarded DelegateEventPayload
	require.NoError(t, json.Unmarshal(packet.Payload, &forwarded))
	require.Equal(t, event.EventID, forwarded.EventID)
	require.False(t, manager.HandleForwardedDelegateRetry(claim.Envelope))
	requireNoDurablePacket(t, conn.send)
	require.Nil(t, manager.LookupActiveRun(event.EventID))
	manager.acksMu.Lock()
	_, tracked := manager.pending[event.EventID]
	manager.acksMu.Unlock()
	require.False(t, tracked)
	require.Equal(t, int32(0), deliveryCount.Load())
	require.Equal(t, int32(0), outputCount.Load())
}

func TestDurableReconnectAndLegacyQueueProduceOneWireRetry(t *testing.T) {
	installDurableLifecycleTestStores(t, false)
	event := durableLifecycleEvent("retry-reconnect", 5501, 5601)
	require.True(t, persistDurablePendingDelegate(context.Background(), durablePendingDelegateRecord{
		Event:   event,
		Attempt: 1,
		Stage:   durablePendingDelegateStageAck,
	}))
	require.True(t, enqueueDelegateEvent(context.Background(), event))

	manager := NewManager("", time.Second, nil, nil, nil, nil)
	defer manager.Shutdown()
	conn := &agentConn{
		agentID:  event.AgentID,
		ownerID:  event.OwnerID,
		clientID: "reconnect-retry",
		send:     make(chan []byte, 4),
	}
	manager.putConnForTest(conn)

	manager.drainDurablePendingDelegateAcks(conn, 10)
	manager.drainQueuedDelegateEvents(conn, 10)
	manager.drainDurablePendingDelegateAcks(conn, 10)
	requireDurablePacket(t, conn.send, "event_msg")
	requireNoDurablePacket(t, conn.send)

	record, ok := loadDurablePendingDelegate(context.Background(), event.EventID)
	require.True(t, ok)
	require.Equal(t, 2, record.Attempt)
	require.Positive(t, record.RetryDispatchedAt)
	manager.acksMu.Lock()
	pending := manager.pending[event.EventID]
	manager.acksMu.Unlock()
	require.NotNil(t, pending)
	require.Equal(t, 2, pending.attempt)
}

func TestNegotiatedTerminalCommitTokenIsDurableAndRequiredForAck(t *testing.T) {
	installDurableLifecycleTestStores(t, true)
	event := durableLifecycleEvent("terminal-token-negotiated", 5551, 5651)
	manager := NewManager("", time.Second, nil, nil, nil, nil)
	defer manager.Shutdown()
	conn := &agentConn{
		agentID:      event.AgentID,
		ownerID:      event.OwnerID,
		clientID:     "terminal-token-connector",
		capabilities: []string{terminalCommitCapability, "event_result_ack"},
		send:         make(chan []byte, 8),
	}
	manager.putConnForTest(conn)

	require.True(t, manager.PushDelegateEvent(event))
	packet := requireDurablePacket(t, conn.send, protocol.CmdEventMsg)
	var dispatched DelegateEventPayload
	require.NoError(t, json.Unmarshal(packet.Payload, &dispatched))
	require.NotEmpty(t, dispatched.TerminalCommitToken)

	ledger, err := store.LoadAgentEventTerminalLedger(event.EventID)
	require.NoError(t, err)
	require.NotNil(t, ledger)
	require.Equal(t, dispatched.TerminalCommitToken, ledger.TerminalCommitToken)
	record, ok := loadDurablePendingDelegate(context.Background(), event.EventID)
	require.True(t, ok)
	require.Equal(t, dispatched.TerminalCommitToken, record.Event.TerminalCommitToken)

	manager.handleEventResult(conn, makePacket(t, protocol.CmdEventResult, 41, EventResultPayload{
		EventID: event.EventID,
		Status:  protocol.AgentEventResultFailed,
		Code:    "test_failure",
	}))
	rejected := requireDurablePacket(t, conn.send, protocol.CmdError)
	var rejection SendNackPayload
	require.NoError(t, json.Unmarshal(rejected.Payload, &rejection))
	require.Equal(t, 4003, rejection.Code)

	manager.handleEventResult(conn, makePacket(t, protocol.CmdEventResult, 42, EventResultPayload{
		EventID:             event.EventID,
		TerminalCommitToken: dispatched.TerminalCommitToken,
		Status:              protocol.AgentEventResultFailed,
		Code:                "test_failure",
	}))
	ack := requireDurablePacket(t, conn.send, protocol.CmdSendAck)
	var ackPayload struct {
		EventID             string `json:"event_id"`
		Status              string `json:"status"`
		TerminalCommitToken string `json:"terminal_commit_token"`
		TerminalCommitted   bool   `json:"terminal_committed"`
	}
	require.NoError(t, json.Unmarshal(ack.Payload, &ackPayload))
	require.Equal(t, event.EventID, ackPayload.EventID)
	require.Equal(t, protocol.AgentEventResultFailed, ackPayload.Status)
	require.Equal(t, dispatched.TerminalCommitToken, ackPayload.TerminalCommitToken)
	require.True(t, ackPayload.TerminalCommitted)

	legacy := &agentConn{
		agentID:  event.AgentID,
		ownerID:  event.OwnerID,
		clientID: "legacy-connector",
		send:     make(chan []byte, 1),
	}
	require.False(t, manager.sendDelegateEventAttempt(legacy, dispatched, 2))
	requireNoDurablePacket(t, legacy.send)
}

func TestDurableRecordSnapshotPreservesTerminalCommitTokenForStopRouting(t *testing.T) {
	event := durableLifecycleEvent("durable-stop-token", 5661, 5761)
	event.TerminalCommitToken = "CaseSensitiveStopToken"
	snapshot := durableRecordToSnapshot(&durablePendingDelegateRecord{Event: event})
	require.NotNil(t, snapshot)
	require.Equal(t, event.EventID, snapshot.EventID)
	require.Equal(t, event.TerminalCommitToken, snapshot.TerminalCommitToken)
}

func TestDurableEventResultWinnerBarrierAndSettledTombstone(t *testing.T) {
	installDurableLifecycleTestStores(t, true)
	event := durableLifecycleEvent("terminal-barrier", 5701, 5801)
	require.True(t, persistDurablePendingDelegate(context.Background(), durablePendingDelegateRecord{
		Event:      event,
		Attempt:    1,
		Stage:      durablePendingDelegateStageResult,
		ReceivedAt: time.Now().UnixMilli(),
	}))

	first := NewManager("", time.Second, nil, nil, nil, nil)
	second := NewManager("", time.Second, nil, nil, nil, nil)
	defer first.Shutdown()
	defer second.Shutdown()
	var deliveryCount, outputCount atomic.Int32
	entered := make(chan struct{})
	release := make(chan struct{})
	first.SetDeliveryStatusHandler(func(protocol.AgentDeliveryStatusPayload) {
		deliveryCount.Add(1)
		close(entered)
		<-release
	})
	first.SetOutputStatusHandler(func(protocol.AgentOutputStatusPayload) {
		outputCount.Add(1)
	})
	second.SetDeliveryStatusHandler(func(protocol.AgentDeliveryStatusPayload) {
		deliveryCount.Add(1)
	})
	second.SetOutputStatusHandler(func(protocol.AgentOutputStatusPayload) {
		outputCount.Add(1)
	})
	firstConn := &agentConn{
		agentID: event.AgentID, ownerID: event.OwnerID,
		capabilities: []string{"event_result_ack"},
		send:         make(chan []byte, 4),
	}
	secondConn := &agentConn{
		agentID: event.AgentID, ownerID: event.OwnerID,
		capabilities: []string{"event_result_ack"},
		send:         make(chan []byte, 4),
	}
	result := EventResultPayload{
		EventID: event.EventID,
		Status:  protocol.AgentEventResultResponded,
	}

	firstDone := make(chan struct{})
	go func() {
		defer close(firstDone)
		first.handleEventResult(firstConn, makePacket(t, protocol.CmdEventResult, 1, result))
	}()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("winner did not enter terminal side effects")
	}

	second.handleEventResult(secondConn, makePacket(t, protocol.CmdEventResult, 2, result))
	requireNoDurablePacket(t, secondConn.send)
	close(release)
	select {
	case <-firstDone:
	case <-time.After(time.Second):
		t.Fatal("winner did not finish settlement")
	}
	requireDurablePacket(t, firstConn.send, protocol.CmdSendAck)

	second.handleEventResult(secondConn, makePacket(t, protocol.CmdEventResult, 3, result))
	requireDurablePacket(t, secondConn.send, protocol.CmdSendAck)
	conflicting := result
	conflicting.Status = protocol.AgentEventResultFailed
	conflicting.Code = "late_conflict"
	second.handleEventResult(secondConn, makePacket(t, protocol.CmdEventResult, 4, conflicting))
	requireDurablePacket(t, secondConn.send, "error")

	record, ok := loadDurablePendingDelegate(context.Background(), event.EventID)
	require.True(t, ok)
	require.Equal(t, durablePendingDelegateStageSettled, record.Stage)
	require.NotNil(t, record.Terminal)
	require.Positive(t, record.Terminal.SettledAt)
	require.Equal(t, int32(1), deliveryCount.Load())
	require.Equal(t, int32(1), outputCount.Load())
}

func TestDurableEventResultReclaimsExpiredIntentAfterCrash(t *testing.T) {
	installDurableLifecycleTestStores(t, true)
	event := durableLifecycleEvent("terminal-crash", 5901, 6001)
	require.True(t, persistDurablePendingDelegate(context.Background(), durablePendingDelegateRecord{
		Event:      event,
		Attempt:    1,
		Stage:      durablePendingDelegateStageResult,
		ReceivedAt: time.Now().UnixMilli(),
	}))
	result := EventResultPayload{
		EventID: event.EventID,
		Status:  protocol.AgentEventResultFailed,
		Code:    "connector_failed",
		Msg:     "worker exited",
	}
	crashed, err := claimDurableTerminalIntent(
		context.Background(),
		event.EventID,
		event.AgentID,
		event.OwnerID,
		result,
		nil,
	)
	require.NoError(t, err)
	require.Equal(t, terminalIntentClaimed, crashed.Disposition)
	require.NotNil(t, crashed.Record)
	crashed.Record.Terminal.ClaimUntil = time.Now().Add(-time.Second).UnixMilli()
	forceDurableLifecycleRecord(t, crashed.Record)

	manager := NewManager("", time.Second, nil, nil, nil, nil)
	defer manager.Shutdown()
	var deliveryCount atomic.Int32
	manager.SetDeliveryStatusHandler(func(protocol.AgentDeliveryStatusPayload) {
		deliveryCount.Add(1)
	})
	conn := &agentConn{
		agentID: event.AgentID, ownerID: event.OwnerID,
		capabilities: []string{"event_result_ack"},
		send:         make(chan []byte, 2),
	}
	manager.handleEventResult(conn, makePacket(t, protocol.CmdEventResult, 11, result))
	requireDurablePacket(t, conn.send, protocol.CmdSendAck)
	record, ok := loadDurablePendingDelegate(context.Background(), event.EventID)
	require.True(t, ok)
	require.Equal(t, durablePendingDelegateStageSettled, record.Stage)
	require.Equal(t, int32(1), deliveryCount.Load())
}

func TestLateEventResultAfterTrackingExpiryUsesChatStateCAS(t *testing.T) {
	installDurableLifecycleTestStores(t, true)
	const (
		agentID = int64(6101)
		ownerID = int64(6201)
	)

	t.Run("terminal fence beats delayed running write", func(t *testing.T) {
		event := durableLifecycleEvent("terminal-before-running", agentID, ownerID)
		completedAt := time.Now()
		changed, err := store.SettleSessionAgentStateByRun(model.SessionAgentState{
			SessionID:   event.SessionID,
			OwnerID:     event.OwnerID,
			AgentID:     event.AgentID,
			State:       model.SessionAgentStateCompleted,
			LastRunID:   event.EventID,
			CompletedAt: &completedAt,
		})
		require.NoError(t, err)
		require.True(t, changed)

		delayedStart := completedAt.Add(-time.Second)
		store.UpsertSessionAgentStateRunning(
			event.SessionID,
			event.OwnerID,
			event.AgentID,
			event.EventID,
			delayedStart,
		)
		var state model.SessionAgentState
		require.NoError(t, store.DB.First(&state, "session_id = ? AND owner_id = ?", event.SessionID, ownerID).Error)
		require.Equal(t, model.SessionAgentStateCompleted, state.State)
		require.Equal(t, event.EventID, state.LastRunID)

		newRunID := event.EventID + "-new"
		newStart := completedAt.Add(time.Second)
		store.UpsertSessionAgentStateRunning(
			event.SessionID,
			event.OwnerID,
			event.AgentID,
			newRunID,
			newStart,
		)
		store.UpsertSessionAgentStateRunning(
			event.SessionID,
			event.OwnerID,
			event.AgentID,
			event.EventID,
			delayedStart,
		)
		require.NoError(t, store.DB.First(&state, "session_id = ? AND owner_id = ?", event.SessionID, ownerID).Error)
		require.Equal(t, model.SessionAgentStateRunning, state.State)
		require.Equal(t, newRunID, state.LastRunID)
	})

	t.Run("current run settles and acknowledges", func(t *testing.T) {
		event := durableLifecycleEvent("late-current", agentID, ownerID)
		store.UpsertSessionAgentStateRunning(
			event.SessionID,
			event.OwnerID,
			event.AgentID,
			event.EventID,
			time.Now(),
		)
		deleteDurablePendingDelegate(context.Background(), event.EventID, event.AgentID)

		manager := NewManager("", time.Second, nil, nil, nil, nil)
		defer manager.Shutdown()
		var deliveryCount, outputCount atomic.Int32
		manager.SetDeliveryStatusHandler(func(protocol.AgentDeliveryStatusPayload) {
			deliveryCount.Add(1)
		})
		manager.SetOutputStatusHandler(func(protocol.AgentOutputStatusPayload) {
			outputCount.Add(1)
		})
		conn := &agentConn{
			agentID: agentID, ownerID: ownerID,
			capabilities: []string{"event_result_ack"},
			send:         make(chan []byte, 2),
		}
		manager.handleEventResult(conn, makePacket(t, protocol.CmdEventResult, 21, EventResultPayload{
			EventID: event.EventID,
			Status:  protocol.AgentEventResultResponded,
		}))
		requireDurablePacket(t, conn.send, protocol.CmdSendAck)

		var state model.SessionAgentState
		require.NoError(t, store.DB.First(&state, "session_id = ? AND owner_id = ?", event.SessionID, ownerID).Error)
		require.Equal(t, model.SessionAgentStateCompleted, state.State)
		require.Equal(t, event.EventID, state.LastRunID)
		require.Equal(t, int32(1), deliveryCount.Load())
		require.Equal(t, int32(1), outputCount.Load())
		record, ok := loadDurablePendingDelegate(context.Background(), event.EventID)
		require.True(t, ok)
		require.Equal(t, durablePendingDelegateStageSettled, record.Stage)
	})

	t.Run("older result cannot overwrite newer run", func(t *testing.T) {
		oldEvent := durableLifecycleEvent("late-old", agentID, ownerID)
		newRunID := "late-new"
		store.UpsertSessionAgentStateRunning(
			oldEvent.SessionID,
			oldEvent.OwnerID,
			oldEvent.AgentID,
			newRunID,
			time.Now(),
		)
		manager := NewManager("", time.Second, nil, nil, nil, nil)
		defer manager.Shutdown()
		conn := &agentConn{
			agentID: agentID, ownerID: ownerID,
			capabilities: []string{"event_result_ack"},
			send:         make(chan []byte, 2),
		}
		manager.handleEventResult(conn, makePacket(t, protocol.CmdEventResult, 22, EventResultPayload{
			EventID: oldEvent.EventID,
			Status:  protocol.AgentEventResultFailed,
			Code:    "stale_failure",
		}))
		requireDurablePacket(t, conn.send, "error")

		var state model.SessionAgentState
		require.NoError(t, store.DB.First(&state, "session_id = ? AND owner_id = ?", oldEvent.SessionID, ownerID).Error)
		require.Equal(t, model.SessionAgentStateRunning, state.State)
		require.Equal(t, newRunID, state.LastRunID)
		_, durable := loadDurablePendingDelegate(context.Background(), oldEvent.EventID)
		require.False(t, durable)
	})

	t.Run("foreign owner is rejected without acknowledgement", func(t *testing.T) {
		event := durableLifecycleEvent("late-foreign", agentID, ownerID)
		store.UpsertSessionAgentStateRunning(
			event.SessionID,
			event.OwnerID,
			event.AgentID,
			event.EventID,
			time.Now(),
		)
		manager := NewManager("", time.Second, nil, nil, nil, nil)
		defer manager.Shutdown()
		conn := &agentConn{
			agentID: agentID, ownerID: ownerID + 1,
			capabilities: []string{"event_result_ack"},
			send:         make(chan []byte, 2),
		}
		manager.handleEventResult(conn, makePacket(t, protocol.CmdEventResult, 23, EventResultPayload{
			EventID: event.EventID,
			Status:  protocol.AgentEventResultResponded,
		}))
		requireDurablePacket(t, conn.send, "error")

		var state model.SessionAgentState
		require.NoError(t, store.DB.First(&state, "session_id = ? AND owner_id = ?", event.SessionID, ownerID).Error)
		require.Equal(t, model.SessionAgentStateRunning, state.State)
		require.Equal(t, event.EventID, state.LastRunID)
	})
}
