package approvalcards

import (
	"encoding/json"
	"fmt"
	"hash/fnv"
	"net/url"
	"reflect"
	"strings"
	"sync/atomic"
	"time"

	"github.com/askie/grix/backend/internal/agentadapter"
	"github.com/askie/grix/backend/internal/agentadapter/internal/util"
)

var approvalIDCounter atomic.Uint64

// NormalizeBizCard rewrites approval biz_card payloads into grix card links.
// It returns ok=false when the payload does not contain a supported biz_card.
func NormalizeBizCard(p *agentadapter.InboundSendMsgPayload) (normalizedContent string, normalizedExtra json.RawMessage, ok bool) {
	if strings.Contains(p.Content, "grix://card/") {
		return p.Content, util.CloneRawMessage(p.Extra), true
	}

	bizCard := util.DecodeJSONObject(p.BizCard)
	if len(bizCard) == 0 {
		return "", nil, false
	}

	cardContent, matched := buildApprovalCardContentFromBizCard(bizCard)
	if !matched {
		return "", nil, false
	}
	return cardContent, util.CloneRawMessage(p.Extra), true
}

// Normalize rewrites approval-related inbound content into grix card links.
// It returns ok=false when the payload does not represent an approval card.
func Normalize(p *agentadapter.InboundSendMsgPayload) (normalizedContent string, normalizedExtra json.RawMessage, ok bool) {
	if strings.Contains(p.Content, "grix://card/") {
		return p.Content, util.CloneRawMessage(p.Extra), true
	}

	if len(p.ChannelData) > 0 || len(p.BizCard) > 0 {
		if cardContent, matched := buildStructuredApprovalCardContent(p.ChannelData, p.BizCard); matched {
			return cardContent, util.CloneRawMessage(p.Extra), true
		}
	}

	if channelData, matched := buildPlainTextExecApprovalChannelData(p.Content); matched {
		if cardContent, matched := buildExecApprovalCardContent(channelData); matched {
			return cardContent, mergeInboundChannelData(p.Extra, channelData), true
		}
	}
	if channelData, matched := buildPlainTextExecStatusChannelData(p.Content); matched {
		if cardContent, matched := buildExecStatusCardContent(channelData); matched {
			return cardContent, mergeInboundChannelData(p.Extra, channelData), true
		}
	}

	return "", nil, false
}

func buildStructuredApprovalCardContent(channelDataRaw, bizCardRaw json.RawMessage) (string, bool) {
	channelData := util.DecodeJSONObject(channelDataRaw)
	if len(channelData) > 0 {
		if cardContent, matched := buildExecApprovalCardContent(channelData); matched {
			return cardContent, true
		}
		if cardContent, matched := buildExecStatusCardContent(channelData); matched {
			return cardContent, true
		}
	}

	bizCard := util.DecodeJSONObject(bizCardRaw)
	if len(bizCard) == 0 {
		return "", false
	}
	return buildApprovalCardContentFromBizCard(bizCard)
}

func buildApprovalCardContentFromBizCard(bizCard map[string]any) (string, bool) {
	cardType := util.NormalizeText(bizCard["type"])
	payload := util.NestedObject(bizCard, "payload")
	if cardType == "" || len(payload) == 0 {
		return "", false
	}

	switch cardType {
	case "exec_approval":
		return buildExecApprovalCardContentFromPayload(payload)
	case "exec_status":
		return buildExecStatusCardContentFromPayload(payload)
	default:
		return "", false
	}
}

