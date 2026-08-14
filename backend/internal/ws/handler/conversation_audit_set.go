package handler

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	agenttoolbar "github.com/askie/grix/backend/internal/agenttoolbar"
	toolprotocol "github.com/askie/grix/backend/internal/agenttoolbar/protocol"
	"github.com/askie/grix/backend/internal/conversationaudit"
	"github.com/askie/grix/backend/internal/pkg/logger"
	wsprotocol "github.com/askie/grix/backend/internal/ws/protocol"
)

var errAuditToolbarUnavailable = errors.New("toolbar service unavailable")

// resolveAuditToolbarSnapshot 是 conversation_audit_set 做访问校验与解析目标
// Agent 的入口；抽成包级变量便于测试替换。
//
// 注意：私聊场景 resolver 会忽略 targetAgentID、按会话成员解析真实 Agent，
// 因此调用方必须使用返回快照里的 AgentID，不能信任客户端传入的值。
var resolveAuditToolbarSnapshot = func(ctx context.Context, ownerID int64, sessionID string, targetAgentID int64) (toolprotocol.Snapshot, error) {
	svc := agenttoolbar.GetGlobal()
	if svc == nil {
		return toolprotocol.Snapshot{}, errAuditToolbarUnavailable
	}
	return svc.GetSnapshot(ctx, ownerID, sessionID, targetAgentID)
}

// HandleConversationAuditSet 持久化当前用户对指定 Agent 的对话审计开关
// （user+agent 维度），成功后按 Agent 刷新并 fanout 已索引会话的工具栏快照，
// 让该用户所有设备/会话的开关状态与服务端保持一致。
//
// 校验顺序固定为：先 feature gate、再 GetSnapshot 访问校验（能拿到快照才允许设置），
// 两者失败均返回 4003，避免向无权限用户暴露会话/agent 存在性。
func HandleConversationAuditSet(_ HubInterface, conn ConnInterface, pkt *wsprotocol.Packet) {
	var payload wsprotocol.ConversationAuditSetPayload
	_ = json.Unmarshal(pkt.Payload, &payload)
	respond := func(code int, msg string, agentID int64, enabled bool) {
		conn.SendPayload(wsprotocol.CmdConversationAuditSetResp, pkt.Seq, wsprotocol.ConversationAuditSetRespPayload{
			Code:      code,
			Msg:       msg,
			SessionID: payload.SessionID,
			AgentID:   agentID,
			Enabled:   enabled,
		})
	}
	if payload.AgentID <= 0 || strings.TrimSpace(payload.SessionID) == "" {
		respond(4001, "invalid conversation_audit_set payload", payload.AgentID, false)
		return
	}

	userID := conn.GetUserID()
	if !conversationaudit.FeatureEnabled(userID) {
		respond(4003, "conversation audit is unavailable", payload.AgentID, false)
		return
	}
	ctx := context.Background()
	snapshot, err := resolveAuditToolbarSnapshot(ctx, userID, payload.SessionID, payload.AgentID)
	if errors.Is(err, errAuditToolbarUnavailable) {
		respond(5000, "toolbar service unavailable", payload.AgentID, false)
		return
	}
	if err != nil || snapshot.AgentID <= 0 {
		respond(4003, "conversation audit is unavailable", payload.AgentID, false)
		return
	}
	// 必须使用访问校验后解析出的 AgentID：私聊场景 resolver 忽略客户端传入的
	// targetAgentID，直接信任 payload 会把开关写到未授权 Agent 上。
	agentID := snapshot.AgentID
	if err := conversationaudit.SetAuditEnabled(userID, agentID, payload.Enabled); err != nil {
		logger.L.Warnf("conversation_audit_set persist failed user=%d agent=%d err=%v", userID, agentID, err)
		respond(5000, "failed to persist conversation audit state", agentID, false)
		return
	}
	// 返回落库后的实际值，供前端校准乐观更新。
	actual, err := conversationaudit.GetAuditEnabled(userID, agentID)
	if err != nil {
		logger.L.Warnf("conversation_audit_set readback failed user=%d agent=%d err=%v", userID, agentID, err)
		actual = payload.Enabled
	}
	if svc := agenttoolbar.GetGlobal(); svc != nil {
		if err := svc.RefreshByAgent(ctx, userID, agentID, "conversation_audit_set"); err != nil {
			logger.L.Warnf("conversation_audit_set toolbar refresh failed user=%d agent=%d err=%v", userID, agentID, err)
		}
	}
	respond(0, "", agentID, actual)
}
