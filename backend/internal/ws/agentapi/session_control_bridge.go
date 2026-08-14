package agentapi

import (
	"context"
	"fmt"
	"strings"

	tooli18n "github.com/askie/grix/backend/internal/agenttoolbar/i18n"
	"github.com/askie/grix/backend/internal/grixactions"
	"github.com/askie/grix/backend/internal/pkg/logger"
	"github.com/askie/grix/backend/internal/ws/protocol"
)

type sessionControlBridgeConfig struct {
	actionType string
	usage      string
	logLabel   string
}

func (m *Manager) handleSessionControlCommand(evt DelegateEventPayload, config sessionControlBridgeConfig) bool {
	parsed, matched, err := grixactions.ParseSessionControlCommand(evt.Content)
	if !matched {
		return false
	}

	actionID := fmt.Sprintf("%s:%s:%d", config.actionType, strings.TrimSpace(evt.EventID), evt.MsgID)
	if strings.TrimSpace(evt.EventID) == "" {
		actionID = fmt.Sprintf("%s:%s:%d:%d", config.actionType, evt.SessionID, evt.AgentID, evt.MsgID)
	}

	params := map[string]any{
		"session_id": strings.TrimSpace(evt.SessionID),
		"verb":       strings.TrimSpace(parsed.Verb),
	}
	if cwd := strings.TrimSpace(parsed.Cwd); cwd != "" {
		params["cwd"] = cwd
	}
	cardInstanceID := strings.TrimSpace(parsed.CardInstanceID)
	if cardInstanceID == "" {
		cardInstanceID = loadOpenSessionCardInstanceID(
			context.Background(),
			evt.SessionID,
			evt.QuotedMessageID,
		)
	}

	pending := &pendingLocalAction{
		actionID:         actionID,
		kind:             config.actionType,
		agentID:          evt.AgentID,
		ownerID:          evt.OwnerID,
		sessionID:        evt.SessionID,
		threadID:         evt.ThreadID,
		quotedMessageID:  evt.MsgID,
		actionType:       config.actionType,
		referenceID:      parsed.Verb,
		cardInstanceID:   cardInstanceID,
		submittedPath:    strings.TrimSpace(parsed.Cwd),
		bindingCardMsgID: loadBindingCardMsgID(context.Background(), evt.AgentID, evt.SessionID),
	}

	// Usage error (e.g. /grix open without cwd): reply inline instead of
	// sending a local_action that will fail for the same reason.
	if err != nil {
		// 命令来自 delegate 事件，evt.OwnerID 即发起者，按其连接做能力检查与下发。
		if m.canSendSessionControlCheck(evt.AgentID, evt.OwnerID, config.actionType) {
			return m.sendPendingLocalActionReply(*pending, pendingLocalActionReply{content: config.usage})
		}
		return false
	}

	logger.L.Infof(
		"%s session control intercepted session=%s owner=%d agent=%d msg_id=%d verb=%s",
		config.logLabel,
		evt.SessionID,
		evt.OwnerID,
		evt.AgentID,
		evt.MsgID,
		parsed.Verb,
	)

	// sendLocalActionWithPendingForOwner handles both local and cross-node dispatch:
	// it tries local delivery first, then falls back to Redis forwarding to
	// the node where the agent is actually connected。按发起者 owner 路由到对应连接
	// （agent 共享多连接物理隔离）。
	if m.sendLocalActionWithPendingForOwner(evt.AgentID, evt.OwnerID, protocol.LocalActionPayload{
		ActionID:   actionID,
		EventID:    evt.EventID,
		ActionType: config.actionType,
		Params:     params,
		TimeoutMs:  15_000,
	}, pending) {
		return true
	}

	// Agent unreachable on any node. For open verbs, update the binding card
	// with an unsupported notice so it does not stay in pending forever.
	if strings.TrimSpace(parsed.Verb) == "open" {
		m.notifyBindingCardUnsupported(evt)
	}
	return false
}

func (m *Manager) notifyBindingCardUnsupported(evt DelegateEventPayload) {
	bindingCardMsgID := loadBindingCardMsgID(context.Background(), evt.AgentID, evt.SessionID)
	if bindingCardMsgID <= 0 {
		return
	}
	lang := ownerCardLanguage(evt.OwnerID)
	cardPayload := map[string]any{
		"category":     "session",
		"status":       "warning",
		"summary":      tooli18n.T(lang, "bind_unsupported"),
		"detail_text":  tooli18n.T(lang, "bind_unsupported_detail"),
		"reference_id": evt.SessionID,
	}
	if cardInstanceID := loadOpenSessionCardInstanceID(context.Background(), evt.SessionID, bindingCardMsgID); cardInstanceID != "" {
		cardPayload["card_instance_id"] = cardInstanceID
	}
	reply := buildAgentStatusCardReply(cardPayload)
	if strings.TrimSpace(reply.content) == "" {
		return
	}
	pending := pendingLocalAction{
		agentID:          evt.AgentID,
		ownerID:          evt.OwnerID,
		sessionID:        evt.SessionID,
		threadID:         evt.ThreadID,
		quotedMessageID:  evt.MsgID,
		cardInstanceID:   strings.TrimSpace(fmt.Sprint(cardPayload["card_instance_id"])),
		bindingCardMsgID: bindingCardMsgID,
	}
	if ok := m.sendOrUpdateBindingCardReply(pending, reply); ok {
		logger.L.Infof("binding card updated with unsupported notice agent=%d session=%s msg_id=%d",
			evt.AgentID, evt.SessionID, bindingCardMsgID)
	}
}

// canSendSessionControlCheck 按发起者 owner 选连接检查能力（agent 共享多连接物理隔离）。
func (m *Manager) canSendSessionControlCheck(agentID, ownerID int64, actionType string) bool {
	if m == nil || m.sendFn == nil {
		return false
	}
	conn := m.lookupConnForOwner(agentID, ownerID)
	if conn == nil {
		return false
	}
	return hasDeclaredName(conn.capabilities, "local_action_v1") &&
		hasDeclaredName(conn.localActions, actionType)
}
