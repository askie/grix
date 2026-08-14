package agentapi

import (
	"strings"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/store"
)

type delegateEventInterceptor struct {
	name   string
	family string
	handle func(evt DelegateEventPayload) bool
}

func (m *Manager) registerDefaultDelegateEventInterceptors() {
	m.delegateEventInterceptors = nil
	// 访问审批回传必须最先注册：其 request_id 带 access: 前缀，需在各家 question
	// 流之前消费，避免被 family 级 question 处理器误报「无待处理问题」。
	m.registerDelegateEventInterceptor(
		"access_approval_reply",
		"",
		m.tryHandleAccessApprovalReply,
	)
	m.registerDelegateEventInterceptor(
		"gemini_open_session_submit",
		"gemini",
		m.tryHandleGeminiOpenSessionSubmit,
	)
	m.registerDelegateEventInterceptor(
		"gemini_question_reply",
		"gemini",
		m.tryHandleGeminiQuestionReply,
	)
	m.registerDelegateEventInterceptor(
		"session_control",
		"",
		m.tryHandleSessionControlCommand,
	)
	m.registerDelegateEventInterceptor(
		"claude_question_command",
		"claude",
		m.tryHandleClaudeQuestionCommand,
	)
	m.registerDelegateEventInterceptor(
		"exec_approval_command",
		"",
		m.tryHandleExecApprovalCommand,
	)
	m.registerDelegateEventInterceptor(
		"gemini_question_requirement",
		"gemini",
		m.tryHandleGeminiQuestionRequirement,
	)
}

func (m *Manager) registerDelegateEventInterceptor(name, family string, handle func(evt DelegateEventPayload) bool) {
	if m == nil || handle == nil {
		return
	}
	m.delegateEventInterceptors = append(m.delegateEventInterceptors, delegateEventInterceptor{
		name:   strings.TrimSpace(name),
		family: normalizeDelegateEventFamily(family),
		handle: handle,
	})
}

func (m *Manager) tryInterceptDelegateEvent(evt DelegateEventPayload) bool {
	if m == nil || evt.IsRecordOnly() || len(m.delegateEventInterceptors) == 0 {
		return false
	}

	family := m.resolveDelegateEventFamily(evt.AgentID)
	for _, interceptor := range m.delegateEventInterceptors {
		if interceptor.handle == nil || !matchDelegateEventFamily(family, interceptor.family) {
			continue
		}
		if interceptor.handle(evt) {
			return true
		}
	}

	return false
}

func (m *Manager) resolveDelegateEventFamily(agentID int64) string {
	if m == nil || agentID <= 0 {
		return ""
	}
	conn := m.lookupConn(agentID)
	if conn != nil {
		if family := normalizeDelegateEventFamily(conn.adapterID); family != "" {
			return family
		}
		if family := normalizeDelegateEventFamily(conn.clientType); family != "" {
			return family
		}
	}

	if store.DB == nil {
		return ""
	}
	var agent model.Agent
	if err := store.DB.Select("id,agent_client_type").First(&agent, agentID).Error; err != nil {
		return ""
	}
	return normalizeDelegateEventFamily(agent.AgentClientType)
}

func matchDelegateEventFamily(currentFamily, interceptorFamily string) bool {
	// Empty family means generic interceptor.
	if interceptorFamily == "" {
		return true
	}
	// Keep backward compatibility: when family is unknown, still let
	// interceptors run and rely on their own internal guards.
	if currentFamily == "" {
		return true
	}
	return currentFamily == interceptorFamily
}

func normalizeDelegateEventFamily(value string) string {
	normalized := strings.ToLower(strings.TrimSpace(value))
	if normalized == "" {
		return ""
	}
	if slash := strings.Index(normalized, "/"); slash > 0 {
		return strings.TrimSpace(normalized[:slash])
	}
	return normalized
}
