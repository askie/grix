package agentapi

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	tooli18n "github.com/askie/grix/backend/internal/agenttoolbar/i18n"
	"github.com/askie/grix/backend/internal/notification"
	"github.com/askie/grix/backend/internal/pkg/logger"
	"github.com/askie/grix/backend/internal/ws/protocol"
)

const execApprovalUsage = "Usage: /grix approval <id> allow|deny\n/approve <id> allow|deny"

var execApprovalDecisionAliases = map[string]string{
	"allow":        "allow",
	"once":         "allow",
	"allow-once":   "allow-once",
	"allowonce":    "allow-once",
	"always":       "allow-always",
	"allow-always": "allow-always",
	"allowalways":  "allow-always",
	"deny":         "deny",
	"reject":       "deny",
	"block":        "deny",
}

type parsedExecApprovalCommand struct {
	matched           bool
	ok                bool
	err               string
	id                string
	approvalID        string
	approvalCommandID string
	decision          string
	ruleIndex         string
	reason            string
}

func parseExecApprovalCommand(raw string) parsedExecApprovalCommand {
	if parsed := parseExecApprovalResolutionDirective(raw); parsed.matched {
		return parsed
	}

	trimmed := strings.TrimSpace(raw)
	rest := ""
	if strings.HasPrefix(strings.ToLower(trimmed), "/approve") {
		rest = strings.TrimSpace(trimmed[len("/approve"):])
	} else {
		fields := strings.Fields(trimmed)
		if len(fields) < 2 || !((fields[0] == "/grix" || fields[0] == "grix") && strings.EqualFold(fields[1], "approval")) {
			return parsedExecApprovalCommand{}
		}
		rest = strings.TrimSpace(strings.Join(fields[2:], " "))
	}

	if strings.HasPrefix(rest, "@") {
		fields := strings.Fields(rest)
		if len(fields) == 0 {
			return parsedExecApprovalCommand{matched: true, err: execApprovalUsage}
		}
		rest = strings.TrimSpace(strings.TrimPrefix(rest, fields[0]))
	}
	if rest == "" {
		return parsedExecApprovalCommand{matched: true, err: execApprovalUsage}
	}

	tokens := strings.Fields(rest)
	if len(tokens) < 2 {
		return parsedExecApprovalCommand{matched: true, err: execApprovalUsage}
	}

	if decision, ruleIndex, consumed, ok := parseExecApprovalDecision(tokens, 0); ok {
		id := strings.TrimSpace(strings.Join(tokens[consumed:], " "))
		if id == "" {
			return parsedExecApprovalCommand{matched: true, err: execApprovalUsage}
		}
		return parsedExecApprovalCommand{
			matched:           true,
			ok:                true,
			id:                id,
			approvalCommandID: id,
			decision:          decision,
			ruleIndex:         ruleIndex,
		}
	}

	decision, ruleIndex, consumed, ok := parseExecApprovalDecision(tokens, 1)
	if !ok {
		return parsedExecApprovalCommand{matched: true, err: execApprovalUsage}
	}
	id := strings.TrimSpace(tokens[0])
	if id == "" {
		return parsedExecApprovalCommand{matched: true, err: execApprovalUsage}
	}
	reason := ""
	if consumed < len(tokens) {
		reason = strings.TrimSpace(strings.Join(tokens[consumed:], " "))
	}
	return parsedExecApprovalCommand{
		matched:           true,
		ok:                true,
		id:                id,
		approvalCommandID: id,
		decision:          decision,
		ruleIndex:         ruleIndex,
		reason:            reason,
	}
}

