package service

import (
	"time"

	"github.com/askie/grix/backend/internal/model"
)

const (
	qrLoginSessionTTL      = 2 * time.Minute
	qrLoginPollIntervalMS  = int64(1000)
	qrLoginTokenByteLength = 24
)

func calcQRLoginExpiresIn(now time.Time, expiresAt time.Time) int64 {
	if !expiresAt.After(now) {
		return 0
	}
	return int64(time.Until(expiresAt).Seconds())
}

func markQRLoginExpiredIfNeeded(rec *model.AuthQRLoginSession, now time.Time) bool {
	if rec == nil {
		return false
	}
	if rec.ExpiresAt.After(now) {
		return false
	}
	if isQRLoginTerminalStatus(rec.Status) {
		return false
	}
	rec.Status = model.AuthQRLoginStatusExpired
	rec.UpdatedAt = now
	rec.CanceledAt = nil
	return true
}
