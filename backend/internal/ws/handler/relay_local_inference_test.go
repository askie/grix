package handler

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/store"
	"github.com/askie/grix/backend/internal/ws/protocol"
)

func makeRelayLocalStreamPacket(t *testing.T, cmd string, payload any) *protocol.Packet {
	t.Helper()
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal %s payload error: %v", cmd, err)
	}
	return &protocol.Packet{
		Cmd:     cmd,
		Seq:     100,
		Payload: raw,
	}
}

func TestHandleRelayLocalStreamFinishBroadcastsReplyTarget(t *testing.T) {
	cleanup := setupSendMsgTest(t)
	defer cleanup()

	const (
		sessionID    = "session-local-stream-reply"
		originUserID = int64(9101)
		peerUserID   = int64(9102)
		agentID      = int64(9201)
		triggerMsgID = int64(18889990001)
	)

	now := time.Now()
	records := []any{
		&model.Session{
			SessionID:   sessionID,
			SessionType: 2,
			OwnerID:     originUserID,
			CreatedAt:   now,
			UpdatedAt:   now,
		},
		&model.SessionMember{
			SessionID:     sessionID,
			MemberID:      originUserID,
			MemberType:    1,
			LastReadMsgID: 0,
			LastActiveAt:  now,
			JoinedAt:      now,
		},
		&model.SessionMember{
			SessionID:     sessionID,
			MemberID:      peerUserID,
			MemberType:    1,
			LastReadMsgID: 0,
			LastActiveAt:  now,
			JoinedAt:      now,
		},
		&model.SessionMember{
			SessionID:     sessionID,
			MemberID:      agentID,
			MemberType:    2,
			LastReadMsgID: 0,
			LastActiveAt:  now,
			JoinedAt:      now,
		},
		&model.Agent{
			ID:            agentID,
			OwnerID:       originUserID,
			AgentName:     "local-agent",
			ProviderType:  model.AgentProviderLocal,
			LocalEndpoint: "http://127.0.0.1:11434",
			Status:        model.AgentStatusActive,
			CreatedAt:     now,
			UpdatedAt:     now,
		},
		&model.Message{
			MsgID:      triggerMsgID,
			SessionID:  sessionID,
			SenderID:   originUserID,
			SenderType: 1,
			MsgType:    1,
			Content:    "trigger",
			CreatedAt:  now,
		},
	}
	for _, record := range records {
		if err := store.DB.Create(record).Error; err != nil {
			t.Fatalf("create fixture error: %v", err)
		}
	}

	originConn := &sendMsgMockConn{userID: originUserID, deviceID: "dev-origin"}
	peerConn := &sendMsgMockConn{userID: peerUserID, deviceID: "dev-peer"}
	hub := &sendMsgMockHub{
		nodeID: "node-a",
		conns: map[int64][]ConnInterface{
			originUserID: {originConn},
			peerUserID:   {peerConn},
		},
	}

	HandleRelayLocalStreamStart(
		hub,
		originConn,
		makeRelayLocalStreamPacket(t, protocol.CmdRelayLocalStreamStart, protocol.RelayLocalStreamStartPayload{
			SessionID:    sessionID,
			AgentID:      agentID,
			TriggerMsgID: triggerMsgID,
		}),
	)

	var startAck protocol.RelayLocalStreamStartAckPayload
	var foundStartAck bool
	for _, sent := range originConn.sent {
		if sent.cmd != protocol.CmdRelayLocalStreamStartAck {
			continue
		}
		payload, ok := sent.payload.(protocol.RelayLocalStreamStartAckPayload)
		if !ok {
			t.Fatalf("start ack payload type=%T", sent.payload)
		}
		startAck = payload
		foundStartAck = true
		break
	}
	if !foundStartAck {
		t.Fatalf("origin should receive start ack, got=%#v", originConn.sent)
	}
	if startAck.Code != 200 || startAck.MsgID <= 0 {
		t.Fatalf("unexpected start ack: %#v", startAck)
	}

	HandleRelayLocalStreamFinish(
		hub,
		originConn,
		makeRelayLocalStreamPacket(t, protocol.CmdRelayLocalStreamFinish, protocol.RelayLocalStreamFinishPayload{
			SessionID:    sessionID,
			MsgID:        startAck.MsgID,
			FinalContent: "本地 agent 回复",
		}),
	)

	var finishPayload protocol.StreamFinishPayload
	var foundFinish bool
	for _, sent := range peerConn.sent {
		if sent.cmd != protocol.CmdStreamFinish {
			continue
		}
		payload, ok := sent.payload.(protocol.StreamFinishPayload)
		if !ok {
			t.Fatalf("finish payload type=%T", sent.payload)
		}
		finishPayload = payload
		foundFinish = true
		break
	}
	if !foundFinish {
		t.Fatalf("peer should receive stream_finish, got=%#v", peerConn.sent)
	}
	if finishPayload.QuotedMessageID != triggerMsgID {
		t.Fatalf("stream_finish quoted_message_id=%d want=%d", finishPayload.QuotedMessageID, triggerMsgID)
	}
	if finishPayload.FinalContent != "本地 agent 回复" {
		t.Fatalf("stream_finish final_content=%q", finishPayload.FinalContent)
	}

	var saved model.Message
	if err := store.DB.Where("msg_id = ? AND session_id = ?", startAck.MsgID, sessionID).
		First(&saved).Error; err != nil {
		t.Fatalf("load saved stream message error: %v", err)
	}
	if saved.QuotedMessageID != triggerMsgID {
		t.Fatalf("saved quoted_message_id=%d want=%d", saved.QuotedMessageID, triggerMsgID)
	}
}

