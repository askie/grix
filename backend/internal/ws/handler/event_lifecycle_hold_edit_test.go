package handler

import (
	"encoding/json"
	"testing"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/testutil"
	"github.com/askie/grix/backend/internal/store"
	"github.com/askie/grix/backend/internal/ws/protocol"
	"github.com/stretchr/testify/require"
)

func makeEventHoldPacket(t *testing.T, sessionID, eventID string, hold bool) *protocol.Packet {
	t.Helper()
	raw, err := json.Marshal(protocol.EventHoldPayload{SessionID: sessionID, EventID: eventID, Hold: hold, Reason: "manual"})
	require.NoError(t, err)
	return &protocol.Packet{Cmd: protocol.CmdEventHold, Seq: 1, Payload: raw}
}

func makeQueueEditPacket(t *testing.T, sessionID, eventID, content string) *protocol.Packet {
	t.Helper()
	raw, err := json.Marshal(protocol.QueueEditPayload{SessionID: sessionID, EventID: eventID, Content: content})
	require.NoError(t, err)
	return &protocol.Packet{Cmd: protocol.CmdQueueEdit, Seq: 1, Payload: raw}
}

func lastResultOf(t *testing.T, conn *reorderMockConn, wantCmd string) map[string]any {
	t.Helper()
	require.NotEmpty(t, conn.sent)
	last := conn.sent[len(conn.sent)-1]
	require.Equal(t, wantCmd, last.cmd)
	return last.payload
}

func TestHandleEventHold(t *testing.T) {
	tdb := testutil.NewTestDB()
	defer tdb.Close()
	store.DB = tdb.DB
	origRDB := store.RDB
	store.RDB = nil
	defer func() { store.RDB = origRDB }()

	const userID int64 = 1101
	const sessionID = "sess-hold-1"
	require.NoError(t, store.DB.Create(&model.Session{SessionID: sessionID, OwnerID: userID}).Error)
	require.NoError(t, store.DB.Create(&model.SessionMember{SessionID: sessionID, MemberID: userID, MemberType: 1}).Error)

	t.Run("invalid payload: 空 session", func(t *testing.T) {
		conn := &reorderMockConn{userID: userID}
		HandleEventHold(nil, conn, makeEventHoldPacket(t, "", "e1", true))
		result := lastResultOf(t, conn, protocol.CmdEventHoldResult)
		require.Equal(t, false, result["ok"])
		require.Equal(t, false, result["held"])
		require.Equal(t, "bad_request", result["error"])
	})

	t.Run("invalid payload: 空 event_id", func(t *testing.T) {
		conn := &reorderMockConn{userID: userID}
		HandleEventHold(nil, conn, makeEventHoldPacket(t, sessionID, "  ", true))
		result := lastResultOf(t, conn, protocol.CmdEventHoldResult)
		require.Equal(t, false, result["ok"])
		require.Equal(t, "bad_request", result["error"])
	})

	t.Run("权限拒绝: 会话不存在", func(t *testing.T) {
		conn := &reorderMockConn{userID: userID}
		HandleEventHold(nil, conn, makeEventHoldPacket(t, "sess-not-exist", "e1", true))
		result := lastResultOf(t, conn, protocol.CmdEventHoldResult)
		require.Equal(t, false, result["ok"])
		require.NotEmpty(t, result["error"])
		require.NotEqual(t, "bad_request", result["error"])
	})

	t.Run("权限拒绝: 非会话成员", func(t *testing.T) {
		conn := &reorderMockConn{userID: 2202}
		HandleEventHold(nil, conn, makeEventHoldPacket(t, sessionID, "e1", true))
		result := lastResultOf(t, conn, protocol.CmdEventHoldResult)
		require.Equal(t, false, result["ok"])
		require.NotEmpty(t, result["error"])
	})

	t.Run("无可路由 agent", func(t *testing.T) {
		conn := &reorderMockConn{userID: userID}
		HandleEventHold(nil, conn, makeEventHoldPacket(t, sessionID, "e1", true))
		result := lastResultOf(t, conn, protocol.CmdEventHoldResult)
		require.Equal(t, false, result["ok"])
		require.Equal(t, "delegate agent not found", result["error"])
	})

	t.Run("有 agent 成员但通道不可用", func(t *testing.T) {
		require.NoError(t, store.DB.Create(&model.SessionMember{SessionID: sessionID, MemberID: 3303, MemberType: 2}).Error)
		defer store.DB.Where("session_id = ? AND member_type = 2", sessionID).Delete(&model.SessionMember{})
		conn := &reorderMockConn{userID: userID}
		HandleEventHold(nil, conn, makeEventHoldPacket(t, sessionID, "e1", false))
		result := lastResultOf(t, conn, protocol.CmdEventHoldResult)
		require.Equal(t, false, result["ok"])
		require.Equal(t, "agent channel unavailable", result["error"])
	})
}

