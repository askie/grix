package model

import "time"

type OAuthAccount struct {
	ID          int64     `gorm:"primaryKey" json:"id,string"`
	UserID      int64     `gorm:"index;not null" json:"user_id,string"`
	Provider    string    `gorm:"size:50;index;uniqueIndex:idx_oauth_accounts_provider_uid_unique;not null" json:"provider"` // e.g., 'google', 'apple'
	ProviderUID string    `gorm:"size:255;index;uniqueIndex:idx_oauth_accounts_provider_uid_unique;not null" json:"provider_uid"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

func (OAuthAccount) TableName() string { return "oauth_accounts" }
