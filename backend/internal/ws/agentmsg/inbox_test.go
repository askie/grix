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

// 复现线上事故的发生源：客户端在流式输出期间就把占位消息的最终 msg_id 计入
// 已读边界并推进了 last_read_msg_id；读者离开会话后 viewing key 失效，此时
// finalize 不能再 unread+1，否则已读内容会复活成未读（且因 command_id 幂等
// 短路而无法自愈）。修复后 finalize 在行锁内比较游标，已覆盖则跳过增量。
func TestFinalizeStreamMessageSkipsUnreadWhenReaderAlreadyPassedMessage(t *testing.T) {
	cleanup := setupInboxTest(t)
	defer cleanup()

	const (
		sessionID = "session-agentmsg-read-ahead-1"
		senderID  = int64(5301)
		readerID  = int64(5302)
		msgID     = int64(953001)
	)
	mustCreateSessionWithHumanMembers(t, sessionID, senderID, []int64{senderID, readerID})
	// 读者已把游标推进到该消息（流式期间已看到内容），未读为 0，且无 viewing key。
	if err := store.DB.Model(&model.SessionMember{}).
		Where("session_id = ? AND member_id = ?", sessionID, readerID).
		Updates(map[string]any{"last_read_msg_id": msgID, "unread_count": 0}).Error; err != nil {
		t.Fatal(err)
	}
	if err := store.DB.Create(&model.Message{MsgID: msgID, SessionID: sessionID, SenderID: senderID, SenderType: 2, MsgType: 4}).Error; err != nil {
		t.Fatal(err)
	}

	if err := FinalizeStreamMessage(context.Background(), sessionID, msgID, senderID, nil, "final", map[string]any{"content": "final", "msg_type": 1}); err != nil {
		t.Fatal(err)
	}

	// 投递本身（inbox 行）不能丢，只是未读数不能再涨。
	var inboxCount int64
	if err := store.DB.Model(&model.UserInbox{}).
		Where("user_id = ? AND msg_id = ? AND session_id = ?", readerID, msgID, sessionID).
		Count(&inboxCount).Error; err != nil {
		t.Fatal(err)
	}
	if inboxCount != 1 {
		t.Fatalf("inbox count=%d want=1", inboxCount)
	}

	var reader model.SessionMember
	if err := store.DB.First(&reader, "session_id = ? AND member_id = ? AND member_type = 1", sessionID, readerID).Error; err != nil {
		t.Fatal(err)
	}
	if reader.UnreadCount != 0 {
		t.Fatalf("reader unread_count=%d want=0 (already read past this message)", reader.UnreadCount)
	}
	if reader.LastReadMsgID != msgID {
		t.Fatalf("reader last_read_msg_id=%d want=%d", reader.LastReadMsgID, msgID)
	}

	ctx := context.Background()
	if exists, err := store.RDB.HExists(ctx, fmt.Sprintf("im:unread:%d", readerID), sessionID).Result(); err != nil || exists {
		t.Fatalf("redis unread mirror should stay empty, exists=%v err=%v", exists, err)
	}
}

