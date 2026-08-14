package service

import (
	"errors"
	"strings"
	"time"

	"github.com/askie/grix/backend/internal/store"
	"gorm.io/gorm"
)

func QueryQRLoginStatus(sessionID, pollToken string) (*QRLoginStatusResp, error) {
	normalizedSessionID := strings.TrimSpace(sessionID)
	normalizedPollToken := strings.TrimSpace(pollToken)
	if normalizedSessionID == "" || normalizedPollToken == "" {
		return nil, ErrQRLoginInvalidCode
	}

	pollTokenHash := hashQRLoginToken(normalizedPollToken)
	resp := &QRLoginStatusResp{}
	var scanUserID int64
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
		}

		resp.Status = toQRLoginStatusText(rec.Status)
		resp.ExpiresIn = calcQRLoginExpiresIn(now, rec.ExpiresAt)
		if rec.ScanUserID != nil {
			scanUserID = *rec.ScanUserID
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	if scanUserID > 0 && (resp.Status == "scanned" || resp.Status == "confirmed") {
		scannerUser, err := loadQRLoginScannerUser(scanUserID)
		if err == nil {
			resp.ScannerUser = scannerUser
		}
	}

	return resp, nil
}
