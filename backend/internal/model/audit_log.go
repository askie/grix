package model

import (
	"time"

	"gorm.io/datatypes"
)

type AuditLog struct {
	ID        int64          `gorm:"primaryKey;autoIncrement" json:"id"`
	EventType string         `gorm:"size:50;not null" json:"event_type"`
	UserID    *int64         `json:"user_id"`
	SessionID *string        `gorm:"size:50" json:"session_id"`
	MsgID     *int64         `json:"msg_id"`
	Detail    datatypes.JSON `gorm:"type:jsonb" json:"detail"`
	ClientIP  string         `gorm:"size:45" json:"client_ip"`
	UserAgent string         `gorm:"size:255" json:"user_agent"`
	CreatedAt time.Time      `json:"created_at"`
}

func (AuditLog) TableName() string { return "audit_logs" }
