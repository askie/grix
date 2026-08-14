package model

import (
	"time"

	"gorm.io/gorm"
)

// SessionHistoryReset stores per-user local-history reset cutoffs for a session.
// Messages at or before DeletedBefore should be hidden from this user and excluded from LLM context.
type SessionHistoryReset struct {
	SessionID     string    `gorm:"primaryKey;size:50" json:"session_id"`
	UserID        int64     `gorm:"primaryKey" json:"user_id,string"`
	DeletedBefore time.Time `gorm:"not null" json:"deleted_before"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

func (SessionHistoryReset) TableName() string { return "session_history_resets" }

func (r *SessionHistoryReset) BeforeCreate(tx *gorm.DB) error {
	r.DeletedBefore = r.DeletedBefore.UTC()
	if !r.CreatedAt.IsZero() {
		r.CreatedAt = r.CreatedAt.UTC()
	} else {
		r.CreatedAt = time.Now().UTC()
	}
	if !r.UpdatedAt.IsZero() {
		r.UpdatedAt = r.UpdatedAt.UTC()
	} else {
		r.UpdatedAt = r.CreatedAt
	}
	return nil
}