func buildExecApprovalCardContent(channelData map[string]any) (string, bool) {
	replyMeta := util.NestedObject(channelData, "execApproval")
	structured := util.NestedObject(util.NestedObject(channelData, "grix"), "execApproval")
	if len(replyMeta) == 0 || len(structured) == 0 {
		return "", false
	}

	approvalID := util.NormalizeText(replyMeta["approvalId"])
	approvalSlug := util.NormalizeText(replyMeta["approvalSlug"])
	approvalCommandID := util.NormalizeText(structured["approval_command_id"])
	command := util.NormalizeText(structured["command"])
	host := util.NormalizeText(structured["host"])
	if approvalID == "" || approvalSlug == "" || approvalCommandID == "" || command == "" {
		return "", false
	}

	allowedDecisions := normalizeApprovalDecisions(replyMeta["allowedDecisions"])
	if len(allowedDecisions) == 0 {
		allowedDecisions = []string{"allow-once", "allow-always", "deny"}
	}

	payload := map[string]any{
		"approval_id":         approvalID,
		"approval_slug":       approvalSlug,
		"approval_command_id": approvalCommandID,
		"command":             command,
		"host":                host,
		"allowed_decisions":   allowedDecisions,
	}
	if nodeID := util.NormalizeText(structured["node_id"]); nodeID != "" {
		payload["node_id"] = nodeID
	}
	if cwd := util.NormalizeText(structured["cwd"]); cwd != "" {
		payload["cwd"] = cwd
	}
	if warningText := util.NormalizeText(structured["warning_text"]); warningText != "" {
		payload["warning_text"] = warningText
	}
	if expiresInSeconds, ok := normalizeNonNegativeInt(structured["expires_in_seconds"]); ok {
		payload["expires_in_seconds"] = expiresInSeconds
	}
	if expiresAtMs, ok := normalizePositiveInt(structured["expires_at_ms"]); ok {
		payload["expires_at_ms"] = expiresAtMs
	}

	return buildExecApprovalCardContentFromPayload(payload)
}

func buildExecApprovalCardContentFromPayload(payload map[string]any) (string, bool) {
	approvalID := util.NormalizeText(payload["approval_id"])
	approvalSlug := util.NormalizeText(payload["approval_slug"])
	approvalCommandID := util.NormalizeText(payload["approval_command_id"])
	command := util.NormalizeText(payload["command"])
	host := util.NormalizeText(payload["host"])
	if approvalID == "" || approvalSlug == "" || approvalCommandID == "" || command == "" {
		return "", false
	}

	normalizedPayload := map[string]any{
		"approval_id":         approvalID,
		"approval_slug":       approvalSlug,
		"approval_command_id": approvalCommandID,
		"command":             command,
		"host":                host,
	}
	if nodeID := util.NormalizeText(payload["node_id"]); nodeID != "" {
		normalizedPayload["node_id"] = nodeID
	}
	if cwd := util.NormalizeText(payload["cwd"]); cwd != "" {
		normalizedPayload["cwd"] = cwd
	}
	if warningText := util.NormalizeText(payload["warning_text"]); warningText != "" {
		normalizedPayload["warning_text"] = warningText
	}
	if expiresInSeconds, ok := normalizeNonNegativeInt(payload["expires_in_seconds"]); ok {
		normalizedPayload["expires_in_seconds"] = expiresInSeconds
	}
	if expiresAtMs, ok := normalizePositiveInt(payload["expires_at_ms"]); ok {
		normalizedPayload["expires_at_ms"] = expiresAtMs
	}
	if allowedDecisions := normalizeApprovalDecisions(payload["allowed_decisions"]); len(allowedDecisions) > 0 {
		normalizedPayload["allowed_decisions"] = allowedDecisions
	}
	if decisionCommands := normalizeStringMap(payload["decision_commands"]); len(decisionCommands) > 0 {
		normalizedPayload["decision_commands"] = decisionCommands
	}

	fallbackText := fmt.Sprintf("[Exec Approval] %s", compactText(command, 160))
	if host != "" {
		fallbackText = fmt.Sprintf("%s (%s)", fallbackText, host)
	}
	fallbackText = fmt.Sprintf("%s\n/approve %s allow-once", fallbackText, approvalCommandID)

	return buildGrixCardLink(fallbackText, "exec_approval", normalizedPayload), true
}

