package model

import (
	"time"

	"gorm.io/datatypes"
)

const (
	ReportTargetTypeUser  int16 = 1
	ReportTargetTypeGroup int16 = 2
)

const (
	ReportStatusPending  int16 = 1
	ReportStatusReview   int16 = 2
	ReportStatusResolved int16 = 3
)

const (
	ReportResolutionUnset     int16 = 0
	ReportResolutionReject    int16 = 1
	ReportResolutionBanUser   int16 = 2
	ReportResolutionBanGroup  int16 = 3
	ReportResolutionNoAction  int16 = 4
	ReportResolutionDuplicate int16 = 5
)

type Report struct {
	ID               int64          `gorm:"primaryKey;autoIncrement" json:"id,string"`
	ReporterUserID   int64          `gorm:"not null;index" json:"reporter_user_id,string"`
	TargetType       int16          `gorm:"not null" json:"target_type"`
	TargetUserID     int64          `gorm:"not null;default:0;index" json:"target_user_id,string"`
	TargetSessionID  string         `gorm:"size:50;not null;default:'';index" json:"target_session_id"`
	SourceSessionID  string         `gorm:"size:50;not null;default:''" json:"source_session_id"`
	ReasonCode       string         `gorm:"size:32;not null" json:"reason_code"`
	Description      string         `gorm:"size:500;not null;default:''" json:"description"`
	Status           int16          `gorm:"not null;default:1;index" json:"status"`
	Resolution       int16          `gorm:"not null;default:0" json:"resolution"`
	AssignedAdminID  *int64         `json:"assigned_admin_id,string,omitempty"`
	ResolvedAdminID  *int64         `json:"resolved_admin_id,string,omitempty"`
	ResolvedNote     string         `gorm:"size:500;not null;default:''" json:"resolved_note"`
	ReporterSnapshot datatypes.JSON `gorm:"type:jsonb;not null" json:"reporter_snapshot"`
	TargetSnapshot   datatypes.JSON `gorm:"type:jsonb;not null" json:"target_snapshot"`
	ResolvedAt       *time.Time     `json:"resolved_at,omitempty"`
	CreatedAt        time.Time      `json:"created_at"`
	UpdatedAt        time.Time      `json:"updated_at"`
}

func (Report) TableName() string { return "reports" }
