package model

import "time"

type SessionMember struct {
	SessionID                string     `gorm:"primaryKey;size:50" json:"session_id"`
	MemberID                 int64      `gorm:"primaryKey" json:"member_id,string"`
	MemberType               int16      `gorm:"primaryKey;default:1" json:"member_type"` // 1:人类 2:智能体
	CustomTitle              string     `gorm:"size:255" json:"custom_title"`
	GroupNickname            string     `gorm:"size:255" json:"group_nickname"`
	AgentReceiveMode         int16      `gorm:"not null;default:1" json:"agent_receive_mode"`
	AgentReceiveBacklogCount int        `gorm:"column:agent_receive_backlog_count;not null;default:8" json:"agent_receive_backlog_count"`
	IsPinned                 bool       `gorm:"default:false" json:"is_pinned"`
	IsMuted                  bool       `gorm:"default:false" json:"is_muted"`
	IsSpeakMuted             bool       `gorm:"default:false" json:"is_speak_muted"`
	CanSpeakWhenAllMuted     bool       `gorm:"default:false" json:"can_speak_when_all_muted"`
	PinnedAt                 *time.Time `json:"pinned_at"`
	Role                     int16      `gorm:"default:1" json:"role"` // 1:普通 2:管理员 3:创建者
	UnreadCount              int        `gorm:"default:0" json:"unread_count"`
	LastReadMsgID            int64      `gorm:"default:0" json:"last_read_msg_id,string"`
	LastActiveAt             time.Time  `gorm:"index" json:"last_active_at"`
	JoinedAt                 time.Time  `json:"joined_at"`
}

func (SessionMember) TableName() string { return "session_members" }
