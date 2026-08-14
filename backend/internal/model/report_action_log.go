package model

import (
	"time"

	"gorm.io/datatypes"
)

type ReportActionLog struct {
	ID        int64          `gorm:"primaryKey;autoIncrement" json:"id,string"`
	ReportID  int64          `gorm:"not null;index" json:"report_id,string"`
	AdminID   int64          `gorm:"not null;index" json:"admin_id,string"`
	Action    string         `gorm:"size:32;not null" json:"action"`
	Detail    datatypes.JSON `gorm:"type:jsonb;not null" json:"detail"`
	CreatedAt time.Time      `json:"created_at"`
}

func (ReportActionLog) TableName() string { return "report_action_logs" }
