package model

import "time"

type MessageReaction struct {
	ID        int64     `gorm:"primaryKey;autoIncrement" json:"id,string"`
	MsgID     int64     `gorm:"not null" json:"msg_id,string"`
	SessionID string    `gorm:"size:50;not null" json:"session_id"`
	UserID    int64     `gorm:"not null" json:"user_id,string"`
	Emoji     string    `gorm:"size:32;not null" json:"emoji"`
	CreatedAt time.Time `json:"created_at"`
}

func (MessageReaction) TableName() string { return "message_reactions" }
