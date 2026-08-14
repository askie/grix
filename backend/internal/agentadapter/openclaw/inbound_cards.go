package openclaw

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

func normalizeInboundSendMsg(rawPayload []byte) (*agentadapter.NormalizedInboundEvent, error) {
	payload, err := agentadapter.ParseInboundPayload(rawPayload)
	if err != nil {
		return nil, err
	}

	content, extra := normalizeStructuredInboundCard(payload)

	if isOpenClawApprovalTextDup(payload, content) {
		return &agentadapter.NormalizedInboundEvent{
			SessionID: strings.TrimSpace(payload.SessionID),
			Drop:      true,
		}, nil
	}

	return &agentadapter.NormalizedInboundEvent{
		SessionID: strings.TrimSpace(payload.SessionID),
		Content:   content,
		Extra:     extra,
	}, nil
}

// isOpenClawApprovalTextDup detects the redundant plain-text approval message
// that OpenClaw agents send alongside the structured execApprovalPending card.
func isOpenClawApprovalTextDup(payload *agentadapter.InboundSendMsgPayload, normalizedContent string) bool {
	if strings.Contains(normalizedContent, "grix://card/") {
		return false
	}
	if !strings.HasPrefix(payload.Content, "Approval required.\n\nRun:\n\n```txt\n/approve") {
		return false
	}
	channelData := util.DecodeJSONObject(payload.ChannelData)
	execApproval := util.NestedObject(channelData, "execApproval")
	if len(execApproval) == 0 {
		return false
	}
	return util.NormalizeText(execApproval["approvalId"]) != "" &&
		util.NormalizeText(execApproval["approvalKind"]) == "exec"
}

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

	channelData := util.DecodeJSONObject(p.ChannelData)
	if len(channelData) > 0 {
		if cardContent, normalizedChannelData, ok := buildExecApprovalCardContentFromOpenClawChannelData(channelData); ok {
			return cardContent, mergeInboundChannelData(p.Extra, normalizedChannelData)
		}
		if cardContent, normalizedChannelData, ok := buildExecStatusCardContentFromOpenClawChannelData(channelData); ok {
			return cardContent, mergeInboundChannelData(p.Extra, normalizedChannelData)
		}
		if cardContent, ok := buildEggInstallStatusCardContent(channelData); ok {
			return cardContent, util.CloneRawMessage(p.Extra)
		}
		if cardContent, ok := buildUserProfileCardContent(channelData); ok {
			return cardContent, util.CloneRawMessage(p.Extra)
		}
		if cardContent, ok := buildToolExecutionCardContent(channelData); ok {
			return cardContent, util.CloneRawMessage(p.Extra)
		}
	}

	return p.Content, util.CloneRawMessage(p.Extra)
}

func buildExecApprovalCardContentFromOpenClawChannelData(channelData map[string]any) (string, map[string]any, bool) {
	raw := util.NestedObject(util.NestedObject(channelData, "openclaw"), "execApprovalPending")
	if len(raw) == 0 {
		return "", nil, false
	}

	approvalID := util.NormalizeText(raw["id"])
	request := util.NestedObject(raw, "request")
	command := resolveOpenClawApprovalCommand(request)
	if approvalID == "" || command == "" {
		return "", nil, false
	}

	replyMeta := util.CloneJSONObject(util.NestedObject(channelData, "execApproval"))
	if len(replyMeta) == 0 {
		replyMeta = map[string]any{}
	}
	replyMeta["approvalId"] = approvalID
	if approvalSlug := util.NormalizeText(replyMeta["approvalSlug"]); approvalSlug == "" {
		replyMeta["approvalSlug"] = defaultApprovalSlug(approvalID)
	}
	if allowedDecisions := normalizeApprovalDecisions(replyMeta["allowedDecisions"]); len(allowedDecisions) == 0 {
		replyMeta["allowedDecisions"] = []string{"allow-once", "allow-always", "deny"}
	}

	structured := map[string]any{
		"approval_command_id": approvalID,
		"command":             command,
		"host":                resolveOpenClawHost(request),
	}
	if cwd := util.NormalizeText(request["cwd"]); cwd != "" {
		structured["cwd"] = cwd
	}
	if nodeID := util.NormalizeText(request["nodeId"]); nodeID != "" {
		structured["node_id"] = nodeID
	}
	if warningText := util.NormalizeText(raw["warningText"]); warningText != "" {
		structured["warning_text"] = warningText
	}
	if expiresAtMs, ok := normalizePositiveInt(raw["expiresAtMs"]); ok {
		structured["expires_at_ms"] = expiresAtMs
	}

	normalizedChannelData := util.CloneJSONObject(channelData)
	normalizedChannelData["execApproval"] = replyMeta
	grixData := util.CloneJSONObject(util.NestedObject(normalizedChannelData, "grix"))
	if len(grixData) == 0 {
		grixData = map[string]any{}
	}
	grixData["execApproval"] = structured
	normalizedChannelData["grix"] = grixData

	cardContent, ok := buildExecApprovalCardContent(normalizedChannelData)
	return cardContent, normalizedChannelData, ok
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

	fallbackText := fmt.Sprintf("[Exec Approval] %s", compactText(command, 160))
	if host != "" {
		fallbackText = fmt.Sprintf("%s (%s)", fallbackText, host)
	}
	fallbackText = fmt.Sprintf("%s\n/approve %s allow-once", fallbackText, approvalCommandID)

	return buildGrixCardLink(fallbackText, "exec_approval", payload), true
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

	return buildGrixCardLink(
		fmt.Sprintf("[Exec Status] %s", compactText(summary, 180)),
		"exec_status",
		payload,
	), true
}

