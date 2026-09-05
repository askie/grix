package service

import (
	"errors"
	"strings"
	"time"

	"github.com/askie/grix/backend/internal/agentreceive"
	"github.com/askie/grix/backend/internal/liveactivity"
	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/store"
	"github.com/askie/grix/backend/internal/systemsetting"
	"gorm.io/gorm"
)

func SessionGroupDetail(agentID, ownerID int64, sessionID string) (*SessionDetailResp, error) {
	sid := strings.TrimSpace(sessionID)
	if sid == "" {
		return nil, ErrSessionNotFound
	}

	var session model.Session
	if err := store.DB.Select("session_id", "session_type", "moderation_status").
		Where("session_id = ? AND is_deleted = false", sid).
		First(&session).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrSessionNotFound
		}
		return nil, err
	}
	if err := ensureLoadedSessionAccessible(session); err != nil {
		return nil, err
	}
	if session.SessionType != 2 {
		return nil, ErrSessionInvalidType
	}

	if err := ensureAgentSessionGroupDetailAccess(agentID, ownerID, sid); err != nil {
		return nil, err
	}

	return loadSessionDetail(ownerID, sid)
}

func SessionDetail(userID int64, sessionID string) (*SessionDetailResp, error) {
	if err := ensureHumanSessionDetailAccess(userID, sessionID); err != nil {
		return nil, err
	}

	return loadSessionDetail(userID, sessionID)
}

func ensureHumanSessionDetailAccess(userID int64, sessionID string) error {
	if err := ensureHumanSessionMembership(userID, sessionID); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrSessionPermissionDenied
		}
		return err
	}
	return nil
}

func ensureAgentSessionGroupDetailAccess(agentID, ownerID int64, sessionID string) error {
	if agentID <= 0 || ownerID <= 0 {
		return ErrSessionPermissionDenied
	}

	agentMember, err := findSessionMember(sessionID, agentID, 2)
	if err != nil {
		return err
	}
	if agentMember != nil {
		return nil
	}

	delegatedOwnerID, err := resolveDelegatedLeaveOwnerID(agentID, ownerID, sessionID)
	if err != nil {
		return err
	}
	if delegatedOwnerID <= 0 {
		return ErrSessionPermissionDenied
	}

	humanMember, err := findSessionMember(sessionID, delegatedOwnerID, 1)
	if err != nil {
		return err
	}
	if humanMember == nil {
		return ErrSessionPermissionDenied
	}
	return nil
}

