package agentapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/askie/grix/backend/internal/agentadapter"
	"github.com/askie/grix/backend/internal/agentsync"
	agenttoolbar "github.com/askie/grix/backend/internal/agenttoolbar"
	tooli18n "github.com/askie/grix/backend/internal/agenttoolbar/i18n"
	toolresolver "github.com/askie/grix/backend/internal/agenttoolbar/resolver"
	toolstore "github.com/askie/grix/backend/internal/agenttoolbar/store"
	"github.com/askie/grix/backend/internal/api/service"
	"github.com/askie/grix/backend/internal/gateway/provisioning"
	"github.com/askie/grix/backend/internal/geminisession"
	"github.com/askie/grix/backend/internal/pkg/logger"
	"github.com/askie/grix/backend/internal/pkg/snowflake"
	"github.com/askie/grix/backend/internal/ws/protocol"
)

// pendingLocalAction tracks a local_action sent to a plugin, waiting for result.
type pendingLocalAction struct {
	actionID             string
	kind                 string
	agentID              int64
	ownerID              int64
	sessionID            string
	threadID             string
	quotedMessageID      int64
	actionType           string
	decision             string
	approvalCommandID    string
	approvalID           string
	referenceID          string
	cardInstanceID       string
	displayLabel         string
	submittedPath        string
	timeoutMs            int                                      // per-action timeout in ms; 0 means use the global default
	bindingCardMsgID     int64                                    // msg_id of the original agent_open_session card for in-place editing
	approvalCardMsgID    int64                                    // msg_id of the original approval/interaction card for in-place editing
	geminiCleanup        bool                                     // if true, delete gemini pending workspace after successful edit
	timeoutTimer         *trackedTimer                            // fires when the plugin does not respond in time
	fileListResultCh     chan<- *fileListResponse                 // used by file_list to deliver result synchronously
	createFolderResultCh chan<- *createFolderResponse             // used by create_folder to deliver result synchronously
	sessionListResultCh  chan<- *sessionListResponse              // used by list_sessions to deliver result synchronously
	sessionBindResultCh  chan<- *sessionBindResponse              // used by session bind/import to deliver result synchronously
	sessionSyncResultCh  chan<- *SessionHistorySyncResponse       // used by connector-native history sync to deliver result synchronously
	forwardedResultCh    chan<- protocol.LocalActionResultPayload // used by forwarded local_actions to deliver raw result
	skillUploadResultCh  chan<- *skillUploadResponse              // used by skill_upload to deliver result synchronously
	skillLibraryResultCh chan<- *skillLibraryActionResponse       // used by skill_enable/skill_disable to deliver result synchronously
	fallbackEvent        *DelegateEventPayload                    // 问答回卡无人接收(not_pending)时降级为普通消息重投的事件，内容已改写为可读文本
	fallbackDelivered    bool                                     // 降级重投是否已成功，用于结果卡区分"已转发"与"失败"
}

type pendingLocalActionReply struct {
	content string
	extra   json.RawMessage
}

// handleLocalActionResult processes local_action_result packets from the plugin.
func (m *Manager) handleLocalActionResult(conn *agentConn, pkt *protocol.Packet) {
	var payload protocol.LocalActionResultPayload
	if err := json.Unmarshal(pkt.Payload, &payload); err != nil {
		conn.sendPayload("error", pkt.Seq, SendNackPayload{
			Code: 4001,
			Msg:  "invalid local_action_result payload",
		})
		return
	}

	actionID := strings.TrimSpace(payload.ActionID)
	if actionID == "" {
		conn.sendPayload("error", pkt.Seq, SendNackPayload{
			Code: 4001,
			Msg:  "action_id required",
		})
		return
	}

	status := strings.TrimSpace(payload.Status)
	switch status {
	case "ok", "failed", "unsupported":
		// valid
	case "timeout":
		if isHermesConn(conn) {
			conn.sendPayload("error", pkt.Seq, SendNackPayload{
				Code: 4001,
				Msg:  "invalid status, expected ok|failed|unsupported",
			})
			return
		}
		// valid
	default:
		conn.sendPayload("error", pkt.Seq, SendNackPayload{
			Code: 4001,
			Msg:  "invalid status, expected ok|failed|unsupported|timeout",
		})
		return
	}

	logger.L.Infof(
		"local_action_result agent=%d action_id=%s status=%s",
		conn.agentID, actionID, status,
	)
	if pending := m.takePendingLocalAction(actionID); pending != nil {
		m.handlePendingLocalActionResult(conn, pending, payload)
	}

	if !isHermesConn(conn) {
		conn.sendPayload("local_action_ack", pkt.Seq, map[string]interface{}{
			"action_id": actionID,
			"received":  true,
		})
	}
}

func (m *Manager) handlePendingLocalActionResult(conn *agentConn, pending *pendingLocalAction, payload protocol.LocalActionResultPayload) {
	if pending == nil {
		return
	}
	switch pending.kind {
	case "exec_approval":
		replyText := buildExecApprovalResultReply(pending, payload)
		if strings.TrimSpace(replyText.content) == "" {
			return
		}
		if ok := m.sendOrUpdateApprovalCardReply(*pending, replyText); !ok {
			logger.L.Warnf(
				"local_action reply send failed agent=%d owner=%d action_id=%s status=%s",
				conn.agentID,
				pending.ownerID,
				pending.actionID,
				payload.Status,
			)
		}
	case "claude_interaction_reply_permission", "claude_interaction_reply_elicitation":
		// 无人等待的问答回卡（超时/进程重启/冷启动后迟到）：把用户答案降级成
		// 普通消息重投给 agent，保证内容不丢；卡片同时置为过期态。
		m.deliverQuestionReplyFallback(pending, payload)
		replyText := buildClaudeInteractionReplyResultReply(pending, payload)
		if strings.TrimSpace(replyText.content) == "" {
			return
		}
		if ok := m.sendOrUpdateApprovalCardReply(*pending, replyText); !ok {
			logger.L.Warnf(
				"local_action question reply send failed agent=%d owner=%d action_id=%s status=%s",
				conn.agentID,
				pending.ownerID,
				pending.actionID,
				payload.Status,
			)
		}
	case "session_control":
		m.persistToolbarBinding(conn, pending, payload)
		config := inferProviderReplyConfig(conn.adapterID)
		replyText := buildSessionControlResultReply(pending, payload, config)
		if strings.TrimSpace(replyText.content) == "" {
			break
		}
		replyPending := *pending
		if replyPending.bindingCardMsgID <= 0 {
			// 快捷绑定（无卡片提交）时 pending 建立早于插件上报 update_binding_card，
			// 快照里没有绑定卡消息号；此处重读一次，命中则原地编辑绑定卡，
			// 避免同一次绑定出现"绑定状态卡 + 回复卡"两张重复卡片。
			replyPending.bindingCardMsgID = loadBindingCardMsgID(context.Background(), replyPending.agentID, replyPending.sessionID)
		}
		if ok := m.sendOrUpdateBindingCardReply(replyPending, replyText); !ok {
			logger.L.Warnf(
				"local_action session control reply send failed agent=%d owner=%d action_id=%s status=%s",
				conn.agentID,
				pending.ownerID,
				pending.actionID,
				payload.Status,
			)
		}
	case "set_model", "set_mode", "set_provider", "set_preset", "set_reasoning_effort", "set_service_tier", "set_sandbox_mode":
		m.persistToolbarBinding(conn, pending, payload)
		if isGeminiToolbarSelectionAction(conn, pending) {
			m.persistGeminiToolbarSelection(pending, payload)
			replyText := buildGeminiToolbarSelectionResultReply(pending, payload)
			if strings.TrimSpace(replyText.content) == "" {
				break
			}
			if ok := m.sendOrUpdateBindingCardReply(*pending, replyText); !ok {
				logger.L.Warnf(
					"local_action toolbar selection reply send failed agent=%d owner=%d action_id=%s status=%s",
					conn.agentID,
					pending.ownerID,
					pending.actionID,
					payload.Status,
				)
			}
		} else {
			replyText := buildToolbarSelectionResultReply(pending, payload)
			if strings.TrimSpace(replyText.content) == "" {
				break
			}
			if ok := m.sendOrUpdateBindingCardReply(*pending, replyText); !ok {
				logger.L.Warnf(
					"local_action toolbar selection reply send failed agent=%d owner=%d action_id=%s status=%s",
					conn.agentID,
					pending.ownerID,
					pending.actionID,
					payload.Status,
				)
			}
		}
	case "get_context":
		m.persistToolbarBinding(conn, pending, payload)
	case "file_list":
		m.handleFileListPendingResult(pending, payload)
	case "list_sessions":
		m.handleSessionListPendingResult(pending, payload)
	case "session_bind":
		m.handleSessionBindPendingResult(pending, payload)
	case "session_history_sync":
		m.handleSessionHistorySyncPendingResult(pending, payload)
	case "create_folder":
		m.handleCreateFolderPendingResult(pending, payload)
	case "permission_approval":
		replyText := buildExecApprovalResultReply(pending, payload)
		if strings.TrimSpace(replyText.content) == "" {
			return
		}
		if ok := m.sendOrUpdateApprovalCardReply(*pending, replyText); !ok {
			logger.L.Warnf(
				"permission approval reply send failed agent=%d owner=%d action_id=%s status=%s",
				conn.agentID,
				pending.ownerID,
				pending.actionID,
				payload.Status,
			)
		}
	case "turn_interrupt":
		logger.L.Infof(
			"local_action turn_interrupt completed agent=%d action_id=%s status=%s",
			conn.agentID, pending.actionID, payload.Status,
		)
	case "thread_compact":
		reply := buildThreadCompactResultReply(pending, payload)
		if strings.TrimSpace(reply.content) != "" {
			if ok := m.sendOrUpdateBindingCardReply(*pending, reply); !ok {
				logger.L.Warnf(
					"thread_compact reply send failed agent=%d owner=%d action_id=%s status=%s",
					conn.agentID, pending.ownerID, pending.actionID, payload.Status,
				)
			}
		}
		if svc := agenttoolbar.GetGlobal(); svc != nil && pending.ownerID > 0 && strings.TrimSpace(pending.sessionID) != "" {
			_ = svc.RefreshSession(context.Background(), pending.ownerID, pending.sessionID, "thread_compact_result")
		}
	case "get_session_usage":
		logger.L.Infof("[toolbar-local-action] get_session_usage result: action_id=%s pending_session_id=%s status=%s agent=%d owner=%d", pending.actionID, pending.sessionID, payload.Status, conn.agentID, pending.ownerID)
		reply := buildSessionUsageResultReply(pending, payload)
		if strings.TrimSpace(reply.content) != "" {
			if ok := m.sendOrUpdateBindingCardReply(*pending, reply); !ok {
				logger.L.Warnf(
					"session_usage reply send failed agent=%d owner=%d action_id=%s status=%s",
					conn.agentID, pending.ownerID, pending.actionID, payload.Status,
				)
			}
		}
	case "get_rate_limits":
		m.persistRateLimitsResult(pending, payload)
	case "forwarded_local_action":
		if pending.forwardedResultCh != nil {
			pending.forwardedResultCh <- payload
		}
	case "skill_upload":
		m.handleSkillUploadPendingResult(pending, payload)
	case "skill_enable", "skill_disable":
		m.handleSkillLibraryActionPendingResult(pending, payload)
	case "skill_refresh":
		m.handleSkillLibraryActionPendingResult(pending, payload)
	case provisioning.ApplyRelayStateActionType:
		// 中转开关服务端化（设计 §2.4 路径 B 回执）：revision 校验后写回 applied。
		m.handleApplyRelayStateResult(pending, payload)
	}
	if isToolbarStateRefreshAction(pending.kind) {
		if svc := agenttoolbar.GetGlobal(); svc != nil && pending.ownerID > 0 && strings.TrimSpace(pending.sessionID) != "" {
			_ = svc.RefreshSession(context.Background(), pending.ownerID, pending.sessionID, "local_action_result")
		}
	}
}

func buildExecApprovalResultReply(pending *pendingLocalAction, payload protocol.LocalActionResultPayload) pendingLocalActionReply {
	if pending == nil {
		return pendingLocalActionReply{}
	}
	approvalID := strings.TrimSpace(pending.approvalID)
	commandID := strings.TrimSpace(pending.approvalCommandID)
	if commandID == "" {
		commandID = approvalID
	}

	lang := ownerCardLanguage(pending.ownerID)
	status := ""
	summary := ""
	detailText := ""
	warningText := ""
	decision := ""

	switch strings.TrimSpace(payload.Status) {
	case "ok":
		decision = strings.TrimSpace(pending.decision)
		if decision == "" {
			decision = strings.TrimSpace(fmt.Sprintf("%v", payload.Result))
		}
		switch decision {
		case "allow-once":
			status = "resolved-allow-once"
			summary = tooli18n.T(lang, "exec_approval_once")
		case "allow-always":
			status = "resolved-allow-always"
			summary = tooli18n.T(lang, "exec_approval_always")
		case "allow-rule":
			status = "resolved-allow-rule"
			summary = tooli18n.T(lang, "exec_approval_rule")
		case "deny":
			status = "resolved-deny"
			summary = tooli18n.T(lang, "exec_approval_denied")
		default:
			status = "approval-forwarded"
			summary = tooli18n.T(lang, "exec_approval_submitted")
		}
		detailText = ""
	case "timeout":
		status = "approval-unavailable"
		summary = tooli18n.T(lang, "exec_approval_submit_timeout")
		warningText = tooli18n.T(lang, "exec_approval_submit_timeout_detail")
	case "unsupported":
		status = "approval-unavailable"
		summary = tooli18n.T(lang, "exec_approval_unavailable")
		if msg := strings.TrimSpace(payload.ErrorMsg); msg != "" {
			warningText = msg
		} else {
			warningText = tooli18n.T(lang, "exec_approval_unavailable_detail")
		}
	case "failed":
		switch strings.TrimSpace(payload.ErrorCode) {
		case "exec_approval_disabled":
			status = "approval-unavailable"
			summary = tooli18n.T(lang, "exec_approval_disabled")
			warningText = tooli18n.T(lang, "exec_approval_disabled_detail")
		case "exec_approval_unauthorized":
			status = "approval-unavailable"
			summary = tooli18n.T(lang, "exec_approval_unauthorized")
			warningText = tooli18n.T(lang, "exec_approval_unauthorized_detail")
		default:
			if isExpiredExecApprovalError(payload) {
				status = "approval-expired"
				summary = tooli18n.T(lang, "exec_approval_expired")
				warningText = tooli18n.T(lang, "exec_approval_expired_detail")
				break
			}
			status = "approval-unavailable"
			summary = tooli18n.T(lang, "exec_approval_failed")
			if msg := strings.TrimSpace(payload.ErrorMsg); msg != "" {
				warningText = msg
			} else {
				warningText = tooli18n.T(lang, "exec_approval_failed_detail")
			}
		}
	default:
		return pendingLocalActionReply{}
	}

	if status == "" || summary == "" {
		return pendingLocalActionReply{}
	}

	payloadMap := map[string]any{
		"status":  status,
		"summary": summary,
	}
	if approvalID != "" {
		payloadMap["approval_id"] = approvalID
	}
	if commandID != "" {
		payloadMap["approval_command_id"] = commandID
	}
	if decision != "" {
		payloadMap["decision"] = decision
	}
	if detailText != "" {
		payloadMap["detail_text"] = detailText
	}
	if warningText != "" {
		payloadMap["warning_text"] = warningText
	}
	if commandID != "" && approvalID == "" {
		payloadMap["approval_id"] = commandID
	}

	return buildExecStatusCardReply(payloadMap)
}

func isExpiredExecApprovalError(payload protocol.LocalActionResultPayload) bool {
	errorCode := strings.ToLower(strings.TrimSpace(payload.ErrorCode))
	errorMsg := strings.ToLower(strings.TrimSpace(payload.ErrorMsg))
	return strings.Contains(errorCode, "expired") ||
		strings.Contains(errorMsg, "expired approval") ||
		strings.Contains(errorMsg, "unknown or expired approval id") ||
		strings.Contains(errorMsg, "approval has expired")
}

func buildExecStatusCardReply(payload map[string]any) pendingLocalActionReply {
	summary := strings.TrimSpace(fmt.Sprintf("%v", payload["summary"]))
	if summary == "" {
		return pendingLocalActionReply{}
	}
	return pendingLocalActionReply{
		content: buildLocalGrixCardLink(
			fmt.Sprintf("[Exec Status] %s", compactReplyText(summary, 180)),
			"exec_status",
			payload,
		),
		extra: buildExecStatusCardExtra(payload),
	}
}

