package handler

import (
	"context"
	"errors"
	"fmt"
	"strings"

	agenttoolbar "github.com/askie/grix/backend/internal/agenttoolbar"
	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/logger"
	wsagentapi "github.com/askie/grix/backend/internal/ws/agentapi"
)

// stopSlashCommand 是主人在会话里手打的停止命令。
const stopSlashCommand = "/stop"

// stopSlashCommandStopper 执行一次停止，返回工具栏原本的受理结果与提示文案。
type stopSlashCommandStopper func(
	ctx context.Context,
	ownerID int64,
	sessionID string,
	agentID int64,
	clientActionID string,
) (accepted bool, notice string, err error)

// stopSlashCommandNoticeSender 把提示写回会话。
type stopSlashCommandNoticeSender func(ctx context.Context, req wsagentapi.SendMessageReq) error

// isStopSlashCommand 只认精确的 "/stop"（忽略首尾空白与大小写）。
// "/stop xxx" 或正文里夹带 /stop 的消息一律不拦，按普通文本下发。
func isStopSlashCommand(msgType int16, content string) bool {
	if msgType != model.MsgTypeText {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(content), stopSlashCommand)
}

// toolbarStopOutput 走 agenttoolbar 的 stop_output 动作，与工具栏「停止」按钮同一入口：
// 由各 agent 包决定停止方式（Hermes 走 SendStopText，其余走 StopOutput），
// 提示文案直接沿用工具栏 ack（已按用户语言本地化）。
func toolbarStopOutput(
	ctx context.Context,
	ownerID int64,
	sessionID string,
	agentID int64,
	clientActionID string,
) (bool, string, error) {
	svc := agenttoolbar.GetGlobal()
	if svc == nil {
		return false, "", errors.New("agent toolbar service unavailable")
	}
	ack, err := svc.StopOutputBySession(ctx, ownerID, sessionID, agentID, clientActionID)
	if err != nil {
		return false, "", err
	}
	return ack.Accepted, strings.TrimSpace(ack.Message), nil
}

// maybeHandleStopSlashCommand 把主人手打的 "/stop" 收口到工具栏停止按钮的同一条路径。
//
// 不做这件事时，"/stop" 只是普通文本，会作为 user_chat/group_mention 事件排在正在跑的
// 任务后面进连接器队列，等于没停。这里在事件下发前拦截：对本次路由里属于发送者自己的
// Agent API 目标逐个执行停止，再把工具栏原本的 ack 文案写回会话作为提示。
//
// 返回 true 表示这条消息已被消费，调用方不应再下发任何事件（含 mirror）。
// 停止本身失败（如工具栏服务不可用）时返回 false，消息退回普通文本链路，
// 而不是被静默吞掉。
func maybeHandleStopSlashCommand(
	ctx context.Context,
	sessionID string,
	senderID int64,
	senderType int16,
	triggerMsgID int64,
	msgType int16,
	content string,
	route *directSessionRoute,
	stop stopSlashCommandStopper,
	sendNotice stopSlashCommandNoticeSender,
) bool {
	if route == nil || senderID <= 0 || senderType == 2 || stop == nil {
		return false
	}
	if !isStopSlashCommand(msgType, content) {
		return false
	}

	handled := false
	for _, target := range route.Targets {
		agent := target.Agent
		// 只处理发送者自己的 Agent API 目标：群里别人的 agent 不该被一句 /stop 停掉。
		if agent.ProviderType != model.AgentProviderAPI || agent.ID <= 0 || agent.OwnerID != senderID {
			continue
		}
		// 用触发消息 ID 做幂等键：同一条 /stop 被重投（如 retry_msg）不会重复停止。
		clientActionID := fmt.Sprintf("slash_stop:%d:%d", agent.ID, triggerMsgID)
		accepted, notice, err := stop(ctx, senderID, sessionID, agent.ID, clientActionID)
		if err != nil {
			logger.L.Warnf(
				"slash /stop failed session=%s owner=%d agent=%d msg_id=%d: %v",
				sessionID, senderID, agent.ID, triggerMsgID, err,
			)
			continue
		}
		handled = true
		logger.L.Infof(
			"slash /stop handled session=%s owner=%d agent=%d msg_id=%d accepted=%t",
			sessionID, senderID, agent.ID, triggerMsgID, accepted,
		)
		if notice == "" || sendNotice == nil {
			continue
		}
		if err := sendNotice(ctx, wsagentapi.SendMessageReq{
			AgentID:     agent.ID,
			OwnerID:     agent.OwnerID,
			SessionID:   sessionID,
			ClientMsgID: fmt.Sprintf("slash_stop_notice_%d_%d", agent.ID, triggerMsgID),
			MsgType:     model.MsgTypeText,
			Content:     notice,
		}); err != nil {
			logger.L.Warnf(
				"slash /stop notice send failed session=%s owner=%d agent=%d msg_id=%d: %v",
				sessionID, senderID, agent.ID, triggerMsgID, err,
			)
		}
	}
	return handled
}
