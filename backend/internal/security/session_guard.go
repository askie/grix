package security

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/store"
	"gorm.io/gorm"
)

func IsLoginSessionRevoked(userID int64, sessionID string) bool {
	if userID <= 0 {
		return false
	}

	normalizedSessionID := strings.TrimSpace(sessionID)
	if normalizedSessionID == "" {
		return false
	}

	if store.RDB != nil {
		exists, err := store.RDB.Exists(
			context.Background(),
			revokedLoginSessionKey(userID, normalizedSessionID),
		).Result()
		if err == nil && exists > 0 {
			return true
		}
	}

	if store.DB == nil {
		return false
	}

	var found model.LoginDeviceSession
	err := store.DB.
		Select("session_id", "revoked_at").
		Where("user_id = ? AND session_id = ?", userID, normalizedSessionID).
		First(&found).Error
	if err != nil {
		return false
	}
	if found.RevokedAt == nil {
		return false
	}

	_ = MarkLoginSessionRevoked(userID, normalizedSessionID)
	return true
}

func MarkLoginSessionRevoked(userID int64, sessionID string) error {
	if store.RDB == nil || userID <= 0 {
		return nil
	}

	normalizedSessionID := strings.TrimSpace(sessionID)
	if normalizedSessionID == "" {
		return nil
	}

	return store.RDB.Set(
		context.Background(),
		revokedLoginSessionKey(userID, normalizedSessionID),
		"1",
		0,
	).Err()
}

func IsLoginSessionRevokedWithDB(db *gorm.DB, userID int64, sessionID string) (bool, error) {
	if db == nil {
		return false, ErrDBUnavailable
	}
	if userID <= 0 {
		return false, nil
	}

	normalizedSessionID := strings.TrimSpace(sessionID)
	if normalizedSessionID == "" {
		return false, nil
	}

	var found model.LoginDeviceSession
	err := db.
		Select("session_id", "revoked_at").
		Where("user_id = ? AND session_id = ?", userID, normalizedSessionID).
		First(&found).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return false, nil
		}
		return false, err
	}

	return found.RevokedAt != nil, nil
}

func revokedLoginSessionKey(userID int64, sessionID string) string {
	return fmt.Sprintf("auth:revoked:session:%d:%s", userID, sessionID)
}
