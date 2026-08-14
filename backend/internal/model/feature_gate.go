package model

import "time"

const (
	FeatureStatusDisabled = "disabled" // 全局关闭，无人可见
	FeatureStatusWhitelist = "whitelist" // 仅白名单用户可见
	FeatureStatusEnabled  = "enabled"  // 全局开放，所有人可见
)

type FeatureGate struct {
	Key         string    `gorm:"primaryKey;size:64" json:"key"`
	DisplayName string    `gorm:"size:128;not null;default:''" json:"display_name"`
	Status      string    `gorm:"size:16;not null;default:'disabled'" json:"status"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

func (FeatureGate) TableName() string { return "feature_gates" }

type FeatureGateUser struct {
	FeatureKey string    `gorm:"primaryKey;size:64" json:"feature_key"`
	UserID     int64     `gorm:"primaryKey" json:"user_id"`
	CreatedAt  time.Time `json:"created_at"`
}

func (FeatureGateUser) TableName() string { return "feature_gate_users" }
