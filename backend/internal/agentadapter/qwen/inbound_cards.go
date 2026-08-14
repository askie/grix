package qwen

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	"github.com/askie/grix/backend/internal/agentadapter"
	"github.com/askie/grix/backend/internal/agentadapter/agentcards"
	"github.com/askie/grix/backend/internal/agentadapter/approvalcards"
	"github.com/askie/grix/backend/internal/agentadapter/grixcards"
	"github.com/askie/grix/backend/internal/agentadapter/internal/util"
)

func normalizeStructuredInboundCard(p *agentadapter.InboundSendMsgPayload) (string, json.RawMessage) {
	if cardContent, normalizedExtra, ok := approvalcards.Normalize(p); ok {
		return cardContent, normalizedExtra
	}
	if cardContent, normalizedExtra, ok := grixcards.Normalize(p); ok {
		return cardContent, normalizedExtra
	}
	if cardContent, normalizedExtra, ok := agentcards.Normalize(p); ok {
		return cardContent, normalizedExtra
	}

	if normalizedP, ok := synthesizeQwenApprovalPayload(p); ok {
		if cardContent, normalizedExtra, ok := approvalcards.Normalize(normalizedP); ok {
			return cardContent, normalizedExtra
		}
		return p.Content, util.CloneRawMessage(p.Extra)
	}
	if len(p.ChannelData) > 0 {
		channelData := util.DecodeJSONObject(p.ChannelData)
		if len(channelData) > 0 {
			if cardContent, ok := buildQwenSessionBindingCardContent(channelData); ok {
				return cardContent, util.CloneRawMessage(p.Extra)
			}
		}
	}

	return p.Content, util.CloneRawMessage(p.Extra)
}

func synthesizeQwenApprovalPayload(p *agentadapter.InboundSendMsgPayload) (*agentadapter.InboundSendMsgPayload, bool) {
	channelData := util.DecodeJSONObject(p.ChannelData)
	if len(channelData) == 0 {
		return nil, false
	}

	requestPermission := util.NestedObject(util.NestedObject(channelData, "qwen"), "requestPermission")
	if len(requestPermission) == 0 {
		return nil, false
	}

	toolCall := util.NestedObject(requestPermission, "toolCall")
	approvalID := util.NormalizeText(toolCall["toolCallId"])
	command := resolveQwenApprovalCommand(requestPermission)
	if approvalID == "" || command == "" {
		return nil, false
	}

	replyMeta := util.CloneJSONObject(util.NestedObject(channelData, "execApproval"))
	if len(replyMeta) == 0 {
		replyMeta = map[string]any{}
	}
	replyMeta["approvalId"] = approvalID
	if util.NormalizeText(replyMeta["approvalSlug"]) == "" {
		replyMeta["approvalSlug"] = approvalID
	}
	if allowedDecisions := resolveQwenApprovalDecisions(requestPermission["options"]); len(allowedDecisions) > 0 {
		replyMeta["allowedDecisions"] = allowedDecisions
	}
	channelData["execApproval"] = replyMeta

	grixData := util.CloneJSONObject(util.NestedObject(channelData, "grix"))
	if len(grixData) == 0 {
		grixData = map[string]any{}
	}
	grixApproval := map[string]any{
		"approval_command_id": approvalID,
		"command":             command,
		"host":                "qwen",
	}
	if warningText := resolveQwenApprovalWarningText(requestPermission, command); warningText != "" {
		grixApproval["warning_text"] = warningText
	}
	grixData["execApproval"] = grixApproval
	channelData["grix"] = grixData

	channelDataRaw, _ := json.Marshal(channelData)
	result := &agentadapter.InboundSendMsgPayload{
		Content:     p.Content,
		Extra:       p.Extra,
		ChannelData: channelDataRaw,
	}
	agentadapter.MergeCardsIntoExtra(result)
	return result, true
}

func buildQwenSessionBindingCardContent(channelData map[string]any) (string, bool) {
	record := util.NestedObject(util.NestedObject(channelData, "qwen"), "sessionBinding")
	if len(record) == 0 {
		return "", false
	}

	status := util.NormalizeText(record["status"])
	reason := util.NormalizeText(record["reason"])
	errorCode := util.NormalizeText(record["error_code"])
	if status != "missing" && reason != "binding_missing" && errorCode != "session_binding_missing" {
		return "", false
	}

	payload := map[string]any{
		"summary_text": "当前对话还没有打开工作目录。",
		"detail_text":  "先提交一个工作目录。校验通过后，Qwen 会自动继续处理刚才那条消息。",
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

func resolveQwenApprovalCommand(requestPermission map[string]any) string {
	toolCall := util.NestedObject(requestPermission, "toolCall")
	rawInput := util.NestedObject(toolCall, "rawInput")
	title := util.NormalizeText(toolCall["title"])
	kind := util.NormalizeText(toolCall["kind"])

	if kind == "execute" {
		argv := joinStringArray(rawInput["argv"])
		return util.FirstNonEmpty(
			util.NormalizeText(rawInput["command"]),
			util.NormalizeText(rawInput["cmd"]),
			argv,
			title,
			"Qwen execute request",
		)
	}

	if kind == "edit" {
		return util.FirstNonEmpty(
			util.NormalizeText(rawInput["path"]),
			util.NormalizeText(rawInput["filePath"]),
			title,
			"Qwen edit request",
		)
	}

	return util.FirstNonEmpty(title, fmt.Sprintf("Qwen %s request", fallbackKind(kind)))
}

func resolveQwenApprovalWarningText(requestPermission map[string]any, command string) string {
	toolCall := util.NestedObject(requestPermission, "toolCall")
	title := util.NormalizeText(toolCall["title"])
	kind := util.NormalizeText(toolCall["kind"])
	if title != "" && title != command {
		return title
	}
	if kind != "" && kind != "execute" {
		return fmt.Sprintf("Qwen %s request", kind)
	}
	return ""
}

func resolveQwenApprovalDecisions(value any) []string {
	options, ok := value.([]any)
	if !ok {
		return nil
	}

	decisions := make([]string, 0, 3)
	appendDecision := func(decision string) {
		for _, existing := range decisions {
			if existing == decision {
				return
			}
		}
		decisions = append(decisions, decision)
	}

	for _, item := range options {
		option := util.AsJSONObject(item)
		switch util.NormalizeText(option["kind"]) {
		case "allow_once":
			appendDecision("allow-once")
		case "allow_always":
			appendDecision("allow-always")
		case "reject_once", "reject_always":
			appendDecision("deny")
		}
	}

	return decisions
}

func joinStringArray(value any) string {
	items, ok := value.([]any)
	if !ok {
		return ""
	}
	parts := make([]string, 0, len(items))
	for _, item := range items {
		text := util.NormalizeText(item)
		if text == "" {
			continue
		}
		parts = append(parts, text)
	}
	return strings.Join(parts, " ")
}

func fallbackKind(kind string) string {
	if kind == "" {
		return "tool"
	}
	return kind
}
