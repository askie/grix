package model

import (
	"time"

	"gorm.io/datatypes"
)

type AgentQueuedEvent struct {
	ID               int64          `gorm:"primaryKey" json:"id,string"`
	AgentID          int64          `gorm:"not null;index:idx_agent_queued_events_agent_created,priority:1;index:idx_agent_queued_events_agent_owner_created,priority:1" json:"agent_id,string"`
	OwnerID          int64          `gorm:"not null;default:0;index:idx_agent_queued_events_agent_owner_created,priority:2" json:"owner_id,string"`
	Cmd              string         `gorm:"size:32;not null" json:"cmd"`
	EventKey         string         `gorm:"size:191;not null;uniqueIndex" json:"event_key"`
	Payload          datatypes.JSON `gorm:"type:jsonb;not null" json:"payload"`
	DispatchAttempts int            `gorm:"not null;default:0" json:"dispatch_attempts"`
	DispatchedAt     *time.Time     `gorm:"index" json:"dispatched_at,omitempty"`
	CreatedAt        time.Time      `gorm:"index:idx_agent_queued_events_agent_created,priority:2;index:idx_agent_queued_events_agent_owner_created,priority:3" json:"created_at"`
	UpdatedAt        time.Time      `json:"updated_at"`
}

func (AgentQueuedEvent) TableName() string { return "agent_queued_events" }
