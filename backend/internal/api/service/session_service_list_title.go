package service

import (
	"fmt"
	"strings"
	"time"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/textutil"
	"github.com/askie/grix/backend/internal/store"
)

type visitorSessionMeta struct {
	VisitorID    int64
	VisitorName  string
	VisitorEmail string
}

func buildSessionItems(userID int64, members []model.SessionMember) ([]SessionItem, error) {
	if len(members) == 0 {
		return []SessionItem{}, nil
	}

	sessionIDs := make([]string, 0, len(members))
	unreadMap := make(map[string]int, len(members))
	pinnedMap := make(map[string]bool, len(members))
	pinnedAtMap := make(map[string]int64, len(members))
	mutedMap := make(map[string]bool, len(members))
	for _, m := range members {
		sid := strings.TrimSpace(m.SessionID)
		if sid == "" {
			continue
		}
		sessionIDs = append(sessionIDs, sid)
		unreadMap[sid] = m.UnreadCount
		pinnedMap[sid] = m.IsPinned
		mutedMap[sid] = m.IsMuted
		if m.PinnedAt != nil {
			pinnedAtMap[sid] = m.PinnedAt.Unix()
		}
	}
	if len(sessionIDs) == 0 {
		return []SessionItem{}, nil
	}

	var sessions []model.Session
	if err := store.DB.
		Where(
			"session_id IN ? AND is_deleted = false AND (session_type <> ? OR moderation_status = ?)",
			sessionIDs,
			model.SessionTypeGroup,
			model.SessionModerationStatusActive,
		).
		Find(&sessions).Error; err != nil {
		return nil, err
	}
	sessionMap := make(map[string]model.Session, len(sessions))
	privateIDs := make([]string, 0, len(sessions))
	for _, s := range sessions {
		sessionMap[s.SessionID] = s
		if s.SessionType == 1 {
			privateIDs = append(privateIDs, s.SessionID)
		}
	}

	peerBySession, err := loadPrivatePeers(userID, privateIDs)
	if err != nil {
		return nil, err
	}
	userDisplayNameMap, userUsernameMap, agentNameMap, err := loadPeerNames(userID, peerBySession)
	if err != nil {
		return nil, err
	}

	friendPinMap, err := loadFriendPinMap(userID, peerBySession)
	if err != nil {
		return nil, err
	}

	// loadFirstMessageTitleMap 已不再需要，custom_title 在首条消息时自动写入。
	firstMessageTitleMap := map[string]string{}
	// 访客会话集合与详情同源（widget_sessions 同一过滤条件），一次查询取详情，
	// 是否访客直接由该会话是否命中详情派生，避免对同表同条件查两遍。
	visitorMetaBySession, err := loadVisitorSessionMetaMap(sessionIDs)
	if err != nil {
		return nil, err
	}
	visitorSessionSet := make(map[string]bool, len(visitorMetaBySession))
	for sid := range visitorMetaBySession {
		visitorSessionSet[sid] = true
	}
	// 按 viewer 计算每个会话真正可见的最后一条消息摘要，口径与聊天历史一致，
	// 避免会话列表展示该用户在聊天页打不开的消息（cutoff 之前 / 不在 visible_to）。
	visibleLastMsgMap, err := loadVisibleLastMsgSummaryMap(userID, sessionIDs)
	if err != nil {
		return nil, err
	}

	list := make([]SessionItem, 0, len(members))
	for _, m := range members {
		s, ok := sessionMap[m.SessionID]
		if !ok {
			continue
		}

		title := resolveSessionTitle(
			s,
			m.CustomTitle,
			peerBySession[m.SessionID],
			userDisplayNameMap,
			agentNameMap,
			firstMessageTitleMap[m.SessionID],
		)
		visibleLast := visibleLastMsgMap[s.SessionID]
		item := SessionItem{
			SessionID:   s.SessionID,
			Title:       title,
			SessionType: s.SessionType,
			IsVisitor:   visitorSessionSet[s.SessionID],
			LastMsg:     visibleLast.Summary,
			LastMsgTime: visibleLast.CreatedAt,
			Unread:      unreadMap[s.SessionID],
			UpdatedAt:   s.UpdatedAt.Unix(),
			IsPinned:    pinnedMap[s.SessionID],
			PinnedAt:    pinnedAtMap[s.SessionID],
			IsMuted:     mutedMap[s.SessionID],
		}

		if s.SessionType == 1 {
			if item.IsVisitor {
				if meta, ok := visitorMetaBySession[s.SessionID]; ok {
					peerName := strings.TrimSpace(meta.VisitorName)
					if peerName == "" {
						peerName = strings.TrimSpace(meta.VisitorEmail)
					}
					item.Peer = &SessionPeer{
						ID:       meta.VisitorID,
						Type:     1,
						Nickname: peerName,
						Username: strings.TrimSpace(meta.VisitorEmail),
					}
				}
			}
			if peer, ok := peerBySession[s.SessionID]; ok {
				if item.Peer == nil {
					item.Peer = &SessionPeer{
						ID:       peer.MemberID,
						Type:     peer.MemberType,
						Nickname: resolvePrivatePeerName(peer, userDisplayNameMap, agentNameMap),
						Username: resolvePrivatePeerUsername(peer, userUsernameMap, userDisplayNameMap),
					}
				}
				if fp, ok := friendPinMap[peer.MemberID]; ok {
					item.FriendIsPinned = fp.IsPinned
					item.FriendPinnedAt = fp.PinnedAt
				}
			}
		}

		list = append(list, item)
	}
	return list, nil
}

