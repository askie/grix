package agentcards

import (
	"encoding/json"
	"fmt"
	"net/url"
	"reflect"
	"strings"

	"github.com/askie/grix/backend/internal/agentadapter"
	"github.com/askie/grix/backend/internal/agentadapter/internal/util"
)

// Normalize rewrites generic agent biz_card and channel_data payloads into grix card links.
// It returns ok=false when the payload does not match a supported card shape.
func Normalize(p *agentadapter.InboundSendMsgPayload) (normalizedContent string, normalizedExtra json.RawMessage, ok bool) {
	if strings.Contains(p.Content, "grix://card/") {
		return p.Content, util.CloneRawMessage(p.Extra), true
	}

	// Try channel_data.grix paths first (matches grixcards pattern).
	if len(p.ChannelData) > 0 {
		if cardContent, matched := buildAgentCardContentFromChannelData(p.ChannelData); matched {
			return cardContent, util.CloneRawMessage(p.Extra), true
		}
	}

	bizCard := util.DecodeJSONObject(p.BizCard)
	if len(bizCard) == 0 {
		return "", nil, false
	}

	cardContent, matched := buildAgentCardContent(bizCard)
	if !matched {
		return "", nil, false
	}
	return cardContent, util.CloneRawMessage(p.Extra), true
}

// buildAgentCardContentFromChannelData checks channel_data.grix for agent card signals.
func buildAgentCardContentFromChannelData(channelDataRaw json.RawMessage) (string, bool) {
	channelData := util.DecodeJSONObject(channelDataRaw)
	if len(channelData) == 0 {
		return "", false
	}
	grixData := util.NestedObject(channelData, "grix")
	if len(grixData) == 0 {
		return "", false
	}

	type cardCheck struct {
		key      string
		cardType string
		builder  func(map[string]any) (string, bool)
	}
	checks := []cardCheck{
		{"agentStatus", "agent_status", buildAgentStatusCardContent},
		{"agentQuestion", "agent_question", buildAgentQuestionCardContent},
		{"agentPairing", "agent_pairing", buildAgentPairingCardContent},
		{"agentOpenSession", "agent_open_session", buildAgentOpenSessionCardContent},
		{"agentError", "agent_error", buildAgentErrorCardContent},
	}
	for _, check := range checks {
		payload := util.NestedObject(grixData, check.key)
		if len(payload) == 0 {
			continue
		}
		if cardContent, matched := check.builder(payload); matched {
			return cardContent, true
		}
	}
	return "", false
}

func buildAgentCardContent(bizCard map[string]any) (string, bool) {
	cardType := strings.ToLower(util.NormalizeText(bizCard["type"]))
	payload := util.NestedObject(bizCard, "payload")
	if cardType == "" || len(payload) == 0 {
		return "", false
	}

	switch cardType {
	case "agent_status":
		return buildAgentStatusCardContent(payload)
	case "agent_question":
		return buildAgentQuestionCardContent(payload)
	case "agent_pairing":
		return buildAgentPairingCardContent(payload)
	case "agent_open_session":
		return buildAgentOpenSessionCardContent(payload)
	case "agent_error":
		return buildAgentErrorCardContent(payload)
	default:
		return "", false
	}
}

func buildAgentStatusCardContent(payload map[string]any) (string, bool) {
	category := util.NormalizeText(payload["category"])
	status := util.NormalizeText(payload["status"])
	summary := util.NormalizeText(payload["summary"])
	if category == "" || status == "" || summary == "" {
		return "", false
	}

	normalizedPayload := map[string]any{
		"category": category,
		"status":   status,
		"summary":  summary,
	}
	if detailText := util.NormalizeText(payload["detail_text"]); detailText != "" {
		normalizedPayload["detail_text"] = detailText
	}
	if referenceID := util.NormalizeText(payload["reference_id"]); referenceID != "" {
		normalizedPayload["reference_id"] = referenceID
	}
	if cardInstanceID := util.NormalizeText(payload["card_instance_id"]); cardInstanceID != "" {
		normalizedPayload["card_instance_id"] = cardInstanceID
	}

	return buildGrixCardLink(
		fmt.Sprintf("[Agent Status] %s", compactText(summary, 180)),
		"agent_status",
		normalizedPayload,
	), true
}

