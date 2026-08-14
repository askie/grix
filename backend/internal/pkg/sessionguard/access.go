package sessionguard

import (
	"context"
	"errors"
	"strings"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/store"
	"gorm.io/gorm"
)

var ErrSessionBanned = errors.New("group banned")

type sessionAccessState struct {
	SessionType      int16 `gorm:"column:session_type"`
	ModerationStatus int16 `gorm:"column:moderation_status"`
	IsDeleted        bool  `gorm:"column:is_deleted"`
}

func ValidateSessionAvailable(
	ctx context.Context,
	db *gorm.DB,
	sessionID string,
) error {
	sid := strings.TrimSpace(sessionID)
	if sid == "" {
		return gorm.ErrRecordNotFound
	}

	queryDB := db
	if queryDB == nil {
		queryDB = store.DB
	}
	if ctx == nil {
		ctx = context.Background()
	}

	var state sessionAccessState
	if err := queryDB.WithContext(ctx).
		Table("sessions").
		Select("session_type", "moderation_status", "is_deleted").
		Where("session_id = ?", sid).
		Take(&state).Error; err != nil {
		return err
	}
	if state.IsDeleted {
		return gorm.ErrRecordNotFound
	}
	if state.SessionType == model.SessionTypeGroup &&
		state.ModerationStatus == model.SessionModerationStatusBanned {
		return ErrSessionBanned
	}
	return nil
}
