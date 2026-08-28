package agentapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	"github.com/askie/grix/backend/internal/grixactions"
	"github.com/askie/grix/backend/internal/notification"
	"github.com/askie/grix/backend/internal/pkg/logger"
	"github.com/askie/grix/backend/internal/ws/protocol"
)

const (
	claudeQuestionUsage = "Unsupported question action payload."
)

type parsedClaudeQuestionCommand struct {
	matched   bool
	ok        bool
	err       string
	requestID string
	action    string
	response  map[string]any
}

func parseClaudeQuestionCommand(raw string) parsedClaudeQuestionCommand {
	reply, matched, err := grixactions.ParseQuestionReply(raw)
	if !matched {
		return parsedClaudeQuestionCommand{}
	}
	if err != nil {
		return parsedClaudeQuestionCommand{matched: true, err: claudeQuestionUsage}
	}
	if reply.Action != "" {
		return parsedClaudeQuestionCommand{
			matched:   true,
			ok:        true,
			requestID: reply.RequestID,
			action:    reply.Action,
		}
	}
	return parsedClaudeQuestionCommand{
		matched:   true,
		ok:        true,
		requestID: reply.RequestID,
		response:  reply.Response,
	}
}

func (m *Manager) tryHandleClaudeQuestionCommand(evt DelegateEventPayload) bool {
	parsed := parseClaudeQuestionCommand(evt.Content)
	if !parsed.matched {
		return false
	}

	if !parsed.ok {
		// 命令来自 delegate 事件，evt.OwnerID 即发起者，按其连接做能力检查与下发。
		if m.canSendClaudeElicitationReply(evt.AgentID, evt.OwnerID) {
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

	fallbackEvent := buildClaudeQuestionReplyFallbackEvent(evt, parsed)

	if !m.canSendClaudeElicitationReply(evt.AgentID, evt.OwnerID) && !m.canForwardClaudeElicitationReply(evt.AgentID, evt.OwnerID) {
		// 连接不可达（冷启动/离线）：原始卡片 URI 对 agent 不可读，把答案改写成
		// 可读文本按普通消息链路投递（在线直投、离线入队），内容不丢。
		if fallbackEvent != nil {
			logger.L.Infof(
				"claude question reply rewritten for offline delivery session=%s owner=%d agent=%d msg_id=%d request_id=%s",
				evt.SessionID, evt.OwnerID, evt.AgentID, evt.MsgID, parsed.requestID,
			)
			delivered := m.PushDelegateEvent(*fallbackEvent)
			if delivered {
				// 答案已降级为普通文本投递，卡片通道不会再有 local_action_result，
				// 立即把原问答卡标记为「已转发」，避免卡片停在「提交中」。
				m.markQuestionReplyForwarded(evt, parsed.requestID)
			}
			return delivered
		}
		return false
	}

	actionID := fmt.Sprintf("claude_interaction_reply_elicitation:%s:%d", strings.TrimSpace(evt.EventID), evt.MsgID)
	if strings.TrimSpace(evt.EventID) == "" {
		actionID = fmt.Sprintf("claude_interaction_reply_elicitation:%s:%d:%d", evt.SessionID, evt.AgentID, evt.MsgID)
	}
	logger.L.Infof(
		"claude question command intercepted session=%s owner=%d agent=%d msg_id=%d request_id=%s",
		evt.SessionID,
		evt.OwnerID,
		evt.AgentID,
		evt.MsgID,
		parsed.requestID,
	)
	// 主人已回答：打标记，让还在路上的提问推送不再弹出。
	notification.MarkQuestionResolved(context.Background(), evt.SessionID, parsed.requestID)
	// 按发起者 owner 路由到对应连接下发（agent 共享多连接物理隔离）。
	return m.sendLocalActionWithPendingForOwner(evt.AgentID, evt.OwnerID, protocol.LocalActionPayload{
		ActionID:   actionID,
		EventID:    evt.EventID,
		ActionType: "claude_interaction_reply",
		Params: map[string]interface{}{
			"session_id": evt.SessionID,
			"kind":       "elicitation",
			"request_id": parsed.requestID,
			"resolution": buildClaudeQuestionResolution(parsed),
		},
		TimeoutMs: 15_000,
	}, &pendingLocalAction{
		actionID:          actionID,
		kind:              "claude_interaction_reply_elicitation",
		agentID:           evt.AgentID,
		ownerID:           evt.OwnerID,
		sessionID:         evt.SessionID,
		threadID:          evt.ThreadID,
		quotedMessageID:   evt.MsgID,
		actionType:        "claude_interaction_reply",
		referenceID:       parsed.requestID,
		approvalCardMsgID: loadApprovalCardMsgID(context.Background(), evt.AgentID, evt.SessionID, parsed.requestID),
		fallbackEvent:     fallbackEvent,
	})
}

// buildClaudeQuestionReplyFallbackEvent 把问答回卡改写成 agent 可读的普通文本事件，
// 复用原事件的路由与标识（EventID/MsgID/发送者），仅替换内容。用于卡片通道无人
// 接收（超时/进程重启/冷启动）时兜底投递，返回 nil 表示无可投递内容（如取消动作）。
func buildClaudeQuestionReplyFallbackEvent(evt DelegateEventPayload, parsed parsedClaudeQuestionCommand) *DelegateEventPayload {
	content := buildClaudeQuestionReplyFallbackContent(parsed)
	if content == "" {
		return nil
	}
	fallback := evt
	fallback.Content = content
	return &fallback
}

func buildClaudeQuestionReplyFallbackContent(parsed parsedClaudeQuestionCommand) string {
	if parsed.action != "" {
		// 取消/确认类动作没有可转述的答案内容。
		return ""
	}
	var lines []string
	switch strings.TrimSpace(fmt.Sprint(parsed.response["type"])) {
	case "map":
		entries, _ := parsed.response["entries"].([]any)
		for _, rawEntry := range entries {
			entry, ok := rawEntry.(map[string]any)
			if !ok {
				continue
			}
			key := strings.TrimSpace(fmt.Sprint(entry["key"]))
			value := strings.TrimSpace(fmt.Sprint(entry["value"]))
			if value == "" {
				continue
			}
			if key != "" {
				lines = append(lines, key+": "+value)
			} else {
				lines = append(lines, value)
			}
		}
	default:
		if value := strings.TrimSpace(fmt.Sprint(parsed.response["value"])); value != "" && value != "<nil>" {
			lines = append(lines, value)
		}
	}
	if len(lines) == 0 {
		return ""
	}
	return fmt.Sprintf(
		"[用户已通过提问卡 %s 作答，卡片通道未送达，请按以下回答继续]\n%s",
		strings.TrimSpace(parsed.requestID),
		strings.Join(lines, "\n"),
	)
}

// extractAgentQuestionRequestID 从 agent_question 提问卡内容里解析 request_id，
// 供发送时登记卡片消息号；非提问卡返回空串。
func extractAgentQuestionRequestID(content string) string {
	idx := strings.Index(content, "grix://card/agent_question?")
	if idx < 0 {
		return ""
	}
	uriStr := content[idx:]
	if end := strings.IndexByte(uriStr, ')'); end >= 0 {
		uriStr = uriStr[:end]
	}
	parsed, err := url.Parse(uriStr)
	if err != nil {
		return ""
	}
	raw := strings.TrimSpace(parsed.Query().Get("d"))
	if raw == "" {
		return ""
	}
	var payload struct {
		RequestID string `json:"request_id"`
	}
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return ""
	}
	return strings.TrimSpace(payload.RequestID)
}

// canForwardClaudeElicitationReply checks whether the agent is reachable on a
// remote node and supports the claude_interaction_reply action, using the
// Redis-cached capabilities. 按 ownerID 优先查 owner 维度的能力集,共享场景下
// connector 版本异构时也能据 B 自己 connector 的能力集判定。
func (m *Manager) canForwardClaudeElicitationReply(agentID, ownerID int64) bool {
	cached := loadAgentCapabilitiesForOwner(context.Background(), agentID, ownerID)
	if len(cached) == 0 {
		return false
	}
	return hasDeclaredName(cached, "claude_interaction_reply")
}

// canSendClaudeElicitationReply 按发起者 owner 选连接检查能力（agent 共享多连接物理隔离）。
func (m *Manager) canSendClaudeElicitationReply(agentID, ownerID int64) bool {
	if m == nil || m.sendFn == nil {
		return false
	}
	conn := m.lookupConnForOwner(agentID, ownerID)
	if conn == nil {
		return false
	}
	return hasDeclaredName(conn.capabilities, "local_action_v1") &&
		hasDeclaredName(conn.localActions, "claude_interaction_reply")
}

func buildClaudeQuestionResolution(parsed parsedClaudeQuestionCommand) map[string]any {
	if parsed.action != "" {
		return map[string]any{
			"type":  "action",
			"value": parsed.action,
		}
	}

	responseType := strings.TrimSpace(fmt.Sprint(parsed.response["type"]))
	switch responseType {
	case "map":
		return map[string]any{
			"type":    "map",
			"entries": parsed.response["entries"],
		}
	case "single":
		return map[string]any{
			"type":  "text",
			"value": strings.TrimSpace(fmt.Sprint(parsed.response["value"])),
		}
	default:
		return map[string]any{
			"type":  "text",
			"value": strings.TrimSpace(fmt.Sprint(parsed.response["value"])),
		}
	}
}
