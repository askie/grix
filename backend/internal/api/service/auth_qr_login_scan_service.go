package service

import (
	"errors"
	"time"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/store"
	"gorm.io/gorm"
)

func ScanQRLoginSession(scanUserID int64, rawPayload string) (*QRLoginScanResp, error) {
	if scanUserID <= 0 {
		return nil, ErrQRLoginForbidden
	}

	sessionID, qrToken, qrRegion, err := parseQRLoginText(rawPayload)
	if err != nil {
		return nil, err
	}
	// 二维码携带了区域标识且与本区不符：说明扫的是另一区域网页的登录码，
	// 直接给出明确错误，避免落到查库 not found 的笼统"二维码无效"。
	// 旧版二维码不带区域标识（qrRegion 为空），跳过该检查。
	if qrRegion != "" && normalizeQRLoginRegion(qrRegion) != qrLoginDeployRegion() {
		return nil, ErrQRLoginRegionMismatch
	}
	qrTokenHash := hashQRLoginToken(qrToken)

	resp := &QRLoginScanResp{}
	err = store.DB.Transaction(func(tx *gorm.DB) error {
		rec, err := getQRLoginSessionForUpdateBySessionAndQRTokenHash(tx, sessionID, qrTokenHash)
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
		case model.AuthQRLoginStatusPendingScan:
			rec.Status = model.AuthQRLoginStatusScanned
			rec.ScanUserID = &scanUserID
			rec.ScannedAt = &now
			rec.UpdatedAt = now
			if err := tx.Save(rec).Error; err != nil {
				return err
			}
		case model.AuthQRLoginStatusScanned:
			if rec.ScanUserID == nil || *rec.ScanUserID != scanUserID {
				return ErrQRLoginAlreadyScannedByPeer
			}
		case model.AuthQRLoginStatusConfirmed:
			return ErrQRLoginAlreadyConfirmed
		case model.AuthQRLoginStatusConsumed:
			return ErrQRLoginAlreadyConsumed
		case model.AuthQRLoginStatusCanceled:
			return ErrQRLoginCanceled
		case model.AuthQRLoginStatusExpired:
			return ErrQRLoginExpired
		default:
			return ErrQRLoginInvalidCode
		}

		resp.QRSessionID = rec.SessionID
		resp.Status = toQRLoginStatusText(rec.Status)
		resp.ExpiresIn = calcQRLoginExpiresIn(now, rec.ExpiresAt)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return resp, nil
}
