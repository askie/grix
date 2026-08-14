package service

import (
	"errors"
	"strings"
	"time"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/store"
	"gorm.io/gorm"
)

var (
	ErrSessionSpeakingSettingRequired = errors.New("at least one speaking setting is required")
	ErrSessionSpeakingTargetDenied    = errors.New("admin can only update normal members speaking settings")
	ErrSessionOwnerSpeakingImmutable  = errors.New("cannot update owner speaking settings")
)

type SessionAllMembersMutedResp struct {
	SessionID       string `json:"session_id"`
	AllMembersMuted bool   `json:"all_members_muted"`
}

type SessionUpdateMemberSpeakingResp struct {
	SessionID            string `json:"session_id"`
	MemberID             int64  `json:"member_id,string"`
	MemberType           int16  `json:"member_type"`
	IsSpeakMuted         bool   `json:"is_speak_muted"`
	CanSpeakWhenAllMuted bool   `json:"can_speak_when_all_muted"`
}

func SessionUpdateAllMembersMuted(
	userID int64,
	sessionID string,
	allMembersMuted bool,
) (*SessionAllMembersMutedResp, error) {
	sid := strings.TrimSpace(sessionID)
	if sid == "" {
		return nil, ErrSessionNotFound
	}

	operator, session, err := loadSpeakingGovernanceContext(userID, sid)
	if err != nil {
		return nil, err
	}
	if operator.Role != 2 && operator.Role != 3 {
		return nil, ErrSessionRuntimeSettingDenied
	}

	if session.AllMembersMuted != allMembersMuted {
		now := time.Now()
		if err := store.DB.Model(&model.Session{}).
			Where("session_id = ?", sid).
			Updates(map[string]any{
				"all_members_muted": allMembersMuted,
				"updated_at":        now,
			}).Error; err != nil {
			return nil, err
		}

		humanMemberIDs, err := listSessionHumanMemberIDs(sid)
		if err != nil {
			return nil, err
		}
		notifySessionMemberChanged(
			sid,
			"speaking",
			userID,
			humanMemberIDs,
			sessionMemberChangedNotifyMeta{},
		)
	}

	return &SessionAllMembersMutedResp{
		SessionID:       sid,
		AllMembersMuted: allMembersMuted,
	}, nil
}

func SessionUpdateMemberSpeaking(
	userID int64,
	sessionID string,
	memberID int64,
	memberType int16,
	isSpeakMuted *bool,
	canSpeakWhenAllMuted *bool,
) (*SessionUpdateMemberSpeakingResp, error) {
	sid := strings.TrimSpace(sessionID)
	if sid == "" {
		return nil, ErrSessionNotFound
	}
	if memberID <= 0 {
		return nil, ErrInvalidMemberID
	}
	if memberType == 0 {
		memberType = 1
	}
	if memberType != 1 && memberType != 2 {
		return nil, ErrInvalidMemberType
	}
	if isSpeakMuted == nil && canSpeakWhenAllMuted == nil {
		return nil, ErrSessionSpeakingSettingRequired
	}

	operator, _, err := loadSpeakingGovernanceContext(userID, sid)
	if err != nil {
		return nil, err
	}
	if operator.Role != 2 && operator.Role != 3 {
		return nil, ErrSessionRuntimeSettingDenied
	}
	if memberType == 1 && memberID == userID {
		return nil, ErrSessionCannotOperateSelf
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
	if target.Role == 3 {
		return nil, ErrSessionOwnerSpeakingImmutable
	}
	if operator.Role == 2 && target.Role != 1 {
		return nil, ErrSessionSpeakingTargetDenied
	}

	nextIsSpeakMuted := target.IsSpeakMuted
	if isSpeakMuted != nil {
		nextIsSpeakMuted = *isSpeakMuted
	}
	nextCanSpeakWhenAllMuted := target.CanSpeakWhenAllMuted
	if canSpeakWhenAllMuted != nil {
		nextCanSpeakWhenAllMuted = *canSpeakWhenAllMuted
	}

	if target.IsSpeakMuted != nextIsSpeakMuted ||
		target.CanSpeakWhenAllMuted != nextCanSpeakWhenAllMuted {
		now := time.Now()
		if err := store.DB.Transaction(func(tx *gorm.DB) error {
			if err := tx.Model(&model.SessionMember{}).
				Where(
					"session_id = ? AND member_id = ? AND member_type = ?",
					sid,
					memberID,
					memberType,
				).
				Updates(map[string]any{
					"is_speak_muted":           nextIsSpeakMuted,
					"can_speak_when_all_muted": nextCanSpeakWhenAllMuted,
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
			"speaking",
			userID,
			humanMemberIDs,
			sessionMemberChangedNotifyMeta{
				MemberID: memberID,
			},
		)
	}

	return &SessionUpdateMemberSpeakingResp{
		SessionID:            sid,
		MemberID:             memberID,
		MemberType:           memberType,
		IsSpeakMuted:         nextIsSpeakMuted,
		CanSpeakWhenAllMuted: nextCanSpeakWhenAllMuted,
	}, nil
}

func loadSpeakingGovernanceContext(
	userID int64,
	sessionID string,
) (model.SessionMember, model.Session, error) {
	var operator model.SessionMember
	if err := store.DB.Select("role").
		Where("session_id = ? AND member_id = ? AND member_type = 1", sessionID, userID).
		First(&operator).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return model.SessionMember{}, model.Session{}, ErrSessionPermissionDenied
		}
		return model.SessionMember{}, model.Session{}, err
	}

	var session model.Session
	if err := store.DB.
		Select("session_id", "session_type", "all_members_muted", "moderation_status").
		Where("session_id = ? AND is_deleted = false", sessionID).
		First(&session).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return model.SessionMember{}, model.Session{}, ErrSessionNotFound
		}
		return model.SessionMember{}, model.Session{}, err
	}
	if err := ensureLoadedSessionAccessible(session); err != nil {
		return model.SessionMember{}, model.Session{}, err
	}
	if session.SessionType != 2 {
		return model.SessionMember{}, model.Session{}, ErrSessionInvalidType
	}

	return operator, session, nil
}
