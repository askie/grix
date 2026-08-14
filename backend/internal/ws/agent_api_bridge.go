package ws

import (
	"context"
	"encoding/json"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/mention"
	"github.com/askie/grix/backend/internal/store"
	"github.com/askie/grix/backend/internal/ws/agentmsg"
	"github.com/askie/grix/backend/internal/ws/agentstream"
	"github.com/askie/grix/backend/internal/ws/handler"
	"github.com/askie/grix/backend/internal/ws/protocol"
	"github.com/askie/grix/backend/internal/ws/threadmeta"
	"sync/atomic"
)

type agentBridgeConn struct {
	userID   int64
	deviceID string
	seq      int64
	sent     []bridgeSentPayload
}

type bridgeSentPayload struct {
	cmd     string
	seq     int64
	payload any
}

func (c *agentBridgeConn) SendPayload(cmd string, seq int64, payload interface{}) {
	c.sent = append(c.sent, bridgeSentPayload{
		cmd:     cmd,
		seq:     seq,
		payload: payload,
	})
}

func (c *agentBridgeConn) SendPacket(pkt *protocol.Packet) {}

func (c *agentBridgeConn) AckPush(msgID int64) {}

func (c *agentBridgeConn) Close() {}

func (c *agentBridgeConn) NextSeq() int64 {
	return atomic.AddInt64(&c.seq, 1)
}

func (c *agentBridgeConn) GetUserID() int64 {
	return c.userID
}

func (c *agentBridgeConn) GetDeviceID() string {
	return c.deviceID
}

func (c *agentBridgeConn) GetPlatform() string { return "" }

func (c *agentBridgeConn) SetAuth(userID int64, sessionID, deviceID, platform string) {}

func (c *agentBridgeConn) IsAuthed() bool { return true }

func agentAPIStreamStateKeys(agentID int64, clientMsgID string) (string, string) {
	return agentstream.StateKeys(agentID, clientMsgID)
}

func agentAPIStreamRegistryKey(agentID int64) string {
	return agentstream.RegistryKey(agentID)
}

func agentAPIStreamStateIdentity(s agentstream.State) *agentmsg.SenderIdentity {
	if s.SenderID <= 0 || s.SenderType <= 0 {
		return nil
	}
	return &agentmsg.SenderIdentity{
		SenderID:   s.SenderID,
		SenderType: s.SenderType,
	}
}

func storeAgentAPIStreamState(ctx context.Context, agentID int64, clientMsgID string, state agentstream.State) {
	agentstream.StoreState(ctx, agentID, clientMsgID, state)
}

func loadAgentAPIStreamState(ctx context.Context, agentID int64, clientMsgID string) (agentstream.State, bool) {
	return agentstream.LoadState(ctx, agentID, clientMsgID)
}

func cleanupAgentAPIStreamState(ctx context.Context, agentID int64, clientMsgID string) {
	agentstream.CleanupState(ctx, agentID, clientMsgID)
}

func resolveSessionType(sessionID string) int16 {
	sessionType := int16(1)
	if err := store.DB.Model(&model.Session{}).
		Select("session_type").
		Where("session_id = ?", sessionID).
		Scan(&sessionType).Error; err != nil {
		return 1
	}
	return sessionType
}

func normalizeAgentAPIStreamExtra(
	sessionID string,
	ownerID int64,
	sessionType int16,
	content string,
	quotedMessageID int64,
	threadID string,
	agentID int64,
	identity *agentmsg.SenderIdentity,
) json.RawMessage {
	extraRaw := mergeAgentAPIExtraWithIdentity(threadmeta.Merge(nil, threadID), content, agentID, identity)
	if sessionType != 2 {
		return mention.RemoveMentionUserIDs(extraRaw)
	}
	senderID := agentID
	if identity != nil && identity.SenderID > 0 {
		senderID = identity.SenderID
	}
	return handler.NormalizeGroupMentionExtra(
		sessionID,
		ownerID,
		senderID,
		content,
		quotedMessageID,
		extraRaw,
	)
}
