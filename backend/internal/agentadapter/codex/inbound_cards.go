package codex

import (
	"encoding/json"
	"net/url"
	"strconv"
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
	return &agentadapter.NormalizedInboundEvent{
		SessionID: strings.TrimSpace(payload.SessionID),
		Content:   content,
		Extra:     extra,
	}, nil
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

	if normalizedP, ok := buildCodexExecApprovalPayload(p); ok {
		if cardContent, normalizedExtra, ok := approvalcards.Normalize(normalizedP); ok {
			return cardContent, normalizedExtra
		}
		return p.Content, util.CloneRawMessage(p.Extra)
	}
	if normalizedP, ok := buildCodexExecApprovalPendingPayload(p); ok {
		if cardContent, normalizedExtra, ok := approvalcards.Normalize(normalizedP); ok {
			return cardContent, normalizedExtra
		}
		return p.Content, util.CloneRawMessage(p.Extra)
	}
	if len(p.ChannelData) > 0 {
		channelData := util.DecodeJSONObject(p.ChannelData)
		if len(channelData) > 0 {
			if cardContent, ok := buildCodexSessionBindingCardContent(channelData); ok {
				return cardContent, util.CloneRawMessage(p.Extra)
			}
		}
	}

	if readable, ok := extractCodexJSONError(p.Content); ok {
		return readable, util.CloneRawMessage(p.Extra)
	}

	return p.Content, util.CloneRawMessage(p.Extra)
}

// extractCodexJSONError detects when content is a JSON error payload and
// extracts a human-readable error message. This is a Codex-specific fork —
// other adapters should implement their own version based on their error format.
//
// Supported patterns:
//   - {"error": {"message": "..."}}
//   - {"error": "..."}
//   - {"error": {"code": "...", "message": "..."}}
//   - {"message": "..."} when "error" key is also present
func extractCodexJSONError(content string) (string, bool) {
	trimmed := strings.TrimSpace(content)
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return "", false
	}

	var obj map[string]any
	if json.Unmarshal([]byte(trimmed), &obj) != nil {
		return "", false
	}

	if _, hasError := obj["error"]; !hasError {
		return "", false
	}

	switch err := obj["error"].(type) {
	case string:
		if msg := strings.TrimSpace(err); msg != "" {
			return "Error: " + msg, true
		}
	case map[string]any:
		msg := util.FirstNonEmpty(
			util.NormalizeText(err["message"]),
			util.NormalizeText(err["msg"]),
			util.NormalizeText(obj["message"]),
		)
		if code := util.NormalizeText(err["code"]); code != "" {
			if msg != "" {
				return "Error (" + code + "): " + msg, true
			}
			return "Error: " + code, true
		}
		if msg != "" {
			return "Error: " + msg, true
		}
	}

	// fallback: has "error" key but couldn't extract a readable message
	// try top-level "message" field
	if msg := util.NormalizeText(obj["message"]); msg != "" {
		return "Error: " + msg, true
	}

	return "", false
}

func buildCodexExecApprovalPayload(p *agentadapter.InboundSendMsgPayload) (*agentadapter.InboundSendMsgPayload, bool) {
	if p.SessionID == "" || len(p.ChannelData) == 0 {
		return nil, false
	}

	channelData := util.DecodeJSONObject(p.ChannelData)
	if len(channelData) == 0 {
		return nil, false
	}

	normalizedChannelData, ok := buildExecApprovalChannelDataFromCodex(p.SessionID, channelData)
	if !ok {
		return nil, false
	}
	channelDataRaw, _ := json.Marshal(normalizedChannelData)
	result := &agentadapter.InboundSendMsgPayload{
		Content:     p.Content,
		Extra:       p.Extra,
		ChannelData: channelDataRaw,
	}
	agentadapter.MergeCardsIntoExtra(result)
	return result, true
}

func buildCodexSessionBindingCardContent(channelData map[string]any) (string, bool) {
	record := util.NestedObject(util.NestedObject(channelData, "codex"), "sessionBinding")
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
		"detail_text":  "先提交一个工作目录。校验通过后，Codex 会自动继续处理刚才那条消息。",
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

	return buildCodexGrixCardLink(
		"[Open Workspace] 当前对话还没有打开工作目录。",
		"agent_open_session",
		payload,
	), true
}

func buildCodexGrixCardLink(fallbackText, cardType string, payload map[string]any) string {
	return "[" + fallbackText + "](" + buildCodexGrixCardURI(cardType, payload) + ")"
}

