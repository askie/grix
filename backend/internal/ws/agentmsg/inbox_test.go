package agentmsg

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/testutil"
	"github.com/askie/grix/backend/internal/store"
)

func setupInboxTest(t *testing.T) func() {
	t.Helper()

	testDB := testutil.NewTestDB()
	store.DB = testDB.DB
	store.RDB = testutil.NewMockRedis()

	return func() {
		if store.RDB != nil {
			_ = store.RDB.Close()
		}
		testDB.Close()
	}
}

func mustCreateSessionWithHumanMembers(
	t *testing.T,
	sessionID string,
	ownerID int64,
	memberIDs []int64,
) {
	t.Helper()

	if err := store.DB.Create(&model.Session{
		SessionID:   sessionID,
		OwnerID:     ownerID,
		SessionType: 2,
	}).Error; err != nil {
		t.Fatalf("create session error: %v", err)
	}

	for _, memberID := range memberIDs {
		if err := store.DB.Create(&model.SessionMember{
			SessionID:  sessionID,
			MemberID:   memberID,
			MemberType: 1,
		}).Error; err != nil {
			t.Fatalf("create session member error user=%d: %v", memberID, err)
		}
	}
}

func TestResolveHumanSessionViewingUsers(t *testing.T) {
	cleanup := setupInboxTest(t)
	defer cleanup()

	const (
		sessionID = "session-agentmsg-viewing-map-1"
		user1     = int64(5101)
		user2     = int64(5102)
	)

	ctx := context.Background()
	viewingKey := fmt.Sprintf("im:activity:%s:human:%d:viewing", sessionID, user1)
	if err := store.RDB.Set(ctx, viewingKey, "1", 12*time.Second).Err(); err != nil {
		t.Fatalf("seed viewing key error: %v", err)
	}

	viewingUsers := resolveHumanSessionViewingUsers(ctx, sessionID, []int64{user1, user2, user1, 0, -1})
	if !viewingUsers[user1] {
		t.Fatalf("expected user1 viewing=true")
	}
	if viewingUsers[user2] {
		t.Fatalf("expected user2 viewing=false")
	}
}

func TestFinalizeStreamMessageSkipsUnreadForViewingRecipients(t *testing.T) {
	cleanup := setupInboxTest(t)
	defer cleanup()

	const (
		sessionID      = "session-agentmsg-stream-viewing-1"
		senderID       = int64(5201)
		viewingUserID  = int64(5202)
		nonViewingID   = int64(5203)
		streamFinishID = int64(952001)
	)
	mustCreateSessionWithHumanMembers(t, sessionID, senderID, []int64{senderID, viewingUserID, nonViewingID})
	if err := store.DB.Create(&model.Message{MsgID: streamFinishID, SessionID: sessionID, SenderID: senderID, SenderType: 1, MsgType: 4}).Error; err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	viewingKey := fmt.Sprintf("im:activity:%s:human:%d:viewing", sessionID, viewingUserID)
	if err := store.RDB.Set(ctx, viewingKey, "1", 12*time.Second).Err(); err != nil {
		t.Fatalf("seed viewing key error: %v", err)
	}

	if err := FinalizeStreamMessage(ctx, sessionID, streamFinishID, senderID, nil, "final", map[string]any{"content": "final", "msg_type": 1}); err != nil {
		t.Fatal(err)
	}

	for _, uid := range []int64{senderID, viewingUserID, nonViewingID} {
		var count int64
		if err := store.DB.Model(&model.UserInbox{}).
			Where("user_id = ? AND msg_id = ? AND session_id = ?", uid, streamFinishID, sessionID).
			Count(&count).Error; err != nil {
			t.Fatalf("query inbox count error user=%d: %v", uid, err)
		}
		if count != 1 {
			t.Fatalf("inbox count mismatch user=%d got=%d want=1", uid, count)
		}
	}

	var viewingMember model.SessionMember
	if err := store.DB.Where("session_id = ? AND member_id = ?", sessionID, viewingUserID).
		First(&viewingMember).Error; err != nil {
		t.Fatalf("query viewing member error: %v", err)
	}
	if viewingMember.UnreadCount != 0 {
		t.Fatalf("viewing member unread_count=%d want=0", viewingMember.UnreadCount)
	}
	if viewingMember.LastReadMsgID != streamFinishID {
		t.Fatalf("viewing member last_read_msg_id=%d want=%d", viewingMember.LastReadMsgID, streamFinishID)
	}

	var nonViewingMember model.SessionMember
	if err := store.DB.Where("session_id = ? AND member_id = ?", sessionID, nonViewingID).
		First(&nonViewingMember).Error; err != nil {
		t.Fatalf("query non-viewing member error: %v", err)
	}
	if nonViewingMember.UnreadCount != 1 {
		t.Fatalf("non-viewing member unread_count=%d want=1", nonViewingMember.UnreadCount)
	}

	viewingUnreadExists, err := store.RDB.HExists(ctx, fmt.Sprintf("im:unread:%d", viewingUserID), sessionID).Result()
	if err != nil {
		t.Fatalf("query viewing unread hash error: %v", err)
	}
	if viewingUnreadExists {
		t.Fatalf("viewing user should not keep unread hash field")
	}

	nonViewingUnread, err := store.RDB.HGet(ctx, fmt.Sprintf("im:unread:%d", nonViewingID), sessionID).Result()
	if err != nil {
		t.Fatalf("query non-viewing unread hash error: %v", err)
	}
	if nonViewingUnread != "1" {
		t.Fatalf("non-viewing unread hash=%q want=1", nonViewingUnread)
	}
}

