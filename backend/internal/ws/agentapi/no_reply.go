package agentapi

import (
	"context"
	"strings"
)

const NoReplyCommand = "/no_reply"

const NoReplyProtocolInstruction = `静默协议：当这类上游内部任务、主动引导、工具或调度指令判定"不需要给用户回复"时，必须只返回固定命令 /no_reply；不要返回"选择沉默""无需引导""快照显示"等解释文字。服务端收到 /no_reply 会静默 ACK，不会入库或推送给用户。`

func AppendNoReplyProtocolInstruction(content string) string {
	trimmed := strings.TrimSpace(content)
	if trimmed == "" || strings.Contains(trimmed, NoReplyCommand) {
		return trimmed
	}
	return trimmed + "\n\n" + NoReplyProtocolInstruction
}

// IsNoReplyCommand reports whether the output is the /no_reply command. A
// trailing explanation after the command ("/no_reply — nothing to add") still
// counts: the model chose silence and the explanation must not reach the user.
// "/no_reply_x" style tokens are not the command.
func IsNoReplyCommand(content string) bool {
	trimmed := strings.TrimSpace(content)
	if !strings.HasPrefix(trimmed, NoReplyCommand) {
		return false
	}
	rest := trimmed[len(NoReplyCommand):]
	if rest == "" {
		return true
	}
	r := rune(rest[0])
	return !(r == '_' || (r >= '0' && r <= '9') || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z'))
}

// ShouldSilentlyAckInboundOutput reports whether an agent output must be
// silently acked instead of delivered. Outside a no-reply protocol context
// only an exact /no_reply counts, so a normal reply that merely starts with
// the command (e.g. quoting it to the user) is never swallowed.
func ShouldSilentlyAckInboundOutput(content string, noReplyContext bool) bool {
	if noReplyContext {
		return IsNoReplyCommand(content)
	}
	return strings.TrimSpace(content) == NoReplyCommand
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
