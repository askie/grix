package service

import (
	"strings"
	"testing"
	"time"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/testutil"
	"github.com/askie/grix/backend/internal/store"
)

// seedPrivateSessionForLastMsg 建一个私聊会话 + 自己/对端成员 + 对端用户，
// 并写入一个“服务端冗余摘要”(LastMsgSummary)，用于验证 last_msg 是否被
// per-viewer 可见消息覆盖。
func seedPrivateSessionForLastMsg(t *testing.T, sid string, selfID, peerID int64, snapshotSummary string, now time.Time) {
	t.Helper()
	if err := store.DB.Create(&model.Session{
		SessionID:      sid,
		SessionType:    1,
		LastMsgSummary: snapshotSummary,
		UpdatedAt:      now,
		CreatedAt:      now,
	}).Error; err != nil {
		t.Fatalf("seed session error: %v", err)
	}
	if err := store.DB.Create(&model.SessionMember{SessionID: sid, MemberID: selfID, MemberType: 1, JoinedAt: now, LastActiveAt: now}).Error; err != nil {
		t.Fatalf("seed self member error: %v", err)
	}
	if err := store.DB.Create(&model.SessionMember{SessionID: sid, MemberID: peerID, MemberType: 1, JoinedAt: now, LastActiveAt: now}).Error; err != nil {
		t.Fatalf("seed peer member error: %v", err)
	}
	if err := store.DB.Create(&model.User{ID: peerID, Username: "peer-user", Email: "peer@example.com", Nickname: "Peer", PasswordHash: "x"}).Error; err != nil {
		t.Fatalf("seed peer user error: %v", err)
	}
}

func seedMessage(t *testing.T, msgID int64, sid string, senderID int64, msgType int16, content string, createdAt time.Time) {
	t.Helper()
	if err := store.DB.Create(&model.Message{
		MsgID:      msgID,
		SessionID:  sid,
		SenderID:   senderID,
		SenderType: 1,
		MsgType:    msgType,
		Content:    content,
		CreatedAt:  createdAt,
	}).Error; err != nil {
		t.Fatalf("seed message %d error: %v", msgID, err)
	}
}

