package service

import (
	"strings"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/store"
)

const (
	// bootstrapRecentMessagesSessionLimit 限定 sync v2 bootstrap 只为列表前 N 个
	// （按会话列表既有排序：置顶优先、其余按 last_active_at DESC）会话附带最近
	// 消息，控制每设备首连 bootstrap 的响应体积，不要再扩大。
	bootstrapRecentMessagesSessionLimit = 20
	// bootstrapRecentMessagesPerSession 为每个会话附带的最大最近消息条数。
	bootstrapRecentMessagesPerSession = 30
)

// bootstrapRecentMessagesAttacher 是测试可替换的挂载入口（同 sessionMemberAddedOfflinePushRunner 惯例）。
var bootstrapRecentMessagesAttacher = attachBootstrapRecentMessages

// attachBootstrapRecentMessages 为 sync v2 bootstrap（sync_head=1）的会话列表前
// bootstrapRecentMessagesSessionLimit 个条目各附最新 bootstrapRecentMessagesPerSession 条
// 完整消息。端侧把 durable 游标直接跳到 head 后，head 之前的 message.upsert 不再回放；
// 快照里 last_msg 只有 60 字符摘要，附上完整 body 落库后新设备本地渲染才不缺
// 快照覆盖范围内的历史消息（如只有几条消息的新会话首条）。
func attachBootstrapRecentMessages(userID int64, list []SessionItem) error {
	head := list
	if len(head) > bootstrapRecentMessagesSessionLimit {
		head = head[:bootstrapRecentMessagesSessionLimit]
	}
	if len(head) == 0 {
		return nil
	}
	sessionIDs := make([]string, 0, len(head))
	for i := range head {
		sessionIDs = append(sessionIDs, head[i].SessionID)
	}
	recentMap, err := loadBootstrapRecentMessages(userID, sessionIDs)
	if err != nil {
		return err
	}
	for i := range head {
		head[i].RecentMessages = recentMap[head[i].SessionID]
	}
	return nil
}

// loadBootstrapRecentMessages 一次 SQL 取回多个会话各自最新的可见消息（窗口函数按
// session_id 分区、msg_id DESC 取前 bootstrapRecentMessagesPerSession 条），无 N+1。
// 可见性口径与聊天历史 buildVisibleSessionMessageQuery 完全一致：排除已删除消息与
// msg_type=4 流式占位，套用 per-user 历史 cutoff(session_history_resets) 与群成员
// joined_at 截断，并仅在 PostgreSQL 下套用 visible_to 过滤。无可见消息的会话不出现在
// 结果中，调用方据此把 recent_messages 留空（omitempty）。
func loadBootstrapRecentMessages(userID int64, sessionIDs []string) (map[string][]model.Message, error) {
	result := make(map[string][]model.Message, len(sessionIDs))
	if userID <= 0 || len(sessionIDs) == 0 {
		return result, nil
	}

	sql, args := bootstrapRecentMessagesSQL(store.IsPostgres(), userID, sessionIDs)

	var rows []model.Message
	if err := store.DB.Raw(sql, args...).Scan(&rows).Error; err != nil {
		return nil, err
	}
	// 出口重签媒体 URL，与 /messages/history 的 egress 处理（signMessagePage）一致。
	signMessagePage(rows)
	for _, row := range rows {
		sid := strings.TrimSpace(row.SessionID)
		if sid == "" {
			continue
		}
		result[sid] = append(result[sid], row)
	}
	return result, nil
}

// bootstrapRecentMessagesSQL 单一窗口函数 SQL 同时覆盖 MySQL/PostgreSQL/SQLite
// （同 loadVisibleLastMsgSummaryMap 的非 Postgres 分支写法），仅 visible_to 过滤
// 按方言条件追加——与 buildVisibleSessionMessageQuery 的方言处理一致。
// 外层 SELECT ranked.* 会带出 rn 列，gorm Scan 对无对应字段的列扫入占位丢弃，不影响映射。
func bootstrapRecentMessagesSQL(postgres bool, userID int64, sessionIDs []string) (string, []interface{}) {
	args := []interface{}{userID, userID, sessionIDs, model.MsgTypeAIStream, model.SessionTypeGroup}
	visibleToFilter := ""
	if postgres {
		visibleToFilter = `
      AND (m.visible_to IS NULL OR m.sender_id = ? OR m.visible_to @> to_jsonb(?::bigint))`
		args = append(args, userID, userID)
	}
	args = append(args, bootstrapRecentMessagesPerSession)
	return `
SELECT ranked.*
FROM (
    SELECT
        m.*,
        ROW_NUMBER() OVER (
            PARTITION BY m.session_id
            ORDER BY m.msg_id DESC
        ) AS rn
    FROM messages m
    LEFT JOIN session_history_resets r
        ON r.session_id = m.session_id AND r.user_id = ?
    JOIN session_members me
        ON me.session_id = m.session_id AND me.member_id = ? AND me.member_type = 1
    JOIN sessions s
        ON s.session_id = m.session_id
    WHERE m.session_id IN ?
      AND m.is_deleted = false
      AND m.msg_type <> ?
      AND (r.deleted_before IS NULL OR m.created_at > r.deleted_before)
      AND (s.session_type <> ? OR m.created_at >= me.joined_at)` + visibleToFilter + `
) AS ranked
WHERE ranked.rn <= ?
ORDER BY ranked.session_id ASC, ranked.msg_id DESC`, args
}
