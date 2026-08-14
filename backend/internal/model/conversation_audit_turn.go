package model

import "time"

// ConversationAuditTurn stores correlation and lifecycle metadata for one
// user-selected audited turn. Replay bodies remain exclusively in the
// connector's local audit store.
type ConversationAuditTurn struct {
	ID           int64     `gorm:"primaryKey" json:"id,string"`
	OwnerID      int64     `gorm:"not null;index:idx_conversation_audit_turn_owner_msg,priority:1" json:"owner_id,string"`
	AgentID      int64     `gorm:"not null;uniqueIndex:idx_conversation_audit_turn_agent_event,priority:1;index:idx_conversation_audit_turn_owner_msg,priority:3" json:"agent_id,string"`
	SessionID    string    `gorm:"size:64;not null;index:idx_conversation_audit_turn_owner_msg,priority:2" json:"session_id"`
	MsgID        int64     `gorm:"not null;index:idx_conversation_audit_turn_owner_msg,priority:3" json:"msg_id,string"`
	EventID      string    `gorm:"size:191;not null;uniqueIndex:idx_conversation_audit_turn_agent_event,priority:2" json:"event_id"`
	AuditID      string    `gorm:"size:191;not null;default:'';index" json:"audit_id,omitempty"`
	TurnID       string    `gorm:"size:191;not null;default:'';index" json:"turn_id,omitempty"`
	State        string    `gorm:"size:32;not null" json:"state"`
	Revision     int       `gorm:"not null;default:0" json:"revision"`
	Quality      string    `gorm:"size:32;not null;default:''" json:"quality,omitempty"`
	Truncated    bool      `gorm:"not null;default:false" json:"truncated"`
	ErrorCode    string    `gorm:"size:96;not null;default:''" json:"error_code,omitempty"`
	ErrorMessage string    `gorm:"size:512;not null;default:''" json:"error_message,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

func (ConversationAuditTurn) TableName() string { return "conversation_audit_turns" }
