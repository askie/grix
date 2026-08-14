package model

import (
	"time"

	"gorm.io/datatypes"
)

// --- Release status constants ---

const (
	ReleaseStatusDraft     int16 = 1
	ReleaseStatusPublished int16 = 2
	ReleaseStatusRevoked   int16 = 3
	ReleaseStatusPaused    int16 = 4
)

// ConnectorRelease stores a published version of grix-connector or grix-hermes.
type ConnectorRelease struct {
	ID          int64          `gorm:"primaryKey" json:"id,string"`
	ClientType  string         `gorm:"size:32;not null;default:'grix-connector'" json:"client_type"`
	Version     string         `gorm:"size:32;not null" json:"version"`
	Channel     string         `gorm:"size:16;not null;default:'stable'" json:"channel"`
	Changelog   string         `gorm:"type:text" json:"changelog"`
	MinVersion  *string        `gorm:"size:32" json:"min_version"`
	NpmPackage  string         `gorm:"size:128;not null;default:'grix-connector'" json:"npm_package"`
	NpmTag      string         `gorm:"size:32;not null;default:'latest'" json:"npm_tag"`
	Force       bool           `gorm:"not null;default:false" json:"force"`
	Metadata    datatypes.JSON `gorm:"type:jsonb;default:'{}'" json:"metadata"`
	Status      int16          `gorm:"not null;default:1" json:"status"`
	CreatedAt   time.Time      `json:"created_at"`
	PublishedAt *time.Time     `json:"published_at"`
}

func (ConnectorRelease) TableName() string { return "connector_releases" }

// --- Rollout rule status ---

const (
	RolloutRuleActive  int16 = 1
	RolloutRulePaused  int16 = 2
)

// ConnectorRolloutRule defines how a release is distributed to agents.
type ConnectorRolloutRule struct {
	ID              int64          `gorm:"primaryKey" json:"id,string"`
	ReleaseID       int64          `gorm:"not null;index:idx_rollout_release_status" json:"release_id,string"`
	RuleType        string         `gorm:"size:32;not null" json:"rule_type"`
	RuleValue       datatypes.JSON `gorm:"type:jsonb;not null" json:"rule_value"`
	Priority        int            `gorm:"not null;default:0" json:"priority"`
	Status          int16          `gorm:"not null;default:1;index:idx_rollout_release_status" json:"status"`
	AutoPauseConfig datatypes.JSON `gorm:"type:jsonb;default:'{}'" json:"auto_pause_config"`
	CreatedAt       time.Time      `json:"created_at"`
}

func (ConnectorRolloutRule) TableName() string { return "connector_rollout_rules" }

// --- Upgrade report statuses ---

const (
	UpgradeReportInstalled  = "installed"
	UpgradeReportSuccess    = "success"
	UpgradeReportFailed     = "failed"
	UpgradeReportRolledBack = "rolled_back"
)

// ConnectorUpgradeReport records the result of an upgrade attempt.
type ConnectorUpgradeReport struct {
	ID           int64      `gorm:"primaryKey" json:"id,string"`
	AgentID      int64      `gorm:"not null;index:idx_report_agent_time" json:"agent_id,string"`
	ClientType   string     `gorm:"size:32;not null;default:'grix-connector'" json:"client_type"`
	FromVersion  string     `gorm:"size:32;not null" json:"from_version"`
	ToVersion    string     `gorm:"size:32;not null" json:"to_version"`
	Status       string     `gorm:"size:16;not null" json:"status"`
	ErrorCode    *string    `gorm:"size:32" json:"error_code"`
	ErrorMsg     *string    `gorm:"type:text" json:"error_msg"`
	UpgradeLog   *string    `gorm:"type:text" json:"upgrade_log"`
	CrashCount   int        `gorm:"default:0" json:"crash_count"`
	NpmVersion   *string    `gorm:"size:16" json:"npm_version"`
	NodeVersion  *string    `gorm:"size:32" json:"node_version"`
	DiskFreeMb   *int       `json:"disk_free_mb"`
	Platform     *string    `gorm:"size:16" json:"platform"`
	Arch         *string    `gorm:"size:16" json:"arch"`
	DurationMs   *int       `json:"duration_ms"`
	HostName     *string    `gorm:"size:255;index:idx_report_host_version" json:"host_name"`
	InstallID    *string    `gorm:"size:64;index:idx_report_install_version" json:"install_id"`
	ReportedAt   time.Time  `gorm:"index:idx_report_agent_time" json:"reported_at"`
}

func (ConnectorUpgradeReport) TableName() string { return "connector_upgrade_reports" }
