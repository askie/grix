package service

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/store"
)

const (
	sessionConversationDefaultLimit = 30
	sessionConversationMaxLimit     = 60
)

type ConversationItem struct {
	GroupKey         string       `json:"group_key"`
	ConversationType string       `json:"conversation_type"`
	LatestSessionID  string       `json:"latest_session_id"`
	Title            string       `json:"title"`
	Peer             *SessionPeer `json:"peer"`
	SessionType      int16        `json:"session_type"`
	IsVisitor        bool         `json:"is_visitor"`
	LastMsg          string       `json:"last_msg"`
	// LastMsgTime 为「最后一条可见消息」的时间(unix 秒)，用于列表展示时间；无可见消息为 0。
	LastMsgTime int64 `json:"last_msg_time"`
	Unread      int   `json:"unread"`
	BadgeUnread int   `json:"badge_unread"`
	UpdatedAt   int64 `json:"updated_at"`
	// LatestActiveAt 为该分组内最新会话的排序时间(unix 秒)：取「最后一条可见消息」
	// 的时间，会话尚无可见消息时回退到会话行的 updated_at。查看会话、卡片与工具状态
	// 等不可见消息都不推进它——用户看到什么时间，列表就按什么时间排。
	LatestActiveAt int64 `json:"latest_active_at"`
	IsPinned       bool  `json:"is_pinned"`
	PinnedAt       int64 `json:"pinned_at"`
	IsMuted        bool  `json:"is_muted"`
	ThreadCount    int   `json:"thread_count"`
	HasMoreThreads bool  `json:"has_more_threads"`
}

type ConversationListResp struct {
	HasMore    bool               `json:"has_more"`
	NextCursor string             `json:"next_cursor,omitempty"`
	List       []ConversationItem `json:"list"`
}

type ConversationThreadListResp struct {
	GroupKey   string        `json:"group_key"`
	HasMore    bool          `json:"has_more"`
	NextCursor string        `json:"next_cursor,omitempty"`
	List       []SessionItem `json:"list"`
}

type conversationCandidate struct {
	member       model.SessionMember
	sessionType  int16
	peerID       int64
	peerType     int16
	groupKey     string
	activityAt   int64
	pinned       bool
	pinnedAt     int64
	sortPinned   bool
	sortPinnedAt int64
	peerMuted    bool
}

func SessionConversations(userID int64, limit int, cursor string) (*ConversationListResp, error) {
	limit = normalizeConversationLimit(limit)
	offset := decodeOffsetCursor(cursor)

	candidates, err := loadConversationCandidates(userID)
	if err != nil {
		return nil, err
	}
	groups := foldConversationCandidates(candidates)
	if offset >= len(groups) {
		return &ConversationListResp{List: []ConversationItem{}}, nil
	}

	end := offset + limit
	hasMore := end < len(groups)
	if end > len(groups) {
		end = len(groups)
	}
	pageGroups := groups[offset:end]

	members := make([]model.SessionMember, 0, len(pageGroups))
	groupBySession := make(map[string]*conversationGroup, len(pageGroups))
	for i := range pageGroups {
		group := &pageGroups[i]
		members = append(members, group.latest.member)
		groupBySession[group.latest.member.SessionID] = group
	}

	sessionItems, err := buildSessionItems(userID, members)
	if err != nil {
		return nil, err
	}
	items := make([]ConversationItem, 0, len(sessionItems))
	for _, item := range sessionItems {
		group, ok := groupBySession[item.SessionID]
		if !ok {
			continue
		}
		items = append(items, buildConversationItem(item, *group))
	}

	resp := &ConversationListResp{HasMore: hasMore, List: items}
	if hasMore {
		resp.NextCursor = encodeOffsetCursor(end)
	}
	return resp, nil
}

