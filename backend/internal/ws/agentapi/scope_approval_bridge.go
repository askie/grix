package agentapi

// scope 审批桥：agent 调用平台动作缺 scope 时，向主人发标准 agent_question 审批卡；
// 主人批准后追加 scope 并按共享 resume 路径唤醒 agent 续跑。

import (
	"context"
	"fmt"
	"strings"

	tooli18n "github.com/askie/grix/backend/internal/agenttoolbar/i18n"
	"github.com/askie/grix/backend/internal/api/service"
	"github.com/askie/grix/backend/internal/grixactions"
	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/agentscope"
	"github.com/askie/grix/backend/internal/pkg/logger"
	"github.com/askie/grix/backend/internal/store"
)

func (m *Manager) maybeNotifyScopeApproval(agentID, ownerID int64, scope string) string {
	scope = strings.TrimSpace(scope)
	if m == nil || m.sendFn == nil || agentID <= 0 || ownerID <= 0 || scope == "" {
		return ""
	}
	if _, err := agentscope.Normalize([]string{scope}); err != nil {
		return ""
	}
	ctx := context.Background()
	if scopeApprovalDenied(ctx, agentID, scope) {
		return ""
	}
	if _, pending := loadScopeApprovalPending(ctx, agentID, scope); pending {
		return missingScopeApprovalMessage(scope)
	}
	if store.RDB != nil {
		if count, err := store.RDB.Get(ctx, scopeApprovalCountKey(agentID)).Int(); err == nil && count >= maxPendingScopeApprovalsPerAgent {
			return ""
		}
	}
	resume := m.captureScopeResumeContext(agentID)
	sent, pendingCreatedAt, err := saveScopeApprovalPending(ctx, agentID, scope, resume)
	if err != nil {
		logger.L.Warnf("scope approval state failed agent=%d scope=%s: %v", agentID, scope, err)
		return ""
	}
	if !sent {
		return ""
	}
	if !m.sendScopeApprovalCard(agentID, ownerID, scope, resume, pendingCreatedAt) {
		clearScopeApprovalPending(ctx, agentID, scope)
		return ""
	}
	return missingScopeApprovalMessage(scope)
}

func missingScopeApprovalMessage(scope string) string {
	return fmt.Sprintf(
		"Missing scope %s; an approval request was sent to the owner. Do not retry now — after approval you will be notified in this session to continue the interrupted action.",
		strings.TrimSpace(scope),
	)
}

func (m *Manager) sendScopeApprovalCard(agentID, ownerID int64, scope string, resume scopeResumeContext, pendingCreatedAt int64) bool {
	lang := ownerCardLanguage(ownerID)
	label := agentscopeScopeLabel(lang, scope)
	desc := scopeDescription(lang, scope)
	sessionHint := scopeApprovalSessionHint(lang, resume)
	triggerHint := scopeApprovalTriggerHint(lang, resume)

	resp, err := service.SessionCreateForAgentBinding(ownerID, agentID, accessApprovalThreadKey, tooli18n.T(lang, "access_thread_title"))
	if err != nil || resp == nil || strings.TrimSpace(resp.SessionID) == "" {
		logger.L.Warnf("scope approval thread resolve failed agent=%d owner=%d: %v", agentID, ownerID, err)
		return false
	}

	requestID := fmt.Sprintf("%s%d:%s", scopeApprovalRequestPrefix, agentID, scope)
	messageParts := []string{tooli18n.Tf(lang, "scope_request_message", label, desc)}
	if triggerHint != "" {
		messageParts = append(messageParts, triggerHint)
	}
	if sessionHint != "" {
		messageParts = append(messageParts, sessionHint)
	}
	payload := map[string]any{
		"request_id":  requestID,
		"mode":        "form",
		"message":     strings.Join(messageParts, "\n"),
		"footer_text": tooli18n.T(lang, "scope_request_footer"),
		"questions": []map[string]any{
			{
				"index":   1,
				"header":  tooli18n.T(lang, "scope_request_header"),
				"prompt":  tooli18n.Tf(lang, "scope_request_prompt", label),
				"options": []string{tooli18n.T(lang, "access_option_allow"), tooli18n.T(lang, "access_option_deny")},
			},
		},
	}
	content := buildLocalGrixCardLink("[Agent Question] "+tooli18n.T(lang, "scope_request_header"), "agent_question", payload)
	if _, err := m.sendFn(context.Background(), SendMessageReq{
		AgentID:     agentID,
		OwnerID:     ownerID,
		SessionID:   strings.TrimSpace(resp.SessionID),
		ClientMsgID: fmt.Sprintf("agent_scope_approval_%d_%s_%d", agentID, strings.TrimSpace(scope), pendingCreatedAt),
		MsgType:     1,
		Content:     content,
	}); err != nil {
		logger.L.Warnf("scope approval card send failed agent=%d owner=%d scope=%s: %v", agentID, ownerID, scope, err)
		return false
	}
	return true
}

func scopeDescription(lang, scope string) string {
	for _, item := range agentscope.AllowedScopeItems(lang) {
		if item.Scope == scope && strings.TrimSpace(item.Description) != "" {
			return item.Description
		}
	}
	return scope
}

