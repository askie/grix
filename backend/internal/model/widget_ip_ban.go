package model

import "time"

// WidgetIPBan 是 widget 访客来源 IP 的封禁规则，按 owner 维度生效
// （对该 owner 名下所有 widget 站点生效），与按 visitor_key 的会话封禁相互独立。
// IPCIDR 支持单 IP 或 CIDR 网段；ExpiresAt 为空表示永不过期，到期规则不再生效；
// 重复封禁同一 (OwnerUserID, IPCIDR) 走 upsert 刷新过期时间。
// Signature 是服务端 HMAC 签名（见 security.SignWidgetIPBan，同 AgentIPRule 先例）：
// 加载时逐条验签，签名不符的规则拒绝生效并告警，防止直改数据库篡改规则。
// 签名覆盖 (OwnerUserID, IPCIDR, ExpiresAt) 三个决定规则生效范围与时效的字段。
type WidgetIPBan struct {
	ID              int64      `gorm:"primaryKey" json:"id,string"`
	OwnerUserID     int64      `gorm:"not null;uniqueIndex:uq_widget_ip_bans_owner_ip,priority:1;index:idx_widget_ip_bans_owner_expires,priority:1" json:"owner_user_id,string"`
	IPCIDR          string     `gorm:"column:ip_cidr;size:64;not null;uniqueIndex:uq_widget_ip_bans_owner_ip,priority:2" json:"ip_cidr"`
	Reason          string     `gorm:"size:255;not null;default:''" json:"reason"`
	SourceSessionID string     `gorm:"size:64;not null;default:''" json:"source_session_id"`
	ExpiresAt       *time.Time `gorm:"index:idx_widget_ip_bans_owner_expires,priority:2" json:"expires_at"`
	Signature       string     `gorm:"size:128;not null;default:''" json:"-"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
}

func (WidgetIPBan) TableName() string { return "widget_ip_bans" }
