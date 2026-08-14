package handler

import (
	"testing"
	"time"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/store"
	"github.com/askie/grix/backend/internal/ws/protocol"
)

func TestHandleSendMsgRejectsMutedMemberSpeaking(t *testing.T) {
	cleanup := setupSendMsgTest(t)
	defer cleanup()

	const (
		sessionID = "session-speaking-send-1"
		ownerID   = int64(55101)
		memberID  = int64(55102)
	)

	now := time.Now()
	session := model.Session{
		SessionID:       sessionID,
		OwnerID:         ownerID,
		SessionType:     model.SessionTypeGroup,
		AllMembersMuted: false,
		LastMsgSummary:  "group",
	}
	if err := store.DB.Create(&session).Error; err != nil {
		t.Fatalf("create session error: %v", err)
	}
	members := []model.SessionMember{
		{SessionID: sessionID, MemberID: ownerID, MemberType: 1, Role: 3, LastActiveAt: now, JoinedAt: now},
		{SessionID: sessionID, MemberID: memberID, MemberType: 1, Role: 1, IsSpeakMuted: true, LastActiveAt: now, JoinedAt: now},
	}
	if err := store.DB.Create(&members).Error; err != nil {
		t.Fatalf("create members error: %v", err)
	}

	conn := &sendMsgMockConn{userID: memberID, deviceID: "dev-member"}
	hub := &sendMsgMockHub{nodeID: "node-a", conns: map[int64][]ConnInterface{
		memberID: {conn},
	}}

	HandleSendMsg(hub, conn, makeSendMsgPacket(t, protocol.SendMsgPayload{
		SessionID:   sessionID,
		ClientMsgID: "speaking-muted-1",
		MsgType:     1,
		Content:     "blocked",
	}))

	if len(conn.sent) != 1 || conn.sent[0].cmd != protocol.CmdSendNack {
		t.Fatalf("expected send_nack, got=%#v", conn.sent)
	}
	nack := conn.sent[0].payload.(protocol.SendNackPayload)
	if nack.Msg != "member is muted" {
		t.Fatalf("expected member muted message, got=%q", nack.Msg)
	}
}

func TestHandleSendMsgAllowsWhitelistedMemberDuringAllMuted(t *testing.T) {
	cleanup := setupSendMsgTest(t)
	defer cleanup()

	const (
		sessionID = "session-speaking-send-2"
		ownerID   = int64(55201)
		memberID  = int64(55202)
	)

	now := time.Now()
	session := model.Session{
		SessionID:       sessionID,
		OwnerID:         ownerID,
		SessionType:     model.SessionTypeGroup,
		AllMembersMuted: true,
		LastMsgSummary:  "group",
	}
	if err := store.DB.Create(&session).Error; err != nil {
		t.Fatalf("create session error: %v", err)
	}
	members := []model.SessionMember{
		{SessionID: sessionID, MemberID: ownerID, MemberType: 1, Role: 3, LastActiveAt: now, JoinedAt: now},
		{
			SessionID: sessionID, MemberID: memberID, MemberType: 1, Role: 1,
			CanSpeakWhenAllMuted: true, LastActiveAt: now, JoinedAt: now,
		},
	}
	if err := store.DB.Create(&members).Error; err != nil {
		t.Fatalf("create members error: %v", err)
	}

	conn := &sendMsgMockConn{userID: memberID, deviceID: "dev-member"}
	hub := &sendMsgMockHub{nodeID: "node-a", conns: map[int64][]ConnInterface{
		memberID: {conn},
	}}

	HandleSendMsg(hub, conn, makeSendMsgPacket(t, protocol.SendMsgPayload{
		SessionID:   sessionID,
		ClientMsgID: "speaking-allowed-1",
		MsgType:     1,
		Content:     "allowed",
	}))

	if countSentCmd(conn.sent, protocol.CmdSendAck) != 1 {
		t.Fatalf("expected send_ack, got=%#v", conn.sent)
	}
}
