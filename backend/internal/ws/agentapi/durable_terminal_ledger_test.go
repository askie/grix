package agentapi

import (
	"context"
	"encoding/json"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/testutil"
	"github.com/askie/grix/backend/internal/store"
	"github.com/askie/grix/backend/internal/ws/protocol"
	"github.com/stretchr/testify/require"
	"gorm.io/datatypes"
)

func TestDurableDrainScansPastIneligibleRecords(t *testing.T) {
	installDurableLifecycleTestStores(t, false)
	const (
		agentID     = int64(7101)
		targetOwner = int64(7201)
	)
	for i := 0; i < durablePendingDelegateDrainBatch+32; i++ {
		event := durableLifecycleEvent(
			"drain-ineligible-"+time.UnixMilli(int64(i)).Format("150405.000"),
			agentID,
			targetOwner+1,
		)
		event.CreatedAt = int64(i + 1)
		require.True(t, persistDurablePendingDelegate(context.Background(), durablePendingDelegateRecord{
			Event:   event,
			Attempt: 1,
			Stage:   durablePendingDelegateStageResult,
		}))
	}
	target := durableLifecycleEvent("drain-target", agentID, targetOwner)
	target.CreatedAt = 999999
	require.True(t, persistDurablePendingDelegate(context.Background(), durablePendingDelegateRecord{
		Event:   target,
		Attempt: 1,
		Stage:   durablePendingDelegateStageAck,
	}))

	manager := NewManager("", time.Second, nil, nil, nil, nil)
	defer manager.Shutdown()
	conn := &agentConn{
		agentID: target.AgentID,
		ownerID: target.OwnerID,
		send:    make(chan []byte, 2),
	}
	manager.putConnForTest(conn)
	manager.drainDurablePendingDelegateAcks(conn, 1)

	packet := requireDurablePacket(t, conn.send, "event_msg")
	var sent DelegateEventPayload
	require.NoError(t, json.Unmarshal(packet.Payload, &sent))
	require.Equal(t, target.EventID, sent.EventID)
	record, ok := loadDurablePendingDelegate(context.Background(), target.EventID)
	require.True(t, ok)
	require.Equal(t, 2, record.Attempt)
}

func TestDurableVersionCASRejectsSameMillisecondMutation(t *testing.T) {
	installDurableLifecycleTestStores(t, false)
	event := durableLifecycleEvent("version-same-ms", 7301, 7401)
	const observedAt = int64(1785211200000)
	require.True(t, persistDurablePendingDelegate(context.Background(), durablePendingDelegateRecord{
		Event:           event,
		Attempt:         1,
		Stage:           durablePendingDelegateStageAck,
		RetryToken:      "same-ms-token",
		RetryClaimUntil: observedAt + 1000,
		UpdatedAt:       observedAt,
	}))
	before, ok := loadDurablePendingDelegate(context.Background(), event.EventID)
	require.True(t, ok)
	require.Equal(t, int64(1), before.Version)

	require.NoError(t, releaseDurableRetryDispatchScript.Run(
		context.Background(),
		store.RDB,
		[]string{durablePendingDelegateRecordKey(event.EventID)},
		"same-ms-token",
		observedAt,
		durablePendingDelegateTTLSeconds(),
	).Err())
	after, ok := loadDurablePendingDelegate(context.Background(), event.EventID)
	require.True(t, ok)
	require.Equal(t, observedAt, after.UpdatedAt)
	require.Equal(t, before.Version+1, after.Version)

	require.False(t, deleteDurablePendingDelegateIfUnchanged(
		context.Background(),
		event.EventID,
		event.AgentID,
		event.OwnerID,
		before.Version,
		before.Stage,
		before.Attempt,
		before.DispatchGeneration,
	))
	_, ok = loadDurablePendingDelegate(context.Background(), event.EventID)
	require.True(t, ok)
}

func TestAckLoserSynchronizesResultStageAndVersion(t *testing.T) {
	installDurableLifecycleTestStores(t, false)
	event := durableLifecycleEvent("ack-loser-sync", 7351, 7451)
	manager := NewManager("", time.Second, nil, nil, nil, nil)
	defer manager.Shutdown()
	manager.eventAckWait = 20 * time.Millisecond
	manager.eventResultWait = time.Second
	require.Equal(
		t,
		pendingEventRegistrationCreated,
		manager.registerPendingEventAck(event, 1),
	)

	advanced, winnerRecord, err := advanceDurablePendingDelegateAck(
		context.Background(),
		event.EventID,
		event.AgentID,
		event.OwnerID,
		time.Now().UnixMilli(),
	)
	require.NoError(t, err)
	require.True(t, advanced)
	require.NotNil(t, winnerRecord)
	manager.resolvePendingEventAck(event.EventID, time.Now().UnixMilli())

	manager.acksMu.Lock()
	entry := manager.pending[event.EventID]
	require.NotNil(t, entry)
	require.Equal(t, pendingEventStageResult, entry.stage)
	require.Equal(t, winnerRecord.Attempt, entry.attempt)
	require.Equal(t, winnerRecord.Version, entry.durableVersion)
	manager.acksMu.Unlock()
	time.Sleep(3 * manager.eventAckWait)
	manager.acksMu.Lock()
	entry = manager.pending[event.EventID]
	require.NotNil(t, entry)
	require.Equal(t, pendingEventStageResult, entry.stage)
	manager.acksMu.Unlock()
}

