package model

import (
	"time"

	"gorm.io/datatypes"
)

// AppRelease stores a published version of the Grix client app (mobile + desktop).
type AppRelease struct {
	ID           int64          `gorm:"primaryKey" json:"id,string"`
	Version      string         `gorm:"size:32;not null" json:"version"`
	BuildNumber  int            `gorm:"not null;default:0" json:"build_number"`
	Platform     string         `gorm:"size:16;not null" json:"platform"` // ios | android | macos | windows
	Channel      string         `gorm:"size:16;not null;default:'stable'" json:"channel"`
	Changelog    string         `gorm:"type:text" json:"changelog"`
	MinVersion   *string        `gorm:"size:32" json:"min_version"`
	MinBuild     *int           `json:"min_build"`
	UpdateMethod string         `gorm:"size:16;not null;default:'download'" json:"update_method"` // download | app_store | google_play
	DownloadURL  string         `gorm:"size:512" json:"download_url"`
	AppStoreURL  string         `gorm:"size:512" json:"app_store_url"`
	FileSize     int64          `gorm:"default:0" json:"file_size"`
	Sha256          string         `gorm:"size:64" json:"sha256"`
	EddsaSignature  string         `gorm:"size:128" json:"eddsa_signature"`
	Metadata        datatypes.JSON `gorm:"type:jsonb;default:'{}'" json:"metadata"`
	Status       int16          `gorm:"not null;default:1" json:"status"`
	CreatedAt    time.Time      `json:"created_at"`
	PublishedAt  *time.Time     `json:"published_at"`
}

func (AppRelease) TableName() string { return "app_releases" }

// AppRolloutRule defines how an app release is distributed to users.
type AppRolloutRule struct {
	ID        int64          `gorm:"primaryKey" json:"id,string"`
	ReleaseID int64          `gorm:"not null;index:idx_app_rollout_release_status" json:"release_id,string"`
	RuleType  string         `gorm:"size:32;not null" json:"rule_type"` // user_list | percentage
	RuleValue datatypes.JSON `gorm:"type:jsonb;not null" json:"rule_value"`
	Priority  int            `gorm:"not null;default:0" json:"priority"`
	Status    int16          `gorm:"not null;default:1;index:idx_app_rollout_release_status" json:"status"`
	CreatedAt time.Time      `json:"created_at"`
}

func (AppRolloutRule) TableName() string { return "app_rollout_rules" }

// AppDownloadReport records a completed app download event.
type AppDownloadReport struct {
	ID         int64     `gorm:"primaryKey" json:"id,string"`
	UserID     int64     `gorm:"not null" json:"user_id,string"`
	ReleaseID  int64     `gorm:"not null" json:"release_id,string"`
	FromBuild  *int      `json:"from_build"`
	Platform   string    `gorm:"size:16;not null" json:"platform"`
	ErrorMsg   string    `gorm:"type:text" json:"error_msg"`
	DurationMs int       `json:"duration_ms"`
	ReportedAt time.Time `json:"reported_at"`
}

func (AppDownloadReport) TableName() string { return "app_download_reports" }