// 复现线上事故的另一半：两条流式消息中较旧的一条（Thinking）最后 finalize，
// 把 sessions.last_msg_id / last_msg_summary 回写成了旧消息。修复后会话摘要
// 只能单调前进，迟到 finalize 不得回滚 tip，也不得刷新 updated_at。
func TestFinalizeStreamMessageDoesNotRegressSessionTip(t *testing.T) {
	cleanup := setupInboxTest(t)
	defer cleanup()

	const (
		sessionID = "session-agentmsg-stale-tip-1"
		senderID  = int64(5501)
		readerID  = int64(5502)
		olderID   = int64(955001)
		newerID   = int64(955009)
	)
	mustCreateSessionWithHumanMembers(t, sessionID, senderID, []int64{senderID, readerID})
	pinnedUpdatedAt := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	if err := store.DB.Model(&model.Session{}).
		Where("session_id = ?", sessionID).
		UpdateColumns(map[string]any{
			"last_msg_id":      newerID,
			"last_msg_summary": "newer message summary",
			"updated_at":       pinnedUpdatedAt,
			"state_version":    7,
		}).Error; err != nil {
		t.Fatal(err)
	}
	if err := store.DB.Create(&model.Message{MsgID: olderID, SessionID: sessionID, SenderID: senderID, SenderType: 2, MsgType: 4}).Error; err != nil {
		t.Fatal(err)
	}

	if err := FinalizeStreamMessage(context.Background(), sessionID, olderID, senderID, nil, "stale final", map[string]any{"content": "stale final", "msg_type": 1}); err != nil {
		t.Fatal(err)
	}

	var session model.Session
	if err := store.DB.First(&session, "session_id = ?", sessionID).Error; err != nil {
		t.Fatal(err)
	}
	if session.LastMsgID == nil || *session.LastMsgID != newerID {
		t.Fatalf("last_msg_id regressed: got=%v want=%d", session.LastMsgID, newerID)
	}
	if session.LastMsgSummary != "newer message summary" {
		t.Fatalf("last_msg_summary overwritten by stale message: %q", session.LastMsgSummary)
	}
	if !session.UpdatedAt.Equal(pinnedUpdatedAt) {
		t.Fatalf("updated_at refreshed by stale finalize: got=%v want=%v", session.UpdatedAt, pinnedUpdatedAt)
	}
	if session.StateVersion != 7 {
		t.Fatalf("state_version bumped by stale finalize: got=%d want=7", session.StateVersion)
	}

	// 投递与未读计数不受摘要守卫影响，旧消息仍正常送达。
	var inboxCount int64
	if err := store.DB.Model(&model.UserInbox{}).
		Where("user_id = ? AND msg_id = ? AND session_id = ?", readerID, olderID, sessionID).
		Count(&inboxCount).Error; err != nil {
		t.Fatal(err)
	}
	if inboxCount != 1 {
		t.Fatalf("inbox count=%d want=1", inboxCount)
	}
}

// 正常路径回归保护：新消息 finalize 仍然推进 last_msg_id / last_msg_summary。
func TestFinalizeStreamMessageAdvancesSessionTip(t *testing.T) {
	cleanup := setupInboxTest(t)
	defer cleanup()

	const (
		sessionID = "session-agentmsg-advance-tip-1"
		senderID  = int64(5601)
		readerID  = int64(5602)
		firstID   = int64(956001)
		secondID  = int64(956002)
	)
	mustCreateSessionWithHumanMembers(t, sessionID, senderID, []int64{senderID, readerID})
	for _, msgID := range []int64{firstID, secondID} {
		if err := store.DB.Create(&model.Message{MsgID: msgID, SessionID: sessionID, SenderID: senderID, SenderType: 2, MsgType: 4}).Error; err != nil {
			t.Fatal(err)
		}
	}

	if err := FinalizeStreamMessage(context.Background(), sessionID, firstID, senderID, nil, "first final", map[string]any{"content": "first final", "msg_type": 1}); err != nil {
		t.Fatal(err)
	}
	var session model.Session
	if err := store.DB.First(&session, "session_id = ?", sessionID).Error; err != nil {
		t.Fatal(err)
	}
	if session.LastMsgID == nil || *session.LastMsgID != firstID {
		t.Fatalf("last_msg_id after first finalize: got=%v want=%d", session.LastMsgID, firstID)
	}
	if session.LastMsgSummary != "first final" {
		t.Fatalf("last_msg_summary after first finalize: %q", session.LastMsgSummary)
	}

	if err := FinalizeStreamMessage(context.Background(), sessionID, secondID, senderID, nil, "second final", map[string]any{"content": "second final", "msg_type": 1}); err != nil {
		t.Fatal(err)
	}
	if err := store.DB.First(&session, "session_id = ?", sessionID).Error; err != nil {
		t.Fatal(err)
	}
	if session.LastMsgID == nil || *session.LastMsgID != secondID {
		t.Fatalf("last_msg_id after second finalize: got=%v want=%d", session.LastMsgID, secondID)
	}
	if session.LastMsgSummary != "second final" {
		t.Fatalf("last_msg_summary after second finalize: %q", session.LastMsgSummary)
	}
}
