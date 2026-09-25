package service

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/store"
)

// TestSessionListWithSyncHeadAttachesRecentMessages 验证 sync_head=1 的 bootstrap
// 响应为会话附带完整消息 body（含首条），按 msg_id DESC 排列，且排除已删除消息与
// msg_type=4 流式占位；不带 sync_head 的普通列表不附带。
func TestSessionListWithSyncHeadAttachesRecentMessages(t *testing.T) {
	_, cleanup := setupSessionTest(t)
	defer cleanup()

	now := time.Now().UTC()
	const selfID = int64(31001)
	const peerID = int64(31002)
	seedPrivateSessionForLastMsg(t, "s_boot_1", selfID, peerID, "summary", now)

	longBody := strings.Repeat("bootstrap full body ", 8) // 远超 60 字符摘要长度
	seedMessage(t, 101, "s_boot_1", peerID, 1, "first message of a fresh session", now.Add(-3*time.Minute))
	seedMessage(t, 102, "s_boot_1", selfID, 1, longBody, now.Add(-2*time.Minute))
	seedMessage(t, 103, "s_boot_1", peerID, 1, "deleted message", now.Add(-90*time.Second))
	seedMessage(t, 104, "s_boot_1", peerID, model.MsgTypeAIStream, "stream placeholder", now.Add(-time.Minute))
	if err := store.DB.Model(&model.Message{}).
		Where("msg_id = ? AND session_id = ?", 103, "s_boot_1").
		Update("is_deleted", true).Error; err != nil {
		t.Fatalf("soft delete message error: %v", err)
	}

	resp, err := SessionListWithSyncHead(selfID, 20, 0)
	if err != nil {
		t.Fatalf("SessionListWithSyncHead() error = %v", err)
	}
	if len(resp.List) != 1 {
		t.Fatalf("expected 1 session, got %d", len(resp.List))
	}
	got := resp.List[0].RecentMessages
	if len(got) != 2 {
		t.Fatalf("recent_messages len = %d, want 2 (deleted/stream excluded): %#v", len(got), got)
	}
	if got[0].MsgID != 102 || got[1].MsgID != 101 {
		t.Fatalf("recent_messages order = [%d %d], want msg_id DESC [102 101]", got[0].MsgID, got[1].MsgID)
	}
	if got[0].Content != longBody {
		t.Fatalf("recent_messages[0] content truncated: len=%d, want full body len=%d", len(got[0].Content), len(longBody))
	}
	if got[1].Content != "first message of a fresh session" {
		t.Fatalf("recent_messages[1] content = %q, want first message body", got[1].Content)
	}

	plain, err := SessionList(selfID, 20, 0)
	if err != nil {
		t.Fatalf("SessionList() error = %v", err)
	}
	if len(plain.List) != 1 || plain.List[0].RecentMessages != nil {
		t.Fatalf("non-sync-head list must not attach recent_messages, got %#v", plain.List)
	}
}

// TestBootstrapRecentMessagesRespectHistoryCutoff 验证 cutoff(session_history_resets)
// 之前的消息不进入 recent_messages，口径与聊天历史一致。
func TestBootstrapRecentMessagesRespectHistoryCutoff(t *testing.T) {
	_, cleanup := setupSessionTest(t)
	defer cleanup()

	now := time.Now().UTC()
	const selfID = int64(31011)
	const peerID = int64(31012)
	seedPrivateSessionForLastMsg(t, "s_boot_2", selfID, peerID, "summary", now)
	seedMessage(t, 201, "s_boot_2", peerID, 1, "before cutoff", now.Add(-10*time.Minute))
	seedMessage(t, 202, "s_boot_2", peerID, 1, "after cutoff", now.Add(-30*time.Second))
	if err := store.DB.Create(&model.SessionHistoryReset{
		SessionID:     "s_boot_2",
		UserID:        selfID,
		DeletedBefore: now.Add(-time.Minute),
	}).Error; err != nil {
		t.Fatalf("seed history reset error: %v", err)
	}

	resp, err := SessionListWithSyncHead(selfID, 20, 0)
	if err != nil {
		t.Fatalf("SessionListWithSyncHead() error = %v", err)
	}
	if len(resp.List) != 1 {
		t.Fatalf("expected 1 session, got %d", len(resp.List))
	}
	got := resp.List[0].RecentMessages
	if len(got) != 1 || got[0].MsgID != 202 {
		t.Fatalf("recent_messages = %#v, want only msg 202 (after cutoff)", got)
	}
}

// TestBootstrapRecentMessagesPerSessionCap 验证每会话最多附带
// bootstrapRecentMessagesPerSession 条，且保留最新的一批。
func TestBootstrapRecentMessagesPerSessionCap(t *testing.T) {
	_, cleanup := setupSessionTest(t)
	defer cleanup()

	now := time.Now().UTC()
	const selfID = int64(31021)
	const peerID = int64(31022)
	seedPrivateSessionForLastMsg(t, "s_boot_3", selfID, peerID, "summary", now)
	const total = bootstrapRecentMessagesPerSession + 5
	for i := 1; i <= total; i++ {
		seedMessage(t, int64(300+i), "s_boot_3", peerID, 1, fmt.Sprintf("msg %d", i), now.Add(time.Duration(i)*time.Second))
	}

	resp, err := SessionListWithSyncHead(selfID, 20, 0)
	if err != nil {
		t.Fatalf("SessionListWithSyncHead() error = %v", err)
	}
	if len(resp.List) != 1 {
		t.Fatalf("expected 1 session, got %d", len(resp.List))
	}
	got := resp.List[0].RecentMessages
	if len(got) != bootstrapRecentMessagesPerSession {
		t.Fatalf("recent_messages len = %d, want cap %d", len(got), bootstrapRecentMessagesPerSession)
	}
	if got[0].MsgID != int64(300+total) {
		t.Fatalf("recent_messages[0] msg_id = %d, want newest %d", got[0].MsgID, 300+total)
	}
	if got[len(got)-1].MsgID != int64(300+total-bootstrapRecentMessagesPerSession+1) {
		t.Fatalf("recent_messages last msg_id = %d, want %d",
			got[len(got)-1].MsgID, 300+total-bootstrapRecentMessagesPerSession+1)
	}
}

