package handler

import (
	"context"
	"fmt"
	"strings"

	"github.com/askie/grix/backend/internal/claudeaccess"
	"github.com/askie/grix/backend/internal/featuregate"
	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/logger"
	wsagentapi "github.com/askie/grix/backend/internal/ws/agentapi"
)

type agentAccessGateRequest struct {
	Agent        model.Agent
	OwnerID      int64
	SessionID    string
	SessionType  int16
	SenderID     int64
	SenderType   int16
	TriggerMsgID int64
}

type agentAccessSendFunc func(context.Context, wsagentapi.SendMessageReq) error

// isAccessGateTarget 判定 agent 是否受访问门禁管辖：全部 API 接入 agent 为目标。
// Claude 始终受控（原行为不变）；其余 client_type 由功能开关按 agent 主人灰度放量。
func isAccessGateTarget(agent model.Agent) bool {
	return agent.ProviderType == model.AgentProviderAPI
}

// fullGateEnabled 判定是否启用完整门禁（含群聊维度）：Claude 始终启用（原行为不变），
// 其余 client_type 由功能开关按 agent 主人灰度放量。
// 注意：私聊的"非主人不得驱动"是平台硬规则，不经此判定、不受开关影响。
func fullGateEnabled(agent model.Agent) bool {
	if strings.TrimSpace(agent.AgentClientType) == model.AgentClientTypeClaude {
		return true
	}
	return accessGateEnabledForOwner(agent.OwnerID)
}

// accessGateEnabledForOwner 读功能开关。读取失败按未启用处理：门禁基础设施故障时宁可放行，不能误拦正常消息。
func accessGateEnabledForOwner(ownerID int64) bool {
	if ownerID <= 0 {
		return false
	}
	features, err := featuregate.GetUserFeatures(ownerID)
	if err != nil {
		logger.L.Warnf("agent access gate feature read failed owner=%d: %v", ownerID, err)
		return false
	}
	for _, key := range features {
		if key == featuregate.FeatureAgentAccessGate {
			return true
		}
	}
	return false
}

