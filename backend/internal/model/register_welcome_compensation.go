package model

import "time"

const (
	RegisterWelcomeCompensationStatusPending int16 = 0
	RegisterWelcomeCompensationStatusDone    int16 = 1
	RegisterWelcomeCompensationStatusFailed  int16 = 2
)

type RegisterWelcomeCompensation struct {
	ID             int64     `gorm:"primaryKey;autoIncrement" json:"id,string"`
	RegisterUserID int64     `gorm:"not null;uniqueIndex:idx_register_welcome_compensations_target" json:"register_user_id,string"`
	CustomerUserID int64     `gorm:"not null;uniqueIndex:idx_register_welcome_compensations_target" json:"customer_user_id,string"`
	Status         int16     `gorm:"not null;default:0;index" json:"status"`
	AttemptCount   int32     `gorm:"not null;default:0" json:"attempt_count"`
	MaxAttempts    int32     `gorm:"not null;default:5" json:"max_attempts"`
	NextRetryAt    time.Time `gorm:"not null;index" json:"next_retry_at"`
	LastError      string    `gorm:"type:text;not null;default:''" json:"last_error"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

func (RegisterWelcomeCompensation) TableName() string { return "register_welcome_compensations" }
