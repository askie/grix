package model

import (
	"time"

	"gorm.io/datatypes"
)

type AgentSessionBinding struct {
	AgentID      int64          `gorm:"primaryKey" json:"agent_id,string"`
	SessionID    string         `gorm:"primaryKey;size:64" json:"session_id"`
	ProviderKey  string         `gorm:"size:32;not null;default:''" json:"provider_key"`
	BindingID    string         `gorm:"size:255;not null;default:''" json:"binding_id"`
	Cwd          string         `gorm:"size:2048;not null;default:''" json:"cwd"`
	Status       string         `gorm:"size:64;not null;default:''" json:"status"`
	WorkerStatus string         `gorm:"size:64;not null;default:''" json:"worker_status"`
	MetaJSON     datatypes.JSON `gorm:"type:jsonb;not null;default:'{}'" json:"meta_json"`
	CreatedAt    time.Time      `json:"created_at"`
	UpdatedAt    time.Time      `json:"updated_at"`
}

func (AgentSessionBinding) TableName() string { return "agent_session_bindings" }
