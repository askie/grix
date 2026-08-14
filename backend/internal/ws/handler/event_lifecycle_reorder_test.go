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

type reorderMockConn struct {
	userID int64
	sent   []struct {
		cmd     string
		payload map[string]any
	}
}

func (c *reorderMockConn) SendPayload(cmd string, _ int64, payload interface{}) {
	data, _ := json.Marshal(payload)
	var m map[string]any
	_ = json.Unmarshal(data, &m)
	c.sent = append(c.sent, struct {
		cmd     string
		payload map[string]any
	}{cmd, m})
}
func (c *reorderMockConn) SendPacket(*protocol.Packet)          {}
func (c *reorderMockConn) AckPush(int64)                        {}
func (c *reorderMockConn) NextSeq() int64                       { return 1 }
func (c *reorderMockConn) Close()                               {}
func (c *reorderMockConn) GetUserID() int64                     { return c.userID }
func (c *reorderMockConn) GetDeviceID() string                  { return "test-device" }
func (c *reorderMockConn) GetPlatform() string                  { return "test" }
func (c *reorderMockConn) SetAuth(int64, string, string, string) {}
func (c *reorderMockConn) IsAuthed() bool                       { return true }

func makeReorderPacket(t *testing.T, sessionID string, ids []string) *protocol.Packet {
	t.Helper()
	raw, err := json.Marshal(protocol.QueueReorderPayload{SessionID: sessionID, OrderedEventIDs: ids})
	require.NoError(t, err)
	return &protocol.Packet{Cmd: protocol.CmdQueueReorder, Seq: 1, Payload: raw}
}

func lastReorderResult(t *testing.T, conn *reorderMockConn) map[string]any {
	t.Helper()
	require.NotEmpty(t, conn.sent)
	last := conn.sent[len(conn.sent)-1]
	require.Equal(t, protocol.CmdQueueReorderResult, last.cmd)
	return last.payload
}

func TestHandleQueueReorder(t *testing.T) {
	tdb := testutil.NewTestDB()
	defer tdb.Close()
	store.DB = tdb.DB
	origRDB := store.RDB
	store.RDB = nil
	defer func() { store.RDB = origRDB }()

	const userID int64 = 1001
	const sessionID = "sess-reorder-1"
	require.NoError(t, store.DB.Create(&model.Session{SessionID: sessionID, OwnerID: userID}).Error)
	require.NoError(t, store.DB.Create(&model.SessionMember{SessionID: sessionID, MemberID: userID, MemberType: 1}).Error)

	t.Run("invalid payload: 空 session", func(t *testing.T) {
		conn := &reorderMockConn{userID: userID}
		HandleQueueReorder(nil, conn, makeReorderPacket(t, "", []string{"e1"}))
		result := lastReorderResult(t, conn)
		require.Equal(t, false, result["success"])
		require.Equal(t, "invalid payload", result["msg"])
	})

	t.Run("invalid payload: 空清单", func(t *testing.T) {
		conn := &reorderMockConn{userID: userID}
		HandleQueueReorder(nil, conn, makeReorderPacket(t, sessionID, []string{" ", ""}))
		result := lastReorderResult(t, conn)
		require.Equal(t, false, result["success"])
		require.Equal(t, "invalid payload", result["msg"])
	})

	t.Run("权限拒绝: 会话不存在", func(t *testing.T) {
		conn := &reorderMockConn{userID: userID}
		HandleQueueReorder(nil, conn, makeReorderPacket(t, "sess-not-exist", []string{"e1"}))
		result := lastReorderResult(t, conn)
		require.Equal(t, false, result["success"])
		require.NotEmpty(t, result["msg"])
		require.NotEqual(t, "invalid payload", result["msg"])
	})

	t.Run("权限拒绝: 非会话成员", func(t *testing.T) {
		conn := &reorderMockConn{userID: 2002}
		HandleQueueReorder(nil, conn, makeReorderPacket(t, sessionID, []string{"e1"}))
		result := lastReorderResult(t, conn)
		require.Equal(t, false, result["success"])
		require.NotEmpty(t, result["msg"])
	})

	t.Run("无可路由 agent", func(t *testing.T) {
		conn := &reorderMockConn{userID: userID}
		HandleQueueReorder(nil, conn, makeReorderPacket(t, sessionID, []string{"e1", "e2"}))
		result := lastReorderResult(t, conn)
		require.Equal(t, false, result["success"])
		require.Equal(t, "delegate agent not found", result["msg"])
	})

	t.Run("有 agent 成员但通道不可用", func(t *testing.T) {
		require.NoError(t, store.DB.Create(&model.SessionMember{SessionID: sessionID, MemberID: 3003, MemberType: 2}).Error)
		defer store.DB.Where("session_id = ? AND member_type = 2", sessionID).Delete(&model.SessionMember{})
		conn := &reorderMockConn{userID: userID}
		HandleQueueReorder(nil, conn, makeReorderPacket(t, sessionID, []string{"e1"}))
		result := lastReorderResult(t, conn)
		require.Equal(t, false, result["success"])
		require.Equal(t, "agent channel unavailable", result["msg"])
	})
}