func buildExecStatusCardContent(channelData map[string]any) (string, bool) {
	record := util.NestedObject(util.NestedObject(channelData, "grix"), "execStatus")
	if len(record) == 0 {
		return "", false
	}

	status := util.NormalizeText(record["status"])
	summary := util.NormalizeText(record["summary"])
	if !isOneOf(status,
		"approval-expired",
		"approval-forwarded",
		"approval-unavailable",
		"resolved-allow-once",
		"resolved-allow-always",
		"resolved-allow-rule",
		"resolved-deny",
		"running",
		"finished",
		"denied",
	) || summary == "" {
		return "", false
	}

	payload := map[string]any{
		"status":  status,
		"summary": summary,
	}
	if detailText := util.NormalizeText(record["detail_text"]); detailText != "" {
		payload["detail_text"] = detailText
	}
	if approvalID := util.NormalizeText(record["approval_id"]); approvalID != "" {
		payload["approval_id"] = approvalID
	}
	if approvalCommandID := util.NormalizeText(record["approval_command_id"]); approvalCommandID != "" {
		payload["approval_command_id"] = approvalCommandID
	}
	if host := util.NormalizeText(record["host"]); host != "" {
		payload["host"] = host
	}
	if nodeID := util.NormalizeText(record["node_id"]); nodeID != "" {
		payload["node_id"] = nodeID
	}
	if sessionID := util.NormalizeText(record["session_id"]); sessionID != "" {
		payload["session_id"] = sessionID
	}
	if reason := util.NormalizeText(record["reason"]); reason != "" {
		payload["reason"] = reason
	}
	if decision := util.NormalizeText(record["decision"]); isOneOf(decision, "allow-once", "allow-always", "allow-rule", "deny") {
		payload["decision"] = decision
	}
	if resolvedByID := util.NormalizeText(record["resolved_by_id"]); resolvedByID != "" {
		payload["resolved_by_id"] = resolvedByID
	}
	if command := util.NormalizeText(record["command"]); command != "" {
		payload["command"] = command
	}
	if exitLabel := util.NormalizeText(record["exit_label"]); exitLabel != "" {
		payload["exit_label"] = exitLabel
	}
	if channelLabel := util.NormalizeText(record["channel_label"]); channelLabel != "" {
		payload["channel_label"] = channelLabel
	}
	if warningText := util.NormalizeText(record["warning_text"]); warningText != "" {
		payload["warning_text"] = warningText
	}

	return buildExecStatusCardContentFromPayload(payload)
}

func buildExecStatusCardContentFromPayload(payload map[string]any) (string, bool) {
	status := util.NormalizeText(payload["status"])
	summary := util.NormalizeText(payload["summary"])
	if !isOneOf(status,
		"approval-expired",
		"approval-forwarded",
		"approval-unavailable",
		"resolved-allow-once",
		"resolved-allow-always",
		"resolved-allow-rule",
		"resolved-deny",
		"running",
		"finished",
		"denied",
	) || summary == "" {
		return "", false
	}

	normalizedPayload := map[string]any{
		"status":  status,
		"summary": summary,
	}
	if detailText := util.NormalizeText(payload["detail_text"]); detailText != "" {
		normalizedPayload["detail_text"] = detailText
	}
	if approvalID := util.NormalizeText(payload["approval_id"]); approvalID != "" {
		normalizedPayload["approval_id"] = approvalID
	}
	if approvalCommandID := util.NormalizeText(payload["approval_command_id"]); approvalCommandID != "" {
		normalizedPayload["approval_command_id"] = approvalCommandID
	}
	if host := util.NormalizeText(payload["host"]); host != "" {
		normalizedPayload["host"] = host
	}
	if nodeID := util.NormalizeText(payload["node_id"]); nodeID != "" {
		normalizedPayload["node_id"] = nodeID
	}
	if sessionID := util.NormalizeText(payload["session_id"]); sessionID != "" {
		normalizedPayload["session_id"] = sessionID
	}
	if reason := util.NormalizeText(payload["reason"]); reason != "" {
		normalizedPayload["reason"] = reason
	}
	if decision := util.NormalizeText(payload["decision"]); isOneOf(decision, "allow-once", "allow-always", "allow-rule", "deny") {
		normalizedPayload["decision"] = decision
	}
	if resolvedByID := util.NormalizeText(payload["resolved_by_id"]); resolvedByID != "" {
		normalizedPayload["resolved_by_id"] = resolvedByID
	}
	if command := util.NormalizeText(payload["command"]); command != "" {
		normalizedPayload["command"] = command
	}
	if exitLabel := util.NormalizeText(payload["exit_label"]); exitLabel != "" {
		normalizedPayload["exit_label"] = exitLabel
	}
	if channelLabel := util.NormalizeText(payload["channel_label"]); channelLabel != "" {
		normalizedPayload["channel_label"] = channelLabel
	}
	if warningText := util.NormalizeText(payload["warning_text"]); warningText != "" {
		normalizedPayload["warning_text"] = warningText
	}

	return buildGrixCardLink(
		fmt.Sprintf("[Exec Status] %s", compactText(summary, 180)),
		"exec_status",
		normalizedPayload,
	), true
}

