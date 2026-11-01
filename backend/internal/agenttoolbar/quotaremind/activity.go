package quotaremind

import (
	"context"
	"strings"
	"time"

	appstore "github.com/askie/grix/backend/internal/store"
)

const (
	// defaultActiveWithin 冷会话门槛：窗口内若没有任何「真实对话」消息，
	// 则不发送限额交接提醒，避免重连/用量回执把提醒打进几天前的旧会话。
	defaultActiveWithin = 24 * time.Hour

	// quotaRemindContentZH / quotaRemindContentEN 用于排除历史提醒本身，
	// 避免「只有提醒在滚动」被当成会话仍活跃。
	quotaRemindContentZH = "%用量限额只剩不到%"
	quotaRemindContentEN = "%usage limit has less than%"
)

// SessionActivity 判断会话是否仍有近期真实对话（非限额提醒）。
type SessionActivity interface {
	HasRecentConversation(ctx context.Context, sessionID string, within time.Duration) (bool, error)
}

// DBSessionActivity 基于 messages 表的活跃度检查。
type DBSessionActivity struct{}

// NewDBSessionActivity 返回默认的 DB 实现。
func NewDBSessionActivity() SessionActivity {
	return DBSessionActivity{}
}

// HasRecentConversation 在 within 窗口内是否存在非删除、非撤回、且不是限额提醒的消息。
// store.DB 未初始化时返回 false（失败关闭：不发提醒）。
func (DBSessionActivity) HasRecentConversation(ctx context.Context, sessionID string, within time.Duration) (bool, error) {
	sessionID = strings.TrimSpace(sessionID)
	if appstore.DB == nil || sessionID == "" {
		return false, nil
	}
	if within <= 0 {
		within = defaultActiveWithin
	}
	if ctx == nil {
		ctx = context.Background()
	}
	cutoff := time.Now().Add(-within)
	var exists bool
	err := appstore.DB.WithContext(ctx).Raw(`
SELECT EXISTS (
	SELECT 1
	FROM messages
	WHERE session_id = ?
	  AND created_at >= ?
	  AND is_deleted = false
	  AND is_revoked = false
	  AND content NOT LIKE ?
	  AND content NOT LIKE ?
)
`, sessionID, cutoff, quotaRemindContentZH, quotaRemindContentEN).Scan(&exists).Error
	if err != nil {
		return false, err
	}
	return exists, nil
}