func buildExecStatusCardContentFromOpenClawChannelData(channelData map[string]any) (string, map[string]any, bool) {
	raw := util.NestedObject(util.NestedObject(channelData, "openclaw"), "execApprovalResolved")
	if len(raw) == 0 {
		return "", nil, false
	}

	approvalID := util.NormalizeText(raw["id"])
	decision := util.NormalizeText(raw["decision"])
	status, summary, ok := resolveOpenClawResolvedExecStatus(decision)
	if approvalID == "" || !ok {
		return "", nil, false
	}

	record := map[string]any{
		"status":              status,
		"summary":             summary,
		"approval_id":         approvalID,
		"approval_command_id": approvalID,
		"decision":            decision,
	}
	if resolvedByID := util.NormalizeText(raw["resolvedBy"]); resolvedByID != "" {
		record["resolved_by_id"] = resolvedByID
		record["detail_text"] = fmt.Sprintf("Resolved by %s.", resolvedByID)
	}

	request := util.NestedObject(raw, "request")
	if command := resolveOpenClawApprovalCommand(request); command != "" {
		record["command"] = command
	}
	if host := resolveOpenClawHost(request); host != "" {
		record["host"] = host
	}
	if nodeID := util.NormalizeText(request["nodeId"]); nodeID != "" {
		record["node_id"] = nodeID
	}
	if reason := util.NormalizeText(raw["reason"]); reason != "" {
		record["reason"] = reason
	}
	if warningText := util.NormalizeText(raw["warningText"]); warningText != "" {
		record["warning_text"] = warningText
	}

	normalizedChannelData := util.CloneJSONObject(channelData)
	grixData := util.CloneJSONObject(util.NestedObject(normalizedChannelData, "grix"))
	if len(grixData) == 0 {
		grixData = map[string]any{}
	}
	grixData["execStatus"] = record
	normalizedChannelData["grix"] = grixData

	cardContent, ok := buildExecStatusCardContent(normalizedChannelData)
	return cardContent, normalizedChannelData, ok
}

func buildEggInstallStatusCardContent(channelData map[string]any) (string, bool) {
	record := util.NestedObject(util.NestedObject(channelData, "grix"), "eggInstall")
	if len(record) == 0 {
		return "", false
	}

	installID := util.NormalizeText(record["install_id"])
	status := util.NormalizeText(record["status"])
	if installID == "" || !isOneOf(status, "running", "success", "failed") {
		return "", false
	}

	payload := map[string]any{
		"install_id": installID,
		"status":     status,
	}
	if step := util.NormalizeText(record["step"]); step != "" {
		payload["step"] = step
	}
	summary := util.NormalizeText(record["summary"])
	if summary == "" {
		summary = defaultEggInstallSummary(status, util.NormalizeText(record["step"]))
	}
	payload["summary"] = summary
	if detailText := util.NormalizeText(record["detail_text"]); detailText != "" {
		payload["detail_text"] = detailText
	}
	if targetAgentID := util.NormalizeText(record["target_agent_id"]); targetAgentID != "" {
		payload["target_agent_id"] = targetAgentID
	}
	if errorCode := util.NormalizeText(record["error_code"]); errorCode != "" {
		payload["error_code"] = errorCode
	}
	if errorMsg := util.NormalizeText(record["error_msg"]); errorMsg != "" {
		payload["error_msg"] = errorMsg
	}

	return buildGrixCardLink(
		fmt.Sprintf("[Egg Install] %s", compactText(summary, 180)),
		"egg_install_status",
		payload,
	), true
}