func SessionConversationThreads(userID int64, groupKey string, limit int, cursor string) (*ConversationThreadListResp, error) {
	limit = normalizeConversationLimit(limit)
	offset := decodeOffsetCursor(cursor)
	groupKey = strings.TrimSpace(groupKey)
	if groupKey == "" {
		return nil, ErrSessionNotFound
	}

	candidates, err := loadConversationCandidates(userID)
	if err != nil {
		return nil, err
	}

	filtered := make([]conversationCandidate, 0)
	for _, candidate := range candidates {
		if candidate.groupKey == groupKey {
			filtered = append(filtered, candidate)
		}
	}
	// 线程列表按会话级置顶排序（与用户资料页口径一致）。会话摘要列表用的是
	// 好友级置顶（sortPinned），若在这里复用会让会话级置顶的旧会话排到分页窗口
	// 之外，导致资料页与线程弹窗看到的顺序和内容不一致。
	sortConversationThreadCandidates(filtered)
	if offset >= len(filtered) {
		return &ConversationThreadListResp{GroupKey: groupKey, List: []SessionItem{}}, nil
	}

	end := offset + limit
	hasMore := end < len(filtered)
	if end > len(filtered) {
		end = len(filtered)
	}

	members := make([]model.SessionMember, 0, end-offset)
	for _, candidate := range filtered[offset:end] {
		members = append(members, candidate.member)
	}
	items, err := buildSessionItems(userID, members)
	if err != nil {
		return nil, err
	}

	resp := &ConversationThreadListResp{GroupKey: groupKey, HasMore: hasMore, List: items}
	if hasMore {
		resp.NextCursor = encodeOffsetCursor(end)
	}
	return resp, nil
}

type conversationGroup struct {
	groupKey    string
	latest      conversationCandidate
	unread      int
	badgeUnread int
	threadCount int
	// allMuted is session-level: true only when every thread is muted.
	// peerMuted is user-level for private conversations and is independent
	// of session_members.is_muted, so later threads inherit it.
	allMuted  bool
	peerMuted bool
}

