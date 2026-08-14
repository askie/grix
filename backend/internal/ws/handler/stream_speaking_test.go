package handler

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/store"
	"github.com/askie/grix/backend/internal/ws/protocol"
)

func makeClientStreamChunkPacket(t *testing.T, payload protocol.ClientStreamChunkPayload) *protocol.Packet {
	t.Helper()
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal client_stream_chunk payload error: %v", err)
	}
	return &protocol.Packet{
		Cmd:     protocol.CmdClientStreamChunk,
		Seq:     101,
		Payload: raw,
	}
}

func TestHandleClientStreamChunkRejectsMutedRequester(t *testing.T) {
	cleanup := setupSendMsgTest(t)
	defer cleanup()

	const (
		sessionID = "session-client-stream-speaking-1"
		ownerID   = int64(67101)
		memberID  = int64(67102)
	)

	now := time.Now()
	records := []any{
		&model.Session{
			SessionID:      sessionID,
			SessionType:    model.SessionTypeGroup,
			OwnerID:        ownerID,
			LastMsgSummary: "group",
			CreatedAt:      now,
			UpdatedAt:      now,
		},
		&model.SessionMember{
			SessionID:    sessionID,
			MemberID:     ownerID,
			MemberType:   1,
			Role:         3,
			LastActiveAt: now,
			JoinedAt:     now,
		},
		&model.SessionMember{
			SessionID:     sessionID,
			MemberID:      memberID,
			MemberType:    1,
			Role:          1,
			IsSpeakMuted:  true,
			LastActiveAt:  now,
			JoinedAt:      now,
			LastReadMsgID: 0,
		},
	}
	for _, record := range records {
		if err := store.DB.Create(record).Error; err != nil {
			t.Fatalf("create fixture error: %v", err)
		}
	}

	conn := &sendMsgMockConn{userID: memberID, deviceID: "dev-member"}
	HandleClientStreamChunk(nil, conn, makeClientStreamChunkPacket(t, protocol.ClientStreamChunkPayload{
		SessionID:    sessionID,
		DeltaContent: "hello",
		IsFinish:     true,
	}))

	if len(conn.sent) != 1 || conn.sent[0].cmd != protocol.CmdStreamError {
		t.Fatalf("expected one stream_error, got=%#v", conn.sent)
	}
	errPayload, ok := conn.sent[0].payload.(protocol.StreamErrorPayload)
	if !ok {
		t.Fatalf("stream_error payload type=%T", conn.sent[0].payload)
	}
	if errPayload.ErrorCode != 4003 || errPayload.ErrorMsg != "member is muted" {
		t.Fatalf("unexpected stream_error payload=%#v", errPayload)
	}

	exists, err := store.RDB.Exists(context.Background(), "ai:ctx_ver:"+sessionID).Result()
	if err != nil {
		t.Fatalf("check ctx_ver exists error: %v", err)
	}
	if exists != 0 {
		t.Fatalf("ctx_ver should not be updated for muted requester, got=%d", exists)
	}
}

func TestHandleClientStreamChunkRejectsNonMember(t *testing.T) {
	cleanup := setupSendMsgTest(t)
	defer cleanup()

	const (
		sessionID = "session-client-stream-speaking-2"
		ownerID   = int64(67201)
		stranger  = int64(67202)
	)

	now := time.Now()
	if err := store.DB.Create(&model.Session{
		SessionID:      sessionID,
		SessionType:    model.SessionTypeGroup,
		OwnerID:        ownerID,
		LastMsgSummary: "group",
		CreatedAt:      now,
		UpdatedAt:      now,
	}).Error; err != nil {
		t.Fatalf("create session error: %v", err)
	}
	if err := store.DB.Create(&model.SessionMember{
		SessionID:    sessionID,
		MemberID:     ownerID,
		MemberType:   1,
		Role:         3,
		LastActiveAt: now,
		JoinedAt:     now,
	}).Error; err != nil {
		t.Fatalf("create member error: %v", err)
	}

	conn := &sendMsgMockConn{userID: stranger, deviceID: "dev-stranger"}
	HandleClientStreamChunk(nil, conn, makeClientStreamChunkPacket(t, protocol.ClientStreamChunkPayload{
		SessionID:    sessionID,
		DeltaContent: "hello",
		IsFinish:     true,
	}))

	if len(conn.sent) != 1 || conn.sent[0].cmd != protocol.CmdStreamError {
		t.Fatalf("expected one stream_error, got=%#v", conn.sent)
	}
	errPayload := conn.sent[0].payload.(protocol.StreamErrorPayload)
	if errPayload.ErrorCode != 4003 || errPayload.ErrorMsg != "permission denied" {
		t.Fatalf("unexpected stream_error payload=%#v", errPayload)
	}

	exists, err := store.RDB.Exists(context.Background(), "ai:ctx_ver:"+sessionID).Result()
	if err != nil {
		t.Fatalf("check ctx_ver exists error: %v", err)
	}
	if exists != 0 {
		t.Fatalf("ctx_ver should not be updated for non-member, got=%d", exists)
	}
}

func TestHandleClientStreamChunkAllowedFinishUpdatesContextVersion(t *testing.T) {
	cleanup := setupSendMsgTest(t)
	defer cleanup()

	const (
		sessionID = "session-client-stream-speaking-3"
		ownerID   = int64(67301)
	)

	now := time.Now()
	if err := store.DB.Create(&model.Session{
		SessionID:      sessionID,
		SessionType:    model.SessionTypeGroup,
		OwnerID:        ownerID,
		LastMsgSummary: "group",
		CreatedAt:      now,
		UpdatedAt:      now,
	}).Error; err != nil {
		t.Fatalf("create session error: %v", err)
	}
	if err := store.DB.Create(&model.SessionMember{
		SessionID:    sessionID,
		MemberID:     ownerID,
		MemberType:   1,
		Role:         3,
		LastActiveAt: now,
		JoinedAt:     now,
	}).Error; err != nil {
		t.Fatalf("create member error: %v", err)
	}

	conn := &sendMsgMockConn{userID: ownerID, deviceID: "dev-owner"}
	HandleClientStreamChunk(nil, conn, makeClientStreamChunkPacket(t, protocol.ClientStreamChunkPayload{
		SessionID:    sessionID,
		DeltaContent: "hello",
		IsFinish:     true,
	}))

	if len(conn.sent) != 0 {
		t.Fatalf("allowed requester should not receive stream_error, got=%#v", conn.sent)
	}

	version, err := store.RDB.Get(context.Background(), "ai:ctx_ver:"+sessionID).Int64()
	if err != nil {
		t.Fatalf("load ctx_ver error: %v", err)
	}
	if version != 1 {
		t.Fatalf("ctx_ver=%d want=1", version)
	}
}
