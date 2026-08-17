package model

import "time"

// UserPeerMute stores per-viewer mute state for a human or agent peer.
// It is independent of session_members.is_muted: new private threads with
// the same peer inherit this flag without copying session-level mute.
type UserPeerMute struct {
	ID         int64      `gorm:"primaryKey" json:"id,string"`
	UserID     int64      `gorm:"uniqueIndex:idx_user_peer_mute;not null" json:"user_id,string"`
	PeerUserID int64      `gorm:"uniqueIndex:idx_user_peer_mute;not null" json:"peer_user_id,string"`
	IsMuted    bool       `gorm:"default:false" json:"is_muted"`
	MutedAt    *time.Time `json:"muted_at"`
	CreatedAt  time.Time  `json:"created_at"`
	UpdatedAt  time.Time  `json:"updated_at"`
}

func (UserPeerMute) TableName() string { return "user_peer_mutes" }
