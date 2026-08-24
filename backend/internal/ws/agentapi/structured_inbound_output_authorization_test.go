package agentapi

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/testutil"
	"github.com/askie/grix/backend/internal/store"
	"github.com/stretchr/testify/require"
)

func installStructuredOutputTestStores(t *testing.T) {
	t.Helper()
	previousDB, previousRDB := store.DB, store.RDB
	testDB := testutil.NewTestDB()
	store.DB = testDB.DB
	store.RDB = testutil.NewMockRedis()
	t.Cleanup(func() {
		_ = store.RDB.Close()
		testDB.Close()
		store.DB, store.RDB = previousDB, previousRDB
	})
}

func TestRecordOnlyInternalFenceSurvivesAckAndManagerRestart(t *testing.T) {
	installStructuredOutputTestStores(t)
	event := DelegateEventPayload{
		EventID:    "customer_coach:restart-fence",
		EventType:  "customer_coach_snapshot",
		MirrorMode: MirrorModeRecordOnly,
		AgentID:    7101,
		OwnerID:    7201,
		SessionID:  "sess-record-only-restart-fence",
	}

	dispatcher := NewManager("", time.Second, nil, nil, nil, nil)
	require.Equal(t, pendingEventRegistrationCreated, dispatcher.registerPendingEventAck(event, 1))
	dispatcher.resolvePendingEventAck(event.EventID, time.Now().UnixMilli())
	dispatcher.acksMu.Lock()
	require.Nil(t, dispatcher.pending[event.EventID])
	dispatcher.acksMu.Unlock()
	_, durableExists := loadDurablePendingDelegate(context.Background(), event.EventID)
	require.False(t, durableExists)
	dispatcher.Shutdown()

	streamHandler := &mockStreamChunkHandler{}
	restarted := NewManager("", time.Second, nil, streamHandler.handle, nil, nil)
	defer restarted.Shutdown()
	conn := &agentConn{
		agentID: event.AgentID,
		ownerID: event.OwnerID,
		send:    make(chan []byte, 2),
	}
	_, guardErr := restarted.authorizeInboundOutput(
		context.Background(), conn, "", event.SessionID,
	)
	require.NotNil(t, guardErr)
	require.Equal(t, 4003, guardErr.Code)

	restarted.handleCodexEvent(conn, makePacket(t, "codex_event", 1, CodexEventPayload{
		SessionID:    event.SessionID,
		CodexMethod:  "item/agentMessage/delta",
		CodexPayload: json.RawMessage(`{"params":{"delta":"must not leak"}}`),
	}))
	restarted.handlePiEvent(conn, makePacket(t, "pi_event", 2, PiEventPayload{
		SessionID:   event.SessionID,
		PiEventType: "message_update",
		PiPayload:   json.RawMessage(`{"assistantMessageEvent":{"type":"text_delta","delta":"must not leak"}}`),
	}))
	require.Empty(t, streamHandler.calls)
	require.Len(t, conn.send, 2)
}

func seedExpiredOutputLedger(t *testing.T, event DelegateEventPayload, generation int64) {
	t.Helper()
	old := time.Now().Add(-2 * time.Hour).UTC()
	entry, err := dispatchLedgerEntry(event, 1, old.UnixMilli(), false, generation)
	require.NoError(t, err)
	entry.CreatedAt = old
	entry.UpdatedAt = old
	entry.StartedAt = &old
	require.NoError(t, store.DB.Create(&entry).Error)
}

func seedAgentSessionMembership(t *testing.T, sessionID string, agentID int64) {
	t.Helper()
	now := time.Now().UTC()
	require.NoError(t, store.DB.Create(&model.SessionMember{
		SessionID:    sessionID,
		MemberID:     agentID,
		MemberType:   2,
		JoinedAt:     now,
		LastActiveAt: now,
	}).Error)
}