func TestHandleQueueEdit(t *testing.T) {
	tdb := testutil.NewTestDB()
	defer tdb.Close()
	store.DB = tdb.DB
	origRDB := store.RDB
	store.RDB = nil
	defer func() { store.RDB = origRDB }()

	const userID int64 = 1201
	const sessionID = "sess-qedit-1"
	require.NoError(t, store.DB.Create(&model.Session{SessionID: sessionID, OwnerID: userID}).Error)
	require.NoError(t, store.DB.Create(&model.SessionMember{SessionID: sessionID, MemberID: userID, MemberType: 1}).Error)

	t.Run("invalid payload: 空 session", func(t *testing.T) {
		conn := &reorderMockConn{userID: userID}
		HandleQueueEdit(nil, conn, makeQueueEditPacket(t, "", "e1", "新文本"))
		result := lastResultOf(t, conn, protocol.CmdQueueEditResult)
		require.Equal(t, false, result["ok"])
		require.Equal(t, "bad_request", result["error"])
	})

	t.Run("invalid payload: 空 content", func(t *testing.T) {
		conn := &reorderMockConn{userID: userID}
		HandleQueueEdit(nil, conn, makeQueueEditPacket(t, sessionID, "e1", "   "))
		result := lastResultOf(t, conn, protocol.CmdQueueEditResult)
		require.Equal(t, false, result["ok"])
		require.Equal(t, "empty_content", result["error"])
	})

	t.Run("权限拒绝: 会话不存在", func(t *testing.T) {
		conn := &reorderMockConn{userID: userID}
		HandleQueueEdit(nil, conn, makeQueueEditPacket(t, "sess-not-exist", "e1", "新文本"))
		result := lastResultOf(t, conn, protocol.CmdQueueEditResult)
		require.Equal(t, false, result["ok"])
		require.NotEmpty(t, result["error"])
		require.NotEqual(t, "bad_request", result["error"])
	})

	t.Run("权限拒绝: 非会话成员", func(t *testing.T) {
		conn := &reorderMockConn{userID: 2402}
		HandleQueueEdit(nil, conn, makeQueueEditPacket(t, sessionID, "e1", "新文本"))
		result := lastResultOf(t, conn, protocol.CmdQueueEditResult)
		require.Equal(t, false, result["ok"])
		require.NotEmpty(t, result["error"])
	})

	t.Run("无可路由 agent", func(t *testing.T) {
		conn := &reorderMockConn{userID: userID}
		HandleQueueEdit(nil, conn, makeQueueEditPacket(t, sessionID, "e1", "新文本"))
		result := lastResultOf(t, conn, protocol.CmdQueueEditResult)
		require.Equal(t, false, result["ok"])
		require.Equal(t, "delegate agent not found", result["error"])
	})

	t.Run("有 agent 成员但通道不可用", func(t *testing.T) {
		require.NoError(t, store.DB.Create(&model.SessionMember{SessionID: sessionID, MemberID: 3403, MemberType: 2}).Error)
		defer store.DB.Where("session_id = ? AND member_type = 2", sessionID).Delete(&model.SessionMember{})
		conn := &reorderMockConn{userID: userID}
		HandleQueueEdit(nil, conn, makeQueueEditPacket(t, sessionID, "e1", "新文本"))
		result := lastResultOf(t, conn, protocol.CmdQueueEditResult)
		require.Equal(t, false, result["ok"])
		require.Equal(t, "agent channel unavailable", result["error"])
	})
}