func scopeApprovalSessionHint(lang string, resume scopeResumeContext) string {
	if !resume.CanWake || strings.TrimSpace(resume.SessionID) == "" {
		return ""
	}
	if resume.SessionType == model.SessionTypeGroup {
		return tooli18n.Tf(lang, "scope_request_session_group", accessApprovalGroupLabel(lang, resume.SessionID))
	}
	return tooli18n.T(lang, "scope_request_session_private")
}

func scopeApprovalTriggerHint(lang string, resume scopeResumeContext) string {
	if !resume.CanWake || resume.SenderID <= 0 {
		return ""
	}
	return tooli18n.Tf(lang, "scope_request_trigger", accessApprovalSenderLabel(lang, resume.SenderID))
}

func (m *Manager) tryHandleScopeApprovalReply(evt DelegateEventPayload) bool {
	reply, matched, err := grixactions.ParseQuestionReply(evt.Content)
	if !matched {
		return false
	}
	if err != nil {
		if strings.Contains(evt.Content, "scope%3A") || strings.Contains(evt.Content, `"scope:`) || strings.Contains(evt.Content, "request_id=scope:") {
			return m.sendScopeApprovalStatusCard(evt, "warning", tooli18n.T(ownerCardLanguage(evt.OwnerID), "scope_reply_unparseable"), "")
		}
		return false
	}
	requestID := strings.TrimSpace(reply.RequestID)
	if !strings.HasPrefix(requestID, scopeApprovalRequestPrefix) {
		return false
	}

	var agentID int64
	scope := strings.TrimSpace(strings.TrimPrefix(requestID, scopeApprovalRequestPrefix))
	if idx := strings.IndexByte(scope, ':'); idx > 0 {
		fmt.Sscanf(scope[:idx], "%d", &agentID)
		scope = strings.TrimSpace(scope[idx+1:])
	}
	lang := ownerCardLanguage(evt.OwnerID)
	if agentID <= 0 || scope == "" || agentID != evt.AgentID {
		return m.sendScopeApprovalStatusCard(evt, "warning", tooli18n.T(lang, "scope_request_unrecognized"), requestID)
	}

	var agent model.Agent
	if dbErr := store.DB.Select("id,owner_id").First(&agent, agentID).Error; dbErr != nil || agent.OwnerID != evt.SenderID {
		return m.sendScopeApprovalStatusCard(evt, "warning", tooli18n.T(lang, "access_owner_only"), requestID)
	}

	if strings.TrimSpace(reply.Action) == "cancel" {
		return m.sendScopeApprovalStatusCard(evt, "info", tooli18n.T(lang, "access_cancelled"), requestID)
	}

	decision := ""
	for _, answer := range questionReplyAnswers(reply.Response) {
		switch strings.TrimSpace(answer) {
		case tooli18n.T("zh", "access_option_allow"), tooli18n.T("en", "access_option_allow"):
			decision = "allow"
		case tooli18n.T("zh", "access_option_deny"), tooli18n.T("en", "access_option_deny"):
			decision = "deny"
		}
		if decision != "" {
			break
		}
	}

	ctx := context.Background()
	switch decision {
	case "allow":
		if !agentscope.IsAllowed(scope) {
			return m.sendScopeApprovalStatusCard(evt, "warning", tooli18n.T(lang, "scope_not_allowed"), requestID)
		}
		pending, _ := consumeScopeApprovalPending(ctx, agentID, scope)
		resume := scopeResumeContext{}
		if pending != nil {
			resume = scopeResumeContext{
				SessionID:   pending.SessionID,
				OwnerID:     pending.OwnerID,
				SenderID:    pending.SenderID,
				SessionType: pending.SessionType,
				CanWake:     pending.CanWake,
			}
		}
		summary := m.resumeAfterScopeApproval(agentID, agent.OwnerID, scope, resume, lang)
		return m.sendScopeApprovalStatusCard(evt, "success", summary, requestID)
	case "deny":
		markScopeApprovalDenied(ctx, agentID, scope)
		return m.sendScopeApprovalStatusCard(evt, "success", tooli18n.T(lang, "scope_denied"), requestID)
	default:
		return m.sendScopeApprovalStatusCard(evt, "warning", tooli18n.T(lang, "access_choose_option"), requestID)
	}
}

func (m *Manager) sendScopeApprovalStatusCard(evt DelegateEventPayload, status, summary, requestID string) bool {
	if m.sendFn == nil {
		return true
	}
	reply := buildAgentStatusCardReply(map[string]any{
		"category":     "scope",
		"status":       status,
		"summary":      summary,
		"reference_id": requestID,
	})
	if strings.TrimSpace(reply.content) == "" {
		return true
	}
	if _, err := m.sendFn(context.Background(), SendMessageReq{
		AgentID:         evt.AgentID,
		OwnerID:         evt.OwnerID,
		SessionID:       evt.SessionID,
		ClientMsgID:     fmt.Sprintf("agent_scope_approval_result_%d_%d", evt.AgentID, evt.MsgID),
		MsgType:         1,
		Content:         reply.content,
		QuotedMessageID: evt.MsgID,
	}); err != nil {
		logger.L.Warnf("scope approval status card send failed agent=%d session=%s: %v", evt.AgentID, evt.SessionID, err)
	}
	return true
}