func TestExpiryTimerCannotDeleteNewerDurableVersion(t *testing.T) {
	installDurableLifecycleTestStores(t, false)
	event := durableLifecycleEvent("expiry-version-fence", 7361, 7461)
	manager := NewManager("", time.Second, nil, nil, nil, nil)
	defer manager.Shutdown()
	manager.pendingTrackingTTL = time.Hour
	require.Equal(
		t,
		pendingEventRegistrationCreated,
		manager.registerPendingEventAck(event, 1),
	)
	manager.acksMu.Lock()
	entry := manager.pending[event.EventID]
	require.NotNil(t, entry)
	oldVersion := entry.durableVersion
	entry.trackingExpireAt = time.Now().Add(-time.Millisecond).UnixMilli()
	manager.acksMu.Unlock()

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
	require.NotNil(t, claim.Record)
	require.Greater(t, claim.Record.Version, oldVersion)

	manager.timeoutPendingEvent(event.EventID)
	durable, ok := loadDurablePendingDelegate(context.Background(), event.EventID)
	require.True(t, ok)
	require.Equal(t, claim.Record.Version, durable.Version)
	manager.acksMu.Lock()
	entry = manager.pending[event.EventID]
	require.NotNil(t, entry)
	require.Equal(t, durable.Version, entry.durableVersion)
	require.Equal(t, durable.Attempt, entry.attempt)
	manager.acksMu.Unlock()
}

func TestMaxAttemptLoserSynchronizesWithoutIncrementLoop(t *testing.T) {
	installDurableLifecycleTestStores(t, false)
	event := durableLifecycleEvent("max-attempt-sync", 7371, 7471)
	require.True(t, persistDurablePendingDelegate(context.Background(), durablePendingDelegateRecord{
		Event:   event,
		Attempt: agentAPIDeliveryMaxAttempts,
		Stage:   durablePendingDelegateStageAck,
	}))
	manager := NewManager("", time.Second, nil, nil, nil, nil)
	defer manager.Shutdown()
	manager.pendingTrackingTTL = time.Hour
	entry := manager.recoverPendingFromDurable(event.EventID, event.AgentID)
	require.NotNil(t, entry)
	manager.timeoutPendingEvent(event.EventID)
	manager.timeoutPendingEvent(event.EventID)

	record, ok := loadDurablePendingDelegate(context.Background(), event.EventID)
	require.True(t, ok)
	require.Equal(t, agentAPIDeliveryMaxAttempts, record.Attempt)
	manager.acksMu.Lock()
	entry = manager.pending[event.EventID]
	require.NotNil(t, entry)
	require.Equal(t, agentAPIDeliveryMaxAttempts, entry.attempt)
	require.Equal(t, record.Version, entry.durableVersion)
	manager.acksMu.Unlock()
}

func TestDuplicateInitialDispatchCannotRegressTerminalRecord(t *testing.T) {
	installDurableLifecycleTestStores(t, false)
	event := durableLifecycleEvent("duplicate-after-terminal", 7501, 7601)
	require.True(t, persistDurablePendingDelegate(context.Background(), durablePendingDelegateRecord{
		Event:      event,
		Attempt:    agentAPIDeliveryMaxAttempts,
		Stage:      durablePendingDelegateStageSettled,
		ReceivedAt: time.Now().UnixMilli(),
		Terminal: &durableTerminalIntent{
			Status:    protocol.AgentEventResultResponded,
			CreatedAt: time.Now().UnixMilli(),
			SettledAt: time.Now().UnixMilli(),
		},
	}))
	before, ok := loadDurablePendingDelegate(context.Background(), event.EventID)
	require.True(t, ok)

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
		agentID: event.AgentID,
		ownerID: event.OwnerID,
		send:    make(chan []byte, 2),
	}

	require.True(t, manager.dispatchDelegateEvent(conn, event))
	requireNoDurablePacket(t, conn.send)
	after, ok := loadDurablePendingDelegate(context.Background(), event.EventID)
	require.True(t, ok)
	require.Equal(t, durablePendingDelegateStageSettled, after.Stage)
	require.Equal(t, before.Version, after.Version)
	require.Equal(t, int32(0), deliveryCount.Load())
	require.Equal(t, int32(0), outputCount.Load())
	require.Nil(t, manager.LookupActiveRun(event.EventID))
}

func TestInitialSendFailureRequeuesWithoutFalseTerminalState(t *testing.T) {
	installDurableLifecycleTestStores(t, true)
	event := durableLifecycleEvent("initial-send-requeue", 7701, 7801)
	manager := NewManager("", time.Second, nil, nil, nil, nil)
	defer manager.Shutdown()
	var failedDelivery, failedOutput atomic.Int32
	manager.SetDeliveryStatusHandler(func(status protocol.AgentDeliveryStatusPayload) {
		if status.Status == protocol.AgentDeliveryStatusFailed {
			failedDelivery.Add(1)
		}
	})
	manager.SetOutputStatusHandler(func(status protocol.AgentOutputStatusPayload) {
		if status.State == protocol.AgentOutputStateFailed {
			failedOutput.Add(1)
		}
	})
	dead := &agentConn{
		agentID: event.AgentID,
		ownerID: event.OwnerID,
		send:    make(chan []byte, 1),
		done:    make(chan struct{}),
	}
	dead.send <- []byte("full")
	manager.putConnForTest(dead)

	require.True(t, manager.PushDelegateEvent(event))
	require.Equal(t, int32(0), failedDelivery.Load())
	require.Equal(t, int32(0), failedOutput.Load())
	require.Nil(t, manager.LookupActiveRun(event.EventID))
	_, durable := loadDurablePendingDelegate(context.Background(), event.EventID)
	require.False(t, durable)
	var chatCount int64
	require.NoError(t, store.DB.Model(&model.SessionAgentState{}).
		Where("last_run_id = ?", event.EventID).
		Count(&chatCount).Error)
	require.Zero(t, chatCount)
	require.Equal(t, int64(1), store.RDB.LLen(
		context.Background(),
		queuedDelegateEventListKey(event.AgentID),
	).Val())

	reconnected := &agentConn{
		agentID: event.AgentID,
		ownerID: event.OwnerID,
		send:    make(chan []byte, 2),
		done:    make(chan struct{}),
	}
	manager.register(reconnected)
	packet := requireDurablePacket(t, reconnected.send, "event_msg")
	var replay DelegateEventPayload
	require.NoError(t, json.Unmarshal(packet.Payload, &replay))
	require.Equal(t, event.EventID, replay.EventID)
	require.Equal(t, int32(0), failedDelivery.Load())
	require.Equal(t, int32(0), failedOutput.Load())
}

