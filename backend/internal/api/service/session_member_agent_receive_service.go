package service

import (
	"errors"
	"strings"
	"time"

	"github.com/askie/grix/backend/internal/agentreceive"
	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/store"
	"gorm.io/gorm"
)

func SessionUpdateMemberAgentReceiveSetting(
	userID int64,
	sessionID string,
	memberID int64,
	memberType int16,
	mode int16,
	backlogCount int,
) (*SessionMemberAgentReceiveSettingResp, error) {
	sid := strings.TrimSpace(sessionID)
	if sid == "" {
		return nil, ErrSessionNotFound
	}
	if memberID <= 0 {
		return nil, ErrInvalidMemberID
	}
	if memberType != 1 && memberType != 2 {
		return nil, ErrInvalidMemberType
	}
	if mode != agentreceive.ModeNormal && mode != agentreceive.ModeMentionOnly {
		return nil, ErrSessionAgentReceiveModeInvalid
	}
	if backlogCount < 0 || backlogCount > agentreceive.MaxBacklogCount {
		return nil, ErrSessionAgentReceiveBacklogInvalid
	}

	var operator model.SessionMember
	if err := store.DB.Select("role").
		Where("session_id = ? AND member_id = ? AND member_type = 1", sid, userID).
		First(&operator).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrSessionPermissionDenied
		}
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

	var target model.SessionMember
	if err := store.DB.Where(
		"session_id = ? AND member_id = ? AND member_type = ?",
		sid,
		memberID,
		memberType,
	).First(&target).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrSessionMemberNotFound
		}
		return nil, err
	}
	_, effectiveBacklogCount := agentreceive.Normalize(
		mode,
		resolveAgentReceiveBacklogCount(backlogCount, target.AgentReceiveBacklogCount),
	)

	if memberType == 1 {
		if memberID != userID {
			return nil, ErrSessionMemberSettingDenied
		}
	} else {
		var agent model.Agent
		if err := store.DB.Select("id", "owner_id").First(&agent, memberID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil, ErrMemberAgentNotFound
			}
			return nil, err
		}
		if agent.OwnerID != userID {
			return nil, ErrSessionMemberSettingDenied
		}
	}

	now := time.Now().UTC()
	if err := store.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&model.SessionMember{}).
			Where("session_id = ? AND member_id = ? AND member_type = ?", sid, memberID, memberType).
			Updates(map[string]any{
				"agent_receive_mode":          mode,
				"agent_receive_backlog_count": effectiveBacklogCount,
			}).Error; err != nil {
			return err
		}
		return tx.Model(&model.Session{}).
			Where("session_id = ?", sid).
			Update("updated_at", now).Error
	}); err != nil {
		return nil, err
	}

	humanMemberIDs, err := listSessionHumanMemberIDs(sid)
	if err != nil {
		return nil, err
	}
	notifySessionMemberChanged(
		sid,
		"member_agent_receive",
		userID,
		humanMemberIDs,
		sessionMemberChangedNotifyMeta{MemberID: memberID},
	)

	return &SessionMemberAgentReceiveSettingResp{
		SessionID:                sid,
		MemberID:                 memberID,
		MemberType:               memberType,
		AgentReceiveMode:         mode,
		AgentReceiveBacklogCount: effectiveBacklogCount,
	}, nil
}

func resolveAgentReceiveBacklogCount(requested int, current int) int {
	if requested > 0 {
		return requested
	}
	if current > 0 {
		return current
	}
	return agentreceive.DefaultBacklogCount
}