func buildAgentQuestionCardContent(payload map[string]any) (string, bool) {
	requestID := util.NormalizeText(payload["request_id"])
	if requestID == "" {
		return "", false
	}

	mode := strings.ToLower(util.NormalizeText(payload["mode"]))
	if mode == "" {
		mode = "form"
	}

	normalizedPayload := map[string]any{
		"request_id": requestID,
		"mode":       mode,
	}
	if message := util.NormalizeText(payload["message"]); message != "" {
		normalizedPayload["message"] = message
	}
	if footerText := util.NormalizeText(payload["footer_text"]); footerText != "" {
		normalizedPayload["footer_text"] = footerText
	}
	if submittedAnswer := util.NormalizeText(payload["submitted_answer"]); submittedAnswer != "" {
		normalizedPayload["submitted_answer"] = submittedAnswer
	}
	if submittedAcceptText := util.NormalizeText(payload["submitted_accept_text"]); submittedAcceptText != "" {
		normalizedPayload["submitted_accept_text"] = submittedAcceptText
	}
	if submittedCancelText := util.NormalizeText(payload["submitted_cancel_text"]); submittedCancelText != "" {
		normalizedPayload["submitted_cancel_text"] = submittedCancelText
	}

	if mode == "url" {
		urlValue := util.NormalizeText(payload["url"])
		if urlValue == "" {
			return "", false
		}
		normalizedPayload["url"] = urlValue
		if openURLLabel := util.NormalizeText(payload["open_url_label"]); openURLLabel != "" {
			normalizedPayload["open_url_label"] = openURLLabel
		}
		normalizedPayload["questions"] = []map[string]any{}
	} else {
		questions := normalizeAgentQuestionPayloads(payload["questions"])
		if len(questions) == 0 {
			return "", false
		}
		normalizedPayload["questions"] = questions
	}

	return buildGrixCardLink(
		fmt.Sprintf("[Agent Question] %s", compactText(requestID, 120)),
		"agent_question",
		normalizedPayload,
	), true
}

func buildAgentPairingCardContent(payload map[string]any) (string, bool) {
	pairingCode := util.NormalizeText(payload["pairing_code"])
	if pairingCode == "" {
		return "", false
	}

	normalizedPayload := map[string]any{
		"pairing_code": pairingCode,
	}
	if instructionText := util.NormalizeText(payload["instruction_text"]); instructionText != "" {
		normalizedPayload["instruction_text"] = instructionText
	}
	if commandHint := util.NormalizeText(payload["command_hint"]); commandHint != "" {
		normalizedPayload["command_hint"] = commandHint
	}

	return buildGrixCardLink(
		fmt.Sprintf("[Agent Pairing] %s", compactText(pairingCode, 80)),
		"agent_pairing",
		normalizedPayload,
	), true
}

func buildAgentOpenSessionCardContent(payload map[string]any) (string, bool) {
	summaryText := util.NormalizeText(payload["summary_text"])
	detailText := util.NormalizeText(payload["detail_text"])
	initialCwd := util.NormalizeText(payload["initial_cwd"])
	submittedPath := util.NormalizeText(payload["submitted_path"])
	if summaryText == "" &&
		detailText == "" &&
		initialCwd == "" &&
		submittedPath == "" {
		return "", false
	}

	normalizedPayload := map[string]any{}
	if cardInstanceID := util.NormalizeText(payload["card_instance_id"]); cardInstanceID != "" {
		normalizedPayload["card_instance_id"] = cardInstanceID
	}
	if summaryText != "" {
		normalizedPayload["summary_text"] = summaryText
	}
	if detailText != "" {
		normalizedPayload["detail_text"] = detailText
	}
	if initialCwd != "" {
		normalizedPayload["initial_cwd"] = initialCwd
	}
	if submittedPath != "" {
		normalizedPayload["submitted_path"] = submittedPath
	}

	label := summaryText
	if label == "" {
		label = detailText
	}
	if label == "" {
		label = initialCwd
	}
	if label == "" {
		label = submittedPath
	}
	return buildGrixCardLink(
		fmt.Sprintf("[Open Workspace] %s", compactText(label, 160)),
		"agent_open_session",
		normalizedPayload,
	), true
}