func buildClaudeQuestionResultReply(pending *pendingLocalAction, payload protocol.LocalActionResultPayload) pendingLocalActionReply {
	if pending == nil {
		return pendingLocalActionReply{}
	}
	requestID := strings.TrimSpace(pending.referenceID)
	if requestID == "" {
		requestID = strings.TrimSpace(pending.approvalCommandID)
	}
	if requestID == "" {
		requestID = strings.TrimSpace(pending.approvalID)
	}
	if requestID == "" {
		return pendingLocalActionReply{}
	}

	lang := ownerCardLanguage(pending.ownerID)
	cardPayload := map[string]any{
		"category":     "question",
		"reference_id": requestID,
	}
	switch strings.TrimSpace(payload.Status) {
	case "ok":
		cardPayload["status"] = "success"
		cardPayload["summary"] = tooli18n.Tf(lang, "question_recorded", requestID)
	case "timeout":
		cardPayload["status"] = "warning"
		cardPayload["summary"] = tooli18n.Tf(lang, "question_timeout", requestID)
		cardPayload["detail_text"] = tooli18n.T(lang, "question_timeout_detail")
	case "unsupported":
		cardPayload["status"] = "warning"
		cardPayload["summary"] = tooli18n.Tf(lang, "question_record_failed", requestID)
		cardPayload["detail_text"] = strings.TrimSpace(payload.ErrorMsg)
	case "failed":
		cardPayload["status"] = "error"
		cardPayload["summary"] = tooli18n.Tf(lang, "question_record_failed", requestID)
		cardPayload["detail_text"] = strings.TrimSpace(payload.ErrorMsg)
	default:
		return pendingLocalActionReply{}
	}

	if strings.TrimSpace(fmt.Sprintf("%v", cardPayload["detail_text"])) == "" {
		delete(cardPayload, "detail_text")
	}
	return buildAgentStatusCardReply(cardPayload)
}

func buildClaudeInteractionReplyResultReply(pending *pendingLocalAction, payload protocol.LocalActionResultPayload) pendingLocalActionReply {
	if pending == nil {
		return pendingLocalActionReply{}
	}

	requestID := strings.TrimSpace(pending.referenceID)
	if requestID == "" {
		requestID = strings.TrimSpace(pending.approvalCommandID)
	}
	if requestID == "" {
		requestID = strings.TrimSpace(pending.approvalID)
	}

	lang := ownerCardLanguage(pending.ownerID)
	category := "question"
	summary := tooli18n.T(lang, "reply_recorded")
	if pending.kind == "claude_interaction_reply_permission" {
		category = "approval"
		summary = tooli18n.T(lang, "approval_recorded")
	}

	cardPayload := map[string]any{
		"category":     category,
		"reference_id": requestID,
	}

	switch strings.TrimSpace(payload.Status) {
	case "ok":
		cardPayload["status"] = "success"
		cardPayload["summary"] = summary
	case "failed":
		if pending.fallbackDelivered {
			// 卡片通道已失效，但答案已降级为普通消息送达 agent。
			cardPayload["status"] = "warning"
			cardPayload["summary"] = tooli18n.T(lang, "reply_forwarded")
			cardPayload["detail_text"] = tooli18n.T(lang, "reply_forwarded_detail")
			break
		}
		cardPayload["status"] = "error"
		cardPayload["summary"] = interactionReplyFailureSummary(lang, category)
		cardPayload["detail_text"] = firstNonEmpty(strings.TrimSpace(payload.ErrorMsg), interactionReplyErrorMessage(lang, payload.ErrorCode))
	case "timeout":
		// 插件在时限内未响应：明确告知失败，不能让卡片永远停在提交中。
		cardPayload["status"] = "error"
		cardPayload["summary"] = interactionReplyFailureSummary(lang, category)
		cardPayload["detail_text"] = tooli18n.T(lang, "interaction_reply_timeout_detail")
	default:
		return pendingLocalActionReply{}
	}

	if strings.TrimSpace(fmt.Sprint(cardPayload["detail_text"])) == "" {
		delete(cardPayload, "detail_text")
	}
	return buildAgentStatusCardReply(cardPayload)
}

// deliverQuestionReplyFallback 在问答回卡无人接收（not_pending）时，把拦截时预先
// 改写好的可读文本事件按普通消息链路重投给 agent，保证用户答案不随卡片通道丢失。
// 返回是否已重投成功，供结果卡展示区分"已转发"与"彻底失败"。
func (m *Manager) deliverQuestionReplyFallback(pending *pendingLocalAction, payload protocol.LocalActionResultPayload) bool {
	if pending == nil || pending.fallbackEvent == nil || pending.kind != "claude_interaction_reply_elicitation" {
		return false
	}
	if strings.TrimSpace(payload.Status) != "failed" ||
		strings.TrimSpace(payload.ErrorCode) != "interaction_request_not_pending" {
		return false
	}
	evt := *pending.fallbackEvent
	if !m.PushDelegateEvent(evt) {
		logger.L.Warnf(
			"question reply fallback delivery failed agent=%d owner=%d session=%s event=%s",
			evt.AgentID, evt.OwnerID, evt.SessionID, evt.EventID,
		)
		return false
	}
	pending.fallbackDelivered = true
	logger.L.Infof(
		"question reply delivered as fallback message agent=%d owner=%d session=%s event=%s",
		evt.AgentID, evt.OwnerID, evt.SessionID, evt.EventID,
	)
	return true
}

type sessionControlReplyConfig struct {
	providerName  string
	bindingIDKeys []string
}

func inferProviderReplyConfig(adapterID string) sessionControlReplyConfig {
	switch adapterID {
	case "codex/base":
		return sessionControlReplyConfig{
			providerName:  "Codex",
			bindingIDKeys: []string{"codex_thread_id", "codexThreadId"},
		}
	case "cursor/base":
		return sessionControlReplyConfig{
			providerName:  "Cursor",
			bindingIDKeys: []string{"cursor_session_id", "cursorSessionId"},
		}
	case "claude/base":
		return sessionControlReplyConfig{
			providerName:  "Claude",
			bindingIDKeys: []string{"claude_session_id", "claudeSessionId"},
		}
	case "qwen/base":
		return sessionControlReplyConfig{
			providerName:  "Qwen",
			bindingIDKeys: []string{"qwen_session_id", "qwenSessionId", "acp_session_id", "acpSessionId"},
		}
	case "gemini/base":
		return sessionControlReplyConfig{
			providerName:  "Gemini",
			bindingIDKeys: []string{"gemini_session_id", "geminiSessionId"},
		}
	case "reasonix/base":
		return sessionControlReplyConfig{
			providerName:  "Reasonix",
			bindingIDKeys: []string{"reasonix_session_id", "reasonixSessionId", "acp_session_id", "acpSessionId"},
		}
	case "codewhale/base":
		return sessionControlReplyConfig{
			providerName:  "CodeWhale",
			bindingIDKeys: []string{"codewhale_thread_id", "codewhaleThreadId"},
		}
	case "opencode/base":
		return sessionControlReplyConfig{
			providerName:  "OpenCode",
			bindingIDKeys: []string{"opencode_session_id", "opencodeSessionId"},
		}
	case "kiro/base":
		return sessionControlReplyConfig{
			providerName:  "Kiro",
			bindingIDKeys: []string{"kiro_session_id", "kiroSessionId", "acp_session_id", "acpSessionId"},
		}
	case "copilot/base":
		return sessionControlReplyConfig{
			providerName:  "Copilot",
			bindingIDKeys: []string{"acp_session_id", "acpSessionId"},
		}
	case "kimi/base":
		return sessionControlReplyConfig{
			providerName:  "Kimi",
			bindingIDKeys: []string{"kimi_session_id", "kimiSessionId", "acp_session_id", "acpSessionId"},
		}
	default:
		return sessionControlReplyConfig{
			providerName:  adapterID,
			bindingIDKeys: nil,
		}
	}
}

func buildSessionControlResultReply(pending *pendingLocalAction, payload protocol.LocalActionResultPayload, config sessionControlReplyConfig) pendingLocalActionReply {
	if pending == nil {
		return pendingLocalActionReply{}
	}
	if reply := buildSessionControlOpenRetryReply(pending, payload, config); strings.TrimSpace(reply.content) != "" {
		return reply
	}

	result := localActionResultObject(payload.Result)
	verb := firstNonEmpty(resultString(result, "verb"), strings.TrimSpace(pending.referenceID))
	outcome := resultString(result, "outcome")
	binding := nestedResultObject(result, "binding")
	cwd := resultString(binding, "cwd")
	workerStatus := firstNonEmpty(
		resultString(binding, "worker_status"),
		resultString(binding, "workerStatus"),
	)

	lang := ownerCardLanguage(pending.ownerID)
	cardPayload := map[string]any{
		"category": "session",
	}
	if cardInstanceID := strings.TrimSpace(pending.cardInstanceID); cardInstanceID != "" {
		cardPayload["card_instance_id"] = cardInstanceID
	}

	switch strings.TrimSpace(payload.Status) {
	case "ok":
		cardPayload["status"] = "success"
		if outcome == "exec" {
			execCommand := resultString(result, "command")
			execMessage := resultString(result, "message")
			if execMessage != "" {
				cardPayload["summary"] = execMessage
			} else if execCommand != "" {
				cardPayload["summary"] = tooli18n.Tf(lang, "command_exec_completed", config.providerName, execCommand)
			} else {
				cardPayload["summary"] = tooli18n.Tf(lang, "command_completed", config.providerName)
			}
			if execData := result["data"]; execData != nil {
				cardPayload["detail_text"] = fmt.Sprintf("%v", execData)
			}
		} else {
			cardPayload["summary"] = sessionControlSuccessSummary(lang, config.providerName, verb, outcome, cwd)
			// 绑定/开关会话的成功卡只保留一句结论；
			// 仅查询类动作（where/status）继续附带工作区详情。
			normalizedVerb := strings.TrimSpace(verb)
			if outcome == "where" || outcome == "status" || normalizedVerb == "where" || normalizedVerb == "status" {
				cardPayload["detail_text"] = sessionControlDetailText(lang, cwd, workerStatus)
			}
		}
	case "failed":
		cardPayload["status"] = "error"
		cardPayload["summary"] = sessionControlFailureSummary(lang, config.providerName, verb)
		cardPayload["detail_text"] = firstNonEmpty(
			strings.TrimSpace(payload.ErrorMsg),
			sessionControlErrorMessage(lang, config, payload.ErrorCode),
		)
	default:
		return pendingLocalActionReply{}
	}

	if strings.TrimSpace(fmt.Sprint(cardPayload["detail_text"])) == "" {
		delete(cardPayload, "detail_text")
	}
	if strings.TrimSpace(fmt.Sprint(cardPayload["reference_id"])) == "" {
		delete(cardPayload, "reference_id")
	}
	return buildAgentStatusCardReply(cardPayload)
}

func buildSessionControlOpenRetryReply(pending *pendingLocalAction, payload protocol.LocalActionResultPayload, config sessionControlReplyConfig) pendingLocalActionReply {
	if pending == nil || strings.TrimSpace(pending.referenceID) != "open" || strings.TrimSpace(payload.Status) != "failed" {
		return pendingLocalActionReply{}
	}

	cardPayload := map[string]any{}
	if cardInstanceID := strings.TrimSpace(pending.cardInstanceID); cardInstanceID != "" {
		cardPayload["card_instance_id"] = cardInstanceID
	}
	switch strings.TrimSpace(payload.ErrorCode) {
	case "session_cwd_required":
		cardPayload["summary_text"] = fmt.Sprintf("%s workspace path is required.", config.providerName)
		cardPayload["detail_text"] = "Choose a workspace folder before submitting again."
	case "session_invalid_cwd":
		cardPayload["summary_text"] = fmt.Sprintf("%s workspace path is invalid.", config.providerName)
		cardPayload["detail_text"] = firstNonEmpty(
			strings.TrimSpace(payload.ErrorMsg),
			"Choose a valid workspace folder and try again.",
		)
	default:
		cardPayload["summary_text"] = fmt.Sprintf("%s session could not be opened.", config.providerName)
		cardPayload["detail_text"] = firstNonEmpty(
			strings.TrimSpace(payload.ErrorMsg),
			sessionControlErrorMessage("en", config, payload.ErrorCode),
			"Please try again.",
		)
	}
	if cwd := strings.TrimSpace(pending.submittedPath); cwd != "" {
		cardPayload["initial_cwd"] = cwd
	}
	return buildAgentOpenSessionCardReply(cardPayload)
}

func resultString(result map[string]any, key string) string {
	if len(result) == 0 {
		return ""
	}
	return strings.TrimSpace(stringValue(result[key]))
}

func resultBool(result map[string]any, key string) bool {
	if len(result) == 0 {
		return false
	}
	b, _ := result[key].(bool)
	return b
}

func resultStrings(result map[string]any, keys ...string) []string {
	values := make([]string, 0, len(keys))
	for _, key := range keys {
		if value := resultString(result, key); value != "" {
			values = append(values, value)
		}
	}
	return values
}

// ownerCardLanguage 返回该 owner 用于渲染 agent 状态卡片文案的语言（zh/en），
// 与 agenttoolbar 工具栏按钮/菜单文案使用的是同一份用户语言偏好，
// 保证同一块 UI 里卡片文案和工具栏文案语言一致。带超时是因为这里没有请求级
// context 可传（调用方是事件处理器，非 HTTP handler），DB 卡住时不能让查语言
// 这一步把卡片渲染一起挂住，超时后降级到 zh 兜底。
func ownerCardLanguage(ownerID int64) string {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	return toolresolver.LoadPreferredLanguage(ctx, ownerID)
}

func buildAgentStatusCardReply(payload map[string]any) pendingLocalActionReply {
	summary := strings.TrimSpace(fmt.Sprintf("%v", payload["summary"]))
	if summary == "" {
		return pendingLocalActionReply{}
	}
	return pendingLocalActionReply{
		content: buildLocalGrixCardLink(
			fmt.Sprintf("[Agent Status] %s", compactReplyText(summary, 180)),
			"agent_status",
			payload,
		),
	}
}

func buildAgentOpenSessionCardReply(payload map[string]any) pendingLocalActionReply {
	label := firstNonEmpty(
		strings.TrimSpace(fmt.Sprintf("%v", payload["summary_text"])),
		strings.TrimSpace(fmt.Sprintf("%v", payload["detail_text"])),
		strings.TrimSpace(fmt.Sprintf("%v", payload["initial_cwd"])),
	)
	if label == "" {
		return pendingLocalActionReply{}
	}
	return pendingLocalActionReply{
		content: buildLocalGrixCardLink(
			fmt.Sprintf("[Open Workspace] %s", compactReplyText(label, 160)),
			"agent_open_session",
			payload,
		),
		extra: buildAgentOpenSessionCardExtra(payload),
	}
}

func localActionResultObject(value any) map[string]any {
	object, _ := value.(map[string]any)
	if object != nil {
		return object
	}
	legacyObject, _ := value.(map[string]interface{})
	if legacyObject == nil {
		return nil
	}
	converted := make(map[string]any, len(legacyObject))
	for key, rawValue := range legacyObject {
		converted[key] = rawValue
	}
	return converted
}

func buildAgentOpenSessionCardExtra(payload map[string]any) json.RawMessage {
	envelope := map[string]any{
		"biz_card": map[string]any{
			"version": 1,
			"type":    "agent_open_session",
			"payload": payload,
		},
	}
	encoded, err := json.Marshal(envelope)
	if err != nil {
		return nil
	}
	return encoded
}

func nestedResultObject(parent map[string]any, key string) map[string]any {
	if len(parent) == 0 {
		return nil
	}
	return localActionResultObject(parent[key])
}

func stringValue(value any) string {
	if value == nil {
		return ""
	}
	return fmt.Sprint(value)
}

func interactionReplyFailureSummary(lang, category string) string {
	if category == "approval" {
		return tooli18n.T(lang, "approval_record_failed")
	}
	return tooli18n.T(lang, "reply_record_failed")
}

func interactionReplyErrorMessage(lang, errorCode string) string {
	switch strings.TrimSpace(errorCode) {
	case "interaction_request_id_required":
		return tooli18n.T(lang, "err_request_id_required")
	case "interaction_request_not_found":
		return tooli18n.T(lang, "err_request_not_found")
	case "interaction_request_not_pending":
		return tooli18n.T(lang, "err_request_not_pending")
	case "interaction_resolution_invalid":
		return tooli18n.T(lang, "err_reply_invalid")
	case "interaction_forward_failed":
		return tooli18n.T(lang, "err_reply_rejected")
	default:
		return ""
	}
}