// TestBootstrapRecentMessagesSessionCap 验证只有列表前
// bootstrapRecentMessagesSessionLimit 个会话附带 recent_messages。
func TestBootstrapRecentMessagesSessionCap(t *testing.T) {
	_, cleanup := setupSessionTest(t)
	defer cleanup()

	now := time.Now().UTC()
	const selfID = int64(31031)
	const total = bootstrapRecentMessagesSessionLimit + 1
	for i := 0; i < total; i++ {
		sid := fmt.Sprintf("s_cap_%02d", i)
		activeAt := now.Add(-time.Duration(i) * time.Minute)
		if err := store.DB.Create(&model.Session{
			SessionID:   sid,
			SessionType: 1,
			UpdatedAt:   activeAt,
			CreatedAt:   activeAt,
		}).Error; err != nil {
			t.Fatalf("seed session %s error: %v", sid, err)
		}
		if err := store.DB.Create(&model.SessionMember{
			SessionID:    sid,
			MemberID:     selfID,
			MemberType:   1,
			JoinedAt:     activeAt,
			LastActiveAt: activeAt,
		}).Error; err != nil {
			t.Fatalf("seed member %s error: %v", sid, err)
		}
		seedMessage(t, int64(400+i), sid, selfID, 1, "cap probe", activeAt)
	}

	resp, err := SessionListWithSyncHead(selfID, 50, 0)
	if err != nil {
		t.Fatalf("SessionListWithSyncHead() error = %v", err)
	}
	if len(resp.List) != total {
		t.Fatalf("expected %d sessions, got %d", total, len(resp.List))
	}
	for i, item := range resp.List {
		if i < bootstrapRecentMessagesSessionLimit {
			if len(item.RecentMessages) != 1 {
				t.Fatalf("item %d (%s) recent_messages len = %d, want 1", i, item.SessionID, len(item.RecentMessages))
			}
			continue
		}
		if item.RecentMessages != nil {
			t.Fatalf("item %d (%s) beyond session cap must not attach recent_messages", i, item.SessionID)
		}
	}
}

// TestSessionListWithSyncHeadDegradesWhenAttachFails 验证附加查询失败时 bootstrap
// 仍成功返回会话列表（recent_messages 缺省），与端侧 best-effort 吞错对齐，
// 避免临时故障把首连打成 5xx 重连循环。
func TestSessionListWithSyncHeadDegradesWhenAttachFails(t *testing.T) {
	_, cleanup := setupSessionTest(t)
	defer cleanup()

	now := time.Now().UTC()
	const selfID = int64(31051)
	const peerID = int64(31052)
	seedPrivateSessionForLastMsg(t, "s_boot_err", selfID, peerID, "summary", now)
	seedMessage(t, 501, "s_boot_err", peerID, 1, "hello", now.Add(-time.Minute))

	prev := bootstrapRecentMessagesAttacher
	bootstrapRecentMessagesAttacher = func(userID int64, list []SessionItem) error {
		return errors.New("injected attach failure")
	}
	defer func() { bootstrapRecentMessagesAttacher = prev }()

	resp, err := SessionListWithSyncHead(selfID, 20, 0)
	if err != nil {
		t.Fatalf("SessionListWithSyncHead() must not fail when attach fails, got %v", err)
	}
	if len(resp.List) != 1 {
		t.Fatalf("expected 1 session, got %d", len(resp.List))
	}
	if resp.List[0].RecentMessages != nil {
		t.Fatalf("recent_messages must stay empty on attach failure, got %#v", resp.List[0].RecentMessages)
	}
}

// TestBootstrapRecentMessagesSQLDialects 锁定方言差异：visible_to 过滤仅 PostgreSQL 追加，
// 与 buildVisibleSessionMessageQuery 的方言处理一致。
func TestBootstrapRecentMessagesSQLDialects(t *testing.T) {
	pgSQL, pgArgs := bootstrapRecentMessagesSQL(true, 31041, []string{"s_boot_x"})
	if !strings.Contains(pgSQL, "m.visible_to @> to_jsonb(?::bigint)") {
		t.Fatalf("postgres SQL missing visible_to filter:\n%s", pgSQL)
	}
	if len(pgArgs) != 8 {
		t.Fatalf("postgres SQL args len = %d, want 8", len(pgArgs))
	}

	mySQL, myArgs := bootstrapRecentMessagesSQL(false, 31041, []string{"s_boot_x"})
	if strings.Contains(mySQL, "visible_to") {
		t.Fatalf("non-postgres SQL must not filter visible_to:\n%s", mySQL)
	}
	if len(myArgs) != 6 {
		t.Fatalf("non-postgres SQL args len = %d, want 6", len(myArgs))
	}
	for _, want := range []string{"ROW_NUMBER()", "PARTITION BY m.session_id", "ORDER BY m.msg_id DESC", "ranked.rn <= ?"} {
		if !strings.Contains(mySQL, want) {
			t.Fatalf("SQL missing %q:\n%s", want, mySQL)
		}
	}
}
