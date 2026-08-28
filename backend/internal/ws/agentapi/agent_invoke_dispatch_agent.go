package agentapi

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/askie/grix/backend/internal/agenttoolbar"
	toolruntime "github.com/askie/grix/backend/internal/agenttoolbar/runtime"
	toolstore "github.com/askie/grix/backend/internal/agenttoolbar/store"
	"github.com/askie/grix/backend/internal/api/service"
	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/textutil"
	"github.com/askie/grix/backend/internal/store"
	"github.com/askie/grix/backend/internal/ws/protocol"
)

// dispatchTitleMaxRunes 是会话标题的最大字数：既是调用方显式 title 的上限，
// 也是从任务文本兜底截取标题时的截断长度。title 只是标签，不允许把任务正文塞进来。
const dispatchTitleMaxRunes = 40

// dispatchDispatchAgent 派一个 owner 名下的 agent 去指定工作目录干活。
//   - openclaw / hermes：建/复用主人↔agent 私聊，把"工作目录+任务"组织成提示词以主人身份发送。
//   - claude / codex / 其他：建/复用绑定会话，绑定工作目录，成功后把任务以主人身份发送。
func dispatchDispatchAgent(callerAgentID, ownerID int64, params map[string]interface{}, hooks agentInvokeHooks) (interface{}, int, string) {

	targetAgentID, ok := paramInt64(params, "agent_id")
	if !ok || targetAgentID <= 0 {
		return nil, 4001, "agent_id required"
	}
	cwd, ok := paramString(params, "cwd")
	if !ok || strings.TrimSpace(cwd) == "" {
		return nil, 4001, "cwd required"
	}
	cwd = strings.TrimSpace(cwd)
	task, ok := paramString(params, "task")
	if !ok || strings.TrimSpace(task) == "" {
		return nil, 4001, "task required"
	}
	task = strings.TrimSpace(task)
	// title 可选：调用方提炼的任务标题，没传则从任务文本兜底截取。
	// 显式 title 超长直接拒绝——title 只是标签，超长说明调用方把任务正文错放进了 title。
	title, _ := paramString(params, "title")
	title = strings.TrimSpace(title)
	if utf8.RuneCountInString(title) > dispatchTitleMaxRunes {
		return nil, 4001, fmt.Sprintf("title too long: max %d runes", dispatchTitleMaxRunes)
	}
	if title == "" {
		title = deriveTitleFromTask(task)
	}
	if hooks.sendMessage == nil {
		return nil, 5001, "message handler unavailable"
	}

	// 校验目标 agent 属于本 owner 且可用。
	var agent model.Agent
	if err := store.DB.Select("id", "owner_id", "status", "agent_client_type").
		First(&agent, targetAgentID).Error; err != nil {
		return nil, 4004, "agent not found"
	}
	if agent.OwnerID != ownerID {
		return nil, 4003, "agent not owned by you"
	}
	if agent.Status != model.AgentStatusActive {
		return nil, 4002, "agent unavailable"
	}
	if dispatchTaskRequiresQuoteCallback(task) {
		profile, found, err := toolruntime.LoadProfile(context.Background(), targetAgentID)
		if err != nil {
			return nil, 5001, err.Error()
		}
		if !found || !hasDeclaredName(profile.Capabilities, protocol.AgentAPISessionSendQuoteCapability) {
			return nil, 4002, "target agent runtime does not support quoted dispatch callbacks"
		}
	}

	clientType := model.NormalizeAgentClientType(agent.AgentClientType)
	switch clientType {
	case model.AgentClientTypeOpenClaw, model.AgentClientTypeHermes:
		return dispatchAgentViaPrompt(ownerID, callerAgentID, targetAgentID, cwd, task, title, hooks)
	default:
		return dispatchAgentViaBinding(ownerID, callerAgentID, targetAgentID, clientType, cwd, task, title, hooks)
	}
}

func dispatchTaskRequiresQuoteCallback(task string) bool {
	return strings.Contains(task, "report_dispatch_result") && strings.Contains(task, "quoted_message_id")
}

// deriveTitleFromTask 在调用方未提供 title 时，从任务文本截取核心表达作为会话标题兜底（取首行非空文本）。
func deriveTitleFromTask(task string) string {
	for _, line := range strings.Split(task, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		return textutil.TruncateRunes(line, dispatchTitleMaxRunes)
	}
	return textutil.TruncateRunes(strings.TrimSpace(task), dispatchTitleMaxRunes)
}

// dispatchOriginAgentID 派发任务的 origin 是调用方 agent，路由层据此只排除调用方。
// 调用方把任务派给自己时不打 origin，否则目标会被当成发出者跳过而收不到任务。
func dispatchOriginAgentID(callerAgentID, targetAgentID int64) int64 {
	if callerAgentID == targetAgentID {
		return 0
	}
	return callerAgentID
}