func sessionControlSuccessSummary(lang, providerName, verb, outcome, cwd string) string {
	switch outcome {
	case "opened":
		if cwd != "" {
			return tooli18n.Tf(lang, "bound_path", cwd)
		}
		return tooli18n.T(lang, "bound_ok")
	case "already_bound":
		if cwd != "" {
			return tooli18n.Tf(lang, "bound_path", cwd)
		}
		return tooli18n.Tf(lang, "already_bound", providerName)
	case "where":
		if cwd != "" {
			return tooli18n.Tf(lang, "where_path", providerName, cwd)
		}
		return tooli18n.Tf(lang, "where_ok", providerName)
	case "stopped":
		if cwd != "" {
			return tooli18n.Tf(lang, "stopped_path", providerName, cwd)
		}
		return tooli18n.Tf(lang, "stopped", providerName)
	case "restarted":
		if cwd != "" {
			return tooli18n.Tf(lang, "restarted_path", providerName, cwd)
		}
		return tooli18n.Tf(lang, "restarted", providerName)
	case "status":
		fallthrough
	default:
		if strings.TrimSpace(verb) == "restart" {
			return tooli18n.Tf(lang, "restarted", providerName)
		}
		if strings.TrimSpace(verb) == "where" && cwd != "" {
			return tooli18n.Tf(lang, "where_path", providerName, cwd)
		}
		return tooli18n.Tf(lang, "status_ok", providerName)
	}
}

func sessionControlFailureSummary(lang, providerName, verb string) string {
	switch strings.TrimSpace(verb) {
	case "open":
		return tooli18n.Tf(lang, "open_failed", providerName)
	case "stop":
		return tooli18n.Tf(lang, "stop_failed", providerName)
	case "restart":
		return tooli18n.Tf(lang, "restart_failed", providerName)
	case "where":
		return tooli18n.Tf(lang, "where_failed", providerName)
	default:
		return tooli18n.Tf(lang, "status_failed", providerName)
	}
}

func sessionControlErrorMessage(lang string, config sessionControlReplyConfig, errorCode string) string {
	switch strings.TrimSpace(errorCode) {
	case "session_cwd_required":
		return tooli18n.T(lang, "err_cwd_required")
	case "session_invalid_cwd":
		return tooli18n.T(lang, "err_cwd_invalid")
	case "session_binding_missing":
		return tooli18n.Tf(lang, "binding_missing", config.providerName)
	case "session_rebind_forbidden":
		return tooli18n.T(lang, "err_rebind_forbidden")
	case "session_verb_invalid":
		return tooli18n.T(lang, "err_verb_invalid")
	case "session_runtime_error":
		return tooli18n.Tf(lang, "runtime_error", config.providerName)
	default:
		return ""
	}
}

func sessionControlDetailText(lang, cwd, workerStatus string) string {
	parts := make([]string, 0, 2)
	if cwd != "" {
		parts = append(parts, tooli18n.Tf(lang, "detail_workspace", cwd))
	}
	if workerStatus != "" {
		parts = append(parts, tooli18n.Tf(lang, "detail_worker", workerStatus))
	}
	return strings.Join(parts, "\n")
}

func buildSessionControlTimeoutReply(pending *pendingLocalAction, config sessionControlReplyConfig) pendingLocalActionReply {
	if pending == nil {
		return pendingLocalActionReply{}
	}
	lang := ownerCardLanguage(pending.ownerID)
	cardPayload := map[string]any{
		"category":     "session",
		"status":       "warning",
		"summary":      tooli18n.Tf(lang, "timeout", config.providerName),
		"detail_text":  tooli18n.T(lang, "timeout_detail"),
		"reference_id": pending.sessionID,
	}
	if cardInstanceID := strings.TrimSpace(pending.cardInstanceID); cardInstanceID != "" {
		cardPayload["card_instance_id"] = cardInstanceID
	}
	return buildAgentStatusCardReply(cardPayload)
}

func buildGeminiToolbarSelectionStartReply(pending *pendingLocalAction) pendingLocalActionReply {
	if pending == nil {
		return pendingLocalActionReply{}
	}
	targetType, targetLabel := describeGeminiToolbarSelectionTarget(pending)
	if targetType == "" || targetLabel == "" {
		return pendingLocalActionReply{}
	}
	lang := ownerCardLanguage(pending.ownerID)
	return buildAgentStatusCardReply(map[string]any{
		"category":         "session",
		"status":           "pending",
		"summary":          tooli18n.Tf(lang, "gemini_toolbar_pending", tooli18n.LocalizeText(lang, targetType), targetLabel),
		"detail_text":      tooli18n.T(lang, "gemini_toolbar_pending_detail"),
		"reference_id":     pending.sessionID,
		"card_instance_id": strings.TrimSpace(pending.cardInstanceID),
	})
}

func buildGeminiToolbarSelectionResultReply(pending *pendingLocalAction, payload protocol.LocalActionResultPayload) pendingLocalActionReply {
	if pending == nil {
		return pendingLocalActionReply{}
	}
	targetType, targetLabel := describeGeminiToolbarSelectionTarget(pending)
	if targetType == "" || targetLabel == "" {
		return pendingLocalActionReply{}
	}

	lang := ownerCardLanguage(pending.ownerID)
	localizedType := tooli18n.LocalizeText(lang, targetType)
	cardPayload := map[string]any{
		"category":         "session",
		"reference_id":     pending.sessionID,
		"card_instance_id": strings.TrimSpace(pending.cardInstanceID),
	}

	switch strings.TrimSpace(payload.Status) {
	case "ok":
		cardPayload["status"] = "success"
		cardPayload["summary"] = tooli18n.Tf(lang, "gemini_switched", localizedType, targetLabel)
	case "failed":
		cardPayload["status"] = "error"
		cardPayload["summary"] = tooli18n.Tf(lang, "gemini_switch_failed", localizedType)
		cardPayload["detail_text"] = firstNonEmpty(
			strings.TrimSpace(payload.ErrorMsg),
			tooli18n.Tf(lang, "gemini_switch_failed_detail", targetLabel),
		)
	case "unsupported":
		cardPayload["status"] = "warning"
		cardPayload["summary"] = tooli18n.Tf(lang, "gemini_switch_unsupported", localizedType)
		cardPayload["detail_text"] = firstNonEmpty(
			strings.TrimSpace(payload.ErrorMsg),
			tooli18n.T(lang, "gemini_toolbar_unsupported_detail"),
		)
	default:
		return pendingLocalActionReply{}
	}

	if strings.TrimSpace(fmt.Sprint(cardPayload["detail_text"])) == "" {
		delete(cardPayload, "detail_text")
	}
	return buildAgentStatusCardReply(cardPayload)
}

func buildGeminiToolbarSelectionTimeoutReply(pending *pendingLocalAction) pendingLocalActionReply {
	if pending == nil {
		return pendingLocalActionReply{}
	}
	targetType, _ := describeGeminiToolbarSelectionTarget(pending)
	if targetType == "" {
		return pendingLocalActionReply{}
	}
	lang := ownerCardLanguage(pending.ownerID)
	return buildAgentStatusCardReply(map[string]any{
		"category":         "session",
		"status":           "warning",
		"summary":          tooli18n.Tf(lang, "gemini_switch_timeout", tooli18n.LocalizeText(lang, targetType)),
		"detail_text":      tooli18n.T(lang, "timeout_detail"),
		"reference_id":     pending.sessionID,
		"card_instance_id": strings.TrimSpace(pending.cardInstanceID),
	})
}

func buildToolbarSelectionResultReply(pending *pendingLocalAction, payload protocol.LocalActionResultPayload) pendingLocalActionReply {
	if pending == nil {
		return pendingLocalActionReply{}
	}
	targetType := "设置"
	targetLabel := firstNonEmpty(strings.TrimSpace(pending.displayLabel), strings.TrimSpace(pending.referenceID))
	switch strings.TrimSpace(pending.kind) {
	case "set_model":
		targetType = "模型"
	case "set_provider":
		targetType = "供应商"
	case "set_preset":
		targetType = "场景"
	case "set_mode":
		targetType = "模式"
	case "set_reasoning_effort":
		targetType = "推理力度"
	case "set_service_tier":
		targetType = "速度档"
	}

	lang := ownerCardLanguage(pending.ownerID)
	localizedType := tooli18n.LocalizeText(lang, targetType)
	cardPayload := map[string]any{
		"category":         "session",
		"reference_id":     pending.sessionID,
		"card_instance_id": strings.TrimSpace(pending.cardInstanceID),
	}

	switch strings.TrimSpace(payload.Status) {
	case "ok":
		cardPayload["status"] = "success"
		if targetLabel != "" {
			cardPayload["summary"] = tooli18n.Tf(lang, "switched", localizedType, targetLabel)
		} else {
			cardPayload["summary"] = tooli18n.Tf(lang, "switch_ok", localizedType)
		}
	case "failed":
		cardPayload["status"] = "error"
		cardPayload["summary"] = tooli18n.Tf(lang, "switch_failed", localizedType)
		cardPayload["detail_text"] = firstNonEmpty(
			strings.TrimSpace(payload.ErrorMsg),
			tooli18n.Tf(lang, "switch_failed_detail", targetLabel),
		)
	default:
		return pendingLocalActionReply{}
	}

	if strings.TrimSpace(fmt.Sprint(cardPayload["detail_text"])) == "" {
		delete(cardPayload, "detail_text")
	}
	return buildAgentStatusCardReply(cardPayload)
}

func describeGeminiToolbarSelectionTarget(pending *pendingLocalAction) (targetType string, targetLabel string) {
	if pending == nil {
		return "", ""
	}
	targetLabel = firstNonEmpty(strings.TrimSpace(pending.displayLabel), strings.TrimSpace(pending.referenceID))
	switch strings.TrimSpace(pending.kind) {
	case "set_model":
		return "模型", targetLabel
	case "set_mode":
		return "审批模式", targetLabel
	default:
		return "", targetLabel
	}
}

func buildThreadCompactStartReply(lang string) pendingLocalActionReply {
	return buildAgentStatusCardReply(map[string]any{
		"category": "session",
		"status":   "pending",
		"summary":  tooli18n.T(lang, "compact_pending"),
	})
}

func buildThreadCompactResultReply(pending *pendingLocalAction, payload protocol.LocalActionResultPayload) pendingLocalActionReply {
	if pending == nil {
		return pendingLocalActionReply{}
	}
	lang := ownerCardLanguage(pending.ownerID)
	cardPayload := map[string]any{"category": "session"}
	if cardInstanceID := strings.TrimSpace(pending.cardInstanceID); cardInstanceID != "" {
		cardPayload["card_instance_id"] = cardInstanceID
	}
	switch strings.TrimSpace(payload.Status) {
	case "ok":
		cardPayload["status"] = "success"
		cardPayload["summary"] = tooli18n.T(lang, "compact_done")
		cardPayload["reference_id"] = pending.sessionID
	case "failed":
		cardPayload["status"] = "error"
		cardPayload["summary"] = tooli18n.T(lang, "compact_failed")
		cardPayload["detail_text"] = firstNonEmpty(strings.TrimSpace(payload.ErrorMsg), tooli18n.T(lang, "retry_later"))
		cardPayload["reference_id"] = pending.sessionID
	default:
		return pendingLocalActionReply{}
	}
	return buildAgentStatusCardReply(cardPayload)
}

func buildThreadCompactTimeoutReply(pending *pendingLocalAction) pendingLocalActionReply {
	if pending == nil {
		return pendingLocalActionReply{}
	}
	lang := ownerCardLanguage(pending.ownerID)
	return buildAgentStatusCardReply(map[string]any{
		"category":         "session",
		"status":           "warning",
		"summary":          tooli18n.T(lang, "compact_timeout"),
		"detail_text":      tooli18n.T(lang, "timeout_detail"),
		"reference_id":     pending.sessionID,
		"card_instance_id": strings.TrimSpace(pending.cardInstanceID),
	})
}

type sessionUsageTokenStats struct {
	InputTokens              int64
	OutputTokens             int64
	CacheReadInputTokens     int64
	CacheCreationInputTokens int64
}

type sessionUsageModel struct {
	Model string
	Turns int64
	Total sessionUsageTokenStats
}

type sessionUsageReport struct {
	SessionID   string
	AdapterType string
	Models      []sessionUsageModel
	Total       sessionUsageTokenStats
	Turns       int64
	SampledAt   string
}

func buildSessionUsageResultReply(pending *pendingLocalAction, payload protocol.LocalActionResultPayload) pendingLocalActionReply {
	if pending == nil {
		return pendingLocalActionReply{}
	}

	switch strings.TrimSpace(payload.Status) {
	case "ok":
		report := parseSessionUsageReport(localActionResultObject(payload.Result))
		summary, _ := sessionUsageSuccessText(report)
		cardPayload := buildSessionUsageCardPayload(pending, report, summary)
		return buildAgentStatusCardReply(cardPayload)
	case "failed", "unsupported":
		lang := ownerCardLanguage(pending.ownerID)
		errorCode := strings.TrimSpace(payload.ErrorCode)
		if strings.TrimSpace(payload.Status) == "unsupported" && errorCode == "" {
			errorCode = "unsupported_local_action"
		}
		status, summary := sessionUsageFailureStatusSummary(lang, errorCode)
		cardPayload := map[string]any{
			"category":     "session",
			"status":       status,
			"summary":      summary,
			"detail_text":  sessionUsageErrorDetail(lang, errorCode, payload.ErrorMsg),
			"reference_id": pending.sessionID,
		}
		if cardInstanceID := strings.TrimSpace(pending.cardInstanceID); cardInstanceID != "" {
			cardPayload["card_instance_id"] = cardInstanceID
		}
		if strings.TrimSpace(fmt.Sprint(cardPayload["detail_text"])) == "" {
			delete(cardPayload, "detail_text")
		}
		return buildAgentStatusCardReply(cardPayload)
	default:
		return pendingLocalActionReply{}
	}
}

func buildSessionUsageCardPayload(pending *pendingLocalAction, report sessionUsageReport, summary string) map[string]any {
	// 构建结构化的模型明细
	models := make([]map[string]any, 0, len(report.Models))
	for _, m := range report.Models {
		models = append(models, map[string]any{
			"model":              m.Model,
			"turns":              m.Turns,
			"input_tokens":       m.Total.InputTokens,
			"output_tokens":      m.Total.OutputTokens,
			"cache_read_tokens":  m.Total.CacheReadInputTokens,
			"cache_write_tokens": m.Total.CacheCreationInputTokens,
		})
	}

	usageData := map[string]any{
		"type":       "session_usage",
		"provider":   sessionUsageProviderName(report.AdapterType),
		"session_id": firstNonEmpty(strings.TrimSpace(report.SessionID), pending.sessionID),
		"turns":      report.Turns,
		"sampled_at": report.SampledAt,
		"total": map[string]any{
			"input_tokens":       report.Total.InputTokens,
			"output_tokens":      report.Total.OutputTokens,
			"cache_read_tokens":  report.Total.CacheReadInputTokens,
			"cache_write_tokens": report.Total.CacheCreationInputTokens,
		},
		"models": models,
	}

	cardPayload := map[string]any{
		"category":     "session",
		"status":       "success",
		"summary":      summary,
		"reference_id": firstNonEmpty(strings.TrimSpace(report.SessionID), pending.sessionID),
		"usage_data":   usageData,
	}
	if cardInstanceID := strings.TrimSpace(pending.cardInstanceID); cardInstanceID != "" {
		cardPayload["card_instance_id"] = cardInstanceID
	}
	return cardPayload
}

func buildSessionUsageTimeoutReply(pending *pendingLocalAction) pendingLocalActionReply {
	if pending == nil {
		return pendingLocalActionReply{}
	}
	lang := ownerCardLanguage(pending.ownerID)
	return buildAgentStatusCardReply(map[string]any{
		"category":         "session",
		"status":           "warning",
		"summary":          tooli18n.T(lang, "usage_timeout"),
		"detail_text":      tooli18n.T(lang, "timeout_detail"),
		"reference_id":     pending.sessionID,
		"card_instance_id": strings.TrimSpace(pending.cardInstanceID),
	})
}

func parseSessionUsageReport(result map[string]any) sessionUsageReport {
	report := sessionUsageReport{
		SessionID:   resultString(result, "sessionId"),
		AdapterType: strings.ToLower(resultString(result, "adapterType")),
		Total:       parseSessionUsageTokenStats(localActionResultObject(result["total"])),
		Turns:       numberToInt64(result["turns"]),
		SampledAt:   resultString(result, "sampledAt"),
	}
	modelsRaw, _ := result["models"].([]any)
	for _, raw := range modelsRaw {
		modelMap := localActionResultObject(raw)
		report.Models = append(report.Models, sessionUsageModel{
			Model: firstNonEmpty(resultString(modelMap, "model"), "unknown"),
			Turns: numberToInt64(modelMap["turns"]),
			Total: parseSessionUsageTokenStats(localActionResultObject(modelMap["total"])),
		})
	}
	if report.Turns <= 0 {
		var turns int64
		for _, model := range report.Models {
			turns += model.Turns
		}
		report.Turns = turns
	}
	return report
}

