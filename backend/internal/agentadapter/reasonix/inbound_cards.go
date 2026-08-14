package reasonix

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

func normalizeReasonixStructuredInbound(p *agentadapter.InboundSendMsgPayload) (string, json.RawMessage, bool) {
	if normalizedPayload, ok := synthesizeReasonixRawEventPayload(p); ok {
		if normalizedContent, normalizedExtra, ok := approvalcards.Normalize(normalizedPayload); ok {
			return normalizedContent, normalizedExtra, true
		}
		if normalizedContent, normalizedExtra, ok := grixcards.Normalize(normalizedPayload); ok {
			return normalizedContent, normalizedExtra, true
		}
		if normalizedContent, normalizedExtra, ok := agentcards.Normalize(normalizedPayload); ok {
			return normalizedContent, normalizedExtra, true
		}
		return normalizedPayload.Content, util.CloneRawMessage(normalizedPayload.Extra), true
	}
	if rawType, hasRaw := detectReasonixRawEventType(p); hasRaw {
		if logger.L != nil {
			logger.L.Warnf("reasonix adapter raw_event fallback to plain text: session=%s type=%s", strings.TrimSpace(p.SessionID), rawType)
		}
	}
	return "", nil, false
}

func synthesizeReasonixRawEventPayload(p *agentadapter.InboundSendMsgPayload) (*agentadapter.InboundSendMsgPayload, bool) {
	channelData, eventType, rawPayload, ok := decodeReasonixRawEventEnvelope(p)
	if !ok {
		return nil, false
	}

	switch eventType {
	case "permission_request":
		return synthesizeReasonixRawApprovalPayload(p, channelData, rawPayload)
	case "tool_use":
		return synthesizeReasonixRawToolExecutionPayload(p, channelData, rawPayload)
	case "tool_result":
		return synthesizeReasonixRawToolResultPayload(p, channelData, rawPayload)
	case "error":
		return synthesizeReasonixRawStatusPayload(p, channelData, eventType, rawPayload)
	case "result":
		return nil, false
	case "question", "question_request":
		return synthesizeReasonixRawQuestionPayload(p, channelData, rawPayload)
	default:
		return nil, false
	}
}

func detectReasonixRawEventType(p *agentadapter.InboundSendMsgPayload) (string, bool) {
	_, eventType, _, ok := decodeReasonixRawEventEnvelope(p)
	if !ok {
		return "", false
	}
	return eventType, true
}

