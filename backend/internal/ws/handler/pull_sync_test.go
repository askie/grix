package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/store"
	"github.com/askie/grix/backend/internal/ws/protocol"
)

func makePullSyncPacket(t *testing.T, lastInboxSeq int64) *protocol.Packet {
	t.Helper()
	raw, err := json.Marshal(protocol.PullSyncPayload{LastInboxSeq: lastInboxSeq})
	if err != nil {
		t.Fatalf("marshal pull_sync payload error: %v", err)
	}
	return &protocol.Packet{
		Cmd:     protocol.CmdPullSync,
		Seq:     88,
		Payload: raw,
	}
}

func TestHandlePullSyncReturnsOrderedMessages(t *testing.T) {
	cleanup := setupSendMsgTest(t)
	defer cleanup()

	sessionID := "session-pull-1"
	if err := store.DB.Create(&model.Session{
		SessionID:   sessionID,
		OwnerID:     5001,
		SessionType: 1,
	}).Error; err != nil {
		t.Fatalf("create session error: %v", err)
	}
	msg1 := model.Message{
		MsgID:      7001,
		SessionID:  sessionID,
		SenderID:   5002,
		SenderType: 1,
		MsgType:    1,
		Content:    "first",
	}
	msg2 := model.Message{
		MsgID:      7002,
		SessionID:  sessionID,
		SenderID:   5002,
		SenderType: 1,
		MsgType:    1,
		Content:    "second",
	}
	if err := store.DB.Create(&msg1).Error; err != nil {
		t.Fatalf("create message1 error: %v", err)
	}
	if err := store.DB.Create(&msg2).Error; err != nil {
		t.Fatalf("create message2 error: %v", err)
	}

	inboxes := []model.UserInbox{
		{UserID: 5001, InboxSeq: 10, MsgID: 7001, SessionID: sessionID},
		{UserID: 5001, InboxSeq: 11, MsgID: 7002, SessionID: sessionID},
	}
	for _, inbox := range inboxes {
		if err := store.DB.Create(&inbox).Error; err != nil {
			t.Fatalf("create user_inbox error: %v", err)
		}
	}

	if err := store.DB.Create(&model.SessionMember{
		SessionID:   sessionID,
		MemberID:    5001,
		MemberType:  1,
		UnreadCount: 5,
	}).Error; err != nil {
		t.Fatalf("create session member error: %v", err)
	}
	conn := &sendMsgMockConn{userID: 5001, deviceID: "dev-pull"}
	HandlePullSync(nil, conn, makePullSyncPacket(t, 9))

	if len(conn.sent) != 1 {
		t.Fatalf("expected one pull_sync_resp payload, got=%d", len(conn.sent))
	}
	resp, ok := conn.sent[0].payload.(protocol.PullSyncRespPayload)
	if !ok {
		t.Fatalf("payload type mismatch: got=%T", conn.sent[0].payload)
	}
	if resp.HasMore {
		t.Fatalf("expected has_more=false for two rows")
	}
	if len(resp.Messages) != 2 {
		t.Fatalf("expected 2 messages, got=%d", len(resp.Messages))
	}
	if resp.UnreadSnapshot[sessionID] != 5 {
		t.Fatalf("expected unread_snapshot[%s]=5, got=%d", sessionID, resp.UnreadSnapshot[sessionID])
	}
	if resp.Messages[0].InboxSeq != 10 || resp.Messages[0].MsgID != 7001 {
		t.Fatalf("first message mismatch: inbox_seq=%d msg_id=%d",
			resp.Messages[0].InboxSeq, resp.Messages[0].MsgID)
	}
	if resp.Messages[1].InboxSeq != 11 || resp.Messages[1].MsgID != 7002 {
		t.Fatalf("second message mismatch: inbox_seq=%d msg_id=%d",
			resp.Messages[1].InboxSeq, resp.Messages[1].MsgID)
	}
}