func loadSessionDetail(userID int64, sessionID string) (*SessionDetailResp, error) {
	var session model.Session
	if err := store.DB.Where("session_id = ? AND is_deleted = false", sessionID).
		First(&session).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrSessionNotFound
		}
		return nil, err
	}
	if err := ensureLoadedSessionAccessible(session); err != nil {
		return nil, err
	}
	groupSettings, err := systemsetting.GetGroupSettings()
	if err != nil {
		return nil, err
	}

	var members []model.SessionMember
	if err := store.DB.Where("session_id = ?", sessionID).
		Order("joined_at ASC").
		Find(&members).Error; err != nil {
		return nil, err
	}

	userIDs := make([]int64, 0, len(members))
	seenUsers := make(map[int64]struct{}, len(members))
	var agentIDs []int64
	seenAgents := make(map[int64]struct{}, len(members))
	for _, m := range members {
		if m.MemberID <= 0 {
			continue
		}
		if m.MemberType == 2 {
			if _, ok := seenAgents[m.MemberID]; ok {
				continue
			}
			seenAgents[m.MemberID] = struct{}{}
			agentIDs = append(agentIDs, m.MemberID)
			continue
		}
		if m.MemberType != 1 {
			continue
		}
		if _, ok := seenUsers[m.MemberID]; ok {
			continue
		}
		seenUsers[m.MemberID] = struct{}{}
		userIDs = append(userIDs, m.MemberID)
	}

	userDisplayNameMap := make(map[int64]string, len(userIDs))
	remarkNameMap := make(map[int64]string, len(userIDs))
	if len(userIDs) > 0 {
		var users []model.User
		if err := store.DB.Select("id", "nickname", "username").
			Where("id IN ?", userIDs).
			Find(&users).Error; err != nil {
			return nil, err
		}
		for _, u := range users {
			displayName := strings.TrimSpace(u.Nickname)
			if displayName == "" {
				displayName = strings.TrimSpace(u.Username)
			}
			if displayName != "" {
				userDisplayNameMap[u.ID] = displayName
			}
		}
		remarkNameMap, err = loadFriendRemarkNameMap(userID, userIDs)
		if err != nil {
			return nil, err
		}
	}

	agentNameMap := make(map[int64]string, len(agentIDs))
	agentOwnerMap := make(map[int64]int64, len(agentIDs))
	if len(agentIDs) > 0 {
		var agents []model.Agent
		if err := store.DB.Select("id, agent_name, owner_id").
			Where("id IN ?", agentIDs).
			Find(&agents).Error; err == nil {
			for _, a := range agents {
				agentNameMap[a.ID] = a.AgentName
				agentOwnerMap[a.ID] = a.OwnerID
			}
		}
	}

	items := make([]SessionDetailMember, 0, len(members))
	for _, m := range members {
		mode, backlogCount := agentreceive.Normalize(m.AgentReceiveMode, m.AgentReceiveBacklogCount)
		item := SessionDetailMember{
			MemberID:                 m.MemberID,
			MemberType:               m.MemberType,
			Role:                     m.Role,
			LastReadMsgID:            m.LastReadMsgID,
			IsSpeakMuted:             m.IsSpeakMuted,
			CanSpeakWhenAllMuted:     m.CanSpeakWhenAllMuted,
			AgentReceiveMode:         mode,
			AgentReceiveBacklogCount: backlogCount,
		}
		if m.MemberType == 2 {
			item.Nickname = agentNameMap[m.MemberID]
			item.AgentReceiveEditable = agentOwnerMap[m.MemberID] == userID
		} else if m.MemberType == 1 {
			if remarkName := strings.TrimSpace(remarkNameMap[m.MemberID]); remarkName != "" {
				item.Nickname = remarkName
			} else if groupNickname := strings.TrimSpace(m.GroupNickname); groupNickname != "" {
				item.Nickname = groupNickname
			} else {
				item.Nickname = userDisplayNameMap[m.MemberID]
			}
			item.GroupNickname = strings.TrimSpace(m.GroupNickname)
			item.AgentReceiveEditable = m.MemberID == userID
		}
		items = append(items, item)
	}
	visitorInfo, err := loadSessionVisitorInfo(sessionID)
	if err != nil {
		return nil, err
	}

	return &SessionDetailResp{
		SessionID:             session.SessionID,
		GroupName:             strings.TrimSpace(session.GroupName),
		SessionType:           session.SessionType,
		IsVisitor:             visitorInfo != nil,
		VisitorInfo:           visitorInfo,
		MemberCount:           len(items),
		AllowMemberInvite:     session.AllowMemberInvite,
		AllMembersMuted:       session.AllMembersMuted,
		MemberInviteThreshold: groupSettings.MemberInviteThreshold,
		Members:               items,
	}, nil
}