func loadConversationCandidates(userID int64) ([]conversationCandidate, error) {
	var rows []struct {
		SessionID    string     `gorm:"column:session_id"`
		CustomTitle  string     `gorm:"column:custom_title"`
		IsPinned     bool       `gorm:"column:is_pinned"`
		IsMuted      bool       `gorm:"column:is_muted"`
		PinnedAt     *time.Time `gorm:"column:pinned_at"`
		UnreadCount  int        `gorm:"column:unread_count"`
		LastActiveAt time.Time  `gorm:"column:last_active_at"`
		JoinedAt     time.Time  `gorm:"column:joined_at"`
		Role         int16      `gorm:"column:role"`
		SessionType  int16      `gorm:"column:session_type"`
		// SessionUpdatedAt 是会话行的更新时间，只在会话没有任何可见消息时兜底排序。
		// 不能用 me.last_active_at 排序：它被 session_read 刷成当前时间，「点开
		// 看一眼」就会把会话顶到最前。
		SessionUpdatedAt time.Time `gorm:"column:session_updated_at"`
		PeerID           *int64    `gorm:"column:peer_id"`
		PeerType         *int16    `gorm:"column:peer_type"`
	}

	// 会话有效性口径：未删除会话 + 群组活跃 + (从未历史重置 OR 重置点之后仍有可见消息)。
	// 末项与底部角标(pull_sync 未读快照)共用 store.VisibleAfterCutoffExistsSQL，
	// 确保「删除后又来新消息」的会话两边同步重现，杜绝角标与会话列表分叉。
	existsSQL, existsArgs := store.VisibleAfterCutoffExistsSQL("me.session_id", "shr.deleted_before", "me.joined_at", "s.session_type", userID)
	err := store.DB.
		Table("session_members AS me").
		Select(
			"me.session_id, me.custom_title, me.is_pinned, me.is_muted, me.pinned_at, me.unread_count, me.last_active_at, me.joined_at, me.role, "+
				"s.session_type, s.updated_at AS session_updated_at, peer.member_id AS peer_id, peer.member_type AS peer_type",
		).
		Joins("JOIN sessions AS s ON s.session_id = me.session_id").
		Joins(
			"LEFT JOIN session_members AS peer ON s.session_type = ? AND peer.session_id = me.session_id AND NOT (peer.member_type = 1 AND peer.member_id = ?)",
			model.SessionTypeDirect,
			userID,
		).
		Joins("LEFT JOIN session_history_resets AS shr ON shr.session_id = me.session_id AND shr.user_id = ?", userID).
		Where("me.member_id = ? AND me.member_type = 1", userID).
		Where("s.is_deleted = false AND (s.session_type <> ? OR s.moderation_status = ?)", model.SessionTypeGroup, model.SessionModerationStatusActive).
		Where("shr.session_id IS NULL OR "+existsSQL, existsArgs...).
		Order("s.updated_at DESC").
		Find(&rows).Error
	if err != nil {
		return nil, err
	}

	peerBySession := make(map[string]model.SessionMember, len(rows))
	for _, row := range rows {
		if row.SessionType != model.SessionTypeDirect || row.PeerID == nil || row.PeerType == nil {
			continue
		}
		peerBySession[row.SessionID] = model.SessionMember{
			SessionID:  row.SessionID,
			MemberID:   *row.PeerID,
			MemberType: *row.PeerType,
		}
	}
	friendPinMap, err := loadFriendPinMap(userID, peerBySession)
	if err != nil {
		return nil, err
	}
	friendMuteMap, err := loadFriendMuteMap(userID, peerBySession)
	if err != nil {
		return nil, err
	}

	// 排序时间取「最后一条可见消息」的时间，与用户点进会话看到的最后一条对齐。
	// 会话活跃时间（session 行 updated_at / 成员 last_active_at）会被卡片、工具
	// 状态等不可见消息以及已读回执推进，不能拿来排序。
	sessionIDs := make([]string, 0, len(rows))
	for _, row := range rows {
		sid := strings.TrimSpace(row.SessionID)
		if sid != "" {
			sessionIDs = append(sessionIDs, sid)
		}
	}
	visibleLastMsgMap, err := loadVisibleLastMsgSummaryMap(userID, sessionIDs)
	if err != nil {
		return nil, err
	}

	candidates := make([]conversationCandidate, 0, len(rows))
	seen := make(map[string]struct{}, len(rows))
	for _, row := range rows {
		if strings.TrimSpace(row.SessionID) == "" {
			continue
		}
		rowKey := fmt.Sprintf("%s:%d:%d", row.SessionID, valueInt16(row.PeerType), valueInt64(row.PeerID))
		if _, ok := seen[rowKey]; ok {
			continue
		}
		seen[rowKey] = struct{}{}

		member := model.SessionMember{
			SessionID:    row.SessionID,
			MemberID:     userID,
			MemberType:   1,
			CustomTitle:  row.CustomTitle,
			IsPinned:     row.IsPinned,
			IsMuted:      row.IsMuted,
			PinnedAt:     row.PinnedAt,
			UnreadCount:  row.UnreadCount,
			LastActiveAt: row.LastActiveAt,
			JoinedAt:     row.JoinedAt,
			Role:         row.Role,
		}

		groupKey := fmt.Sprintf("session:%s", row.SessionID)
		if row.SessionType == model.SessionTypeDirect && row.PeerID != nil && row.PeerType != nil {
			groupKey = fmt.Sprintf("private:%d:%d", *row.PeerType, *row.PeerID)
		}

		pinnedAt := int64(0)
		if row.PinnedAt != nil {
			pinnedAt = row.PinnedAt.Unix()
		}
		// For private chats, only friend-level pin controls the
		// conversation list sort. Session-level pin must not leak
		// into the conversation summary ordering — it is only used
		// in the user profile / thread popup views.
		sortPinned := false
		sortPinnedAt := int64(0)
		if row.SessionType == model.SessionTypeDirect {
			if row.PeerID != nil {
				if fp, ok := friendPinMap[*row.PeerID]; ok {
					sortPinned = fp.IsPinned
					sortPinnedAt = fp.PinnedAt
				}
			}
		} else {
			sortPinned = row.IsPinned
			sortPinnedAt = pinnedAt
		}

		peerMuted := false
		if row.SessionType == model.SessionTypeDirect && row.PeerID != nil {
			peerMuted = friendMuteMap[*row.PeerID]
		}

		candidates = append(candidates, conversationCandidate{
			member:       member,
			sessionType:  row.SessionType,
			peerID:       valueInt64(row.PeerID),
			peerType:     valueInt16(row.PeerType),
			groupKey:     groupKey,
			activityAt:   conversationActivityAt(row.SessionID, row.SessionUpdatedAt, visibleLastMsgMap),
			pinned:       row.IsPinned,
			pinnedAt:     pinnedAt,
			sortPinned:   sortPinned,
			sortPinnedAt: sortPinnedAt,
			peerMuted:    peerMuted,
		})
	}
	return candidates, nil
}

