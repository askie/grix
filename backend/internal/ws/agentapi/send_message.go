package agentapi

import (
	"context"
	"errors"
)

func (m *Manager) SendMessage(ctx context.Context, req SendMessageReq) (*SendMessageResult, error) {
	if m == nil || m.sendFn == nil {
		return nil, errors.New("agent api send message unavailable")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	result, err := m.sendFn(ctx, req)
	if err != nil || result == nil || result.MsgID <= 0 {
		return result, err
	}
	// invoke send_msg (grix_message_send) and other SendMessage callers persist
	// the card without going through handleSendMsg. That packet handler is the
	// only place that used to publish the offline approval push and Live
	// Activity, so a card sent here reached the chat and then stayed silent.
	m.noteSentApprovalCard(req, result)
	return result, nil
}

// noteSentApprovalCard records an exec_approval card and tells the owner.
// Plain messages and other card types are ignored. handleSendMsg does this
// itself and does not call SendMessage, so the two paths do not double-publish.
func (m *Manager) noteSentApprovalCard(req SendMessageReq, result *SendMessageResult) {
	if m == nil || result == nil || result.MsgID <= 0 {
		return
	}
	approvalID := extractApprovalIDFromCard(req.Content, req.Extra)
	if approvalID == "" {
		return
	}
	saveApprovalCardMsgIDWithType(
		context.Background(),
		req.AgentID,
		req.SessionID,
		approvalID,
		result.MsgID,
		extractApprovalCardType(req.Extra),
	)
	m.publishApprovalNotification(
		req.OwnerID,
		req.AgentID,
		req.SessionID,
		approvalPushSummary(req.Content, req.Extra),
		approvalID,
	)
}

func SendMessage(ctx context.Context, req SendMessageReq) (*SendMessageResult, error) {
	globalMu.RLock()
	manager := globalManager
	globalMu.RUnlock()
	if manager == nil {
		return nil, errors.New("agent api manager unavailable")
	}
	return manager.SendMessage(ctx, req)
}
