package agentapi

import (
	"errors"
	"strings"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/store"
)

type claudeInteractionRequestResult struct {
	Kind       string `json:"kind"`
	RequestID  string `json:"request_id"`
	SessionID  string `json:"session_id"`
	MessageID  string `json:"message_id"`
	NoticeSent bool   `json:"notice_sent"`
}

// ensureClaudeInteractionActor 在通用 actor 校验之上额外要求 client_type=claude：
// 交互请求卡的回程 claude_interaction_reply 是 Claude 适配器专属本地动作，
// 其他 agent 创建的卡片点了没人接，会留下永远无法完成的交互卡。
func ensureClaudeInteractionActor(agentID, ownerID int64) error {
	if err := ensureAccessControlActor(agentID, ownerID); err != nil {
		return err
	}
	var agent model.Agent
	if err := store.DB.Select("id,agent_client_type").First(&agent, agentID).Error; err != nil {
		return err
	}
	if strings.TrimSpace(agent.AgentClientType) != model.AgentClientTypeClaude {
		return errors.New("claude interaction requests require a claude agent actor")
	}
	return nil
}

func dispatchClaudeInteractionRequestCreate(agentID, ownerID int64, params map[string]interface{}, hooks agentInvokeHooks) (interface{}, int, string) {
	if err := ensureClaudeInteractionActor(agentID, ownerID); err != nil {
		return nil, 4003, err.Error()
	}
	if hooks.sendMessage == nil {
		return nil, 5001, "claude interaction request send hook unavailable"
	}

	kind, ok := paramString(params, "kind")
	if !ok || strings.TrimSpace(kind) == "" {
		return nil, 4001, "kind required"
	}
	requestID, ok := paramString(params, "request_id")
	if !ok || strings.TrimSpace(requestID) == "" {
		return nil, 4001, "request_id required"
	}
	sessionID, ok := paramString(params, "session_id")
	if !ok || strings.TrimSpace(sessionID) == "" {
		return nil, 4001, "session_id required"
	}
	messageID, ok := paramString(params, "message_id")
	if !ok || strings.TrimSpace(messageID) == "" {
		return nil, 4001, "message_id required"
	}
	requestPayload, ok := paramJSONObject(params, "payload")
	if !ok || len(requestPayload) == 0 {
		return nil, 4001, "payload required"
	}

	normalizedKind := strings.TrimSpace(kind)
	switch normalizedKind {
	case "permission":
		return sendClaudePermissionInteractionRequest(agentID, ownerID, normalizedKind, requestID, sessionID, messageID, requestPayload, hooks)
	case "elicitation":
		return sendClaudeElicitationInteractionRequest(agentID, ownerID, normalizedKind, requestID, sessionID, messageID, requestPayload, hooks)
	default:
		return nil, 4001, "kind invalid"
	}
}