// dispatchAgentViaPrompt 处理 openclaw / hermes：每次新建独立私聊后把工作目录与任务组织成提示词发送。
func dispatchAgentViaPrompt(ownerID, callerAgentID, targetAgentID int64, cwd, task, title string, hooks agentInvokeHooks) (interface{}, int, string) {
	created, err := service.SessionCreateForAgentDispatch(ownerID, targetAgentID, title)
	if err != nil {
		return nil, 5001, err.Error()
	}
	prompt := fmt.Sprintf("工作目录：%s\n\n%s", cwd, task)
	result, err := sendAsOwner(targetAgentID, dispatchOriginAgentID(callerAgentID, targetAgentID), ownerID, created.SessionID, prompt, 0, hooks)
	if err != nil {
		return nil, 5001, err.Error()
	}
	return map[string]interface{}{
		"session_id": created.SessionID,
		"msg_id":     result.MsgID,
		"mode":       "prompt",
	}, 0, ""
}

// dispatchAgentViaBinding 处理 claude / codex / 其他：每次新建独立会话，绑定工作目录，成功后发送任务。
func dispatchAgentViaBinding(ownerID, callerAgentID, targetAgentID int64, clientType, cwd, task, title string, hooks agentInvokeHooks) (interface{}, int, string) {
	if hooks.bindSession == nil {
		return nil, 5001, "bind handler unavailable"
	}
	providerKey := dispatchProviderKey(clientType)
	ctx := context.Background()

	// 每次派发新建独立会话，不再按 cwd 复用历史会话。
	created, err := service.SessionCreateForAgentDispatch(ownerID, targetAgentID, title)
	if err != nil {
		return nil, 5001, err.Error()
	}
	sessionID := created.SessionID

	_ = toolstore.UpsertBinding(ctx, toolstore.BindingRecord{
		AgentID:      targetAgentID,
		SessionID:    sessionID,
		ProviderKey:  providerKey,
		Cwd:          cwd,
		Status:       "pending",
		WorkerStatus: "pending",
		Meta:         map[string]any{"requested_cwd": cwd},
	})

	bindResp, err := hooks.bindSession(targetAgentID, sessionID, strconv.FormatInt(ownerID, 10), cwd, providerKey)
	if err != nil {
		status := "failed"
		code := 5001
		switch {
		case errors.Is(err, ErrSessionBindAgentOffline), errors.Is(err, ErrSessionBindNotSupported):
			code = 4002
		case errors.Is(err, ErrSessionBindTimeout):
			status = "pending"
			code = 4290
		}
		_ = toolstore.UpsertBinding(ctx, toolstore.BindingRecord{
			AgentID:      targetAgentID,
			SessionID:    sessionID,
			ProviderKey:  providerKey,
			Cwd:          cwd,
			Status:       status,
			WorkerStatus: status,
			Meta:         map[string]any{"error": err.Error()},
		})
		return nil, code, "目录绑定失败: " + err.Error()
	}

	finalProviderKey := firstNonEmptyString(bindResp.ProviderKey, providerKey)
	finalCwd := firstNonEmptyString(bindResp.Cwd, cwd)
	bindingID := firstNonEmptyString(bindResp.BindingID, bindResp.AgentSessionID)
	workerStatus := firstNonEmptyString(bindResp.WorkerStatus, "ready")
	_ = toolstore.UpsertBinding(ctx, toolstore.BindingRecord{
		AgentID:      targetAgentID,
		SessionID:    sessionID,
		ProviderKey:  finalProviderKey,
		BindingID:    bindingID,
		Cwd:          finalCwd,
		Status:       "active",
		WorkerStatus: workerStatus,
		Meta:         map[string]any{"agent_session_id": bindingID},
	})
	if svc := agenttoolbar.GetGlobal(); svc != nil {
		_ = svc.RefreshSession(ctx, ownerID, sessionID, "dispatch_agent")
	}

	result, err := sendAsOwner(targetAgentID, dispatchOriginAgentID(callerAgentID, targetAgentID), ownerID, sessionID, task, 0, hooks)
	if err != nil {
		return nil, 5001, "目录已绑定但任务发送失败: " + err.Error()
	}
	return map[string]interface{}{
		"session_id": sessionID,
		"msg_id":     result.MsgID,
		"mode":       "binding",
		"cwd":        finalCwd,
	}, 0, ""
}

// dispatchProviderKey 把 agent client type 映射为绑定用 provider_key（与 ws session-bind handler 一致）。
func dispatchProviderKey(clientType string) string {
	switch model.NormalizeAgentClientType(clientType) {
	case model.AgentClientTypeClaude:
		return "claude"
	case model.AgentClientTypeCodex:
		return "codex"
	case model.AgentClientTypePi:
		return "pi"
	case model.AgentClientTypeCodeWhale:
		return "codewhale"
	default:
		return "acp"
	}
}
