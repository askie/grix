// Package modelroute 是"模型名 → 厂商"的唯一路由知识：哪些前缀归哪个厂商、
// 哪些厂商当前真有转发适配器、旧别名怎么收敛成计价用的规范名。
//
// 单独成包是因为有两个消费方必须用同一份判定：
//   - 网关请求链路（internal/gateway/httpapi）：决定这次请求路由到谁、按什么名字计价；
//   - C端"可用模型清单"（internal/api/service）：决定哪些价目规则算"用户可选的模型"。
//
// 两边判定一旦不一致，清单里就会出现网关根本服务不了的模型（塘主录一条 openai/gpt-5
// 的价目，它就进了用户下拉，用户选中当兜底 → 所有请求 400）。这正是"清单污染"，
// 必须从数据源上掐死，不能靠两边碰巧一致。
package modelroute

import "strings"

// SupportedProviders 是网关目前能构造转发适配器的厂商集合。
var SupportedProviders = map[string]bool{"deepseek": true, "volcano_ark": true}

// ResolveProvider 按模型名前缀决定路由到哪个厂商。接入新厂商时在这里加前缀判断。
func ResolveProvider(model string) string {
	if strings.HasPrefix(model, "deepseek") {
		return "deepseek"
	}
	if strings.HasPrefix(model, "doubao") {
		return "volcano_ark"
	}
	if strings.HasPrefix(model, "gpt") || strings.HasPrefix(model, "o1") || strings.HasPrefix(model, "o3") {
		return "openai"
	}
	return ""
}

// CanonicalModel 把厂商即将下线的旧别名映射成计价用的规范模型名。
// DeepSeek 的 deepseek-chat/deepseek-reasoner 就是这种情况（2026-07-24 下线，实际是 v4-flash/v4-pro 的别名）。
func CanonicalModel(provider, requested string) string {
	if provider == "deepseek" {
		switch requested {
		case "deepseek-chat":
			return "deepseek-v4-flash"
		case "deepseek-reasoner":
			return "deepseek-v4-pro"
		}
	}
	return requested
}

// Routable 判断一个模型名能否被网关路由到一个真有转发适配器的厂商。
// 这是"模型可用"的必要条件（充分条件还要价目表里有基准价）。
func Routable(model string) bool {
	p := ResolveProvider(model)
	return p != "" && SupportedProviders[p]
}

// NativeAnthropicMessages 判断厂商是否原生支持 Anthropic Messages 协议
// （即 httpapi.buildAnthropicUpstream 能为它构造适配器）。直连（direct relay）的
// Claude capability 只允许对返回 true 的厂商声明 supported，连接器不得自行猜测。
func NativeAnthropicMessages(provider string) bool {
	return provider == "deepseek"
}

// NativeResponses 判断厂商的上游适配器是否原生实现 upstream.ResponsesUpstream。
// 直连（direct relay）的 Codex capability 只允许对返回 true 的厂商声明 supported——
// 其余厂商在网关侧走 Responses↔Chat 桥接，direct Codex 不得静默降级进桥接。
// 新增厂商时必须先验证 tools/reasoning/usage/SSE/错误语义，再在这里登记。
func NativeResponses(provider string) bool {
	return provider == "deepseek"
}
