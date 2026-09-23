package handler

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/store"
	"github.com/askie/grix/backend/internal/ws/protocol"
)

func makeSessionHistoryResetPacket(t *testing.T, payload protocol.SessionHistoryResetPayload) *protocol.Packet {
	t.Helper()
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal session_history_reset payload error: %v", err)
	}
	return &protocol.Packet{
		Cmd:     protocol.CmdSessionHistoryReset,
		Seq:     66,
		Payload: raw,
	}
}

func TestHandleSessionHistoryResetUpsertsCutoff(t *testing.T) {
	cleanup := setupSendMsgTest(t)
	defer cleanup()

	userID := int64(6101)
	sessionID := "session-history-reset-1"
	if err := store.DB.Create(&model.Session{
		SessionID:   sessionID,
		OwnerID:     userID,
		SessionType: 1,
	}).Error; err != nil {
		t.Fatalf("create session error: %v", err)
	}
	if err := store.DB.Create(&model.SessionMember{
		SessionID:   sessionID,
		MemberID:    userID,
		MemberType:  1,
		UnreadCount: 0,
	}).Error; err != nil {
		t.Fatalf("create session member error: %v", err)
	}

	conn := &sendMsgMockConn{userID: userID, deviceID: "dev-history-reset"}
	firstDeletedAt := time.Now().Add(-time.Hour).UnixMilli()
	HandleSessionHistoryReset(nil, conn, makeSessionHistoryResetPacket(t, protocol.SessionHistoryResetPayload{
		SessionID: sessionID,
		DeletedAt: firstDeletedAt,
		CommandID: "history-reset-command-1",
	}))

	var saved model.SessionHistoryReset
	if err := store.DB.Where("session_id = ? AND user_id = ?", sessionID, userID).First(&saved).Error; err != nil {
		t.Fatalf("query session_history_resets error: %v", err)
	}
	if saved.DeletedBefore.UnixMilli() != firstDeletedAt {
		t.Fatalf("deleted_before mismatch got=%d want=%d", saved.DeletedBefore.UnixMilli(), firstDeletedAt)
	}
	HandleSessionHistoryReset(nil, conn, makeSessionHistoryResetPacket(t, protocol.SessionHistoryResetPayload{
		SessionID: sessionID,
		DeletedAt: time.Now().UnixMilli(),
		CommandID: "history-reset-command-1",
	}))
	if err := store.DB.Where("session_id = ? AND user_id = ?", sessionID, userID).First(&saved).Error; err != nil {
		t.Fatal(err)
	}
	if saved.DeletedBefore.UnixMilli() != firstDeletedAt || saved.StateVersion != 1 {
		t.Fatalf("duplicate command changed reset=%+v", saved)
	}

	olderDeletedAt := time.Now().Add(-2 * time.Hour).UnixMilli()
	HandleSessionHistoryReset(nil, conn, makeSessionHistoryResetPacket(t, protocol.SessionHistoryResetPayload{
		SessionID: sessionID,
		DeletedAt: olderDeletedAt,
	}))
	var savedAfterOlder model.SessionHistoryReset
	if err := store.DB.Where("session_id = ? AND user_id = ?", sessionID, userID).First(&savedAfterOlder).Error; err != nil {
		t.Fatalf("query session_history_resets after older update error: %v", err)
	}
	if savedAfterOlder.DeletedBefore.UnixMilli() != firstDeletedAt {
		t.Fatalf("older deleted_at should not rollback cutoff got=%d want=%d", savedAfterOlder.DeletedBefore.UnixMilli(), firstDeletedAt)
	}
	var eventCount int64
	if err := store.DB.Model(&model.UserSyncEvent{}).Where("user_id = ? AND entity_id = ?", userID, sessionID).Count(&eventCount).Error; err != nil {
		t.Fatal(err)
	}
	if eventCount != 1 {
		t.Fatalf("history reset appended events=%d want=1", eventCount)
	}
}