// conversationActivityAt 返回会话的排序时间：最后一条可见消息的时间，
// 没有可见消息时回退到会话行的更新时间，避免新建或清空历史的会话沉底。
func conversationActivityAt(
	sessionID string,
	sessionUpdatedAt time.Time,
	visibleLastMsgMap map[string]visibleLastMsg,
) int64 {
	if last, ok := visibleLastMsgMap[strings.TrimSpace(sessionID)]; ok && last.CreatedAt > 0 {
		return last.CreatedAt
	}
	return sessionUpdatedAt.Unix()
}

func foldConversationCandidates(candidates []conversationCandidate) []conversationGroup {
	groupMap := make(map[string]*conversationGroup)
	for _, candidate := range candidates {
		group, ok := groupMap[candidate.groupKey]
		if !ok {
			groupMap[candidate.groupKey] = &conversationGroup{
				groupKey:    candidate.groupKey,
				latest:      candidate,
				unread:      candidate.member.UnreadCount,
				badgeUnread: conversationBadgeUnread(candidate),
				threadCount: 1,
				allMuted:    candidate.member.IsMuted,
				peerMuted:   candidate.peerMuted,
			}
			continue
		}
		group.unread += candidate.member.UnreadCount
		group.badgeUnread += conversationBadgeUnread(candidate)
		group.threadCount++
		group.allMuted = group.allMuted && candidate.member.IsMuted
		group.peerMuted = group.peerMuted || candidate.peerMuted
		if compareConversationCandidateByActivity(candidate, group.latest) < 0 {
			group.latest = candidate
		}
	}

	groups := make([]conversationGroup, 0, len(groupMap))
	for _, group := range groupMap {
		groups = append(groups, *group)
	}
	sort.SliceStable(groups, func(i, j int) bool {
		return compareConversationCandidate(groups[i].latest, groups[j].latest) < 0
	})
	return groups
}

func buildConversationItem(item SessionItem, group conversationGroup) ConversationItem {
	latest := group.latest
	isPinned := latest.sortPinned
	pinnedAt := latest.sortPinnedAt
	if item.SessionType != model.SessionTypeDirect {
		isPinned = item.IsPinned
		pinnedAt = item.PinnedAt
	}
	conversationType := "group"
	if strings.HasPrefix(group.groupKey, "private:") {
		conversationType = "private"
	}
	isMuted := group.allMuted
	if conversationType == "private" {
		isMuted = group.peerMuted
	}
	return ConversationItem{
		GroupKey:         group.groupKey,
		ConversationType: conversationType,
		LatestSessionID:  item.SessionID,
		Title:            item.Title,
		Peer:             item.Peer,
		SessionType:      item.SessionType,
		IsVisitor:        item.IsVisitor,
		LastMsg:          item.LastMsg,
		LastMsgTime:      item.LastMsgTime,
		Unread:           group.unread,
		BadgeUnread:      group.badgeUnread,
		UpdatedAt:        item.UpdatedAt,
		LatestActiveAt:   latest.activityAt,
		IsPinned:         isPinned,
		PinnedAt:         pinnedAt,
		IsMuted:          isMuted,
		ThreadCount:      group.threadCount,
		HasMoreThreads:   group.threadCount > 1,
	}
}