func TestTerminalLedgerReplaysAfterRedisExpiryAndThenPrunesSnapshot(t *testing.T) {
	installDurableLifecycleTestStores(t, true)
	event := durableLifecycleEvent("ledger-replay", 7901, 8001)
	event.ThreadID = "thread-ledger"
	event.QuotedMessageID = 9876
	event.Content = "sensitive prompt retained only while effects are pending"
	event.Extra = json.RawMessage(`{"workspace":"/tmp/project"}`)
	event.ContextMessages = []protocol.ContextMessagePayload{{
		MsgID:      44,
		SenderID:   event.OwnerID,
		SenderType: 1,
		MsgType:    1,
		Content:    "prior context",
		CreatedAt:  1234,
	}}
	record := &durablePendingDelegateRecord{
		Event:      event,
		Attempt:    1,
		Stage:      durablePendingDelegateStageResult,
		StartedAt:  time.Now().Add(-time.Second).UnixMilli(),
		ReceivedAt: time.Now().UnixMilli(),
	}
	payload := EventResultPayload{
		EventID: event.EventID,
		Status:  protocol.AgentEventResultResponded,
		Code:    "ok",
		Msg:     "done",
	}
	preparer := NewManager("", time.Second, nil, nil, nil, nil)
	defer preparer.Shutdown()
	ledger, err := preparer.prepareTerminalResult(payload, record)
	require.NoError(t, err)
	require.NotNil(t, ledger)
	require.True(t, ledger.TaskNotificationAllowed)
	restored := durableRecordFromTerminalLedger(ledger)
	require.NotNil(t, restored)
	require.Equal(t, event.ThreadID, restored.Event.ThreadID)
	require.Equal(t, event.QuotedMessageID, restored.Event.QuotedMessageID)
	require.Equal(t, event.Content, restored.Event.Content)
	require.JSONEq(t, string(event.Extra), string(restored.Event.Extra))
	require.Equal(t, event.ContextMessages, restored.Event.ContextMessages)
	committed, err := store.MarkAgentEventTerminalRedisCommitted(
		event.EventID,
		event.OwnerID,
		event.AgentID,
		payload.Status,
		payload.Code,
		payload.Msg,
	)
	require.NoError(t, err)
	require.True(t, committed)
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
		agentID:      event.AgentID,
		ownerID:      event.OwnerID,
		capabilities: []string{"event_result_ack"},
		send:         make(chan []byte, 4),
	}
	manager.handleEventResult(conn, makePacket(t, protocol.CmdEventResult, 100, payload))
	requireDurablePacket(t, conn.send, protocol.CmdSendAck)
	require.Equal(t, int32(1), deliveryCount.Load())
	require.Equal(t, int32(1), outputCount.Load())

	var completed model.AgentEventTerminalLedger
	require.NoError(t, store.DB.First(&completed, "event_id = ?", event.EventID).Error)
	require.Equal(t, model.AgentTerminalEffectsDone, completed.EffectsState)
	require.JSONEq(t, `{}`, string(completed.DelegateEvent))
	require.Nil(t, durableRecordFromTerminalLedger(&completed))

	manager.handleEventResult(conn, makePacket(t, protocol.CmdEventResult, 101, payload))
	requireDurablePacket(t, conn.send, protocol.CmdSendAck)
	require.Equal(t, int32(1), deliveryCount.Load())
	require.Equal(t, int32(1), outputCount.Load())

	conflict := payload
	conflict.Code = "different"
	manager.handleEventResult(conn, makePacket(t, protocol.CmdEventResult, 102, conflict))
	requireDurablePacket(t, conn.send, "error")
	foreign := &agentConn{
		agentID:      conn.agentID,
		ownerID:      conn.ownerID + 1,
		capabilities: append([]string(nil), conn.capabilities...),
		send:         make(chan []byte, 1),
	}
	manager.handleEventResult(foreign, makePacket(t, protocol.CmdEventResult, 103, payload))
	requireDurablePacket(t, foreign.send, "error")
}