func loadSessionVisitorInfo(sessionID string) (*SessionVisitorInfo, error) {
	sid := strings.TrimSpace(sessionID)
	if sid == "" {
		return nil, nil
	}
	var row struct {
		SiteID       int64     `gorm:"column:site_id"`
		SiteName     string    `gorm:"column:site_name"`
		VisitorID    int64     `gorm:"column:visitor_id"`
		VisitorKey   string    `gorm:"column:visitor_key"`
		VisitorName  string    `gorm:"column:visitor_name"`
		VisitorEmail string    `gorm:"column:visitor_email"`
		LastPageURL  string    `gorm:"column:last_page_url"`
		Status       int16     `gorm:"column:status"`
		LastActiveAt time.Time `gorm:"column:last_active_at"`
	}
	err := store.DB.Table("widget_sessions ws").
		Select("ws.site_id, ws.visitor_id, ws.visitor_key, ws.visitor_name, ws.visitor_email, ws.last_page_url, ws.status, ws.last_active_at, coalesce(site.site_name,'') as site_name").
		Joins("LEFT JOIN widget_sites site ON site.id = ws.site_id").
		Where("ws.session_id = ?", sid).
		Limit(1).
		Scan(&row).Error
	if err != nil {
		return nil, err
	}
	if row.SiteID <= 0 || row.VisitorID <= 0 {
		return nil, nil
	}
	return &SessionVisitorInfo{
		SiteID:       row.SiteID,
		SiteName:     strings.TrimSpace(row.SiteName),
		VisitorID:    row.VisitorID,
		VisitorKey:   strings.TrimSpace(row.VisitorKey),
		VisitorName:  strings.TrimSpace(row.VisitorName),
		VisitorEmail: strings.TrimSpace(row.VisitorEmail),
		LastPageURL:  strings.TrimSpace(row.LastPageURL),
		Status:       row.Status,
		LastActiveAt: row.LastActiveAt.Unix(),
	}, nil
}

func SessionRename(userID int64, sessionID, rawTitle string) (*SessionRenameResp, error) {
	sid := strings.TrimSpace(sessionID)
	if sid == "" {
		return nil, ErrSessionNotFound
	}

	title, err := normalizeCustomSessionTitle(rawTitle)
	if err != nil {
		return nil, err
	}

	var session model.Session
	if err := store.DB.Select("session_id", "session_type", "moderation_status").
		Where("session_id = ? AND is_deleted = false", sid).
		First(&session).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrSessionNotFound
		}
		return nil, err
	}
	if err := ensureLoadedSessionAccessible(session); err != nil {
		return nil, err
	}

	var member model.SessionMember
	if err := store.DB.Select("session_id").
		Where("session_id = ? AND member_id = ? AND member_type = 1", sid, userID).
		First(&member).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrSessionPermissionDenied
		}
		return nil, err
	}

	if err := store.DB.Model(&model.SessionMember{}).
		Where("session_id = ? AND member_type = 1", sid).
		Update("custom_title", title).Error; err != nil {
		return nil, err
	}

	humanMemberIDs, err := listSessionHumanMemberIDs(sid)
	if err != nil {
		return nil, err
	}
	notifySessionMemberChanged(
		sid,
		"rename",
		userID,
		humanMemberIDs,
		sessionMemberChangedNotifyMeta{
			Title: title,
		},
	)

	go func() {
		store.UpdateSessionAgentStateTitleBySession(sid, title)
		// 锁屏卡片上显示的就是这个标题。改名时前端会连着写几次，
		// OnTitleChanged 内部按会话做 5 秒合并。
		liveactivity.OnTitleChanged(sid)
	}()

	return &SessionRenameResp{
		SessionID: sid,
		Title:     title,
	}, nil
}

func normalizeCustomSessionTitle(raw string) (string, error) {
	normalized := strings.Join(strings.Fields(strings.TrimSpace(raw)), " ")
	if len([]rune(normalized)) > sessionCustomTitleMax {
		return "", ErrSessionTitleTooLong
	}
	return normalized, nil
}

