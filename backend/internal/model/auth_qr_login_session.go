package model

import "time"

const (
	AuthQRLoginStatusPendingScan int16 = 0
	AuthQRLoginStatusScanned     int16 = 1
	AuthQRLoginStatusConfirmed   int16 = 2
	AuthQRLoginStatusConsumed    int16 = 3
	AuthQRLoginStatusCanceled    int16 = 4
	AuthQRLoginStatusExpired     int16 = 5
)

const (
	AuthQRLoginSceneWebDesktop int16 = 1
)

type AuthQRLoginSession struct {
	SessionID          string     `gorm:"primaryKey;size:64" json:"session_id"`
	QRTokenHash        string     `gorm:"size:64;uniqueIndex;not null" json:"-"`
	PollTokenHash      string     `gorm:"size:64;uniqueIndex;not null" json:"-"`
	Status             int16      `gorm:"index:idx_auth_qr_login_status_expires,priority:1;not null;default:0" json:"status"`
	Scene              int16      `gorm:"not null;default:1" json:"scene"`
	RequestIP          string     `gorm:"size:45;not null;default:''" json:"request_ip"`
	RequestUserAgent   string     `gorm:"size:255;not null;default:''" json:"request_user_agent"`
	RequestDeviceLabel string     `gorm:"size:120;not null;default:''" json:"request_device_label"`
	ScanUserID         *int64     `gorm:"index:idx_auth_qr_login_scan_user_status,priority:1" json:"scan_user_id,omitempty,string"`
	ScannedAt          *time.Time `json:"scanned_at,omitempty"`
	ConfirmedAt        *time.Time `json:"confirmed_at,omitempty"`
	ConsumedAt         *time.Time `json:"consumed_at,omitempty"`
	CanceledAt         *time.Time `json:"canceled_at,omitempty"`
	ExpiresAt          time.Time  `gorm:"index:idx_auth_qr_login_status_expires,priority:2;not null" json:"expires_at"`
	CreatedAt          time.Time  `json:"created_at"`
	UpdatedAt          time.Time  `json:"updated_at"`
}

func (AuthQRLoginSession) TableName() string { return "auth_qr_login_sessions" }