func parseSessionUsageTokenStats(result map[string]any) sessionUsageTokenStats {
	return sessionUsageTokenStats{
		InputTokens:              numberToInt64(result["inputTokens"]),
		OutputTokens:             numberToInt64(result["outputTokens"]),
		CacheReadInputTokens:     numberToInt64(result["cacheReadInputTokens"]),
		CacheCreationInputTokens: numberToInt64(result["cacheCreationInputTokens"]),
	}
}

func numberToInt64(value any) int64 {
	switch typed := value.(type) {
	case int:
		return int64(typed)
	case int8:
		return int64(typed)
	case int16:
		return int64(typed)
	case int32:
		return int64(typed)
	case int64:
		return typed
	case uint:
		return int64(typed)
	case uint8:
		return int64(typed)
	case uint16:
		return int64(typed)
	case uint32:
		return int64(typed)
	case uint64:
		if typed > uint64(^uint64(0)>>1) {
			return int64(^uint64(0) >> 1)
		}
		return int64(typed)
	case float32:
		return int64(typed)
	case float64:
		return int64(typed)
	case json.Number:
		if parsed, err := typed.Int64(); err == nil {
			return parsed
		}
		if parsed, err := typed.Float64(); err == nil {
			return int64(parsed)
		}
	case string:
		text := strings.TrimSpace(typed)
		if text == "" {
			return 0
		}
		if parsed, err := strconv.ParseInt(text, 10, 64); err == nil {
			return parsed
		}
		if parsed, err := strconv.ParseFloat(text, 64); err == nil {
			return int64(parsed)
		}
	}
	return 0
}

func (s sessionUsageTokenStats) TotalTokens() int64 {
	return s.InputTokens + s.OutputTokens + s.CacheReadInputTokens + s.CacheCreationInputTokens
}

func formatSessionUsageTokenStats(stats sessionUsageTokenStats) string {
	return fmt.Sprintf(
		"输入 %d / 输出 %d / 缓存命中 %d / 缓存写入 %d / 总计 %d",
		stats.InputTokens,
		stats.OutputTokens,
		stats.CacheReadInputTokens,
		stats.CacheCreationInputTokens,
		stats.TotalTokens(),
	)
}

func sessionUsageSuccessText(report sessionUsageReport) (summary string, detail string) {
	summary = fmt.Sprintf("%d turns, %d tokens", report.Turns, report.Total.TotalTokens())
	return summary, ""
}

func sessionUsageProviderName(adapterType string) string {
	switch strings.TrimSpace(strings.ToLower(adapterType)) {
	case "claude":
		return "Claude"
	case "codex":
		return "Codex"
	case "acp":
		return "ACP"
	case "pi":
		return "Pi"
	default:
		return strings.TrimSpace(adapterType)
	}
}

func sessionUsageFailureStatusSummary(lang, errorCode string) (status string, summary string) {
	switch strings.TrimSpace(errorCode) {
	case "no_binding":
		return "warning", tooli18n.T(lang, "usage_no_binding")
	case "usage_not_found":
		return "warning", tooli18n.T(lang, "usage_not_found")
	case "unsupported_local_action":
		return "warning", tooli18n.T(lang, "usage_unsupported")
	case "session_id_required":
		return "error", tooli18n.T(lang, "usage_invalid_params")
	case "timeout":
		return "warning", tooli18n.T(lang, "usage_timeout")
	default:
		return "error", tooli18n.T(lang, "usage_failed")
	}
}

// sessionUsageErrorDetail 返回失败详情文案。errorCode 为内部转发链路
// 自产生的诊断码（如 timeout）时，errorMsg 是给日志看的内部字符串，
// 不面向用户，需优先用友好文案覆盖，而不是原样透传。
func sessionUsageErrorDetail(lang, errorCode, errorMsg string) string {
	switch strings.TrimSpace(errorCode) {
	case "timeout":
		return tooli18n.T(lang, "timeout_detail")
	}
	if msg := strings.TrimSpace(errorMsg); msg != "" {
		return msg
	}
	switch strings.TrimSpace(errorCode) {
	case "no_binding":
		return tooli18n.T(lang, "usage_no_binding_detail")
	case "usage_not_found":
		return tooli18n.T(lang, "usage_not_found_detail")
	case "unsupported_local_action":
		return tooli18n.T(lang, "usage_unsupported_detail")
	case "session_id_required":
		return tooli18n.T(lang, "usage_invalid_params_detail")
	default:
		return ""
	}
}

func buildLocalGrixCardLink(fallbackText, cardType string, payload map[string]any) string {
	return "[" + sanitizeMarkdownLinkText(fallbackText) + "](" + buildLocalGrixCardURI(cardType, payload) + ")"
}

// sanitizeMarkdownLinkText strips characters from card fallback text that
// could break markdown link parsing (e.g. $ symbols that trigger LaTeX
// inline math in the frontend renderer). The fallback text is display-only;
// the actual card data lives in the grix://card URI parameters.
func sanitizeMarkdownLinkText(text string) string {
	return strings.ReplaceAll(text, "$", "")
}

func buildLocalGrixCardURI(cardType string, payload map[string]any) string {
	values := url.Values{}
	if hasComplexReplyPayload(payload) {
		data, _ := json.Marshal(payload)
		values.Set("d", string(data))
	} else {
		for key, value := range payload {
			switch typed := value.(type) {
			case nil:
				continue
			case string:
				if typed == "" {
					continue
				}
				values.Set(key, typed)
			default:
				values.Set(key, fmt.Sprint(value))
			}
		}
	}

	return (&url.URL{
		Scheme:   "grix",
		Host:     "card",
		Path:     "/" + strings.TrimSpace(cardType),
		RawQuery: values.Encode(),
	}).String()
}

func hasComplexReplyPayload(payload map[string]any) bool {
	for _, value := range payload {
		if value == nil {
			continue
		}
		switch reflect.ValueOf(value).Kind() {
		case reflect.Map, reflect.Slice, reflect.Array:
			return true
		}
	}
	return false
}

func compactReplyText(text string, limit int) string {
	compact := strings.Join(strings.Fields(strings.TrimSpace(text)), " ")
	if limit <= 3 || len(compact) <= limit {
		return compact
	}
	return compact[:limit-3] + "..."
}

func isToolbarStateRefreshAction(kind string) bool {
	switch strings.TrimSpace(kind) {
	case "session_control", "set_model", "set_mode", "set_provider", "set_preset", "set_reasoning_effort", "set_service_tier", "set_sandbox_mode", "get_context", "get_session_usage", "get_rate_limits":
		return true
	default:
		return false
	}
}

func isGeminiToolbarSelectionAction(conn *agentConn, pending *pendingLocalAction) bool {
	if conn == nil || pending == nil {
		return false
	}
	if kind := strings.TrimSpace(pending.kind); kind != "set_model" && kind != "set_mode" {
		return false
	}
	return strings.EqualFold(normalizeToolbarProviderKey(conn), "gemini")
}

func (m *Manager) shouldSendGeminiToolbarSelectionCard(pending *pendingLocalAction) bool {
	if m == nil || pending == nil {
		return false
	}
	if kind := strings.TrimSpace(pending.kind); kind != "set_model" && kind != "set_mode" {
		return false
	}
	if conn := m.lookupConn(pending.agentID); conn != nil {
		return isGeminiToolbarSelectionAction(conn, pending)
	}
	record, ok, err := toolstore.LoadBinding(context.Background(), pending.agentID, pending.sessionID)
	if err != nil || !ok {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(record.ProviderKey), "gemini")
}

// sendOrUpdateBindingCardReply edits the original binding card in-place when
// a bindingCardMsgID is tracked. Falls back to creating a new message if editing
// is not possible or fails.
func (m *Manager) sendOrUpdateBindingCardReply(pending pendingLocalAction, reply pendingLocalActionReply) bool {
	isRetry := isRetryBindingCardReply(reply.content)
	if pending.bindingCardMsgID > 0 && m.editMsgFn != nil {
		err := m.editMsgFn(context.Background(), pending.agentID, pending.ownerID, EditMsgPayload{
			SessionID: pending.sessionID,
			MsgID:     pending.bindingCardMsgID,
			Content:   reply.content,
			Extra:     reply.extra,
		})
		if err != nil {
			logger.L.Warnf("edit binding card failed, falling back to new message: agent=%d session=%s msg_id=%d err=%v",
				pending.agentID, pending.sessionID, pending.bindingCardMsgID, err)
		} else {
			isAgentStatus := strings.Contains(reply.content, "grix://card/agent_status")
			if !isRetry && !isAgentStatus {
				deleteBindingCardMsgID(context.Background(), pending.agentID, pending.sessionID)
			}
			if pending.geminiCleanup && !isRetry {
				deleteGeminiPendingWorkspace(context.Background(), pending.agentID, pending.sessionID)
			}
			return true
		}
	}
	if pending.geminiCleanup && !isRetry {
		deleteGeminiPendingWorkspace(context.Background(), pending.agentID, pending.sessionID)
	}
	return m.sendPendingLocalActionReply(pending, reply)
}

func (m *Manager) sendOrUpdateApprovalCardReply(pending pendingLocalAction, reply pendingLocalActionReply) bool {
	if pending.approvalCardMsgID > 0 && m.editMsgFn != nil {
		err := m.editMsgFn(context.Background(), pending.agentID, pending.ownerID, EditMsgPayload{
			SessionID: pending.sessionID,
			MsgID:     pending.approvalCardMsgID,
			Content:   reply.content,
			Extra:     reply.extra,
		})
		if err != nil {
			logger.L.Warnf("edit approval card failed, falling back to new message: agent=%d session=%s msg_id=%d err=%v",
				pending.agentID, pending.sessionID, pending.approvalCardMsgID, err)
		} else {
			requestID := firstNonEmpty(
				strings.TrimSpace(pending.approvalCommandID),
				strings.TrimSpace(pending.referenceID),
			)
			deleteApprovalCardMsgID(context.Background(), pending.agentID, pending.sessionID, requestID)
			return true
		}
	}
	return m.sendPendingLocalActionReply(pending, reply)
}

func isRetryBindingCardReply(content string) bool {
	return strings.Contains(content, "grix://card/agent_open_session")
}

func (m *Manager) sendPendingLocalActionReply(pending pendingLocalAction, reply pendingLocalActionReply) bool {
	if m == nil || m.sendFn == nil {
		return false
	}
	if strings.TrimSpace(pending.sessionID) == "" || pending.agentID <= 0 || pending.ownerID <= 0 || strings.TrimSpace(reply.content) == "" {
		return false
	}

	clientMsgID := fmt.Sprintf("local_action_%s_reply", strings.TrimSpace(pending.actionID))
	adapterID := ""
	if conn := m.lookupConn(pending.agentID); conn != nil {
		adapterID = conn.adapterID
		if strings.TrimSpace(adapterID) == "" {
			adapterID = conn.clientType
		}
	}
	// 审批族结果卡同样不得落进托管代答的客户会话（例如主人在客户会话里手动
	// /approve 的边界场景会走到这里），与发卡路径同口径改投主人私聊。
	sessionID := pending.sessionID
	threadID := pending.threadID
	quotedMessageID := pending.quotedMessageID
	if isApprovalFamilyCard(reply.content) {
		sessionID = resolveApprovalCardSessionID(context.Background(), pending.sessionID, pending.agentID, pending.ownerID)
		if sessionID != pending.sessionID {
			threadID = ""
			quotedMessageID = 0
		}
	}
	result, err := m.sendFn(context.Background(), SendMessageReq{
		AgentID:         pending.agentID,
		OwnerID:         pending.ownerID,
		SessionID:       sessionID,
		ThreadID:        threadID,
		ClientMsgID:     clientMsgID,
		MsgType:         1,
		Content:         reply.content,
		Extra:           append(json.RawMessage(nil), reply.extra...),
		VisibleTo:       ownerVisibleToForAdapterCard(adapterID, reply.content, reply.extra, pending.ownerID),
		QuotedMessageID: quotedMessageID,
	})
	if err != nil {
		logger.L.Warnf(
			"send local_action reply failed agent=%d owner=%d session=%s action_id=%s err=%v",
			pending.agentID,
			pending.ownerID,
			pending.sessionID,
			pending.actionID,
			err,
		)
		return false
	}
	// 只有 session_control（目录绑定）语义的状态卡才能登记为"绑定卡"，供后续
	// update_binding_card 原地编辑。此前任何 agent_status 卡（问答回执、切模型
	// 回执等）都会被登记，随后被绑定状态更新原地改写成"已绑定 <cwd>"，看起来
	// 像执行了绑定动作。
	if result != nil && result.MsgID > 0 && pending.kind == "session_control" &&
		strings.Contains(reply.content, "grix://card/agent_status") {
		saveBindingCardMsgID(context.Background(), pending.agentID, pending.sessionID, result.MsgID)
	}
	return true
}

const localActionTimeout = 20 * time.Second

func (m *Manager) storePendingLocalAction(pending *pendingLocalAction) {
	if m == nil || pending == nil {
		return
	}
	actionID := strings.TrimSpace(pending.actionID)
	if actionID == "" {
		return
	}
	copyPending := *pending
	timeout := localActionTimeout
	if pending.timeoutMs > 0 {
		timeout = time.Duration(pending.timeoutMs)*time.Millisecond + 5*time.Second
	}
	copyPending.timeoutTimer = m.afterFunc(timeout, func() {
		m.timeoutPendingLocalAction(actionID)
	})
	m.localActionsMu.Lock()
	if old, ok := m.pendingLocalActions[actionID]; ok && old.timeoutTimer != nil {
		old.timeoutTimer.Stop()
	}
	m.pendingLocalActions[actionID] = &copyPending
	m.localActionsMu.Unlock()
}

func (m *Manager) deletePendingLocalAction(actionID string) {
	if m == nil {
		return
	}
	actionID = strings.TrimSpace(actionID)
	if actionID == "" {
		return
	}
	m.localActionsMu.Lock()
	if old, ok := m.pendingLocalActions[actionID]; ok && old.timeoutTimer != nil {
		old.timeoutTimer.Stop()
	}
	delete(m.pendingLocalActions, actionID)
	m.localActionsMu.Unlock()
}

func (m *Manager) takePendingLocalAction(actionID string) *pendingLocalAction {
	if m == nil {
		return nil
	}
	actionID = strings.TrimSpace(actionID)
	if actionID == "" {
		return nil
	}
	m.localActionsMu.Lock()
	pending := m.pendingLocalActions[actionID]
	delete(m.pendingLocalActions, actionID)
	m.localActionsMu.Unlock()
	if pending != nil && pending.timeoutTimer != nil {
		pending.timeoutTimer.Stop()
	}
	return pending
}

func (m *Manager) timeoutPendingLocalAction(actionID string) {
	actionID = strings.TrimSpace(actionID)
	if actionID == "" {
		return
	}
	m.localActionsMu.Lock()
	pending := m.pendingLocalActions[actionID]
	delete(m.pendingLocalActions, actionID)
	m.localActionsMu.Unlock()
	if pending == nil {
		return
	}

	logger.L.Warnf("local_action timed out agent=%d action_id=%s kind=%s session=%s",
		pending.agentID, actionID, pending.kind, pending.sessionID)

	switch pending.kind {
	case "session_control":
		config := inferProviderReplyConfig("")
		reply := buildSessionControlTimeoutReply(pending, config)
		if strings.TrimSpace(reply.content) != "" {
			m.sendOrUpdateBindingCardReply(*pending, reply)
		}
	case "set_model", "set_mode":
		if m.shouldSendGeminiToolbarSelectionCard(pending) {
			reply := buildGeminiToolbarSelectionTimeoutReply(pending)
			if strings.TrimSpace(reply.content) != "" {
				m.sendOrUpdateBindingCardReply(*pending, reply)
			}
		}
	case "exec_approval":
		reply := buildExecApprovalResultReply(pending, protocol.LocalActionResultPayload{Status: "timeout"})
		if strings.TrimSpace(reply.content) != "" {
			m.sendOrUpdateApprovalCardReply(*pending, reply)
		}
	case "permission_approval":
		reply := buildExecApprovalResultReply(pending, protocol.LocalActionResultPayload{Status: "timeout"})
		if strings.TrimSpace(reply.content) != "" {
			m.sendOrUpdateApprovalCardReply(*pending, reply)
		}
	case "claude_interaction_reply_permission", "claude_interaction_reply_elicitation":
		reply := buildClaudeInteractionReplyResultReply(pending, protocol.LocalActionResultPayload{Status: "timeout"})
		if strings.TrimSpace(reply.content) != "" {
			m.sendOrUpdateApprovalCardReply(*pending, reply)
		}
	case "thread_compact":
		reply := buildThreadCompactTimeoutReply(pending)
		if strings.TrimSpace(reply.content) != "" {
			m.sendOrUpdateBindingCardReply(*pending, reply)
		}
	case "get_session_usage":
		reply := buildSessionUsageTimeoutReply(pending)
		if strings.TrimSpace(reply.content) != "" {
			m.sendOrUpdateBindingCardReply(*pending, reply)
		}
	case "get_rate_limits":
		logger.L.Infof("[toolbar-local-action] get_rate_limits timeout: action_id=%s session_id=%s agent=%d owner=%d", pending.actionID, pending.sessionID, pending.agentID, pending.ownerID)
	case provisioning.ApplyRelayStateActionType:
		// apply_relay_state 超时 = 期望未达成：按 ok=false 写回 applied（带下发时的
		// revision 兜底），与 connector 显式失败回执同一语义（设计 §2.4）。
		revision, _ := strconv.ParseInt(strings.TrimSpace(pending.referenceID), 10, 64)
		if _, ec := service.GatewayRelayStateApplyResult(pending.ownerID, pending.agentID, revision, false); ec != nil {
			logger.L.Warnf("apply_relay_state timeout write applied=false failed agent=%d owner=%d biz=%d msg=%s",
				pending.agentID, pending.ownerID, ec.BizCode, ec.Msg)
		}
	case "file_list":
		if pending.fileListResultCh != nil {
			pending.fileListResultCh <- &fileListResponse{}
		}
	}

	if isToolbarStateRefreshAction(pending.kind) {
		if svc := agenttoolbar.GetGlobal(); svc != nil && pending.ownerID > 0 && strings.TrimSpace(pending.sessionID) != "" {
			_ = svc.RefreshSession(context.Background(), pending.ownerID, pending.sessionID, "local_action_timeout")
		}
	}
}