func TestExpiredCodexFallbackStreamsKeepDistinctLocalIdentity(t *testing.T) {
	installStructuredOutputTestStores(t)
	const (
		agentID   = int64(7301)
		ownerID   = int64(7401)
		sessionID = "sess-expired-codex-streams"
	)
	seedAgentSessionMembership(t, sessionID, agentID)
	for index, eventID := range []string{"evt-expired-codex-a", "evt-expired-codex-b"} {
		seedExpiredOutputLedger(t, DelegateEventPayload{
			EventID: eventID, AgentID: agentID, OwnerID: ownerID, SessionID: sessionID,
		}, int64(index+1))
	}

	handler := &mockStreamChunkHandler{}
	manager := NewManager("", time.Second, nil, handler.handle, nil, nil)
	defer manager.Shutdown()
	manager.pendingTrackingTTL = time.Hour
	conn := &agentConn{agentID: agentID, ownerID: ownerID, send: make(chan []byte, 4)}

	for seq, eventID := range []string{"evt-expired-codex-a", "evt-expired-codex-b"} {
		manager.handleCodexEvent(conn, makePacket(t, "codex_event", int64(seq+1), CodexEventPayload{
			EventID: eventID, SessionID: sessionID,
			CodexMethod:  "item/agentMessage/delta",
			CodexPayload: json.RawMessage(`{"params":{"delta":"chunk"}}`),
		}))
	}

	require.Len(t, handler.calls, 2)
	require.Empty(t, handler.calls[0].EventID)
	require.Empty(t, handler.calls[1].EventID)
	require.NotEqual(t, handler.calls[0].ClientMsgID, handler.calls[1].ClientMsgID)
	require.Equal(t, int64(1), handler.calls[0].ChunkSeq)
	require.Equal(t, int64(1), handler.calls[1].ChunkSeq)
	for _, eventID := range []string{"evt-expired-codex-a", "evt-expired-codex-b"} {
		ledger, err := store.LoadAgentEventTerminalLedger(eventID)
		require.NoError(t, err)
		require.Nil(t, ledger, "expired dispatch seed must be retired instead of re-authorized")
	}
}

func TestExpiredPiFallbackStreamsKeepDistinctLocalIdentity(t *testing.T) {
	installStructuredOutputTestStores(t)
	const (
		agentID   = int64(7501)
		ownerID   = int64(7601)
		sessionID = "sess-expired-pi-streams"
	)
	seedAgentSessionMembership(t, sessionID, agentID)
	for index, eventID := range []string{"evt-expired-pi-a", "evt-expired-pi-b"} {
		seedExpiredOutputLedger(t, DelegateEventPayload{
			EventID: eventID, AgentID: agentID, OwnerID: ownerID, SessionID: sessionID,
		}, int64(index+1))
	}

	handler := &mockStreamChunkHandler{}
	manager := NewManager("", time.Second, nil, handler.handle, nil, nil)
	defer manager.Shutdown()
	manager.pendingTrackingTTL = time.Hour
	conn := &agentConn{agentID: agentID, ownerID: ownerID, send: make(chan []byte, 4)}

	for seq, eventID := range []string{"evt-expired-pi-a", "evt-expired-pi-b"} {
		manager.handlePiEvent(conn, makePacket(t, "pi_event", int64(seq+1), PiEventPayload{
			EventID: eventID, SessionID: sessionID, PiEventType: "message_update",
			PiPayload: json.RawMessage(`{"assistantMessageEvent":{"type":"text_delta","delta":"chunk"}}`),
		}))
	}

	require.Len(t, handler.calls, 2)
	require.Empty(t, handler.calls[0].EventID)
	require.Empty(t, handler.calls[1].EventID)
	require.NotEqual(t, handler.calls[0].ClientMsgID, handler.calls[1].ClientMsgID)
	require.Equal(t, int64(1), handler.calls[0].ChunkSeq)
	require.Equal(t, int64(1), handler.calls[1].ChunkSeq)
}

