package model

import (
	"strings"
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
	// FirstOrigin 是该站点第一个被访客实际接入的域名（规范化 host，不含协议和端口，
	// 小写）。只在首次访客初始化时写入一次，之后不再覆盖，用于客户端按接入站点
	// 分组访客会话；空字符串表示该站点还没有可用的接入域名。
	FirstOrigin   string         `gorm:"size:255;not null;default:''" json:"first_origin"`
	DisplayConfig datatypes.JSON `gorm:"type:jsonb;not null;default:'{}'" json:"display_config"`
	Status        int16          `gorm:"not null;default:1;index:idx_widget_sites_owner_status_updated,priority:2" json:"status"`
	CreatedAt     time.Time      `json:"created_at"`
	UpdatedAt     time.Time      `gorm:"index:idx_widget_sites_owner_status_updated,priority:3,sort:desc" json:"updated_at"`
}

func (WidgetSite) TableName() string { return "widget_sites" }

// VisitorDomain 是客户端展示/分组用的站点标识：优先该站点首个接入域名，
// 站点还没有记录域名时回退站点名。
func (s WidgetSite) VisitorDomain() string {
	if domain := strings.TrimSpace(s.FirstOrigin); domain != "" {
		return domain
	}
	return strings.TrimSpace(s.SiteName)
}
