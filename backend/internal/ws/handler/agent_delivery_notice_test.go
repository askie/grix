package handler

import (
	"context"
	"fmt"
	"testing"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/store"
	"github.com/askie/grix/backend/internal/ws/protocol"
)

func TestEmitAgentDeliveryFailureMessageSkipsUnreadForViewingUsers(t *testing.T) {
	cleanup := setupSendMsgTest(t)
	defer cleanup()

	const (
		sessionID = "session-agent-delivery-notice-viewing-1"
		ownerID   = int64(8801)
		peerID    = int64(8802)
		agentID   = int64(9901)
	)

	if err := store.DB.Create(&model.Session{
		SessionID:   sessionID,
		OwnerID:     ownerID,
		SessionType: 2,
	}).Error; err != nil {
		t.Fatalf("create session error: %v", err)
	}
	for _, memberID := range []int64{ownerID, peerID} {
		if err := store.DB.Create(&model.SessionMember{
			SessionID:  sessionID,
			MemberID:   memberID,
			MemberType: 1,
		}).Error; err != nil {
			t.Fatalf("create session member error user=%d: %v", memberID, err)
		}
	}

	ownerConn := &sendMsgMockConn{userID: ownerID, deviceID: "owner-dev"}
	peerConn := &sendMsgMockConn{userID: peerID, deviceID: "peer-dev"}
	hub := &sendMsgMockHub{
		nodeID: "node-a",
		conns: map[int64][]ConnInterface{
			ownerID: {ownerConn},
			peerID:  {peerConn},
		},
	}

	if err := UpsertSessionActivity(context.Background(), hub, protocol.SessionActivityPayload{
		SessionID:    sessionID,
		Kind:         protocol.SessionActivityKindViewing,
		ActorID:      ownerID,
		ActorType:    protocol.SessionActivityActorTypeHuman,
		ExecutorID:   ownerID,
		ExecutorType: protocol.SessionActivityActorTypeHuman,
		Source:       protocol.SessionActivitySourceHumanInput,
	}); err != nil {
		t.Fatalf("upsert viewing activity error: %v", err)
	}
	ownerConn.sent = nil
	peerConn.sent = nil

	ctx := context.Background()
	EmitAgentDeliveryFailureMessage(
		hub,
		ctx,
		sessionID,
		ownerID,
		agentID,
		123456,
		protocol.AgentDeliveryScopeDirect,
		protocol.AgentDeliveryCodeChannelUnavailable,
	)

	var notice model.Message
	if err := store.DB.Where("session_id = ? AND sender_id = ?", sessionID, agentID).
		Order("msg_id DESC").
		First(&notice).Error; err != nil {
		t.Fatalf("query notice message error: %v", err)
	}
	if notice.MsgID <= 0 {
		t.Fatalf("notice msg id should be positive, got=%d", notice.MsgID)
	}
	if notice.MsgType != 1 || notice.SenderType != 2 {
		t.Fatalf("notice type=(%d,%d), want agent text", notice.SenderType, notice.MsgType)
	}

	if len(ownerConn.sent) != 1 || ownerConn.sent[0].cmd != protocol.CmdPushMsg {
		t.Fatalf("owner should receive one push_msg, sent=%#v", ownerConn.sent)
	}
	if len(peerConn.sent) != 1 || peerConn.sent[0].cmd != protocol.CmdPushMsg {
		t.Fatalf("peer should receive one push_msg, sent=%#v", peerConn.sent)
	}

	var ownerMember model.SessionMember
	if err := store.DB.Where("session_id = ? AND member_id = ?", sessionID, ownerID).
		First(&ownerMember).Error; err != nil {
		t.Fatalf("query owner member error: %v", err)
	}
	if ownerMember.UnreadCount != 0 {
		t.Fatalf("viewing owner unread_count=%d want=0", ownerMember.UnreadCount)
	}
	if ownerMember.LastReadMsgID != notice.MsgID {
		t.Fatalf("viewing owner last_read_msg_id=%d want=%d", ownerMember.LastReadMsgID, notice.MsgID)
	}

	var peerMember model.SessionMember
	if err := store.DB.Where("session_id = ? AND member_id = ?", sessionID, peerID).
		First(&peerMember).Error; err != nil {
		t.Fatalf("query peer member error: %v", err)
	}
	if peerMember.UnreadCount != 1 {
		t.Fatalf("non-viewing peer unread_count=%d want=1", peerMember.UnreadCount)
	}

	ownerUnreadExists, err := store.RDB.HExists(ctx, fmt.Sprintf("im:unread:%d", ownerID), sessionID).Result()
	if err != nil {
		t.Fatalf("query owner unread hash error: %v", err)
	}
	if ownerUnreadExists {
		t.Fatalf("viewing owner should not keep unread hash field")
	}

	peerUnread, err := store.RDB.HGet(ctx, fmt.Sprintf("im:unread:%d", peerID), sessionID).Result()
	if err != nil {
		t.Fatalf("query peer unread hash error: %v", err)
	}
	if peerUnread != "1" {
		t.Fatalf("non-viewing peer unread hash=%q want=1", peerUnread)
	}
}

func TestBuildAgentDeliveryFailureMessageContent(t *testing.T) {
	if got := buildAgentDeliveryFailureMessageContent(protocol.AgentDeliveryCodeAckTimeout); got != "智能体响应超时，请稍后重试。" {
		t.Fatalf("timeout content=%q", got)
	}
	if got := buildAgentDeliveryFailureMessageContent("provider_rejected"); got != "智能体暂时不可用，请稍后重试。" {
		t.Fatalf("default content=%q", got)
	}
}
