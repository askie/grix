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
