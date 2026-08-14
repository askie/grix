package model

import "time"

// FriendQRCode stores the owner-to-code mapping used by add-friend QR links.
type FriendQRCode struct {
	UserID    int64     `gorm:"primaryKey;autoIncrement:false" json:"user_id,string"`
	Code      string    `gorm:"size:64;not null;uniqueIndex" json:"code"`
	RotatedAt time.Time `gorm:"not null" json:"rotated_at"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func (FriendQRCode) TableName() string { return "friend_qr_codes" }
