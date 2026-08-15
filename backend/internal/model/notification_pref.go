package model

import (
	"time"

	"gorm.io/datatypes"
)

// NotificationPref stores a user's per-event notification preference: whether
// the event notifies at all, and which channels it uses. One row per
// (user_id, event_key). See
// Agent notification preference model.
type NotificationPref struct {
	UserID    int64          `gorm:"primaryKey;autoIncrement:false" json:"user_id,string"`
	EventKey  string         `gorm:"primaryKey;size:64" json:"event_key"`
	Enabled   bool           `gorm:"not null;default:true" json:"enabled"`
	Channels  datatypes.JSON `gorm:"type:jsonb;not null;default:'[\"push\"]'" json:"channels"`
	UpdatedAt time.Time      `json:"updated_at"`
}

func (NotificationPref) TableName() string { return "notification_prefs" }
