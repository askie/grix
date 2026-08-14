package claude

import (
	"encoding/json"
	"net/url"
	"strings"

	"github.com/askie/grix/backend/internal/agentadapter"
	"github.com/askie/grix/backend/internal/agentadapter/agentcards"
	"github.com/askie/grix/backend/internal/agentadapter/approvalcards"
	"github.com/askie/grix/backend/internal/agentadapter/grixcards"
	"github.com/askie/grix/backend/internal/agentadapter/internal/util"
)

func normalizeInboundSendMsg(rawPayload []byte) (*agentadapter.NormalizedInboundEvent, error) {
	payload, err := agentadapter.ParseInboundPayload(rawPayload)
	if err != nil {
		return nil, err
	}

	if shouldSuppressHookNotification(payload.Content, payload.Extra) {
		return &agentadapter.NormalizedInboundEvent{Drop: true}, nil
	}

	content, extra := normalizeStructuredInboundCard(payload)
	return &agentadapter.NormalizedInboundEvent{
		SessionID: strings.TrimSpace(payload.SessionID),
		Content:   content,
		Extra:     extra,
	}, nil
}

func shouldSuppressHookNotification(content string, extraRaw json.RawMessage) bool {
	if len(extraRaw) == 0 {
		return false
	}
	var extra hookNotificationExtra
	if json.Unmarshal(extraRaw, &extra) != nil || !extra.GrixHookNotification {
		return false
	}
	if extra.HookEventName == "Stop" || extra.HookEventName == "PreToolUse" {
		return true
	}
	if extra.HookEventName == "PostToolUse" {
		summary := stripEmojiPrefix(content)
		if strings.HasPrefix(summary, "mcp__grix") || strings.HasPrefix(summary, "grix_") || summary == "TaskUpdate" || summary == "unknown" {
			return true
		}
	}
	return false
}

type hookNotificationExtra struct {
	GrixHookNotification bool            `json:"grix_hook_notification"`
	HookEventName        string          `json:"hook_event_name"`
	ToolInput            json.RawMessage `json:"tool_input,omitempty"`
}

func normalizeHookNotificationCard(content string, extraRaw json.RawMessage) (string, json.RawMessage, bool) {
	if len(extraRaw) == 0 {
		return "", nil, false
	}
	var extra hookNotificationExtra
	if err := json.Unmarshal(extraRaw, &extra); err != nil {
		return "", nil, false
	}
	if !extra.GrixHookNotification {
		return "", nil, false
	}

	toolName := stripEmojiPrefix(util.NormalizeText(content))
	if toolName == "" {
		return "", nil, false
	}

	summary := toolName
	if extra.HookEventName == "PostToolUse" || extra.HookEventName == "PostToolUseFailure" {
		summary = formatToolSummary(toolName, extra.ToolInput)
	}

	payload := map[string]any{
		"summary_text": summary,
	}
	if detail := util.NormalizeText(extra.HookEventName); detail != "" {
		payload["detail_text"] = detail
	}

	cardLink := buildGrixCardLink(
		"[Tool] "+compactCardText(summary, 180),
		"tool_execution",
		payload,
	)
	return cardLink, util.CloneRawMessage(extraRaw), true
}

func formatToolSummary(toolName string, toolInputRaw json.RawMessage) string {
	if len(toolInputRaw) == 0 {
		return toolName
	}
	var input map[string]any
	if err := json.Unmarshal(toolInputRaw, &input); err != nil {
		return toolName
	}

	if v := jsonStr(input, "command"); v != "" {
		return toolName + ": " + compactCardText(v, 120)
	}
	if v := jsonStr(input, "file_path"); v != "" {
		return toolName + ": " + v
	}
	if v := jsonStr(input, "pattern"); v != "" {
		return toolName + ": " + compactCardText(v, 120)
	}
	if v := jsonStr(input, "notebook_path"); v != "" {
		return toolName + ": " + v
	}
	if v := jsonStr(input, "query"); v != "" {
		return toolName + ": " + compactCardText(v, 120)
	}
	if v := jsonStr(input, "task_id"); v != "" {
		return toolName + ": " + v
	}
	if v := jsonStr(input, "description"); v != "" {
		return toolName + ": " + compactCardText(v, 80)
	}
	if v := jsonStr(input, "skill"); v != "" {
		return toolName + ": " + v
	}
	return toolName
}

