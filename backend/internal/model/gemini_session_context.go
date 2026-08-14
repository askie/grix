package model

import (
	"time"

	"gorm.io/gorm"
)

type GeminiSessionContext struct {
	AgentID   int64     `gorm:"primaryKey" json:"agent_id,string"`
	SessionID string    `gorm:"primaryKey;size:64" json:"session_id"`
	Cwd       string    `gorm:"not null;size:2048" json:"cwd"`
	ModeID    string    `gorm:"size:191" json:"mode_id"`
	ModelID   string    `gorm:"size:191" json:"model_id"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func (GeminiSessionContext) TableName() string { return "gemini_session_contexts" }

func (c *GeminiSessionContext) BeforeCreate(tx *gorm.DB) error {
	now := time.Now().UTC()
	if c.CreatedAt.IsZero() {
		c.CreatedAt = now
	} else {
		c.CreatedAt = c.CreatedAt.UTC()
	}
	if c.UpdatedAt.IsZero() {
		c.UpdatedAt = now
	} else {
		c.UpdatedAt = c.UpdatedAt.UTC()
	}
	return nil
}

func (c *GeminiSessionContext) BeforeUpdate(tx *gorm.DB) error {
	c.UpdatedAt = time.Now().UTC()
	return nil
}
