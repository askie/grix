package handler

import (
	"testing"
	"time"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/sessionguard"
	"github.com/askie/grix/backend/internal/store"
	"github.com/askie/grix/backend/internal/ws/protocol"
)

func TestHandleSendMsgRejectsBannedGroup(t *testing.T) {
	cleanup := setupSendMsgTest(t)
	defer cleanup()

	const (
		userID    = int64(9101)
		sessionID = "banned-group-send"
	)
	now := time.Now().UTC()

	if err := store.DB.Create(&model.Session{
		SessionID:        sessionID,
		OwnerID:          userID,
		SessionType:      model.SessionTypeGroup,
		GroupName:        "Banned Group",
		ModerationStatus: model.SessionModerationStatusBanned,
		CreatedAt:        now,
		UpdatedAt:        now,
	}).Error; err != nil {
		t.Fatalf("create session error: %v", err)
	}
	if err := store.DB.Create(&model.SessionMember{
		SessionID:    sessionID,
		MemberID:     userID,
		MemberType:   1,
		Role:         3,
		JoinedAt:     now,
		LastActiveAt: now,
	}).Error; err != nil {
		t.Fatalf("create session member error: %v", err)
	}

	conn := &sendMsgMockConn{userID: userID, deviceID: "device-1"}
	hub := &sendMsgMockHub{
		nodeID: "node-1",
		conns:  map[int64][]ConnInterface{userID: []ConnInterface{conn}},
	}

	HandleSendMsg(hub, conn, makeSendMsgPacket(t, protocol.SendMsgPayload{
		SessionID:   sessionID,
		ClientMsgID: "banned-group-cmsg",
		MsgType:     1,
		Content:     "hello",
	}))

	if len(conn.sent) != 1 {
		t.Fatalf("expected 1 response payload, got %d", len(conn.sent))
	}
	if conn.sent[0].cmd != protocol.CmdSendNack {
		t.Fatalf("expected send_nack, got %s", conn.sent[0].cmd)
	}
	nack, ok := conn.sent[0].payload.(protocol.SendNackPayload)
	if !ok {
		t.Fatalf("expected SendNackPayload, got %T", conn.sent[0].payload)
	}
	if nack.Code != 4003 {
		t.Fatalf("expected code=4003, got %d", nack.Code)
	}
	if nack.Msg != sessionguard.ErrSessionBanned.Error() {
		t.Fatalf("expected msg=%q, got %q", sessionguard.ErrSessionBanned.Error(), nack.Msg)
	}
}
