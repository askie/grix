// Package service 的这个文件是中转凭证响应里的 versioned direct_relay capability：
// Claude/Codex 官方客户端"原生配置直连网关"所需的能力声明，由后端集中裁决，
// 连接器不得凭 clientType 自行猜测（plan-direct-relay-first-refactor D1）。
//
// 合同要点：
//   - 旧字段（virtual_key/anthropic_base_url/openai_base_url/relay_model）语义不变，
//     direct_relay 是可选追加对象，旧连接器安全忽略（D6）。
//   - base_url 是官方客户端的 base（不带重复 /v1）：SDK 自己会补 /v1/messages 等后缀，
//     而旧的 anthropic_base_url 带 /v1 是专给 connector MITM 路由拼接用的，两者不能混用。
//   - Codex 只对"原生实现 ResponsesUpstream 的厂商"（当前仅 DeepSeek）声明 supported；
//     其余厂商直连会静默掉进 Responses↔Chat 桥接，语义失真，明确不允许（D4）。
//   - capability 里绝不含虚拟 Key 或真实厂商 Key；返回内容与服务端日志都不打印 Key。
package service

import (
	"encoding/json"
	"net/url"
	"strings"

	"github.com/askie/grix/backend/config"
	"github.com/askie/grix/backend/internal/gateway/modelcatalog"
	"github.com/askie/grix/backend/internal/gateway/modelroute"
	"github.com/askie/grix/backend/internal/model"
)

// directRelayVersion 是 direct_relay capability 的合同版本。
// 连接器只接受它认识的版本；字段破坏性变更时必须 bump。
const directRelayVersion = 1

// DirectRelayClaudeCapability 是 Claude 官方客户端（ANTHROPIC_* 环境变量）直连所需的能力。
type DirectRelayClaudeCapability struct {
	Supported bool `json:"supported"`
	// BaseURL 是 Anthropic SDK 的 base（不带 /v1），如 https://host/anthropic。
	BaseURL string `json:"base_url,omitempty"`
	// PrimaryModel 映射 ANTHROPIC_MODEL 及 Opus/Sonnet 默认模型。
	PrimaryModel string `json:"primary_model,omitempty"`
	// FastModel 映射 Haiku/subagent 默认模型；缺省时连接器回落到 primary_model。
	FastModel string `json:"fast_model,omitempty"`
	// Effort 是后端允许并选定的 effort 档位；缺省表示不下发 effort 覆盖。
	Effort string `json:"effort,omitempty"`
}

// DirectRelayCodexCapability 是 Codex 官方客户端（私有 CODEX_HOME + config.toml）直连所需的能力。
type DirectRelayCodexCapability struct {
	Supported bool `json:"supported"`
	// BaseURL 是 OpenAI 客户端的 base（不带 /v1），如 https://host/openai。
	BaseURL string `json:"base_url,omitempty"`
	// WireAPI 恒为 "responses"：direct Codex 只走原生 Responses，不走 Chat 桥接。
	WireAPI string `json:"wire_api,omitempty"`
	Model   string `json:"model,omitempty"`
	// ReasoningEffort 缺省表示不下发档位覆盖（用 catalog 里该模型的默认档）。
	ReasoningEffort string `json:"reasoning_effort,omitempty"`
	// Catalog 是内嵌的版本化模型 catalog（交付方式 A），连接器校验
	// catalog_version + catalog_sha256 后原子落盘，失败继续用上一版好文件。
	CatalogVersion string `json:"catalog_version,omitempty"`
	CatalogSHA256  string `json:"catalog_sha256,omitempty"`
	// CatalogJSON 保留参与摘要计算的原始字节文本；中间客户端 JSON decode/re-encode
	// 可能改变对象 key 顺序，连接器优先用本字段复算 hash。Catalog 保留给直接消费对象的客户端。
	CatalogJSON string          `json:"catalog_json,omitempty"`
	Catalog     json.RawMessage `json:"catalog,omitempty"`
}

// DirectRelayCapability 是凭证响应的 direct_relay 扩展对象（versioned）。
// 只有 claude/codex 类型的 agent 会带出对应小节；其它类型整个对象缺席。
type DirectRelayCapability struct {
	Version int                          `json:"version"`
	Claude  *DirectRelayClaudeCapability `json:"claude,omitempty"`
	Codex   *DirectRelayCodexCapability  `json:"codex,omitempty"`
}

// buildDirectRelayCapability 按 agent 客户端类型和本次签发选定的 relay model 构造能力声明。
// 返回 nil 表示响应不带 direct_relay（flag 关闭或非 claude/codex 类型）。
//
// relayModel 为空或字段凑不齐时只能声明 supported=false——capability 不完整时
// 连接器必须保持旧路径，后端绝不替它猜一个"看起来能用"的配置（D1）。
func buildDirectRelayCapability(clientType, relayModel, anthropicBaseURL, openaiBaseURL string) *DirectRelayCapability {
	if !config.C.Gateway.DirectRelayEnabled {
		return nil
	}
	capability := &DirectRelayCapability{Version: directRelayVersion}
	switch clientType {
	case model.AgentClientTypeClaude:
		capability.Claude = buildClaudeDirectCapability(relayModel, anthropicBaseURL)
	case model.AgentClientTypeCodex:
		capability.Codex = buildCodexDirectCapability(relayModel, openaiBaseURL)
	default:
		return nil
	}
	return capability
}

func buildClaudeDirectCapability(relayModel, anthropicBaseURL string) *DirectRelayClaudeCapability {
	claude := &DirectRelayClaudeCapability{}
	provider := modelroute.ResolveProvider(relayModel)
	baseURL := directBaseURL(anthropicBaseURL)
	if relayModel == "" || baseURL == "" || !modelroute.NativeAnthropicMessages(provider) {
		return claude
	}
	claude.Supported = true
	claude.BaseURL = baseURL
	claude.PrimaryModel = relayModel
	return claude
}

func buildCodexDirectCapability(relayModel, openaiBaseURL string) *DirectRelayCodexCapability {
	codex := &DirectRelayCodexCapability{}
	provider := modelroute.ResolveProvider(relayModel)
	canonical := modelroute.CanonicalModel(provider, relayModel)
	baseURL := directBaseURL(openaiBaseURL)
	if relayModel == "" || baseURL == "" || !modelroute.NativeResponses(provider) || !modelcatalog.Has(canonical) {
		return codex
	}
	codex.Supported = true
	codex.BaseURL = baseURL
	codex.WireAPI = "responses"
	// capability 的 model 必须能在同一响应下发的 catalog 中找到；alias 只用于路由解析。
	codex.Model = canonical
	codex.CatalogVersion = modelcatalog.Version
	catalogJSON := modelcatalog.CanonicalJSON()
	codex.CatalogSHA256 = modelcatalog.SHA256()
	codex.CatalogJSON = string(catalogJSON)
	codex.Catalog = catalogJSON
	return codex
}

// directBaseURL 把旧的"带 /v1 的 connector 拼接专用 base"换算成官方客户端 SDK 的 base。
// 例：https://host/anthropic/v1 → https://host/anthropic（SDK 自己补 /v1/messages）。
func directBaseURL(legacyBaseURL string) string {
	base := strings.TrimSuffix(strings.TrimRight(strings.TrimSpace(legacyBaseURL), "/"), "/v1")
	parsed, err := url.Parse(base)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "https" && parsed.Scheme != "http") ||
		parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return ""
	}
	return base
}