func TestFinalizeStreamMessageUpdatesPlaceholderWithoutHumanRecipients(t *testing.T) {
	cleanup := setupInboxTest(t)
	defer cleanup()

	const (
		sessionID = "session-agent-only-finalize"
		msgID     = int64(952099)
	)
	if err := store.DB.Create(&model.Session{SessionID: sessionID, SessionType: 2}).Error; err != nil {
		t.Fatal(err)
	}
	if err := store.DB.Create(&model.Message{MsgID: msgID, SessionID: sessionID, SenderID: 99, SenderType: 2, MsgType: 4, Content: "pending"}).Error; err != nil {
		t.Fatal(err)
	}

	if err := FinalizeStreamMessage(context.Background(), sessionID, msgID, 99, nil, "final", map[string]any{"content": "final", "msg_type": 1}); err != nil {
		t.Fatal(err)
	}
	var message model.Message
	if err := store.DB.First(&message, "msg_id = ?", msgID).Error; err != nil {
		t.Fatal(err)
	}
	if message.Content != "final" || message.MsgType != 1 || message.StateVersion == 0 {
		t.Fatalf("placeholder not finalized: %#v", message)
	}
}

func TestFinalizeStreamMessageAtomicallyAppendsV2EventsAndIsIdempotent(t *testing.T) {
	cleanup := setupInboxTest(t)
	defer cleanup()
	const (
		sessionID   = "session-agentmsg-finalize-v2"
		senderID    = int64(5401)
		recipientID = int64(5402)
		msgID       = int64(954001)
	)
	mustCreateSessionWithHumanMembers(t, sessionID, senderID, []int64{senderID, recipientID})
	if err := store.DB.Create(&model.Message{MsgID: msgID, SessionID: sessionID, SenderID: senderID, SenderType: 1, MsgType: 4}).Error; err != nil {
		t.Fatal(err)
	}
	updates := map[string]any{"content": "final", "msg_type": 1}
	for i := 0; i < 2; i++ {
		if err := FinalizeStreamMessage(context.Background(), sessionID, msgID, senderID, nil, "final", updates); err != nil {
			t.Fatal(err)
		}
	}
	var msg model.Message
	if err := store.DB.First(&msg, "msg_id = ?", msgID).Error; err != nil {
		t.Fatal(err)
	}
	if msg.StateVersion != 2 || msg.MsgType != 1 || msg.Content != "final" {
		t.Fatalf("final message=%+v", msg)
	}
	var recipient model.SessionMember
	if err := store.DB.First(&recipient, "session_id = ? AND member_id = ? AND member_type = 1", sessionID, recipientID).Error; err != nil {
		t.Fatal(err)
	}
	if recipient.UnreadCount != 1 || recipient.StateVersion != 2 {
		t.Fatalf("recipient after duplicate finalize=%+v", recipient)
	}
	for _, userID := range []int64{senderID, recipientID} {
		var count int64
		if err := store.DB.Model(&model.UserSyncEvent{}).Where("user_id = ?", userID).Count(&count).Error; err != nil {
			t.Fatal(err)
		}
		if count != 3 {
			t.Fatalf("user=%d event count=%d want=3", userID, count)
		}
	}
}