func TestTerminalLedgerPersistsTurnKindsAndNotificationFence(t *testing.T) {
	installDurableLifecycleTestStores(t, true)
	manager := NewManager("", time.Second, nil, nil, nil, nil)
	defer manager.Shutdown()
	tests := []struct {
		name         string
		event        DelegateEventPayload
		callTurn     bool
		taskEligible bool
	}{
		{
			name:         "task",
			event:        durableLifecycleEvent("ledger-kind-task", 8101, 8201),
			taskEligible: true,
		},
		{
			name: "proxy",
			event: func() DelegateEventPayload {
				event := durableLifecycleEvent("ledger-kind-proxy", 8102, 8202)
				event.SenderID = event.OwnerID + 99
				return event
			}(),
		},
		{
			name:     "call",
			event:    durableLifecycleEvent("ledger-kind-call", 8103, 8203),
			callTurn: true,
		},
		{
			name: "record_only",
			event: func() DelegateEventPayload {
				event := durableLifecycleEvent("ledger-kind-record", 8104, 8204)
				event.MirrorMode = MirrorModeRecordOnly
				return event
			}(),
		},
		{
			name: "internal_protocol_event",
			event: func() DelegateEventPayload {
				event := durableLifecycleEvent("customer_coach:8105:ws_auth:1", 8105, 8205)
				event.EventType = "customer_coach_snapshot"
				event.MirrorMode = MirrorModeRecordAndProcess
				return event
			}(),
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			record := &durablePendingDelegateRecord{
				Event:     tc.event,
				Stage:     durablePendingDelegateStageResult,
				Attempt:   1,
				StartedAt: time.Now().UnixMilli(),
				CallTurn:  tc.callTurn,
			}
			payload := EventResultPayload{
				EventID: tc.event.EventID,
				Status:  protocol.AgentEventResultResponded,
			}
			ledger, err := manager.prepareTerminalResult(payload, record)
			require.NoError(t, err)
			require.Equal(t, tc.taskEligible, ledger.TaskEligible)
			require.Equal(t, tc.taskEligible, ledger.TaskNotificationAllowed)
			require.Equal(t, tc.callTurn, ledger.CallTurn)
			require.Equal(t, tc.event.IsRecordOnly(), ledger.RecordOnly)
			restored := durableRecordFromTerminalLedger(ledger)
			require.NotNil(t, restored)
			require.Equal(t, tc.callTurn, restored.CallTurn)
			require.Equal(t, tc.event.MirrorMode, restored.Event.MirrorMode)

			var count int64
			require.NoError(t, store.DB.Model(&model.SessionAgentState{}).
				Where("last_run_id = ?", tc.event.EventID).
				Count(&count).Error)
			if tc.taskEligible {
				require.Equal(t, int64(1), count)
			} else {
				require.Zero(t, count)
			}
		})
	}
}

func TestLegacySettledTombstoneImportsAsEffectsDone(t *testing.T) {
	installDurableLifecycleTestStores(t, true)
	event := durableLifecycleEvent("legacy-settled-import", 8301, 8401)
	require.True(t, persistDurablePendingDelegate(context.Background(), durablePendingDelegateRecord{
		Event:      event,
		Attempt:    2,
		Stage:      durablePendingDelegateStageResult,
		ReceivedAt: time.Now().UnixMilli(),
	}))
	payload := EventResultPayload{
		EventID: event.EventID,
		Status:  protocol.AgentEventResultFailed,
		Code:    "legacy_failure",
		Msg:     "already emitted by old node",
	}
	legacyClaim, err := claimDurableTerminalIntent(
		context.Background(),
		event.EventID,
		event.AgentID,
		event.OwnerID,
		payload,
		nil,
	)
	require.NoError(t, err)
	require.Equal(t, terminalIntentClaimed, legacyClaim.Disposition)
	settled, err := settleDurableTerminalIntent(
		context.Background(),
		legacyClaim.Record,
		legacyClaim.Token,
	)
	require.NoError(t, err)
	require.True(t, settled)

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
		agentID:      event.AgentID,
		ownerID:      event.OwnerID,
		capabilities: []string{"event_result_ack"},
		send:         make(chan []byte, 2),
	}
	manager.handleEventResult(conn, makePacket(t, protocol.CmdEventResult, 200, payload))
	requireDurablePacket(t, conn.send, protocol.CmdSendAck)
	require.Equal(t, int32(0), deliveryCount.Load())
	require.Equal(t, int32(0), outputCount.Load())

	var ledger model.AgentEventTerminalLedger
	require.NoError(t, store.DB.First(&ledger, "event_id = ?", event.EventID).Error)
	require.Equal(t, model.AgentTerminalEffectsDone, ledger.EffectsState)
	require.True(t, ledger.EffectsSuppressed)
	require.NotNil(t, ledger.RedisCommittedAt)
	require.JSONEq(t, `{}`, string(ledger.DelegateEvent))
}

func TestTerminalLedgerRecoversWhenRedisSettlementFails(t *testing.T) {
	installDurableLifecycleTestStores(t, true)
	event := durableLifecycleEvent("ledger-redis-failure", 8451, 8461)
	record := &durablePendingDelegateRecord{
		Event:      event,
		Attempt:    1,
		Stage:      durablePendingDelegateStageResult,
		ReceivedAt: time.Now().UnixMilli(),
	}
	require.True(t, persistDurablePendingDelegate(context.Background(), *record))
	payload := EventResultPayload{
		EventID: event.EventID,
		Status:  protocol.AgentEventResultFailed,
		Code:    "connector_failed",
		Msg:     "retry after Redis restart",
	}
	claim, err := claimDurableTerminalIntent(
		context.Background(),
		event.EventID,
		event.AgentID,
		event.OwnerID,
		payload,
		nil,
	)
	require.NoError(t, err)
	require.Equal(t, terminalIntentClaimed, claim.Disposition)
	preparer := NewManager("", time.Second, nil, nil, nil, nil)
	defer preparer.Shutdown()
	ledger, err := preparer.prepareTerminalResult(payload, claim.Record)
	require.NoError(t, err)
	require.Equal(t, model.AgentTerminalEffectsPending, ledger.EffectsState)
	require.Nil(t, ledger.RedisCommittedAt)

	require.NoError(t, store.RDB.Close())
	settled, settleErr := settleDurableTerminalIntent(
		context.Background(),
		claim.Record,
		claim.Token,
	)
	require.Error(t, settleErr)
	require.False(t, settled)
	var pending model.AgentEventTerminalLedger
	require.NoError(t, store.DB.First(&pending, "event_id = ?", event.EventID).Error)
	require.Equal(t, model.AgentTerminalEffectsPending, pending.EffectsState)
	require.Nil(t, pending.RedisCommittedAt)

	store.RDB = testutil.NewMockRedis()
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
		agentID:      event.AgentID,
		ownerID:      event.OwnerID,
		capabilities: []string{"event_result_ack"},
		send:         make(chan []byte, 2),
	}
	manager.handleEventResult(conn, makePacket(t, protocol.CmdEventResult, 250, payload))
	requireDurablePacket(t, conn.send, protocol.CmdSendAck)
	require.Equal(t, int32(1), deliveryCount.Load())
	require.Equal(t, int32(1), outputCount.Load())
	require.NoError(t, store.DB.First(&pending, "event_id = ?", event.EventID).Error)
	require.Equal(t, model.AgentTerminalEffectsDone, pending.EffectsState)
	require.NotNil(t, pending.RedisCommittedAt)
}

