package model

import "time"

const (
	UserInboxEventKindMessage = "message"
	UserInboxEventKindRevoke  = "revoke"
	UserInboxEventKindEdit    = "edit"
)

type UserInbox struct {
	UserID    int64     `gorm:"primaryKey" json:"user_id,string"`
	InboxSeq  int64     `gorm:"primaryKey" json:"inbox_seq,string"`
	MsgID     int64     `gorm:"not null" json:"msg_id,string"`
	SessionID string    `gorm:"size:50;not null" json:"session_id"`
	EventKind string    `gorm:"size:16;not null;default:message" json:"event_kind,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

func (UserInbox) TableName() string { return "user_inbox" }
