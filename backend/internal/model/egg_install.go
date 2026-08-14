package model

import "time"

type EggInstall struct {
	InstallID       string    `gorm:"primaryKey;size:64" json:"install_id"`
	UserID          int64     `gorm:"not null;uniqueIndex:idx_egg_installs_user_idempotency,priority:1;index:idx_egg_installs_user_created,priority:1" json:"user_id,string"`
	EggID           string    `gorm:"size:128;not null" json:"egg_id"`
	Version         int       `gorm:"not null" json:"version"`
	Status          string    `gorm:"size:16;not null;default:pending;index:idx_egg_installs_status_created,priority:1" json:"status"`
	Step            string    `gorm:"size:64;not null;default:pending" json:"step"`
	ExecutorAgentID *int64    `json:"executor_agent_id,string,omitempty"`
	SessionID       string    `gorm:"size:64;not null;default:''" json:"session_id"`
	TargetAgentID   *int64    `json:"target_agent_id,string,omitempty"`
	ErrorCode       string    `gorm:"size:64;not null;default:''" json:"error_code"`
	ErrorMsg        string    `gorm:"type:text;not null;default:''" json:"error_msg"`
	IdempotencyKey  string    `gorm:"size:128;not null;uniqueIndex:idx_egg_installs_user_idempotency,priority:2" json:"idempotency_key"`
	CounterApplied  bool      `gorm:"not null;default:false" json:"counter_applied"`
	CreatedAt       time.Time `gorm:"index:idx_egg_installs_user_created,priority:2;index:idx_egg_installs_status_created,priority:2" json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

func (EggInstall) TableName() string { return "egg_installs" }