func TestTerminalEffectsStopsAfterLeaseLossAndCanRecover(t *testing.T) {
	installDurableLifecycleTestStores(t, true)
	event := durableLifecycleEvent("effects-lease-loss", 8651, 8661)
	record := &durablePendingDelegateRecord{
		Event:      event,
		Attempt:    1,
		Stage:      durablePendingDelegateStageResult,
		ReceivedAt: time.Now().UnixMilli(),
	}
	payload := EventResultPayload{
		EventID: event.EventID,
		Status:  protocol.AgentEventResultResponded,
	}
	first := NewManager("", time.Second, nil, nil, nil, nil)
	defer first.Shutdown()
	ledger, err := first.prepareTerminalResult(payload, record)
	require.NoError(t, err)
	committed, err := store.MarkAgentEventTerminalRedisCommitted(
		event.EventID,
		event.OwnerID,
		event.AgentID,
		payload.Status,
		payload.Code,
		payload.Msg,
	)
	require.NoError(t, err)
	require.True(t, committed)

	deliveryEntered := make(chan struct{})
	releaseDelivery := make(chan struct{})
	var firstOutput atomic.Int32
	var firstUpdatedAt atomic.Int64
	first.SetDeliveryStatusHandler(func(status protocol.AgentDeliveryStatusPayload) {
		firstUpdatedAt.Store(status.UpdatedAt)
		close(deliveryEntered)
		<-releaseDelivery
	})
	first.SetOutputStatusHandler(func(protocol.AgentOutputStatusPayload) {
		firstOutput.Add(1)
	})
	type finishResult struct {
		done bool
		err  error
	}
	firstDone := make(chan finishResult, 1)
	go func() {
		done, finishErr := first.finishTerminalEffects(nil, payload, ledger, record)
		firstDone <- finishResult{done: done, err: finishErr}
	}()
	select {
	case <-deliveryEntered:
	case <-time.After(time.Second):
		t.Fatal("first effects worker did not reach delivery effect")
	}
	require.NoError(t, store.DB.Exec(
		"UPDATE agent_event_terminal_effects SET claim_until = DATETIME(CURRENT_TIMESTAMP, '-1 second') WHERE event_id = ? AND effect = ?",
		event.EventID,
		model.AgentTerminalEffectDelivery,
	).Error)
	recovery := NewManager("", time.Second, nil, nil, nil, nil)
	defer recovery.Shutdown()
	var recoveredDelivery, recoveredOutput atomic.Int32
	var recoveredUpdatedAt atomic.Int64
	recovery.SetDeliveryStatusHandler(func(status protocol.AgentDeliveryStatusPayload) {
		recoveredDelivery.Add(1)
		recoveredUpdatedAt.Store(status.UpdatedAt)
	})
	recovery.SetOutputStatusHandler(func(protocol.AgentOutputStatusPayload) {
		recoveredOutput.Add(1)
	})
	done, err := recovery.finishTerminalEffects(nil, payload, ledger, record)
	require.NoError(t, err)
	require.True(t, done)
	require.Equal(t, int32(1), recoveredDelivery.Load())
	require.Equal(t, int32(1), recoveredOutput.Load())

	close(releaseDelivery)
	select {
	case result := <-firstDone:
		require.NoError(t, result.err)
		require.False(t, result.done, "stale claim cannot complete the outbox")
	case <-time.After(time.Second):
		t.Fatal("lost worker did not stop after lease loss")
	}
	require.Zero(t, firstOutput.Load(), "lost worker must not continue into output/notification effects")
	require.Equal(t, ledger.TerminalAt.UnixMilli(), recoveredUpdatedAt.Load())
	require.Equal(t, firstUpdatedAt.Load(), recoveredUpdatedAt.Load(), "reclaimed delivery is byte-order stable")

	var completed model.AgentEventTerminalLedger
	require.NoError(t, store.DB.First(&completed, "event_id = ?", event.EventID).Error)
	require.Equal(t, model.AgentTerminalEffectsDone, completed.EffectsState)
}