func TestHandlePullSyncReturnsRevokedMessagesAsDeleteEvents(t *testing.T) {
	cleanup := setupSendMsgTest(t)
	defer cleanup()

	sessionID := "session-pull-revoke"
	if err := store.DB.Create(&model.Session{
		SessionID:   sessionID,
		OwnerID:     5101,
		SessionType: 1,
	}).Error; err != nil {
		t.Fatalf("create session error: %v", err)
	}
	if err := store.DB.Create(&model.Message{
		MsgID:      7101,
		SessionID:  sessionID,
		SenderID:   5102,
		SenderType: 1,
		MsgType:    1,
		Content:    "deleted later",
		IsDeleted:  true,
		IsRevoked:  true,
	}).Error; err != nil {
		t.Fatalf("create revoked message error: %v", err)
	}
	if err := store.DB.Create(&model.UserInbox{
		UserID:    5101,
		InboxSeq:  12,
		MsgID:     7101,
		SessionID: sessionID,
	}).Error; err != nil {
		t.Fatalf("create revoke inbox error: %v", err)
	}

	conn := &sendMsgMockConn{userID: 5101, deviceID: "dev-pull-revoke"}
	HandlePullSync(nil, conn, makePullSyncPacket(t, 11))

	if len(conn.sent) != 1 {
		t.Fatalf("expected one pull_sync_resp payload, got=%d", len(conn.sent))
	}
	resp, ok := conn.sent[0].payload.(protocol.PullSyncRespPayload)
	if !ok {
		t.Fatalf("payload type mismatch: got=%T", conn.sent[0].payload)
	}
	if len(resp.Messages) != 1 {
		t.Fatalf("expected 1 revoke event, got=%d", len(resp.Messages))
	}
	if resp.Messages[0].InboxSeq != 12 {
		t.Fatalf("revoke inbox_seq=%d want=12", resp.Messages[0].InboxSeq)
	}
	if resp.Messages[0].MsgID != 7101 {
		t.Fatalf("revoke msg_id=%d want=7101", resp.Messages[0].MsgID)
	}
	if !resp.Messages[0].IsRevoked {
		t.Fatal("expected pull_sync message to carry is_revoked=true")
	}
	if resp.Messages[0].Content != "" {
		t.Fatalf("revoked tombstone content=%q want empty", resp.Messages[0].Content)
	}
	if len(resp.Messages[0].Extra) != 0 {
		t.Fatalf("revoked tombstone extra should be empty, got=%s", string(resp.Messages[0].Extra))
	}
}

func TestHandlePullSyncUnreadSnapshotFallsBackToDB(t *testing.T) {
	cleanup := setupSendMsgTest(t)
	defer cleanup()

	sessionID := "session-pull-unread-db"
	if err := store.DB.Create(&model.Session{
		SessionID:   sessionID,
		OwnerID:     5201,
		SessionType: model.SessionTypeDirect,
	}).Error; err != nil {
		t.Fatalf("create session error: %v", err)
	}
	if err := store.DB.Create(&model.SessionMember{
		SessionID:   sessionID,
		MemberID:    5201,
		MemberType:  1,
		UnreadCount: 4,
	}).Error; err != nil {
		t.Fatalf("create session member error: %v", err)
	}

	conn := &sendMsgMockConn{userID: 5201, deviceID: "dev-pull-db"}
	HandlePullSync(nil, conn, makePullSyncPacket(t, 0))

	if len(conn.sent) != 1 {
		t.Fatalf("expected one pull_sync_resp payload, got=%d", len(conn.sent))
	}
	resp, ok := conn.sent[0].payload.(protocol.PullSyncRespPayload)
	if !ok {
		t.Fatalf("payload type mismatch: got=%T", conn.sent[0].payload)
	}
	if resp.UnreadSnapshot[sessionID] != 4 {
		t.Fatalf("expected unread_snapshot[%s]=4 from db fallback, got=%d", sessionID, resp.UnreadSnapshot[sessionID])
	}
}