func loadVisitorSessionMetaMap(sessionIDs []string) (map[string]visitorSessionMeta, error) {
	result := make(map[string]visitorSessionMeta, len(sessionIDs))
	if len(sessionIDs) == 0 {
		return result, nil
	}
	var rows []struct {
		SessionID    string `gorm:"column:session_id"`
		VisitorID    int64  `gorm:"column:visitor_id"`
		VisitorName  string `gorm:"column:visitor_name"`
		VisitorEmail string `gorm:"column:visitor_email"`
	}
	if err := store.DB.Model(&model.WidgetSession{}).
		Select("session_id", "visitor_id", "visitor_name", "visitor_email").
		Where("session_id IN ? AND status IN ?", sessionIDs, []int16{model.WidgetSessionStatusActive, model.WidgetSessionStatusClosed}).
		Find(&rows).Error; err != nil {
		return nil, err
	}
	for _, row := range rows {
		sid := strings.TrimSpace(row.SessionID)
		if sid == "" {
			continue
		}
		result[sid] = visitorSessionMeta{
			VisitorID:    row.VisitorID,
			VisitorName:  strings.TrimSpace(row.VisitorName),
			VisitorEmail: strings.TrimSpace(row.VisitorEmail),
		}
	}
	return result, nil
}

func loadPrivatePeers(userID int64, privateSessionIDs []string) (map[string]model.SessionMember, error) {
	peerBySession := make(map[string]model.SessionMember, len(privateSessionIDs))
	if len(privateSessionIDs) == 0 {
		return peerBySession, nil
	}

	var rows []model.SessionMember
	if err := store.DB.
		Select("session_id", "member_id", "member_type", "joined_at").
		Where(
			"session_id IN ? AND NOT (member_type = 1 AND member_id = ?)",
			privateSessionIDs,
			userID,
		).
		Order("joined_at ASC").
		Find(&rows).Error; err != nil {
		return nil, err
	}

	for _, row := range rows {
		if _, exists := peerBySession[row.SessionID]; exists {
			continue
		}
		peerBySession[row.SessionID] = row
	}
	return peerBySession, nil
}