func sortConversationCandidates(candidates []conversationCandidate) {
	sort.SliceStable(candidates, func(i, j int) bool {
		return compareConversationCandidate(candidates[i], candidates[j]) < 0
	})
}

// sortConversationThreadCandidates 按会话级置顶（session_members.is_pinned）排序，
// 供资料页 / 线程弹窗的单会话列表使用。
func sortConversationThreadCandidates(candidates []conversationCandidate) {
	sort.SliceStable(candidates, func(i, j int) bool {
		return compareConversationThreadCandidate(candidates[i], candidates[j]) < 0
	})
}

func compareConversationThreadCandidate(a, b conversationCandidate) int {
	if a.pinned != b.pinned {
		if a.pinned {
			return -1
		}
		return 1
	}
	if a.pinned && a.pinnedAt != b.pinnedAt {
		if a.pinnedAt > b.pinnedAt {
			return -1
		}
		return 1
	}
	if a.activityAt != b.activityAt {
		if a.activityAt > b.activityAt {
			return -1
		}
		return 1
	}
	if a.member.SessionID < b.member.SessionID {
		return -1
	}
	if a.member.SessionID > b.member.SessionID {
		return 1
	}
	return 0
}

func compareConversationCandidate(a, b conversationCandidate) int {
	if a.sortPinned != b.sortPinned {
		if a.sortPinned {
			return -1
		}
		return 1
	}
	if a.activityAt != b.activityAt {
		if a.activityAt > b.activityAt {
			return -1
		}
		return 1
	}
	if a.sortPinnedAt != b.sortPinnedAt {
		if a.sortPinnedAt > b.sortPinnedAt {
			return -1
		}
		return 1
	}
	if a.member.SessionID < b.member.SessionID {
		return -1
	}
	if a.member.SessionID > b.member.SessionID {
		return 1
	}
	return 0
}

// compareConversationCandidateByActivity compares candidates by activity time only,
// ignoring pin status. Used to select the "latest" session within a conversation
// group so the summary always reflects the most recently active session.
func compareConversationCandidateByActivity(a, b conversationCandidate) int {
	if a.activityAt != b.activityAt {
		if a.activityAt > b.activityAt {
			return -1
		}
		return 1
	}
	if a.member.SessionID < b.member.SessionID {
		return -1
	}
	if a.member.SessionID > b.member.SessionID {
		return 1
	}
	return 0
}

func conversationBadgeUnread(candidate conversationCandidate) int {
	if candidate.peerMuted || candidate.member.IsMuted {
		return 0
	}
	return candidate.member.UnreadCount
}

func normalizeConversationLimit(limit int) int {
	if limit <= 0 {
		return sessionConversationDefaultLimit
	}
	if limit > sessionConversationMaxLimit {
		return sessionConversationMaxLimit
	}
	return limit
}

func decodeOffsetCursor(cursor string) int {
	cursor = strings.TrimSpace(cursor)
	if cursor == "" {
		return 0
	}
	offset, err := strconv.Atoi(cursor)
	if err != nil || offset < 0 {
		return 0
	}
	return offset
}

func encodeOffsetCursor(offset int) string {
	if offset <= 0 {
		return ""
	}
	return strconv.Itoa(offset)
}

func valueInt64(v *int64) int64 {
	if v == nil {
		return 0
	}
	return *v
}

func valueInt16(v *int16) int16 {
	if v == nil {
		return 0
	}
	return *v
}