func parseExecApprovalResolutionDirective(raw string) parsedExecApprovalCommand {
	const (
		prefix = "[[exec-approval-resolution|"
		suffix = "]]"
	)
	// 整条消息必须就是这条指令：卡片按钮生成的回传消息只含指令本身，标记是固定字面量。
	// 用「包含即匹配」会把任何正文里提到该标记的普通消息也吞成审批指令，用户消息因此
	// 永远送不到 agent，只换回一句 usage 提示；在小写串上取下标却切原串，还会被
	// 小写后字节变短的字符（如 İ）错位，把真实审批切坏。与 handler 侧解析口径保持一致。
	text := strings.TrimSpace(raw)
	if !strings.HasPrefix(text, prefix) || !strings.HasSuffix(text, suffix) {
		return parsedExecApprovalCommand{}
	}

	body := strings.TrimSpace(text[len(prefix) : len(text)-len(suffix)])
	if body == "" {
		return parsedExecApprovalCommand{matched: true, err: execApprovalUsage}
	}

	payload := make(map[string]string)
	for _, segment := range strings.Split(body, "|") {
		segment = strings.TrimSpace(segment)
		if segment == "" {
			return parsedExecApprovalCommand{matched: true, err: execApprovalUsage}
		}
		separator := strings.Index(segment, "=")
		if separator <= 0 || separator >= len(segment)-1 {
			return parsedExecApprovalCommand{matched: true, err: execApprovalUsage}
		}
		key := strings.TrimSpace(segment[:separator])
		value := decodeDirectiveValue(segment[separator+1:])
		if key == "" || value == "" {
			return parsedExecApprovalCommand{matched: true, err: execApprovalUsage}
		}
		payload[key] = value
	}

	decision, ruleIndex, ok := parseExecApprovalDecisionValue(
		strings.TrimSpace(payload["decision"]),
		strings.TrimSpace(payload["rule_index"]),
	)
	approvalID := strings.TrimSpace(payload["approval_id"])
	approvalCommandID := strings.TrimSpace(payload["approval_command_id"])
	if approvalCommandID == "" {
		approvalCommandID = approvalID
	}
	if approvalCommandID == "" {
		approvalCommandID = strings.TrimSpace(payload["approval_slug"])
	}
	if !ok || approvalCommandID == "" {
		return parsedExecApprovalCommand{matched: true, err: execApprovalUsage}
	}

	return parsedExecApprovalCommand{
		matched:           true,
		ok:                true,
		id:                approvalCommandID,
		approvalID:        approvalID,
		approvalCommandID: approvalCommandID,
		decision:          decision,
		ruleIndex:         ruleIndex,
		reason:            strings.TrimSpace(payload["reason"]),
	}
}

func parseExecApprovalDecision(tokens []string, start int) (decision string, ruleIndex string, consumed int, ok bool) {
	if start < 0 || start >= len(tokens) {
		return "", "", 0, false
	}
	value := strings.ToLower(strings.TrimSpace(tokens[start]))
	if mapped, exists := execApprovalDecisionAliases[value]; exists {
		return mapped, "", start + 1, true
	}
	if strings.HasPrefix(value, "allow-rule:") {
		ruleIndex = strings.TrimSpace(strings.TrimPrefix(value, "allow-rule:"))
		if ruleIndex == "" {
			return "", "", 0, false
		}
		return "allow-rule", ruleIndex, start + 1, true
	}
	if value != "allow-rule" {
		return "", "", 0, false
	}
	if start+1 >= len(tokens) {
		return "", "", 0, false
	}
	ruleIndex = strings.TrimSpace(tokens[start+1])
	if ruleIndex == "" {
		return "", "", 0, false
	}
	return "allow-rule", ruleIndex, start + 2, true
}

func parseExecApprovalDecisionValue(rawDecision string, rawRuleIndex string) (decision string, ruleIndex string, ok bool) {
	value := strings.ToLower(strings.TrimSpace(rawDecision))
	if mapped, exists := execApprovalDecisionAliases[value]; exists {
		return mapped, "", true
	}
	if strings.HasPrefix(value, "allow-rule:") {
		ruleIndex = strings.TrimSpace(strings.TrimPrefix(value, "allow-rule:"))
		if ruleIndex == "" {
			return "", "", false
		}
		return "allow-rule", ruleIndex, true
	}
	if value != "allow-rule" {
		return "", "", false
	}
	ruleIndex = strings.TrimSpace(rawRuleIndex)
	if ruleIndex == "" {
		return "", "", false
	}
	return "allow-rule", ruleIndex, true
}

