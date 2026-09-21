package handler

import (
	"encoding/json"
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
