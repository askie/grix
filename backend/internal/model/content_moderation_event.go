package model

import (
	"time"

	"gorm.io/datatypes"
)

type ContentModerationEvent struct {
	ID              int64          `gorm:"primaryKey;autoIncrement" json:"id"`
	SessionID       string         `gorm:"size:50;not null;index:idx_content_moderation_events_sender" json:"session_id"`
	MsgID           int64          `gorm:"not null;uniqueIndex:idx_content_moderation_events_msg" json:"msg_id,string"`
	SenderID        int64          `gorm:"not null;index:idx_content_moderation_events_sender" json:"sender_id,string"`
	SenderType      int16          `gorm:"not null;index:idx_content_moderation_events_sender" json:"sender_type"`
	MatchedKeywords datatypes.JSON `gorm:"type:jsonb;not null" json:"matched_keywords"`
	RecallStatus    string         `gorm:"size:32;not null;default:'pending';index:idx_content_moderation_events_retry,priority:1" json:"recall_status"`
	RecallAttempts  int            `gorm:"not null;default:0" json:"recall_attempts"`
	NextRetryAt     *time.Time     `gorm:"index:idx_content_moderation_events_retry,priority:2" json:"next_retry_at,omitempty"`
	HitCount        int            `gorm:"not null;default:0" json:"hit_count"`
	MuteApplied     bool           `gorm:"not null;default:false" json:"mute_applied"`
	CreatedAt       time.Time      `json:"created_at"`
}

func (ContentModerationEvent) TableName() string { return "content_moderation_events" }
