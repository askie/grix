package agentapi

import (
	"encoding/json"
	"strings"
)

func ownerVisibleToForAdapterCard(adapterID, content string, extraRaw json.RawMessage, ownerID int64) []int64 {
	if ownerID <= 0 {
		return nil
	}
	if !isOwnerVisibilityAdapter(adapterID) {
		return nil
	}
	if !isOwnerVisibilityCard(content, extraRaw) {
		return nil
	}
	return []int64{ownerID}
}

func isOwnerVisibilityAdapter(adapterID string) bool {
	normalized := strings.ToLower(strings.TrimSpace(adapterID))
	if normalized == "" {
		return false
	}
	return normalized == "claude" || strings.HasPrefix(normalized, "claude/") ||
		normalized == "gemini" || strings.HasPrefix(normalized, "gemini/") ||
		normalized == "codex" || strings.HasPrefix(normalized, "codex/") ||
		normalized == "cursor" || strings.HasPrefix(normalized, "cursor/") ||
		normalized == "qwen" || strings.HasPrefix(normalized, "qwen/") ||
		normalized == "openclaw" || strings.HasPrefix(normalized, "openclaw/") ||
		normalized == "hermes" || strings.HasPrefix(normalized, "hermes/") ||
		normalized == "pi" || strings.HasPrefix(normalized, "pi/") ||
		normalized == "reasonix" || strings.HasPrefix(normalized, "reasonix/") ||
		normalized == "codewhale" || strings.HasPrefix(normalized, "codewhale/") ||
		normalized == "opencode" || strings.HasPrefix(normalized, "opencode/") ||
		normalized == "kiro" || strings.HasPrefix(normalized, "kiro/") ||
		normalized == "copilot" || strings.HasPrefix(normalized, "copilot/") ||
		normalized == "kimi" || strings.HasPrefix(normalized, "kimi/") ||
		normalized == "agy" || strings.HasPrefix(normalized, "agy/") ||
		normalized == "openhuman" || strings.HasPrefix(normalized, "openhuman/") ||
		normalized == "deepseek" || strings.HasPrefix(normalized, "deepseek/") ||
		normalized == "acp" || strings.HasPrefix(normalized, "acp/")
}

func isOwnerVisibilityCard(content string, extraRaw json.RawMessage) bool {
	normalized := strings.ToLower(strings.TrimSpace(content))
	if normalized != "" && (strings.Contains(normalized, "grix://card/agent_open_session") ||
		strings.Contains(normalized, "grix://card/exec_approval") ||
		strings.Contains(normalized, "grix://card/exec_status")) {
		return true
	}
	return isOwnerVisibilityExtra(extraRaw)
}

func isOwnerVisibilityExtra(extraRaw json.RawMessage) bool {
	if len(extraRaw) == 0 {
		return false
	}
	var envelope map[string]any
	if err := json.Unmarshal(extraRaw, &envelope); err != nil {
		return false
	}

	if isOwnerVisibilityBizCard(asMap(envelope["biz_card"])) {
		return true
	}
	return isOwnerVisibilityChannelData(asMap(envelope["channel_data"]))
}

func isOwnerVisibilityBizCard(bizCard map[string]any) bool {
	if len(bizCard) == 0 {
		return false
	}
	cardType := strings.TrimSpace(strings.ToLower(asString(bizCard["type"])))
	return cardType == "agent_open_session" || cardType == "exec_approval" || cardType == "exec_status"
}

func isOwnerVisibilityChannelData(channelData map[string]any) bool {
	if len(channelData) == 0 {
		return false
	}

	if len(asMap(channelData["execApproval"])) > 0 {
		return true
	}

	grix := asMap(channelData["grix"])
	if len(asMap(grix["execApproval"])) > 0 || len(asMap(grix["execStatus"])) > 0 {
		return true
	}

	if hasMissingSessionBinding(asMap(asMap(channelData["codex"])["sessionBinding"])) {
		return true
	}
	if hasMissingSessionBinding(asMap(asMap(channelData["cursor"])["sessionBinding"])) {
		return true
	}
	if hasMissingSessionBinding(asMap(asMap(channelData["qwen"])["sessionBinding"])) {
		return true
	}
	if hasMissingSessionBinding(asMap(asMap(channelData["grix-claude"])["sessionBinding"])) {
		return true
	}
	if hasMissingSessionBinding(asMap(asMap(channelData["gemini"])["sessionBinding"])) {
		return true
	}
	if hasMissingSessionBinding(asMap(asMap(channelData["acp"])["sessionBinding"])) {
		return true
	}
	if hasMissingSessionBinding(asMap(asMap(channelData["pi"])["sessionBinding"])) {
		return true
	}
	if hasMissingSessionBinding(asMap(asMap(channelData["codewhale"])["sessionBinding"])) {
		return true
	}
	if hasMissingSessionBinding(asMap(asMap(channelData["opencode"])["sessionBinding"])) {
		return true
	}
	if hasMissingSessionBinding(asMap(asMap(channelData["deepseek"])["sessionBinding"])) {
		return true
	}

	return false
}

func hasMissingSessionBinding(record map[string]any) bool {
	if len(record) == 0 {
		return false
	}

	status := strings.ToLower(strings.TrimSpace(asString(record["status"])))
	reason := strings.ToLower(strings.TrimSpace(asString(record["reason"])))
	errorCode := strings.ToLower(strings.TrimSpace(asString(record["error_code"])))
	return status == "missing" || reason == "binding_missing" || errorCode == "session_binding_missing"
}

func asMap(value any) map[string]any {
	object, _ := value.(map[string]any)
	return object
}

func asString(value any) string {
	text, _ := value.(string)
	return text
}

// resolveTriggerVisibleTo returns the cached trigger message visible_to from
// the active run. No DB query — the value was loaded once at registerActiveRun.
func (m *Manager) resolveTriggerVisibleTo(eventID, sessionID string) []int64 {
	eventID = strings.TrimSpace(eventID)
	if eventID == "" {
		return nil
	}
	run := m.LookupActiveRun(eventID)
	if run == nil {
		return nil
	}
	return run.TriggerVisibleTo
}

// mergeVisibleToForSendMsg combines card-type visibility with trigger message
// visibility. If the trigger message was a hidden message, triggerVisibleTo
// holds the sender's ID and the response is directed back to that sender.
// Otherwise, card-type visibility is used.
func mergeVisibleToForSendMsg(cardVisibleTo, triggerVisibleTo []int64) []int64 {
	if len(triggerVisibleTo) > 0 {
		return append([]int64(nil), triggerVisibleTo...)
	}
	if len(cardVisibleTo) > 0 {
		return append([]int64(nil), cardVisibleTo...)
	}
	return nil
}
