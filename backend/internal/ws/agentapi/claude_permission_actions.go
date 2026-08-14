package agentapi

import (
	"context"
	"fmt"
	"strconv"
	"strings"
)

func sendClaudePermissionInteractionRequest(agentID, ownerID int64, kind, requestID, sessionID, messageID string, requestPayload map[string]interface{}, hooks agentInvokeHooks) (interface{}, int, string) {
	trimmedRequestID := strings.TrimSpace(requestID)
	trimmedSessionID := strings.TrimSpace(sessionID)
	cardPayload := buildClaudePermissionApprovalPayload(trimmedRequestID, requestPayload)
	cardMessage := buildExecApprovalCardMessage(cardPayload)
	if strings.TrimSpace(cardMessage.content) == "" {
		return nil, 5001, "claude interaction request card build failed"
	}

	// 托管代答场景审批卡改投主人私聊（口径见 handleSendMsg 的同款处理）；
	// 引用消息属于原会话语境，改投后一并丢弃。
	targetSessionID := resolveApprovalCardSessionID(context.Background(), trimmedSessionID, agentID, ownerID)
	quotedMessageID := parseOptionalQuotedMessageID(messageID)
	if targetSessionID != trimmedSessionID {
		quotedMessageID = 0
	}

	result, err := hooks.sendMessage(SendMessageReq{
		AgentID:         agentID,
		OwnerID:         ownerID,
		SessionID:       targetSessionID,
		ClientMsgID:     fmt.Sprintf("claude_permission_request_%s", trimmedRequestID),
		MsgType:         1,
		Content:         cardMessage.content,
		Extra:           cardMessage.extra,
		VisibleTo:       ownerVisibleToForAdapterCard("claude/base", cardMessage.content, cardMessage.extra, ownerID),
		QuotedMessageID: quotedMessageID,
	})
	if err != nil {
		return nil, 5001, err.Error()
	}

	if result != nil && result.MsgID > 0 {
		saveApprovalCardMsgID(context.Background(), agentID, targetSessionID, trimmedRequestID, result.MsgID)
	}

	return claudeInteractionRequestResult{
		Kind:       strings.TrimSpace(kind),
		RequestID:  trimmedRequestID,
		SessionID:  targetSessionID,
		MessageID:  strings.TrimSpace(messageID),
		NoticeSent: true,
	}, 0, "ok"
}

func buildClaudePermissionApprovalPayload(requestID string, requestPayload map[string]interface{}) map[string]any {
	command := buildClaudePermissionCommandText(requestPayload)
	payload := map[string]any{
		"approval_id":         requestID,
		"approval_slug":       requestID,
		"approval_command_id": requestID,
		"command":             command,
		"host":                "Claude Grix",
		"allowed_decisions":   []string{"allow-once", "deny"},
		"warning_text":        "Claude approvals support allow once or deny.",
	}
	return payload
}

func buildClaudePermissionCommandText(requestPayload map[string]interface{}) string {
	toolName := normalizeOptionalText(requestPayload["tool_name"])
	description := normalizeOptionalText(requestPayload["description"])
	inputPreview := normalizeOptionalText(requestPayload["input_preview"])

	lines := make([]string, 0, 3)
	if toolName != "" {
		lines = append(lines, toolName)
	}
	if description != "" && description != toolName {
		lines = append(lines, description)
	}
	if inputPreview != "" && inputPreview != description && inputPreview != toolName {
		lines = append(lines, inputPreview)
	}
	if len(lines) == 0 {
		return "Claude requested approval."
	}
	return strings.Join(lines, "\n")
}

func normalizeOptionalText(value interface{}) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(value))
}

func parseOptionalQuotedMessageID(raw string) int64 {
	value := strings.TrimSpace(raw)
	if value == "" {
		return 0
	}
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return 0
	}
	return parsed
}

func paramJSONObject(params map[string]interface{}, key string) (map[string]interface{}, bool) {
	value, ok := params[key]
	if !ok {
		return nil, false
	}
	object, ok := value.(map[string]interface{})
	if !ok {
		return nil, false
	}
	return object, true
}
