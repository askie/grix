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

func TestEnqueueStreamInboxSkipsUnreadForViewingRecipients(t *testing.T) {
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

	ctx := context.Background()
	viewingKey := fmt.Sprintf("im:activity:%s:human:%d:viewing", sessionID, viewingUserID)
	if err := store.RDB.Set(ctx, viewingKey, "1", 12*time.Second).Err(); err != nil {
		t.Fatalf("seed viewing key error: %v", err)
	}

	EnqueueStreamInbox(ctx, sessionID, streamFinishID, senderID, nil)

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

func TestEnqueueStreamInboxIsIdempotentForSameStreamMessage(t *testing.T) {
	cleanup := setupInboxTest(t)
	defer cleanup()

	const (
		sessionID      = "session-agentmsg-stream-idempotent-1"
		senderID       = int64(5301)
		recipientID    = int64(5302)
		streamFinishID = int64(953001)
	)
	mustCreateSessionWithHumanMembers(t, sessionID, senderID, []int64{senderID, recipientID})

	ctx := context.Background()
	EnqueueStreamInbox(ctx, sessionID, streamFinishID, senderID, nil)
	EnqueueStreamInbox(ctx, sessionID, streamFinishID, senderID, nil)

	for _, uid := range []int64{senderID, recipientID} {
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

	var recipient model.SessionMember
	if err := store.DB.Where("session_id = ? AND member_id = ?", sessionID, recipientID).
		First(&recipient).Error; err != nil {
		t.Fatalf("query recipient member error: %v", err)
	}
	if recipient.UnreadCount != 1 {
		t.Fatalf("recipient unread_count=%d want=1", recipient.UnreadCount)
	}

	unreadVal, err := store.RDB.HGet(ctx, fmt.Sprintf("im:unread:%d", recipientID), sessionID).Result()
	if err != nil {
		t.Fatalf("query recipient unread hash error: %v", err)
	}
	if unreadVal != "1" {
		t.Fatalf("recipient unread hash=%q want=1", unreadVal)
	}
}
