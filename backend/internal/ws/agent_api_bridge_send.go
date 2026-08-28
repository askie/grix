package ws

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/askie/grix/backend/internal/chatmarkdown"
	"github.com/askie/grix/backend/internal/pkg/logger"
	"github.com/askie/grix/backend/internal/ws/agentapi"
	"github.com/askie/grix/backend/internal/ws/agentmsg"
	"github.com/askie/grix/backend/internal/ws/handler"
	"github.com/askie/grix/backend/internal/ws/protocol"
	"github.com/askie/grix/backend/internal/ws/threadmeta"
)

func (s *Server) handleAgentAPISend(ctx context.Context, req agentapi.SendMessageReq) (*agentapi.SendMessageResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	eventID := strings.TrimSpace(req.EventID)
	if mgr := agentapi.GetGlobal(); mgr != nil && eventID != "" && mgr.ShouldFenceEventReply(eventID) {
		return &agentapi.SendMessageResult{
			CreatedAt: time.Now().UnixMilli(),
		}, nil
	}
	identityMode := strings.TrimSpace(req.IdentityMode)
	if identityMode == "" {
		identityMode = agentmsg.ModeAgentAPI
	}
	identity, resolveErr := agentmsg.ResolveIdentity(ctx, agentmsg.IdentityParams{
		Mode:      identityMode,
		SessionID: req.SessionID,
		OwnerID:   req.OwnerID,
		AgentID:   req.AgentID,
		CallerID:  req.CallerID,
	})
	if resolveErr != nil {
		if errors.Is(resolveErr, agentmsg.ErrPermissionDenied) {
			return nil, &agentapi.SendError{Code: 4003, Msg: "permission denied"}
		}
		return nil, &agentapi.SendError{Code: 5001, Msg: "send message failed"}
	}
	if agentapi.ShouldSilentlyAckInboundOutput(req.Content, agentapi.IsNoReplyProtocolContext(eventID)) {
		return &agentapi.SendMessageResult{
			CreatedAt: time.Now().UnixMilli(),
		}, nil
	}
	deliverableContent, deliverable := agentapi.GateUserFacingOutput(req.Content, eventID)
	if !deliverable {
		logger.L.Infof(
			"internal task output withheld: no /to_user segment event_id=%s session=%s agent=%d owner=%d",
			eventID, strings.TrimSpace(req.SessionID), req.AgentID, req.OwnerID,
		)
		return &agentapi.SendMessageResult{
			CreatedAt: time.Now().UnixMilli(),
		}, nil
	}
	repairedContent := chatmarkdown.RepairFinal(deliverableContent).Output

	payload := protocol.SendMsgPayload{
		SessionID:   req.SessionID,
		ThreadID:    req.ThreadID,
		ClientMsgID: req.ClientMsgID,
		MsgType:     req.MsgType,
		Content:     repairedContent,
		Extra: mergeAgentAPIExtraWithIdentity(
			threadmeta.Merge(agentapi.MergeMediaURLIntoExtra(req.Extra, req.MediaURL), req.ThreadID),
			repairedContent,
			req.AgentID,
			identity,
		),
		QuotedMessageID: req.QuotedMessageID,
		VisibleTo:       req.VisibleTo,
	}
	if mgr := agentapi.GetGlobal(); mgr != nil {
		// Single authority for agent output visibility: eventless or expired
		// output in a group must still follow the latest hidden trigger.
		payload.VisibleTo = mgr.ResolveOutboundVisibleTo(
			req.AgentID, req.OwnerID, req.SessionID, eventID, req.QuotedMessageID, req.VisibleTo,
		)
	}
	raw, _ := json.Marshal(payload)
	conn := &agentBridgeConn{
		userID:   identity.SenderID,
		deviceID: fmt.Sprintf("agent_api_%d", req.AgentID),
	}
	pkt := &protocol.Packet{
		Cmd:     protocol.CmdSendMsg,
		Seq:     conn.NextSeq(),
		Payload: raw,
	}
	handler.HandleSendMsg(s.hub, conn, pkt)

	var ack *protocol.SendAckPayload
	var nack *protocol.SendNackPayload
	for i := range conn.sent {
		item := conn.sent[i]
		switch item.cmd {
		case protocol.CmdSendAck:
			if p, ok := item.payload.(protocol.SendAckPayload); ok {
				tmp := p
				ack = &tmp
			}
		case protocol.CmdSendNack:
			if p, ok := item.payload.(protocol.SendNackPayload); ok {
				tmp := p
				nack = &tmp
			}
		}
	}

	if nack != nil {
		logger.L.Warnf("agent api send rejected session=%s agent=%d owner=%d sender=%d code=%d msg=%s",
			req.SessionID, req.AgentID, req.OwnerID, identity.SenderID, nack.Code, nack.Msg)
		return nil, &agentapi.SendError{Code: nack.Code, Msg: nack.Msg}
	}
	if ack == nil {
		logger.L.Warnf("agent api send missing ack session=%s agent=%d owner=%d sender=%d",
			req.SessionID, req.AgentID, req.OwnerID, identity.SenderID)
		return nil, &agentapi.SendError{Code: 5001, Msg: "send message failed"}
	}

	logger.L.Infof("agent api send accepted session=%s agent=%d owner=%d sender=%d msg_id=%d",
		req.SessionID, req.AgentID, req.OwnerID, identity.SenderID, ack.MsgID)
	// 接点B（非流式路径）：若该会话正在进行 AI 语音通话，把文字大脑的回复注入语音侧（豆包 502）。
	// 流式路径的注入在 agent_api_bridge_stream.go:MaybeInjectVoiceReply。
	// 红线：只注入给人看的真实文本回复。排除转写片段(msg_type=6，回声/自答)、工具执行卡片
	// (channel_data.grix.toolExecution，grix://card 卡片链接噪音)、思考过程(thinking)——
	// 详见 handler.ShouldInjectVoiceMessage。直拨/语音大脑场景关闭了"每轮一次"去重护栏，
	// 必须在此源头拦住。
	if handler.ShouldInjectVoiceMessage(req.MsgType, req.Extra) {
		handler.MaybeInjectVoiceReply(ctx, req.SessionID, repairedContent)
	}
	if mgr := agentapi.GetGlobal(); mgr != nil && eventID != "" {
		// A visible send_msg should mark the run as streaming, but completion
		// must remain driven by event_result/event_stop_result terminal states.
		mgr.MarkRunStreaming(eventID, ack.MsgID)
	}

	return &agentapi.SendMessageResult{
		MsgID:     ack.MsgID,
		InboxSeq:  ack.InboxSeq,
		CreatedAt: ack.CreatedAt,
	}, nil
}