func maybeHandleAgentAccessGate(
	ctx context.Context,
	req agentAccessGateRequest,
	sendFunc agentAccessSendFunc,
) (bool, error) {
	if !isAccessGateTarget(req.Agent) {
		return false, nil
	}

	// agent 主人永远豁免：否则名单为空时主人自己会被拦在门外，而被拦后再无入口解锁。
	if req.SenderID > 0 && req.SenderID == req.Agent.OwnerID {
		return false, nil
	}

	// agent 发送者豁免：门禁管的是人类访客。agent 间协作消息由主人的建群/共享行为授权，
	// 且 agent ID 与用户 ID 不同命名空间——落入审批流会把 agent ID 当用户 ID 写进名单。
	if req.SenderType == 2 {
		return false, nil
	}

	// 私聊硬规则（无状态，不进 Redis）：私聊是主人专线，主人已在上方豁免，
	// 其余只放被共享者；对全部 API agent 无条件生效、不受功能开关影响。
	if req.SessionType != model.SessionTypeGroup {
		if req.SenderID > 0 {
			if shared, shareErr := wsagentapi.AgentShareActive(req.Agent.ID, req.SenderID); shareErr == nil && shared {
				return false, nil
			}
		}
		if sendFunc == nil {
			return true, nil
		}
		content := claudeaccess.BuildStatusCardMarkdown(
			"Direct messages to this agent are limited to its owner. Please reach it in a group chat instead.",
			claudeaccess.StatusWarning,
			fmt.Sprintf("%d", req.TriggerMsgID),
		)
		if err := sendFunc(ctx, wsagentapi.SendMessageReq{
			AgentID:     req.Agent.ID,
			OwnerID:     req.OwnerID,
			SessionID:   req.SessionID,
			ClientMsgID: fmt.Sprintf("agent_access_private_only_%d_%d", req.Agent.ID, req.TriggerMsgID),
			MsgType:     1,
			Content:     content,
		}); err != nil {
			logger.L.Warnf(
				"agent access gate private notice send failed session=%s agent=%d: %v",
				req.SessionID, req.Agent.ID, err,
			)
		}
		return true, nil
	}

	// 群聊维度按 fullGateEnabled 灰度。
	if !fullGateEnabled(req.Agent) {
		return false, nil
	}

	result, err := claudeaccess.EvaluateInbound(
		ctx,
		req.Agent.ID,
		fmt.Sprintf("%d", req.SenderID),
		req.SessionID,
		req.SessionType,
	)
	if err != nil {
		// 门禁基础设施故障（如 Redis 不可用）不拦消息：与功能开关读取失败同一 fail-open 原则——
		// 静默吞掉用户消息比短暂放行更糟。
		logger.L.Warnf(
			"agent access gate evaluate failed, dispatching (fail-open) session=%s agent=%d sender=%d: %v",
			req.SessionID, req.Agent.ID, req.SenderID, err,
		)
		return false, nil
	}
	if result.Dispatch {
		return false, nil
	}

	// 被共享者豁免：共享本身就是主人的显式授权，不应被访问名单二次吊销
	//（被共享者无法自助配对，也无权改主人的名单）。仅在将被拦截时查询，避免热路径 DB 开销。
	// 评估可能刚为其落了一条待批申请（幽灵记录，主人无从对应），顺手清掉。
	if req.SenderID > 0 {
		if shared, shareErr := wsagentapi.AgentShareActive(req.Agent.ID, req.SenderID); shareErr == nil && shared {
			if result.Reason == "pairing_required" && result.PairingCode != "" {
				if _, cleanupErr := claudeaccess.CancelPending(ctx, req.Agent.ID, result.PairingCode); cleanupErr != nil {
					logger.L.Warnf("agent access gate ghost pending cleanup failed agent=%d code=%s: %v", req.Agent.ID, result.PairingCode, cleanupErr)
				}
			}
			return false, nil
		}
	}

	if sendFunc == nil {
		return true, nil
	}

	var content string
	clientMsgID := ""
	switch result.Reason {
	case "policy_disabled":
		content = claudeaccess.BuildStatusCardMarkdown(
			"Grix access is currently disabled for this agent.",
			claudeaccess.StatusWarning,
			fmt.Sprintf("%d", req.TriggerMsgID),
		)
		clientMsgID = fmt.Sprintf("agent_access_disabled_%d_%d", req.Agent.ID, req.TriggerMsgID)
	case "pairing_pending", "pairing_denied":
		// 已有待批申请 / 主人拒绝的粘性窗口内：静默拦截，不打扰任何人。
		return true, nil
	case "pairing_required":
		// 群访问申请：不在群里发任何东西（申请人无感），改为给主人私发标准审批卡，
		// 主人点「允许」即入名单——全程服务端闭环，不经过 agent。
		// 通知失败必须回滚待批记录：否则 pending 挡住后续所有重试、主人又没收到卡，
		// 双方在 TTL 窗口内零感知。回滚后访客下次 @ 会重新发起。
		if !wsagentapi.NotifyGroupAccessApproval(req.Agent.ID, req.Agent.OwnerID, req.SenderID, req.SessionID, result.PairingCode) {
			if _, rollbackErr := claudeaccess.CancelPending(ctx, req.Agent.ID, result.PairingCode); rollbackErr != nil {
				logger.L.Warnf("agent access gate pending rollback failed agent=%d code=%s: %v", req.Agent.ID, result.PairingCode, rollbackErr)
			}
		}
		return true, nil
	default:
		content = claudeaccess.BuildStatusCardMarkdown(
			"Grix access is temporarily unavailable.",
			claudeaccess.StatusError,
			fmt.Sprintf("%d", req.TriggerMsgID),
		)
		clientMsgID = fmt.Sprintf("agent_access_unavailable_%d_%d", req.Agent.ID, req.TriggerMsgID)
	}

	if err := sendFunc(ctx, wsagentapi.SendMessageReq{
		AgentID:     req.Agent.ID,
		OwnerID:     req.OwnerID,
		SessionID:   req.SessionID,
		ClientMsgID: clientMsgID,
		MsgType:     1,
		Content:     content,
	}); err != nil {
		logger.L.Warnf(
			"agent access gate send failed session=%s owner=%d agent=%d reason=%s: %v",
			req.SessionID,
			req.OwnerID,
			req.Agent.ID,
			result.Reason,
			err,
		)
	}
	return true, nil
}

func sendAgentAccessMessage(ctx context.Context, req wsagentapi.SendMessageReq) error {
	_, err := wsagentapi.SendMessage(ctx, req)
	return err
}