func TestHandlePullSyncIncludesEditSyncEvent(t *testing.T) {
	cleanup := setupSendMsgTest(t)
	defer cleanup()

	sessionID := "session-pull-edit"
	if err := store.DB.Create(&model.Session{
		SessionID:   sessionID,
		OwnerID:     5101,
		SessionType: 1,
	}).Error; err != nil {
		t.Fatalf("create session error: %v", err)
	}
	if err := store.DB.Create(&model.Message{
		MsgID:      7201,
		SessionID:  sessionID,
		SenderID:   5102,
		SenderType: 1,
		MsgType:    1,
		Content:    "edited content",
		CreatedAt:  time.Now().UTC(),
	}).Error; err != nil {
		t.Fatalf("create edited message error: %v", err)
	}
	if err := store.DB.Create(&model.UserInbox{
		UserID:    5101,
		InboxSeq:  13,
		MsgID:     7201,
		SessionID: sessionID,
		EventKind: model.UserInboxEventKindEdit,
	}).Error; err != nil {
		t.Fatalf("create edit inbox error: %v", err)
	}

	conn := &sendMsgMockConn{userID: 5101, deviceID: "dev-pull-edit"}
	HandlePullSync(nil, conn, makePullSyncPacket(t, 12))

	if len(conn.sent) != 1 {
		t.Fatalf("expected one pull_sync_resp payload, got=%d", len(conn.sent))
	}
	resp, ok := conn.sent[0].payload.(protocol.PullSyncRespPayload)
	if !ok {
		t.Fatalf("payload type mismatch: got=%T", conn.sent[0].payload)
	}
	if len(resp.Messages) != 1 {
		t.Fatalf("expected 1 edit event, got=%d", len(resp.Messages))
	}
	if resp.Messages[0].SyncEvent != model.UserInboxEventKindEdit {
		t.Fatalf("sync_event=%q want=%q", resp.Messages[0].SyncEvent, model.UserInboxEventKindEdit)
	}
	if resp.Messages[0].Content != "edited content" {
		t.Fatalf("content=%q want edited content", resp.Messages[0].Content)
	}
}

func TestHandlePullSyncIncludesThreadID(t *testing.T) {
	cleanup := setupSendMsgTest(t)
	defer cleanup()

	sessionID := "session-pull-thread"
	if err := store.DB.Create(&model.Session{
		SessionID:   sessionID,
		OwnerID:     5601,
		SessionType: 1,
	}).Error; err != nil {
		t.Fatalf("create session error: %v", err)
	}
	if err := store.DB.Create(&model.Message{
		MsgID:      7301,
		SessionID:  sessionID,
		ThreadID:   "topic-a",
		SenderID:   5602,
		SenderType: 1,
		MsgType:    1,
		Content:    "threaded message",
		CreatedAt:  time.Now().UTC(),
	}).Error; err != nil {
		t.Fatalf("create threaded message error: %v", err)
	}
	if err := store.DB.Create(&model.UserInbox{
		UserID:    5601,
		InboxSeq:  14,
		MsgID:     7301,
		SessionID: sessionID,
	}).Error; err != nil {
		t.Fatalf("create inbox error: %v", err)
	}

	conn := &sendMsgMockConn{userID: 5601, deviceID: "dev-pull-thread"}
	HandlePullSync(nil, conn, makePullSyncPacket(t, 13))

	if len(conn.sent) != 1 {
		t.Fatalf("expected one pull_sync_resp payload, got=%d", len(conn.sent))
	}
	resp, ok := conn.sent[0].payload.(protocol.PullSyncRespPayload)
	if !ok {
		t.Fatalf("payload type mismatch: got=%T", conn.sent[0].payload)
	}
	if len(resp.Messages) != 1 {
		t.Fatalf("expected 1 threaded message, got=%d", len(resp.Messages))
	}
	if resp.Messages[0].ThreadID != "topic-a" {
		t.Fatalf("thread_id=%q want=topic-a", resp.Messages[0].ThreadID)
	}
}

