package model

import "time"

const (
	// GatewayCredentialPurposeInference 推理转发用（DeepSeek Key / 火山 ARK Key）。
	GatewayCredentialPurposeInference = "inference"
	// GatewayCredentialPurposeReconcile 对账用（火山费用中心 AK/SK，跟推理Key不是一回事）。
	GatewayCredentialPurposeReconcile = "reconcile"
)

// GatewayUpstreamCredential 是网关上游厂商的官方凭据，由塘主后台动态增删。
// api_key/api_secret 以 AES-GCM 密文存储（APIKeyEnc/APISecretEnc），明文永不落库、永不出接口；
// 展示只给 KeyHint（明文末4位）。同一 provider+purpose 可挂多把启用凭据，网关运行时轮询取用。
type GatewayUpstreamCredential struct {
	ID           int64     `gorm:"primaryKey" json:"id,string"`
	Provider     string    `gorm:"size:32;not null;index:idx_gateway_upstream_cred_lookup,priority:1" json:"provider"`
	Purpose      string    `gorm:"size:16;not null;default:inference;index:idx_gateway_upstream_cred_lookup,priority:2" json:"purpose"`
	APIKeyEnc    string    `gorm:"column:api_key_enc;not null" json:"-"`
	APISecretEnc string    `gorm:"column:api_secret_enc;not null;default:''" json:"-"`
	KeyHint      string    `gorm:"size:16;not null;default:''" json:"key_hint"`
	BaseURL      string    `gorm:"not null;default:''" json:"base_url"`
	Region       string    `gorm:"size:32;not null;default:''" json:"region"`
	Label        string    `gorm:"size:64;not null;default:''" json:"label"`
	Enabled      bool      `gorm:"not null;default:true;index:idx_gateway_upstream_cred_lookup,priority:3" json:"enabled"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

func (GatewayUpstreamCredential) TableName() string { return "gateway_upstream_credentials" }
