package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
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
		protocol.AgentDeliveryCodeAckTimeout,
		"",
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
	if got, ownerOnly := buildAgentDeliveryFailureMessageContent(protocol.AgentDeliveryCodeAckTimeout, "", "zh"); got != "智能体响应超时，请稍后重试。" || ownerOnly {
		t.Fatalf("timeout content=%q ownerOnly=%v", got, ownerOnly)
	}
	if got, ownerOnly := buildAgentDeliveryFailureMessageContent(protocol.AgentDeliveryCodeProcessingFailed, "queue full", "en"); got != "The agent's message queue is full. Please try again later." || ownerOnly {
		t.Fatalf("queue full content=%q ownerOnly=%v", got, ownerOnly)
	}
	if got, ownerOnly := buildAgentDeliveryFailureMessageContent("provider_rejected", "No API key found for the selected model.\n\nUse /login to log in.", "unknown"); got != "智能体处理失败：No API key found for the selected model." || !ownerOnly {
		t.Fatalf("reason content=%q ownerOnly=%v", got, ownerOnly)
	}
	if got, ownerOnly := buildAgentDeliveryFailureMessageContent(protocol.AgentDeliveryCodeChannelUnavailable, "  \n ", "zh"); got != "" || ownerOnly {
		t.Fatalf("empty reason should not produce a chat message, got=%q", got)
	}
	long := strings.Repeat("x", 300)
	if got, _ := buildAgentDeliveryFailureMessageContent("", long, "en"); len([]rune(got)) > len([]rune("The agent failed to process this message: "))+agentFailureReasonMaxRunes {
		t.Fatalf("reason should be truncated, len=%d", len([]rune(got)))
	}
}

func TestAgentDeliveryFailureCopyComplete(t *testing.T) {
	expectedLanguages := []string{"zh", "en", "ja", "ko", "de", "fr", "es", "pt", "ru", "ar", "hi"}
	if len(agentDeliveryFailureCopyByLanguage) != len(expectedLanguages) {
		t.Fatalf("language count=%d want=%d", len(agentDeliveryFailureCopyByLanguage), len(expectedLanguages))
	}
	for _, language := range expectedLanguages {
		copy, ok := agentDeliveryFailureCopyByLanguage[language]
		if !ok {
			t.Fatalf("missing language %q", language)
		}
		if copy.ackTimeout == "" || copy.queueFull == "" || copy.failedPrefix == "" || copy.offlineQueued == "" {
			t.Fatalf("language %q has incomplete delivery-failure copy", language)
		}
	}
}

func TestBuildAgentDeliveryFailureMessageContentQueuedOffline(t *testing.T) {
	got, _ := buildAgentDeliveryFailureMessageContent(protocol.AgentDeliveryCodeQueuedOffline, "", "en")
	want := agentDeliveryFailureCopyByLanguage["en"].offlineQueued
	if got != want || got == "" {
		t.Fatalf("queued-offline content=%q want=%q", got, want)
	}
}

func TestNotifyAgentQueuedOfflineCooldownSuppressesRepeat(t *testing.T) {
	cleanup := setupSendMsgTest(t)
	defer cleanup()

	const (
		sessionID = "session-agent-queued-offline-cooldown"
		ownerID   = int64(8901)
		agentID   = int64(9902)
	)

	if err := store.DB.Create(&model.Session{
		SessionID:   sessionID,
		OwnerID:     ownerID,
		SessionType: 1,
	}).Error; err != nil {
		t.Fatalf("create session error: %v", err)
	}
	if err := store.DB.Create(&model.SessionMember{
		SessionID:  sessionID,
		MemberID:   ownerID,
		MemberType: 1,
	}).Error; err != nil {
		t.Fatalf("create session member error: %v", err)
	}

	ownerConn := &sendMsgMockConn{userID: ownerID, deviceID: "owner-dev"}
	hub := &sendMsgMockHub{
		nodeID: "node-a",
		conns: map[int64][]ConnInterface{
			ownerID: {ownerConn},
		},
	}

	ctx := context.Background()
	notifyAgentQueuedOffline(hub, ctx, ownerID, sessionID, agentID, 1, protocol.AgentDeliveryScopeDirect)
	if countSentCmd(ownerConn.sent, protocol.CmdAgentDeliveryStatus) != 1 || countSentCmd(ownerConn.sent, protocol.CmdPushMsg) != 1 {
		t.Fatalf("first call should notify once, sent=%#v", ownerConn.sent)
	}

	ownerConn.sent = nil
	notifyAgentQueuedOffline(hub, ctx, ownerID, sessionID, agentID, 2, protocol.AgentDeliveryScopeDirect)
	if len(ownerConn.sent) != 0 {
		t.Fatalf("repeat call within cooldown should be suppressed, sent=%#v", ownerConn.sent)
	}
}