func buildPlainTextExecApprovalChannelData(content string) (map[string]any, bool) {
	normalized := strings.TrimSpace(strings.ReplaceAll(content, "\r\n", "\n"))
	lines := strings.Split(normalized, "\n")
	if len(lines) < 3 || strings.TrimSpace(lines[0]) != "🔒 Exec approval required" {
		return nil, false
	}

	index := 1
	approvalID, ok := consumeLabeledLine(lines, &index, "ID:")
	if !ok || approvalID == "" {
		return nil, false
	}

	command, ok := consumeExecApprovalCommand(lines, &index)
	if !ok || command == "" {
		return nil, false
	}

	structured := map[string]any{
		"approval_command_id": approvalID,
		"command":             command,
	}
	for index < len(lines) {
		line := strings.TrimSpace(lines[index])
		index++
		switch {
		case line == "":
			continue
		case strings.HasPrefix(line, "CWD:"):
			if value := strings.TrimSpace(strings.TrimPrefix(line, "CWD:")); value != "" {
				structured["cwd"] = value
			}
		case strings.HasPrefix(line, "Node:"):
			if value := strings.TrimSpace(strings.TrimPrefix(line, "Node:")); value != "" {
				structured["node_id"] = value
			}
		case strings.HasPrefix(line, "Host:"):
			if value := strings.TrimSpace(strings.TrimPrefix(line, "Host:")); value != "" {
				structured["host"] = value
			}
		case strings.HasPrefix(line, "Expires in:"):
			if seconds, ok := parseApprovalExpiresInSeconds(strings.TrimSpace(strings.TrimPrefix(line, "Expires in:"))); ok {
				structured["expires_in_seconds"] = seconds
			}
		}
	}

	return map[string]any{
		"execApproval": map[string]any{
			"approvalId":       approvalID,
			"approvalSlug":     defaultApprovalSlug(approvalID),
			"allowedDecisions": []string{"allow-once", "allow-always", "deny"},
		},
		"grix": map[string]any{
			"execApproval": structured,
		},
	}, true
}

func buildPlainTextExecStatusChannelData(content string) (map[string]any, bool) {
	normalized := strings.TrimSpace(strings.ReplaceAll(content, "\r\n", "\n"))
	if record, ok := parseResolvedExecApprovalStatus(normalized); ok {
		return map[string]any{
			"grix": map[string]any{
				"execStatus": record,
			},
		}, true
	}
	if record, ok := parseExpiredExecApprovalStatus(normalized); ok {
		return map[string]any{
			"grix": map[string]any{
				"execStatus": record,
			},
		}, true
	}
	return nil, false
}

func consumeLabeledLine(lines []string, index *int, label string) (string, bool) {
	if index == nil || *index < 0 || *index >= len(lines) {
		return "", false
	}
	line := strings.TrimSpace(lines[*index])
	if !strings.HasPrefix(line, label) {
		return "", false
	}
	*index = *index + 1
	return strings.TrimSpace(strings.TrimPrefix(line, label)), true
}

func consumeExecApprovalCommand(lines []string, index *int) (string, bool) {
	if index == nil || *index < 0 || *index >= len(lines) {
		return "", false
	}

	line := strings.TrimSpace(lines[*index])
	if strings.HasPrefix(line, "Command: ") {
		*index = *index + 1
		return normalizeInlineExecApprovalCommand(strings.TrimSpace(strings.TrimPrefix(line, "Command: "))), true
	}
	if line != "Command:" {
		return "", false
	}

	*index = *index + 1
	if *index >= len(lines) {
		return "", false
	}
	fence := strings.TrimSpace(lines[*index])
	if !strings.HasPrefix(fence, "```") {
		return "", false
	}

	*index = *index + 1
	commandLines := make([]string, 0)
	for *index < len(lines) {
		line = lines[*index]
		*index = *index + 1
		if strings.TrimSpace(line) == fence {
			return strings.TrimSpace(strings.Join(commandLines, "\n")), true
		}
		commandLines = append(commandLines, line)
	}
	return "", false
}