func decodeReasonixRawEventEnvelope(p *agentadapter.InboundSendMsgPayload) (map[string]any, string, any, bool) {
	channelData := util.DecodeJSONObject(p.ChannelData)
	if len(channelData) == 0 {
		return nil, "", nil, false
	}
	acpEnvelope := util.NestedObject(channelData, "acp")
	if len(acpEnvelope) == 0 {
		return nil, "", nil, false
	}
	rawEvent := util.AsJSONObject(acpEnvelope["raw_event"])
	if len(rawEvent) == 0 {
		rawEvent = util.AsJSONObject(acpEnvelope["rawEvent"])
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

func synthesizeReasonixRawApprovalPayload(
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
	commandInput := renderReasonixRawInput(firstNonNil(
		payload["tool_input"],
		payload["toolInput"],
		payload["raw_input"],
		payload["rawInput"],
		payload["input"],
	))
	command := commandTitle
	switch {
	case commandTitle != "" && commandInput != "" && commandTitle != commandInput:
		command = truncateSingleLine(fmt.Sprintf("%s: %s", commandTitle, commandInput), 240)
	case command == "":
		command = truncateSingleLine(commandInput, 240)
	}
	if command == "" {
		command = "Reasonix tool call"
	}

	replyMeta := map[string]any{
		"approvalId":       approvalID,
		"approvalSlug":     approvalSlug,
		"allowedDecisions": normalizeReasonixAllowedDecisions(payload["options"]),
	}
	grixExecApproval := map[string]any{
		"approval_command_id": approvalCommandID,
		"command":             command,
		"host":                "Reasonix ACP",
		"warning_text":        "Reasonix requested approval before continuing this step.",
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

func synthesizeReasonixRawToolExecutionPayload(
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
		util.NormalizeText(payload["kind"]),
	)
	detail := renderReasonixRawInput(firstNonNil(
		payload["tool_input"],
		payload["toolInput"],
		payload["raw_input"],
		payload["rawInput"],
		payload["input"],
	))
	if detail == "" {
		detail = util.FirstNonEmpty(
			util.NormalizeText(payload["content"]),
			util.NormalizeText(payload["tool_output"]),
			util.NormalizeText(payload["toolOutput"]),
		)
	}
	if summary == "" && detail != "" {
		summary = truncateSingleLine(detail, 120)
	}
	if summary == "" {
		return nil, false
	}

	normalizedChannelData := util.CloneJSONObject(channelData)
	if len(normalizedChannelData) == 0 {
		normalizedChannelData = map[string]any{}
	}
	grixNamespace := util.CloneJSONObject(util.NestedObject(normalizedChannelData, "grix"))
	if len(grixNamespace) == 0 {
		grixNamespace = map[string]any{}
	}
	toolExecution := map[string]any{
		"summary_text": summary,
	}
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

func synthesizeReasonixRawToolResultPayload(
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
		return nil, false
	}

	summary := toolName
	if summary == "" {
		summary = truncateSingleLine(content, 120)
	}

	normalizedChannelData := util.CloneJSONObject(channelData)
	grixNamespace := util.CloneJSONObject(util.NestedObject(normalizedChannelData, "grix"))
	if len(grixNamespace) == 0 {
		grixNamespace = map[string]any{}
	}
	toolExecution := map[string]any{
		"summary_text": summary,
		"detail_text":  content,
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

func synthesizeReasonixRawStatusPayload(
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
			summary = "Reasonix run failed."
		}
	case "result":
		statusLevel = "success"
		if summary == "" {
			summary = "Reasonix run completed."
		}
		statusText := strings.ToLower(util.FirstNonEmpty(util.NormalizeText(payload["status"])))
		if strings.Contains(statusText, "cancel") {
			statusLevel = "warning"
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

func synthesizeReasonixRawQuestionPayload(
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
		prompt = "Reasonix needs your confirmation to continue."
	}

	questionPayload := map[string]any{
		"request_id": requestID,
		"mode":       "form",
		"questions": []map[string]any{
			{
				"index":  1,
				"header": "Reasonix",
				"prompt": prompt,
			},
		},
	}
	if options := normalizeStringOptions(payload["options"]); len(options) > 0 {
		questionPayload["questions"] = []map[string]any{
			{
				"index":   1,
				"header":  "Reasonix",
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

func normalizeReasonixAllowedDecisions(value any) []string {
	seen := map[string]struct{}{}
	decisions := make([]string, 0, 3)

	rawItems, _ := value.([]any)
	for _, raw := range rawItems {
		var decision string
		switch typed := raw.(type) {
		case map[string]any:
			decision = mapReasonixDecision(util.FirstNonEmpty(
				util.NormalizeText(typed["kind"]),
				util.NormalizeText(typed["name"]),
				util.NormalizeText(typed["optionId"]),
				util.NormalizeText(typed["option_id"]),
			))
		case string:
			decision = mapReasonixDecision(typed)
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

func mapReasonixDecision(kind string) string {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "allow_once", "allow-once":
		return "allow-once"
	case "allow_always", "allow-always":
		return "allow-always"
	case "allow":
		return "allow-once"
	case "reject_once", "reject_always", "reject-once", "reject-always", "reject", "deny":
		return "deny"
	default:
		return ""
	}
}

func renderReasonixRawInput(rawInput any) string {
	switch value := rawInput.(type) {
	case string:
		return strings.TrimSpace(value)
	case map[string]any:
		if command := util.NormalizeText(value["command"]); command != "" {
			return command
		}
		encoded, err := json.Marshal(value)
		if err != nil {
			return ""
		}
		return strings.TrimSpace(string(encoded))
	default:
		return ""
	}
}

func firstNonNil(values ...any) any {
	for _, value := range values {
		if value != nil {
			return value
		}
	}
	return nil
}

func normalizeStringOptions(value any) []string {
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

func truncateSingleLine(value string, limit int) string {
	compact := strings.Join(strings.Fields(value), " ")
	if len(compact) <= limit {
		return compact
	}
	return strings.TrimSpace(compact[:maxLen(limit-3, 0)]) + "..."
}

func maxLen(left, right int) int {
	if left > right {
		return left
	}
	return right
}

func buildReasonixSessionBindingCardContent(channelData map[string]any) (string, bool) {
	record := util.NestedObject(util.NestedObject(channelData, "acp"), "sessionBinding")
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
		"detail_text":  "先提交一个工作目录。校验通过后，Reasonix 会自动继续处理刚才那条消息。",
	}
	if initialCwd := util.FirstNonEmpty(
		util.NormalizeText(record["initial_cwd"]),
		util.NormalizeText(record["initialCwd"]),
	); initialCwd != "" {
		payload["initial_cwd"] = initialCwd
	}

	return buildReasonixCardLink(
		"[Open Workspace] 当前对话还没有打开工作目录。",
		"agent_open_session",
		payload,
	), true
}

func buildReasonixCardLink(fallbackText, cardType string, payload map[string]any) string {
	return "[" + fallbackText + "](" + buildReasonixCardURI(cardType, payload) + ")"
}

func buildReasonixCardURI(cardType string, payload map[string]any) string {
	u := url.URL{Scheme: "grix", Host: "card", Path: cardType}
	q := u.Query()
	for k, v := range payload {
		q.Set(k, fmt.Sprintf("%v", v))
	}
	u.RawQuery = q.Encode()
	return u.String()
}
