package service

import (
	"github.com/askie/grix/backend/internal/pkg/errcode"
	"gorm.io/datatypes"
)

const maxAgentsPerUser = 50
const maxContextFileBytes = 64 * 1024 // 64KB
const maxAgentNameRunes = 100
const maxAgentIntroductionRunes = 3072

var errAgentAvatarURLManagedOnly = errcode.ErrCode{
	HTTPStatus: 400,
	BizCode:    10003,
	Msg:        "头像必须通过上传接口设置",
}

// AgentCreateReq is the request payload for creating an agent.
type AgentCreateReq struct {
	AgentName       string `json:"agent_name" binding:"required"`
	Introduction    string `json:"introduction"`
	ModelProvider   string `json:"model_provider"`
	SystemPrompt    string `json:"system_prompt"`
	AvatarURL       string `json:"avatar_url"`
	CategoryID      int64  `json:"category_id,string"`
	ProviderType    int16  `json:"provider_type"` // 1=remote, 2=local, 3=agent API, 4=voice
	AgentClientType string `json:"agent_client_type"`
	IsMain          bool   `json:"is_main"`
	LocalEndpoint   string `json:"local_endpoint"`
	LocalModelName  string `json:"local_model_name"`
	ContextFile     string `json:"context_file"`
	// 语音大模型 BYOK（provider_type=4）
	VoiceProvider           string `json:"voice_provider"`
	VoiceID                 string `json:"voice_id"`
	VoiceModel              string `json:"voice_model"`
	VoiceEndpoint           string `json:"voice_endpoint"`
	VoiceAPIKey             string `json:"voice_api_key"` // 只写：用户自带 API key
	VoiceMaxCallSeconds     int    `json:"voice_max_call_seconds"`
	VoiceDailyCallLimit     int    `json:"voice_daily_call_limit"`
	VoiceMaxConcurrentCalls int    `json:"voice_max_concurrent_calls"`
	VoiceAllowVisitor       bool   `json:"voice_allow_visitor"`
	// VoiceWelcomeI18n 按语言存语音开场白文案（key 见 pkg/locale.Supported），
	// 通话建立后主动播报；缺省不打招呼。
	VoiceWelcomeI18n map[string]string `json:"voice_welcome_i18n"`
}

// AgentUpdateReq is the request payload for updating an agent.
type AgentUpdateReq struct {
	AgentName       *string `json:"agent_name"`
	Introduction    *string `json:"introduction"`
	ModelProvider   *string `json:"model_provider"`
	SystemPrompt    *string `json:"system_prompt"`
	AvatarURL       *string `json:"avatar_url"`
	CategoryID      *int64  `json:"category_id,string"`
	ProviderType    *int16  `json:"provider_type"`
	AgentClientType *string `json:"agent_client_type"`
	LocalEndpoint   *string `json:"local_endpoint"`
	LocalModelName  *string `json:"local_model_name"`
	SortOrder       *int    `json:"sort_order"`
	// 语音大模型 BYOK（provider_type=4）；VoiceAPIKey 留空表示保持原值
	VoiceProvider           *string            `json:"voice_provider"`
	VoiceID                 *string            `json:"voice_id"`
	VoiceModel              *string            `json:"voice_model"`
	VoiceEndpoint           *string            `json:"voice_endpoint"`
	VoiceAPIKey             *string            `json:"voice_api_key"`
	VoiceMaxCallSeconds     *int               `json:"voice_max_call_seconds"`
	VoiceDailyCallLimit     *int               `json:"voice_daily_call_limit"`
	VoiceMaxConcurrentCalls *int               `json:"voice_max_concurrent_calls"`
	VoiceAllowVisitor       *bool              `json:"voice_allow_visitor"`
	VoiceWelcomeI18n        *map[string]string `json:"voice_welcome_i18n"`
}

type AgentProfileResp struct {
	AvatarURL    string `json:"avatar_url"`
	Introduction string `json:"introduction"`
}

type AgentResp struct {
	ID              int64            `json:"id,string"`
	AgentName       string           `json:"agent_name"`
	Introduction    string           `json:"introduction"`
	ModelProvider   string           `json:"model_provider"`
	SystemPrompt    string           `json:"system_prompt"`
	AvatarURL       string           `json:"avatar_url"`
	Profile         AgentProfileResp `json:"profile"`
	OwnerID         int64            `json:"owner_id,string"`
	CategoryID      int64            `json:"category_id,string"`
	SortOrder       int              `json:"sort_order"`
	ProviderType    int16            `json:"provider_type"`
	IsMain          bool             `json:"is_main"`
	AgentClientType string           `json:"agent_client_type"`
	LocalEndpoint   string           `json:"local_endpoint"`
	LocalModelName  string           `json:"local_model_name"`
	ContextFile     string           `json:"context_file"`
	APIEndpoint     string           `json:"api_endpoint"`
	APIKey          string           `json:"api_key,omitempty"`
	APIKeyHint      string           `json:"api_key_hint"`
	// 语音大模型 BYOK（provider_type=4）；永不回传 voice_api_key 明文
	MediaCapability         string            `json:"media_capability,omitempty"`
	VoiceProvider           string            `json:"voice_provider,omitempty"`
	VoiceID                 string            `json:"voice_id,omitempty"`
	VoiceModel              string            `json:"voice_model,omitempty"`
	VoiceEndpoint           string            `json:"voice_endpoint,omitempty"`
	VoiceAPIKeyHint         string            `json:"voice_api_key_hint,omitempty"`
	VoiceMaxCallSeconds     int               `json:"voice_max_call_seconds,omitempty"`
	VoiceDailyCallLimit     int               `json:"voice_daily_call_limit,omitempty"`
	VoiceMaxConcurrentCalls int               `json:"voice_max_concurrent_calls,omitempty"`
	VoiceAllowVisitor       bool              `json:"voice_allow_visitor,omitempty"`
	VoiceWelcomeI18n        map[string]string `json:"voice_welcome_i18n,omitempty"`
	Online                  bool              `json:"online"`
	Config                  datatypes.JSON    `json:"config"`
	Status                  int16             `json:"status"`
	SessionID               string            `json:"session_id"`
	CreatedAt               int64             `json:"created_at"`
	UpdatedAt               int64             `json:"updated_at"`
}
