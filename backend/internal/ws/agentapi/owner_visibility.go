package agentapi

import (
	"encoding/json"
	"strings"
)

// OwnerVisibleToForAdapterCard is the exported form used by the agent API send
// bridge as defense-in-depth when a caller omits VisibleTo on an owner-only card.
func OwnerVisibleToForAdapterCard(adapterID, content string, extraRaw json.RawMessage, ownerID int64) []int64 {
	return ownerVisibleToForAdapterCard(adapterID, content, extraRaw, ownerID)
}

// ownerVisibleToForAdapterCard scopes owner-only cards to [ownerID].
//
// Privacy fail-closed: matching card content/extra is enough. An empty or
// unknown adapterID must NOT widen the card to the whole group — that was the
// production leak where exec_approval stayed hidden in chat (client/history
// filtered by visible_to when set) but still reached peers when visibility
// was dropped to nil before send_msg fan-out. adapterID is retained in the
// signature for call-site compatibility and the adapter-family registry guard.
func ownerVisibleToForAdapterCard(adapterID, content string, extraRaw json.RawMessage, ownerID int64) []int64 {
	_ = adapterID
	if ownerID <= 0 {
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
		normalized == "deveco" || strings.HasPrefix(normalized, "deveco/") ||
		normalized == "kiro" || strings.HasPrefix(normalized, "kiro/") ||
		normalized == "copilot" || strings.HasPrefix(normalized, "copilot/") ||
		normalized == "kimi" || strings.HasPrefix(normalized, "kimi/") ||
		normalized == "agy" || strings.HasPrefix(normalized, "agy/") ||
		normalized == "openhuman" || strings.HasPrefix(normalized, "openhuman/") ||
		normalized == "deepseek" || strings.HasPrefix(normalized, "deepseek/") ||
		normalized == "qodercli" || strings.HasPrefix(normalized, "qodercli/") ||
		normalized == "qoderclicn" || strings.HasPrefix(normalized, "qoderclicn/") ||
		normalized == "mcode" || strings.HasPrefix(normalized, "mcode/") ||
		normalized == "dim" || strings.HasPrefix(normalized, "dim/") ||
		normalized == "traecli" || strings.HasPrefix(normalized, "traecli/") ||
		normalized == "omp" || strings.HasPrefix(normalized, "omp/") ||
		normalized == "codebuddy" || strings.HasPrefix(normalized, "codebuddy/") ||
		normalized == "grok" || strings.HasPrefix(normalized, "grok/") ||
		normalized == "qwenpaw" || strings.HasPrefix(normalized, "qwenpaw/") ||
		normalized == "zeroclaw" || strings.HasPrefix(normalized, "zeroclaw/") ||
		normalized == "acp" || strings.HasPrefix(normalized, "acp/")
}

func isOwnerVisibilityCard(content string, extraRaw json.RawMessage) bool {
	normalized := strings.ToLower(strings.TrimSpace(content))
	if normalized != "" && ownerVisibilityCardInContent(normalized) {
		return true
	}
	return isOwnerVisibilityExtra(extraRaw)
}

// ownerVisibilityCardInContent matches agent↔owner interaction cards that must
// stay owner-only in group chats. Default-safe: miss one type and peers get
// inbox + offline push (prod evidence: agent_status with visible_to NULL landed
// in non-owner user_inbox). tool_execution* is intentionally excluded — process
// noise is suppressed at the push layer instead.
func ownerVisibilityCardInContent(content string) bool {
	markers := []string{
		"grix://card/agent_open_session",
		"grix://card/exec_approval",
		"grix://card/exec_status",
		"grix://card/call_owner",
		"grix://card/agent_status",
		"grix://card/agent_question", // also matches agent_question_reply
	}
	for _, marker := range markers {
		if strings.Contains(content, marker) {
			return true
		}
	}
	return false
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
	switch cardType {
	case "agent_open_session",
		"exec_approval",
		"exec_status",
		"call_owner",
		"agent_status",
		"agent_question",
		"agent_question_reply":
		return true
	default:
		return false
	}
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
