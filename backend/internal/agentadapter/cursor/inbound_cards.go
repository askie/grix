package cursor

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
	"github.com/askie/grix/backend/internal/pkg/logger"
)

func normalizeInboundSendMsg(rawPayload []byte) (*agentadapter.NormalizedInboundEvent, error) {
	p, err := agentadapter.ParseInboundPayload(rawPayload)
	if err != nil {
		return nil, err
	}

	content, extra, drop := normalizeStructuredInboundCard(p)
	return &agentadapter.NormalizedInboundEvent{
		SessionID: p.SessionID,
		Content:   content,
		Extra:     extra,
		Drop:      drop,
	}, nil
}

func normalizeStructuredInboundCard(p *agentadapter.InboundSendMsgPayload) (string, json.RawMessage, bool) {
	if normalizedPayload, ok := synthesizeCursorRawEventPayload(p); ok {
		if cardContent, normalizedExtra, ok := approvalcards.Normalize(normalizedPayload); ok {
			return cardContent, normalizedExtra, false
		}
		if cardContent, normalizedExtra, ok := grixcards.Normalize(normalizedPayload); ok {
			return cardContent, normalizedExtra, false
		}
		if cardContent, normalizedExtra, ok := agentcards.Normalize(normalizedPayload); ok {
			return cardContent, normalizedExtra, false
		}
		fallback := strings.TrimSpace(normalizedPayload.Content)
		if fallback == "" {
			fallback = "[cursor] raw_event"
		}
		return fallback, util.CloneRawMessage(normalizedPayload.Extra), false
	}
	if rawType, hasRaw := detectCursorRawEventType(p); hasRaw {
		// Cursor emits system/user raw_event as internal state signals; do not render them as chat messages.
		if rawType == "system" || rawType == "user" {
			return "", nil, true
		}
		if logger.L != nil {
			logger.L.Warnf("cursor adapter raw_event fallback to plain text: session=%s type=%s", strings.TrimSpace(p.SessionID), rawType)
		}
	}

	if cardContent, normalizedExtra, ok := approvalcards.Normalize(p); ok {
		return cardContent, normalizedExtra, false
	}
	if cardContent, normalizedExtra, ok := grixcards.Normalize(p); ok {
		return cardContent, normalizedExtra, false
	}
	if cardContent, normalizedExtra, ok := agentcards.Normalize(p); ok {
		return cardContent, normalizedExtra, false
	}
	if len(p.ChannelData) > 0 {
		channelData := util.DecodeJSONObject(p.ChannelData)
		if len(channelData) > 0 {
			if cardContent, ok := buildCursorSessionBindingCardContent(channelData); ok {
				return cardContent, util.CloneRawMessage(p.Extra), false
			}
		}
	}

	return p.Content, p.Extra, false
}

func detectCursorRawEventType(p *agentadapter.InboundSendMsgPayload) (string, bool) {
	_, eventType, _, ok := decodeCursorRawEventEnvelope(p)
	if !ok {
		return "", false
	}
	return eventType, true
}

func synthesizeCursorRawEventPayload(p *agentadapter.InboundSendMsgPayload) (*agentadapter.InboundSendMsgPayload, bool) {
	channelData, eventType, rawPayload, ok := decodeCursorRawEventEnvelope(p)
	if !ok {
		return nil, false
	}

	switch eventType {
	case "permission_request":
		return synthesizeCursorRawApprovalPayload(p, channelData, rawPayload)
	case "tool_use", "tool_call", "tool_execution_start":
		return synthesizeCursorRawToolExecutionPayload(p, channelData, rawPayload)
	case "tool_result", "tool_execution_end", "tool_execution_update":
		return synthesizeCursorRawToolResultPayload(p, channelData, rawPayload)
	case "error":
		return synthesizeCursorRawStatusPayload(p, channelData, eventType, rawPayload)
	case "result":
		return nil, false
	case "question", "question_request":
		return synthesizeCursorRawQuestionPayload(p, channelData, rawPayload)
	default:
		return nil, false
	}
}

