package agentsync

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/snowflake"
	"github.com/askie/grix/backend/internal/pkg/testutil"
	"github.com/askie/grix/backend/internal/store"
)

func init() {
	_ = snowflake.Init(1)
}

func setupHistoryImportTest(t *testing.T) (*testutil.TestDB, func()) {
	t.Helper()
	testDB := testutil.NewTestDB()
	store.DB = testDB.DB
	store.RDB = nil
	return testDB, func() {
		testDB.Close()
	}
}

func seedBoundAgentSession(t *testing.T, db *testutil.TestDB, ownerID, agentID int64, sessionID string) {
	t.Helper()
	now := time.Now().UTC()
	if err := db.DB.Create(&model.Session{
		SessionID:   sessionID,
		OwnerID:     ownerID,
		SessionType: model.SessionTypeDirect,
		CreatedAt:   now,
		UpdatedAt:   now,
	}).Error; err != nil {
		t.Fatalf("seed session: %v", err)
	}
	members := []model.SessionMember{
		{SessionID: sessionID, MemberID: ownerID, MemberType: 1, Role: 3, JoinedAt: now, LastActiveAt: now},
		{SessionID: sessionID, MemberID: agentID, MemberType: 2, Role: 1, JoinedAt: now, LastActiveAt: now},
	}
	if err := db.DB.Create(&members).Error; err != nil {
		t.Fatalf("seed members: %v", err)
	}
}

func TestImportPageOrdersAndDeduplicatesNativeMessages(t *testing.T) {
	db, cleanup := setupHistoryImportTest(t)
	defer cleanup()

	ownerID := int64(7001)
	agentID := int64(8001)
	sessionID := "history-sync-session"
	seedBoundAgentSession(t, db, ownerID, agentID, sessionID)

	ident := SyncIdentity{
		AgentID:     agentID,
		OwnerID:     ownerID,
		SessionID:   sessionID,
		ProviderKey: "claude",
		BindingID:   "native-session-1",
		SyncRunID:   "sync-test",
	}
	if err := Queue(context.Background(), ident); err != nil {
		t.Fatalf("queue sync: %v", err)
	}

	base := time.Now().UTC().Add(-2 * time.Hour)
	extra, _ := json.Marshal(map[string]any{"raw_type": "assistant"})
	imported, err := ImportPage(context.Background(), ImportPageParams{
		SyncIdentity: ident,
		Messages: []NativeMessage{
			{NativeMessageID: "m2", Role: "assistant", Content: "second", CreatedAt: base.Add(time.Minute), Extra: extra},
			{NativeMessageID: "m1", Role: "user", Content: "first", CreatedAt: base},
		},
	})
	if err != nil {
		t.Fatalf("import page: %v", err)
	}
	if imported != 2 {
		t.Fatalf("imported=%d want=2", imported)
	}

	var messages []model.Message
	if err := db.DB.Where("session_id = ?", sessionID).Order("msg_id ASC").Find(&messages).Error; err != nil {
		t.Fatalf("load messages: %v", err)
	}
	if len(messages) != 2 {
		t.Fatalf("messages=%d want=2", len(messages))
	}
	if messages[0].Content != "first" || messages[0].SenderID != ownerID || messages[0].SenderType != 1 {
		t.Fatalf("first message mismatch: %#v", messages[0])
	}
	if messages[1].Content != "second" || messages[1].SenderID != agentID || messages[1].SenderType != 2 {
		t.Fatalf("second message mismatch: %#v", messages[1])
	}

	importedAgain, err := ImportPage(context.Background(), ImportPageParams{
		SyncIdentity: ident,
		Messages: []NativeMessage{
			{NativeMessageID: "m1", Role: "user", Content: "first duplicate", CreatedAt: base},
			{NativeMessageID: "m2", Role: "assistant", Content: "second duplicate", CreatedAt: base.Add(time.Minute)},
		},
	})
	if err != nil {
		t.Fatalf("second import page: %v", err)
	}
	if importedAgain != 0 {
		t.Fatalf("importedAgain=%d want=0", importedAgain)
	}

	var msgCount int64
	if err := db.DB.Model(&model.Message{}).Where("session_id = ?", sessionID).Count(&msgCount).Error; err != nil {
		t.Fatalf("count messages: %v", err)
	}
	if msgCount != 2 {
		t.Fatalf("msgCount=%d want=2", msgCount)
	}

	var session model.Session
	if err := db.DB.Where("session_id = ?", sessionID).First(&session).Error; err != nil {
		t.Fatalf("load session: %v", err)
	}
	if session.LastMsgSummary != "second" {
		t.Fatalf("last_msg_summary=%q want second", session.LastMsgSummary)
	}

	var inboxCount int64
	if err := db.DB.Model(&model.UserInbox{}).Where("session_id = ? AND user_id = ?", sessionID, ownerID).Count(&inboxCount).Error; err != nil {
		t.Fatalf("count inbox: %v", err)
	}
	if inboxCount != 2 {
		t.Fatalf("inboxCount=%d want=2", inboxCount)
	}
}

