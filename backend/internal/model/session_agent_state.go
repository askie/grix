package model

import "time"

// Session agent state is a single mutually-exclusive value. A session is in
// exactly one of these at any time, so the consumer can decide what to do from
// the state alone. waiting_approval / waiting_question are run phases (the run
// is alive, blocked on the owner), not a separate "attention" axis.
const (
	SessionAgentStateRunning         = "running"
	SessionAgentStateWaitingApproval = "waiting_approval"
	SessionAgentStateWaitingQuestion = "waiting_question"
	SessionAgentStateCompleted       = "completed"
	SessionAgentStateFailed          = "failed"
	SessionAgentStateIdle            = "idle"
)

// StopReasonMaxRunes matches the chat_states.stop_reason VARCHAR(255) column.
// PostgreSQL counts characters for VARCHAR, so stop reasons must be bounded by
// rune count, not bytes.
const StopReasonMaxRunes = 255

// SessionAgentState holds the current agent run state for a session, collapsed
// to one row per (session_id, owner_id). Multiple runs within the session
// upsert into this single record.
type SessionAgentState struct {
	SessionID     string     `gorm:"primaryKey;size:64" json:"session_id"`
	OwnerID       int64      `gorm:"primaryKey" json:"owner_id,string"`
	AgentID       int64      `gorm:"not null" json:"agent_id,string"`
	State         string     `gorm:"size:32;not null;default:'idle'" json:"state"`
	TaskTitle     string     `gorm:"size:255;not null;default:''" json:"task_title"`
	LastRunID     string     `gorm:"size:192;not null;default:''" json:"last_run_id"`
	RunGeneration int64      `gorm:"not null;default:0" json:"run_generation"`
	StopReason    string     `gorm:"size:255;not null;default:''" json:"stop_reason"`
	StartedAt     *time.Time `json:"started_at,omitempty"`
	CompletedAt   *time.Time `json:"completed_at,omitempty"`
	NotifiedAt    *time.Time `json:"notified_at,omitempty"`
	UpdatedAt     time.Time  `json:"updated_at"`
}

func (SessionAgentState) TableName() string { return "chat_states" }