func lastSessionHistoryResetAck(t *testing.T, conn *sendMsgMockConn) protocol.SessionHistoryResetAckPayload {
	t.Helper()
	if len(conn.sent) == 0 {
		t.Fatal("session_history_reset sent no ack")
	}
	last := conn.sent[len(conn.sent)-1]
	if last.cmd != protocol.CmdSessionHistoryResetAck {
		t.Fatalf("last cmd=%s want=%s", last.cmd, protocol.CmdSessionHistoryResetAck)
	}
	ack, ok := last.payload.(protocol.SessionHistoryResetAckPayload)
	if !ok {
		t.Fatalf("ack payload type=%T", last.payload)
	}
	return ack
}

func TestHandleSessionHistoryResetAckEchoesCommandID(t *testing.T) {
	cleanup := setupSendMsgTest(t)
	defer cleanup()

	userID := int64(6102)
	sessionID := "session-history-reset-ack-echo"
	if err := store.DB.Create(&model.Session{
		SessionID:   sessionID,
		OwnerID:     userID,
		SessionType: 1,
	}).Error; err != nil {
		t.Fatalf("create session error: %v", err)
	}
	if err := store.DB.Create(&model.SessionMember{
		SessionID:  sessionID,
		MemberID:   userID,
		MemberType: 1,
	}).Error; err != nil {
		t.Fatalf("create session member error: %v", err)
	}

	conn := &sendMsgMockConn{userID: userID, deviceID: "dev-history-reset-ack"}
	send := func(raw string) protocol.SessionHistoryResetAckPayload {
		t.Helper()
		HandleSessionHistoryReset(nil, conn, &protocol.Packet{
			Cmd:     protocol.CmdSessionHistoryReset,
			Seq:     67,
			Payload: json.RawMessage(raw),
		})
		return lastSessionHistoryResetAck(t, conn)
	}

	cases := []struct {
		name          string
		raw           string
		wantCode      int
		wantMsg       string
		wantCommandID string
	}{
		{"applied", `{"session_id":"session-history-reset-ack-echo","command_id":"cmd-applied"}`, 0, "", "cmd-applied"},
		{"duplicate retry", `{"session_id":"session-history-reset-ack-echo","command_id":"cmd-applied"}`, 0, "", "cmd-applied"},
		{"not a member", `{"session_id":"session-history-reset-other","command_id":"cmd-denied"}`, 4003, "permission denied", "cmd-denied"},
		{"missing session id", `{"command_id":"cmd-missing-session"}`, 4001, "invalid payload", "cmd-missing-session"},
		{"undecodable field", `{"session_id":"session-history-reset-ack-echo","deleted_at":"soon","command_id":"cmd-bad-field"}`, 4001, "invalid payload", "cmd-bad-field"},
		{"legacy client", `{"session_id":"session-history-reset-ack-echo"}`, 0, "", ""},
	}
	for _, tc := range cases {
		ack := send(tc.raw)
		if ack.Code != tc.wantCode || ack.Msg != tc.wantMsg || ack.CommandID != tc.wantCommandID {
			t.Fatalf("%s: ack=%+v want code=%d msg=%q command_id=%q", tc.name, ack, tc.wantCode, tc.wantMsg, tc.wantCommandID)
		}
	}
	legacyAck, err := json.Marshal(lastSessionHistoryResetAck(t, conn))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(legacyAck), "command_id") {
		t.Fatalf("legacy ack should omit command_id: %s", legacyAck)
	}

	if err := store.DB.Migrator().DropTable(&model.SessionHistoryReset{}); err != nil {
		t.Fatal(err)
	}
	if ack := send(`{"session_id":"session-history-reset-ack-echo","command_id":"cmd-save-failed"}`); ack.Code != 5001 || ack.Msg != "save failed" || ack.CommandID != "cmd-save-failed" {
		t.Fatalf("save failure ack=%+v", ack)
	}
	if err := store.DB.Migrator().DropTable(&model.SessionMember{}); err != nil {
		t.Fatal(err)
	}
	if ack := send(`{"session_id":"session-history-reset-ack-echo","command_id":"cmd-member-lookup-failed"}`); ack.Code != 5001 || ack.Msg != "permission denied" || ack.CommandID != "cmd-member-lookup-failed" {
		t.Fatalf("member lookup failure ack=%+v", ack)
	}
}
