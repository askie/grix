package store

// VisibleAfterCutoffExistsSQL 返回判定「会话在 per-user 历史 cutoff 之后仍存在可见消息」
// 的 SQL 片段及参数，供会话列表(SessionConversations)与未读快照(pull_sync)共用同一口径，
// 杜绝底部角标与会话列表分叉。
//
// 口径与聊天页历史 (buildVisibleSessionMessageQuery) / loadVisibleLastMsgSummaryMap 完全一致：
// 排除已删除消息与 msg_type=4 流式占位，套用 cutoff(created_at > deleted_before)，
// 群聊再叠加 joined_at（入群前消息不可见），并在 PostgreSQL 下套用 visible_to 过滤。
//
// 入参：
//   - sessionCol：外层会话 id 列引用（如 "me.session_id"）。
//   - shrDeletedBeforeCol：外层已 join 的 session_history_resets.deleted_before 列引用
//     （如 "shr.deleted_before"）。仅在外层 shr 记录存在（即调用方以
//     `shr.session_id IS NULL OR <本片段>` 组合）时求值，故此处 deleted_before 必非空。
//   - joinedAtCol：外层已 join 的 session_members.joined_at 列引用（如 "me.joined_at"）。
//   - sessionTypeCol：外层已 join 的 sessions.session_type 列引用（如 "s.session_type"）。
//   - userID：查看者用户 id（用于 visible_to 过滤）。
//
// 这几个列名均为代码内固定字符串、非用户输入，直接拼接无注入风险；userID 走参数占位符。
func VisibleAfterCutoffExistsSQL(sessionCol, shrDeletedBeforeCol, joinedAtCol, sessionTypeCol string, userID int64) (string, []interface{}) {
	args := []interface{}{}
	visibility := ""
	if IsPostgres() {
		visibility = " AND (m.visible_to IS NULL OR m.sender_id = ? OR m.visible_to @> to_jsonb(?::bigint))"
	}
	sql := "EXISTS (SELECT 1 FROM messages m WHERE m.session_id = " + sessionCol +
		" AND m.is_deleted = false AND m.msg_type <> 4 AND m.created_at > " + shrDeletedBeforeCol +
		" AND (" + sessionTypeCol + " <> 2 OR m.created_at >= " + joinedAtCol + ")" +
		visibility + ")"
	if IsPostgres() {
		args = append(args, userID, userID)
	}
	return sql, args
}
