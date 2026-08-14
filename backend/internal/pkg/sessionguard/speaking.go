package sessionguard

import (
	"context"
	"errors"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/store"
	"gorm.io/gorm"
)

var (
	ErrSpeakForbidden       = errors.New("permission denied")
	ErrMemberSpeakMuted     = errors.New("member is muted")
	ErrGroupAllMembersMuted = errors.New("group is muted")
)

type sessionSpeakState struct {
	SessionType          int16 `gorm:"column:session_type"`
	ModerationStatus     int16 `gorm:"column:moderation_status"`
	AllMembersMuted      bool  `gorm:"column:all_members_muted"`
	Role                 int16 `gorm:"column:role"`
	IsSpeakMuted         bool  `gorm:"column:is_speak_muted"`
	CanSpeakWhenAllMuted bool  `gorm:"column:can_speak_when_all_muted"`
}

func ValidateSpeakPermission(
	ctx context.Context,
	db *gorm.DB,
	sessionID string,
	memberID int64,
	memberType int16,
) error {
	if sessionID == "" || memberID <= 0 {
		return ErrSpeakForbidden
	}
	if memberType == 0 {
		memberType = 1
	}

	queryDB := db
	if queryDB == nil {
		queryDB = store.DB
	}
	if ctx == nil {
		ctx = context.Background()
	}

	var state sessionSpeakState
	err := queryDB.WithContext(ctx).
		Table("session_members AS sm").
		Select(
			"s.session_type",
			"s.moderation_status",
			"s.all_members_muted",
			"sm.role",
			"sm.is_speak_muted",
			"sm.can_speak_when_all_muted",
		).
		Joins("JOIN sessions s ON s.session_id = sm.session_id AND s.is_deleted = false").
		Where("sm.session_id = ? AND sm.member_id = ? AND sm.member_type = ?", sessionID, memberID, memberType).
		Take(&state).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrSpeakForbidden
		}
		return err
	}

	if state.IsSpeakMuted {
		return ErrMemberSpeakMuted
	}
	if state.SessionType != model.SessionTypeGroup {
		return nil
	}
	if state.ModerationStatus == model.SessionModerationStatusBanned {
		return ErrSessionBanned
	}
	if state.Role == 2 || state.Role == 3 {
		return nil
	}
	if state.AllMembersMuted && !state.CanSpeakWhenAllMuted {
		return ErrGroupAllMembersMuted
	}
	return nil
}

func ErrorMessage(err error) string {
	switch {
	case errors.Is(err, ErrMemberSpeakMuted):
		return ErrMemberSpeakMuted.Error()
	case errors.Is(err, ErrGroupAllMembersMuted):
		return ErrGroupAllMembersMuted.Error()
	case errors.Is(err, ErrSessionBanned):
		return ErrSessionBanned.Error()
	default:
		return ErrSpeakForbidden.Error()
	}
}

func IsDeniedError(err error) bool {
	return errors.Is(err, ErrSpeakForbidden) ||
		errors.Is(err, ErrSessionBanned) ||
		errors.Is(err, ErrMemberSpeakMuted) ||
		errors.Is(err, ErrGroupAllMembersMuted)
}
