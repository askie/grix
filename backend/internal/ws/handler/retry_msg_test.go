package handler

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/store"
	wsagentapi "github.com/askie/grix/backend/internal/ws/agentapi"
	"github.com/askie/grix/backend/internal/ws/protocol"
)

func makeRetryMsgPacket(t *testing.T, payload protocol.RetryMsgPayload) *protocol.Packet {
	t.Helper()
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal retry_msg payload error: %v", err)
	}
	return &protocol.Packet{
		Cmd:     protocol.CmdRetryMsg,
		Seq:     101,
		Payload: raw,
	}
}

func findRetryMsgAck(sent []sentPayload) (protocol.RetryMsgAckPayload, bool) {
	for _, item := range sent {
		if item.cmd != protocol.CmdRetryMsgAck {
			continue
		}
		ack, ok := item.payload.(protocol.RetryMsgAckPayload)
		if ok {
			return ack, true
		}
	}
	return protocol.RetryMsgAckPayload{}, false
}

func TestHandleRetryMsgRetriggersDirectAgentDeliveryWithoutCreatingSecondMessage(t *testing.T) {
	cleanup := setupSendMsgTest(t)
	defer cleanup()

	const (
		sessionID = "session-retry-direct"
		ownerID   = int64(8801)
		agentID   = int64(9901)
		msgID     = int64(18889990001)
	)

	prevManager := wsagentapi.GetGlobal()
	wsagentapi.SetGlobal(nil)
	defer wsagentapi.SetGlobal(prevManager)

	now := time.Now().UTC()
	if err := store.DB.Create(&model.Agent{
		ID:           agentID,
		AgentName:    "retry-direct-agent",
		OwnerID:      ownerID,
		ProviderType: model.AgentProviderAPI,
		Status:       1,
		CreatedAt:    now,
		UpdatedAt:    now,
	}).Error; err != nil {
		t.Fatalf("create agent error: %v", err)
	}
	if err := store.DB.Create(&model.Session{
		SessionID:   sessionID,
		OwnerID:     ownerID,
		SessionType: 1,
		CreatedAt:   now,
		UpdatedAt:   now,
	}).Error; err != nil {
		t.Fatalf("create session error: %v", err)
	}
	members := []model.SessionMember{
		{SessionID: sessionID, MemberID: ownerID, MemberType: 1, JoinedAt: now, LastActiveAt: now},
		{SessionID: sessionID, MemberID: agentID, MemberType: 2, JoinedAt: now, LastActiveAt: now},
	}
	for _, m := range members {
		if err := store.DB.Create(&m).Error; err != nil {
			t.Fatalf("create session member error: %v", err)
		}
	}
	if err := store.DB.Create(&model.Message{
		MsgID:      msgID,
		SessionID:  sessionID,
		SenderID:   ownerID,
		SenderType: 1,
		MsgType:    1,
		Content:    "retry me in place",
		CreatedAt:  now,
	}).Error; err != nil {
		t.Fatalf("create message error: %v", err)
	}

	ownerConn := &sendMsgMockConn{userID: ownerID, deviceID: "owner-dev"}
	hub := &sendMsgMockHub{
		nodeID: "node-a",
		conns: map[int64][]ConnInterface{
			ownerID: {ownerConn},
		},
	}

	HandleRetryMsg(hub, ownerConn, makeRetryMsgPacket(t, protocol.RetryMsgPayload{
		SessionID: sessionID,
		MsgID:     msgID,
	}))

	ack, ok := findRetryMsgAck(ownerConn.sent)
	if !ok {
		t.Fatalf("expected retry_msg_ack, sent=%#v", ownerConn.sent)
	}
	if ack.Code != 0 || ack.MsgID != msgID || ack.SessionID != sessionID {
		t.Fatalf("unexpected retry_msg_ack=%#v", ack)
	}
	// retry_msg 本身不应该为被重试的那条消息再造一条聊天记录；但既然重投时 agent 仍不在线，
	// 用户应该收到一条排队提示（与首次发送时同一套 notifyAgentQueuedOffline 逻辑）。
	if countSentCmd(ownerConn.sent, protocol.CmdPushMsg) != 1 ||
		countSentCmd(ownerConn.sent, protocol.CmdAgentDeliveryStatus) != 1 {
		t.Fatalf("retry_msg should push exactly one queued-offline notice, got=%#v", ownerConn.sent)
	}

	var messageCount int64
	if err := store.DB.Model(&model.Message{}).
		Where("session_id = ?", sessionID).
		Count(&messageCount).Error; err != nil {
		t.Fatalf("count messages error: %v", err)
	}
	if messageCount != 2 {
		t.Fatalf("expected the retried message plus one queued-offline notice, got=%d", messageCount)
	}

	queuedCount, err := store.RDB.LLen(
		context.Background(),
		"im:agent_api:queued_events:9901",
	).Result()
	if err != nil {
		t.Fatalf("count queued delegate events error: %v", err)
	}
	if queuedCount != 1 {
		t.Fatalf("expected one queued retry event, got=%d", queuedCount)
	}
}
