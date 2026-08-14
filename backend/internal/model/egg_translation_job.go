package model

import "time"

const (
	EggTranslationJobTypeEggI18n        = "egg_i18n"
	EggTranslationJobTypeEggVersionI18n = "egg_version_i18n"

	EggTranslationJobStatusPending int16 = 0
	EggTranslationJobStatusDone    int16 = 1
	EggTranslationJobStatusFailed  int16 = 2
	EggTranslationJobStatusRunning int16 = 3
)

type EggTranslationJob struct {
	ID              int64     `gorm:"primaryKey;autoIncrement" json:"id,string"`
	JobType         string    `gorm:"size:32;not null;uniqueIndex:idx_egg_translation_jobs_target,priority:1;index:idx_egg_translation_jobs_status_retry,priority:1" json:"job_type"`
	EggID           string    `gorm:"size:128;not null;default:'';uniqueIndex:idx_egg_translation_jobs_target,priority:2" json:"egg_id"`
	Version         int       `gorm:"not null;default:0;uniqueIndex:idx_egg_translation_jobs_target,priority:3" json:"version"`
	SourceLocale    string    `gorm:"size:16;not null;default:''" json:"source_locale"`
	SourceUpdatedAt time.Time `gorm:"not null" json:"source_updated_at"`
	TargetLocale    string    `gorm:"size:16;not null;uniqueIndex:idx_egg_translation_jobs_target,priority:4" json:"target_locale"`
	Status          int16     `gorm:"not null;default:0;index:idx_egg_translation_jobs_status_retry,priority:2" json:"status"`
	AttemptCount    int32     `gorm:"not null;default:0" json:"attempt_count"`
	MaxAttempts     int32     `gorm:"not null;default:5" json:"max_attempts"`
	NextRetryAt     time.Time `gorm:"not null;index:idx_egg_translation_jobs_status_retry,priority:3" json:"next_retry_at"`
	LastError       string    `gorm:"type:text;not null;default:''" json:"last_error"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

func (EggTranslationJob) TableName() string { return "egg_translation_jobs" }
