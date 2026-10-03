package featuregate

import (
	"errors"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/store"
	"gorm.io/gorm"
)

const FeatureRegistration = "auth_register"

// RegistrationEnabled is the authoritative admission check for new accounts.
// It deliberately bypasses the process cache and stale-while-error snapshots.
// Missing rows and any status other than enabled deny registration; DB errors
// return false and an error. Callers must never admit on an error.
func RegistrationEnabled() (bool, error) {
	return ReadRegistrationEnabled(store.DB)
}

// ReadRegistrationEnabled also supports a caller's transaction (admin audit).
func ReadRegistrationEnabled(db *gorm.DB) (bool, error) {
	var gate model.FeatureGate
	err := db.Select("status").Where("key = ?", FeatureRegistration).Take(&gate).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return gate.Status == model.FeatureStatusEnabled, nil
}