// lookupConnForOwner 按发起者 owner 精确选连接：
// ownerID>0 仅精确匹配 (agentID, ownerID)，无对应连接返回 nil；
// ownerID<=0 非法，直接返回 nil。绝不回退主连接或「任一」连接——
// 那会把一个 owner 的 local_action 投到另一个 owner 的 connector 上（共享隔离破坏）。
// 用于 agent 共享多连接物理隔离的反向下发路径。
func (m *Manager) lookupConnForOwner(agentID, ownerID int64) *agentConn {
	if agentID <= 0 || ownerID <= 0 {
		return nil
	}
	m.mu.RLock()
	conn := m.conns[agentID][ownerID]
	m.mu.RUnlock()
	return conn
}

// sendLocalActionWithPendingForOwner 按发起者 owner 路由下发 local_action 并登记 pending：
// 本地按 owner 精确选连接下发；本地不在则跨节点转发时携带 ownerID 让目标节点按 owner 选连接。
// ownerID<=0 非法，直接失败（fail-closed），绝不回退主连接。
func (m *Manager) sendLocalActionWithPendingForOwner(agentID, ownerID int64, action protocol.LocalActionPayload, pending *pendingLocalAction) bool {
	if ownerID <= 0 {
		logger.L.Warnf("reject local_action with missing owner: agent_id=%d action_id=%s action_type=%s", agentID, action.ActionID, action.ActionType)
		return false
	}
	if pending != nil {
		m.storePendingLocalAction(pending)
	}
	// Only trust the local connection when the authoritative Redis route agrees
	// the agent lives on this node. After the agent reconnects to another node
	// (load balancer reshuffle / flapping), this node may still hold a now-dead
	// local conn that was never cleaned up; sending into it silently succeeds at
	// the socket layer but the agent never receives it, so the action hangs until
	// timeout. When the route points elsewhere, skip the stale local conn and
	// forward to where the agent actually is.
	// 按 (agentID, ownerID) 判定:owner 路由优先 / 退主路由。这样在 A 主连接与 B 共享连接
	// 散到不同节点时,B 的 local_action 不会因为路由表只记录主节点而被误判为"本地连接过时"
	// 并错投到 A 的节点。
	if m.localConnIsAuthoritativeForOwner(agentID, ownerID) {
		// 按发起者 owner 精确选连接下发。
		if ok := m.SendLocalActionForOwner(agentID, ownerID, action); ok {
			return true
		}
	}
	// Agent is not connected locally (or our local conn is stale) — forward via
	// Redis to the node that currently owns the agent，携带 ownerID 让目标节点按 owner 选连接。
	if m.tryForwardLocalActionForOwner(agentID, ownerID, action, nil) {
		return true
	}
	if pending != nil {
		m.deletePendingLocalAction(action.ActionID)
	}
	return false
}

// localConnIsAuthoritativeForOwner 按 (agentID, ownerID) 维度判定本节点是否权威。
// ownerID<=0 非法，返回 false。先用 owner 精确取本地连接：取到且 connectionEpoch>0 时
// 用该连接自己的权威记录判定（isAgentConnectionAuthoritative 校验的正是 (agentID, ownerID)
// 的权威记录，语义正确）；取不到或 epoch 缺失则查 owner 路由：路由为空返回 true
// （单机/无 Redis 兼容，后续 SendLocalActionForOwner 找不到连接会自然失败），
// 否则仅当路由指向本节点才权威。绝不因本地持有「别的 owner」的连接而误判权威。
func (m *Manager) localConnIsAuthoritativeForOwner(agentID, ownerID int64) bool {
	if agentID <= 0 || ownerID <= 0 {
		return false
	}
	if conn := m.lookupConnByOwner(agentID, ownerID); conn != nil && conn.connectionEpoch > 0 {
		return m.isAgentConnectionAuthoritative(conn)
	}
	route := loadAgentRouteForOwner(context.Background(), agentID, ownerID)
	if route == "" {
		return true
	}
	return route == m.getNodeID()
}

// SendLocalActionForOwner 按发起者 owner 精确选连接下发 local_action。
// ownerID<=0 非法，直接失败（fail-closed），绝不回退主连接。
// 这是 agent 共享反向下发的精确版本，避免被共享者的请求落到主人的 connector。
func (m *Manager) SendLocalActionForOwner(agentID, ownerID int64, action protocol.LocalActionPayload) bool {
	if ownerID <= 0 {
		logger.L.Warnf("reject local_action send with missing owner: agent_id=%d action_id=%s action_type=%s", agentID, action.ActionID, action.ActionType)
		return false
	}
	conn := m.lookupConnForOwner(agentID, ownerID)

	if conn == nil {
		return false
	}
	if !m.ensureAgentConnectionAuthoritative(conn) {
		return false
	}
	actionType := strings.TrimSpace(action.ActionType)
	if actionType == "" {
		logger.L.Warnf("send local_action skipped agent=%d: empty action_type", agentID)
		return false
	}
	if !hasDeclaredName(conn.capabilities, "local_action_v1") {
		logger.L.Warnf("send local_action skipped agent=%d adapter=%s: local_action_v1 not declared", agentID, conn.adapterID)
		return false
	}
	if !hasDeclaredName(conn.localActions, actionType) {
		logger.L.Warnf("send local_action skipped agent=%d adapter=%s: action_type=%s not declared", agentID, conn.adapterID, actionType)
		return false
	}

	// Route through adapter if available.
	if conn.adapter != nil {
		domainApproval := agentadapter.DomainApprovalEvent{
			ActionType: action.ActionType,
			ActionID:   action.ActionID,
			Params:     marshalPayload(action.Params),
			TimeoutMs:  action.TimeoutMs,
		}
		outbound, err := conn.adapter.NormalizeApproval(context.Background(), domainApproval)
		if err != nil {
			logger.L.Warnf("adapter NormalizeApproval failed agent=%d adapter=%s err=%v, falling back to local_action", agentID, conn.adapterID, err)
		} else if outbound != nil {
			return conn.sendPayload(outbound.Cmd, conn.nextSeq(), json.RawMessage(outbound.Payload))
		}
	}

	return conn.sendPayload(protocol.CmdLocalAction, conn.nextSeq(), action)
}

// ToolbarLocalActionRequest carries the context needed to create a pending entry
// for a toolbar-initiated local_action, so that local_action_result is processed.
type ToolbarLocalActionRequest struct {
	ActionID     string
	ActionType   string
	OwnerID      int64
	AgentID      int64
	SessionID    string
	ReferenceID  string // for session_control: the verb ("status", "where", "stop")
	DisplayLabel string
	Params       map[string]interface{}
	TimeoutMs    int
}

func (m *Manager) DispatchToolbarLocalAction(req ToolbarLocalActionRequest) bool {
	if req.OwnerID <= 0 {
		logger.L.Warnf("[toolbar-local-action] reject dispatch with missing owner: action_id=%s action_type=%s agent_id=%d",
			strings.TrimSpace(req.ActionID), strings.TrimSpace(req.ActionType), req.AgentID)
		return false
	}
	logger.L.Infof("[toolbar-local-action] dispatch action_id=%s action_type=%s session_id=%s agent_id=%d owner_id=%d",
		strings.TrimSpace(req.ActionID), strings.TrimSpace(req.ActionType), strings.TrimSpace(req.SessionID), req.AgentID, req.OwnerID)
	action := protocol.LocalActionPayload{
		ActionID:   strings.TrimSpace(req.ActionID),
		ActionType: strings.TrimSpace(req.ActionType),
		Params:     req.Params,
		TimeoutMs:  req.TimeoutMs,
	}
	pending := &pendingLocalAction{
		actionID:     strings.TrimSpace(req.ActionID),
		kind:         strings.TrimSpace(req.ActionType),
		agentID:      req.AgentID,
		ownerID:      req.OwnerID,
		sessionID:    strings.TrimSpace(req.SessionID),
		actionType:   strings.TrimSpace(req.ActionType),
		referenceID:  strings.TrimSpace(req.ReferenceID),
		displayLabel: strings.TrimSpace(req.DisplayLabel),
		timeoutMs:    req.TimeoutMs,
	}
	if strings.TrimSpace(req.ActionType) == "thread_compact" {
		m.sendThreadCompactStartCard(pending)
	}
	if m.shouldSendGeminiToolbarSelectionCard(pending) {
		m.sendGeminiToolbarSelectionStartCard(pending)
	}
	// 按发起者 owner 精确路由（agent 共享多连接物理隔离）：B 在被共享 agent 工具栏点
	// thread_compact / set_mode / gemini_selection 时,动作必须落到 B 的 connector,
	// 而不是主人 A 的 connector。
	return m.sendLocalActionWithPendingForOwner(req.AgentID, req.OwnerID, action, pending)
}

func (m *Manager) sendThreadCompactStartCard(pending *pendingLocalAction) {
	if m == nil || m.sendFn == nil || pending == nil {
		return
	}
	reply := buildThreadCompactStartReply(ownerCardLanguage(pending.ownerID))
	if strings.TrimSpace(reply.content) == "" {
		return
	}
	clientMsgID := fmt.Sprintf("local_action_%s_start", pending.actionID)
	result, err := m.sendFn(context.Background(), SendMessageReq{
		AgentID:     pending.agentID,
		OwnerID:     pending.ownerID,
		SessionID:   pending.sessionID,
		ClientMsgID: clientMsgID,
		MsgType:     1,
		Content:     reply.content,
		Extra:       append(json.RawMessage(nil), reply.extra...),
	})
	if err != nil {
		logger.L.Warnf("send thread_compact start card failed agent=%d session=%s err=%v", pending.agentID, pending.sessionID, err)
		return
	}
	if result != nil && result.MsgID > 0 {
		pending.bindingCardMsgID = result.MsgID
	}
}

func (m *Manager) sendGeminiToolbarSelectionStartCard(pending *pendingLocalAction) {
	if m == nil || m.sendFn == nil || pending == nil {
		return
	}
	reply := buildGeminiToolbarSelectionStartReply(pending)
	if strings.TrimSpace(reply.content) == "" {
		return
	}
	clientMsgID := fmt.Sprintf("local_action_%s_start", pending.actionID)
	result, err := m.sendFn(context.Background(), SendMessageReq{
		AgentID:     pending.agentID,
		OwnerID:     pending.ownerID,
		SessionID:   pending.sessionID,
		ClientMsgID: clientMsgID,
		MsgType:     1,
		Content:     reply.content,
		Extra:       append(json.RawMessage(nil), reply.extra...),
	})
	if err != nil {
		logger.L.Warnf("send gemini toolbar start card failed agent=%d session=%s err=%v", pending.agentID, pending.sessionID, err)
		return
	}
	if result != nil && result.MsgID > 0 {
		pending.bindingCardMsgID = result.MsgID
	}
}

func (m *Manager) persistToolbarBinding(conn *agentConn, pending *pendingLocalAction, payload protocol.LocalActionResultPayload) {
	if conn == nil || pending == nil || pending.agentID <= 0 || strings.TrimSpace(pending.sessionID) == "" {
		return
	}
	if strings.TrimSpace(payload.Status) != "ok" {
		return
	}
	result := localActionResultObject(payload.Result)
	if len(result) == 0 && toolbarSelectionFallbackID(pending) == "" {
		return
	}
	binding := nestedResultObject(result, "binding")
	sessionContext := nestedResultObject(result, "session_context")
	meta := map[string]any{
		"verb":    resultString(result, "verb"),
		"outcome": resultString(result, "outcome"),
		"binding": binding,
	}
	if len(sessionContext) > 0 {
		meta["session_context"] = sessionContext
	}
	// 这几个键一律「键存在就原样塞进 meta」，nil 也塞——空值该沿用旧值还是清空，
	// 由 mergeToolbarMeta 按 toolbarMetaNullableKeys 统一裁决。若在这里用 != nil
	// 先滤一道，nullable 名单对这条路径就等于失效（合并那步根本收不到这个键）。
	if availableModels, ok := result["available_models"]; ok {
		meta["available_models"] = availableModels
	}
	if availableProviders, ok := result["available_providers"]; ok {
		meta["available_providers"] = availableProviders
	}
	if availablePresets, ok := result["available_presets"]; ok {
		meta["available_presets"] = availablePresets
	} else if availablePresets, ok := binding["available_presets"]; ok {
		meta["available_presets"] = availablePresets
	}
	if plugins, ok := result["dsh_plugins"]; ok {
		meta["dsh_plugins"] = plugins
	} else if plugins, ok := sessionContext["dsh_plugins"]; ok {
		meta["dsh_plugins"] = plugins
	}
	copyToolbarProjectionValue(meta, sessionContext, result, "dsh_plugin_restart_required", "dshPluginRestartRequired")
	if availableEfforts, ok := result["available_efforts"]; ok {
		meta["available_efforts"] = availableEfforts
	}
	if providerID := firstNonEmpty(
		resultString(sessionContext, "provider_id"),
		resultString(sessionContext, "providerId"),
		resultString(result, "provider_id"),
		resultString(result, "providerId"),
		toolbarSelectionFallbackIDForKind(pending, "set_provider"),
	); providerID != "" {
		meta["provider_id"] = providerID
	}
	if modelID := firstNonEmpty(
		resultString(sessionContext, "model_id"),
		resultString(sessionContext, "modelId"),
		resultString(result, "model_id"),
		resultString(result, "modelId"),
		toolbarSelectionFallbackIDForKind(pending, "set_model"),
	); modelID != "" {
		meta["model_id"] = modelID
	}
	if modeID := firstNonEmpty(
		resultString(sessionContext, "mode_id"),
		resultString(sessionContext, "modeId"),
		resultString(result, "mode_id"),
		resultString(result, "modeId"),
		toolbarSelectionFallbackIDForKind(pending, "set_mode"),
	); modeID != "" {
		meta["mode_id"] = modeID
	}
	if presetID := firstNonEmpty(
		resultString(sessionContext, "agent_preset_id"),
		resultString(sessionContext, "agentPreset"),
		resultString(result, "agent_preset_id"),
		resultString(result, "agentPreset"),
		resultString(binding, "agent_preset_id"),
		resultString(binding, "agentPreset"),
		toolbarSelectionFallbackIDForKind(pending, "set_preset"),
	); presetID != "" {
		meta["agent_preset_id"] = presetID
	}
	copyToolbarProjectionValue(meta, sessionContext, result, "agent_preset_locked", "agentPresetLocked")
	if _, ok := meta["agent_preset_locked"]; !ok {
		copyToolbarProjectionValue(meta, binding, nil, "agent_preset_locked", "agentPresetLocked")
	}
	copyToolbarProjectionValue(meta, sessionContext, result, "applied_model_id", "appliedModelId")
	copyToolbarProjectionValue(meta, sessionContext, result, "applied_mode_id", "appliedModeId")
	copyToolbarProjectionValue(meta, sessionContext, result, "applied_provider_id", "appliedProviderId")
	copyToolbarProjectionValue(meta, sessionContext, result, "settings_revision", "settingsRevision")
	copyToolbarProjectionValue(meta, sessionContext, result, "applied_settings_revision", "appliedSettingsRevision")
	copyToolbarProjectionValue(meta, sessionContext, result, "settings_state", "settingsState")
	copyToolbarProjectionValue(meta, sessionContext, result, "settings_error_code", "settingsErrorCode")
	if effort := firstNonEmpty(
		resultString(sessionContext, "effort"),
		resultString(sessionContext, "reasoning_effort"),
		resultString(sessionContext, "reasoningEffort"),
		resultString(result, "effort"),
		resultString(result, "reasoning_effort"),
		resultString(result, "reasoningEffort"),
		toolbarSelectionFallbackIDForKind(pending, "set_reasoning_effort"),
	); effort != "" {
		// Claude's canonical field is effort. Keep the legacy alias in the
		// binding projection so older consumers continue to render the value;
		// other providers retain their existing reasoning_effort projection.
		meta["reasoning_effort"] = effort
		if strings.EqualFold(normalizeToolbarProviderKey(conn), "claude") {
			meta["effort"] = effort
		}
	}
	// 速度档：codex 连接器的 session_context 始终携带 serviceTier 键（null=标准档）。
	// 键存在即为权威：非空写入；为空归位 default（防切模型/连接器守卫归一后残留旧档位，
	// 也防把连接器未生效的请求档位当成功持久化）。键不存在（非 codex / 旧插件）时，
	// 仅允许 set_service_tier 的 default 请求兜底归位，绝不兜底写具体档位。
	serviceTierValue := firstNonEmpty(
		resultString(sessionContext, "service_tier"),
		resultString(sessionContext, "serviceTier"),
	)
	_, hasServiceTierSnake := sessionContext["service_tier"]
	_, hasServiceTierCamel := sessionContext["serviceTier"]
	switch {
	case serviceTierValue != "":
		meta["service_tier"] = serviceTierValue
	case hasServiceTierSnake || hasServiceTierCamel:
		meta["service_tier"] = defaultServiceTierID
	case toolbarSelectionFallbackIDForKind(pending, "set_service_tier") == defaultServiceTierID:
		meta["service_tier"] = defaultServiceTierID
	}
	if approvalPolicy := firstNonEmpty(resultString(sessionContext, "approval_policy"), resultString(sessionContext, "approvalPolicy")); approvalPolicy != "" {
		meta["approval_policy"] = approvalPolicy
	}
	if sandboxMode := firstNonEmpty(resultString(sessionContext, "sandbox_mode"), resultString(sessionContext, "sandboxMode")); sandboxMode != "" {
		meta["sandbox_mode"] = sandboxMode
	}
	if threadID := firstNonEmpty(
		resultString(result, "thread_id"),
		resultString(binding, "thread_id"),
		resultString(binding, "threadId"),
		resultString(binding, "codex_thread_id"),
		resultString(binding, "codexThreadId"),
	); threadID != "" {
		meta["thread_id"] = threadID
	}
	// available_service_tiers 走 meta 一起合并，让 nullable 键的「键存在即覆盖」
	// 规则由 mergeToolbarMeta 统一裁决，不再在这里单独开一个覆盖口子。
	if availableServiceTiers, ok := result["available_service_tiers"]; ok {
		meta["available_service_tiers"] = availableServiceTiers
	}
	record, _, _ := toolstore.LoadBinding(context.Background(), pending.agentID, pending.sessionID)
	record.Meta = mergeToolbarMeta(record.Meta, meta)
	record.AgentID = pending.agentID
	record.SessionID = pending.sessionID
	record.ProviderKey = firstNonEmpty(record.ProviderKey, normalizeToolbarProviderKey(conn))
	record.BindingID = firstNonEmpty(extractToolbarBindingID(conn, binding), record.BindingID)
	record.Cwd = firstNonEmpty(resultString(binding, "cwd"), record.Cwd)
	record.Status = firstNonEmpty(resultString(result, "outcome"), record.Status)
	record.WorkerStatus = firstNonEmpty(
		resultString(binding, "worker_status"),
		resultString(binding, "workerStatus"),
		record.WorkerStatus,
	)
	if err := toolstore.UpsertBinding(context.Background(), record); err != nil {
		logger.L.Warnf("persist toolbar binding failed agent=%d session=%s err=%v", pending.agentID, pending.sessionID, err)
	}
}