func lastItemOf(t *testing.T, selfID int64, sid string, now time.Time) SessionItem {
	t.Helper()
	items, err := buildSessionItems(selfID, []model.SessionMember{{SessionID: sid, MemberID: selfID, MemberType: 1, LastActiveAt: now}})
	if err != nil {
		t.Fatalf("buildSessionItems() error = %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("items len=%d, want 1", len(items))
	}
	return items[0]
}

func lastMsgOf(t *testing.T, selfID int64, sid string, now time.Time) string {
	t.Helper()
	return lastItemOf(t, selfID, sid, now).LastMsg
}

func TestBuildSessionItemsLastMsgFollowsVisibleHistory(t *testing.T) {
	tdb := testutil.NewTestDB()
	defer tdb.Close()
	store.DB = tdb.DB

	now := time.Now().UTC()
	seedPrivateSessionForLastMsg(t, "s_lm_1", 1001, 2001, "stale snapshot summary", now)
	seedMessage(t, 100, "s_lm_1", 2001, 1, "older message", now.Add(-2*time.Minute))
	seedMessage(t, 200, "s_lm_1", 1001, 1, "newest visible message", now.Add(-1*time.Minute))

	if got := lastMsgOf(t, 1001, "s_lm_1", now); got != "newest visible message" {
		t.Fatalf("last_msg = %q, want %q", got, "newest visible message")
	}
}

func TestBuildSessionItemsLastMsgEmptyWhenAllBeforeCutoff(t *testing.T) {
	tdb := testutil.NewTestDB()
	defer tdb.Close()
	store.DB = tdb.DB

	now := time.Now().UTC()
	seedPrivateSessionForLastMsg(t, "s_lm_2", 1001, 2001, "stale snapshot summary", now)
	// 唯一消息落在 cutoff 之前：聊天页拉不到，会话列表摘要也应为空。
	seedMessage(t, 100, "s_lm_2", 2001, 1, "before cutoff message", now.Add(-10*time.Minute))
	if err := store.DB.Create(&model.SessionHistoryReset{
		SessionID:     "s_lm_2",
		UserID:        1001,
		DeletedBefore: now.Add(-1 * time.Minute),
	}).Error; err != nil {
		t.Fatalf("seed history reset error: %v", err)
	}

	if got := lastMsgOf(t, 1001, "s_lm_2", now); got != "" {
		t.Fatalf("last_msg = %q, want empty (all messages before cutoff)", got)
	}
}

// TestBuildSessionItemsLastMsgTimeFollowsVisibleMessage 复现反馈的 bug：
// 列表时间必须等于「最后一条可见消息」的时间，而不是被后台活动（msg_type=4 流式占位）
// 顶起来的会话活跃时间。这里的占位消息比正文晚 5 分钟，会把会话活跃时间往前顶，
// 但 last_msg_time 必须仍指向那条可见正文，与用户点进去看到的最后一条对齐。
func TestBuildSessionItemsLastMsgTimeFollowsVisibleMessage(t *testing.T) {
	tdb := testutil.NewTestDB()
	defer tdb.Close()
	store.DB = tdb.DB

	now := time.Now().UTC()
	seedPrivateSessionForLastMsg(t, "s_lm_t1", 1001, 2001, "stale snapshot summary", now)
	visibleAt := now.Add(-5 * time.Minute)
	seedMessage(t, 100, "s_lm_t1", 2001, 1, "last visible reply", visibleAt)
	// msg_type=4 占位晚 5 分钟，顶高活跃时间但不是可见消息。
	seedMessage(t, 200, "s_lm_t1", 2001, model.MsgTypeAIStream, "", now)

	item := lastItemOf(t, 1001, "s_lm_t1", now)
	if item.LastMsg != "last visible reply" {
		t.Fatalf("last_msg = %q, want %q", item.LastMsg, "last visible reply")
	}
	if item.LastMsgTime != visibleAt.Unix() {
		t.Fatalf("last_msg_time = %d, want %d (visible message time, not bumped activity)", item.LastMsgTime, visibleAt.Unix())
	}
}

// TestBuildSessionItemsLastMsgTimeZeroWhenNoVisible 无可见消息时 last_msg_time 应为 0，
// 前端据此回退到活跃时间展示。
func TestBuildSessionItemsLastMsgTimeZeroWhenNoVisible(t *testing.T) {
	tdb := testutil.NewTestDB()
	defer tdb.Close()
	store.DB = tdb.DB

	now := time.Now().UTC()
	seedPrivateSessionForLastMsg(t, "s_lm_t2", 1001, 2001, "stale snapshot summary", now)
	seedMessage(t, 100, "s_lm_t2", 2001, 1, "before cutoff", now.Add(-10*time.Minute))
	if err := store.DB.Create(&model.SessionHistoryReset{
		SessionID:     "s_lm_t2",
		UserID:        1001,
		DeletedBefore: now.Add(-1 * time.Minute),
	}).Error; err != nil {
		t.Fatalf("seed history reset error: %v", err)
	}

	item := lastItemOf(t, 1001, "s_lm_t2", now)
	if item.LastMsg != "" || item.LastMsgTime != 0 {
		t.Fatalf("want empty last_msg and 0 last_msg_time, got %q / %d", item.LastMsg, item.LastMsgTime)
	}
}

func TestBuildSessionItemsLastMsgSkipsStreamingPlaceholder(t *testing.T) {
	tdb := testutil.NewTestDB()
	defer tdb.Close()
	store.DB = tdb.DB

	now := time.Now().UTC()
	seedPrivateSessionForLastMsg(t, "s_lm_3", 1001, 2001, "stale snapshot summary", now)
	seedMessage(t, 100, "s_lm_3", 2001, 1, "finalized reply", now.Add(-2*time.Minute))
	// msg_type=4 为流式占位（content 为空），不应作为 last_msg。
	seedMessage(t, 200, "s_lm_3", 2001, model.MsgTypeAIStream, "", now.Add(-1*time.Minute))

	if got := lastMsgOf(t, 1001, "s_lm_3", now); got != "finalized reply" {
		t.Fatalf("last_msg = %q, want %q", got, "finalized reply")
	}
}

func TestVisibleLastMsgSummaryPostgresSQLUsesLateralLimit(t *testing.T) {
	sql, args := visibleLastMsgSummarySQLForDialect(true, 1001, []string{"s_lm_1", "s_lm_2"})
	for _, want := range []string{
		"JOIN LATERAL",
		"ORDER BY m.msg_id DESC",
		"LIMIT 1",
		"m.visible_to @> to_jsonb(?::bigint)",
	} {
		if !strings.Contains(sql, want) {
			t.Fatalf("postgres SQL missing %q:\n%s", want, sql)
		}
	}
	if strings.Contains(sql, "ROW_NUMBER()") {
		t.Fatalf("postgres SQL should not use window ranking:\n%s", sql)
	}
	if len(args) != 7 {
		t.Fatalf("postgres SQL args len=%d, want 7", len(args))
	}
}