func seedRecordOnlyOutputLedgerAged(
	t *testing.T,
	event DelegateEventPayload,
	generation int64,
	age time.Duration,
) {
	t.Helper()
	anchor := time.Now().Add(-age).UTC()
	entry, err := dispatchLedgerEntry(event, 1, anchor.UnixMilli(), false, generation)
	require.NoError(t, err)
	entry.CreatedAt = anchor
	entry.UpdatedAt = anchor
	entry.StartedAt = &anchor
	require.NoError(t, store.DB.Create(&entry).Error)
}

// 围栏只应在短窗口内阻塞主动输出。pendingTrackingRetention 是 ledger 的保留期
// （生产默认 48h），复用它会让一条卡住的内部事件把会话主动消息挡两天。
func TestStructuredInternalFenceStopsBlockingAfterFenceWindow(t *testing.T) {
	installStructuredOutputTestStores(t)
	manager := NewManager("", time.Hour, nil, nil, nil, nil)
	defer manager.Shutdown()
	require.Equal(t, structuredInternalOutputFenceWindow, manager.structuredInternalFenceWindow())

	fresh := DelegateEventPayload{
		EventID:    "customer_coach:fence-fresh",
		EventType:  "customer_coach_snapshot",
		MirrorMode: MirrorModeRecordOnly,
		AgentID:    7301,
		OwnerID:    7401,
		SessionID:  "sess-fence-fresh",
	}
	seedRecordOnlyOutputLedgerAged(t, fresh, 1, time.Minute)
	freshConn := &agentConn{agentID: fresh.AgentID, ownerID: fresh.OwnerID, send: make(chan []byte, 1)}
	_, guardErr := manager.authorizeInboundOutput(context.Background(), freshConn, "", fresh.SessionID)
	require.NotNil(t, guardErr)
	require.Equal(t, 4003, guardErr.Code)

	stale := DelegateEventPayload{
		EventID:    "customer_coach:fence-stale",
		EventType:  "customer_coach_snapshot",
		MirrorMode: MirrorModeRecordOnly,
		AgentID:    7302,
		OwnerID:    7402,
		SessionID:  "sess-fence-stale",
	}
	seedRecordOnlyOutputLedgerAged(t, stale, 1, structuredInternalOutputFenceWindow+time.Minute)
	staleConn := &agentConn{agentID: stale.AgentID, ownerID: stale.OwnerID, send: make(chan []byte, 1)}
	_, staleErr := manager.authorizeInboundOutput(context.Background(), staleConn, "", stale.SessionID)
	require.Nil(t, staleErr)

	// ledger 未被围栏顺带删除：回收仍由既有过期逻辑负责。
	remaining, err := store.ListPendingRecordOnlyAgentEventDispatches(
		stale.SessionID, stale.OwnerID, stale.AgentID,
	)
	require.NoError(t, err)
	require.Len(t, remaining, 1)
}

// 强时效的后端主动事件不得落离线队列，否则会在数小时后被重放。
func TestDispatchDelegateEventWithoutQueueSkipsOfflineQueue(t *testing.T) {
	installStructuredOutputTestStores(t)
	manager := NewManager("", time.Second, nil, nil, nil, nil)
	defer manager.Shutdown()

	base := DelegateEventPayload{
		EventType:  "customer_coach_snapshot",
		MirrorMode: MirrorModeRecordOnly,
		AgentID:    7501,
		OwnerID:    7601,
		SessionID:  "sess-no-queue",
		MsgType:    1,
		Content:    "coach context",
		CreatedAt:  time.Now().UnixMilli(),
	}

	queued := base
	queued.EventID = "customer_coach:queued"
	require.True(t, manager.PushDelegateEvent(queued))

	direct := base
	direct.EventID = "customer_coach:direct"
	require.False(t, manager.DispatchDelegateEventWithoutQueue(direct))

	previous := GetGlobalManager()
	SetGlobal(nil)
	defer SetGlobal(previous)
	require.False(t, DispatchDelegateEventWithContext(context.Background(), direct))
}
