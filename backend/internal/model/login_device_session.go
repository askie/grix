package model

import "time"

type LoginDeviceSession struct {
	SessionID  string     `gorm:"primaryKey;size:64" json:"session_id"`
	UserID     int64      `gorm:"index:idx_login_device_sessions_user_revoked,priority:1;uniqueIndex:idx_login_device_sessions_user_device_active,priority:1,where:revoked_at IS NULL;not null" json:"user_id,string"`
	DeviceID   string     `gorm:"size:100;index:idx_login_device_sessions_device_id;uniqueIndex:idx_login_device_sessions_user_device_active,priority:2,where:revoked_at IS NULL;not null" json:"device_id"`
	Platform   string     `gorm:"size:32;not null" json:"platform"`
	LastSeenAt time.Time  `gorm:"not null" json:"last_seen_at"`
	RevokedAt  *time.Time `gorm:"index:idx_login_device_sessions_user_revoked,priority:2" json:"revoked_at,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`
	UpdatedAt  time.Time  `json:"updated_at"`
}

func (LoginDeviceSession) TableName() string { return "login_device_sessions" }