func (m *Manager) persistRateLimitsResult(pending *pendingLocalAction, payload protocol.LocalActionResultPayload) {
	if pending == nil || pending.agentID <= 0 || strings.TrimSpace(pending.sessionID) == "" {
		return
	}
	if strings.TrimSpace(payload.Status) != "ok" {
		return
	}
	result := localActionResultObject(payload.Result)
	if len(result) == 0 {
		return
	}
	rateLimits := nestedResultObject(result, "rateLimits")
	contextWindowRaw, hasContextWindow := result["contextWindow"]
	providerQuotaRaw, hasProviderQuota := result["providerQuota"]
	if len(rateLimits) == 0 && !hasContextWindow && !hasProviderQuota {
		return
	}
	record, _, _ := toolstore.LoadBinding(context.Background(), pending.agentID, pending.sessionID)
	meta := map[string]any{}
	if len(rateLimits) > 0 {
		sampledAt, _ := result["sampledAt"]
		if sampledAt == nil {
			sampledAt = float64(time.Now().UnixMilli())
		}
		rateLimitsData := make(map[string]any, len(rateLimits)+1)
		for k, v := range rateLimits {
			rateLimitsData[k] = v
		}
		rateLimitsData["sampledAt"] = sampledAt
		meta["rate_limits"] = rateLimitsData
	}
	if hasContextWindow {
		meta["context_window"] = normalizeRateLimitProjection(contextWindowRaw, result["sampledAt"])
	}
	if hasProviderQuota {
		meta["provider_quota"] = normalizeRateLimitProjection(providerQuotaRaw, result["sampledAt"])
	}
	record.Meta = mergeToolbarMeta(record.Meta, meta)
	record.AgentID = pending.agentID
	record.SessionID = pending.sessionID
	if err := toolstore.UpsertBinding(context.Background(), record); err != nil {
		logger.L.Warnf("persist rate_limits failed agent=%d session=%s err=%v", pending.agentID, pending.sessionID, err)
	}
}

func copyToolbarProjectionValue(dst, primary, fallback map[string]any, snakeKey, camelKey string) {
	for _, source := range []map[string]any{primary, fallback} {
		if value, ok := source[snakeKey]; ok {
			dst[snakeKey] = value
			return
		}
		if value, ok := source[camelKey]; ok {
			dst[snakeKey] = value
			return
		}
	}
}

func normalizeRateLimitProjection(raw any, sampledAt any) any {
	if raw == nil {
		return nil
	}
	object, ok := raw.(map[string]any)
	if !ok {
		return raw
	}
	if sampledAt == nil {
		sampledAt = float64(time.Now().UnixMilli())
	}
	result := make(map[string]any, len(object)+1)
	for key, value := range object {
		result[key] = value
	}
	result["sampledAt"] = sampledAt
	return result
}

func toolbarSelectionFallbackID(pending *pendingLocalAction) string {
	return firstNonEmpty(
		toolbarSelectionFallbackIDForKind(pending, "set_model"),
		toolbarSelectionFallbackIDForKind(pending, "set_mode"),
		toolbarSelectionFallbackIDForKind(pending, "set_provider"),
		toolbarSelectionFallbackIDForKind(pending, "set_preset"),
		toolbarSelectionFallbackIDForKind(pending, "set_reasoning_effort"),
		toolbarSelectionFallbackIDForKind(pending, "set_service_tier"),
	)
}

func toolbarSelectionFallbackIDForKind(pending *pendingLocalAction, kind string) string {
	if pending == nil || strings.TrimSpace(pending.kind) != strings.TrimSpace(kind) {
		return ""
	}
	return strings.TrimSpace(pending.referenceID)
}

func (m *Manager) persistGeminiToolbarSelection(pending *pendingLocalAction, payload protocol.LocalActionResultPayload) {
	if pending == nil || pending.agentID <= 0 || strings.TrimSpace(pending.sessionID) == "" {
		return
	}
	if strings.TrimSpace(payload.Status) != "ok" {
		return
	}

	snapshot, _, err := geminisession.Load(context.Background(), pending.agentID, pending.sessionID)
	if err != nil {
		logger.L.Warnf("load gemini toolbar selection failed agent=%d session=%s err=%v", pending.agentID, pending.sessionID, err)
	}
	snapshot.AgentID = pending.agentID
	snapshot.SessionID = strings.TrimSpace(pending.sessionID)

	switch strings.TrimSpace(pending.kind) {
	case "set_model":
		snapshot.ModelID = strings.TrimSpace(pending.referenceID)
	case "set_mode":
		snapshot.ModeID = strings.TrimSpace(pending.referenceID)
	default:
		return
	}

	if err := geminisession.Upsert(context.Background(), snapshot); err != nil {
		logger.L.Warnf("persist gemini toolbar selection failed agent=%d session=%s err=%v", pending.agentID, pending.sessionID, err)
	}
}

func extractToolbarBindingID(conn *agentConn, binding map[string]any) string {
	keys := inferProviderReplyConfig(normalizeToolbarProviderAdapterID(conn)).bindingIDKeys
	for _, key := range keys {
		if value := resultString(binding, key); value != "" {
			return value
		}
	}
	return ""
}

func normalizeToolbarProviderAdapterID(conn *agentConn) string {
	if conn == nil {
		return ""
	}
	return firstNonEmpty(strings.TrimSpace(conn.adapterID), strings.TrimSpace(conn.clientType))
}

func normalizeToolbarProviderKey(conn *agentConn) string {
	adapterID := normalizeToolbarProviderAdapterID(conn)
	if idx := strings.IndexByte(adapterID, '/'); idx > 0 {
		return adapterID[:idx]
	}
	return adapterID
}

func hasToolbarMetaValue(value any) bool {
	if value == nil {
		return false
	}
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed) != ""
	case []any:
		return len(typed) > 0
	case map[string]any:
		return len(typed) > 0
	default:
		return true
	}
}

// --- File list synchronous request/response ---

type FileNode struct {
	ID          string  `json:"id"`
	Name        string  `json:"name"`
	IsDirectory bool    `json:"is_directory"`
	Size        *int64  `json:"size,omitempty"`
	ModifiedAt  *string `json:"modified_at,omitempty"`
	MimeType    *string `json:"mime_type,omitempty"`
}

type fileListResponse struct {
	Files       []FileNode `json:"files,omitempty"`
	CurrentPath string     `json:"current_path,omitempty"`
	MachineName string     `json:"machine_name,omitempty"`
	Error       string     `json:"error,omitempty"`
}

type sessionListResponse struct {
	Sessions []map[string]interface{} `json:"sessions,omitempty"`
	Error    string                   `json:"error,omitempty"`
}

type sessionBindResponse struct {
	SessionID      string                 `json:"session_id,omitempty"`
	ProviderKey    string                 `json:"provider_key,omitempty"`
	BindingID      string                 `json:"binding_id,omitempty"`
	AgentSessionID string                 `json:"agent_session_id,omitempty"`
	Cwd            string                 `json:"cwd,omitempty"`
	WorkerStatus   string                 `json:"worker_status,omitempty"`
	Binding        map[string]interface{} `json:"binding,omitempty"`
	ErrorCode      string                 `json:"error_code,omitempty"`
	ErrorMsg       string                 `json:"error_msg,omitempty"`
}

// FileListErrorCodes defines error codes for file list operations.
var (
	ErrFileListAgentOffline = errors.New("agent not connected")
	ErrFileListTimeout      = errors.New("agent did not respond in time")
	ErrFileListNotSupported = errors.New("agent does not support file_list")
)

const fileListActionTimeout = 15 * time.Second

// skillUploadResponse is the synchronous result of a skill_upload local_action
// (docs/architecture/39 §4：工具栏一键上传)。
type skillUploadResponse struct {
	Error string `json:"error,omitempty"`
}

var (
	ErrSkillUploadAgentOffline = errors.New("agent not connected")
	ErrSkillUploadTimeout      = errors.New("agent did not respond in time")
	ErrSkillUploadNotSupported = errors.New("agent does not support skill_upload")
)

const skillUploadActionTimeout = 15 * time.Second

func (m *Manager) handleSkillUploadPendingResult(pending *pendingLocalAction, payload protocol.LocalActionResultPayload) {
	if pending == nil || pending.skillUploadResultCh == nil {
		return
	}
	status := strings.TrimSpace(payload.Status)
	if status != "ok" {
		errMsg := strings.TrimSpace(payload.ErrorMsg)
		if errMsg == "" {
			errMsg = strings.TrimSpace(payload.ErrorCode)
		}
		if errMsg == "" {
			errMsg = fmt.Sprintf("skill_upload failed with status: %s", status)
		}
		pending.skillUploadResultCh <- &skillUploadResponse{Error: errMsg}
		return
	}
	pending.skillUploadResultCh <- &skillUploadResponse{}
}

// SendSkillUploadActionAndWait 把工具栏"上传"点击转成 skill_upload local_action 下发给
// 目标 agent 的 connector，同步等待结果。成功后平台已入库（UpsertUserSkillByName）并已
// 广播 skill_sync，调用方不需要再自己刷新技能库。
// 按发起者 ownerID 精确路由到对应连接（agent 共享多连接物理隔离）。
func (m *Manager) SendSkillUploadActionAndWait(agentID, ownerID int64, sessionID, name, actorID string) error {
	if m == nil {
		return ErrSkillUploadAgentOffline
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return errors.New("skill name is required")
	}

	actionID := fmt.Sprintf("skill_upload:%d:%d", agentID, snowflake.GenID())
	params := map[string]any{
		"session_id": sessionID,
		"name":       name,
		"actor_id":   actorID,
	}

	ch := make(chan *skillUploadResponse, 1)
	pending := &pendingLocalAction{
		actionID:            actionID,
		kind:                "skill_upload",
		agentID:             agentID,
		ownerID:             ownerID,
		sessionID:           sessionID,
		actionType:          "skill_upload",
		skillUploadResultCh: ch,
	}

	action := protocol.LocalActionPayload{
		ActionID:   actionID,
		ActionType: "skill_upload",
		Params:     params,
	}

	dispatchedAt := time.Now()
	if !m.sendLocalActionWithPendingForOwner(agentID, ownerID, action, pending) {
		return ErrSkillUploadNotSupported
	}

	timer := time.NewTimer(skillUploadActionTimeout)
	defer timer.Stop()

	var resp *skillUploadResponse
	select {
	case resp = <-ch:
	case <-m.stopping():
		select {
		case resp = <-ch:
		default:
			m.deletePendingLocalAction(actionID)
			return ErrSkillUploadTimeout
		}
	case <-timer.C:
		m.deletePendingLocalAction(actionID)
		logger.L.Warnf(
			"[skill-upload] timeout action_id=%s agent_id=%d session_id=%s waited=%dms",
			actionID, agentID, sessionID, time.Since(dispatchedAt).Milliseconds(),
		)
		return ErrSkillUploadTimeout
	}

	if resp == nil || resp.Error != "" {
		errMsg := "agent returned an error"
		if resp != nil && resp.Error != "" {
			errMsg = resp.Error
		}
		return errors.New(errMsg)
	}
	return nil
}

// skillLibraryActionResponse is the synchronous result of a skill_enable/skill_disable
// local_action（技能库启用，方案 v2）。EnableState 回显 connector 上报的
// enable_scopes 取值（如 "link"/"unmanaged"/"none"），ConflictKind 仅在失败且属于
// 冲突场景时携带，供客户端据此渲染"覆盖为链接/替换为普通文件"等二次确认选项。
type skillLibraryActionResponse struct {
	EnableState   string
	Uninstallable bool
	ConflictKind  string
	Error         string
}

var (
	ErrSkillEnableAgentOffline = errors.New("agent not connected")
	ErrSkillEnableTimeout      = errors.New("agent did not respond in time")
	ErrSkillEnableNotSupported = errors.New("agent does not support skill_enable")

	ErrSkillDisableAgentOffline = errors.New("agent not connected")
	ErrSkillDisableTimeout      = errors.New("agent did not respond in time")
	ErrSkillDisableNotSupported = errors.New("agent does not support skill_disable")

	ErrSkillRefreshAgentOffline = errors.New("agent not connected")
	ErrSkillRefreshTimeout      = errors.New("agent did not respond in time")
	ErrSkillRefreshNotSupported = errors.New("agent does not support skill_refresh")
)

const skillLibraryActionTimeout = 15 * time.Second

func (m *Manager) handleSkillLibraryActionPendingResult(pending *pendingLocalAction, payload protocol.LocalActionResultPayload) {
	if pending == nil || pending.skillLibraryResultCh == nil {
		return
	}
	result := localActionResultObject(payload.Result)
	status := strings.TrimSpace(payload.Status)
	if status != "ok" {
		errMsg := strings.TrimSpace(payload.ErrorMsg)
		if errMsg == "" {
			errMsg = strings.TrimSpace(payload.ErrorCode)
		}
		if errMsg == "" {
			errMsg = fmt.Sprintf("%s failed with status: %s", pending.actionType, status)
		}
		pending.skillLibraryResultCh <- &skillLibraryActionResponse{
			Error:        errMsg,
			ConflictKind: resultString(result, "conflict_kind"),
		}
		return
	}
	pending.skillLibraryResultCh <- &skillLibraryActionResponse{
		EnableState:   resultString(result, "enable_state"),
		Uninstallable: resultBool(result, "uninstallable"),
	}
}

