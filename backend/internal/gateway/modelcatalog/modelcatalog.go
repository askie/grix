// Package modelcatalog 是 Codex 直连（direct relay）用的**版本化模型 catalog 数据源**。
//
// 网关现有的模型清单（价目表 + modelroute）只有 provider/model/价格，承载不了 Codex
// 私有 models.json 需要的上下文窗口、reasoning 档位、工具能力、可见性等元数据，
// 所以 catalog 单独成包、版本化管理：每次变更必须 bump Version，连接器按
// catalog_version + catalog_sha256 校验后原子落盘，校验失败继续用上一版好文件。
//
// catalog JSON 的 SHA-256 是对 CanonicalJSON() 的字节计算的；Go 结构体 marshal 字段
// 顺序固定，同一 Version 的内容任何进程算出的摘要都一致。
package modelcatalog

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
)

// Version 是当前 catalog 的版本号。任何条目增删改都必须同步 bump（日期.序号），
// 否则连接器会因"版本相同但摘要不一致"拒绝更新，或错过更新。
const Version = "2026-08-04.2"

// ReasoningLevel 是 Codex models.json 接受的 reasoning 档位。
type ReasoningLevel struct {
	Effort      string `json:"effort"`
	Description string `json:"description"`
}

// TruncationPolicy 是 Codex 对本地上下文裁剪的配置。
type TruncationPolicy struct {
	Mode  string `json:"mode"`
	Limit int    `json:"limit"`
}

// Model 直接匹配 Codex `model_catalog_json` 的 models[] schema。
// 这些字段里有一部分在 Codex 0.144 仍是反序列化必填项，即使值为空也不能省略。
type Model struct {
	Slug                       string           `json:"slug"`
	DisplayName                string           `json:"display_name"`
	Description                string           `json:"description,omitempty"`
	BaseInstructions           string           `json:"base_instructions"`
	ExperimentalSupportedTools []string         `json:"experimental_supported_tools"`
	Priority                   int              `json:"priority"`
	ShellType                  string           `json:"shell_type"`
	SupportVerbosity           bool             `json:"support_verbosity"`
	SupportedInAPI             bool             `json:"supported_in_api"`
	SupportedReasoningLevels   []ReasoningLevel `json:"supported_reasoning_levels"`
	DefaultReasoningLevel      string           `json:"default_reasoning_level,omitempty"`
	SupportsParallelToolCalls  bool             `json:"supports_parallel_tool_calls"`
	SupportsReasoningSummaries bool             `json:"supports_reasoning_summaries"`
	TruncationPolicy           TruncationPolicy `json:"truncation_policy"`
	Visibility                 string           `json:"visibility"`
	// ContextWindow 是总上下文窗口（输入+输出），单位 token。
	ContextWindow    int `json:"context_window"`
	MaxContextWindow int `json:"max_context_window"`
}

// Catalog 是一份带版本的模型目录。
type Catalog struct {
	Version string  `json:"version"`
	Models  []Model `json:"models"`
}

// current 是当前生效的 catalog。DeepSeek 双模型的窗口取自官方 API 文档
// （deepseek-chat / deepseek-reasoner：128K 上下文），
// 条目与 modelroute 的规范名（deepseek-v4-flash / deepseek-v4-pro）对齐。
var current = Catalog{
	Version: Version,
	Models: []Model{
		{
			Slug:                       "deepseek-v4-flash",
			DisplayName:                "DeepSeek V4 Flash",
			Description:                "Fast DeepSeek coding model served through the Grix Responses gateway.",
			BaseInstructions:           "You are a coding agent. Follow the developer instructions and use the provided tools to help the user.",
			ExperimentalSupportedTools: []string{},
			Priority:                   1,
			ShellType:                  "shell_command",
			SupportVerbosity:           false,
			SupportedInAPI:             true,
			SupportedReasoningLevels:   []ReasoningLevel{},
			SupportsParallelToolCalls:  true,
			SupportsReasoningSummaries: false,
			TruncationPolicy:           TruncationPolicy{Mode: "tokens", Limit: 10000},
			Visibility:                 "list",
			ContextWindow:              131072,
			MaxContextWindow:           131072,
		},
		{
			Slug:                       "deepseek-v4-pro",
			DisplayName:                "DeepSeek V4 Pro",
			Description:                "DeepSeek reasoning coding model served through the Grix Responses gateway.",
			BaseInstructions:           "You are a coding agent. Follow the developer instructions and use the provided tools to help the user.",
			ExperimentalSupportedTools: []string{},
			Priority:                   2,
			ShellType:                  "shell_command",
			SupportVerbosity:           false,
			SupportedInAPI:             true,
			// reasoning 档位值以厂商 Responses 端点实际接受值为准，新增档位前先实测。
			SupportedReasoningLevels: []ReasoningLevel{
				{Effort: "low", Description: "Fast responses with lighter reasoning"},
				{Effort: "medium", Description: "Balanced speed and reasoning"},
				{Effort: "high", Description: "Greater reasoning depth"},
			},
			DefaultReasoningLevel:      "high",
			SupportsParallelToolCalls:  true,
			SupportsReasoningSummaries: false,
			TruncationPolicy:           TruncationPolicy{Mode: "tokens", Limit: 10000},
			Visibility:                 "list",
			ContextWindow:              131072,
			MaxContextWindow:           131072,
		},
	},
}

// Current 返回当前生效 catalog 的拷贝，调用方修改返回值不影响全局。
func Current() Catalog {
	models := make([]Model, len(current.Models))
	for i, item := range current.Models {
		models[i] = item
		models[i].ExperimentalSupportedTools = append([]string(nil), item.ExperimentalSupportedTools...)
		models[i].SupportedReasoningLevels = append([]ReasoningLevel(nil), item.SupportedReasoningLevels...)
	}
	return Catalog{Version: current.Version, Models: models}
}

// CanonicalJSON 返回当前 catalog 的规范 JSON 字节（结构体字段序固定 = 同版本摘要恒定）。
// 这份字节同时是凭证响应里内嵌下发给连接器的 catalog 负载（交付方式 A）。
func CanonicalJSON() []byte {
	raw, err := json.Marshal(current)
	if err != nil {
		// 纯内存结构体 marshal 不可能失败；真失败是代码事故，panic 让它在测试期现形。
		panic("modelcatalog: marshal catalog: " + err.Error())
	}
	return raw
}

// SHA256 返回 CanonicalJSON() 的 hex 摘要，随凭证响应下发供连接器校验。
func SHA256() string {
	sum := sha256.Sum256(CanonicalJSON())
	return hex.EncodeToString(sum[:])
}

// Has 判断模型 ID 是否在当前 catalog 中（直连 capability 只对有 catalog 条目的模型声明支持）。
func Has(modelID string) bool {
	for _, m := range current.Models {
		if m.Slug == modelID {
			return true
		}
	}
	return false
}
