package service

import (
	"errors"
	"time"

	"github.com/askie/grix/backend/internal/model"
	"gorm.io/gorm"
)

func createDefaultUserSettingsTx(tx *gorm.DB, userID int64, preferredLanguage string) error {
	if tx == nil {
		return errors.New("db transaction is required")
	}
	if userID <= 0 {
		return errors.New("invalid user id")
	}

	now := time.Now()
	setting := model.UserSetting{
		UserID:            userID,
		PreferredLanguage: normalizePreferredLanguage(preferredLanguage),
		FriendAddSetting:  model.FriendAddSettingNeedApproval,
		AllowGroupInvite:  true,
		CreatedAt:         now,
		UpdatedAt:         now,
	}
	return tx.Create(&setting).Error
}
