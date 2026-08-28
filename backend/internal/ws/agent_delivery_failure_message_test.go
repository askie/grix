package ws

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/store"
	"github.com/askie/grix/backend/internal/ws/protocol"
)

func TestNotifyAgentDeliveryStatusPersistsFailedAgentReply(t *testing.T) {
	cleanup := setupAgentAPIBridgeTest(t)
	defer cleanup()

	const (
		sessionID = "agent-delivery-failure-message-1"
		ownerID   = int64(8101)
		agentID   = int64(8102)
		triggerID = int64(8103)
	)
	now := time.Now().UTC()
	if err := store.DB.Create(&model.Session{
		SessionID:   sessionID,
		OwnerID:     ownerID,
		SessionType: model.SessionTypeDirect,
		CreatedAt:   now,
		UpdatedAt:   now,
	}).Error; err != nil {
		t.Fatalf("create session: %v", err)
	}
	for _, member := range []model.SessionMember{
		{SessionID: sessionID, MemberID: ownerID, MemberType: 1, JoinedAt: now},
		{SessionID: sessionID, MemberID: agentID, MemberType: 2, JoinedAt: now},
	} {
		if err := store.DB.Create(&member).Error; err != nil {
			t.Fatalf("create session member: %v", err)
		}
	}
	if err := store.DB.Create(&model.UserSetting{
		UserID:            ownerID,
		PreferredLanguage: "en",
	}).Error; err != nil {
		t.Fatalf("create owner language setting: %v", err)
	}

	server := &Server{hub: NewHub("node-agent-delivery-failure")}
	server.notifyAgentDeliveryStatus(protocol.AgentDeliveryStatusPayload{
		SessionID:    sessionID,
		OwnerID:      ownerID,
		AgentID:      agentID,
		TriggerMsgID: triggerID,
		Scope:        protocol.AgentDeliveryScopeDirect,
		Status:       protocol.AgentDeliveryStatusTimeout,
		Code:         protocol.AgentDeliveryCodeAckTimeout,
		Msg:          "upstream API key rejected",
		UpdatedAt:    now.UnixMilli(),
	})

	var failure model.Message
	if err := store.DB.Where("session_id = ? AND sender_id = ?", sessionID, agentID).
		Order("msg_id DESC").First(&failure).Error; err != nil {
		t.Fatalf("load failure message: %v", err)
	}
	if failure.MsgType != model.MsgTypeText || failure.SenderType != 2 {
		t.Fatalf("failure message type=(%d,%d), want agent text", failure.SenderType, failure.MsgType)
	}
	if failure.Content != "The agent response timed out. Please try again later." {
		t.Fatalf("failure content=%q", failure.Content)
	}
	if string(failure.Extra) == "" || string(failure.Extra) == "null" {
		t.Fatal("failure message should retain its delivery metadata")
	}
	if strings.Contains(string(failure.Extra), "upstream API key rejected") {
		t.Fatal("failure message must not persist the upstream error detail")
	}

	var session model.Session
	if err := store.DB.Where("session_id = ?", sessionID).First(&session).Error; err != nil {
		t.Fatalf("load session: %v", err)
	}
	const expectedSummary = "The agent response timed out. Please try again later."
	if session.LastMsgSummary != expectedSummary {
		t.Fatalf("last message summary=%q want=%q", session.LastMsgSummary, expectedSummary)
	}

	var owner model.SessionMember
	if err := store.DB.Where("session_id = ? AND member_id = ?", sessionID, ownerID).
		First(&owner).Error; err != nil {
		t.Fatalf("load owner member: %v", err)
	}
	if owner.UnreadCount != 1 {
		t.Fatalf("owner unread=%d want=1", owner.UnreadCount)
	}

	server.notifyAgentDeliveryStatus(protocol.AgentDeliveryStatusPayload{
		SessionID:    sessionID,
		OwnerID:      ownerID,
		AgentID:      agentID,
		TriggerMsgID: triggerID + 1,
		Scope:        protocol.AgentDeliveryScopeDirect,
		Status:       protocol.AgentDeliveryStatusFailed,
		Code:         protocol.AgentDeliveryCodeProcessingFailed,
		Msg:          "queue full",
		UpdatedAt:    now.Add(time.Second).UnixMilli(),
	})

	var queueFull model.Message
	if err := store.DB.Where("session_id = ? AND sender_id = ?", sessionID, agentID).
		Order("msg_id DESC").First(&queueFull).Error; err != nil {
		t.Fatalf("load queue-full message: %v", err)
	}
	if queueFull.Content != "The agent's message queue is full. Please try again later." {
		t.Fatalf("queue-full content=%q", queueFull.Content)
	}
	if strings.Contains(string(queueFull.Extra), "queue full") {
		t.Fatal("queue-full message must not persist the internal failure reason")
	}

	server.notifyAgentDeliveryStatus(protocol.AgentDeliveryStatusPayload{
		SessionID:    sessionID,
		OwnerID:      ownerID,
		AgentID:      agentID,
		TriggerMsgID: triggerID + 2,
		Scope:        protocol.AgentDeliveryScopeDirect,
		Status:       protocol.AgentDeliveryStatusFailed,
		Code:         protocol.AgentDeliveryCodeChannelUnavailable,
		Msg:          "upstream API key rejected",
		UpdatedAt:    now.Add(2 * time.Second).UnixMilli(),
	})
	var reasonNotice model.Message
	if err := store.DB.Where("session_id = ? AND sender_id = ?", sessionID, agentID).
		Order("msg_id DESC").First(&reasonNotice).Error; err != nil {
		t.Fatalf("load reason notice: %v", err)
	}
	if reasonNotice.Content != "The agent failed to process this message: upstream API key rejected" {
		t.Fatalf("reason notice content=%q", reasonNotice.Content)
	}
	var visibleTo []int64
	if err := json.Unmarshal(reasonNotice.VisibleTo, &visibleTo); err != nil || len(visibleTo) != 1 || visibleTo[0] != ownerID {
		t.Fatalf("reason notice must be owner-only in direct scope, visible_to=%s err=%v", string(reasonNotice.VisibleTo), err)
	}
	if err := store.DB.Where("session_id = ?", sessionID).First(&session).Error; err != nil {
		t.Fatalf("reload session: %v", err)
	}
	if session.LastMsgSummary == "" || strings.Contains(session.LastMsgSummary, "upstream API key rejected") {
		t.Fatalf("owner-only reason notice must not rewrite the session summary, got=%q", session.LastMsgSummary)
	}
}
