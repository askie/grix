package model

import "time"

type WebhookEndpoint struct {
	ID          int64      `gorm:"primaryKey" json:"id,string"`
	UserID      int64      `gorm:"not null;index:idx_webhook_session_user_active,priority:2" json:"user_id,string"`
	SessionID   string     `gorm:"size:50;not null;index:idx_webhook_session_user_active,priority:1" json:"session_id"`
	TokenHash   string     `gorm:"size:128;not null;uniqueIndex" json:"-"`
	TokenValue  string     `gorm:"size:255;not null" json:"-"`
	TokenPrefix string     `gorm:"size:16;not null" json:"token_prefix"`
	ExpiresAt   *time.Time `json:"expires_at,omitempty"`
	LastUsedAt  *time.Time `json:"last_used_at,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
	DeletedAt   *time.Time `gorm:"index" json:"-"`
}

func (WebhookEndpoint) TableName() string { return "webhook_endpoints" }
