package model

import "time"

// AgentSessionRouteMapping stores OpenClaw route_session_key to Aibot session_id bindings.
// One session_id can map to multiple route keys (one-to-many).
type AgentSessionRouteMapping struct {
	ID              int64     `gorm:"primaryKey;autoIncrement" json:"id,string"`
	AgentID         int64     `gorm:"not null;uniqueIndex:idx_agent_route_mapping_unique,priority:1;index:idx_agent_route_mapping_session,priority:1" json:"agent_id,string"`
	OwnerID         int64     `gorm:"not null;index:idx_agent_route_mapping_owner" json:"owner_id,string"`
	Channel         string    `gorm:"size:32;not null;uniqueIndex:idx_agent_route_mapping_unique,priority:2;index:idx_agent_route_mapping_session,priority:2" json:"channel"`
	AccountID       string    `gorm:"size:64;not null;uniqueIndex:idx_agent_route_mapping_unique,priority:3;index:idx_agent_route_mapping_session,priority:3" json:"account_id"`
	RouteSessionKey string    `gorm:"size:191;not null;uniqueIndex:idx_agent_route_mapping_unique,priority:4" json:"route_session_key"`
	SessionID       string    `gorm:"size:50;not null;index:idx_agent_route_mapping_session,priority:4" json:"session_id"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `gorm:"index" json:"updated_at"`
}

func (AgentSessionRouteMapping) TableName() string { return "agent_session_route_mappings" }
