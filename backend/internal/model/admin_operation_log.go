package model

import (
	"time"

	"gorm.io/datatypes"
)

type AdminOperationLog struct {
	ID         int64          `gorm:"primaryKey;autoIncrement" json:"id"`
	AdminID    int64          `gorm:"not null;index" json:"admin_id,string"`
	Action     string         `gorm:"size:64;not null;index" json:"action"`
	TargetType string         `gorm:"size:64;not null" json:"target_type"`
	TargetID   string         `gorm:"size:128;not null;default:''" json:"target_id"`
	Detail     datatypes.JSON `gorm:"type:jsonb;not null" json:"detail"`
	ClientIP   string         `gorm:"size:45;not null;default:''" json:"client_ip"`
	UserAgent  string         `gorm:"size:255;not null;default:''" json:"user_agent"`
	CreatedAt  time.Time      `json:"created_at"`
}

func (AdminOperationLog) TableName() string { return "admin_operation_logs" }