func normalizeInlineExecApprovalCommand(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if strings.HasPrefix(trimmed, "`") && strings.HasSuffix(trimmed, "`") && len(trimmed) >= 2 {
		trimmed = strings.TrimPrefix(trimmed, "`")
		trimmed = strings.TrimSuffix(trimmed, "`")
	}
	return strings.TrimSpace(trimmed)
}

func parseApprovalExpiresInSeconds(raw string) (int, bool) {
	trimmed := strings.TrimSpace(raw)
	if !strings.HasSuffix(trimmed, "s") {
		return 0, false
	}
	return normalizeNonNegativeInt(strings.TrimSuffix(trimmed, "s"))
}

func parseResolvedExecApprovalStatus(content string) (map[string]any, bool) {
	const prefix = "✅ Exec approval "
	if !strings.HasPrefix(content, prefix) {
		return nil, false
	}

	head, approvalID, ok := strings.Cut(content, " ID: ")
	if !ok {
		return nil, false
	}
	approvalID = strings.TrimSpace(approvalID)
	if approvalID == "" {
		return nil, false
	}

	message := strings.TrimSpace(strings.TrimPrefix(head, prefix))
	resolvedBy := ""
	if summaryText, actor, found := strings.Cut(message, " Resolved by "); found {
		message = strings.TrimSpace(summaryText)
		resolvedBy = strings.TrimSuffix(strings.TrimSpace(actor), ".")
	}

	label := strings.TrimSuffix(strings.TrimSpace(message), ".")
	status := ""
	decision := ""
	switch label {
	case "allowed once":
		status = "resolved-allow-once"
		decision = "allow-once"
	case "allowed always":
		status = "resolved-allow-always"
		decision = "allow-always"
	case "denied":
		status = "resolved-deny"
		decision = "deny"
	default:
		return nil, false
	}

	record := map[string]any{
		"status":              status,
		"summary":             "Exec approval " + label + ".",
		"approval_id":         approvalID,
		"approval_command_id": approvalID,
		"decision":            decision,
	}
	if resolvedBy != "" {
		record["detail_text"] = "Resolved by " + resolvedBy + "."
		record["resolved_by_id"] = resolvedBy
	}
	return record, true
}

func parseExpiredExecApprovalStatus(content string) (map[string]any, bool) {
	const prefix = "⏱️ Exec approval expired. ID: "
	if !strings.HasPrefix(content, prefix) {
		return nil, false
	}
	approvalID := strings.TrimSpace(strings.TrimPrefix(content, prefix))
	if approvalID == "" {
		return nil, false
	}
	return map[string]any{
		"status":              "approval-expired",
		"summary":             "Exec approval expired.",
		"approval_id":         approvalID,
		"approval_command_id": approvalID,
	}, true
}

func defaultApprovalSlug(approvalID string) string {
	normalized := util.NormalizeText(approvalID)
	if normalized == "" {
		return ""
	}
	if len(normalized) <= 8 {
		return normalized
	}
	return normalized[:8]
}

func mergeInboundChannelData(extraRaw json.RawMessage, channelData map[string]any) json.RawMessage {
	if len(channelData) == 0 {
		return util.CloneRawMessage(extraRaw)
	}

	envelope := map[string]any{}
	if len(extraRaw) > 0 {
		_ = json.Unmarshal(extraRaw, &envelope)
	}
	envelope["channel_data"] = channelData

	encoded, err := json.Marshal(envelope)
	if err != nil {
		return util.CloneRawMessage(extraRaw)
	}
	return encoded
}

func buildGrixCardLink(fallbackText, cardType string, payload map[string]any) string {
	return "[" + sanitizeMarkdownLinkText(fallbackText) + "](" + buildGrixCardURI(cardType, payload) + ")"
}