func TestImportPageDoesNotOverrideExistingLiveMessageReadState(t *testing.T) {
	db, cleanup := setupHistoryImportTest(t)
	defer cleanup()

	ownerID := int64(7101)
	agentID := int64(8101)
	sessionID := "history-sync-live-session"
	seedBoundAgentSession(t, db, ownerID, agentID, sessionID)

	liveMsgID := snowflake.GenID()
	lastReadBefore := liveMsgID - 1
	liveSummary := "live message"
	if err := db.DB.Create(&model.Message{
		MsgID:      liveMsgID,
		SessionID:  sessionID,
		SenderID:   agentID,
		SenderType: 2,
		MsgType:    model.MsgTypeText,
		Content:    liveSummary,
		CreatedAt:  time.Now().UTC(),
	}).Error; err != nil {
		t.Fatalf("seed live message: %v", err)
	}
	if err := db.DB.Model(&model.Session{}).Where("session_id = ?", sessionID).
		Updates(map[string]interface{}{
			"last_msg_id":      liveMsgID,
			"last_msg_summary": liveSummary,
		}).Error; err != nil {
		t.Fatalf("seed session last message: %v", err)
	}
	if err := db.DB.Model(&model.SessionMember{}).
		Where("session_id = ? AND member_id = ? AND member_type = 1", sessionID, ownerID).
		Updates(map[string]interface{}{
			"last_read_msg_id": lastReadBefore,
			"unread_count":     1,
		}).Error; err != nil {
		t.Fatalf("seed unread state: %v", err)
	}

	ident := SyncIdentity{
		AgentID:     agentID,
		OwnerID:     ownerID,
		SessionID:   sessionID,
		ProviderKey: "claude",
		BindingID:   "native-session-live",
		SyncRunID:   "sync-live-test",
	}
	imported, err := ImportPage(context.Background(), ImportPageParams{
		SyncIdentity: ident,
		Messages: []NativeMessage{
			{
				NativeMessageID: "old-native",
				Role:            "assistant",
				Content:         "old imported history",
				CreatedAt:       time.Now().UTC().Add(-24 * time.Hour),
			},
		},
	})
	if err != nil {
		t.Fatalf("import page: %v", err)
	}
	if imported != 1 {
		t.Fatalf("imported=%d want=1", imported)
	}

	var importedMsg model.Message
	if err := db.DB.Where("session_id = ? AND content = ?", sessionID, "old imported history").First(&importedMsg).Error; err != nil {
		t.Fatalf("load imported message: %v", err)
	}
	if importedMsg.MsgID >= liveMsgID {
		t.Fatalf("imported msg_id=%d should stay below live msg_id=%d", importedMsg.MsgID, liveMsgID)
	}

	var session model.Session
	if err := db.DB.Where("session_id = ?", sessionID).First(&session).Error; err != nil {
		t.Fatalf("load session: %v", err)
	}
	if session.LastMsgID == nil || *session.LastMsgID != liveMsgID || session.LastMsgSummary != liveSummary {
		t.Fatalf("session last message overwritten: id=%v summary=%q", session.LastMsgID, session.LastMsgSummary)
	}

	var member model.SessionMember
	if err := db.DB.Where("session_id = ? AND member_id = ? AND member_type = 1", sessionID, ownerID).First(&member).Error; err != nil {
		t.Fatalf("load member: %v", err)
	}
	if member.LastReadMsgID != lastReadBefore || member.UnreadCount != 1 {
		t.Fatalf("read state changed: last_read=%d unread=%d", member.LastReadMsgID, member.UnreadCount)
	}
}