func TestHandlePullSyncUnreadSnapshotIgnoresStaleRedisValues(t *testing.T) {
	cleanup := setupSendMsgTest(t)
	defer cleanup()

	sessionID := "session-pull-unread-stale-redis"
	if err := store.DB.Create(&model.Session{
		SessionID:   sessionID,
		OwnerID:     5301,
		SessionType: model.SessionTypeDirect,
	}).Error; err != nil {
		t.Fatalf("create session error: %v", err)
	}
	if err := store.DB.Create(&model.SessionMember{
		SessionID:   sessionID,
		MemberID:    5301,
		MemberType:  1,
		UnreadCount: 2,
	}).Error; err != nil {
		t.Fatalf("create session member error: %v", err)
	}
	if err := store.RDB.HSet(context.Background(), "im:unread:5301", sessionID, 9).Err(); err != nil {
		t.Fatalf("seed redis unread error: %v", err)
	}

	conn := &sendMsgMockConn{userID: 5301, deviceID: "dev-pull-merge"}
	HandlePullSync(nil, conn, makePullSyncPacket(t, 0))

	if len(conn.sent) != 1 {
		t.Fatalf("expected one pull_sync_resp payload, got=%d", len(conn.sent))
	}
	resp, ok := conn.sent[0].payload.(protocol.PullSyncRespPayload)
	if !ok {
		t.Fatalf("payload type mismatch: got=%T", conn.sent[0].payload)
	}
	if resp.UnreadSnapshot[sessionID] != 2 {
		t.Fatalf("expected unread_snapshot[%s]=2 from db authority, got=%d", sessionID, resp.UnreadSnapshot[sessionID])
	}
}

func TestHandlePullSyncUnreadSnapshotOmitsZeroUnreadEvenIfRedisStale(t *testing.T) {
	cleanup := setupSendMsgTest(t)
	defer cleanup()

	sessionID := "session-pull-unread-zero"
	if err := store.DB.Create(&model.SessionMember{
		SessionID:   sessionID,
		MemberID:    5401,
		MemberType:  1,
		UnreadCount: 0,
	}).Error; err != nil {
		t.Fatalf("create session member error: %v", err)
	}
	if err := store.RDB.HSet(context.Background(), "im:unread:5401", sessionID, 4).Err(); err != nil {
		t.Fatalf("seed redis unread error: %v", err)
	}

	conn := &sendMsgMockConn{userID: 5401, deviceID: "dev-pull-zero"}
	HandlePullSync(nil, conn, makePullSyncPacket(t, 0))

	if len(conn.sent) != 1 {
		t.Fatalf("expected one pull_sync_resp payload, got=%d", len(conn.sent))
	}
	resp, ok := conn.sent[0].payload.(protocol.PullSyncRespPayload)
	if !ok {
		t.Fatalf("payload type mismatch: got=%T", conn.sent[0].payload)
	}
	if _, exists := resp.UnreadSnapshot[sessionID]; exists {
		t.Fatalf("expected unread_snapshot[%s] to be omitted for zero unread", sessionID)
	}
}

func TestHandlePullSyncIncludesActiveVoiceCallsSnapshot(t *testing.T) {
	cleanup := setupSendMsgTest(t)
	defer cleanup()

	const owner = int64(5701)
	now := time.Now()
	mkCall := func(id, callee int64, session string, state int16) {
		if err := store.DB.Create(&model.CallRecord{
			ID:             id,
			SessionID:      session,
			CallerID:       6001,
			CalleeID:       callee,
			CallMode:       model.CallModeVoice,
			State:          state,
			DelegationMode: model.CallDelegationAIDelegated,
			StartedAt:      &now,
		}).Error; err != nil {
			t.Fatalf("create call record error: %v", err)
		}
	}
	mkCall(8001, owner, "voice-sess-active", model.CallStateAIDelegated) // ✓ 应进快照
	mkCall(8002, owner, "voice-sess-ended", model.CallStateEnded)        // ✗ 已结束
	mkCall(8003, 9999, "voice-sess-other", model.CallStateAIDelegated)   // ✗ 他人

	conn := &sendMsgMockConn{userID: owner, deviceID: "dev-pull-voice"}
	HandlePullSync(nil, conn, makePullSyncPacket(t, 0))

	if len(conn.sent) != 1 {
		t.Fatalf("expected one pull_sync_resp payload, got=%d", len(conn.sent))
	}
	resp, ok := conn.sent[0].payload.(protocol.PullSyncRespPayload)
	if !ok {
		t.Fatalf("payload type mismatch: got=%T", conn.sent[0].payload)
	}
	if resp.HasMore {
		t.Fatalf("expected has_more=false so snapshot applies")
	}
	if len(resp.ActiveVoiceCalls) != 1 {
		t.Fatalf("expected 1 active voice call, got=%d", len(resp.ActiveVoiceCalls))
	}
	got := resp.ActiveVoiceCalls[0]
	if got.CallID != "8001" || got.SessionID != "voice-sess-active" {
		t.Fatalf("snapshot entry mismatch: call_id=%s session_id=%s", got.CallID, got.SessionID)
	}
}

