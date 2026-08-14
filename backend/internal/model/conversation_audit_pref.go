package model

import "time"

// ConversationAuditPref stores the server-side conversation-audit toggle per
// (user, agent). Once enabled, every session targeting that agent audits.
// A missing row means disabled.
type ConversationAuditPref struct {
	UserID    int64     `gorm:"primaryKey" json:"user_id,string"`
	AgentID   int64     `gorm:"primaryKey" json:"agent_id,string"`
	Enabled   bool      `gorm:"not null;default:false" json:"enabled"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func (ConversationAuditPref) TableName() string { return "conversation_audit_prefs" }