func TestImportPageDoesNotAdvanceLastActiveAtAndUsesMaxImportedMsgID(t *testing.T) {
	db, cleanup := setupHistoryImportTest(t)
	defer cleanup()

	ownerID := int64(7201)
	agentID := int64(8201)
	sessionID := "history-sync-same-time-session"
	seedBoundAgentSession(t, db, ownerID, agentID, sessionID)

	lastActiveBefore := time.Now().UTC().Add(-3 * time.Hour).Truncate(time.Second)
	if err := db.DB.Model(&model.SessionMember{}).
		Where("session_id = ? AND member_id = ? AND member_type = 1", sessionID, ownerID).
		Update("last_active_at", lastActiveBefore).Error; err != nil {
		t.Fatalf("seed last active: %v", err)
	}

	ident := SyncIdentity{
		AgentID:     agentID,
		OwnerID:     ownerID,
		SessionID:   sessionID,
		ProviderKey: "claude",
		BindingID:   "native-session-same-time",
		SyncRunID:   "sync-same-time-test",
	}
	sameCreatedAt := time.Now().UTC().Add(-6 * time.Hour).Truncate(time.Millisecond)
	imported, err := ImportPage(context.Background(), ImportPageParams{
		SyncIdentity: ident,
		Messages: []NativeMessage{
			{NativeMessageID: "same-1", Role: "assistant", Content: "same first", CreatedAt: sameCreatedAt},
			{NativeMessageID: "same-2", Role: "assistant", Content: "same second", CreatedAt: sameCreatedAt},
			{NativeMessageID: "same-3", Role: "assistant", Content: "same third", CreatedAt: sameCreatedAt},
		},
	})
	if err != nil {
		t.Fatalf("import page: %v", err)
	}
	if imported != 3 {
		t.Fatalf("imported=%d want=3", imported)
	}

	var member model.SessionMember
	if err := db.DB.Where("session_id = ? AND member_id = ? AND member_type = 1", sessionID, ownerID).First(&member).Error; err != nil {
		t.Fatalf("load member: %v", err)
	}
	if member.LastActiveAt.After(lastActiveBefore.Add(time.Second)) {
		t.Fatalf("last_active_at advanced: got=%s before=%s", member.LastActiveAt, lastActiveBefore)
	}

	var messages []model.Message
	if err := db.DB.Where("session_id = ?", sessionID).Order("msg_id DESC").Find(&messages).Error; err != nil {
		t.Fatalf("load messages: %v", err)
	}
	if len(messages) != 3 {
		t.Fatalf("messages=%d want=3", len(messages))
	}
	var session model.Session
	if err := db.DB.Where("session_id = ?", sessionID).First(&session).Error; err != nil {
		t.Fatalf("load session: %v", err)
	}
	if session.LastMsgID == nil || *session.LastMsgID != messages[0].MsgID || session.LastMsgSummary != messages[0].Content {
		t.Fatalf("session last message mismatch: last_id=%v summary=%q max_msg=%d content=%q",
			session.LastMsgID, session.LastMsgSummary, messages[0].MsgID, messages[0].Content)
	}
}
