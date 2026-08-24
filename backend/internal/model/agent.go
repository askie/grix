package model

import (
	"strings"
	"time"

	"gorm.io/datatypes"
)

const (
	AgentClientTypeCodex     = "codex"
	AgentClientTypeClaude    = "claude"
	AgentClientTypeGemini    = "gemini"
	AgentClientTypeHermes    = "hermes"
	AgentClientTypeOpenClaw  = "openclaw"
	AgentClientTypeQwen      = "qwen"
	AgentClientTypePi        = "pi"
	AgentClientTypeOpenHuman = "openhuman"
	AgentClientTypeCursor    = "cursor"
	AgentClientTypeReasonix  = "reasonix"
	AgentClientTypeCodeWhale = "codewhale"
	AgentClientTypeOpenCode  = "opencode"
	AgentClientTypeKiro      = "kiro"
	AgentClientTypeCopilot   = "copilot"
	AgentClientTypeAgy       = "agy"
	AgentClientTypeKimi      = "kimi"
	AgentClientTypeDeepSeek  = "deepseek"
)

// validClientTypes is the set of known agent client types.
// New types can be registered at init time via RegisterClientType.
var validClientTypes = map[string]bool{
	"":                       true,
	AgentClientTypeCodex:     true,
	AgentClientTypeClaude:    true,
	AgentClientTypeGemini:    true,
	AgentClientTypeHermes:    true,
	AgentClientTypeOpenClaw:  true,
	AgentClientTypeQwen:      true,
	AgentClientTypePi:        true,
	AgentClientTypeOpenHuman: true,
	AgentClientTypeCursor:    true,
	AgentClientTypeReasonix:  true,
	AgentClientTypeCodeWhale: true,
	AgentClientTypeOpenCode:  true,
	AgentClientTypeKiro:      true,
	AgentClientTypeCopilot:   true,
	AgentClientTypeAgy:       true,
	AgentClientTypeKimi:      true,
	AgentClientTypeDeepSeek:  true,
}

// RegisterClientType adds a client type to the valid set.
// This allows adapter packages to register their own types without
// modifying this file.
func RegisterClientType(clientType string) {
	normalized := NormalizeAgentClientType(clientType)
	if normalized != "" {
		validClientTypes[normalized] = true
	}
}