func decodeCursorRawEventEnvelope(p *agentadapter.InboundSendMsgPayload) (map[string]any, string, any, bool) {
	channelData := util.DecodeJSONObject(p.ChannelData)
	if len(channelData) == 0 {
		return nil, "", nil, false
	}
	cursorEnvelope := util.NestedObject(channelData, "cursor")
	if len(cursorEnvelope) == 0 {
		return nil, "", nil, false
	}
	rawEvent := util.AsJSONObject(cursorEnvelope["raw_event"])
	if len(rawEvent) == 0 {
		rawEvent = util.AsJSONObject(cursorEnvelope["rawEvent"])
	}
	if len(rawEvent) == 0 {
		return nil, "", nil, false
	}
	eventType := strings.ToLower(util.FirstNonEmpty(
		util.NormalizeText(rawEvent["type"]),
		util.NormalizeText(rawEvent["event_type"]),
	))
	if eventType == "" {
		return nil, "", nil, false
	}
	return channelData, eventType, rawEvent["payload"], true
}

func synthesizeCursorRawToolExecutionPayload(
	p *agentadapter.InboundSendMsgPayload,
	channelData map[string]any,
	rawPayload any,
) (*agentadapter.InboundSendMsgPayload, bool) {
	payload := util.AsJSONObject(rawPayload)
	if len(payload) == 0 {
		return nil, false
	}

	summary := util.FirstNonEmpty(
		util.NormalizeText(payload["tool_title"]),
		util.NormalizeText(payload["toolTitle"]),
		util.NormalizeText(payload["title"]),
		util.NormalizeText(payload["tool_name"]),
		util.NormalizeText(payload["toolName"]),
	)
	detail := renderCursorRawInput(payload["tool_input"])
	if detail == "" {
		detail = renderCursorRawInput(payload["toolInput"])
	}
	if detail == "" {
		detail = util.FirstNonEmpty(
			util.NormalizeText(payload["content"]),
			util.NormalizeText(payload["tool_output"]),
			util.NormalizeText(payload["toolOutput"]),
		)
	}
	if summary == "" && detail != "" {
		summary = truncateCursorSingleLine(detail, 120)
	}
	if summary == "" {
		return nil, false
	}

	normalizedChannelData := util.CloneJSONObject(channelData)
	grixNamespace := util.CloneJSONObject(util.NestedObject(normalizedChannelData, "grix"))
	if len(grixNamespace) == 0 {
		grixNamespace = map[string]any{}
	}
	toolExecution := map[string]any{"summary_text": summary}
	if detail != "" {
		toolExecution["detail_text"] = detail
	}
	grixNamespace["toolExecution"] = toolExecution
	normalizedChannelData["grix"] = grixNamespace

	channelDataRaw, _ := json.Marshal(normalizedChannelData)
	result := &agentadapter.InboundSendMsgPayload{
		Content:     p.Content,
		Extra:       p.Extra,
		ChannelData: channelDataRaw,
	}
	agentadapter.MergeCardsIntoExtra(result)
	return result, true
}

