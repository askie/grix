package handler

import (
	"errors"
	"strings"
	"time"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/logger"
	"github.com/askie/grix/backend/internal/security"
	"github.com/askie/grix/backend/internal/store"
	wsprotocol "github.com/askie/grix/backend/internal/ws/protocol"
	"gorm.io/gorm"
)

const visitorToolbarID = "chat-toolbar:visitor:v1"

func loadOwnedWidgetSession(ownerUserID int64, sessionID string) (model.WidgetSession, bool, error) {
	var widgetSession model.WidgetSession
	if ownerUserID <= 0 || strings.TrimSpace(sessionID) == "" {
		return widgetSession, false, nil
	}
	err := store.DB.Where("owner_user_id = ? AND session_id = ?", ownerUserID, strings.TrimSpace(sessionID)).First(&widgetSession).Error
	if err == nil {
		return widgetSession, true, nil
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return widgetSession, false, nil
	}
	return widgetSession, false, err
}

func buildVisitorToolbarSnapshot(widgetSession model.WidgetSession) wsprotocol.AgentToolbarSnapshotPayload {
	isClosed := widgetSession.Status == model.WidgetSessionStatusClosed
	isBanned := widgetSession.Status == model.WidgetSessionStatusBanned
	updatedAt := widgetSession.UpdatedAt.UnixMilli()
	if updatedAt <= 0 {
		updatedAt = time.Now().UTC().UnixMilli()
	}
	return wsprotocol.AgentToolbarSnapshotPayload{
		SessionID: widgetSession.SessionID,
		AgentID:   0,
		ToolbarID: visitorToolbarID,
		Revision:  updatedAt,
		Visible:   true,
		UpdatedAt: updatedAt,
		Items: []wsprotocol.AgentToolbarItemPayload{
			{
				ItemID:      "visitor_profile",
				GroupID:     "visitor",
				Kind:        "button",
				ActionID:    "visitor_profile",
				Label:       "访客信息",
				Icon:        "info",
				Variant:     "ghost",
				LocalAction: "visitor_profile",
			},
			{
				ItemID:      "visitor_close",
				GroupID:     "visitor",
				Kind:        "button",
				ActionID:    "visitor_close",
				Label:       "关闭会话",
				Icon:        "pause",
				Variant:     "warning",
				LocalAction: "visitor_close",
				Disabled:    isClosed || isBanned,
			},
			{
				ItemID:      "visitor_ban",
				GroupID:     "visitor",
				Kind:        "button",
				ActionID:    "visitor_ban",
				Label:       "封禁访客",
				Icon:        "ban",
				Variant:     "danger",
				LocalAction: "visitor_ban",
				Disabled:    isBanned,
			},
		},
	}
}

// visitorNotifyInfo carries info needed to push a notification to the widget visitor.
type visitorNotifyInfo struct {
	VisitorID int64
	SessionID string
	Reason    string
}

func handleVisitorToolbarAction(ownerUserID int64, payload wsprotocol.AgentToolbarActionPayload) (ack wsprotocol.AgentToolbarActionAckPayload, sync *wsprotocol.AgentToolbarSnapshotPayload, notify *visitorNotifyInfo, handled bool) {
	sid := strings.TrimSpace(payload.SessionID)
	widgetSession, ok, err := loadOwnedWidgetSession(ownerUserID, sid)
	if err != nil {
		return wsprotocol.AgentToolbarActionAckPayload{
			SessionID:      sid,
			ToolbarID:      payload.ToolbarID,
			ClientActionID: payload.ClientActionID,
			Accepted:       false,
			Code:           "internal_error",
			Msg:            err.Error(),
			UpdatedAt:      nowUnixMilli(),
		}, nil, nil, true
	}
	if !ok {
		return wsprotocol.AgentToolbarActionAckPayload{}, nil, nil, false
	}

	ack = wsprotocol.AgentToolbarActionAckPayload{
		SessionID:      sid,
		ToolbarID:      visitorToolbarID,
		ClientActionID: payload.ClientActionID,
		Accepted:       true,
		UpdatedAt:      nowUnixMilli(),
	}

	if strings.TrimSpace(payload.ToolbarID) != "" && strings.TrimSpace(payload.ToolbarID) != visitorToolbarID {
		ack.Accepted = false
		ack.Code = "toolbar_mismatch"
		ack.Msg = "visitor toolbar mismatch"
		return ack, nil, nil, true
	}

	actionID := strings.ToLower(strings.TrimSpace(payload.ActionID))
	switch actionID {
	case "visitor_profile":
		snapshot := buildVisitorToolbarSnapshot(widgetSession)
		return ack, &snapshot, nil, true
	case "visitor_close":
		if widgetSession.Status == model.WidgetSessionStatusBanned || widgetSession.Status == model.WidgetSessionStatusClosed {
			snapshot := buildVisitorToolbarSnapshot(widgetSession)
			return ack, &snapshot, nil, true
		}
		now := time.Now().UTC()
		if err := store.DB.Model(&model.WidgetSession{}).Where("id = ?", widgetSession.ID).Updates(map[string]interface{}{
			"status":     model.WidgetSessionStatusClosed,
			"updated_at": now,
		}).Error; err != nil {
			ack.Accepted = false
			ack.Code = "action_failed"
			ack.Msg = err.Error()
			return ack, nil, nil, true
		}
		widgetSession.Status = model.WidgetSessionStatusClosed
		widgetSession.UpdatedAt = now
		snapshot := buildVisitorToolbarSnapshot(widgetSession)
		ack.UpdatedAt = snapshot.UpdatedAt
		return ack, &snapshot, &visitorNotifyInfo{
			VisitorID: widgetSession.VisitorID,
			SessionID: widgetSession.SessionID,
			Reason:    "closed",
		}, true
	case "visitor_ban":
		if widgetSession.Status == model.WidgetSessionStatusBanned {
			snapshot := buildVisitorToolbarSnapshot(widgetSession)
			return ack, &snapshot, nil, true
		}
		now := time.Now().UTC()
		if err := store.DB.Model(&model.WidgetSession{}).Where("id = ?", widgetSession.ID).Updates(map[string]interface{}{
			"status":     model.WidgetSessionStatusBanned,
			"updated_at": now,
		}).Error; err != nil {
			ack.Accepted = false
			ack.Code = "action_failed"
			ack.Msg = err.Error()
			return ack, nil, nil, true
		}
		widgetSession.Status = model.WidgetSessionStatusBanned
		widgetSession.UpdatedAt = now
		// 附带把该访客会话最近 init IP 加入 owner 维度的 IP 封禁（默认 7 天）；
		// 与 visitor_key 封禁相互独立。LastInitIP 为空时静默跳过，
		// 写封禁失败只告警，不影响已完成的会话封禁。
		if err := security.BanWidgetIP(ownerUserID, widgetSession.LastInitIP, "session_ban", widgetSession.SessionID, security.WidgetIPBanDefaultTTL); err != nil {
			logger.L.Warnf("visitor ban: write ip ban failed owner=%d session=%s err=%v", ownerUserID, widgetSession.SessionID, err)
		}
		snapshot := buildVisitorToolbarSnapshot(widgetSession)
		ack.UpdatedAt = snapshot.UpdatedAt
		return ack, &snapshot, &visitorNotifyInfo{
			VisitorID: widgetSession.VisitorID,
			SessionID: widgetSession.SessionID,
			Reason:    "banned",
		}, true
	default:
		ack.Accepted = false
		ack.Code = "invalid_action"
		ack.Msg = "unsupported visitor toolbar action"
		return ack, nil, nil, true
	}
}