// SendSkillEnableActionAndWait 把工具栏"启用"点击转成 skill_enable local_action 下发给
// 目标 agent 的 connector，同步等待结果。scope 为 "global" 或 "project"；force 可选：
// "replace_with_link"（同内容真实目录改软链）或 "replace_link"（已链向别处时改链）。
// conflict（内容不同）永不允许 force 覆盖。
// 按发起者 ownerID 精确路由到对应连接（agent 共享多连接物理隔离）。
func (m *Manager) SendSkillEnableActionAndWait(agentID, ownerID int64, sessionID, name, scope, actorID, force string) (*skillLibraryActionResponse, error) {
	if m == nil {
		return nil, ErrSkillEnableAgentOffline
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, errors.New("skill name is required")
	}
	scope = strings.TrimSpace(scope)
	if scope == "" {
		return nil, errors.New("scope is required")
	}

	actionID := fmt.Sprintf("skill_enable:%d:%d", agentID, snowflake.GenID())
	params := map[string]any{
		"session_id": sessionID,
		"name":       name,
		"scope":      scope,
		"actor_id":   actorID,
	}
	if force = strings.TrimSpace(force); force != "" {
		params["force"] = force
	}

	ch := make(chan *skillLibraryActionResponse, 1)
	pending := &pendingLocalAction{
		actionID:             actionID,
		kind:                 "skill_enable",
		agentID:              agentID,
		ownerID:              ownerID,
		sessionID:            sessionID,
		actionType:           "skill_enable",
		skillLibraryResultCh: ch,
	}

	action := protocol.LocalActionPayload{
		ActionID:   actionID,
		ActionType: "skill_enable",
		Params:     params,
	}

	dispatchedAt := time.Now()
	if !m.sendLocalActionWithPendingForOwner(agentID, ownerID, action, pending) {
		return nil, ErrSkillEnableNotSupported
	}

	timer := time.NewTimer(skillLibraryActionTimeout)
	defer timer.Stop()

	var resp *skillLibraryActionResponse
	select {
	case resp = <-ch:
	case <-m.stopping():
		select {
		case resp = <-ch:
		default:
			m.deletePendingLocalAction(actionID)
			return nil, ErrSkillEnableTimeout
		}
	case <-timer.C:
		m.deletePendingLocalAction(actionID)
		logger.L.Warnf(
			"[skill-enable] timeout action_id=%s agent_id=%d session_id=%s name=%s scope=%s waited=%dms",
			actionID, agentID, sessionID, name, scope, time.Since(dispatchedAt).Milliseconds(),
		)
		return nil, ErrSkillEnableTimeout
	}

	if resp == nil {
		return nil, errors.New("agent returned an empty response")
	}
	if resp.Error != "" {
		return resp, errors.New(resp.Error)
	}
	return resp, nil
}

// SendSkillDisableActionAndWait 把工具栏"停用"点击转成 skill_disable local_action 下发给
// 目标 agent 的 connector，同步等待结果。scope 为 "global" 或 "project"。
// 按发起者 ownerID 精确路由到对应连接（agent 共享多连接物理隔离）。
func (m *Manager) SendSkillDisableActionAndWait(agentID, ownerID int64, sessionID, name, scope, actorID string) (*skillLibraryActionResponse, error) {
	if m == nil {
		return nil, ErrSkillDisableAgentOffline
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, errors.New("skill name is required")
	}
	scope = strings.TrimSpace(scope)
	if scope == "" {
		return nil, errors.New("scope is required")
	}

	actionID := fmt.Sprintf("skill_disable:%d:%d", agentID, snowflake.GenID())
	params := map[string]any{
		"session_id": sessionID,
		"name":       name,
		"scope":      scope,
		"actor_id":   actorID,
	}

	ch := make(chan *skillLibraryActionResponse, 1)
	pending := &pendingLocalAction{
		actionID:             actionID,
		kind:                 "skill_disable",
		agentID:              agentID,
		ownerID:              ownerID,
		sessionID:            sessionID,
		actionType:           "skill_disable",
		skillLibraryResultCh: ch,
	}

	action := protocol.LocalActionPayload{
		ActionID:   actionID,
		ActionType: "skill_disable",
		Params:     params,
	}

	dispatchedAt := time.Now()
	if !m.sendLocalActionWithPendingForOwner(agentID, ownerID, action, pending) {
		return nil, ErrSkillDisableNotSupported
	}

	timer := time.NewTimer(skillLibraryActionTimeout)
	defer timer.Stop()

	var resp *skillLibraryActionResponse
	select {
	case resp = <-ch:
	case <-m.stopping():
		select {
		case resp = <-ch:
		default:
			m.deletePendingLocalAction(actionID)
			return nil, ErrSkillDisableTimeout
		}
	case <-timer.C:
		m.deletePendingLocalAction(actionID)
		logger.L.Warnf(
			"[skill-disable] timeout action_id=%s agent_id=%d session_id=%s name=%s scope=%s waited=%dms",
			actionID, agentID, sessionID, name, scope, time.Since(dispatchedAt).Milliseconds(),
		)
		return nil, ErrSkillDisableTimeout
	}

	if resp == nil {
		return nil, errors.New("agent returned an empty response")
	}
	if resp.Error != "" {
		return resp, errors.New(resp.Error)
	}
	return resp, nil
}

// SendSkillRefreshActionAndWait 把技能弹窗的「下拉刷新」转成 skill_refresh local_action
// 下发给目标 agent 的 connector/插件，同步等待结果。插件收到后应立即重扫本地
// skills + 技能库并先推 agent_skills_update 再回 local_action_result（同一连接保序），
// 因此本调用返回成功时 redis runtime profile 已是最新，调用方可直接重建工具栏快照。
// 按发起者 ownerID 精确路由到对应连接（agent 共享多连接物理隔离）。
func (m *Manager) SendSkillRefreshActionAndWait(agentID, ownerID int64, sessionID, actorID string) error {
	if m == nil {
		return ErrSkillRefreshAgentOffline
	}

	actionID := fmt.Sprintf("skill_refresh:%d:%d", agentID, snowflake.GenID())
	params := map[string]any{
		"session_id": sessionID,
		"actor_id":   actorID,
	}

	ch := make(chan *skillLibraryActionResponse, 1)
	pending := &pendingLocalAction{
		actionID:             actionID,
		kind:                 "skill_refresh",
		agentID:              agentID,
		ownerID:              ownerID,
		sessionID:            sessionID,
		actionType:           "skill_refresh",
		skillLibraryResultCh: ch,
	}

	action := protocol.LocalActionPayload{
		ActionID:   actionID,
		ActionType: "skill_refresh",
		Params:     params,
	}

	dispatchedAt := time.Now()
	if !m.sendLocalActionWithPendingForOwner(agentID, ownerID, action, pending) {
		return ErrSkillRefreshNotSupported
	}

	timer := time.NewTimer(skillLibraryActionTimeout)
	defer timer.Stop()

	var resp *skillLibraryActionResponse
	select {
	case resp = <-ch:
	case <-m.stopping():
		select {
		case resp = <-ch:
		default:
			m.deletePendingLocalAction(actionID)
			return ErrSkillRefreshTimeout
		}
	case <-timer.C:
		m.deletePendingLocalAction(actionID)
		logger.L.Warnf(
			"[skill-refresh] timeout action_id=%s agent_id=%d session_id=%s waited=%dms",
			actionID, agentID, sessionID, time.Since(dispatchedAt).Milliseconds(),
		)
		return ErrSkillRefreshTimeout
	}

	if resp == nil {
		return errors.New("agent returned an empty response")
	}
	if resp.Error != "" {
		return errors.New(resp.Error)
	}
	return nil
}

func (m *Manager) handleFileListPendingResult(pending *pendingLocalAction, payload protocol.LocalActionResultPayload) {
	if pending == nil || pending.fileListResultCh == nil {
		return
	}
	status := strings.TrimSpace(payload.Status)
	if status != "ok" {
		errMsg := strings.TrimSpace(payload.ErrorMsg)
		if errMsg == "" {
			errMsg = strings.TrimSpace(payload.ErrorCode)
		}
		if errMsg == "" {
			errMsg = fmt.Sprintf("file_list failed with status: %s", status)
		}
		pending.fileListResultCh <- &fileListResponse{Error: errMsg}
		return
	}
	result := localActionResultObject(payload.Result)
	filesRaw, ok := result["files"]
	if !ok {
		pending.fileListResultCh <- &fileListResponse{Error: "missing files in result"}
		return
	}
	raw, err := json.Marshal(filesRaw)
	if err != nil {
		pending.fileListResultCh <- &fileListResponse{Error: "invalid files data"}
		return
	}
	var files []FileNode
	if err := json.Unmarshal(raw, &files); err != nil {
		pending.fileListResultCh <- &fileListResponse{Error: "invalid file node format"}
		return
	}
	var currentPath string
	if cp, ok := result["current_path"]; ok {
		if s, ok := cp.(string); ok {
			currentPath = s
		}
	}
	var machineName string
	if mn, ok := result["machine_name"]; ok {
		if s, ok := mn.(string); ok {
			machineName = s
		}
	}
	pending.fileListResultCh <- &fileListResponse{Files: files, CurrentPath: currentPath, MachineName: machineName}
}

// SendFileListActionAndWait sends a file_list local_action to the agent and waits for the response.
// SendFileListActionAndWait 按发起者 ownerID 精确路由到对应连接（agent 共享多连接物理隔离）。
// ownerID 为请求者用户 ID；缺失（<=0）非法，直接返回离线错误（fail-closed，不回退主连接）。
func (m *Manager) SendFileListActionAndWait(agentID, ownerID int64, sessionID string, parentID *string, showHidden bool, allowedExtensions []string, actorID string) ([]FileNode, string, string, error) {
	if m == nil {
		return nil, "", "", ErrFileListAgentOffline
	}
	if ownerID <= 0 {
		logger.L.Warnf("[file-list-diag] reject file_list with missing owner: agent_id=%d session_id=%s", agentID, sessionID)
		return nil, "", "", ErrFileListAgentOffline
	}

	actionID := fmt.Sprintf("file_list:%d:%d", agentID, snowflake.GenID())

	params := map[string]any{
		"session_id":  sessionID,
		"show_hidden": showHidden,
		"actor_id":    actorID,
	}
	if len(allowedExtensions) > 0 {
		params["allowed_extensions"] = allowedExtensions
	}
	if parentID != nil {
		params["parent_id"] = *parentID
	}

	ch := make(chan *fileListResponse, 1)
	pending := &pendingLocalAction{
		actionID:         actionID,
		kind:             "file_list",
		agentID:          agentID,
		ownerID:          ownerID,
		sessionID:        sessionID,
		actionType:       "file_list",
		fileListResultCh: ch,
	}

	action := protocol.LocalActionPayload{
		ActionID:   actionID,
		ActionType: "file_list",
		Params:     params,
	}

	// Diagnostic: which path will be used (local conn vs redis forward)?
	// 按发起者 owner 判断本地是否有对应连接（agent 共享多连接物理隔离）。
	hasLocalConn := m.lookupConnForOwner(agentID, ownerID) != nil
	logger.L.Infof(
		"[file-list-diag] mgr -> dispatch action_id=%s agent_id=%d owner_id=%d session_id=%s has_local_conn=%v node_id=%s",
		actionID, agentID, ownerID, sessionID, hasLocalConn, m.getNodeID(),
	)

	dispatchedAt := time.Now()
	// 按发起者 owner 路由到对应连接下发。
	if !m.sendLocalActionWithPendingForOwner(agentID, ownerID, action, pending) {
		logger.L.Warnf(
			"[file-list-diag] mgr !! send failed (not_supported / no_route) action_id=%s agent_id=%d session_id=%s",
			actionID, agentID, sessionID,
		)
		return nil, "", "", ErrFileListNotSupported
	}
	logger.L.Infof(
		"[file-list-diag] mgr -> waiting for agent reply action_id=%s agent_id=%d session_id=%s",
		actionID, agentID, sessionID,
	)

	timer := time.NewTimer(fileListActionTimeout)
	defer timer.Stop()

	var resp *fileListResponse
	select {
	case resp = <-ch:
	case <-m.stopping():
		// 本节点关停：agent 连接已被我们断开，回包不会再来了，别把 Shutdown
		// 拖住整整一个超时周期。但要先看结果是不是已经到了——关停信号与回包
		// 同时就绪时 select 会随机选，直接放弃就把一个已经拿到的结果丢了。
		select {
		case resp = <-ch:
		default:
			m.deletePendingLocalAction(actionID)
			logger.L.Warnf(
				"[file-list-diag] mgr !! node shutting down action_id=%s agent_id=%d session_id=%s waited=%dms",
				actionID, agentID, sessionID, time.Since(dispatchedAt).Milliseconds(),
			)
			return nil, "", "", ErrFileListTimeout
		}
	case <-timer.C:
		m.deletePendingLocalAction(actionID)
		logger.L.Warnf(
			"[file-list-diag] mgr !! timeout action_id=%s agent_id=%d session_id=%s waited=%dms",
			actionID, agentID, sessionID, time.Since(dispatchedAt).Milliseconds(),
		)
		return nil, "", "", ErrFileListTimeout
	}

	waited := time.Since(dispatchedAt)
	if resp == nil || resp.Error != "" {
		errMsg := "agent returned an error"
		if resp != nil && resp.Error != "" {
			errMsg = resp.Error
		}
		logger.L.Warnf(
			"[file-list-diag] mgr << agent err action_id=%s agent_id=%d session_id=%s waited=%dms err=%s",
			actionID, agentID, sessionID, waited.Milliseconds(), errMsg,
		)
		return nil, "", "", errors.New(errMsg)
	}
	logger.L.Infof(
		"[file-list-diag] mgr << agent ok action_id=%s agent_id=%d session_id=%s waited=%dms count=%d current_path=%s machine=%s",
		actionID, agentID, sessionID, waited.Milliseconds(), len(resp.Files), resp.CurrentPath, resp.MachineName,
	)
	return resp.Files, resp.CurrentPath, resp.MachineName, nil
}

// --- list_sessions ---

var (
	ErrSessionListAgentOffline = errors.New("agent not connected")
	ErrSessionListNotSupported = errors.New("agent does not support list_sessions")
	ErrSessionListTimeout      = errors.New("agent did not respond in time")
	sessionListActionTimeout   = 15 * time.Second
)

func (m *Manager) handleSessionListPendingResult(pending *pendingLocalAction, payload protocol.LocalActionResultPayload) {
	if pending == nil || pending.sessionListResultCh == nil {
		return
	}
	status := strings.TrimSpace(payload.Status)
	if status != "ok" {
		errMsg := strings.TrimSpace(payload.ErrorMsg)
		if errMsg == "" {
			errMsg = strings.TrimSpace(payload.ErrorCode)
		}
		if errMsg == "" {
			errMsg = fmt.Sprintf("list_sessions failed with status: %s", status)
		}
		pending.sessionListResultCh <- &sessionListResponse{Error: errMsg}
		return
	}
	result := localActionResultObject(payload.Result)
	sessionsRaw, ok := result["sessions"]
	if !ok {
		pending.sessionListResultCh <- &sessionListResponse{Sessions: []map[string]interface{}{}}
		return
	}
	raw, err := json.Marshal(sessionsRaw)
	if err != nil {
		pending.sessionListResultCh <- &sessionListResponse{Error: "invalid sessions data"}
		return
	}
	var sessions []map[string]interface{}
	if err := json.Unmarshal(raw, &sessions); err != nil {
		pending.sessionListResultCh <- &sessionListResponse{Error: "invalid session format"}
		return
	}
	pending.sessionListResultCh <- &sessionListResponse{Sessions: sessions}
}