func synthesizeCursorRawToolResultPayload(
	p *agentadapter.InboundSendMsgPayload,
	channelData map[string]any,
	rawPayload any,
) (*agentadapter.InboundSendMsgPayload, bool) {
	payload := util.AsJSONObject(rawPayload)
	if len(payload) == 0 {
		return nil, false
	}
	toolName := util.FirstNonEmpty(
		util.NormalizeText(payload["tool_name"]),
		util.NormalizeText(payload["toolName"]),
		util.NormalizeText(payload["tool_title"]),
		util.NormalizeText(payload["toolTitle"]),
	)
	content := util.FirstNonEmpty(
		util.NormalizeText(payload["content"]),
		util.NormalizeText(payload["tool_output"]),
		util.NormalizeText(payload["toolOutput"]),
		util.NormalizeText(payload["output"]),
	)
	if content == "" {
		content = renderCursorRawInput(firstNonNil(
			payload["result"],
			payload["response"],
			payload["data"],
		))
	}
	if content == "" {
		return nil, false
	}
	summary := toolName
	if summary == "" {
		summary = truncateCursorSingleLine(content, 120)
	}

	normalizedChannelData := util.CloneJSONObject(channelData)
	grixNamespace := util.CloneJSONObject(util.NestedObject(normalizedChannelData, "grix"))
	if len(grixNamespace) == 0 {
		grixNamespace = map[string]any{}
	}
	grixNamespace["toolExecution"] = map[string]any{
		"summary_text": summary,
		"detail_text":  content,
	}
	normalizedChannelData["grix"] = grixNamespace

	channelDataRaw, _ := json.Marshal(normalizedChannelData)
	result := &agentadapter.InboundSendMsgPayload{
		Content:     p.Content,
		Extra:       p.Extra,
		ChannelData: channelDataRaw,
	}
	agentadapter.MergeCardsIntoExtra(result)
	return result, true
}

func synthesizeCursorRawApprovalPayload(
	p *agentadapter.InboundSendMsgPayload,
	channelData map[string]any,
	rawPayload any,
) (*agentadapter.InboundSendMsgPayload, bool) {
	payload := util.AsJSONObject(rawPayload)
	if len(payload) == 0 {
		return nil, false
	}

	approvalCommandID := util.FirstNonEmpty(
		util.NormalizeText(payload["tool_call_id"]),
		util.NormalizeText(payload["toolCallId"]),
		util.NormalizeText(payload["approval_command_id"]),
		util.NormalizeText(payload["approvalCommandId"]),
		util.NormalizeText(payload["approval_id"]),
		util.NormalizeText(payload["approvalId"]),
		util.NormalizeText(payload["request_id"]),
		util.NormalizeText(payload["requestId"]),
	)
	if approvalCommandID == "" {
		return nil, false
	}

	approvalID := util.FirstNonEmpty(util.NormalizeText(payload["approval_id"]), util.NormalizeText(payload["approvalId"]))
	if approvalID == "" {
		approvalID = approvalCommandID
	}
	approvalSlug := util.FirstNonEmpty(
		util.NormalizeText(payload["tool_name"]),
		util.NormalizeText(payload["toolName"]),
		util.NormalizeText(payload["tool_title"]),
		util.NormalizeText(payload["toolTitle"]),
	)
	if approvalSlug == "" {
		approvalSlug = approvalID
	}

	commandTitle := util.FirstNonEmpty(
		util.NormalizeText(payload["tool_title"]),
		util.NormalizeText(payload["toolTitle"]),
		util.NormalizeText(payload["tool_name"]),
		util.NormalizeText(payload["toolName"]),
	)
	commandInput := renderCursorRawInput(firstNonNil(
		payload["tool_input"],
		payload["toolInput"],
		payload["raw_input"],
		payload["rawInput"],
		payload["input"],
	))
	command := commandTitle
	switch {
	case commandTitle != "" && commandInput != "" && commandTitle != commandInput:
		command = truncateCursorSingleLine(fmt.Sprintf("%s: %s", commandTitle, commandInput), 240)
	case command == "":
		command = truncateCursorSingleLine(commandInput, 240)
	}
	if command == "" {
		command = "Cursor tool call"
	}

	replyMeta := map[string]any{
		"approvalId":       approvalID,
		"approvalSlug":     approvalSlug,
		"allowedDecisions": normalizeCursorAllowedDecisions(payload["options"]),
	}
	grixExecApproval := map[string]any{
		"approval_command_id": approvalCommandID,
		"command":             command,
		"host":                "Cursor",
		"warning_text":        "Cursor requested approval before continuing this step.",
	}

	normalizedChannelData := util.CloneJSONObject(channelData)
	if len(normalizedChannelData) == 0 {
		normalizedChannelData = map[string]any{}
	}
	normalizedChannelData["execApproval"] = replyMeta
	grixNamespace := util.CloneJSONObject(util.NestedObject(normalizedChannelData, "grix"))
	if len(grixNamespace) == 0 {
		grixNamespace = map[string]any{}
	}
	grixNamespace["execApproval"] = grixExecApproval
	normalizedChannelData["grix"] = grixNamespace

	channelDataRaw, _ := json.Marshal(normalizedChannelData)
	result := &agentadapter.InboundSendMsgPayload{
		Content:     p.Content,
		Extra:       p.Extra,
		ChannelData: channelDataRaw,
	}
	agentadapter.MergeCardsIntoExtra(result)
	return result, true
}