func TestHandlePullSyncFiltersMessagesBeforeHistoryReset(t *testing.T) {
	cleanup := setupSendMsgTest(t)
	defer cleanup()

	userID := int64(5501)
	sessionID := "session-pull-reset"
	if err := store.DB.Create(&model.Session{
		SessionID:   sessionID,
		OwnerID:     userID,
		SessionType: 1,
	}).Error; err != nil {
		t.Fatalf("create session error: %v", err)
	}

	oldMsgTime := time.Now().Add(-2 * time.Hour)
	newMsgTime := time.Now().Add(-time.Minute)
	oldMsg := model.Message{
		MsgID:      7201,
		SessionID:  sessionID,
		SenderID:   5502,
		SenderType: 1,
		MsgType:    1,
		Content:    "old",
		CreatedAt:  oldMsgTime,
	}
	newMsg := model.Message{
		MsgID:      7202,
		SessionID:  sessionID,
		SenderID:   5502,
		SenderType: 1,
		MsgType:    1,
		Content:    "new",
		CreatedAt:  newMsgTime,
	}
	if err := store.DB.Create(&oldMsg).Error; err != nil {
		t.Fatalf("create old message error: %v", err)
	}
	if err := store.DB.Create(&newMsg).Error; err != nil {
		t.Fatalf("create new message error: %v", err)
	}

	if err := store.DB.Create(&model.UserInbox{
		UserID:    userID,
		InboxSeq:  1,
		MsgID:     oldMsg.MsgID,
		SessionID: sessionID,
	}).Error; err != nil {
		t.Fatalf("create old inbox error: %v", err)
	}
	if err := store.DB.Create(&model.UserInbox{
		UserID:    userID,
		InboxSeq:  2,
		MsgID:     newMsg.MsgID,
		SessionID: sessionID,
	}).Error; err != nil {
		t.Fatalf("create new inbox error: %v", err)
	}

	if err := store.DB.Create(&model.SessionHistoryReset{
		SessionID:     sessionID,
		UserID:        userID,
		DeletedBefore: oldMsgTime.Add(30 * time.Minute),
		CreatedAt:     time.Now(),
		UpdatedAt:     time.Now(),
	}).Error; err != nil {
		t.Fatalf("create history reset error: %v", err)
	}

	conn := &sendMsgMockConn{userID: userID, deviceID: "dev-pull-reset"}
	HandlePullSync(nil, conn, makePullSyncPacket(t, 0))

	if len(conn.sent) != 1 {
		t.Fatalf("expected one pull_sync_resp payload, got=%d", len(conn.sent))
	}
	resp, ok := conn.sent[0].payload.(protocol.PullSyncRespPayload)
	if !ok {
		t.Fatalf("payload type mismatch: got=%T", conn.sent[0].payload)
	}
	if len(resp.Messages) != 1 {
		t.Fatalf("expected only one message after reset filter, got=%d", len(resp.Messages))
	}
	if resp.Messages[0].MsgID != newMsg.MsgID {
		t.Fatalf("expected newest message msg_id=%d, got=%d", newMsg.MsgID, resp.Messages[0].MsgID)
	}
}