func decodeDirectiveValue(raw string) string {
	value := strings.TrimSpace(raw)
	if value == "" {
		return ""
	}
	if !strings.Contains(value, "%") {
		return value
	}
	decoded, err := url.QueryUnescape(value)
	if err != nil {
		return value
	}
	return strings.TrimSpace(decoded)
}

func (m *Manager) tryHandleExecApprovalCommand(evt DelegateEventPayload) bool {
	parsed := parseExecApprovalCommand(evt.Content)
	if !parsed.matched {
		return false
	}
	// 只有 agent 主人本人有权裁决审批。托管代答/群聊场景下，客户或其他群成员
	// 可能伪造回传（卡片按钮指令本质是一条普通消息），不放行就必须吞掉——
	// 返回 true 避免指令被当作用户消息转发给 agent。
	if evt.SenderID > 0 && evt.OwnerID > 0 && evt.SenderID != evt.OwnerID {
		logger.L.Warnf(
			"exec approval resolution rejected: sender is not the agent owner session=%s agent=%d sender=%d owner=%d",
			evt.SessionID, evt.AgentID, evt.SenderID, evt.OwnerID,
		)
		return true
	}
	if !parsed.ok {
		// 审批命令来自 delegate 事件，evt.OwnerID 即发起者，按其连接做能力检查与下发。
		if m.canHandleExecApprovalReply(evt.AgentID, evt.OwnerID) {
			return m.sendPendingLocalActionReply(pendingLocalAction{
				agentID:         evt.AgentID,
				ownerID:         evt.OwnerID,
				sessionID:       evt.SessionID,
				threadID:        evt.ThreadID,
				quotedMessageID: evt.MsgID,
			}, pendingLocalActionReply{content: parsed.err})
		}
		return false
	}

	// 主人已裁决：立刻打标记，让还在路上（积压 / 重投）的审批推送不再弹出。
	notification.MarkApprovalResolved(context.Background(), evt.SessionID, parsed.approvalCommandID)

	action, pending, ok := m.buildExecApprovalReplyAction(evt, parsed)
	if !ok {
		// Swallow the directive so it isn't forwarded to the agent as a user message.
		// (This can happen in group sessions when the event reaches an agent that
		// didn't issue the approval card — e.g., the reverse-lookup bypassed filtering
		// but the card belongs to a different agent.)
		return true
	}
	// 按发起者 owner 路由到对应连接下发（agent 共享多连接物理隔离）。
	if m.sendLocalActionWithPendingForOwner(evt.AgentID, evt.OwnerID, action, pending) {
		return true
	}
	// Agent is not reachable (disconnected or no node holds the connection).
	// Immediately update the approval card so the frontend loading state clears.
	if pending != nil {
		logger.L.Warnf(
			"exec approval local_action undeliverable agent=%d session=%s approval_command_id=%s — updating card as unavailable",
			evt.AgentID, evt.SessionID, parsed.approvalCommandID,
		)
		reply := buildExecApprovalResultReply(pending, protocol.LocalActionResultPayload{
			Status:   "failed",
			ErrorMsg: "The agent is not connected and cannot receive approval requests at this time.",
		})
		if strings.TrimSpace(reply.content) != "" {
			m.sendOrUpdateApprovalCardReply(*pending, reply)
		}
	}
	return true
}