func NormalizeAgentClientType(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

// IsProprietaryAgentClientType returns true for agents that require
// mention-only dispatch in group sessions (claude, codex, gemini, qwen, pi, openhuman).
// Generic agents (openclaw, hermes) receive all group messages.
func IsProprietaryAgentClientType(clientType string) bool {
	switch NormalizeAgentClientType(clientType) {
	case AgentClientTypeClaude, AgentClientTypeCodex, AgentClientTypeGemini, AgentClientTypeQwen, AgentClientTypePi, AgentClientTypeOpenHuman, AgentClientTypeCursor, AgentClientTypeReasonix, AgentClientTypeCodeWhale, AgentClientTypeOpenCode, AgentClientTypeKiro, AgentClientTypeCopilot, AgentClientTypeAgy, AgentClientTypeKimi, AgentClientTypeDeepSeek:
		return true
	default:
		return false
	}
}

func IsValidAgentClientType(value string) bool {
	return validClientTypes[NormalizeAgentClientType(value)]
}

// 媒体能力常量
const (
	AgentMediaCapabilityText       = "text"       // 仅文本（默认）
	AgentMediaCapabilityVoice      = "voice"      // 支持语音托管
	AgentMediaCapabilityMultimodal = "multimodal" // 多模态
)

type Agent struct {
	ID              int64          `gorm:"primaryKey" json:"id,string"`
	AgentName       string         `gorm:"size:100;not null" json:"agent_name"`
	Introduction    string         `gorm:"type:text;not null;default:''" json:"introduction"`
	ModelProvider   string         `gorm:"size:50" json:"model_provider"`
	SystemPrompt    string         `gorm:"type:text" json:"system_prompt"`
	AvatarURL       string         `gorm:"size:255" json:"avatar_url"`
	OwnerID         int64          `gorm:"not null" json:"owner_id,string"`
	CategoryID      int64          `gorm:"index;not null;default:0" json:"category_id,string"`
	SortOrder       int            `gorm:"not null;default:0" json:"sort_order"`
	ProviderType    int16          `gorm:"default:1" json:"provider_type"` // 1=remote API, 2=local LLM, 3=agent API
	AgentClientType string         `gorm:"size:32;not null;default:''" json:"agent_client_type"`
	LocalEndpoint   string         `gorm:"size:255" json:"local_endpoint"`
	LocalModelName  string         `gorm:"size:100" json:"local_model_name"`
	ContextFile     string         `gorm:"type:text" json:"context_file"`
	IsMain          bool           `gorm:"default:false" json:"is_main"`
	APIKeyHash      string         `gorm:"size:128" json:"-"`
	APIKeyHint      string         `gorm:"size:16" json:"api_key_hint"`
	Config          datatypes.JSON `gorm:"type:jsonb;default:'{}'" json:"config"`
	Status          int16          `gorm:"default:1" json:"status"` // 1=active, 2=disabled, 3=deleted
	// ConnectorClient / ConnectorVersion / ConnectorVersionSeenAt 记录 agent-api 连接最近一次 WS 鉴权上报的
	// client（grix-connector / hermes-agent / openclaw-grix）与 client_version，
	// 用于筛选仍在跑旧版本 connector 的 agent（见 service.NotifyConnectorUpgrade）。
	ConnectorClient        string     `gorm:"size:32;not null;default:''" json:"connector_client,omitempty"`
	ConnectorVersion       string     `gorm:"size:32;not null;default:''" json:"connector_version,omitempty"`
	ConnectorVersionSeenAt *time.Time `gorm:"column:connector_version_seen_at" json:"connector_version_seen_at,omitempty"`
	// Phase 2: 语音媒体能力
	MediaCapability string `gorm:"size:16;not null;default:'text'" json:"media_capability"` // 'text' | 'voice' | 'multimodal'
	VoiceProvider   string `gorm:"size:32" json:"voice_provider,omitempty"`                 // 'doubao_realtime' 等
	VoiceID         string `gorm:"size:64" json:"voice_id,omitempty"`                       // Provider 侧音色/模型 ID
	// VoiceWelcomeI18n 按语言存语音开场白文案（json: map[locale]string，locale 见 pkg/locale.Supported）。
	// 通话开始时按 VoiceBridgeSpec.Language 选取一份下发给 voicebridge 主动播报；缺省不打招呼。
	VoiceWelcomeI18n datatypes.JSON `gorm:"type:jsonb;default:'{}'" json:"voice_welcome_i18n,omitempty"`
	// 语音大模型 BYOK（provider_type=4）
	VoiceModel              string    `gorm:"size:100" json:"voice_model,omitempty"`                                                  // Provider 侧模型名
	VoiceEndpoint           string    `gorm:"size:255" json:"voice_endpoint,omitempty"`                                               // 可选自定义 base URL
	VoiceAPIKeyCipher       string    `gorm:"size:512" json:"-"`                                                                      // 用户 API key 密文，永不序列化
	VoiceAPIKeyHint         string    `gorm:"size:16" json:"voice_api_key_hint,omitempty"`                                            // API key 末 4 位
	VoiceMaxCallSeconds     int       `gorm:"column:voice_max_call_seconds;not null;default:0" json:"voice_max_call_seconds"`         // 单通话时长上限(秒)，0=不限
	VoiceDailyCallLimit     int       `gorm:"column:voice_daily_call_limit;not null;default:0" json:"voice_daily_call_limit"`         // 每日通话次数上限，0=不限
	VoiceAllowVisitor       bool      `gorm:"column:voice_allow_visitor;not null;default:false" json:"voice_allow_visitor"`           // 是否开放访客/客服通话
	VoiceMaxConcurrentCalls int       `gorm:"column:voice_max_concurrent_calls;not null;default:5" json:"voice_max_concurrent_calls"` // 同一时刻并发接待上限，0=不限
	CreatedAt               time.Time `json:"created_at"`
	UpdatedAt               time.Time `json:"updated_at"`
}

func (Agent) TableName() string { return "agents" }