func SessionSetGroupNickname(userID int64, sessionID, rawNickname string) (*SessionSetGroupNicknameResp, error) {
	sid := strings.TrimSpace(sessionID)
	if sid == "" {
		return nil, ErrSessionNotFound
	}

	groupNickname, err := normalizeGroupNickname(rawNickname)
	if err != nil {
		return nil, err
	}

	var session model.Session
	if err := store.DB.Select("session_id", "session_type", "moderation_status").
		Where("session_id = ? AND is_deleted = false", sid).
		First(&session).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrSessionNotFound
		}
		return nil, err
	}
	if err := ensureLoadedSessionAccessible(session); err != nil {
		return nil, err
	}
	if session.SessionType != 2 {
		return nil, ErrSessionInvalidType
	}

	var member model.SessionMember
	if err := store.DB.Select("session_id").
		Where("session_id = ? AND member_id = ? AND member_type = 1", sid, userID).
		First(&member).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrSessionPermissionDenied
		}
		return nil, err
	}

	if err := store.DB.Model(&model.SessionMember{}).
		Where("session_id = ? AND member_id = ? AND member_type = 1", sid, userID).
		Update("group_nickname", groupNickname).Error; err != nil {
		return nil, err
	}

	humanMemberIDs, err := listSessionHumanMemberIDs(sid)
	if err != nil {
		return nil, err
	}
	notifySessionMemberChanged(
		sid,
		"nickname",
		userID,
		humanMemberIDs,
		sessionMemberChangedNotifyMeta{
			MemberID:      userID,
			GroupNickname: groupNickname,
		},
	)

	return &SessionSetGroupNicknameResp{
		SessionID:     sid,
		GroupNickname: groupNickname,
	}, nil
}

func SessionSetPinned(userID int64, sessionID string, isPinned bool) (*SessionPinResp, error) {
	sid := strings.TrimSpace(sessionID)
	if sid == "" {
		return nil, ErrSessionNotFound
	}

	var session model.Session
	if err := store.DB.Select("session_id", "session_type", "moderation_status").
		Where("session_id = ? AND is_deleted = false", sid).
		First(&session).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrSessionNotFound
		}
		return nil, err
	}
	if err := ensureLoadedSessionAccessible(session); err != nil {
		return nil, err
	}

	var member model.SessionMember
	if err := store.DB.
		Where("session_id = ? AND member_id = ? AND member_type = 1", sid, userID).
		First(&member).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrSessionPermissionDenied
		}
		return nil, err
	}

	now := time.Now()
	updates := map[string]any{
		"is_pinned": isPinned,
	}
	// Do NOT update last_active_at here: session-level pin should not
	// affect the conversation list ordering, which is driven by
	// friend-level pin for private chats and activity time for all.
	pinnedAt := int64(0)
	if isPinned {
		updates["pinned_at"] = now
		pinnedAt = now.Unix()
	} else {
		updates["pinned_at"] = nil
	}

	if err := store.DB.Model(&model.SessionMember{}).
		Where("session_id = ? AND member_id = ? AND member_type = 1", sid, userID).
		Updates(updates).Error; err != nil {
		return nil, err
	}

	return &SessionPinResp{
		SessionID: sid,
		IsPinned:  isPinned,
		PinnedAt:  pinnedAt,
	}, nil
}

func SessionSetMuted(userID int64, sessionID string, isMuted bool) (*SessionMuteResp, error) {
	sid := strings.TrimSpace(sessionID)
	if sid == "" {
		return nil, ErrSessionNotFound
	}

	var session model.Session
	if err := store.DB.Select("session_id", "session_type", "moderation_status").
		Where("session_id = ? AND is_deleted = false", sid).
		First(&session).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrSessionNotFound
		}
		return nil, err
	}
	if err := ensureLoadedSessionAccessible(session); err != nil {
		return nil, err
	}

	var member model.SessionMember
	if err := store.DB.
		Where("session_id = ? AND member_id = ? AND member_type = 1", sid, userID).
		First(&member).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrSessionPermissionDenied
		}
		return nil, err
	}

	if err := store.DB.Model(&model.SessionMember{}).
		Where("session_id = ? AND member_id = ? AND member_type = 1", sid, userID).
		Update("is_muted", isMuted).Error; err != nil {
		return nil, err
	}

	return &SessionMuteResp{
		SessionID: sid,
		IsMuted:   isMuted,
	}, nil
}

func normalizeGroupNickname(raw string) (string, error) {
	normalized := strings.Join(strings.Fields(strings.TrimSpace(raw)), " ")
	if len([]rune(normalized)) > sessionGroupNicknameMax {
		return "", ErrSessionGroupNicknameTooLong
	}
	return normalized, nil
}
