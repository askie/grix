package model

import "time"

type AgentAPIScope struct {
	AgentID   int64     `gorm:"primaryKey;autoIncrement:false" json:"agent_id,string"`
	Scope     string    `gorm:"primaryKey;size:64;not null" json:"scope"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func (AgentAPIScope) TableName() string { return "agent_api_scopes" }