func buildCodexGrixCardURI(cardType string, payload map[string]any) string {
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

func buildExecApprovalChannelDataFromCodex(sessionID string, channelData map[string]any) (map[string]any, bool) {
	raw := util.NestedObject(util.NestedObject(channelData, "codex"), "execApprovalRequest")
	if len(raw) == 0 {
		return nil, false
	}

	approvalCommandID := util.NormalizeText(raw["id"])
	method := util.NormalizeText(raw["method"])
	params := util.CloneJSONObject(util.NestedObject(raw, "params"))
	command := SummarizeCodexApprovalRequest(method, params)
	if approvalCommandID == "" || command == "" {
		return nil, false
	}

	replyMeta := util.CloneJSONObject(util.NestedObject(channelData, "execApproval"))
	if len(replyMeta) == 0 {
		replyMeta = map[string]any{}
	}
	replyMeta["approvalId"] = BuildApprovalID(sessionID, approvalCommandID)
	replyMeta["approvalSlug"] = approvalCommandID
	replyMeta["allowedDecisions"] = NormalizeCodexApprovalDecisions(method, params)

	structured := map[string]any{
		"approval_command_id": approvalCommandID,
		"command":             command,
		"host":                "Codex",
	}
	if cwd := extractCodexApprovalCwd(params); cwd != "" {
		structured["cwd"] = cwd
	}
	if warningText := WarningTextForCodexApproval(method); warningText != "" {
		structured["warning_text"] = warningText
	}

	normalizedChannelData := util.CloneJSONObject(channelData)
	normalizedChannelData["execApproval"] = replyMeta
	grixData := util.CloneJSONObject(util.NestedObject(normalizedChannelData, "grix"))
	if grixData == nil {
		grixData = map[string]any{}
	}
	grixData["execApproval"] = structured
	normalizedChannelData["grix"] = grixData
	return normalizedChannelData, true
}

func buildCodexExecApprovalPendingPayload(p *agentadapter.InboundSendMsgPayload) (*agentadapter.InboundSendMsgPayload, bool) {
	if len(p.ChannelData) == 0 {
		return nil, false
	}
	channelData := util.DecodeJSONObject(p.ChannelData)
	if len(channelData) == 0 {
		return nil, false
	}

	raw := util.NestedObject(util.NestedObject(channelData, "grix"), "execApprovalPending")
	if len(raw) == 0 {
		raw = util.NestedObject(util.NestedObject(channelData, "codex"), "execApprovalPending")
	}
	if len(raw) == 0 {
		return nil, false
	}

	approvalID := util.FirstNonEmpty(
		util.NormalizeText(raw["approval_id"]),
		util.NormalizeText(raw["approvalId"]),
		util.NormalizeText(raw["id"]),
	)
	command := util.NormalizeText(raw["command"])
	if approvalID == "" || command == "" {
		return nil, false
	}

	replyMeta := util.CloneJSONObject(util.NestedObject(channelData, "execApproval"))
	if len(replyMeta) == 0 {
		replyMeta = map[string]any{}
	}
	replyMeta["approvalId"] = approvalID
	if approvalSlug := util.NormalizeText(replyMeta["approvalSlug"]); approvalSlug == "" {
		replyMeta["approvalSlug"] = util.FirstNonEmpty(
			util.NormalizeText(raw["approval_slug"]),
			util.NormalizeText(raw["approvalSlug"]),
			approvalID,
		)
	}
	if allowedDecisions := normalizeCodexPendingDecisions(raw["allowed_decisions"]); len(allowedDecisions) > 0 {
		replyMeta["allowedDecisions"] = allowedDecisions
	} else if allowedDecisions := normalizeCodexPendingDecisions(raw["allowedDecisions"]); len(allowedDecisions) > 0 {
		replyMeta["allowedDecisions"] = allowedDecisions
	} else {
		replyMeta["allowedDecisions"] = []string{"allow-once", "allow-always", "deny"}
	}

	structured := map[string]any{
		"approval_command_id": util.FirstNonEmpty(
			util.NormalizeText(raw["approval_command_id"]),
			util.NormalizeText(raw["approvalCommandId"]),
			approvalID,
		),
		"command": command,
		"host": util.FirstNonEmpty(
			util.NormalizeText(raw["host"]),
			"Codex",
		),
	}
	if warningText := util.FirstNonEmpty(
		util.NormalizeText(raw["description"]),
		util.NormalizeText(raw["warning_text"]),
		util.NormalizeText(raw["warningText"]),
	); warningText != "" {
		structured["warning_text"] = warningText
	}
	if cwd := util.FirstNonEmpty(
		util.NormalizeText(raw["cwd"]),
		util.NormalizeText(raw["working_directory"]),
		util.NormalizeText(raw["workingDirectory"]),
	); cwd != "" {
		structured["cwd"] = cwd
	}

	normalizedChannelData := util.CloneJSONObject(channelData)
	normalizedChannelData["execApproval"] = replyMeta
	grixData := util.CloneJSONObject(util.NestedObject(normalizedChannelData, "grix"))
	if len(grixData) == 0 {
		grixData = map[string]any{}
	}
	grixData["execApproval"] = structured
	normalizedChannelData["grix"] = grixData

	channelDataRaw, _ := json.Marshal(normalizedChannelData)
	result := &agentadapter.InboundSendMsgPayload{
		Content:     p.Content,
		Extra:       p.Extra,
		ChannelData: channelDataRaw,
	}
	agentadapter.MergeCardsIntoExtra(result)
	return result, true
}

func normalizeCodexPendingDecisions(value any) []string {
	items, ok := value.([]any)
	if !ok {
		if rawStrings, ok := value.([]string); ok {
			items = make([]any, 0, len(rawStrings))
			for _, v := range rawStrings {
				items = append(items, v)
			}
		} else {
			return nil
		}
	}

	normalized := make([]string, 0, len(items))
	for _, item := range items {
		decision := util.NormalizeText(item)
		switch decision {
		case "allow-once", "allow-always", "deny":
			normalized = append(normalized, decision)
		}
	}
	return normalized
}

func BuildApprovalID(sessionID, approvalCommandID string) string {
	return "codex_" + SanitizeIdentifier(sessionID) + "_" + SanitizeIdentifier(approvalCommandID)
}

func SanitizeIdentifier(value string) string {
	var builder strings.Builder
	for _, r := range strings.TrimSpace(value) {
		switch {
		case r >= 'a' && r <= 'z':
			builder.WriteRune(r)
		case r >= 'A' && r <= 'Z':
			builder.WriteRune(r)
		case r >= '0' && r <= '9':
			builder.WriteRune(r)
		case r == '.' || r == '_' || r == ':' || r == '-':
			builder.WriteRune(r)
		default:
			builder.WriteByte('_')
		}
	}
	return builder.String()
}

func SummarizeCodexApprovalRequest(method string, params map[string]any) string {
	if method == "item/commandExecution/requestApproval" {
		return util.FirstNonEmpty(
			util.NormalizeText(params["command"]),
			util.NormalizeText(params["commandLine"]),
			util.NormalizeText(params["command_line"]),
			joinPrimitiveArray(params["argv"], " "),
			joinPrimitiveArray(params["args"], " "),
			"Codex wants to run a command.",
		)
	}

	if path := util.FirstNonEmpty(
		util.NormalizeText(params["path"]),
		util.NormalizeText(params["filePath"]),
		util.NormalizeText(params["file_path"]),
		util.NormalizeText(params["targetPath"]),
		util.NormalizeText(params["target_path"]),
	); path != "" {
		return "Codex wants to change " + path
	}

	if paths := util.FirstNonEmpty(
		joinPrimitiveArray(params["paths"], "\n"),
		joinPrimitiveArray(params["files"], "\n"),
	); paths != "" {
		return "Codex wants to change:\n" + paths
	}

	return "Codex wants to change files in the workspace."
}

func extractCodexApprovalCwd(params map[string]any) string {
	return util.FirstNonEmpty(
		util.NormalizeText(params["cwd"]),
		util.NormalizeText(params["workingDirectory"]),
		util.NormalizeText(params["working_directory"]),
	)
}

func WarningTextForCodexApproval(method string) string {
	if method == "item/fileChange/requestApproval" {
		return "Review the requested file changes before approving."
	}
	return "Review this command before approving."
}

func NormalizeCodexApprovalDecisions(method string, params map[string]any) []string {
	decisions := make([]string, 0, 3)
	if hasAvailableDecision(params, "accept") {
		decisions = append(decisions, "allow-once")
	}
	if method == "item/commandExecution/requestApproval" && hasAvailableDecision(params, "acceptForSession") {
		decisions = append(decisions, "allow-always")
	}
	if hasAvailableDecision(params, "deny") {
		decisions = append(decisions, "deny")
	}
	if len(decisions) == 0 {
		return []string{"allow-once"}
	}
	return decisions
}

func hasAvailableDecision(params map[string]any, decision string) bool {
	available, ok := params["availableDecisions"].([]any)
	if !ok {
		return true
	}
	for _, entry := range available {
		if util.NormalizeText(entry) == decision {
			return true
		}
	}
	return false
}

func joinPrimitiveArray(value any, separator string) string {
	items, ok := value.([]any)
	if !ok {
		return ""
	}
	parts := make([]string, 0, len(items))
	for _, entry := range items {
		switch typed := entry.(type) {
		case string:
			if text := strings.TrimSpace(typed); text != "" {
				parts = append(parts, text)
			}
		case float64:
			parts = append(parts, strconv.FormatFloat(typed, 'f', -1, 64))
		case bool:
			parts = append(parts, strconv.FormatBool(typed))
		}
	}
	return strings.TrimSpace(strings.Join(parts, separator))
}
