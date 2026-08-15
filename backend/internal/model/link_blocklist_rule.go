package model

import "time"

// LinkBlocklistRule 链接黑名单规则（用于点击时校验）。
// 链接安全黑名单规则。
type LinkBlocklistRule struct {
	ID        int64     `gorm:"primaryKey;autoIncrement" json:"id"`
	Kind      string    `gorm:"size:16;not null;index:idx_link_blocklist_kind;uniqueIndex:uq_link_blocklist_kind_value,priority:1" json:"kind"`
	Value     string    `gorm:"size:512;not null;uniqueIndex:uq_link_blocklist_kind_value,priority:2" json:"value"`
	Severity  string    `gorm:"size:16;not null;default:'malicious'" json:"severity"`
	Source    string    `gorm:"size:32;not null;default:'manual'" json:"source"`
	Enabled   bool      `gorm:"not null;default:true;index:idx_link_blocklist_enabled" json:"enabled"`
	Note      string    `gorm:"size:256" json:"note"`
	HitCount  int64     `gorm:"not null;default:0" json:"hit_count"`
	LastHitAt *time.Time `gorm:"index" json:"last_hit_at,omitempty"`
	CreatedBy *int64    `json:"created_by,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func (LinkBlocklistRule) TableName() string { return "link_blocklist_rules" }