// TestHandlePullSyncUnreadSnapshotRespectsCutoffAndDeletion 锁定未读快照与会话列表
// 共用的会话有效性口径：删除后无新消息 / 会话已删除 / 群被封禁的未读不计入，
// 删除后又来新消息的未读重新计入，普通会话正常计入。
func TestHandlePullSyncUnreadSnapshotRespectsCutoffAndDeletion(t *testing.T) {
	cleanup := setupSendMsgTest(t)
	defer cleanup()

	userID := int64(5601)
	now := time.Now()

	type seed struct {
		sessionID   string
		sessionType int16
		moderation  int16
		isDeleted   bool
		unread      int
		hasReset    bool
		cutoff      time.Time
		msgAfter    bool // 是否存在 cutoff 之后的可见消息
	}
	seeds := []seed{
		// 删除后无新消息 → 不计入
		{"snap-stale", model.SessionTypeDirect, model.SessionModerationStatusActive, false, 3, true, now.Add(-30 * time.Minute), false},
		// 删除后又来新消息 → 计入
		{"snap-fresh", model.SessionTypeDirect, model.SessionModerationStatusActive, false, 2, true, now.Add(-2 * time.Hour), true},
		// 会话已删除 → 不计入
		{"snap-deleted", model.SessionTypeDirect, model.SessionModerationStatusActive, true, 5, false, time.Time{}, false},
		// 群被封禁 → 不计入
		{"snap-banned", model.SessionTypeGroup, model.SessionModerationStatusBanned, false, 6, false, time.Time{}, false},
		// 普通会话 → 计入
		{"snap-normal", model.SessionTypeDirect, model.SessionModerationStatusActive, false, 4, false, time.Time{}, false},
	}

	var msgID int64 = 7301
	for _, s := range seeds {
		if err := store.DB.Create(&model.Session{
			SessionID:        s.sessionID,
			OwnerID:          userID,
			SessionType:      s.sessionType,
			ModerationStatus: s.moderation,
			IsDeleted:        s.isDeleted,
		}).Error; err != nil {
			t.Fatalf("create session %s error: %v", s.sessionID, err)
		}
		if err := store.DB.Create(&model.SessionMember{
			SessionID:   s.sessionID,
			MemberID:    userID,
			MemberType:  1,
			UnreadCount: s.unread,
		}).Error; err != nil {
			t.Fatalf("create member %s error: %v", s.sessionID, err)
		}
		if s.hasReset {
			if err := store.DB.Create(&model.SessionHistoryReset{
				SessionID:     s.sessionID,
				UserID:        userID,
				DeletedBefore: s.cutoff,
				CreatedAt:     now,
				UpdatedAt:     now,
			}).Error; err != nil {
				t.Fatalf("create reset %s error: %v", s.sessionID, err)
			}
		}
		if s.msgAfter {
			msgID++
			if err := store.DB.Create(&model.Message{
				MsgID:      msgID,
				SessionID:  s.sessionID,
				SenderID:   9999,
				SenderType: 1,
				MsgType:    1,
				Content:    "after cutoff",
				CreatedAt:  now.Add(-time.Minute),
			}).Error; err != nil {
				t.Fatalf("create msg %s error: %v", s.sessionID, err)
			}
		}
	}

	conn := &sendMsgMockConn{userID: userID, deviceID: "dev-pull-cutoff"}
	HandlePullSync(nil, conn, makePullSyncPacket(t, 0))

	if len(conn.sent) != 1 {
		t.Fatalf("expected one pull_sync_resp payload, got=%d", len(conn.sent))
	}
	resp, ok := conn.sent[0].payload.(protocol.PullSyncRespPayload)
	if !ok {
		t.Fatalf("payload type mismatch: got=%T", conn.sent[0].payload)
	}

	if resp.UnreadSnapshot["snap-fresh"] != 2 {
		t.Fatalf("snap-fresh expected 2 (deleted then new msg), got=%d", resp.UnreadSnapshot["snap-fresh"])
	}
	if resp.UnreadSnapshot["snap-normal"] != 4 {
		t.Fatalf("snap-normal expected 4, got=%d", resp.UnreadSnapshot["snap-normal"])
	}
	for _, sid := range []string{"snap-stale", "snap-deleted", "snap-banned"} {
		if v, exists := resp.UnreadSnapshot[sid]; exists && v != 0 {
			t.Fatalf("%s expected omitted from snapshot, got=%d", sid, v)
		}
	}
}

