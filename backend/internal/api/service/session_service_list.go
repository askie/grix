package service

import (
	"strings"
	"time"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/store"
	"gorm.io/gorm"
)

func SessionList(userID int64, limit, offset int) (*SessionListResp, error) {
	cursor := time.Now().Unix()
	var members []model.SessionMember
	err := applySessionListOrder(sessionMemberListQuery(userID)).
		Limit(limit + 1).Offset(offset).
		Find(&members).Error
	if err != nil {
		return nil, err
	}

	hasMore := len(members) > limit
	if hasMore {
		members = members[:limit]
	}

	list, err := buildSessionItems(userID, members)
	if err != nil {
		return nil, err
	}

	return &SessionListResp{HasMore: hasMore, List: list, Cursor: cursor}, nil
}

func SessionSync(userID int64, since int64, limit int) (*SessionSyncResp, error) {
	cursor := time.Now().Unix()

	// since 为秒级游标，last_active_at 与 deleted_at 用 ">" 比较时，同一秒内发生的
	// 多条变更可能被跨调用漏掉。回退 1 秒做重叠拉取兜住同秒边界；客户端按 session_id
	// 幂等 upsert，重复返回的会话无副作用。
	effectiveSince := since
	if effectiveSince > 0 {
		effectiveSince--
	}
	sinceTime := time.Unix(effectiveSince, 0)

	var members []model.SessionMember
	err := applySessionListOrder(sessionMemberListQuery(userID).Where(
		"last_active_at > ?",
		sinceTime,
	)).
		Limit(limit + 1).
		Find(&members).Error
	if err != nil {
		return nil, err
	}

	hasMore := len(members) > limit
	if hasMore {
		members = members[:limit]
	}

	list, err := buildSessionItems(userID, members)
	if err != nil {
		return nil, err
	}

	deletedSessionIDs, err := loadDeletedSessionIDs(userID, sinceTime)
	if err != nil {
		return nil, err
	}

	return &SessionSyncResp{
		HasMore:           hasMore,
		List:              list,
		DeletedSessionIDs: deletedSessionIDs,
		Cursor:            cursor,
	}, nil
}

// SessionListAll 返回 owner 的所有会话，可按 sessionType 过滤（0 = 不过滤，1 = 私聊，2 = 群聊）。
func SessionListAll(userID int64, limit, offset int, sessionType int16) (*SessionSearchResp, error) {
	if limit <= 0 {
		limit = sessionSearchDefaultLimit
	}
	if offset < 0 {
		offset = 0
	}

	var members []model.SessionMember
	if err := applySessionListOrder(sessionMemberListQuery(userID)).
		Limit(limit + 1).Offset(offset).
		Find(&members).Error; err != nil {
		return nil, err
	}

	hasMore := len(members) > limit
	if hasMore {
		members = members[:limit]
	}

	items, err := buildSessionItems(userID, members)
	if err != nil {
		return nil, err
	}

	allItems := make([]SessionSearchItem, 0, len(items))
	for _, item := range items {
		if sessionType != 0 && item.SessionType != sessionType {
			continue
		}
		allItems = append(allItems, SessionSearchItem{
			SessionID:   item.SessionID,
			Title:       item.Title,
			SessionType: item.SessionType,
		})
	}

	return &SessionSearchResp{HasMore: hasMore, List: allItems}, nil
}

// SessionSearch 按关键词搜索会话，可按 sessionType 过滤（0 = 不过滤，1 = 私聊，2 = 群聊）。
func SessionSearch(userID int64, keyword string, limit, offset int, sessionType int16) (*SessionSearchResp, error) {
	searchKeyword := buildSessionSearchKeyword(keyword)
	if searchKeyword.lowered == "" {
		return &SessionSearchResp{List: []SessionSearchItem{}}, nil
	}

	var members []model.SessionMember
	if err := applySessionListOrder(sessionMemberListQuery(userID)).
		Find(&members).Error; err != nil {
		return nil, err
	}

	items, err := buildSessionItems(userID, members)
	if err != nil {
		return nil, err
	}

	matched := make([]SessionSearchItem, 0, len(items))
	for _, item := range items {
		if sessionType != 0 && item.SessionType != sessionType {
			continue
		}
		if !sessionSearchMatchesKeyword(item.SessionID, item.Title, searchKeyword) {
			continue
		}
		matched = append(matched, SessionSearchItem{
			SessionID:   item.SessionID,
			Title:       item.Title,
			SessionType: item.SessionType,
		})
	}

	return buildSessionSearchResp(rankSessionSearchItems(matched, searchKeyword), limit, offset), nil
}

