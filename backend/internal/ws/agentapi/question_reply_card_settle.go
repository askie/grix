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

// settleAgentQuestionReplyCard 收尾「文本事件回包」模式的 agent_question 问答卡。
//
// 非 claude 家族的 connector（如 opencode）不经过 claude_interaction_reply
// local_action：服务端把 grix://card/agent_question_reply 原样作为普通 event_msg
// 投给 connector，由 connector 消费后回 event_result。这条路径此前没有任何代码
// 更新卡片，App 端永远等不到 agent_status 卡，卡片一直停在「提交中」。
// 这里在 event_result 终态结算时把原问答卡原地编辑为 agent_status 结果卡
// （与 local_action 回包路径同一渲染约定），编辑失败或无卡片映射时降级为新发
// 状态卡消息（client_msg_id 由 event_id 派生，重复 event_result 不刷屏）。
func (m *Manager) settleAgentQuestionReplyCard(evt DelegateEventPayload, payload EventResultPayload) error {
	reply, matched, err := grixactions.ParseQuestionReply(evt.Content)
	if !matched || err != nil {
		return nil
	}
	requestID := strings.TrimSpace(reply.RequestID)
	if requestID == "" {
		return nil
	}

	lang := ownerCardLanguage(evt.OwnerID)
	cardPayload := map[string]any{
		"category":     "question",
		"reference_id": requestID,
	}
	switch strings.TrimSpace(payload.Status) {
	case protocol.AgentEventResultResponded:
		cardPayload["status"] = "success"
		cardPayload["summary"] = tooli18n.T(lang, "reply_recorded")
	default:
		cardPayload["status"] = "error"
		cardPayload["summary"] = interactionReplyFailureSummary(lang, "question")
		cardPayload["detail_text"] = firstNonEmpty(
			strings.TrimSpace(payload.Msg),
			interactionReplyErrorMessage(lang, payload.Code),
		)
	}
	if strings.TrimSpace(fmt.Sprint(cardPayload["detail_text"])) == "" {
		delete(cardPayload, "detail_text")
	}
	statusReply := buildAgentStatusCardReply(cardPayload)
	if strings.TrimSpace(statusReply.content) == "" {
		return nil
	}

	ctx := context.Background()
	cardMsgID := loadApprovalCardMsgID(ctx, evt.AgentID, evt.SessionID, requestID)
	if cardMsgID > 0 && m.editMsgFn != nil {
		if err := m.editMsgFn(ctx, evt.AgentID, evt.OwnerID, EditMsgPayload{
			SessionID: evt.SessionID,
			MsgID:     cardMsgID,
			Content:   statusReply.content,
			Extra:     statusReply.extra,
		}); err == nil {
			deleteApprovalCardMsgID(ctx, evt.AgentID, evt.SessionID, requestID)
			return nil
		} else {
			logger.L.Warnf("edit question card failed, falling back to new message: agent=%d session=%s msg_id=%d request=%s err=%v",
				evt.AgentID, evt.SessionID, cardMsgID, requestID, err)
		}
	}
	if !m.sendPendingLocalActionReply(pendingLocalAction{
		actionID:        "question_reply_event:" + strings.TrimSpace(evt.EventID),
		agentID:         evt.AgentID,
		ownerID:         evt.OwnerID,
		sessionID:       evt.SessionID,
		threadID:        evt.ThreadID,
		quotedMessageID: evt.MsgID,
	}, statusReply) {
		return fmt.Errorf("send question reply status card failed")
	}
	return nil
}

// markQuestionReplyForwarded 在问答回卡被降级改写为普通文本投递（连接不可达）时，
// 立即把原问答卡原地编辑为「已转发」状态卡，避免卡片停在「提交中」。
// 与 local_action 超时后 deliverQuestionReplyFallback 的 reply_forwarded 文案同款。
func (m *Manager) markQuestionReplyForwarded(evt DelegateEventPayload, requestID string) {
	requestID = strings.TrimSpace(requestID)
	if requestID == "" {
		return
	}
	ctx := context.Background()
	cardMsgID := loadApprovalCardMsgID(ctx, evt.AgentID, evt.SessionID, requestID)
	if cardMsgID <= 0 || m.editMsgFn == nil {
		return
	}
	lang := ownerCardLanguage(evt.OwnerID)
	statusReply := buildAgentStatusCardReply(map[string]any{
		"category":     "question",
		"reference_id": requestID,
		"status":       "warning",
		"summary":      tooli18n.T(lang, "reply_forwarded"),
		"detail_text":  tooli18n.T(lang, "reply_forwarded_detail"),
	})
	if strings.TrimSpace(statusReply.content) == "" {
		return
	}
	if err := m.editMsgFn(ctx, evt.AgentID, evt.OwnerID, EditMsgPayload{
		SessionID: evt.SessionID,
		MsgID:     cardMsgID,
		Content:   statusReply.content,
		Extra:     statusReply.extra,
	}); err != nil {
		logger.L.Warnf("mark question reply forwarded edit failed: agent=%d session=%s msg_id=%d request=%s err=%v",
			evt.AgentID, evt.SessionID, cardMsgID, requestID, err)
		return
	}
	deleteApprovalCardMsgID(ctx, evt.AgentID, evt.SessionID, requestID)
}
