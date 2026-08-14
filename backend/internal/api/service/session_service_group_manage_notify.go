package service

import (
	"strings"
	"time"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/store"
	"github.com/askie/grix/backend/internal/ws/protocol"
)

func listSessionHumanMemberIDs(sessionID string) ([]int64, error) {
	var members []model.SessionMember
	if err := store.DB.Select("member_id").
		Where("session_id = ? AND member_type = 1", sessionID).
		Find(&members).Error; err != nil {
		return nil, err
	}

	userIDs := make([]int64, 0, len(members))
	for _, m := range members {
		if m.MemberID <= 0 {
			continue
		}
		userIDs = append(userIDs, m.MemberID)
	}
	return userIDs, nil
}

type sessionMemberChangedNotifyMeta struct {
	RemovedUserIDs []int64
	Title          string
	MemberID       int64
	GroupNickname  string
}

func notifySessionMemberChanged(
	sessionID,
	action string,
	operatorID int64,
	userIDs []int64,
	meta sessionMemberChangedNotifyMeta,
) {
	if sessionID == "" || action == "" || len(userIDs) == 0 {
		return
	}

	payload := protocol.SessionMemberChangedPayload{
		SessionID:      sessionID,
		Action:         action,
		OperatorID:     operatorID,
		MemberID:       meta.MemberID,
		RemovedUserIDs: uniqueInt64IDs(meta.RemovedUserIDs),
		Title:          strings.TrimSpace(meta.Title),
		GroupNickname:  strings.TrimSpace(meta.GroupNickname),
		UpdatedAt:      time.Now().UnixMilli(),
	}

	sent := make(map[int64]struct{}, len(userIDs))
	for _, uid := range userIDs {
		if uid <= 0 {
			continue
		}
		if _, ok := sent[uid]; ok {
			continue
		}
		sent[uid] = struct{}{}
		pushRealtimeEvent(uid, protocol.CmdSessionMemberChanged, payload)
	}
}

func pushSessionMemberAddedOfflinePush(
	sessionID string,
	operatorID int64,
	groupName string,
	userIDs []int64,
) {
	if sessionID == "" || len(userIDs) == 0 {
		return
	}

	payload := protocol.SessionMemberChangedPayload{
		SessionID:  sessionID,
		Action:     "add",
		OperatorID: operatorID,
		Title:      strings.TrimSpace(groupName),
		UpdatedAt:  time.Now().UnixMilli(),
	}

	sent := make(map[int64]struct{}, len(userIDs))
	for _, uid := range userIDs {
		if uid <= 0 || uid == operatorID {
			continue
		}
		if _, ok := sent[uid]; ok {
			continue
		}
		sent[uid] = struct{}{}
		if hasOnlineRealtimeRoute(uid) {
			continue
		}
		pushOfflineEvent(uid, protocol.CmdSessionMemberChanged, payload)
	}
}

func uniqueInt64IDs(ids []int64) []int64 {
	if len(ids) == 0 {
		return nil
	}
	seen := make(map[int64]struct{}, len(ids))
	result := make([]int64, 0, len(ids))
	for _, id := range ids {
		if id <= 0 {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		result = append(result, id)
	}
	if len(result) == 0 {
		return nil
	}
	return result
}

// ListSessionHumanMemberIDs exports the internal helper for cross-package use.
func ListSessionHumanMemberIDs(sessionID string) ([]int64, error) {
	return listSessionHumanMemberIDs(sessionID)
}

// NotifySessionMemberChanged exports the internal helper for cross-package use.
func NotifySessionMemberChanged(
	sessionID, action string,
	operatorID int64,
	userIDs []int64,
	meta sessionMemberChangedNotifyMeta,
) {
	notifySessionMemberChanged(sessionID, action, operatorID, userIDs, meta)
}

// PushSessionTitleUpdate pushes a session_member_changed rename event
// with the given title to all human members of the session.
func PushSessionTitleUpdate(sessionID string, operatorID int64, title string) error {
	userIDs, err := listSessionHumanMemberIDs(sessionID)
	if err != nil {
		return err
	}
	if len(userIDs) == 0 {
		return nil
	}
	notifySessionMemberChanged(sessionID, "rename", operatorID, userIDs, sessionMemberChangedNotifyMeta{
		Title: strings.TrimSpace(title),
	})
	return nil
}

// SetSessionCustomTitleIfEmpty 当会话所有人类成员的 custom_title 为空时，
// 批量写入给定的 title。用于首条消息自动设置会话标题。
func SetSessionCustomTitleIfEmpty(sessionID string, title string) error {
	sid := strings.TrimSpace(sessionID)
	if sid == "" || strings.TrimSpace(title) == "" {
		return nil
	}
	return store.DB.Model(&model.SessionMember{}).
		Where("session_id = ? AND member_type = 1 AND (custom_title IS NULL OR TRIM(custom_title) = '')", sid).
		Update("custom_title", strings.TrimSpace(title)).Error
}