func buildUserProfileCardContent(channelData map[string]any) (string, bool) {
	record := util.NestedObject(util.NestedObject(channelData, "grix"), "userProfile")
	if len(record) == 0 {
		return "", false
	}

	userID := util.NormalizeText(record["user_id"])
	nickname := util.NormalizeText(record["nickname"])
	if userID == "" || nickname == "" {
		return "", false
	}

	payload := map[string]any{
		"user_id":  userID,
		"nickname": nickname,
	}
	if peerType, ok := normalizePeerType(record["peer_type"]); ok {
		payload["peer_type"] = peerType
	} else {
		payload["peer_type"] = 1
	}
	if avatarURL := util.NormalizeText(record["avatar_url"]); avatarURL != "" {
		payload["avatar_url"] = avatarURL
	}

	return buildGrixCardLink(
		fmt.Sprintf("[Profile Card] %s", compactText(nickname, 120)),
		"user_profile",
		payload,
	), true
}

func buildToolExecutionCardContent(channelData map[string]any) (string, bool) {
	record := util.NestedObject(util.NestedObject(channelData, "grix"), "toolExecution")
	if len(record) == 0 {
		return "", false
	}

	summary := util.NormalizeText(record["summary_text"])
	if summary == "" {
		return "", false
	}

	payload := map[string]any{
		"summary_text": summary,
	}
	if detailText := util.NormalizeText(record["detail_text"]); detailText != "" {
		payload["detail_text"] = detailText
	}

	return buildGrixCardLink(
		fmt.Sprintf("[Tool] %s", compactText(summary, 180)),
		"tool_execution",
		payload,
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
		switch value.(type) {
		case []any, []string, map[string]any:
			return true
		}
	}
	return false
}

func defaultEggInstallSummary(status, step string) string {
	switch status {
	case "running":
		if step != "" {
			return "Installation in progress: " + step
		}
		return "Installation in progress"
	case "success":
		if step != "" {
			return "Installation completed: " + step
		}
		return "Installation completed"
	case "failed":
		if step != "" {
			return "Installation failed: " + step
		}
		return "Installation failed"
	default:
		return "Installation status updated"
	}
}

func compactText(text string, limit int) string {
	compact := strings.Join(strings.Fields(util.NormalizeText(text)), " ")
	if limit <= 3 || len(compact) <= limit {
		return compact
	}
	return compact[:limit-3] + "..."
}

func resolveOpenClawApprovalCommand(request map[string]any) string {
	if len(request) == 0 {
		return ""
	}
	if command := util.NormalizeText(request["command"]); command != "" {
		return command
	}
	if commandPreview := util.NormalizeText(request["commandPreview"]); commandPreview != "" {
		return commandPreview
	}
	if systemRunPlan := util.NestedObject(request, "systemRunPlan"); len(systemRunPlan) > 0 {
		if commandText := util.NormalizeText(systemRunPlan["commandText"]); commandText != "" {
			return commandText
		}
		if commandPreview := util.NormalizeText(systemRunPlan["commandPreview"]); commandPreview != "" {
			return commandPreview
		}
	}
	return ""
}

func resolveOpenClawHost(request map[string]any) string {
	host := util.NormalizeText(request["host"])
	switch host {
	case "sandbox", "gateway", "node":
		return host
	default:
		return "gateway"
	}
}

func resolveOpenClawResolvedExecStatus(decision string) (status string, summary string, ok bool) {
	switch decision {
	case "allow-once":
		return "resolved-allow-once", "Command approved for one run.", true
	case "allow-always":
		return "resolved-allow-always", "Command approved for future runs.", true
	case "deny":
		return "resolved-deny", "Command approval denied.", true
	default:
		return "", "", false
	}
}

func normalizeApprovalDecisions(value any) []string {
	rawList, ok := value.([]any)
	if !ok {
		return nil
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

func normalizePeerType(value any) (int, bool) {
	switch util.NormalizeText(value) {
	case "1":
		return 1, true
	case "2":
		return 2, true
	default:
		return 0, false
	}
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