func synthesizeCursorRawStatusPayload(
	p *agentadapter.InboundSendMsgPayload,
	channelData map[string]any,
	eventType string,
	rawPayload any,
) (*agentadapter.InboundSendMsgPayload, bool) {
	payload := util.AsJSONObject(rawPayload)

	statusLevel := "info"
	summary := util.FirstNonEmpty(
		util.NormalizeText(payload["summary"]),
		util.NormalizeText(payload["message"]),
		util.NormalizeText(payload["status"]),
	)
	detailText := util.FirstNonEmpty(
		util.NormalizeText(payload["detail_text"]),
		util.NormalizeText(payload["detail"]),
		util.NormalizeText(payload["error"]),
		util.NormalizeText(payload["message"]),
	)
	switch eventType {
	case "error":
		statusLevel = "error"
		if summary == "" {
			summary = "Cursor run failed."
		}
	}
	if summary == "" {
		return nil, false
	}

	statusPayload := map[string]any{
		"category": "execution",
		"status":   statusLevel,
		"summary":  summary,
	}
	if detailText != "" && detailText != summary {
		statusPayload["detail_text"] = detailText
	}
	if referenceID := util.FirstNonEmpty(
		util.NormalizeText(payload["request_id"]),
		util.NormalizeText(payload["requestId"]),
		util.NormalizeText(payload["tool_call_id"]),
		util.NormalizeText(payload["toolCallId"]),
	); referenceID != "" {
		statusPayload["reference_id"] = referenceID
	}

	normalizedChannelData := util.CloneJSONObject(channelData)
	grixNamespace := util.CloneJSONObject(util.NestedObject(normalizedChannelData, "grix"))
	if len(grixNamespace) == 0 {
		grixNamespace = map[string]any{}
	}
	grixNamespace["agentStatus"] = statusPayload
	normalizedChannelData["grix"] = grixNamespace

	channelDataRaw, _ := json.Marshal(normalizedChannelData)
	result := &agentadapter.InboundSendMsgPayload{
		Content:     p.Content,
		Extra:       p.Extra,
		ChannelData: channelDataRaw,
	}
	agentadapter.MergeCardsIntoExtra(result)
	return result, true
}

func synthesizeCursorRawQuestionPayload(
	p *agentadapter.InboundSendMsgPayload,
	channelData map[string]any,
	rawPayload any,
) (*agentadapter.InboundSendMsgPayload, bool) {
	payload := util.AsJSONObject(rawPayload)
	if len(payload) == 0 {
		return nil, false
	}

	requestID := util.FirstNonEmpty(
		util.NormalizeText(payload["request_id"]),
		util.NormalizeText(payload["requestId"]),
		util.NormalizeText(payload["id"]),
	)
	if requestID == "" {
		return nil, false
	}
	prompt := util.FirstNonEmpty(
		util.NormalizeText(payload["message"]),
		util.NormalizeText(payload["prompt"]),
		util.NormalizeText(payload["question"]),
	)
	if prompt == "" {
		prompt = "Cursor needs your confirmation to continue."
	}

	questionPayload := map[string]any{
		"request_id": requestID,
		"mode":       "form",
		"questions": []map[string]any{
			{
				"index":  1,
				"header": "Cursor",
				"prompt": prompt,
			},
		},
	}
	if options := normalizeCursorStringOptions(payload["options"]); len(options) > 0 {
		questionPayload["questions"] = []map[string]any{
			{
				"index":   1,
				"header":  "Cursor",
				"prompt":  prompt,
				"options": options,
			},
		}
	}

	normalizedChannelData := util.CloneJSONObject(channelData)
	grixNamespace := util.CloneJSONObject(util.NestedObject(normalizedChannelData, "grix"))
	if len(grixNamespace) == 0 {
		grixNamespace = map[string]any{}
	}
	grixNamespace["agentQuestion"] = questionPayload
	normalizedChannelData["grix"] = grixNamespace

	channelDataRaw, _ := json.Marshal(normalizedChannelData)
	result := &agentadapter.InboundSendMsgPayload{
		Content:     p.Content,
		Extra:       p.Extra,
		ChannelData: channelDataRaw,
	}
	agentadapter.MergeCardsIntoExtra(result)
	return result, true
}