func loadPeerNames(
	viewerUserID int64,
	peerBySession map[string]model.SessionMember,
) (map[int64]string, map[int64]string, map[int64]string, error) {
	userIDs := make([]int64, 0, len(peerBySession))
	agentIDs := make([]int64, 0, len(peerBySession))
	seenUsers := make(map[int64]struct{}, len(peerBySession))
	seenAgents := make(map[int64]struct{}, len(peerBySession))

	for _, peer := range peerBySession {
		if peer.MemberID <= 0 {
			continue
		}
		if peer.MemberType == 2 {
			if _, ok := seenAgents[peer.MemberID]; ok {
				continue
			}
			seenAgents[peer.MemberID] = struct{}{}
			agentIDs = append(agentIDs, peer.MemberID)
			continue
		}
		if _, ok := seenUsers[peer.MemberID]; ok {
			continue
		}
		seenUsers[peer.MemberID] = struct{}{}
		userIDs = append(userIDs, peer.MemberID)
	}

	userDisplayNameMap := make(map[int64]string, len(userIDs))
	userUsernameMap := make(map[int64]string, len(userIDs))
	if len(userIDs) > 0 {
		var users []model.User
		if err := store.DB.
			Select("id", "nickname", "username").
			Where("id IN ?", userIDs).
			Find(&users).Error; err != nil {
			return nil, nil, nil, err
		}
		for _, u := range users {
			username := strings.TrimSpace(u.Username)
			if username != "" {
				userUsernameMap[u.ID] = username
			}
			displayName := strings.TrimSpace(u.Nickname)
			if displayName == "" {
				displayName = username
			}
			if displayName != "" {
				userDisplayNameMap[u.ID] = displayName
			}
		}

		remarkNameMap, err := loadFriendRemarkNameMap(viewerUserID, userIDs)
		if err != nil {
			return nil, nil, nil, err
		}
		for uid, remarkName := range remarkNameMap {
			if remarkName == "" {
				continue
			}
			userDisplayNameMap[uid] = remarkName
		}
	}

	agentNameMap := make(map[int64]string, len(agentIDs))
	if len(agentIDs) > 0 {
		var agents []model.Agent
		if err := store.DB.
			Select("id", "agent_name").
			Where("id IN ?", agentIDs).
			Find(&agents).Error; err != nil {
			return nil, nil, nil, err
		}
		for _, a := range agents {
			name := strings.TrimSpace(a.AgentName)
			if name != "" {
				agentNameMap[a.ID] = name
			}
		}
	}

	return userDisplayNameMap, userUsernameMap, agentNameMap, nil
}

type friendRemarkRow struct {
	FriendID   int64  `gorm:"column:friend_id"`
	RemarkName string `gorm:"column:remark_name"`
}

type friendPinStatus struct {
	IsPinned bool
	PinnedAt int64
}

type userPeerPinRow struct {
	PeerUserID int64      `gorm:"column:peer_user_id"`
	IsPinned   bool       `gorm:"column:is_pinned"`
	PinnedAt   *time.Time `gorm:"column:pinned_at"`
}

func loadFriendPinMap(viewerUserID int64, peerBySession map[string]model.SessionMember) (map[int64]friendPinStatus, error) {
	result := make(map[int64]friendPinStatus)
	if viewerUserID <= 0 || len(peerBySession) == 0 {
		return result, nil
	}

	var peerIDs []int64
	seen := make(map[int64]struct{})
	for _, peer := range peerBySession {
		if peer.MemberID <= 0 {
			continue
		}
		// Collect all human (1) and agent (2) peers for peer-level pin lookup.
		if peer.MemberType != 1 && peer.MemberType != 2 {
			continue
		}
		if _, ok := seen[peer.MemberID]; ok {
			continue
		}
		seen[peer.MemberID] = struct{}{}
		peerIDs = append(peerIDs, peer.MemberID)
	}
	if len(peerIDs) == 0 {
		return result, nil
	}

	// user_peer_pins is the sole authority for peer-level pin state
	// (human friends, visitors, and agents).
	var peerPinRows []userPeerPinRow
	if err := store.DB.Table("user_peer_pins").
		Select("peer_user_id", "is_pinned", "pinned_at").
		Where("user_id = ? AND peer_user_id IN ?", viewerUserID, peerIDs).
		Scan(&peerPinRows).Error; err != nil {
		return nil, err
	}
	for _, row := range peerPinRows {
		ps := friendPinStatus{IsPinned: row.IsPinned}
		if row.PinnedAt != nil {
			ps.PinnedAt = row.PinnedAt.Unix()
		}
		result[row.PeerUserID] = ps
	}
	return result, nil
}