// 私聊消息必须随身带上会话成员身份：客户端据此在离线补拉落库那一刻就定下会话
// 对端，不再对系统消息（sender_type=3）等推导不出对端的消息留下无 peer 的会话行。
func TestHandlePullSyncFillsPrivateSessionMembers(t *testing.T) {
	cleanup := setupSendMsgTest(t)
	defer cleanup()

	sessionID := "session-pull-members"
	if err := store.DB.Create(&model.Session{
		SessionID:   sessionID,
		OwnerID:     6001,
		SessionType: model.SessionTypeDirect,
	}).Error; err != nil {
		t.Fatalf("create session error: %v", err)
	}
	if err := store.DB.Create(&model.Message{
		MsgID:      7101,
		SessionID:  sessionID,
		SenderID:   0,
		SenderType: 3,
		MsgType:    1,
		Content:    "system notice",
	}).Error; err != nil {
		t.Fatalf("create message error: %v", err)
	}
	if err := store.DB.Create(&model.UserInbox{
		UserID: 6001, InboxSeq: 10, MsgID: 7101, SessionID: sessionID,
	}).Error; err != nil {
		t.Fatalf("create user_inbox error: %v", err)
	}
	members := []model.SessionMember{
		{SessionID: sessionID, MemberID: 6001, MemberType: 1},
		{SessionID: sessionID, MemberID: 6002, MemberType: 2},
	}
	for _, member := range members {
		if err := store.DB.Create(&member).Error; err != nil {
			t.Fatalf("create session member error: %v", err)
		}
	}

	conn := &sendMsgMockConn{userID: 6001, deviceID: "dev-pull-members"}
	HandlePullSync(nil, conn, makePullSyncPacket(t, 9))

	resp, ok := conn.sent[0].payload.(protocol.PullSyncRespPayload)
	if !ok {
		t.Fatalf("payload type mismatch: got=%T", conn.sent[0].payload)
	}
	if len(resp.Messages) != 1 {
		t.Fatalf("expected 1 message, got=%d", len(resp.Messages))
	}
	got := resp.Messages[0].SessionMembers
	if len(got) != 2 {
		t.Fatalf("expected 2 session members, got=%d (%+v)", len(got), got)
	}
	if got[0].MemberID != 6001 || got[0].MemberType != 1 {
		t.Fatalf("unexpected first member: %+v", got[0])
	}
	if got[1].MemberID != 6002 || got[1].MemberType != 2 {
		t.Fatalf("unexpected second member: %+v", got[1])
	}
}

// 群聊的会话归组键本来就是会话 ID，成员身份对客户端归组毫无作用，只会放大包体。
func TestHandlePullSyncOmitsGroupSessionMembers(t *testing.T) {
	cleanup := setupSendMsgTest(t)
	defer cleanup()

	sessionID := "session-pull-group-members"
	if err := store.DB.Create(&model.Session{
		SessionID:   sessionID,
		OwnerID:     6101,
		SessionType: 2,
	}).Error; err != nil {
		t.Fatalf("create session error: %v", err)
	}
	if err := store.DB.Create(&model.Message{
		MsgID:      7201,
		SessionID:  sessionID,
		SenderID:   6102,
		SenderType: 1,
		MsgType:    1,
		Content:    "group hello",
	}).Error; err != nil {
		t.Fatalf("create message error: %v", err)
	}
	if err := store.DB.Create(&model.UserInbox{
		UserID: 6101, InboxSeq: 10, MsgID: 7201, SessionID: sessionID,
	}).Error; err != nil {
		t.Fatalf("create user_inbox error: %v", err)
	}
	members := []model.SessionMember{
		{SessionID: sessionID, MemberID: 6101, MemberType: 1},
		{SessionID: sessionID, MemberID: 6102, MemberType: 1},
	}
	for _, member := range members {
		if err := store.DB.Create(&member).Error; err != nil {
			t.Fatalf("create session member error: %v", err)
		}
	}

	conn := &sendMsgMockConn{userID: 6101, deviceID: "dev-pull-group"}
	HandlePullSync(nil, conn, makePullSyncPacket(t, 9))

	resp, ok := conn.sent[0].payload.(protocol.PullSyncRespPayload)
	if !ok {
		t.Fatalf("payload type mismatch: got=%T", conn.sent[0].payload)
	}
	if len(resp.Messages) != 1 {
		t.Fatalf("expected 1 message, got=%d", len(resp.Messages))
	}
	if got := resp.Messages[0].SessionMembers; len(got) != 0 {
		t.Fatalf("group message must not carry session members, got=%+v", got)
	}

	// omitempty: 群聊消息序列化后不能出现 session_members 字段。
	raw, err := json.Marshal(resp.Messages[0])
	if err != nil {
		t.Fatalf("marshal message error: %v", err)
	}
	if bytes.Contains(raw, []byte("session_members")) {
		t.Fatalf("group message payload must omit session_members: %s", raw)
	}
}
