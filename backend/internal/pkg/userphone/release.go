package userphone

import (
	"time"

	"github.com/askie/grix/backend/internal/model"
	"gorm.io/gorm"
)

func ReleaseTx(tx *gorm.DB, userID int64, now time.Time) error {
	if err := tx.Model(&model.User{}).
		Where("id = ?", userID).
		Updates(map[string]any{
			"phone_e164":    "",
			"phone_cipher":  "",
			"phone_last4":   "",
			"phone_blind":   "",
			"phone_country": "",
			"updated_at":    now,
		}).Error; err != nil {
		return err
	}
	return tx.Where("user_id = ? AND provider IN ?", userID,
		[]string{model.IdentityProviderPhoneSmsCN, model.IdentityProviderPhoneSmsGlobal}).
		Delete(&model.UserIdentity{}).Error
}