func TestStaleTerminalKeepsPerRunStatusButCannotReplaceOrNotifyNewRun(t *testing.T) {
	installDurableLifecycleTestStores(t, true)
	oldEvent := durableLifecycleEvent("stale-terminal-old", 8671, 8681)
	newRunID := "stale-terminal-new"
	newStartedAt := time.Now().UTC()
	store.UpsertSessionAgentStateRunning(
		oldEvent.SessionID,
		oldEvent.OwnerID,
		oldEvent.AgentID,
		newRunID,
		newStartedAt,
	)
	record := &durablePendingDelegateRecord{
		Event:      oldEvent,
		Attempt:    1,
		Stage:      durablePendingDelegateStageResult,
		StartedAt:  newStartedAt.Add(-time.Hour).UnixMilli(),
		ReceivedAt: time.Now().UnixMilli(),
	}
	payload := EventResultPayload{
		EventID: oldEvent.EventID,
		Status:  protocol.AgentEventResultResponded,
	}
	manager := NewManager("", time.Second, nil, nil, nil, nil)
	defer manager.Shutdown()
	manager.runsMu.Lock()
	manager.runs[newRunID] = &activeAgentRun{
		EventID:   newRunID,
		SessionID: oldEvent.SessionID,
		OwnerID:   oldEvent.OwnerID,
		AgentID:   oldEvent.AgentID,
		SenderID:  oldEvent.OwnerID,
		State:     protocol.AgentOutputStateStreaming,
		StartedAt: newStartedAt.UnixMilli(),
		UpdatedAt: newStartedAt.UnixMilli(),
	}
	manager.runBySX[activeRunSessionOwnerKey(oldEvent.SessionID, oldEvent.OwnerID)] = newRunID
	manager.runsMu.Unlock()

	ledger, err := manager.prepareTerminalResult(payload, record)
	require.NoError(t, err)
	require.True(t, ledger.TaskEligible)
	require.False(t, ledger.TaskNotificationAllowed)
	committed, err := store.MarkAgentEventTerminalRedisCommitted(
		oldEvent.EventID,
		oldEvent.OwnerID,
		oldEvent.AgentID,
		payload.Status,
		payload.Code,
		payload.Msg,
	)
	require.NoError(t, err)
	require.True(t, committed)
	var deliveryCount, outputCount atomic.Int32
	manager.SetDeliveryStatusHandler(func(status protocol.AgentDeliveryStatusPayload) {
		if status.EventID == oldEvent.EventID {
			deliveryCount.Add(1)
		}
	})
	manager.SetOutputStatusHandler(func(status protocol.AgentOutputStatusPayload) {
		if status.RunID == oldEvent.EventID {
			outputCount.Add(1)
		}
	})
	done, err := manager.finishTerminalEffects(nil, payload, ledger, record)
	require.NoError(t, err)
	require.True(t, done)
	require.Equal(t, int32(1), deliveryCount.Load())
	require.Equal(t, int32(1), outputCount.Load())

	var state model.SessionAgentState
	require.NoError(t, store.DB.First(
		&state,
		"session_id = ? AND owner_id = ?",
		oldEvent.SessionID,
		oldEvent.OwnerID,
	).Error)
	require.Equal(t, newRunID, state.LastRunID)
	require.Equal(t, model.SessionAgentStateRunning, state.State)
	current := manager.LookupActiveRunBySessionOwner(oldEvent.OwnerID, oldEvent.SessionID)
	require.NotNil(t, current)
	require.Equal(t, newRunID, current.EventID)
}

func TestTerminalFirstRunFenceBlocksDelayedOlderRunningWrite(t *testing.T) {
	installDurableLifecycleTestStores(t, true)
	event := durableLifecycleEvent("run-fence-old", 8701, 8801)
	completedAt := time.Now().UTC()
	changed, err := store.SettleSessionAgentStateByRun(model.SessionAgentState{
		SessionID:   event.SessionID,
		OwnerID:     event.OwnerID,
		AgentID:     event.AgentID,
		State:       model.SessionAgentStateCompleted,
		LastRunID:   "run-terminal-b",
		CompletedAt: &completedAt,
	})
	require.NoError(t, err)
	require.True(t, changed)

	store.UpsertSessionAgentStateRunning(
		event.SessionID,
		event.OwnerID,
		event.AgentID,
		"run-delayed-a",
		completedAt.Add(-time.Hour),
	)
	var state model.SessionAgentState
	require.NoError(t, store.DB.First(
		&state,
		"session_id = ? AND owner_id = ?",
		event.SessionID,
		event.OwnerID,
	).Error)
	require.Equal(t, "run-terminal-b", state.LastRunID)
	require.Equal(t, model.SessionAgentStateCompleted, state.State)

	store.UpsertSessionAgentStateRunning(
		event.SessionID,
		event.OwnerID,
		event.AgentID,
		"run-newer-c",
		completedAt.Add(time.Hour),
	)
	require.NoError(t, store.DB.First(
		&state,
		"session_id = ? AND owner_id = ?",
		event.SessionID,
		event.OwnerID,
	).Error)
	require.Equal(t, "run-newer-c", state.LastRunID)
	require.Equal(t, model.SessionAgentStateRunning, state.State)
}

func TestDelayedSameRunStartCannotRegressWaitingPhase(t *testing.T) {
	installDurableLifecycleTestStores(t, true)
	event := durableLifecycleEvent("run-fence-waiting", 8751, 8851)
	startedAt := time.Now().UTC()
	store.UpsertSessionAgentStateRunning(
		event.SessionID,
		event.OwnerID,
		event.AgentID,
		event.EventID,
		startedAt,
	)
	store.SetSessionAgentStateWaiting(
		event.SessionID,
		event.OwnerID,
		model.SessionAgentStateWaitingApproval,
	)
	store.UpsertSessionAgentStateRunning(
		event.SessionID,
		event.OwnerID,
		event.AgentID,
		event.EventID,
		startedAt,
	)

	var state model.SessionAgentState
	require.NoError(t, store.DB.First(
		&state,
		"session_id = ? AND owner_id = ?",
		event.SessionID,
		event.OwnerID,
	).Error)
	require.Equal(t, model.SessionAgentStateWaitingApproval, state.State)
	require.Equal(t, event.EventID, state.LastRunID)
}

