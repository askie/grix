package model

import (
	"time"

	"gorm.io/datatypes"
)

const (
	AgentSessionSyncStatusQueued    = "queued"
	AgentSessionSyncStatusRunning   = "running"
	AgentSessionSyncStatusCompleted = "completed"
	AgentSessionSyncStatusFailed    = "failed"
	AgentSessionSyncStatusPartial   = "partial"
)

// AgentSessionSyncState tracks connector-native conversation history imports
// for one bound agent session.
type AgentSessionSyncState struct {
	ID           int64          `gorm:"primaryKey;autoIncrement" json:"id,string"`
	AgentID      int64          `gorm:"not null;uniqueIndex:idx_agent_session_sync_unique,priority:1;index:idx_agent_session_sync_lookup,priority:1" json:"agent_id,string"`
	OwnerID      int64          `gorm:"not null;index" json:"owner_id,string"`
	SessionID    string         `gorm:"size:64;not null;uniqueIndex:idx_agent_session_sync_unique,priority:2;index:idx_agent_session_sync_lookup,priority:2" json:"session_id"`
	ProviderKey  string         `gorm:"size:32;not null;default:'';uniqueIndex:idx_agent_session_sync_unique,priority:3" json:"provider_key"`
	BindingID    string         `gorm:"size:255;not null;default:'';uniqueIndex:idx_agent_session_sync_unique,priority:4" json:"binding_id"`
	SyncRunID    string         `gorm:"size:64;not null;default:''" json:"sync_run_id"`
	Status       string         `gorm:"size:32;not null;default:'queued';index" json:"status"`
	Cursor       string         `gorm:"size:2048;not null;default:''" json:"cursor"`
	LastError    string         `gorm:"size:1024;not null;default:''" json:"last_error"`
	Imported     int            `gorm:"not null;default:0" json:"imported"`
	MetaJSON     datatypes.JSON `gorm:"type:jsonb;not null;default:'{}'" json:"meta_json"`
	LastSyncedAt *time.Time     `json:"last_synced_at,omitempty"`
	StartedAt    *time.Time     `json:"started_at,omitempty"`
	CompletedAt  *time.Time     `json:"completed_at,omitempty"`
	CreatedAt    time.Time      `json:"created_at"`
	UpdatedAt    time.Time      `gorm:"index" json:"updated_at"`
}

func (AgentSessionSyncState) TableName() string { return "agent_session_sync_states" }

// AgentNativeMessageImport maps one provider-native message to an aibot
// message, giving history sync an idempotent sink independent of msg_id.
type AgentNativeMessageImport struct {
	ID              int64     `gorm:"primaryKey;autoIncrement" json:"id,string"`
	AgentID         int64     `gorm:"not null;uniqueIndex:idx_agent_native_msg_unique,priority:1;index:idx_agent_native_msg_session,priority:1" json:"agent_id,string"`
	ProviderKey     string    `gorm:"size:32;not null;default:'';uniqueIndex:idx_agent_native_msg_unique,priority:2" json:"provider_key"`
	BindingID       string    `gorm:"size:255;not null;default:'';uniqueIndex:idx_agent_native_msg_unique,priority:3" json:"binding_id"`
	NativeMessageID string    `gorm:"size:255;not null;default:'';uniqueIndex:idx_agent_native_msg_unique,priority:4" json:"native_message_id"`
	SessionID       string    `gorm:"size:64;not null;index:idx_agent_native_msg_session,priority:2" json:"session_id"`
	MsgID           int64     `gorm:"not null;index" json:"msg_id,string"`
	NativeCreatedAt time.Time `json:"native_created_at"`
	CreatedAt       time.Time `json:"created_at"`
}

func (AgentNativeMessageImport) TableName() string { return "agent_native_message_imports" }
