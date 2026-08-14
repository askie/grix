package model

import "time"

// GroupQRCode stores the group-to-code mapping used by group invite QR links.
type GroupQRCode struct {
	SessionID     string    `gorm:"primaryKey;size:50" json:"session_id"`
	Code          string    `gorm:"size:64;not null;uniqueIndex" json:"code"`
	CreatorUserID int64     `gorm:"not null" json:"creator_user_id,string"`
	ExpiresAt     time.Time `gorm:"index;not null" json:"expires_at"`
	RotatedAt     time.Time `gorm:"not null" json:"rotated_at"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

func (GroupQRCode) TableName() string { return "group_qr_codes" }
