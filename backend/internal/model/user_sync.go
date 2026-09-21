package model

import (
	"time"

	"gorm.io/datatypes"
)

const UserSyncStreamName = "chat"

// UserSyncHead is the committed publication watermark for one user's durable
// stream. Writers lock this row and update it in the same transaction as the
// domain mutation and UserSyncEvent insert.
type UserSyncHead struct {
	UserID     int64     `gorm:"primaryKey" json:"user_id,string"`
	HeadCursor int64     `gorm:"not null;default:0" json:"head_cursor,string"`
	UpdatedAt  time.Time `gorm:"not null" json:"updated_at"`
}

func (UserSyncHead) TableName() string { return "user_sync_heads" }

// UserSyncEvent is append-only. Payload is the canonical final entity state;
// EntityVersion prevents older history/snapshots from overwriting it.
type UserSyncEvent struct {
	UserID        int64          `gorm:"primaryKey" json:"user_id,string"`
	StreamCursor  int64          `gorm:"primaryKey" json:"stream_cursor,string"`
	EventKind     string         `gorm:"size:64;not null" json:"event_kind"`
	EntityType    string         `gorm:"size:32;not null" json:"entity_type"`
	EntityID      string         `gorm:"size:128;not null" json:"entity_id"`
	EntityVersion int64          `gorm:"not null;default:0" json:"entity_version,string"`
	Tombstone     bool           `gorm:"not null;default:false" json:"tombstone"`
	CommandID     string         `gorm:"size:128;not null;default:''" json:"command_id,omitempty"`
	Payload       datatypes.JSON `gorm:"type:jsonb;not null" json:"payload"`
	CreatedAt     time.Time      `gorm:"not null" json:"created_at"`
}

func (UserSyncEvent) TableName() string { return "user_sync_events" }

// DeviceSyncCursor records server-observed resume/ack state for one physical
// client. It is diagnostic and replay state; the client's committed local DB
// cursor remains authoritative on resume, so the server never forces it
// forward from this row.
type DeviceSyncCursor struct {
	UserID           int64     `gorm:"primaryKey" json:"user_id,string"`
	DeviceID         string    `gorm:"primaryKey;size:100" json:"device_id"`
	StreamName       string    `gorm:"primaryKey;size:32" json:"stream_name"`
	Generation       string    `gorm:"size:64;not null" json:"generation"`
	LastResumeCursor int64     `gorm:"not null;default:0" json:"last_resume_cursor,string"`
	CommittedCursor  int64     `gorm:"not null;default:0" json:"committed_cursor,string"`
	UpdatedAt        time.Time `gorm:"not null" json:"updated_at"`
}

func (DeviceSyncCursor) TableName() string { return "device_sync_cursors" }

// SyncCommandReceipt makes client outbox retries durable across process
// restarts. CommandID is scoped by user and command kind.
type SyncCommandReceipt struct {
	UserID      int64          `gorm:"primaryKey" json:"user_id,string"`
	CommandKind string         `gorm:"primaryKey;size:64" json:"command_kind"`
	CommandID   string         `gorm:"primaryKey;size:128" json:"command_id"`
	Response    datatypes.JSON `gorm:"type:jsonb;not null" json:"response"`
	CreatedAt   time.Time      `gorm:"not null" json:"created_at"`
	UpdatedAt   time.Time      `gorm:"not null" json:"updated_at"`
}

func (SyncCommandReceipt) TableName() string { return "sync_command_receipts" }
