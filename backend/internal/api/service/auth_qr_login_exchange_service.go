package service

import (
	"errors"
	"strings"
	"time"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/userpref"
	"github.com/askie/grix/backend/internal/security"
	"github.com/askie/grix/backend/internal/store"
	"gorm.io/gorm"
)

func ExchangeQRLoginSession(sessionID, pollToken, deviceID, platform, language string) (*LoginResp, error) {
	normalizedSessionID := strings.TrimSpace(sessionID)
	normalizedPollToken := strings.TrimSpace(pollToken)
	if normalizedSessionID == "" || normalizedPollToken == "" {
		return nil, ErrQRLoginInvalidCode
	}

	pollTokenHash := hashQRLoginToken(normalizedPollToken)
	resp := &LoginResp{}
	var staleDevices []model.Device
	var loginUserID int64
	err := store.DB.Transaction(func(tx *gorm.DB) error {
		rec, err := getQRLoginSessionForUpdateBySessionAndPollTokenHash(tx, normalizedSessionID, pollTokenHash)
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrQRLoginInvalidCode
			}
			return err
		}

		now := time.Now().UTC()
		if markQRLoginExpiredIfNeeded(rec, now) {
			if err := tx.Save(rec).Error; err != nil {
				return err
			}
			return ErrQRLoginExpired
		}

		switch rec.Status {
		case model.AuthQRLoginStatusConfirmed:
		case model.AuthQRLoginStatusPendingScan, model.AuthQRLoginStatusScanned:
			return ErrQRLoginNotReady
		case model.AuthQRLoginStatusConsumed:
			return ErrQRLoginAlreadyConsumed
		case model.AuthQRLoginStatusCanceled:
			return ErrQRLoginCanceled
		case model.AuthQRLoginStatusExpired:
			return ErrQRLoginExpired
		default:
			return ErrQRLoginInvalidCode
		}

		if rec.ScanUserID == nil || *rec.ScanUserID <= 0 {
			return ErrQRLoginInvalidCode
		}

		var user model.User
		if err := tx.First(&user, *rec.ScanUserID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrQRLoginInvalidCode
			}
			return err
		}
		if err := security.EnsureUserActiveWithDB(tx, user.ID); err != nil {
			if err == security.ErrUserDisabled {
				return ErrQRLoginForbidden
			}
			return err
		}

		if err := updateUserPreferredLanguageTx(tx, user.ID, language); err != nil {
			return err
		}

		issueResp, stale, err := issueTokenWithDB(tx, user, deviceID, platform)
		if err != nil {
			return err
		}
		*resp = *issueResp
		staleDevices = stale
		loginUserID = user.ID

		rec.Status = model.AuthQRLoginStatusConsumed
		rec.ConsumedAt = &now
		rec.UpdatedAt = now
		if err := tx.Save(rec).Error; err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	cleanStaleDeviceCacheAfterCommit(loginUserID, deviceID, staleDevices)
	userpref.InvalidatePreferredLanguage(loginUserID)
	return resp, nil
}
