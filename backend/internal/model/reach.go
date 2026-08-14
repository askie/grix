package model

import (
	"time"

	"gorm.io/datatypes"
)

const (
	ReachKindSystemEvent = "system_event"
	ReachKindMarketing   = "marketing"
	ReachKindDirect      = "direct"

	ReachStatusDraft     = "draft"
	ReachStatusScheduled = "scheduled"
	ReachStatusSending   = "sending"
	ReachStatusSent      = "sent"
	ReachStatusPaused    = "paused"
	ReachStatusCancelled = "cancelled"
	ReachStatusFailed    = "failed"

	ReachSendStatusPending = "pending"
	ReachSendStatusSent    = "sent"
	ReachSendStatusFailed  = "failed"
	ReachSendStatusSkipped = "skipped"

	ReachChannelInApp = "in_app"
	ReachChannelPush  = "push"
	ReachChannelEmail = "email"
	ReachChannelSMS   = "sms"
)

type ReachTask struct {
	ID          int64          `gorm:"primaryKey;autoIncrement:false" json:"id,string"`
	Kind        string         `gorm:"size:32;not null" json:"kind"`
	EventKey    string         `gorm:"size:64;not null;default:''" json:"event_key"`
	DedupKey    *string        `gorm:"size:128;uniqueIndex:uq_reach_tasks_dedup_key" json:"-"`
	TemplateID  int64          `gorm:"not null;default:0" json:"template_id,string"`
	Channels    datatypes.JSON `gorm:"type:jsonb;not null;default:'[]'" json:"channels"`
	Audience    datatypes.JSON `gorm:"type:jsonb;not null;default:'{}'" json:"audience"`
	Content     datatypes.JSON `gorm:"type:jsonb;not null;default:'{}'" json:"content"`
	Status      string         `gorm:"size:24;not null;default:'draft'" json:"status"`
	ScheduledAt *time.Time     `json:"scheduled_at,omitempty"`
	Stats       datatypes.JSON `gorm:"type:jsonb;not null;default:'{}'" json:"stats"`
	Region      string         `gorm:"size:16;not null;default:''" json:"region"`
	ABGroupID   string         `gorm:"size:64;not null;default:''" json:"ab_group_id"`
	ABVariant   string         `gorm:"size:32;not null;default:''" json:"ab_variant"`
	CreatedBy   int64          `gorm:"not null;default:0" json:"created_by,string"`
	CreatedAt   time.Time      `json:"created_at"`
	UpdatedAt   time.Time      `json:"updated_at"`
}

func (ReachTask) TableName() string { return "reach_tasks" }

type ReachTemplate struct {
	ID        int64     `gorm:"primaryKey;autoIncrement:false" json:"id,string"`
	Name      string    `gorm:"size:128;not null;default:''" json:"name"`
	Title     string    `gorm:"size:256;not null;default:''" json:"title"`
	InAppBody string    `gorm:"type:text;not null;default:''" json:"in_app_body"`
	PushBody  string    `gorm:"type:text;not null;default:''" json:"push_body"`
	EmailHTML string    `gorm:"type:text;not null;default:''" json:"email_html"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func (ReachTemplate) TableName() string { return "reach_templates" }

type ReachSendLog struct {
	ID        int64      `gorm:"primaryKey;autoIncrement:false" json:"id,string"`
	TaskID    int64      `gorm:"not null;uniqueIndex:uq_reach_send_dedup,priority:1" json:"task_id,string"`
	UserID    int64      `gorm:"not null;uniqueIndex:uq_reach_send_dedup,priority:2" json:"user_id,string"`
	Channel   string     `gorm:"size:32;not null;uniqueIndex:uq_reach_send_dedup,priority:3" json:"channel"`
	Status    string     `gorm:"size:16;not null;default:'pending'" json:"status"`
	Error     string     `gorm:"type:text" json:"error"`
	Region    string     `gorm:"size:16;not null;default:''" json:"region"`
	OpenedAt  *time.Time `json:"opened_at,omitempty"`
	ClickedAt *time.Time `json:"clicked_at,omitempty"`
	CreatedAt time.Time  `json:"created_at"`
}

func (ReachSendLog) TableName() string { return "reach_send_logs" }

type ReachSubscription struct {
	UserID     int64          `gorm:"primaryKey;autoIncrement:false" json:"user_id,string"`
	Subscribed bool           `gorm:"not null;default:true" json:"subscribed"`
	Topics     datatypes.JSON `gorm:"type:jsonb;not null;default:'[]'" json:"topics"`
	UnsubToken string         `gorm:"size:64;not null;uniqueIndex" json:"-"`
	UpdatedAt  time.Time      `json:"updated_at"`
}

func (ReachSubscription) TableName() string { return "reach_subscriptions" }