// canHandleExecApprovalReply 按发起者 owner 选连接检查能力（agent 共享多连接物理隔离）。
func (m *Manager) canHandleExecApprovalReply(agentID, ownerID int64) bool {
	if m == nil || m.sendFn == nil {
		return false
	}
	conn := m.lookupConnForOwner(agentID, ownerID)
	if conn == nil || !hasDeclaredName(conn.capabilities, "local_action_v1") {
		return false
	}
	return hasDeclaredName(conn.localActions, "claude_interaction_reply") ||
		hasDeclaredName(conn.localActions, "exec_approve") ||
		hasDeclaredName(conn.localActions, "exec_reject")
}

func (m *Manager) buildExecApprovalReplyAction(evt DelegateEventPayload, parsed parsedExecApprovalCommand) (protocol.LocalActionPayload, *pendingLocalAction, bool) {
	if m == nil || m.sendFn == nil {
		return protocol.LocalActionPayload{}, nil, false
	}
	// 按发起者 owner 选连接（agent 共享多连接物理隔离），能力检查对该连接做。
	conn := m.lookupConnForOwner(evt.AgentID, evt.OwnerID)

	var localActions []string
	if conn == nil || !hasDeclaredName(conn.capabilities, "local_action_v1") {
		// Agent not connected locally — 按发起者 owner 维度查 Redis 缓存能力集,
		// 跨 connector 版本异构场景下能据 B 自己 connector 的能力集判定。
		cached := loadAgentCapabilitiesForOwner(context.Background(), evt.AgentID, evt.OwnerID)
		if len(cached) == 0 {
			return protocol.LocalActionPayload{}, nil, false
		}
		localActions = cached
	} else {
		localActions = conn.localActions
	}

	// Check if this is a permission approval — route to dedicated action type
	approvalType := loadApprovalCardType(context.Background(), evt.AgentID, evt.SessionID, parsed.approvalCommandID)
	if approvalType == "permission" {
		actionType := "permission_approve"
		permDecision := parsed.decision
		if permDecision == "allow" {
			permDecision = "allow-once"
		}
		if permDecision == "deny" {
			actionType = "permission_reject"
		}
		if !hasDeclaredName(localActions, actionType) {
			return protocol.LocalActionPayload{}, nil, false
		}
		actionID := fmt.Sprintf("permission_approval:%s:%d", strings.TrimSpace(evt.EventID), evt.MsgID)
		if strings.TrimSpace(evt.EventID) == "" {
			actionID = fmt.Sprintf("permission_approval:%s:%d:%d", evt.SessionID, evt.AgentID, evt.MsgID)
		}
		params := map[string]interface{}{
			"exec_context_id": parsed.id,
			"decision":        permDecision,
			"actor_id":        strconv.FormatInt(evt.SenderID, 10),
			"session_id":      evt.SessionID,
			"msg_id":          strconv.FormatInt(evt.MsgID, 10),
			"scope":           "turn",
		}
		if strings.TrimSpace(evt.ThreadID) != "" {
			params["thread_id"] = evt.ThreadID
		}
		if approvalIDValue := parsed.approvalID; approvalIDValue != "" {
			params["approval_id"] = approvalIDValue
		}
		if parsed.approvalCommandID != "" {
			params["approval_command_id"] = parsed.approvalCommandID
		}

		logger.L.Infof(
			"permission approval command intercepted session=%s owner=%d agent=%d msg_id=%d action_type=%s approval_command_id=%s",
			evt.SessionID,
			evt.OwnerID,
			evt.AgentID,
			evt.MsgID,
			actionType,
			parsed.approvalCommandID,
		)
		return protocol.LocalActionPayload{
				ActionID:   actionID,
				EventID:    evt.EventID,
				ActionType: actionType,
				Params:     params,
				TimeoutMs:  15_000,
			}, &pendingLocalAction{
				actionID:          actionID,
				kind:              "permission_approval",
				agentID:           evt.AgentID,
				ownerID:           evt.OwnerID,
				sessionID:         evt.SessionID,
				threadID:          evt.ThreadID,
				quotedMessageID:   evt.MsgID,
				actionType:        actionType,
				decision:          permDecision,
				approvalCommandID: parsed.approvalCommandID,
				approvalID:        parsed.approvalID,
				approvalCardMsgID: loadApprovalCardMsgID(context.Background(), evt.AgentID, evt.SessionID, parsed.approvalCommandID),
			}, true
	}

	if hasDeclaredName(localActions, "claude_interaction_reply") {
		decision, ok := normalizeClaudePermissionDecision(parsed.decision)
		if !ok {
			return protocol.LocalActionPayload{}, nil, false
		}
		actionID := fmt.Sprintf("claude_interaction_reply_permission:%s:%d", strings.TrimSpace(evt.EventID), evt.MsgID)
		if strings.TrimSpace(evt.EventID) == "" {
			actionID = fmt.Sprintf("claude_interaction_reply_permission:%s:%d:%d", evt.SessionID, evt.AgentID, evt.MsgID)
		}
		params := map[string]interface{}{
			"session_id": evt.SessionID,
			"kind":       "permission",
			"request_id": parsed.approvalCommandID,
			"resolution": map[string]any{
				"type":  "decision",
				"value": decision,
			},
		}

		logger.L.Infof(
			"exec approval command intercepted session=%s owner=%d agent=%d msg_id=%d action_type=%s approval_command_id=%s",
			evt.SessionID,
			evt.OwnerID,
			evt.AgentID,
			evt.MsgID,
			"claude_interaction_reply",
			parsed.approvalCommandID,
		)
		return protocol.LocalActionPayload{
				ActionID:   actionID,
				EventID:    evt.EventID,
				ActionType: "claude_interaction_reply",
				Params:     params,
				TimeoutMs:  15_000,
			}, &pendingLocalAction{
				actionID:          actionID,
				kind:              "claude_interaction_reply_permission",
				agentID:           evt.AgentID,
				ownerID:           evt.OwnerID,
				sessionID:         evt.SessionID,
				threadID:          evt.ThreadID,
				quotedMessageID:   evt.MsgID,
				actionType:        "claude_interaction_reply",
				decision:          decision,
				approvalCommandID: parsed.approvalCommandID,
				approvalID:        parsed.approvalID,
				approvalCardMsgID: loadApprovalCardMsgID(context.Background(), evt.AgentID, evt.SessionID, parsed.approvalCommandID),
			}, true
	}

	actionType := "exec_approve"
	openClawDecision := parsed.decision
	if openClawDecision == "allow" {
		openClawDecision = "allow-once"
	}
	if openClawDecision == "deny" {
		actionType = "exec_reject"
	}
	if !hasDeclaredName(localActions, actionType) {
		return protocol.LocalActionPayload{}, nil, false
	}

	actionID := fmt.Sprintf("exec_approval:%s:%d", strings.TrimSpace(evt.EventID), evt.MsgID)
	if strings.TrimSpace(evt.EventID) == "" {
		actionID = fmt.Sprintf("exec_approval:%s:%d:%d", evt.SessionID, evt.AgentID, evt.MsgID)
	}
	params := map[string]interface{}{
		"exec_context_id": parsed.id,
		"decision":        openClawDecision,
		"actor_id":        strconv.FormatInt(evt.SenderID, 10),
		"session_id":      evt.SessionID,
		"msg_id":          strconv.FormatInt(evt.MsgID, 10),
	}
	if strings.TrimSpace(evt.ThreadID) != "" {
		params["thread_id"] = evt.ThreadID
	}
	approvalIDValue := parsed.approvalID
	if approvalIDValue == "" {
		approvalIDValue = parsed.approvalCommandID
	}
	if approvalIDValue != "" {
		params["approval_id"] = approvalIDValue
	}
	if parsed.approvalCommandID != "" {
		params["approval_command_id"] = parsed.approvalCommandID
	}
	if parsed.ruleIndex != "" {
		params["rule_index"] = parsed.ruleIndex
	}
	if parsed.reason != "" {
		params["reason"] = parsed.reason
	}

	logger.L.Infof(
		"exec approval command intercepted session=%s owner=%d agent=%d msg_id=%d action_type=%s approval_command_id=%s",
		evt.SessionID,
		evt.OwnerID,
		evt.AgentID,
		evt.MsgID,
		actionType,
		parsed.approvalCommandID,
	)
	return protocol.LocalActionPayload{
			ActionID:   actionID,
			EventID:    evt.EventID,
			ActionType: actionType,
			Params:     params,
			TimeoutMs:  15_000,
		}, &pendingLocalAction{
			actionID:          actionID,
			kind:              "exec_approval",
			agentID:           evt.AgentID,
			ownerID:           evt.OwnerID,
			sessionID:         evt.SessionID,
			threadID:          evt.ThreadID,
			quotedMessageID:   evt.MsgID,
			actionType:        actionType,
			decision:          openClawDecision,
			approvalCommandID: parsed.approvalCommandID,
			approvalID:        parsed.approvalID,
			approvalCardMsgID: loadApprovalCardMsgID(context.Background(), evt.AgentID, evt.SessionID, parsed.approvalCommandID),
		}, true
}

