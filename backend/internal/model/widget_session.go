package model

import "time"

const (
	WidgetSessionStatusActive int16 = 1
	WidgetSessionStatusClosed int16 = 2
	WidgetSessionStatusBanned int16 = 3
)

type WidgetSession struct {
	ID               int64     `gorm:"primaryKey" json:"id,string"`
	SiteID           int64     `gorm:"not null;index:idx_widget_sessions_site_visitor_status,priority:1;index:idx_widget_sessions_site_init_ip,priority:1" json:"site_id,string"`
	OwnerUserID      int64     `gorm:"not null;index:idx_widget_sessions_owner_status_updated,priority:1" json:"owner_user_id,string"`
	VisitorID        int64     `gorm:"not null;index:idx_widget_sessions_visitor_status,priority:1" json:"visitor_id,string"`
	VisitorKey       string    `gorm:"size:128;not null;index:idx_widget_sessions_site_visitor_status,priority:2" json:"visitor_key"`
	SessionID        string    `gorm:"size:50;not null;uniqueIndex" json:"session_id"`
	VisitorName      string    `gorm:"size:255;not null;default:''" json:"visitor_name"`
	VisitorEmail     string    `gorm:"size:255;not null;default:''" json:"visitor_email"`
	// Locale 是访客浏览器语言归一化后的结果（见 pkg/locale.Normalize），
	// 用于该会话后续发起的语音通话选取对应语言的开场白/system prompt 语言。
	Locale           string    `gorm:"size:16;not null;default:''" json:"locale"`
	LastPageURL      string    `gorm:"type:text;not null;default:''" json:"last_page_url"`
	LastInitIPPrefix string    `gorm:"size:64;not null;default:'';index:idx_widget_sessions_site_init_ip,priority:2" json:"last_init_ip_prefix"`
	// LastInitIP 是访客最近一次 init 的完整 IP（归一化字符串，空表示未知），
	// ban 访客时据此自动写入 owner 维度的 IP 封禁（见 security.BanWidgetIP）。
	// 完整 IP 属敏感数据，不序列化出接口（json:"-"）。
	LastInitIP       string    `gorm:"size:64;not null;default:''" json:"-"`
	Status           int16     `gorm:"not null;default:1;index:idx_widget_sessions_site_visitor_status,priority:3;index:idx_widget_sessions_owner_status_updated,priority:2;index:idx_widget_sessions_visitor_status,priority:2" json:"status"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `gorm:"index:idx_widget_sessions_owner_status_updated,priority:3,sort:desc" json:"updated_at"`
	LastActiveAt     time.Time `json:"last_active_at"`
	LastInitAt       time.Time `gorm:"index:idx_widget_sessions_site_init_ip,priority:3,sort:desc" json:"last_init_at"`
}

func (WidgetSession) TableName() string { return "widget_sessions" }
