package hermes

import (
	"encoding/json"
	"strings"

	"github.com/askie/grix/backend/internal/agentadapter"
	"github.com/askie/grix/backend/internal/agentadapter/approvalcards"
	"github.com/askie/grix/backend/internal/agentadapter/internal/util"
)

func buildExecApprovalCardContentFromHermesChannelData(channelData map[string]any) (string, map[string]any, bool) {
	raw := util.NestedObject(util.NestedObject(channelData, "hermes"), "execApprovalPending")
	if len(raw) == 0 {
		return "", nil, false
	}

	approvalID := util.NormalizeText(raw["approval_id"])
	command := util.NormalizeText(raw["command"])
	if approvalID == "" || command == "" {
		return "", nil, false
	}

	replyMeta := util.CloneJSONObject(util.NestedObject(channelData, "execApproval"))
	if len(replyMeta) == 0 {
		replyMeta = map[string]any{}
	}
	replyMeta["approvalId"] = approvalID
	if approvalSlug := util.NormalizeText(replyMeta["approvalSlug"]); approvalSlug == "" {
		if slug := util.NormalizeText(raw["approval_slug"]); slug != "" {
			replyMeta["approvalSlug"] = slug
		} else {
			replyMeta["approvalSlug"] = defaultApprovalSlug(approvalID)
		}
	}
	if allowedDecisions := normalizeApprovalDecisions(replyMeta["allowedDecisions"]); len(allowedDecisions) == 0 {
		if allowedDecisions := normalizeApprovalDecisions(raw["allowed_decisions"]); len(allowedDecisions) > 0 {
			replyMeta["allowedDecisions"] = allowedDecisions
		} else {
			replyMeta["allowedDecisions"] = []string{"allow-once", "allow-always", "deny"}
		}
	}

	structured := map[string]any{
		"approval_command_id": approvalID,
		"command":             command,
		"host":                normalizeHost(raw["host"]),
	}
	if warningText := util.NormalizeText(raw["description"]); warningText != "" {
		structured["warning_text"] = warningText
	}
	if cwd := util.NormalizeText(raw["cwd"]); cwd != "" {
		structured["cwd"] = cwd
	}
	if nodeID := util.NormalizeText(raw["node_id"]); nodeID != "" {
		structured["node_id"] = nodeID
	}
	if expiresInSeconds, ok := normalizeNonNegativeInt(raw["expires_in_seconds"]); ok {
		structured["expires_in_seconds"] = expiresInSeconds
	}
	if expiresAtMs, ok := normalizePositiveInt(raw["expires_at_ms"]); ok {
		structured["expires_at_ms"] = expiresAtMs
	}
	if decisionCommands := normalizeStringMap(raw["decision_commands"]); len(decisionCommands) > 0 {
		structured["decision_commands"] = decisionCommands
	}

	normalizedChannelData := util.CloneJSONObject(channelData)
	normalizedChannelData["execApproval"] = replyMeta
	grixData := util.CloneJSONObject(util.NestedObject(normalizedChannelData, "grix"))
	if len(grixData) == 0 {
		grixData = map[string]any{}
	}
	grixData["execApproval"] = structured
	normalizedChannelData["grix"] = grixData

	channelDataRaw, err := json.Marshal(normalizedChannelData)
	if err != nil {
		return "", nil, false
	}
	cardContent, _, ok := approvalcards.Normalize(&agentadapter.InboundSendMsgPayload{
		ChannelData: channelDataRaw,
	})
	return cardContent, normalizedChannelData, ok
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

func normalizeApprovalDecisions(value any) []string {
	rawList, ok := value.([]any)
	if !ok {
		if rawStrings, ok := value.([]string); ok {
			rawList = make([]any, 0, len(rawStrings))
			for _, item := range rawStrings {
				rawList = append(rawList, item)
			}
		} else {
			return nil
		}
	}

	normalized := make([]string, 0, len(rawList))
	for _, item := range rawList {
		decision := util.NormalizeText(item)
		if isOneOf(decision, "allow-once", "allow-always", "deny") {
			normalized = append(normalized, decision)
		}
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
		number := strings.TrimSpace(typed)
		if number == "" {
			return 0, false
		}
		parsed, err := json.Number(number).Int64()
		return int(parsed), err == nil
	default:
		return 0, false
	}
}

func normalizeHost(value any) string {
	host := util.NormalizeText(value)
	if host == "" {
		return "hermes"
	}
	return host
}

func isOneOf(value string, allowed ...string) bool {
	for _, candidate := range allowed {
		if value == candidate {
			return true
		}
	}
	return false
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