func TestHandleRelayLocalStreamStartIsIdempotentForSameTrigger(t *testing.T) {
	cleanup := setupSendMsgTest(t)
	defer cleanup()

	const (
		sessionID    = "session-local-stream-idempotent"
		originUserID = int64(9111)
		peerUserID   = int64(9112)
		agentID      = int64(9211)
		triggerMsgID = int64(18889990123)
	)

	now := time.Now()
	records := []any{
		&model.Session{
			SessionID:   sessionID,
			SessionType: 2,
			OwnerID:     originUserID,
			CreatedAt:   now,
			UpdatedAt:   now,
		},
		&model.SessionMember{
			SessionID:     sessionID,
			MemberID:      originUserID,
			MemberType:    1,
			LastReadMsgID: 0,
			LastActiveAt:  now,
			JoinedAt:      now,
		},
		&model.SessionMember{
			SessionID:     sessionID,
			MemberID:      peerUserID,
			MemberType:    1,
			LastReadMsgID: 0,
			LastActiveAt:  now,
			JoinedAt:      now,
		},
		&model.SessionMember{
			SessionID:     sessionID,
			MemberID:      agentID,
			MemberType:    2,
			LastReadMsgID: 0,
			LastActiveAt:  now,
			JoinedAt:      now,
		},
		&model.Agent{
			ID:            agentID,
			OwnerID:       originUserID,
			AgentName:     "local-agent",
			ProviderType:  model.AgentProviderLocal,
			LocalEndpoint: "http://127.0.0.1:11434",
			Status:        model.AgentStatusActive,
			CreatedAt:     now,
			UpdatedAt:     now,
		},
		&model.Message{
			MsgID:      triggerMsgID,
			SessionID:  sessionID,
			SenderID:   originUserID,
			SenderType: 1,
			MsgType:    1,
			Content:    "trigger",
			CreatedAt:  now,
		},
	}
	for _, record := range records {
		if err := store.DB.Create(record).Error; err != nil {
			t.Fatalf("create fixture error: %v", err)
		}
	}

	conn := &sendMsgMockConn{userID: originUserID, deviceID: "dev-origin"}
	hub := &sendMsgMockHub{nodeID: "node-a", conns: map[int64][]ConnInterface{
		originUserID: {conn},
		peerUserID:   {},
	}}

	startPacket := makeRelayLocalStreamPacket(
		t,
		protocol.CmdRelayLocalStreamStart,
		protocol.RelayLocalStreamStartPayload{
			SessionID:    sessionID,
			AgentID:      agentID,
			TriggerMsgID: triggerMsgID,
		},
	)

	HandleRelayLocalStreamStart(hub, conn, startPacket)

	if len(conn.sent) == 0 {
		t.Fatalf("first start should include start ack")
	}
	firstAck, ok := conn.sent[len(conn.sent)-1].payload.(protocol.RelayLocalStreamStartAckPayload)
	if !ok {
		t.Fatalf("unexpected first start ack=%#v", conn.sent[len(conn.sent)-1].payload)
	}
	if firstAck.Code != 200 || firstAck.MsgID <= 0 {
		t.Fatalf("first start ack fields mismatch=%#v", firstAck)
	}

	HandleRelayLocalStreamStart(hub, conn, startPacket)

	if len(conn.sent) < 2 {
		t.Fatalf("second start should include start ack")
	}
	secondAck, ok := conn.sent[len(conn.sent)-1].payload.(protocol.RelayLocalStreamStartAckPayload)
	if !ok {
		t.Fatalf("unexpected second start ack=%#v", conn.sent[len(conn.sent)-1].payload)
	}
	if secondAck.Code != 200 {
		t.Fatalf("duplicate start should still return 200, ack=%#v", secondAck)
	}
	if secondAck.MsgID != firstAck.MsgID {
		t.Fatalf("duplicate start ack msg_id=%d want=%d", secondAck.MsgID, firstAck.MsgID)
	}
	if secondAck.SessionID != sessionID || secondAck.AgentID != agentID || secondAck.TriggerMsgID != triggerMsgID {
		t.Fatalf("second start ack fields mismatch=%#v", secondAck)
	}
}
