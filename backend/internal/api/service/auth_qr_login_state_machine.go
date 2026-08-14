package service

import "github.com/askie/grix/backend/internal/model"

func isQRLoginTerminalStatus(status int16) bool {
	return status == model.AuthQRLoginStatusConsumed ||
		status == model.AuthQRLoginStatusCanceled ||
		status == model.AuthQRLoginStatusExpired
}

func toQRLoginStatusText(status int16) string {
	switch status {
	case model.AuthQRLoginStatusPendingScan:
		return "pending_scan"
	case model.AuthQRLoginStatusScanned:
		return "scanned"
	case model.AuthQRLoginStatusConfirmed:
		return "confirmed"
	case model.AuthQRLoginStatusConsumed:
		return "consumed"
	case model.AuthQRLoginStatusCanceled:
		return "canceled"
	case model.AuthQRLoginStatusExpired:
		return "expired"
	default:
		return "unknown"
	}
}