func loadFriendRemarkNameMap(viewerUserID int64, friendIDs []int64) (map[int64]string, error) {
	remarkMap := make(map[int64]string, len(friendIDs))
	if viewerUserID <= 0 || len(friendIDs) == 0 {
		return remarkMap, nil
	}

	var rows []friendRemarkRow
	if err := store.DB.Table("friends").
		Select("friend_id", "remark_name").
		Where("user_id = ? AND friend_id IN ? AND remark_name <> ''", viewerUserID, friendIDs).
		Scan(&rows).Error; err != nil {
		return nil, err
	}
	for _, row := range rows {
		remarkName := strings.TrimSpace(row.RemarkName)
		if row.FriendID <= 0 || remarkName == "" {
			continue
		}
		remarkMap[row.FriendID] = remarkName
	}
	return remarkMap, nil
}

type firstMessageTitleRow struct {
	SessionID string `gorm:"column:session_id"`
	Content   string `gorm:"column:content"`
}

func loadFirstMessageTitleMap(sessionIDs []string) (map[string]string, error) {
	titleMap := make(map[string]string, len(sessionIDs))
	if len(sessionIDs) == 0 {
		return titleMap, nil
	}

	var rows []firstMessageTitleRow
	if err := store.DB.Raw(`
SELECT ranked.session_id, ranked.content
FROM (
    SELECT
        session_id,
        content,
        ROW_NUMBER() OVER (
            PARTITION BY session_id
            ORDER BY created_at ASC, msg_id ASC
        ) AS rn
    FROM messages
    WHERE session_id IN ? AND is_deleted = false
) AS ranked
WHERE ranked.rn = 1
`, sessionIDs).Scan(&rows).Error; err != nil {
		return nil, err
	}

	for _, row := range rows {
		sid := strings.TrimSpace(row.SessionID)
		if sid == "" {
			continue
		}
		title := buildFallbackTitleFromMessage(row.Content)
		if title == "" {
			continue
		}
		titleMap[sid] = title
	}
	return titleMap, nil
}

func buildFallbackTitleFromMessage(raw string) string {
	compact := strings.Join(strings.Fields(strings.TrimSpace(raw)), " ")
	if compact == "" {
		return ""
	}
	return textutil.TruncateRunes(compact, sessionFallbackMaxRunes)
}

// BuildFallbackTitleFromMessage exports the internal helper for cross-package use.
func BuildFallbackTitleFromMessage(raw string) string {
	return buildFallbackTitleFromMessage(raw)
}

func resolveSessionTitle(
	session model.Session,
	customTitle string,
	peer model.SessionMember,
	userNameMap map[int64]string,
	agentNameMap map[int64]string,
	firstMessageTitle string,
) string {
	if title := strings.TrimSpace(customTitle); title != "" {
		return title
	}
	if session.SessionType == 2 {
		if name := strings.TrimSpace(session.GroupName); name != "" {
			return name
		}
		return session.SessionID
	}
	// 私聊：custom_title 为空时回退到 peer 显示名（仅用于尚未发送首条消息的新会话）。
	// firstMessageTitle 不再使用，因为 custom_title 会在首条消息时自动写入。
	if peerName := strings.TrimSpace(resolvePrivatePeerName(peer, userNameMap, agentNameMap)); peerName != "" {
		return peerName
	}
	return ""
}

func resolvePrivatePeerName(
	peer model.SessionMember,
	userNameMap map[int64]string,
	agentNameMap map[int64]string,
) string {
	if peer.MemberID <= 0 {
		return ""
	}
	if peer.MemberType == 2 {
		if name := strings.TrimSpace(agentNameMap[peer.MemberID]); name != "" {
			return name
		}
		return fmt.Sprintf("Agent %d", peer.MemberID)
	}
	if name := strings.TrimSpace(userNameMap[peer.MemberID]); name != "" {
		return name
	}
	return ""
}

func resolvePrivatePeerUsername(
	peer model.SessionMember,
	userUsernameMap map[int64]string,
	userDisplayNameMap map[int64]string,
) string {
	if peer.MemberType != 1 || peer.MemberID <= 0 {
		return ""
	}
	if username := strings.TrimSpace(userUsernameMap[peer.MemberID]); username != "" {
		return username
	}
	return strings.TrimSpace(userDisplayNameMap[peer.MemberID])
}

const sessionLastMsgSummaryMaxRunes = 60

// visibleLastMsg 是某会话对指定 viewer 真正可见的最后一条消息：既有摘要文本，也有它的时间。
// 会话列表那行的「时间」应展示这条消息的时间，才能与用户点进去看到的最后一条对齐——
// 而不是被工具卡片/流式占位/隐藏消息等后台活动顶起来的会话活跃时间。
type visibleLastMsg struct {
	Summary   string
	CreatedAt int64 // unix 秒；无可见消息时为 0
}

