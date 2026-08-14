package model

import "time"

type LLMUsageLog struct {
	ID               int64     `gorm:"primaryKey;autoIncrement" json:"id"`
	UserID           int64     `gorm:"not null" json:"user_id"`
	SessionID        string    `gorm:"size:50;not null" json:"session_id"`
	AgentID          int64     `gorm:"not null" json:"agent_id"`
	ModelProvider    string    `gorm:"size:50" json:"model_provider"`
	PromptTokens     int       `gorm:"default:0" json:"prompt_tokens"`
	CompletionTokens int       `gorm:"default:0" json:"completion_tokens"`
	IsInterrupted    bool      `gorm:"default:false" json:"is_interrupted"`
	CreatedAt        time.Time `json:"created_at"`
}

func (LLMUsageLog) TableName() string { return "llm_usage_logs" }