// sanitizeMarkdownLinkText strips characters from card fallback text that
// could break markdown link parsing (e.g. $ symbols that trigger LaTeX
// inline math in the frontend renderer). The fallback text is display-only;
// the actual card data lives in the grix://card URI parameters.
func sanitizeMarkdownLinkText(text string) string {
	return strings.ReplaceAll(text, "$", "")
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

func normalizeApprovalDecisions(value any) []string {
	switch typed := value.(type) {
	case []string:
		normalized := make([]string, 0, len(typed))
		for _, item := range typed {
			decision := util.NormalizeText(item)
			if isOneOf(decision, "allow-once", "allow-always", "deny") {
				normalized = append(normalized, decision)
			}
		}
		return normalized
	case []any:
		normalized := make([]string, 0, len(typed))
		for _, item := range typed {
			decision := util.NormalizeText(item)
			if isOneOf(decision, "allow-once", "allow-always", "deny") {
				normalized = append(normalized, decision)
			}
		}
		return normalized
	default:
		return nil
	}
}

func normalizeStringMap(value any) map[string]string {
	typed, ok := value.(map[string]any)
	if !ok {
		return nil
	}
	normalized := make(map[string]string, len(typed))
	for key, rawValue := range typed {
		normalizedKey := util.NormalizeText(key)
		normalizedValue := util.NormalizeText(rawValue)
		if normalizedKey == "" || normalizedValue == "" {
			continue
		}
		normalized[normalizedKey] = normalizedValue
	}
	if len(normalized) == 0 {
		return nil
	}
	return normalized
}

func normalizePositiveInt(value any) (int, bool) {
	parsed, ok := normalizeInt(value)
	return parsed, ok && parsed > 0
}

func normalizeNonNegativeInt(value any) (int, bool) {
	parsed, ok := normalizeInt(value)
	return parsed, ok && parsed >= 0
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

func isOneOf(value string, allowed ...string) bool {
	for _, candidate := range allowed {
		if value == candidate {
			return true
		}
	}
	return false
}

// NormalizeDangerousCommandText parses the Hermes dangerous command approval
// text pattern into an exec_approval card. This handles the fallback plain text
// format that the Hermes agent sends when structured approval delivery fails.
func NormalizeDangerousCommandText(content string) (cardContent string, channelData map[string]any, ok bool) {
	cd, matched := buildDangerousCommandApprovalChannelData(content)
	if !matched {
		return "", nil, false
	}
	card, matched := buildExecApprovalCardContent(cd)
	if !matched {
		return "", nil, false
	}
	return card, cd, true
}

func buildDangerousCommandApprovalChannelData(content string) (map[string]any, bool) {
	normalized := strings.TrimSpace(strings.ReplaceAll(content, "\r\n", "\n"))
	if !strings.Contains(normalized, "⚠️") || !strings.Contains(normalized, "Dangerous command requires approval") {
		return nil, false
	}

	command := extractFencedCodeBlock(normalized)
	if command == "" {
		return nil, false
	}

	reason := extractLabeledLine(normalized, "Reason:")
	approvalID := generateDangerousCommandApprovalID(command, reason)

	structured := map[string]any{
		"approval_command_id": approvalID,
		"command":             command,
		"host":                "hermes",
	}
	if reason != "" {
		structured["warning_text"] = reason
	}

	return map[string]any{
		"execApproval": map[string]any{
			"approvalId":       approvalID,
			"approvalSlug":     defaultApprovalSlug(approvalID),
			"allowedDecisions": []string{"allow-once", "allow-always", "deny"},
		},
		"grix": map[string]any{
			"execApproval": structured,
		},
	}, true
}

func extractFencedCodeBlock(content string) string {
	fenceStart := strings.Index(content, "```")
	if fenceStart < 0 {
		return ""
	}
	afterFence := content[fenceStart+3:]
	newlineIdx := strings.Index(afterFence, "\n")
	if newlineIdx < 0 {
		return ""
	}
	body := afterFence[newlineIdx+1:]
	fenceEnd := strings.Index(body, "```")
	if fenceEnd < 0 {
		return ""
	}
	return strings.TrimSpace(body[:fenceEnd])
}

func extractLabeledLine(content, label string) string {
	idx := strings.Index(content, label)
	if idx < 0 {
		return ""
	}
	after := content[idx+len(label):]
	end := strings.Index(after, "\n")
	if end < 0 {
		return strings.TrimSpace(after)
	}
	return strings.TrimSpace(after[:end])
}

func generateDangerousCommandApprovalID(command, reason string) string {
	h := fnv.New32a()
	h.Write([]byte(command))
	h.Write([]byte(reason))
	seq := approvalIDCounter.Add(1)
	return fmt.Sprintf("hd_%08x_%d_%d", h.Sum32(), time.Now().UnixNano(), seq)
}