func jsonStr(m map[string]any, key string) string {
	v, _ := m[key].(string)
	return strings.TrimSpace(v)
}

func stripEmojiPrefix(text string) string {
	// Emoji prefix was removed from grix-claude outbound text; return as-is.
	return text
}

func compactCardText(text string, limit int) string {
	if limit <= 3 || len(text) <= limit {
		return text
	}
	return text[:limit-3] + "..."
}

func normalizeStructuredInboundCard(p *agentadapter.InboundSendMsgPayload) (string, json.RawMessage) {
	if normalizedContent, normalizedExtra, ok := normalizeHookNotificationCard(p.Content, p.Extra); ok {
		return normalizedContent, normalizedExtra
	}
	if normalizedContent, normalizedExtra, ok := approvalcards.Normalize(p); ok {
		return normalizedContent, normalizedExtra
	}
	if normalizedContent, normalizedExtra, ok := grixcards.Normalize(p); ok {
		return normalizedContent, normalizedExtra
	}
	if normalizedContent, normalizedExtra, ok := agentcards.Normalize(p); ok {
		return normalizedContent, normalizedExtra
	}

	if len(p.ChannelData) > 0 {
		channelData := util.DecodeJSONObject(p.ChannelData)
		if len(channelData) > 0 {
			if cardContent, ok := buildSessionBindingCardContent(channelData); ok {
				return cardContent, util.CloneRawMessage(p.Extra)
			}
		}
	}

	return p.Content, util.CloneRawMessage(p.Extra)
}

func buildSessionBindingCardContent(channelData map[string]any) (string, bool) {
	record := util.NestedObject(util.NestedObject(channelData, "grix-claude"), "sessionBinding")
	if len(record) == 0 {
		return "", false
	}

	status := util.NormalizeText(record["status"])
	reason := util.NormalizeText(record["reason"])
	if status != "missing" && reason != "binding_missing" {
		return "", false
	}

	payload := map[string]any{
		"summary_text": "当前对话还没有打开工作目录。",
		"detail_text":  "先提交一个工作目录。校验通过后，Claude 会自动继续处理刚才那条消息。",
	}
	if initialCwd := util.FirstNonEmpty(
		util.NormalizeText(record["initial_cwd"]),
		util.NormalizeText(record["initialCwd"]),
	); initialCwd != "" {
		payload["initial_cwd"] = initialCwd
	}
	if submittedPath := util.FirstNonEmpty(
		util.NormalizeText(record["submitted_path"]),
		util.NormalizeText(record["submittedPath"]),
	); submittedPath != "" {
		payload["submitted_path"] = submittedPath
	}

	return buildGrixCardLink(
		"[Open Workspace] 当前对话还没有打开工作目录。",
		"agent_open_session",
		payload,
	), true
}

func buildGrixCardLink(fallbackText, cardType string, payload map[string]any) string {
	return "[" + fallbackText + "](" + buildGrixCardURI(cardType, payload) + ")"
}

func buildGrixCardURI(cardType string, payload map[string]any) string {
	values := url.Values{}
	for key, value := range payload {
		text := util.NormalizeText(value)
		if text == "" {
			continue
		}
		values.Set(key, text)
	}
	return (&url.URL{
		Scheme:   "grix",
		Host:     "card",
		Path:     "/" + strings.TrimSpace(cardType),
		RawQuery: values.Encode(),
	}).String()
}

