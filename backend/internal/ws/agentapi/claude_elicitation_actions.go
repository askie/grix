package agentapi

import (
	"context"
	"fmt"
	"strings"
	"time"
)

const (
	claudeQuestionFooterText = "Use the card to answer. Free text is allowed when none of the listed options fit."
	// claudeElicitationAnswerWindow 是 elicitation 路径的作答窗口，
	// 与 connector 侧 elicitation-hook 的内部等待上限（10 分钟）保持一致。
	// 提问卡以此下发 expires_at，前端倒计时并在到期后禁用提交。
	claudeElicitationAnswerWindow = 10 * time.Minute
)

func sendClaudeElicitationInteractionRequest(agentID, ownerID int64, kind, requestID, sessionID, messageID string, requestPayload map[string]interface{}, hooks agentInvokeHooks) (interface{}, int, string) {
	trimmedRequestID := strings.TrimSpace(requestID)
	trimmedSessionID := strings.TrimSpace(sessionID)
	cardPayload, ok := buildClaudeQuestionCardPayload(trimmedRequestID, requestPayload)
	if !ok {
		return nil, 4001, "payload question definition required"
	}
	content := buildLocalGrixCardLink(
		fmt.Sprintf("[Agent Question] %s", trimmedRequestID),
		"agent_question",
		cardPayload,
	)
	result, err := hooks.sendMessage(SendMessageReq{
		AgentID:         agentID,
		OwnerID:         ownerID,
		SessionID:       trimmedSessionID,
		ClientMsgID:     fmt.Sprintf("claude_elicitation_request_%s", trimmedRequestID),
		MsgType:         1,
		Content:         content,
		QuotedMessageID: parseOptionalQuotedMessageID(messageID),
	})
	if err != nil {
		return nil, 5001, err.Error()
	}

	if result != nil && result.MsgID > 0 {
		saveApprovalCardMsgID(context.Background(), agentID, trimmedSessionID, trimmedRequestID, result.MsgID)
	}

	return claudeInteractionRequestResult{
		Kind:       strings.TrimSpace(kind),
		RequestID:  trimmedRequestID,
		SessionID:  trimmedSessionID,
		MessageID:  strings.TrimSpace(messageID),
		NoticeSent: true,
	}, 0, "ok"
}

func buildClaudeQuestionCardPayload(requestID string, requestPayload map[string]interface{}) (map[string]any, bool) {
	if strings.EqualFold(strings.TrimSpace(fmt.Sprint(requestPayload["mode"])), "url") {
		return buildClaudeURLQuestionCardPayload(requestID, requestPayload)
	}
	questions, ok := buildClaudeRequestedSchemaQuestions(requestPayload["requested_schema"])
	if !ok || len(questions) == 0 {
		return nil, false
	}
	payload := map[string]any{
		"request_id":  requestID,
		"mode":        "form",
		"questions":   questions,
		"footer_text": claudeQuestionFooterText,
		"expires_at":  time.Now().Add(claudeElicitationAnswerWindow).UnixMilli(),
	}
	setOptionalClaudeQuestionField(payload, "message", requestPayload["message"])
	return payload, true
}

func buildClaudeURLQuestionCardPayload(requestID string, requestPayload map[string]interface{}) (map[string]any, bool) {
	urlValue := strings.TrimSpace(fmt.Sprint(requestPayload["url"]))
	if urlValue == "" {
		return nil, false
	}

	payload := map[string]any{
		"request_id":            requestID,
		"mode":                  "url",
		"questions":             []map[string]any{},
		"message":               optionalClaudeQuestionText(requestPayload["message"], "Open the authentication page to continue."),
		"url":                   urlValue,
		"open_url_label":        "Open authentication page",
		"footer_text":           "Open the page, finish the flow, then tap Complete. Cancel if you do not want to continue.",
		"submitted_accept_text": "Authentication completed.",
		"submitted_cancel_text": "Authentication canceled.",
		"expires_at":            time.Now().Add(claudeElicitationAnswerWindow).UnixMilli(),
	}
	return payload, true
}

func optionalClaudeQuestionText(rawValue any, fallback string) string {
	if value := normalizeClaudeQuestionText(rawValue); value != "" {
		return value
	}
	return strings.TrimSpace(fallback)
}

func setOptionalClaudeQuestionField(payload map[string]any, key string, value any) {
	normalized := normalizeClaudeQuestionText(value)
	if normalized == "" {
		return
	}
	payload[key] = normalized
}

func normalizeClaudeQuestionText(value any) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(value))
}