type visibleLastMsgRow struct {
	SessionID string    `gorm:"column:session_id"`
	Content   string    `gorm:"column:content"`
	CreatedAt time.Time `gorm:"column:created_at"`
}

// loadVisibleLastMsgSummaryMap 计算每个会话对指定 viewer 真正可见的最后一条消息（摘要+时间）。
// 可见性口径与聊天历史 /messages/history(buildVisibleSessionMessageQuery) 完全一致：
// 排除已删除消息与 msg_type=4 流式占位，套用 per-user 历史 cutoff(session_history_resets)，
// 并在 PostgreSQL 下套用 visible_to 过滤。无可见消息的会话不出现在结果中，调用方据此
// 把 last_msg 置空、last_msg_time 归零，保证会话列表摘要/时间与聊天页可展示内容一致。
func loadVisibleLastMsgSummaryMap(userID int64, sessionIDs []string) (map[string]visibleLastMsg, error) {
	result := make(map[string]visibleLastMsg, len(sessionIDs))
	if userID <= 0 || len(sessionIDs) == 0 {
		return result, nil
	}

	sql, args := visibleLastMsgSummarySQL(userID, sessionIDs)

	var rows []visibleLastMsgRow
	if err := store.DB.Raw(sql, args...).Scan(&rows).Error; err != nil {
		return nil, err
	}
	for _, row := range rows {
		sid := strings.TrimSpace(row.SessionID)
		if sid == "" {
			continue
		}
		var createdAt int64
		if !row.CreatedAt.IsZero() {
			createdAt = row.CreatedAt.Unix()
		}
		result[sid] = visibleLastMsg{
			Summary:   textutil.TruncateRunes(row.Content, sessionLastMsgSummaryMaxRunes),
			CreatedAt: createdAt,
		}
	}
	return result, nil
}

func visibleLastMsgSummarySQL(userID int64, sessionIDs []string) (string, []interface{}) {
	return visibleLastMsgSummarySQLForDialect(store.IsPostgres(), userID, sessionIDs)
}

func visibleLastMsgSummarySQLForDialect(postgres bool, userID int64, sessionIDs []string) (string, []interface{}) {
	if postgres {
		return `
SELECT req.session_id, last_msg.content, last_msg.created_at
FROM (
    SELECT me.session_id, me.joined_at
    FROM session_members me
    WHERE me.session_id IN ? AND me.member_id = ? AND me.member_type = 1
) AS req
JOIN sessions s
    ON s.session_id = req.session_id
LEFT JOIN session_history_resets r
    ON r.session_id = req.session_id AND r.user_id = ?
JOIN LATERAL (
    SELECT m.content, m.created_at, m.msg_id
    FROM messages m
    WHERE m.session_id = req.session_id
      AND m.is_deleted = false
      AND m.msg_type <> ?
      AND m.content NOT LIKE '%](grix://card/%'
      AND (r.deleted_before IS NULL OR m.created_at > r.deleted_before)
      AND (s.session_type <> ? OR m.created_at >= req.joined_at)
      AND (m.visible_to IS NULL OR m.sender_id = ? OR m.visible_to @> to_jsonb(?::bigint))
    ORDER BY m.msg_id DESC
    LIMIT 1
) AS last_msg ON true`, []interface{}{
				sessionIDs,
				userID,
				userID,
				model.MsgTypeAIStream,
				model.SessionTypeGroup,
				userID,
				userID,
			}
	}

	return `
SELECT ranked.session_id, ranked.content, ranked.created_at
FROM (
    SELECT
        m.session_id AS session_id,
        m.content AS content,
        m.created_at AS created_at,
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
	      AND m.content NOT LIKE '%](grix://card/%'
	      AND (r.deleted_before IS NULL OR m.created_at > r.deleted_before)
	      AND (s.session_type <> ? OR m.created_at >= me.joined_at)
) AS ranked
WHERE ranked.rn = 1`, []interface{}{userID, userID, sessionIDs, model.MsgTypeAIStream, model.SessionTypeGroup}
}