func normalizeCursorAllowedDecisions(value any) []string {
	seen := map[string]struct{}{}
	decisions := make([]string, 0, 3)

	rawItems, _ := value.([]any)
	for _, raw := range rawItems {
		var decision string
		switch typed := raw.(type) {
		case map[string]any:
			decision = mapCursorDecision(util.FirstNonEmpty(
				util.NormalizeText(typed["kind"]),
				util.NormalizeText(typed["name"]),
				util.NormalizeText(typed["optionId"]),
				util.NormalizeText(typed["option_id"]),
			))
		case string:
			decision = mapCursorDecision(typed)
		}
		if decision == "" {
			continue
		}
		if _, exists := seen[decision]; exists {
			continue
		}
		seen[decision] = struct{}{}
		decisions = append(decisions, decision)
	}
	if len(decisions) == 0 {
		return []string{"allow-once", "allow-always", "deny"}
	}
	return decisions
}

func mapCursorDecision(kind string) string {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "allow_once", "allow-once", "allow":
		return "allow-once"
	case "allow_always", "allow-always":
		return "allow-always"
	case "reject_once", "reject_always", "reject-once", "reject-always", "reject", "deny":
		return "deny"
	default:
		return ""
	}
}

func normalizeCursorStringOptions(value any) []string {
	rawItems, ok := value.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(rawItems))
	seen := map[string]struct{}{}
	for _, raw := range rawItems {
		option := strings.TrimSpace(fmt.Sprint(raw))
		if option == "" {
			continue
		}
		if _, exists := seen[option]; exists {
			continue
		}
		seen[option] = struct{}{}
		out = append(out, option)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func renderCursorRawInput(value any) string {
	switch v := value.(type) {
	case string:
		return strings.TrimSpace(v)
	case nil:
		return ""
	default:
		b, err := json.Marshal(v)
		if err != nil {
			return strings.TrimSpace(fmt.Sprintf("%v", v))
		}
		return strings.TrimSpace(string(b))
	}
}

func truncateCursorSingleLine(text string, max int) string {
	normalized := strings.Join(strings.Fields(strings.TrimSpace(text)), " ")
	if max <= 0 || len(normalized) <= max {
		return normalized
	}
	if max <= 3 {
		return normalized[:max]
	}
	return normalized[:max-3] + "..."
}

func firstNonNil(values ...any) any {
	for _, value := range values {
		if value != nil {
			return value
		}
	}
	return nil
}

func buildCursorSessionBindingCardContent(channelData map[string]any) (string, bool) {
	record := util.NestedObject(util.NestedObject(channelData, "cursor"), "sessionBinding")
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
		"detail_text":  "先提交一个工作目录。校验通过后，Cursor 会自动继续处理刚才那条消息。",
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

	return buildCursorGrixCardLink(
		"[Open Workspace] 当前对话还没有打开工作目录。",
		"agent_open_session",
		payload,
	), true
}

func buildCursorGrixCardLink(fallbackText, cardType string, payload map[string]any) string {
	return "[" + fallbackText + "](" + buildCursorGrixCardURI(cardType, payload) + ")"
}

func buildCursorGrixCardURI(cardType string, payload map[string]any) string {
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
