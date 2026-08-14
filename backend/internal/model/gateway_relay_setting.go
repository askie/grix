package model

import (
	"time"

	"gorm.io/datatypes"
)

// GatewayRelaySetting 是"Grix中转"的用户级模型设置：兜底模型 + 模型映射表。
// 网关按它解析每次请求真正要调用(也是要计费)的模型，客户端那边把模型叫什么名字后端不关心。
//
// 用户级(按钱包)而非按Agent：connector 的 MITM 代理是机器级共享的，
// per-Agent 的模型设置在同机多Agent场景下做不到。
type GatewayRelaySetting struct {
	WalletID int64 `gorm:"primaryKey" json:"wallet_id,string"`
	// DefaultModel 是兜底模型：没被映射命中、本身又不是后端支持模型的请求都落到它。
	// 它是"Claude/Codex 发新模型不会打挂链路"的保证，必填。
	DefaultModel string `gorm:"size:64;not null" json:"default_model"`
	// ModelMap 是 {客户端侧模型名: 后端支持的模型名}。key 用户自定义，value 写入时校验合法。
	ModelMap  datatypes.JSONMap `gorm:"type:jsonb;not null;default:'{}'" json:"model_map"`
	CreatedAt time.Time         `json:"created_at"`
	UpdatedAt time.Time         `json:"updated_at"`
}

func (GatewayRelaySetting) TableName() string { return "gateway_relay_settings" }
