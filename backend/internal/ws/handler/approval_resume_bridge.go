package handler

import (
	"context"
	"encoding/json"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/logger"
	"github.com/askie/grix/backend/internal/store"
	wsagentapi "github.com/askie/grix/backend/internal/ws/agentapi"
	"gorm.io/datatypes"
)

var approvalResumeHub HubInterface

// SetApprovalResumeHub injects the WS hub used to redispatch blocked messages after access approval.
func SetApprovalResumeHub(h HubInterface) {
	approvalResumeHub = h
}

// InitApprovalResumeBridge wires agentapi redispatch callbacks to handler routing.
func InitApprovalResumeBridge(h HubInterface) {
	SetApprovalResumeHub(h)
	wsagentapi.SetAccessRedispatchHandler(func(params wsagentapi.AccessRedispatchParams) {
		RedispatchAccessBlockedMessage(h, params)
	})
}

// RedispatchAccessBlockedMessage re-delivers the first blocked message to a single agent only.
func RedispatchAccessBlockedMessage(hub HubInterface, params wsagentapi.AccessRedispatchParams) {
	if hub == nil || params.AgentID <= 0 || params.TriggerMsgID <= 0 || params.SessionID == "" {
		return
	}
	ctx := context.Background()
	var msg model.Message
	if err := store.DB.Where("msg_id = ? AND session_id = ?", params.TriggerMsgID, params.SessionID).
		First(&msg).Error; err != nil {
		logger.L.Warnf("access approval redispatch skipped: message missing session=%s msg=%d", params.SessionID, params.TriggerMsgID)
		return
	}
	if msg.IsRevoked || msg.IsDeleted {
		logger.L.Warnf("access approval redispatch skipped: message revoked session=%s msg=%d", params.SessionID, params.TriggerMsgID)
		return
	}

	sessionType := int16(1)
	if err := store.DB.Model(&model.Session{}).
		Select("session_type").
		Where("session_id = ?", params.SessionID).
		Scan(&sessionType).Error; err != nil {
		sessionType = 1
	}

	var extraRaw json.RawMessage
	if len(msg.Extra) > 0 {
		extraRaw = json.RawMessage(msg.Extra)
	}
	visibleTo := parseMessageVisibleTo(msg.VisibleTo)

	var semantics *groupDispatchSemantics
	if sessionType == model.SessionTypeGroup {
		resolved, resolveErr := resolvePersistedGroupDispatchSemantics(
			ctx,
			params.SessionID,
			msg.SenderID,
			msg.SenderType,
			msg.MsgID,
			msg.QuotedMessageID,
			msg.Content,
			extraRaw,
		)
		if resolveErr != nil {
			logger.L.Warnf("access approval redispatch semantics failed session=%s msg=%d: %v", params.SessionID, params.TriggerMsgID, resolveErr)
			return
		}
		semantics = &resolved
	}

	route, err := resolveDirectSessionRoute(
		params.SessionID,
		sessionType,
		msg.SenderID,
		msg.SenderType,
		msg.MsgID,
		msg.QuotedMessageID,
		msg.MsgType,
		msg.Content,
		extraRaw,
		semantics,
		visibleTo,
		nil,
		false,
	)
	if err != nil || route == nil {
		return
	}

	filtered := make([]directDispatchTarget, 0, 1)
	for _, target := range route.Targets {
		if target.Agent.ID == params.AgentID {
			target.Mentioned = true
			filtered = append(filtered, target)
			break
		}
	}
	if len(filtered) == 0 {
		logger.L.Warnf("access approval redispatch skipped: agent %d not in route session=%s msg=%d", params.AgentID, params.SessionID, params.TriggerMsgID)
		return
	}
	route.Targets = filtered
	route.MirrorTargets = nil

	dispatchDirectSessionRoute(
		hub,
		ctx,
		params.SessionID,
		sessionType,
		msg.SenderID,
		msg.SenderType,
		msg.MsgID,
		msg.QuotedMessageID,
		msg.MsgType,
		msg.Content,
		extraRaw,
		route,
		false,
	)
}

func parseMessageVisibleTo(raw datatypes.JSON) []int64 {
	if len(raw) == 0 {
		return nil
	}
	var ids []int64
	if json.Unmarshal(raw, &ids) != nil {
		return nil
	}
	return ids
}
