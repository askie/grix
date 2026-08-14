package service

import (
	"errors"
	"strings"
	"time"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/store"
	"gorm.io/gorm"
)

func ConfirmQRLoginSession(confirmUserID int64, sessionID string, approve bool) (*QRLoginConfirmResp, error) {
	if confirmUserID <= 0 {
		return nil, ErrQRLoginForbidden
	}
	normalizedSessionID := strings.TrimSpace(sessionID)
	if normalizedSessionID == "" {
		return nil, ErrQRLoginInvalidCode
	}

	resp := &QRLoginConfirmResp{}
	err := store.DB.Transaction(func(tx *gorm.DB) error {
		rec, err := getQRLoginSessionForUpdateBySessionID(tx, normalizedSessionID)
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
		if rec.ScanUserID == nil || *rec.ScanUserID != confirmUserID {
			return ErrQRLoginForbidden
		}

		switch rec.Status {
		case model.AuthQRLoginStatusScanned:
			rec.UpdatedAt = now
			if approve {
				rec.Status = model.AuthQRLoginStatusConfirmed
				rec.ConfirmedAt = &now
			} else {
				rec.Status = model.AuthQRLoginStatusCanceled
				rec.CanceledAt = &now
			}
			if err := tx.Save(rec).Error; err != nil {
				return err
			}
		case model.AuthQRLoginStatusConfirmed:
			if !approve {
				return ErrQRLoginAlreadyConfirmed
			}
		case model.AuthQRLoginStatusPendingScan:
			return ErrQRLoginNotReady
		case model.AuthQRLoginStatusCanceled:
			return ErrQRLoginCanceled
		case model.AuthQRLoginStatusConsumed:
			return ErrQRLoginAlreadyConsumed
		case model.AuthQRLoginStatusExpired:
			return ErrQRLoginExpired
		default:
			return ErrQRLoginInvalidCode
		}

		resp.QRSessionID = rec.SessionID
		resp.Status = toQRLoginStatusText(rec.Status)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return resp, nil
}