// SendSessionListActionAndWait 按发起者 ownerID 精确路由到对应连接（agent 共享多连接物理隔离）。
// ownerID 为 0 时回退主连接，兼容无 owner 上下文的旧调用。
func (m *Manager) SendSessionListActionAndWait(agentID, ownerID int64, sessionID string, actorID string) ([]map[string]interface{}, error) {
	if m == nil {
		return nil, ErrSessionListAgentOffline
	}

	actionID := fmt.Sprintf("list_sessions:%d:%d", agentID, snowflake.GenID())

	params := map[string]any{
		"session_id": sessionID,
		"verb":       "list_sessions",
		"actor_id":   actorID,
	}

	ch := make(chan *sessionListResponse, 1)
	pending := &pendingLocalAction{
		actionID:            actionID,
		kind:                "list_sessions",
		agentID:             agentID,
		ownerID:             ownerID,
		sessionID:           sessionID,
		actionType:          "session_control",
		sessionListResultCh: ch,
	}

	action := protocol.LocalActionPayload{
		ActionID:   actionID,
		ActionType: "session_control",
		Params:     params,
	}

	if !m.sendLocalActionWithPendingForOwner(agentID, ownerID, action, pending) {
		return nil, ErrSessionListNotSupported
	}

	timer := time.NewTimer(sessionListActionTimeout)
	defer timer.Stop()

	var resp *sessionListResponse
	select {
	case resp = <-ch:
	case <-m.stopping():
		// 本节点关停：别把 Shutdown 拖住一个完整超时周期；但先看结果是不是已经到了
		// （关停信号与回包同时就绪时 select 会随机选，直接放弃就把结果丢了）。
		select {
		case resp = <-ch:
		default:
			m.deletePendingLocalAction(actionID)
			return nil, ErrSessionListTimeout
		}
	case <-timer.C:
		m.deletePendingLocalAction(actionID)
		return nil, ErrSessionListTimeout
	}

	if resp == nil || resp.Error != "" {
		errMsg := "agent returned an error"
		if resp != nil && resp.Error != "" {
			errMsg = resp.Error
		}
		return nil, errors.New(errMsg)
	}
	return resp.Sessions, nil
}

// --- session_bind ---

var (
	ErrSessionBindAgentOffline = errors.New("agent not connected")
	ErrSessionBindNotSupported = errors.New("agent does not support session bind")
	ErrSessionBindTimeout      = errors.New("agent did not respond in time")
	sessionBindActionTimeout   = 15 * time.Second
)

func (m *Manager) handleSessionBindPendingResult(pending *pendingLocalAction, payload protocol.LocalActionResultPayload) {
	if pending == nil || pending.sessionBindResultCh == nil {
		return
	}
	status := strings.TrimSpace(payload.Status)
	if status != "ok" {
		errMsg := strings.TrimSpace(payload.ErrorMsg)
		if errMsg == "" {
			errMsg = strings.TrimSpace(payload.ErrorCode)
		}
		if errMsg == "" {
			errMsg = fmt.Sprintf("session bind failed with status: %s", status)
		}
		pending.sessionBindResultCh <- &sessionBindResponse{
			ErrorCode: strings.TrimSpace(payload.ErrorCode),
			ErrorMsg:  errMsg,
		}
		return
	}
	result := localActionResultObject(payload.Result)
	binding := map[string]interface{}{}
	if rawBinding, ok := result["binding"]; ok {
		raw, err := json.Marshal(rawBinding)
		if err == nil {
			_ = json.Unmarshal(raw, &binding)
		}
	}
	if len(binding) == 0 {
		if cwd, ok := result["cwd"].(string); ok && strings.TrimSpace(cwd) != "" {
			binding["cwd"] = cwd
		}
	}
	resp := &sessionBindResponse{Binding: binding}
	resp.ProviderKey = firstNonEmptyString(binding["providerKey"], binding["provider_key"])
	resp.BindingID = firstNonEmptyString(binding["bindingId"], binding["binding_id"])
	resp.AgentSessionID = firstNonEmptyString(binding["agentSessionId"], binding["agent_session_id"], resp.BindingID)
	resp.Cwd = firstNonEmptyString(binding["cwd"], result["cwd"])
	resp.WorkerStatus = firstNonEmptyString(binding["workerStatus"], binding["worker_status"])
	if resp.WorkerStatus == "" {
		resp.WorkerStatus = "ready"
	}
	pending.sessionBindResultCh <- resp
}

func firstNonEmptyString(values ...interface{}) string {
	for _, value := range values {
		if text := strings.TrimSpace(fmt.Sprint(value)); text != "" && text != "<nil>" {
			return text
		}
	}
	return ""
}

// SendSessionBindActionAndWait 按发起者 ownerID 精确路由到对应连接（agent 共享多连接物理隔离）。
// 这条路径是 dispatch_agent 给目标 agent 绑定 cwd 的关键链路:被共享者 B 派活时,
// 必须把绑定动作下发到 B 的 connector 实例,而不是主人 A 的。
// ownerID<=0 非法，直接返回离线错误（fail-closed，不回退主连接）。
func (m *Manager) SendSessionBindActionAndWait(agentID, ownerID int64, aibotSessionID string, actorID string, cwd string, providerKey string, agentSessionID string) (*sessionBindResponse, error) {
	if m == nil {
		return nil, ErrSessionBindAgentOffline
	}
	if ownerID <= 0 {
		logger.L.Warnf("reject session_bind with missing owner: agent_id=%d session=%s", agentID, strings.TrimSpace(aibotSessionID))
		return nil, ErrSessionBindAgentOffline
	}

	actionID := fmt.Sprintf("session_bind:%d:%s:%d", agentID, strings.TrimSpace(aibotSessionID), snowflake.GenID())
	params := map[string]any{
		"session_id": strings.TrimSpace(aibotSessionID),
		"verb":       "open",
		"cwd":        strings.TrimSpace(cwd),
		"actor_id":   actorID,
	}
	if providerKey = strings.TrimSpace(providerKey); providerKey != "" {
		params["provider_key"] = providerKey
	}
	if agentSessionID = strings.TrimSpace(agentSessionID); agentSessionID != "" {
		params["agent_session_id"] = agentSessionID
	}

	ch := make(chan *sessionBindResponse, 1)
	pending := &pendingLocalAction{
		actionID:            actionID,
		kind:                "session_bind",
		agentID:             agentID,
		ownerID:             ownerID,
		sessionID:           strings.TrimSpace(aibotSessionID),
		actionType:          "session_control",
		sessionBindResultCh: ch,
	}

	action := protocol.LocalActionPayload{
		ActionID:   actionID,
		ActionType: "session_control",
		Params:     params,
		TimeoutMs:  15_000,
	}

	if !m.sendLocalActionWithPendingForOwner(agentID, ownerID, action, pending) {
		return nil, ErrSessionBindNotSupported
	}

	timer := time.NewTimer(sessionBindActionTimeout)
	defer timer.Stop()

	var resp *sessionBindResponse
	select {
	case resp = <-ch:
	case <-m.stopping():
		// 本节点关停：别把 Shutdown 拖住一个完整超时周期；但先看结果是不是已经到了
		// （关停信号与回包同时就绪时 select 会随机选，直接放弃就把结果丢了）。
		select {
		case resp = <-ch:
		default:
			m.deletePendingLocalAction(actionID)
			return nil, ErrSessionBindTimeout
		}
	case <-timer.C:
		m.deletePendingLocalAction(actionID)
		return nil, ErrSessionBindTimeout
	}

	if resp == nil {
		return nil, errors.New("agent returned empty bind response")
	}
	if strings.TrimSpace(resp.ErrorMsg) != "" || strings.TrimSpace(resp.ErrorCode) != "" {
		return resp, errors.New(firstNonEmptyString(resp.ErrorMsg, resp.ErrorCode))
	}
	return resp, nil
}

// --- session_history_sync ---

var (
	ErrSessionHistorySyncAgentOffline = errors.New("agent not connected")
	ErrSessionHistorySyncNotSupported = errors.New("agent does not support session history sync")
	ErrSessionHistorySyncTimeout      = errors.New("agent did not respond in time")
	sessionHistorySyncActionTimeout   = 30 * time.Second
)

const SessionHistorySyncErrorInvalidCursor = "history_invalid_cursor"

type SessionHistorySyncError struct {
	Code    string
	Message string
}

func (e *SessionHistorySyncError) Error() string {
	if e == nil {
		return ""
	}
	return firstNonEmptyString(strings.TrimSpace(e.Message), strings.TrimSpace(e.Code))
}

type SessionHistorySyncResponse struct {
	Messages   []agentsync.NativeMessage `json:"messages,omitempty"`
	HasMore    bool                      `json:"has_more,omitempty"`
	NextCursor string                    `json:"next_cursor,omitempty"`
	SyncRunID  string                    `json:"sync_run_id,omitempty"`
	ErrorCode  string                    `json:"error_code,omitempty"`
	ErrorMsg   string                    `json:"error_msg,omitempty"`
}

func sessionHistorySyncResponseError(resp *SessionHistorySyncResponse) error {
	if resp == nil || (strings.TrimSpace(resp.ErrorMsg) == "" && strings.TrimSpace(resp.ErrorCode) == "") {
		return nil
	}
	return &SessionHistorySyncError{
		Code:    strings.TrimSpace(resp.ErrorCode),
		Message: strings.TrimSpace(resp.ErrorMsg),
	}
}

func (m *Manager) handleSessionHistorySyncPendingResult(pending *pendingLocalAction, payload protocol.LocalActionResultPayload) {
	if pending == nil || pending.sessionSyncResultCh == nil {
		return
	}
	status := strings.TrimSpace(payload.Status)
	if status != "ok" {
		errMsg := strings.TrimSpace(payload.ErrorMsg)
		if errMsg == "" {
			errMsg = strings.TrimSpace(payload.ErrorCode)
		}
		if errMsg == "" {
			errMsg = fmt.Sprintf("session history sync failed with status: %s", status)
		}
		pending.sessionSyncResultCh <- &SessionHistorySyncResponse{
			ErrorCode: strings.TrimSpace(payload.ErrorCode),
			ErrorMsg:  errMsg,
		}
		return
	}
	raw, err := json.Marshal(payload.Result)
	if err != nil {
		pending.sessionSyncResultCh <- &SessionHistorySyncResponse{ErrorMsg: err.Error()}
		return
	}
	var resp SessionHistorySyncResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		pending.sessionSyncResultCh <- &SessionHistorySyncResponse{ErrorMsg: err.Error()}
		return
	}
	pending.sessionSyncResultCh <- &resp
}

// SendSessionHistorySyncActionAndWait asks the owner's connector instance to
// page provider-native conversation history for an already-bound aibot session.
func (m *Manager) SendSessionHistorySyncActionAndWait(
	agentID, ownerID int64,
	aibotSessionID string,
	actorID string,
	cwd string,
	providerKey string,
	agentSessionID string,
	cursor string,
	limit int,
	syncRunID string,
) (*SessionHistorySyncResponse, error) {
	if m == nil {
		return nil, ErrSessionHistorySyncAgentOffline
	}
	if ownerID <= 0 {
		logger.L.Warnf("reject session history sync with missing owner: agent_id=%d session=%s", agentID, strings.TrimSpace(aibotSessionID))
		return nil, ErrSessionHistorySyncAgentOffline
	}
	if limit <= 0 {
		limit = 100
	}
	actionID := fmt.Sprintf("session_history_sync:%d:%s:%d", agentID, strings.TrimSpace(aibotSessionID), snowflake.GenID())
	params := map[string]any{
		"session_id":       strings.TrimSpace(aibotSessionID),
		"verb":             "sync_history",
		"actor_id":         strings.TrimSpace(actorID),
		"provider_key":     strings.TrimSpace(providerKey),
		"agent_session_id": strings.TrimSpace(agentSessionID),
		"cwd":              strings.TrimSpace(cwd),
		"cursor":           strings.TrimSpace(cursor),
		"limit":            limit,
		"sync_run_id":      strings.TrimSpace(syncRunID),
	}

	ch := make(chan *SessionHistorySyncResponse, 1)
	pending := &pendingLocalAction{
		actionID:            actionID,
		kind:                "session_history_sync",
		agentID:             agentID,
		ownerID:             ownerID,
		sessionID:           strings.TrimSpace(aibotSessionID),
		actionType:          "session_control",
		timeoutMs:           30_000,
		sessionSyncResultCh: ch,
	}
	action := protocol.LocalActionPayload{
		ActionID:   actionID,
		ActionType: "session_control",
		Params:     params,
		TimeoutMs:  30_000,
	}
	if !m.sendLocalActionWithPendingForOwner(agentID, ownerID, action, pending) {
		return nil, ErrSessionHistorySyncNotSupported
	}

	timer := time.NewTimer(sessionHistorySyncActionTimeout)
	defer timer.Stop()

	var resp *SessionHistorySyncResponse
	select {
	case resp = <-ch:
	case <-m.stopping():
		select {
		case resp = <-ch:
		default:
			m.deletePendingLocalAction(actionID)
			return nil, ErrSessionHistorySyncTimeout
		}
	case <-timer.C:
		m.deletePendingLocalAction(actionID)
		return nil, ErrSessionHistorySyncTimeout
	}
	if resp == nil {
		return nil, errors.New("agent returned empty session history sync response")
	}
	if err := sessionHistorySyncResponseError(resp); err != nil {
		return resp, err
	}
	return resp, nil
}

// --- create_folder ---

type createFolderResponse struct {
	Folder *FileNode `json:"folder,omitempty"`
	Error  string    `json:"error,omitempty"`
}

var (
	ErrCreateFolderAgentOffline = errors.New("agent not connected")
	ErrCreateFolderTimeout      = errors.New("agent did not respond in time")
	ErrCreateFolderNotSupported = errors.New("agent does not support create_folder")
)

const createFolderActionTimeout = 15 * time.Second

func (m *Manager) handleCreateFolderPendingResult(pending *pendingLocalAction, payload protocol.LocalActionResultPayload) {
	if pending == nil || pending.createFolderResultCh == nil {
		return
	}
	status := strings.TrimSpace(payload.Status)
	if status != "ok" {
		errMsg := strings.TrimSpace(payload.ErrorMsg)
		if errMsg == "" {
			errMsg = strings.TrimSpace(payload.ErrorCode)
		}
		if errMsg == "" {
			errMsg = fmt.Sprintf("create_folder failed with status: %s", status)
		}
		pending.createFolderResultCh <- &createFolderResponse{Error: errMsg}
		return
	}
	result := localActionResultObject(payload.Result)
	folderRaw, ok := result["folder"]
	if !ok {
		pending.createFolderResultCh <- &createFolderResponse{Error: "missing folder in result"}
		return
	}
	raw, err := json.Marshal(folderRaw)
	if err != nil {
		pending.createFolderResultCh <- &createFolderResponse{Error: "invalid folder data"}
		return
	}
	var folder FileNode
	if err := json.Unmarshal(raw, &folder); err != nil {
		pending.createFolderResultCh <- &createFolderResponse{Error: "invalid folder node format"}
		return
	}
	pending.createFolderResultCh <- &createFolderResponse{Folder: &folder}
}

// SendCreateFolderActionAndWait 按发起者 ownerID 精确路由到对应连接（agent 共享多连接物理隔离）。
// 被共享者 B 建文件夹必须落到 B 自己的 connector 实例（B 的文件系统视图），
// 而不是主人 A 的机器。ownerID 为 0 时回退主连接。
func (m *Manager) SendCreateFolderActionAndWait(agentID, ownerID int64, sessionID string, parentID *string, name string, actorID string) (*FileNode, error) {
	if m == nil {
		return nil, ErrCreateFolderAgentOffline
	}

	actionID := fmt.Sprintf("create_folder:%d:%d", agentID, snowflake.GenID())

	params := map[string]any{
		"session_id": sessionID,
		"name":       name,
		"actor_id":   actorID,
	}
	if parentID != nil {
		params["parent_id"] = *parentID
	}

	ch := make(chan *createFolderResponse, 1)
	pending := &pendingLocalAction{
		actionID:             actionID,
		kind:                 "create_folder",
		agentID:              agentID,
		ownerID:              ownerID,
		sessionID:            sessionID,
		actionType:           "create_folder",
		createFolderResultCh: ch,
	}

	action := protocol.LocalActionPayload{
		ActionID:   actionID,
		ActionType: "create_folder",
		Params:     params,
	}

	if !m.sendLocalActionWithPendingForOwner(agentID, ownerID, action, pending) {
		return nil, ErrCreateFolderNotSupported
	}

	timer := time.NewTimer(createFolderActionTimeout)
	defer timer.Stop()

	var resp *createFolderResponse
	select {
	case resp = <-ch:
	case <-m.stopping():
		// 本节点关停：别把 Shutdown 拖住一个完整超时周期；但先看结果是不是已经到了
		// （关停信号与回包同时就绪时 select 会随机选，直接放弃就把结果丢了）。
		select {
		case resp = <-ch:
		default:
			m.deletePendingLocalAction(actionID)
			return nil, ErrCreateFolderTimeout
		}
	case <-timer.C:
		m.deletePendingLocalAction(actionID)
		return nil, ErrCreateFolderTimeout
	}

	if resp == nil || resp.Error != "" {
		errMsg := "agent returned an error"
		if resp != nil && resp.Error != "" {
			errMsg = resp.Error
		}
		return nil, errors.New(errMsg)
	}
	return resp.Folder, nil
}
