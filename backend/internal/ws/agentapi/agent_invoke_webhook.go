package agentapi

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/store"
	"github.com/askie/grix/backend/internal/webhook"
)

var webhookInvokeService = webhook.NewService()

// dispatchWebhookCreate 让有 webhook.create scope 的 agent 为自己所在的会话创建 webhook 入口。
// 限制：agent 必须是目标会话成员（member_type=2），主人也必须是成员（由 webhook 服务校验）。
// 入口以主人身份投递消息，因而能像人类发言一样唤醒该会话里的 agent；
// 典型用途是本机计划任务/cron 定时 POST 触发 agent。
func dispatchWebhookCreate(agentID, ownerID int64, params map[string]interface{}) (interface{}, int, string) {
	sessionID, ok := paramString(params, "session_id")
	sessionID = strings.TrimSpace(sessionID)
	if !ok || sessionID == "" {
		return nil, 4001, "session_id required"
	}
	var expiresAt *time.Time
	if raw, ok := paramString(params, "expires_at"); ok && strings.TrimSpace(raw) != "" {
		parsed, err := time.Parse(time.RFC3339, strings.TrimSpace(raw))
		if err != nil {
			return nil, 4001, "expires_at must be RFC3339"
		}
		u := parsed.UTC()
		expiresAt = &u
	}

	if code, msg := requireAgentInSession(agentID, sessionID); code != 0 {
		return nil, code, msg
	}

	baseURL := webhook.BaseURL()
	if baseURL == "" {
		return nil, 5001, "webhook base url not configured"
	}
	item, err := webhookInvokeService.CreateEndpoint(context.Background(), webhook.CreateRequest{
		UserID:    ownerID,
		SessionID: sessionID,
		ExpiresAt: expiresAt,
		BaseURL:   baseURL,
	})
	if err != nil {
		switch {
		case errors.Is(err, webhook.ErrForbidden):
			return nil, 4003, "owner is not a member of the session"
		case errors.Is(err, webhook.ErrInvalidPayload), errors.Is(err, webhook.ErrExpiresInPast):
			return nil, 4001, err.Error()
		case errors.Is(err, webhook.ErrLimitExceeded):
			return nil, 4001, "too many active webhooks for this session; reuse the one you already registered"
		default:
			return nil, 5001, err.Error()
		}
	}
	// CreateEndpoint 的视图不带 session_id，这里补上便于 agent 记录归属。
	item.SessionID = sessionID
	return item, 0, ""
}

// requireAgentInSession 校验 agent 是该会话成员（member_type=2）。
func requireAgentInSession(agentID int64, sessionID string) (int, string) {
	var memberCount int64
	if err := store.DB.Model(&model.SessionMember{}).
		Where("session_id = ? AND member_id = ? AND member_type = 2", sessionID, agentID).
		Count(&memberCount).Error; err != nil {
		return 5001, err.Error()
	}
	if memberCount == 0 {
		return 4003, "agent is not a member of the session"
	}
	return 0, ""
}

// dispatchWebhookList 列出 agent 所在会话下主人名下的 webhook 入口（含完整 URL），供复用而不重复创建。
func dispatchWebhookList(agentID, ownerID int64, params map[string]interface{}) (interface{}, int, string) {
	sessionID, ok := paramString(params, "session_id")
	sessionID = strings.TrimSpace(sessionID)
	if !ok || sessionID == "" {
		return nil, 4001, "session_id required"
	}
	if code, msg := requireAgentInSession(agentID, sessionID); code != 0 {
		return nil, code, msg
	}
	baseURL := webhook.BaseURL()
	if baseURL == "" {
		return nil, 5001, "webhook base url not configured"
	}
	items, err := webhookInvokeService.ListEndpoints(context.Background(), ownerID, sessionID, baseURL)
	if err != nil {
		if errors.Is(err, webhook.ErrForbidden) {
			return nil, 4003, "owner is not a member of the session"
		}
		return nil, 5001, err.Error()
	}
	for i := range items {
		items[i].SessionID = sessionID
	}
	return map[string]interface{}{"items": items}, 0, ""
}

// dispatchWebhookDelete 删除一条入口；入口必须属于主人，且 agent 必须是该入口所在会话的成员。
func dispatchWebhookDelete(agentID, ownerID int64, params map[string]interface{}) (interface{}, int, string) {
	endpointID, ok := paramInt64(params, "id")
	if !ok || endpointID <= 0 {
		return nil, 4001, "id required"
	}
	entity, err := webhookInvokeService.GetEndpoint(context.Background(), ownerID, endpointID)
	if err != nil {
		if errors.Is(err, webhook.ErrNotFound) {
			return nil, 4004, "webhook not found"
		}
		return nil, 5001, err.Error()
	}
	if code, msg := requireAgentInSession(agentID, entity.SessionID); code != 0 {
		return nil, code, msg
	}
	if err := webhookInvokeService.DeleteEndpoint(context.Background(), ownerID, endpointID); err != nil {
		if errors.Is(err, webhook.ErrNotFound) {
			return nil, 4004, "webhook not found"
		}
		return nil, 5001, err.Error()
	}
	return map[string]interface{}{"id": entity.ID, "session_id": entity.SessionID, "deleted": true}, 0, ""
}
