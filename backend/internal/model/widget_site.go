package model

import (
	"time"

	"gorm.io/datatypes"
)

const (
	WidgetSiteStatusActive   int16 = 1
	WidgetSiteStatusDisabled int16 = 2
)

type WidgetSite struct {
	ID             int64          `gorm:"primaryKey" json:"id,string"`
	OwnerUserID    int64          `gorm:"not null;index:idx_widget_sites_owner_status_updated,priority:1" json:"owner_user_id,string"`
	SiteKey        string         `gorm:"size:64;not null;uniqueIndex" json:"site_key"`
	SiteSecretHash string         `gorm:"size:255;not null" json:"-"`
	SiteName       string         `gorm:"size:255;not null" json:"site_name"`
	AllowedOrigins datatypes.JSON `gorm:"type:jsonb;not null;default:'[]'" json:"allowed_origins"`
	DisplayConfig  datatypes.JSON `gorm:"type:jsonb;not null;default:'{}'" json:"display_config"`
	Status         int16          `gorm:"not null;default:1;index:idx_widget_sites_owner_status_updated,priority:2" json:"status"`
	CreatedAt      time.Time      `json:"created_at"`
	UpdatedAt      time.Time      `gorm:"index:idx_widget_sites_owner_status_updated,priority:3,sort:desc" json:"updated_at"`
}

func (WidgetSite) TableName() string { return "widget_sites" }