func normalizeAgentQuestionPayloads(value any) []map[string]any {
	rawQuestions, ok := value.([]any)
	if !ok {
		return nil
	}
	normalized := make([]map[string]any, 0, len(rawQuestions))
	for index, rawQuestion := range rawQuestions {
		question := util.AsJSONObject(rawQuestion)
		if len(question) == 0 {
			continue
		}
		header := util.NormalizeText(question["header"])
		prompt := util.NormalizeText(question["prompt"])
		if header == "" || prompt == "" {
			continue
		}
		normalizedQuestion := map[string]any{
			"index":  normalizeQuestionIndex(question["index"], index+1),
			"header": header,
			"prompt": prompt,
		}
		if options := normalizeStringSlice(question["options"]); len(options) > 0 {
			normalizedQuestion["options"] = options
		}
		if question["multi_select"] == true {
			normalizedQuestion["multi_select"] = true
		}
		normalized = append(normalized, normalizedQuestion)
	}
	if len(normalized) == 0 {
		return nil
	}
	return normalized
}

func normalizeQuestionIndex(value any, fallback int) int {
	if parsed, ok := normalizeInt(value); ok && parsed > 0 {
		return parsed
	}
	return fallback
}

func buildGrixCardLink(fallbackText, cardType string, payload map[string]any) string {
	return "[" + fallbackText + "](" + buildGrixCardURI(cardType, payload) + ")"
}

func buildGrixCardURI(cardType string, payload map[string]any) string {
	values := url.Values{}
	if hasComplexPayload(payload) {
		data, _ := json.Marshal(payload)
		values.Set("d", string(data))
	} else {
		for key, value := range payload {
			switch typed := value.(type) {
			case nil:
				continue
			case string:
				if typed == "" {
					continue
				}
				values.Set(key, typed)
			default:
				values.Set(key, fmt.Sprint(value))
			}
		}
	}

	return (&url.URL{
		Scheme:   "grix",
		Host:     "card",
		Path:     "/" + strings.TrimSpace(cardType),
		RawQuery: values.Encode(),
	}).String()
}

func hasComplexPayload(payload map[string]any) bool {
	for _, value := range payload {
		if value == nil {
			continue
		}
		switch reflect.ValueOf(value).Kind() {
		case reflect.Map, reflect.Slice, reflect.Array:
			return true
		}
	}
	return false
}

func compactText(text string, limit int) string {
	compact := strings.Join(strings.Fields(util.NormalizeText(text)), " ")
	if limit <= 3 || len(compact) <= limit {
		return compact
	}
	return compact[:limit-3] + "..."
}

func normalizeStringSlice(value any) []string {
	switch typed := value.(type) {
	case []string:
		normalized := make([]string, 0, len(typed))
		for _, item := range typed {
			if normalizedItem := util.NormalizeText(item); normalizedItem != "" {
				normalized = append(normalized, normalizedItem)
			}
		}
		return normalized
	case []any:
		normalized := make([]string, 0, len(typed))
		for _, item := range typed {
			if normalizedItem := util.NormalizeText(item); normalizedItem != "" {
				normalized = append(normalized, normalizedItem)
			}
		}
		return normalized
	default:
		return nil
	}
}

func normalizeInt(value any) (int, bool) {
	switch typed := value.(type) {
	case float64:
		return int(typed), typed == float64(int(typed))
	case float32:
		return int(typed), typed == float32(int(typed))
	case int:
		return typed, true
	case int64:
		return int(typed), true
	case json.Number:
		parsed, err := typed.Int64()
		return int(parsed), err == nil
	case string:
		if strings.TrimSpace(typed) == "" {
			return 0, false
		}
		number := json.Number(strings.TrimSpace(typed))
		parsed, err := number.Int64()
		return int(parsed), err == nil
	default:
		return 0, false
	}
}

func buildAgentErrorCardContent(payload map[string]any) (string, bool) {
	errorObj := util.NestedObject(payload, "error")
	if len(errorObj) == 0 {
		return "", false
	}

	summary := util.NormalizeText(errorObj["message"])
	if summary == "" {
		summary = "An unexpected error occurred."
	}

	normalizedPayload := map[string]any{
		"summary": summary,
	}
	if code := util.NormalizeText(errorObj["code"]); code != "" {
		normalizedPayload["code"] = code
	}
	if name := util.NormalizeText(errorObj["name"]); name != "" {
		normalizedPayload["name"] = name
	}

	return buildGrixCardLink(
		fmt.Sprintf("[Error] %s", compactText(summary, 180)),
		"agent_error",
		normalizedPayload,
	), true
}