// SessionSearchByID 按 session_id 精确查找，可按 sessionType 过滤（0 = 不过滤，1 = 私聊，2 = 群聊）。
func SessionSearchByID(userID int64, sessionID string, limit, offset int, sessionType int16) (*SessionSearchResp, error) {
	normalizedSessionID := strings.TrimSpace(sessionID)
	if normalizedSessionID == "" {
		return &SessionSearchResp{List: []SessionSearchItem{}}, nil
	}

	var members []model.SessionMember
	if err := applySessionListOrder(sessionMemberListQuery(userID).Where("session_id = ?", normalizedSessionID)).
		Find(&members).Error; err != nil {
		return nil, err
	}

	items, err := buildSessionItems(userID, members)
	if err != nil {
		return nil, err
	}

	matched := make([]SessionSearchItem, 0, len(items))
	for _, item := range items {
		if sessionType != 0 && item.SessionType != sessionType {
			continue
		}
		matched = append(matched, SessionSearchItem{
			SessionID:   item.SessionID,
			Title:       item.Title,
			SessionType: item.SessionType,
		})
	}

	return buildSessionSearchResp(matched, limit, offset), nil
}

// SessionSearchByPeer 按对方账户精确定位当前用户与 peer 的私聊会话（走 direct_key，不依赖标题）。
// 没有会话时返回空列表；不会自动创建会话。
func SessionSearchByPeer(userID, peerID int64) (*SessionSearchResp, error) {
	if userID <= 0 || peerID <= 0 || userID == peerID {
		return &SessionSearchResp{List: []SessionSearchItem{}}, nil
	}
	directKey := buildDirectKey(userID, peerID, 1)

	var sessionIDs []string
	if err := store.DB.Model(&model.Session{}).
		Where("direct_key = ? AND session_type = 1 AND is_deleted = false", directKey).
		Order("updated_at DESC").
		Pluck("session_id", &sessionIDs).Error; err != nil {
		return nil, err
	}
	if len(sessionIDs) == 0 {
		return &SessionSearchResp{List: []SessionSearchItem{}}, nil
	}

	var members []model.SessionMember
	if err := applySessionListOrder(sessionMemberListQuery(userID).Where("session_id IN ?", sessionIDs)).
		Find(&members).Error; err != nil {
		return nil, err
	}
	items, err := buildSessionItems(userID, members)
	if err != nil {
		return nil, err
	}
	matched := make([]SessionSearchItem, 0, len(items))
	for _, item := range items {
		matched = append(matched, SessionSearchItem{
			SessionID:   item.SessionID,
			Title:       item.Title,
			SessionType: item.SessionType,
		})
	}
	return buildSessionSearchResp(matched, len(matched), 0), nil
}

func buildSessionSearchResp(items []SessionSearchItem, limit, offset int) *SessionSearchResp {
	if limit <= 0 {
		limit = sessionSearchDefaultLimit
	}
	if offset < 0 {
		offset = 0
	}
	if offset >= len(items) {
		return &SessionSearchResp{List: []SessionSearchItem{}}
	}

	end := offset + limit
	hasMore := end < len(items)
	if end > len(items) {
		end = len(items)
	}
	return &SessionSearchResp{
		HasMore: hasMore,
		List:    items[offset:end],
	}
}

func sessionMemberListQuery(userID int64) *gorm.DB {
	return store.DB.Where("member_id = ? AND member_type = 1", userID)
}

func applySessionListOrder(query *gorm.DB) *gorm.DB {
	return query.
		Order("is_pinned DESC").
		Order("CASE WHEN pinned_at IS NULL THEN 1 ELSE 0 END ASC").
		Order("pinned_at DESC").
		Order("last_active_at DESC")
}
