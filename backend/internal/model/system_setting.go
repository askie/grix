package model

import (
	"time"

	"gorm.io/datatypes"
)

type SystemSetting struct {
	Key       string         `gorm:"primaryKey;size:100" json:"key"`
	Value     datatypes.JSON `gorm:"type:jsonb;not null" json:"value"`
	UpdatedBy *int64         `json:"updated_by,omitempty"`
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
}

func (SystemSetting) TableName() string { return "system_settings" }
