// Package grixcard is the single inventory of grix://card/* types used for
// group visibility and offline-push copy. New card types MUST be added here;
// the registry tests fail when a producer ships a type that is missing.
package grixcard

import (
	"encoding/json"
	"strings"
)

// Production card types observed in messages / card codecs (ordered by product
// family, not frequency). Keep this list exhaustive — the registry test locks it.
const (
	TypeExecApproval          = "exec_approval"
	TypeExecStatus            = "exec_status"
	TypeAgentOpenSession      = "agent_open_session"
	TypeAgentOpenSessionSubmit = "agent_open_session_submit"
	TypeAgentStatus           = "agent_status"
	TypeAgentQuestion         = "agent_question"
	TypeAgentQuestionReply    = "agent_question_reply"
	TypeCallOwner             = "call_owner"
	TypeEggInstallStatus      = "egg_install_status"
	TypeAgentPairing          = "agent_pairing"
	TypeToolExecution         = "tool_execution"
	TypeToolExecutionGroup    = "tool_execution_group"
)

// KnownTypes is the complete set of card types the platform produces today.
var KnownTypes = []string{
	TypeExecApproval,
	TypeExecStatus,
	TypeAgentOpenSession,
	TypeAgentOpenSessionSubmit,
	TypeAgentStatus,
	TypeAgentQuestion,
	TypeAgentQuestionReply,
	TypeCallOwner,
	TypeEggInstallStatus,
	TypeAgentPairing,
	TypeToolExecution,
	TypeToolExecutionGroup,
}

// publicInGroupTypes is the inverted allowlist: only these card types may fan
// out to every group member. Everything else that looks like a grix card is
// owner-only by default (fail closed). Currently empty — agent interaction and
// process cards are private to the agent owner.
var publicInGroupTypes = map[string]struct{}{}

// IsOwnerOnlyInGroup reports whether a detected card type must be scoped to
// the agent owner in group sessions. Unknown / empty types that still carry a
// grix://card/ URI are treated as owner-only by callers via HasCardURI.
func IsOwnerOnlyInGroup(cardType string) bool {
	cardType = strings.TrimSpace(strings.ToLower(cardType))
	if cardType == "" {
		return false
	}
	if _, ok := publicInGroupTypes[cardType]; ok {
		return false
	}
	return true
}

// HasCardURI reports whether content embeds any grix://card/ link.
func HasCardURI(content string) bool {
	return strings.Contains(strings.ToLower(content), "grix://card/")
}

// DetectType resolves the card type from content URI and/or extra envelopes.
// Longer type names are matched before their prefixes (question_reply before
// question, tool_execution_group before tool_execution, open_session_submit
// before open_session).
func DetectType(content string, extraRaw json.RawMessage) string {
	if t := typeFromContent(content); t != "" {
		return t
	}
	return typeFromExtra(extraRaw)
}

func typeFromContent(content string) string {
	normalized := strings.ToLower(content)
	if !strings.Contains(normalized, "grix://card/") {
		return ""
	}
	// Longest-prefix first so _reply / _group / _submit win over the base type.
	ordered := []string{
		TypeAgentQuestionReply,
		TypeAgentOpenSessionSubmit,
		TypeToolExecutionGroup,
		TypeExecApproval,
		TypeExecStatus,
		TypeAgentOpenSession,
		TypeAgentStatus,
		TypeAgentQuestion,
		TypeCallOwner,
		TypeEggInstallStatus,
		TypeAgentPairing,
		TypeToolExecution,
	}
	for _, t := range ordered {
		if strings.Contains(normalized, "grix://card/"+t) {
			return t
		}
	}
	// Unknown future card: extract path segment after grix://card/.
	idx := strings.Index(normalized, "grix://card/")
	if idx < 0 {
		return ""
	}
	rest := normalized[idx+len("grix://card/"):]
	end := len(rest)
	for i, r := range rest {
		if r == '?' || r == '#' || r == '/' || r == ')' || r == '"' || r == '\'' || r == ' ' || r == '\n' {
			end = i
			break
		}
	}
	return strings.TrimSpace(rest[:end])
}

func typeFromExtra(extraRaw json.RawMessage) string {
	if len(extraRaw) == 0 {
		return ""
	}
	var envelope map[string]any
	if err := json.Unmarshal(extraRaw, &envelope); err != nil {
		return ""
	}
	if biz, ok := envelope["biz_card"].(map[string]any); ok {
		if t, _ := biz["type"].(string); strings.TrimSpace(t) != "" {
			return strings.ToLower(strings.TrimSpace(t))
		}
	}
	if t, _ := envelope["card_type"].(string); strings.TrimSpace(t) != "" {
		return strings.ToLower(strings.TrimSpace(t))
	}
	channelData, _ := envelope["channel_data"].(map[string]any)
	if len(channelData) == 0 {
		return ""
	}
	if len(asMap(channelData["execApproval"])) > 0 {
		return TypeExecApproval
	}
	grix := asMap(channelData["grix"])
	if len(asMap(grix["execApproval"])) > 0 {
		return TypeExecApproval
	}
	if len(asMap(grix["execStatus"])) > 0 {
		return TypeExecStatus
	}
	if len(asMap(grix["toolExecution"])) > 0 {
		return TypeToolExecution
	}
	return ""
}

// IsProcessNoise is true for tool-execution process cards that must not become
// OS notifications (they still follow owner-only inbox scoping in groups).
func IsProcessNoise(cardType string) bool {
	switch strings.TrimSpace(strings.ToLower(cardType)) {
	case TypeToolExecution, TypeToolExecutionGroup:
		return true
	default:
		return false
	}
}

// PushBody returns human copy for a card. Never returns a string containing
// "grix://". Unknown types fall back to a generic phrase; callers may still
// prefer a markdown label when available.
func PushBody(cardType string) string {
	switch strings.TrimSpace(strings.ToLower(cardType)) {
	case TypeExecApproval:
		return "有任务需要审批"
	case TypeExecStatus:
		return "审批状态更新"
	case TypeCallOwner:
		return "请求与你语音通话"
	case TypeAgentQuestion:
		return "智能体有问题需要你回答"
	case TypeAgentQuestionReply:
		return "已回复智能体提问"
	case TypeAgentOpenSession, TypeAgentOpenSessionSubmit:
		return "需要打开工作目录"
	case TypeAgentStatus:
		return "智能体状态更新"
	case TypeEggInstallStatus:
		return "技能安装状态更新"
	case TypeAgentPairing:
		return "智能体配对请求"
	case TypeToolExecution, TypeToolExecutionGroup:
		return "智能体正在执行工具"
	default:
		return "收到一条智能体卡片消息"
	}
}

func asMap(value any) map[string]any {
	object, _ := value.(map[string]any)
	return object
}
