package agentapi

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/testutil"
	"github.com/askie/grix/backend/internal/store"
	"github.com/askie/grix/backend/internal/ws/protocol"
	"github.com/stretchr/testify/require"
)

func seedInboundOutputLedger(
	t *testing.T,
	event DelegateEventPayload,
	generation int64,
	status string,
) {
	t.Helper()
	entry, err := dispatchLedgerEntry(event, 1, time.Now().UnixMilli(), false, generation)
	require.NoError(t, err)
	entry.Status = status
	entry.EffectsState = model.AgentTerminalEffectsDone
	require.NoError(t, store.DB.Create(&entry).Error)
}

func TestTerminalLedgerAbsorbsLateSendAndStreamOutput(t *testing.T) {
	previousDB, previousRDB := store.DB, store.RDB
	testDB := testutil.NewTestDB()
	store.DB = testDB.DB
	store.RDB = testutil.NewMockRedis()
	t.Cleanup(func() {
		_ = store.RDB.Close()
		testDB.Close()
		store.DB, store.RDB = previousDB, previousRDB
	})

	event := DelegateEventPayload{
		EventID:   "evt-terminal-output-fence",
		AgentID:   6101,
		OwnerID:   6201,
		SessionID: "sess-terminal-output-fence",
		SenderID:  6201,
		MsgID:     6301,
	}
	seedInboundOutputLedger(t, event, 1, protocol.AgentEventResultResponded)

	sendHandler := &mockSendMessageHandler{
		result: &SendMessageResult{MsgID: 6401, CreatedAt: time.Now().UnixMilli()},
	}
	streamHandler := &mockStreamChunkHandler{}
	manager := NewManager("", time.Second, sendHandler.handle, streamHandler.handle, nil, nil)
	defer manager.Shutdown()
	conn := &agentConn{
		agentID: event.AgentID,
		ownerID: event.OwnerID,
		send:    make(chan []byte, 4),
	}

	manager.handleSendMsg(conn, makePacket(t, protocol.CmdSendMsg, 1, SendMsgPayload{
		EventID: event.EventID, SessionID: event.SessionID,
		ClientMsgID: "late-terminal-send", Content: "must be dropped",
	}))
	manager.handleClientStreamChunk(conn, makePacket(t, protocol.CmdClientStreamChunk, 2, AgentStreamChunkPayload{
		EventID: event.EventID, SessionID: event.SessionID,
		ClientMsgID: "late-terminal-stream", ChunkSeq: 1, DeltaContent: "must be dropped",
	}))

	require.Empty(t, sendHandler.calls, "terminal late send_msg must not reach sink")
	require.Empty(t, streamHandler.calls, "terminal late stream must not reach sink")
	require.Len(t, conn.send, 2, "both absorbed packets must be acknowledged")
	for index := 0; index < 2; index++ {
		var packet protocol.Packet
		require.NoError(t, json.Unmarshal(<-conn.send, &packet))
		require.Equal(t, protocol.CmdSendAck, packet.Cmd)
	}
	require.Equal(t, int32(0), conn.violations.Load(), "absorbed terminal output is not a violation")
}

func TestPendingLedgerRecoversOutputSnapshotBeforeSessionFallback(t *testing.T) {
	previousDB, previousRDB := store.DB, store.RDB
	testDB := testutil.NewTestDB()
	store.DB = testDB.DB
	store.RDB = testutil.NewMockRedis()
	t.Cleanup(func() {
		_ = store.RDB.Close()
		testDB.Close()
		store.DB, store.RDB = previousDB, previousRDB
	})

	event := DelegateEventPayload{
		EventID:   "evt-pending-output-recovery",
		AgentID:   6111,
		OwnerID:   6211,
		SessionID: "sess-pending-output-recovery",
		SenderID:  6211,
		MsgID:     6311,
	}
	seedInboundOutputLedger(t, event, 7, "")

	sendHandler := &mockSendMessageHandler{
		result: &SendMessageResult{MsgID: 6411, CreatedAt: time.Now().UnixMilli()},
	}
	manager := NewManager("", time.Second, sendHandler.handle, nil, nil, nil)
	defer manager.Shutdown()
	conn := &agentConn{
		agentID: event.AgentID,
		ownerID: event.OwnerID,
		send:    make(chan []byte, 2),
	}
	manager.handleSendMsg(conn, makePacket(t, protocol.CmdSendMsg, 1, SendMsgPayload{
		EventID: event.EventID, SessionID: event.SessionID,
		ClientMsgID: "pending-recovered-send", Content: "valid in-flight output",
	}))

	require.Len(t, sendHandler.calls, 1)
	require.Equal(t, event.EventID, sendHandler.calls[0].EventID)
	record, ok := loadDurablePendingDelegate(context.Background(), event.EventID)
	require.True(t, ok)
	require.Equal(t, int64(7), record.DispatchGeneration)
	manager.acksMu.Lock()
	pending := manager.pending[event.EventID]
	manager.acksMu.Unlock()
	require.NotNil(t, pending)
	require.Equal(t, int64(7), pending.dispatchGeneration)
}

func TestForeignTerminalLedgerOutputIsRejected(t *testing.T) {
	previousDB, previousRDB := store.DB, store.RDB
	testDB := testutil.NewTestDB()
	store.DB = testDB.DB
	store.RDB = testutil.NewMockRedis()
	t.Cleanup(func() {
		_ = store.RDB.Close()
		testDB.Close()
		store.DB, store.RDB = previousDB, previousRDB
	})

	event := DelegateEventPayload{
		EventID:   "evt-foreign-terminal-output",
		AgentID:   6121,
		OwnerID:   6221,
		SessionID: "sess-foreign-terminal-output",
		SenderID:  6221,
		MsgID:     6321,
	}
	seedInboundOutputLedger(t, event, 1, protocol.AgentEventResultResponded)

	sendHandler := &mockSendMessageHandler{
		result: &SendMessageResult{MsgID: 6421, CreatedAt: time.Now().UnixMilli()},
	}
	manager := NewManager("", time.Second, sendHandler.handle, nil, nil, nil)
	defer manager.Shutdown()
	conn := &agentConn{
		agentID: event.AgentID,
		ownerID: event.OwnerID + 1,
		send:    make(chan []byte, 2),
	}
	manager.handleSendMsg(conn, makePacket(t, protocol.CmdSendMsg, 1, SendMsgPayload{
		EventID: event.EventID, SessionID: event.SessionID,
		ClientMsgID: "foreign-terminal-send", Content: "must be rejected",
	}))

	require.Empty(t, sendHandler.calls)
	require.Equal(t, int32(1), conn.violations.Load())
	var packet protocol.Packet
	require.NoError(t, json.Unmarshal(<-conn.send, &packet))
	require.Equal(t, protocol.CmdSendNack, packet.Cmd)
}
