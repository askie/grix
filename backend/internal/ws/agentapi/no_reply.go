package agentapi

import (
	"context"
	"strings"
)

const NoReplyCommand = "/no_reply"

const NoReplyProtocolInstruction = `静默协议：当这类上游内部任务、主动引导、工具或调度指令判定“不需要给用户回复”时，必须只返回固定命令 /no_reply；不要返回“选择沉默”“无需引导”“快照显示”等解释文字。服务端收到 /no_reply 会静默 ACK，不会入库或推送给用户。`

func AppendNoReplyProtocolInstruction(content string) string {
	trimmed := strings.TrimSpace(content)
	if trimmed == "" || strings.Contains(trimmed, NoReplyCommand) {
		return trimmed
	}
	return trimmed + "\n\n" + NoReplyProtocolInstruction
}

func IsNoReplyCommand(content string) bool {
	return strings.TrimSpace(content) == NoReplyCommand
}

func ShouldSilentlyAckInboundOutput(content string, noReplyContext bool) bool {
	if IsNoReplyCommand(content) {
		return true
	}
	return noReplyContext && looksLikeInternalNoReplyExplanation(content)
}

func looksLikeInternalNoReplyExplanation(content string) bool {
	normalized := strings.TrimSpace(content)
	if normalized == "" {
		return false
	}

	for _, needle := range internalNoReplyNeedles() {
		if strings.Contains(normalized, needle) {
			return true
		}
	}
	return internalNoReplyScore(normalized) >= 2
}

func internalNoReplyNeedles() []string {
	return []string{
		"选择沉默",
		"无需额外引导",
		"无需引导",
		"不需要引导",
		"无需发送",
		"无需回复",
		"保持沉默",
		"核心新手路径已完成",
		"正处于活跃使用状态",
		"快照显示",
		"我来判断是否需要给这位用户发引导消息",
		"我需要用 grix_reply",
		"我需要用grix_reply",
		"查看它的 schema",
		"这是一个快照触发",
		"注册时间等于触发时间",
		"注册时间=触发时间",
		"根据快照引导规则",
		"根据我的记忆规则",
		"发给用户的只能是自然客服口吻",
		"严禁把任何分析、推理、决策过程发给用户",
	}
}

func internalNoReplyScore(content string) int {
	score := 0
	groups := [][]string{
		{"根据快照", "快照触发", "用户状态快照", "<snapshot_markdown>", "新手引导规则"},
		{"我来判断", "我需要发送", "我需要用", "让我确认", "先看用户状态"},
		{"grix_reply", "schema", "tool", "工具"},
		{"用户ID", "用户 ID", "注册时间", "触发时间", "Agent总数", "Agent 总数"},
		{"内部上下文", "不是用户消息", "不要原样复述", "决策过程"},
	}
	for _, group := range groups {
		for _, needle := range group {
			if strings.Contains(content, needle) {
				score++
				break
			}
		}
	}
	return score
}

func ShouldAttachNoReplyProtocol(evt DelegateEventPayload) bool {
	return isNoReplyProtocolEvent(evt)
}

func (m *Manager) IsNoReplyProtocolContext(eventID string) bool {
	eventID = strings.TrimSpace(eventID)
	if eventID == "" {
		return false
	}
	if isNoReplyProtocolEventID(eventID) {
		return true
	}
	if m != nil {
		m.acksMu.Lock()
		entry := m.pending[eventID]
		m.acksMu.Unlock()
		if entry != nil && isNoReplyProtocolEvent(entry.event) {
			return true
		}
	}
	if record, ok := loadDurablePendingDelegate(context.Background(), eventID); ok && record != nil {
		return isNoReplyProtocolEvent(record.Event)
	}
	return false
}

func IsNoReplyProtocolContext(eventID string) bool {
	if mgr := GetGlobal(); mgr != nil {
		return mgr.IsNoReplyProtocolContext(eventID)
	}
	return isNoReplyProtocolEventID(eventID)
}

func isNoReplyProtocolEvent(evt DelegateEventPayload) bool {
	eventType := strings.ToLower(strings.TrimSpace(evt.EventType))
	if eventType == "" {
		return isNoReplyProtocolEventID(evt.EventID) || contentLooksLikeNoReplyProtocolContext(evt.Content)
	}
	if strings.Contains(eventType, "customer_coach") ||
		strings.Contains(eventType, "snapshot") ||
		strings.Contains(eventType, "internal") ||
		strings.Contains(eventType, "system") ||
		strings.Contains(eventType, "proactive") ||
		strings.Contains(eventType, "schedule") ||
		strings.Contains(eventType, "dispatch") {
		return true
	}
	return isNoReplyProtocolEventID(evt.EventID)
}

func isNoReplyProtocolEventID(eventID string) bool {
	normalized := strings.ToLower(strings.TrimSpace(eventID))
	if normalized == "" {
		return false
	}
	prefixes := []string{
		"customer_coach:",
		"internal:",
		"internal-",
		"system:",
		"system-",
		"profile-update:",
		"schedule:",
		"scheduler:",
	}
	for _, prefix := range prefixes {
		if strings.HasPrefix(normalized, prefix) {
			return true
		}
	}
	return false
}

func contentLooksLikeNoReplyProtocolContext(content string) bool {
	normalized := strings.ToLower(strings.TrimSpace(content))
	if normalized == "" {
		return false
	}
	return strings.Contains(normalized, "[system-profile-update]") ||
		strings.Contains(normalized, "<snapshot_markdown>") ||
		strings.Contains(normalized, "不是用户消息") ||
		strings.Contains(normalized, "内部上下文") ||
		strings.Contains(normalized, "主动发一条引导") ||
		strings.Contains(normalized, "新手引导")
}
