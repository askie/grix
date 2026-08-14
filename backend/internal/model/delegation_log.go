package model

import "time"

type DelegationLog struct {
	ID        int64     `gorm:"primaryKey" json:"id"`
	SessionID string    `gorm:"size:64;not null" json:"session_id"`
	UserID    int64     `gorm:"not null" json:"user_id"`
	AgentID   int64     `gorm:"not null" json:"agent_id"`
	Action    string    `gorm:"size:20;not null" json:"action"` // "start" or "stop"
	CreatedAt time.Time `json:"created_at"`
}

func (DelegationLog) TableName() string { return "delegation_logs" }
