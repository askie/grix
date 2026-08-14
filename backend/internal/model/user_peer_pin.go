package model

import "time"

// UserPeerPin stores per-viewer pin state for a human peer, regardless of
// whether the peer is a friend or a visitor/customer user.
type UserPeerPin struct {
	ID         int64      `gorm:"primaryKey" json:"id,string"`
	UserID     int64      `gorm:"uniqueIndex:idx_user_peer_pin;not null" json:"user_id,string"`
	PeerUserID int64      `gorm:"uniqueIndex:idx_user_peer_pin;not null" json:"peer_user_id,string"`
	IsPinned   bool       `gorm:"default:false" json:"is_pinned"`
	PinnedAt   *time.Time `json:"pinned_at"`
	CreatedAt  time.Time  `json:"created_at"`
	UpdatedAt  time.Time  `json:"updated_at"`
}

func (UserPeerPin) TableName() string { return "user_peer_pins" }