func normalizeClaudePermissionDecision(decision string) (string, bool) {
	switch strings.TrimSpace(decision) {
	case "allow", "allow-once", "allow-always":
		return "allow", true
	case "deny":
		return "deny", true
	default:
		return "", false
	}
}

// hermesFallbackApprovalIDPrefix 是后端为 hermes 危险命令纯文本兜底审批代生成的
// 审批 ID 前缀（approvalcards.generateDangerousCommandApprovalID）。这类审批在
// agent 侧没有结构化上下文：hermes 结构化投递失败时才发出兜底文本，它只认
// 兜底文案里约定的纯文本回复（/approve、/approve session、/approve always、/deny）。
// 若按常规流程把决断包成 local_action 下发，hermes 查不到这个后端代生成的 ID，
// 只会回 "unknown or expired approval id"，用户看到的就是「刚提交就提示已过期」。
const hermesFallbackApprovalIDPrefix = "hd_"

// hermesFallbackApprovalContext 记录一次兜底审批改写的结果上下文，
// 供投递失败时回补卡片状态（避免 Redis 故障后二次查找卡片索引）。
type hermesFallbackApprovalContext struct {
	approvalID        string
	approvalCommandID string
	decision          string
	cardMsgID         int64
}

// rewriteHermesFallbackApprovalResolution 把 hermes 兜底审批卡片的回传决断改写为
// 兜底协议约定的纯文本回复，让事件按普通用户消息透传给 agent。
// 返回 true 表示 evt.Content 已被改写，调用方必须跳过审批拦截器直接投递，
// 否则改写后的 /approve 文本会被拦截器再次吞掉；返回的上下文用于投递失败时
// 回补卡片状态。
func (m *Manager) rewriteHermesFallbackApprovalResolution(evt *DelegateEventPayload) (hermesFallbackApprovalContext, bool) {
	if m == nil || evt == nil {
		return hermesFallbackApprovalContext{}, false
	}
	parsed := parseExecApprovalCommand(evt.Content)
	if !parsed.matched || !parsed.ok {
		return hermesFallbackApprovalContext{}, false
	}
	if !strings.HasPrefix(parsed.approvalCommandID, hermesFallbackApprovalIDPrefix) {
		return hermesFallbackApprovalContext{}, false
	}
	// 与 tryHandleExecApprovalCommand 同一口径：只有 agent 主人本人有权裁决审批。
	// 伪造回传不改写，留给拦截器吞掉（兜底文本在托管会话里等同主人的话，绝不能放）。
	if evt.SenderID > 0 && evt.OwnerID > 0 && evt.SenderID != evt.OwnerID {
		return hermesFallbackApprovalContext{}, false
	}
	textReply := hermesFallbackApprovalTextReply(parsed.decision)
	if textReply == "" {
		return hermesFallbackApprovalContext{}, false
	}

	decision := strings.TrimSpace(parsed.decision)
	if decision == "allow" {
		decision = "allow-once"
	}
	notification.MarkApprovalResolved(context.Background(), evt.SessionID, parsed.approvalCommandID)
	ctx := hermesFallbackApprovalContext{
		approvalID:        strings.TrimSpace(parsed.approvalID),
		approvalCommandID: parsed.approvalCommandID,
		decision:          decision,
		cardMsgID:         loadApprovalCardMsgID(context.Background(), evt.AgentID, evt.SessionID, parsed.approvalCommandID),
	}
	m.settleHermesFallbackApprovalCard(*evt, ctx)
	logger.L.Infof(
		"hermes fallback approval rewritten as text reply session=%s owner=%d agent=%d msg_id=%d decision=%s approval_command_id=%s",
		evt.SessionID, evt.OwnerID, evt.AgentID, evt.MsgID, decision, parsed.approvalCommandID,
	)
	evt.Content = textReply
	return ctx, true
}