func TestTerminalLedgerModelAcceptsCanonicalJSONSnapshot(t *testing.T) {
	installDurableLifecycleTestStores(t, true)
	row := model.AgentEventTerminalLedger{
		EventID:       "ledger-json-model",
		OwnerID:       9001,
		AgentID:       9002,
		Status:        protocol.AgentEventResultResponded,
		DelegateEvent: datatypes.JSON([]byte(`{"thread_id":"thread-json"}`)),
		EffectsState:  model.AgentTerminalEffectsPending,
	}
	require.NoError(t, store.DB.Create(&row).Error)
	var loaded model.AgentEventTerminalLedger
	require.NoError(t, store.DB.First(&loaded, "event_id = ?", row.EventID).Error)
	require.JSONEq(t, string(row.DelegateEvent), string(loaded.DelegateEvent))
}

func TestDispatchLedgerRecoversFirstResultAfterRedisExpiryForEveryTurnKind(t *testing.T) {
	tests := []struct {
		name     string
		mutate   func(*DelegateEventPayload)
		callTurn bool
	}{
		{name: "task"},
		{
			name: "proxy",
			mutate: func(event *DelegateEventPayload) {
				event.SenderID = event.OwnerID + 99
			},
		},
		{name: "call", callTurn: true},
		{
			name: "record_only",
			mutate: func(event *DelegateEventPayload) {
				event.MirrorMode = MirrorModeRecordOnly
			},
		},
	}
	for index, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			installDurableLifecycleTestStores(t, true)
			event := durableLifecycleEvent(
				fmt.Sprintf("db-seed-expiry-%s", tc.name),
				9100+int64(index),
				9200+int64(index),
			)
			event.ThreadID = "thread-" + tc.name
			event.Content = "full snapshot " + tc.name
			event.Extra = json.RawMessage(`{"source":"expiry-test"}`)
			if tc.mutate != nil {
				tc.mutate(&event)
			}
			registrar := NewManager("", time.Second, nil, nil, nil, nil)
			defer registrar.Shutdown()
			require.Equal(
				t,
				pendingEventRegistrationCreated,
				registrar.registerPendingEventAckWithMetadata(
					event,
					1,
					time.Now().UTC(),
					tc.callTurn,
				),
			)
			var seeded model.AgentEventTerminalLedger
			require.NoError(t, store.DB.First(&seeded, "event_id = ?", event.EventID).Error)
			require.Empty(t, seeded.Status)
			require.Equal(t, event.ThreadID, mustLedgerEvent(t, seeded).ThreadID)
			require.Equal(t, event.Content, mustLedgerEvent(t, seeded).Content)
			deleteDurablePendingDelegate(context.Background(), event.EventID, event.AgentID)

			recovered := NewManager("", time.Second, nil, nil, nil, nil)
			defer recovered.Shutdown()
			conn := &agentConn{
				agentID:      event.AgentID,
				ownerID:      event.OwnerID,
				capabilities: []string{"event_result_ack"},
				send:         make(chan []byte, 2),
			}
			payload := EventResultPayload{
				EventID: event.EventID,
				Status:  protocol.AgentEventResultResponded,
			}
			recovered.handleEventResult(
				conn,
				makePacket(t, protocol.CmdEventResult, int64(400+index), payload),
			)
			requireDurablePacket(t, conn.send, protocol.CmdSendAck)

			var ledger model.AgentEventTerminalLedger
			require.NoError(t, store.DB.First(&ledger, "event_id = ?", event.EventID).Error)
			require.Equal(t, protocol.AgentEventResultResponded, ledger.Status)
			require.Equal(t, tc.callTurn, ledger.CallTurn)
			require.Positive(t, ledger.DispatchGeneration)
		})
	}
}

func mustLedgerEvent(t *testing.T, ledger model.AgentEventTerminalLedger) DelegateEventPayload {
	t.Helper()
	var event DelegateEventPayload
	require.NoError(t, json.Unmarshal(ledger.DelegateEvent, &event))
	return event
}

func TestRollbackPendingAckCASPreservesNewerAckVersionAndSeed(t *testing.T) {
	installDurableLifecycleTestStores(t, true)
	event := durableLifecycleEvent("rollback-cas-newer", 9301, 9401)
	manager := NewManager("", time.Second, nil, nil, nil, nil)
	defer manager.Shutdown()
	require.Equal(
		t,
		pendingEventRegistrationCreated,
		manager.registerPendingEventAck(event, 1),
	)
	advanced, newer, err := advanceDurablePendingDelegateAck(
		context.Background(),
		event.EventID,
		event.AgentID,
		event.OwnerID,
		time.Now().UnixMilli(),
	)
	require.NoError(t, err)
	require.True(t, advanced)
	require.NotNil(t, newer)

	manager.rollbackPendingEventAck(event.EventID)

	durable, ok := loadDurablePendingDelegate(context.Background(), event.EventID)
	require.True(t, ok)
	require.Equal(t, durablePendingDelegateStageResult, durable.Stage)
	require.Equal(t, newer.Version, durable.Version)
	manager.acksMu.Lock()
	entry := manager.pending[event.EventID]
	manager.acksMu.Unlock()
	require.NotNil(t, entry)
	require.Equal(t, pendingEventStageResult, entry.stage)
	var ledger model.AgentEventTerminalLedger
	require.NoError(t, store.DB.First(&ledger, "event_id = ?", event.EventID).Error)
	require.Empty(t, ledger.Status)
	require.Equal(t, entry.dispatchGeneration, ledger.DispatchGeneration)
}