func TestEmitAgentDeliveryFailureMessageDelegateScopeOwnerOnly(t *testing.T) {
	cleanup := setupSendMsgTest(t)
	defer cleanup()

	const (
		sessionID = "session-agent-delivery-notice-delegate-1"
		ownerID   = int64(8811)
		peerID    = int64(8812)
		agentID   = int64(9911)
	)

	if err := store.DB.Create(&model.Session{
		SessionID:      sessionID,
		OwnerID:        ownerID,
		SessionType:    1,
		LastMsgSummary: "hello",
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

	ctx := context.Background()
	EmitAgentDeliveryFailureMessage(
		hub,
		ctx,
		sessionID,
		ownerID,
		agentID,
		123457,
		protocol.AgentDeliveryScopeDelegate,
		protocol.AgentDeliveryCodeAckTimeout,
		"",
	)

	var notice model.Message
	if err := store.DB.Where("session_id = ? AND sender_id = ?", sessionID, agentID).
		Order("msg_id DESC").
		First(&notice).Error; err != nil {
		t.Fatalf("query notice message error: %v", err)
	}
	var visibleTo []int64
	if err := json.Unmarshal(notice.VisibleTo, &visibleTo); err != nil {
		t.Fatalf("notice visible_to should be a json array, raw=%s err=%v", string(notice.VisibleTo), err)
	}
	if len(visibleTo) != 1 || visibleTo[0] != ownerID {
		t.Fatalf("notice visible_to=%v want=[%d]", visibleTo, ownerID)
	}

	if len(ownerConn.sent) != 1 || ownerConn.sent[0].cmd != protocol.CmdPushMsg {
		t.Fatalf("owner should receive one push_msg, sent=%#v", ownerConn.sent)
	}
	if len(peerConn.sent) != 0 {
		t.Fatalf("peer should receive nothing in delegate scope, sent=%#v", peerConn.sent)
	}

	var peerInboxCount int64
	if err := store.DB.Model(&model.UserInbox{}).
		Where("user_id = ? AND msg_id = ?", peerID, notice.MsgID).
		Count(&peerInboxCount).Error; err != nil {
		t.Fatalf("count peer inbox error: %v", err)
	}
	if peerInboxCount != 0 {
		t.Fatalf("peer inbox rows=%d want=0", peerInboxCount)
	}

	var peerMember model.SessionMember
	if err := store.DB.Where("session_id = ? AND member_id = ?", sessionID, peerID).
		First(&peerMember).Error; err != nil {
		t.Fatalf("query peer member error: %v", err)
	}
	if peerMember.UnreadCount != 0 {
		t.Fatalf("peer unread_count=%d want=0", peerMember.UnreadCount)
	}
	if exists, _ := store.RDB.HExists(ctx, fmt.Sprintf("im:unread:%d", peerID), sessionID).Result(); exists {
		t.Fatalf("peer should not get unread hash field")
	}

	var ownerMember model.SessionMember
	if err := store.DB.Where("session_id = ? AND member_id = ?", sessionID, ownerID).
		First(&ownerMember).Error; err != nil {
		t.Fatalf("query owner member error: %v", err)
	}
	if ownerMember.UnreadCount != 1 {
		t.Fatalf("owner unread_count=%d want=1", ownerMember.UnreadCount)
	}

	var session model.Session
	if err := store.DB.Where("session_id = ?", sessionID).First(&session).Error; err != nil {
		t.Fatalf("query session error: %v", err)
	}
	if session.LastMsgSummary != "hello" {
		t.Fatalf("session last_msg_summary=%q should stay untouched for owner-only notice", session.LastMsgSummary)
	}
}