func hermesFallbackApprovalTextReply(decision string) string {
	switch strings.TrimSpace(decision) {
	case "allow", "allow-once":
		return "/approve"
	case "allow-always":
		return "/approve always"
	case "deny":
		return "/deny"
	}
	// allow-rule 等决断没有对应的 hermes 兜底文本，不改写。
	return ""
}

// settleHermesFallbackApprovalCard 把兜底审批卡片原地更新为「已提交」，
// 清掉前端 loading 态；agent 执行后会发最终 resolved/expired 状态文本，
// 由入站解析（parseResolvedExecApprovalStatus 等）补最终状态卡。
func (m *Manager) settleHermesFallbackApprovalCard(evt DelegateEventPayload, fallback hermesFallbackApprovalContext) {
	if fallback.cardMsgID <= 0 {
		return
	}
	payload := map[string]any{
		"status":              "approval-forwarded",
		"summary":             tooli18n.T(ownerCardLanguage(evt.OwnerID), "exec_approval_submitted"),
		"approval_command_id": fallback.approvalCommandID,
		"decision":            fallback.decision,
	}
	if fallback.approvalID != "" {
		payload["approval_id"] = fallback.approvalID
	}
	reply := buildExecStatusCardReply(payload)
	if strings.TrimSpace(reply.content) == "" {
		return
	}
	m.sendOrUpdateApprovalCardReply(pendingLocalAction{
		agentID:           evt.AgentID,
		ownerID:           evt.OwnerID,
		sessionID:         evt.SessionID,
		approvalCommandID: fallback.approvalCommandID,
		approvalCardMsgID: fallback.cardMsgID,
	}, reply)
}

// failHermesFallbackApprovalCard 在改写后的兜底审批文本最终投递失败
// （agent 离线且离线队列也不可用）时，把已置为「已提交」的卡片回补为失败，
// 避免用户误以为批准已生效。与 tryHandleExecApprovalCommand 里 agent 不可达
// 的口径一致：approval-unavailable + 提交失败说明。
func (m *Manager) failHermesFallbackApprovalCard(evt DelegateEventPayload, fallback hermesFallbackApprovalContext) {
	pending := pendingLocalAction{
		agentID:           evt.AgentID,
		ownerID:           evt.OwnerID,
		sessionID:         evt.SessionID,
		approvalID:        fallback.approvalID,
		approvalCommandID: fallback.approvalCommandID,
		approvalCardMsgID: fallback.cardMsgID,
	}
	reply := buildExecApprovalResultReply(&pending, protocol.LocalActionResultPayload{
		Status:   "failed",
		ErrorMsg: "The agent is not connected and cannot receive approval requests at this time.",
	})
	if strings.TrimSpace(reply.content) == "" {
		return
	}
	m.sendOrUpdateApprovalCardReply(pending, reply)
}
