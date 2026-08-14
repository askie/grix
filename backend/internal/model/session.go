package model

import "time"

const (
	SessionTypeDirect int16 = 1
	SessionTypeGroup  int16 = 2
)

const (
	SessionModerationStatusActive int16 = 1
	SessionModerationStatusBanned int16 = 2
)

type Session struct {
	SessionID         string     `gorm:"primaryKey;size:50" json:"session_id"`
	DirectKey         *string    `gorm:"size:128;index:idx_sessions_direct_key" json:"-"`
	OwnerID           int64      `gorm:"not null" json:"owner_id,string"`
	SessionType       int16      `gorm:"default:1" json:"session_type"` // 1:单聊 2:群聊
	GroupName         string     `gorm:"size:255" json:"group_name"`
	AllowMemberInvite bool       `gorm:"not null;default:true" json:"allow_member_invite"`
	AllMembersMuted   bool       `gorm:"not null;default:false" json:"all_members_muted"`
	LastMsgID         *int64     `json:"last_msg_id,string"`
	LastMsgSummary    string     `gorm:"size:255" json:"last_msg_summary"`
	ModerationStatus  int16      `gorm:"not null;default:1;index" json:"moderation_status"`
	BannedReason      string     `gorm:"size:255;not null;default:''" json:"banned_reason"`
	BannedAt          *time.Time `json:"banned_at,omitempty"`
	BannedBy          *int64     `json:"banned_by,omitempty"`
	IsDeleted         bool       `gorm:"default:false" json:"is_deleted"`
	CreatedAt         time.Time  `json:"created_at"`
	UpdatedAt         time.Time  `gorm:"index" json:"updated_at"`
}

func (Session) TableName() string { return "sessions" }
