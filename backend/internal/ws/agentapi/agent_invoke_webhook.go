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

	var memberCount int64
	if err := store.DB.Model(&model.SessionMember{}).
		Where("session_id = ? AND member_id = ? AND member_type = 2", sessionID, agentID).
		Count(&memberCount).Error; err != nil {
		return nil, 5001, err.Error()
	}
	if memberCount == 0 {
		return nil, 4003, "agent is not a member of the session"
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
		case errors.Is(err, webhook.ErrInvalidPayload):
			return nil, 4001, err.Error()
		default:
			return nil, 5001, err.Error()
		}
	}
	// CreateEndpoint 的视图不带 session_id，这里补上便于 agent 记录归属。
	item.SessionID = sessionID
	return item, 0, ""
}