func TestCommittedLedgerRepairsOppositeRedisVerdict(t *testing.T) {
	installDurableLifecycleTestStores(t, true)
	event := durableLifecycleEvent("ledger-repairs-opposite-redis", 9501, 9601)
	record := &durablePendingDelegateRecord{
		Event:              event,
		Attempt:            1,
		Stage:              durablePendingDelegateStageResult,
		DispatchGeneration: 1,
	}
	payload := EventResultPayload{
		EventID: event.EventID,
		Status:  protocol.AgentEventResultResponded,
	}
	manager := NewManager("", time.Second, nil, nil, nil, nil)
	defer manager.Shutdown()
	ledger, err := manager.prepareTerminalResult(payload, record)
	require.NoError(t, err)
	require.NotNil(t, ledger)
	opposite := *record
	opposite.Stage = durablePendingDelegateStageSettled
	opposite.Terminal = &durableTerminalIntent{
		Status:    protocol.AgentEventResultFailed,
		Code:      "opposite",
		Msg:       "stale redis verdict",
		SettledAt: time.Now().UnixMilli(),
	}
	forceDurableLifecycleRecord(t, &opposite)

	conn := &agentConn{
		agentID:      event.AgentID,
		ownerID:      event.OwnerID,
		capabilities: []string{"event_result_ack"},
		send:         make(chan []byte, 2),
	}
	manager.handleEventResult(conn, makePacket(t, protocol.CmdEventResult, 501, payload))
	requireDurablePacket(t, conn.send, protocol.CmdSendAck)
	repaired, ok := loadDurablePendingDelegate(context.Background(), event.EventID)
	require.True(t, ok)
	require.Equal(t, durablePendingDelegateStageSettled, repaired.Stage)
	require.NotNil(t, repaired.Terminal)
	require.Equal(t, payload.Status, repaired.Terminal.Status)
	require.Empty(t, repaired.Terminal.Code)
	require.Empty(t, repaired.Terminal.Msg)
}

func TestTerminalGateRejectsOldRunWhileNewRunIsActive(t *testing.T) {
	withoutDurableStores(t)
	manager := NewManager("", time.Second, nil, nil, nil, nil)
	defer manager.Shutdown()
	manager.runsMu.Lock()
	manager.runs["new-run"] = &activeAgentRun{
		EventID:   "new-run",
		SessionID: "same-session",
		OwnerID:   9701,
		AgentID:   9801,
		State:     protocol.AgentOutputStateStreaming,
	}
	manager.runsMu.Unlock()
	require.False(t, manager.ShouldClearComposingForTerminal(
		"old-run", "same-session", 9701, 9801,
	))
	require.True(t, manager.ShouldClearComposingForTerminal(
		"old-run", "other-session", 9701, 9801,
	))
}

func TestTerminalGateSeesNewRunFromAnotherNodeViaDispatchLedger(t *testing.T) {
	installDurableLifecycleTestStores(t, true)
	oldEvent := durableLifecycleEvent("cross-node-old-run", 9811, 9821)
	oldEvent.SessionID = "cross-node-shared-session"
	oldSeed, err := dispatchLedgerEntry(oldEvent, 1, time.Now().UnixMilli(), false, 1)
	require.NoError(t, err)
	_, _, _, err = store.SeedAgentEventDispatchLedger(oldSeed)
	require.NoError(t, err)
	_, err = (&Manager{}).prepareTerminalResult(
		EventResultPayload{
			EventID: oldEvent.EventID,
			Status:  protocol.AgentEventResultResponded,
		},
		&durablePendingDelegateRecord{
			Event:              oldEvent,
			Stage:              durablePendingDelegateStageResult,
			DispatchGeneration: 1,
		},
	)
	require.NoError(t, err)

	newEvent := oldEvent
	newEvent.EventID = "cross-node-new-proxy-run"
	newEvent.SenderID = newEvent.OwnerID + 1 // proxy: no chat_states row
	newSeed, err := dispatchLedgerEntry(newEvent, 1, time.Now().UnixMilli(), false, 2)
	require.NoError(t, err)
	_, _, created, err := store.SeedAgentEventDispatchLedger(newSeed)
	require.NoError(t, err)
	require.True(t, created)
	// Prove this is the DB fence, not a same-process map or Redis record.
	deleteDurablePendingDelegate(context.Background(), newEvent.EventID, newEvent.AgentID)

	nodeA := NewManager("", time.Second, nil, nil, nil, nil)
	defer nodeA.Shutdown()
	require.False(t, nodeA.ShouldClearComposingForTerminal(
		oldEvent.EventID,
		oldEvent.SessionID,
		oldEvent.OwnerID,
		oldEvent.AgentID,
	))
}

func TestNotificationReceiptPermanentlyDeduplicatesPerChannel(t *testing.T) {
	installDurableLifecycleTestStores(t, true)
	first, err := store.ClaimAgentNotificationReceipt("terminal:event-1", "push")
	require.NoError(t, err)
	require.True(t, first)
	second, err := store.ClaimAgentNotificationReceipt("terminal:event-1", "push")
	require.NoError(t, err)
	require.False(t, second)
	tts, err := store.ClaimAgentNotificationReceipt("terminal:event-1", "tts")
	require.NoError(t, err)
	require.True(t, tts)
}
