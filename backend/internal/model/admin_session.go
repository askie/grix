package model

import "time"

type AdminSession struct {
	SessionID  string     `gorm:"primaryKey;size:64" json:"session_id"`
	AdminID    int64      `gorm:"not null;index" json:"admin_id,string"`
	ExpiresAt  time.Time  `gorm:"not null;index" json:"expires_at"`
	RevokedAt  *time.Time `json:"revoked_at,omitempty"`
	ClientIP   string     `gorm:"size:45;not null;default:''" json:"client_ip"`
	UserAgent  string     `gorm:"size:255;not null;default:''" json:"user_agent"`
	LastSeenAt time.Time  `gorm:"not null" json:"last_seen_at"`
	CreatedAt  time.Time  `json:"created_at"`
	UpdatedAt  time.Time  `json:"updated_at"`
}

func (AdminSession) TableName() string { return "admin_sessions" }
