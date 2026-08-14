package agentapi

import (
	"sync"
	"testing"
	"time"

	"github.com/askie/grix/backend/internal/pkg/testutil"
	"github.com/askie/grix/backend/internal/store"
	"github.com/askie/grix/backend/internal/ws/protocol"
	"github.com/stretchr/testify/require"
)

func TestExpiredPendingTrackingRetiresMetadataWithoutTerminalizingOrBlockingLateOutput(t *testing.T) {
	previousDB, previousRDB := store.DB, store.RDB
	testDB := testutil.NewTestDB()
	store.DB = testDB.DB
	store.RDB = testutil.NewMockRedis()
	t.Cleanup(func() {
		_ = store.RDB.Close()
		testDB.Close()
		store.DB, store.RDB = previousDB, previousRDB
	})

	const (
		agentID   = int64(9101)
		ownerID   = int64(9201)
		peerID    = int64(9301)
		sessionID = "sess-expired-tracking"
		eventID   = "evt-expired-tracking"
	)
	seedSessionWithAgentMember(t, sessionID, ownerID, peerID, agentID)

	sendHandler := &mockSendMessageHandler{
		result: &SendMessageResult{MsgID: 9401, CreatedAt: time.Now().UnixMilli()},
	}
	streamHandler := &mockStreamChunkHandler{}
	mgr := NewManager("", time.Second, sendHandler.handle, streamHandler.handle, nil, nil)
	defer mgr.Shutdown()
	mgr.eventResultWait = 10 * time.Millisecond
	mgr.pendingTrackingTTL = 60 * time.Millisecond

	var statusesMu sync.Mutex
	var statuses []protocol.AgentDeliveryStatusPayload
	mgr.SetDeliveryStatusHandler(func(payload protocol.AgentDeliveryStatusPayload) {
		statusesMu.Lock()
		statuses = append(statuses, payload)
		statusesMu.Unlock()
	})

	event := DelegateEventPayload{
		EventID: eventID, EventType: "user_chat", AgentID: agentID,
		OwnerID: ownerID, SessionID: sessionID, MsgID: 9501, SenderID: ownerID,
	}
	mgr.registerActiveRun(event)
	mgr.registerPendingEventAck(event, 1)
	mgr.resolvePendingEventAck(eventID, time.Now().UnixMilli())
	mgr.streamChunkTrackers.observe(eventID, 1)
	mgr.piThinkingBuf.Store(eventID+"_thinking", true)
	mgr.piChunkSeq.Store(eventID+"_thinking", new(int64))

	require.Eventually(t, func() bool {
		mgr.acksMu.Lock()
		_, pending := mgr.pending[eventID]
		mgr.acksMu.Unlock()
		return !pending && mgr.LookupActiveRun(eventID) == nil
	}, time.Second, 10*time.Millisecond, "expired metadata should be retired")

	if _, tracked := mgr.streamChunkTrackers.m.Load(eventID); tracked {
		t.Fatal("expired stream chunk tracker was not released")
	}
	if _, tracked := mgr.piThinkingBuf.Load(eventID + "_thinking"); tracked {
		t.Fatal("expired Pi thinking buffer was not released")
	}
	if _, tracked := mgr.piChunkSeq.Load(eventID + "_thinking"); tracked {
		t.Fatal("expired Pi thinking sequence was not released")
	}
	statusesMu.Lock()
	for _, status := range statuses {
		require.NotEqual(t, protocol.AgentDeliveryStatusFailed, status.Status)
		require.NotEqual(t, protocol.AgentDeliveryStatusCanceled, status.Status)
		require.NotEqual(t, protocol.AgentDeliveryStatusTimeout, status.Status)
	}
	statusesMu.Unlock()

	conn := &agentConn{
		agentID: agentID, ownerID: ownerID,
		send: make(chan []byte, 4),
	}
	mgr.handleClientStreamChunk(conn, makePacket(t, protocol.CmdClientStreamChunk, 1, AgentStreamChunkPayload{
		EventID: eventID, SessionID: sessionID,
		ClientMsgID: "late-stream", ChunkSeq: 2, DeltaContent: "late but valid",
	}))
	require.Len(t, streamHandler.calls, 1, "late stream must use authorized session fallback")
	require.Empty(t, streamHandler.calls[0].EventID, "expired tracking id must not be trusted downstream")

	mgr.handleClientStreamChunk(conn, makePacket(t, protocol.CmdClientStreamChunk, 2, AgentStreamChunkPayload{
		EventID: eventID, SessionID: sessionID,
		ChunkSeq: 3, DeltaContent: "late stream without a client id",
	}))
	require.Len(t, streamHandler.calls, 2)
	require.Empty(t, streamHandler.calls[1].EventID, "expired tracking id must stay stripped downstream")
	require.Equal(
		t,
		"agentapi_stream_"+eventID,
		streamHandler.calls[1].ClientMsgID,
		"late chunks from the same original event need a stable fallback stream id",
	)

	mgr.handleSendMsg(conn, makePacket(t, protocol.CmdSendMsg, 3, SendMsgPayload{
		EventID: eventID, SessionID: sessionID,
		ClientMsgID: "late-send", Content: "late final details",
	}))
	require.Len(t, sendHandler.calls, 1, "late send_msg must use authorized session fallback")
}

func TestResolvePendingEventAckCapsFutureReceivedAtForTrackingDeadline(t *testing.T) {
	mgr := NewManager("", time.Second, nil, nil, nil, nil)
	defer mgr.Shutdown()
	mgr.pendingTrackingTTL = time.Hour

	event := DelegateEventPayload{
		EventID: "evt-future-received-at", AgentID: 101, OwnerID: 201,
		SessionID: "sess-future-received-at", MsgID: 301,
	}
	mgr.registerPendingEventAck(event, 1)
	beforeAck := time.Now()
	mgr.resolvePendingEventAck(event.EventID, beforeAck.Add(365*24*time.Hour).UnixMilli())

	mgr.acksMu.Lock()
	entry := mgr.pending[event.EventID]
	receivedAt := entry.status.ReceivedAt
	updatedAt := entry.status.UpdatedAt
	trackingExpireAt := entry.trackingExpireAt
	mgr.acksMu.Unlock()

	require.LessOrEqual(t, receivedAt, time.Now().UnixMilli(), "future connector timestamp must be capped")
	require.GreaterOrEqual(t, updatedAt, beforeAck.UnixMilli(), "status ordering must use the server observation clock")
	require.LessOrEqual(t, updatedAt, time.Now().UnixMilli())
	require.GreaterOrEqual(t, trackingExpireAt, beforeAck.Add(time.Hour).Add(-time.Second).UnixMilli())
	require.LessOrEqual(t, trackingExpireAt, time.Now().Add(time.Hour).UnixMilli())
}
