package handler

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"time"

	"github.com/askie/grix/backend/internal/call"
	"github.com/askie/grix/backend/internal/pkg/logger"
	"github.com/askie/grix/backend/internal/pkg/snowflake"
	"github.com/askie/grix/backend/internal/ws/protocol"
)

// HandleCallAnswerWithAI 处理 B 选择 AI 代接（RINGING → AI_DELEGATED）。
func HandleCallAnswerWithAI(hub HubInterface, conn ConnInterface, pkt *protocol.Packet) {
	if callCtrl == nil {
		conn.SendPayload(protocol.CmdError, pkt.Seq, errPayload("call service unavailable"))
		return
	}
	var payload protocol.CallAnswerWithAIPayload
	if err := json.Unmarshal(pkt.Payload, &payload); err != nil {
		conn.SendPayload(protocol.CmdError, pkt.Seq, errPayload("invalid payload"))
		return
	}
	callID, err := strconv.ParseInt(payload.CallID, 10, 64)
	if err != nil {
		conn.SendPayload(protocol.CmdError, pkt.Seq, errPayload("invalid call_id"))
		return
	}
	agentID, err := strconv.ParseInt(payload.AgentID, 10, 64)
	if err != nil {
		conn.SendPayload(protocol.CmdError, pkt.Seq, errPayload("invalid agent_id"))
		return
	}
	logger.L.Infof("call trace: answer_with_ai recv user=%d call=%d agent=%d seq=%d", conn.GetUserID(), callID, agentID, pkt.Seq)
	if routeOrSendCallRPC(context.Background(), hub, conn, pkt.Seq, callRPCRequest{
		Action:   callRPCActionAnswerWithAI,
		CallID:   callID,
		UserID:   conn.GetUserID(),
		DeviceID: conn.GetDeviceID(),
		AgentID:  agentID,
	}) {
		return
	}

	// 从 agent 表解析语音托管完整配置（含解密 BYOK key），无配置即报错
	spec, err := resolveAgentVoiceSpec(agentID, "")
	if err != nil {
		conn.SendPayload(protocol.CmdError, pkt.Seq, errPayload(err.Error()))
		return
	}
	if !reserveVoiceDailyQuota(agentID, spec.DailyLimit) {
		conn.SendPayload(protocol.CmdError, pkt.Seq, errPayload("今日语音托管次数已达上限"))
		return
	}

	tokenCallee, roomURL, err := callCtrl.AnswerWithAI(context.Background(), callID, conn.GetUserID(), spec)
	if err != nil {
		logger.L.Warnf("call answer_with_ai error call=%d user=%d agent=%d err=%v", callID, conn.GetUserID(), agentID, err)
		conn.SendPayload(protocol.CmdError, pkt.Seq, errPayload(err.Error()))
		return
	}
	notifyCallAnsweredElsewhere(hub, conn.GetUserID(), callID, conn.GetDeviceID())

	conn.SendPayload(protocol.CmdCallPeerAnswered, pkt.Seq, protocol.CallPeerAnsweredPayload{
		CallID:     payload.CallID,
		Mode:       "ai_delegated",
		RoomToken:  tokenCallee,
		RoomURL:    roomURL,
		ICEServers: callICEServers(),
	})
	logger.L.Infof("call trace: answer_with_ai ack user=%d call=%d agent=%d room_url=%s", conn.GetUserID(), callID, agentID, roomURL)
}

// HandleCallDirectAI 处理 owner 直接拨打语音大模型 agent。
// 仅 owner 可发起；直接进入 AI_DELEGATED，发起方与 AI 对话。
func HandleCallDirectAI(hub HubInterface, conn ConnInterface, pkt *protocol.Packet) {
	if callCtrl == nil {
		conn.SendPayload(protocol.CmdError, pkt.Seq, errPayload("call service unavailable"))
		return
	}
	var payload protocol.CallDirectAIPayload
	if err := json.Unmarshal(pkt.Payload, &payload); err != nil {
		conn.SendPayload(protocol.CmdError, pkt.Seq, errPayload("invalid payload"))
		return
	}
	agentID, err := strconv.ParseInt(payload.AgentID, 10, 64)
	if err != nil {
		conn.SendPayload(protocol.CmdError, pkt.Seq, errPayload("invalid agent_id"))
		return
	}
	logger.L.Infof("call trace: direct_ai recv user=%d agent=%d seq=%d", conn.GetUserID(), agentID, pkt.Seq)
	// 仅 owner 可呼，防他人借 direct call 消耗用户 key
	if err := ensureVoiceAgentOwner(conn.GetUserID(), agentID); err != nil {
		logger.L.Warnf("call direct_ai owner check failed user=%d agent=%d err=%v", conn.GetUserID(), agentID, err)
		conn.SendPayload(protocol.CmdError, pkt.Seq, errPayload(err.Error()))
		return
	}
	// resolve 真实 IM 会话，call_records 落库使用真实 session ID
	sessionID, err := resolveCallSession(conn.GetUserID(), agentID)
	if err != nil {
		logger.L.Warnf("call direct_ai resolve session failed user=%d agent=%d err=%v", conn.GetUserID(), agentID, err)
		conn.SendPayload(protocol.CmdError, pkt.Seq, errPayload(err.Error()))
		return
	}
	spec, err := resolveAgentVoiceSpec(agentID, "")
	if err != nil {
		logger.L.Warnf("call direct_ai resolve spec failed user=%d agent=%d err=%v", conn.GetUserID(), agentID, err)
		conn.SendPayload(protocol.CmdError, pkt.Seq, errPayload(err.Error()))
		return
	}
	if !reserveVoiceDailyQuota(agentID, spec.DailyLimit) {
		conn.SendPayload(protocol.CmdError, pkt.Seq, errPayload("今日语音托管次数已达上限"))
		return
	}
	ctx := context.Background()
	callID := snowflake.GenID()
	busyGuard, err := reserveCallBusy(ctx, callID, conn.GetUserID())
	if err != nil {
		logger.L.Warnf("call direct_ai busy guard error user=%d agent=%d call=%d err=%v", conn.GetUserID(), agentID, callID, err)
		if errors.Is(err, call.ErrCallerBusy) {
			conn.SendPayload(protocol.CmdCallBusy, pkt.Seq, map[string]string{"reason": err.Error()})
			return
		}
		conn.SendPayload(protocol.CmdError, pkt.Seq, errPayload("internal error"))
		return
	}
	ownerCommitted := false
	defer func() {
		if !ownerCommitted {
			busyGuard.release(ctx)
		}
	}()

	_, token, roomURL, err := callCtrl.DirectAICallWithID(ctx, callID, conn.GetUserID(), sessionID, spec)
	if err != nil {
		logger.L.Warnf("call direct_ai error user=%d agent=%d err=%v", conn.GetUserID(), agentID, err)
		// caller busy 发 call:busy 信令让前端 CallController 能感知并立即收尾
		if errors.Is(err, call.ErrCallerBusy) {
			conn.SendPayload(protocol.CmdCallBusy, pkt.Seq, map[string]string{"reason": err.Error()})
			return
		}
		conn.SendPayload(protocol.CmdError, pkt.Seq, errPayload(err.Error()))
		return
	}
	if err := rememberCallOwner(ctx, callID, hub.GetNodeID()); err != nil {
		logger.L.Errorf("call direct_ai owner guard error user=%d agent=%d call=%d node=%s err=%v", conn.GetUserID(), agentID, callID, hub.GetNodeID(), err)
		_ = callCtrl.Hangup(ctx, callID, conn.GetUserID())
		conn.SendPayload(protocol.CmdError, pkt.Seq, errPayload("internal error"))
		return
	}
	ownerCommitted = true
	conn.SendPayload(protocol.CmdCallInviteAck, pkt.Seq, protocol.CallInviteAckPayload{
		CallID:     strconv.FormatInt(callID, 10),
		RoomToken:  token,
		RoomURL:    roomURL,
		ICEServers: callICEServers(),
	})
	logger.L.Infof("call trace: direct_ai invite_ack user=%d call=%d agent=%d room_url=%s token_len=%d", conn.GetUserID(), callID, agentID, roomURL, len(token))
}

// HandleCallTakeover 处理 B 接管（AI_DELEGATED → HUMAN_ACTIVE）。
// AI 被打断，静默旁听维持上下文。
func HandleCallTakeover(hub HubInterface, conn ConnInterface, pkt *protocol.Packet) {
	if callCtrl == nil {
		conn.SendPayload(protocol.CmdError, pkt.Seq, errPayload("call service unavailable"))
		return
	}
	var payload protocol.CallTakeoverPayload
	if err := json.Unmarshal(pkt.Payload, &payload); err != nil {
		conn.SendPayload(protocol.CmdError, pkt.Seq, errPayload("invalid payload"))
		return
	}
	callID, err := strconv.ParseInt(payload.CallID, 10, 64)
	if err != nil {
		conn.SendPayload(protocol.CmdError, pkt.Seq, errPayload("invalid call_id"))
		return
	}

	logger.L.Infof("call trace: takeover recv user=%d call=%d seq=%d", conn.GetUserID(), callID, pkt.Seq)
	if routeOrSendCallRPC(context.Background(), hub, conn, pkt.Seq, callRPCRequest{
		Action: callRPCActionTakeover,
		CallID: callID,
		UserID: conn.GetUserID(),
	}) {
		return
	}
	if err := callCtrl.Takeover(context.Background(), callID, conn.GetUserID()); err != nil {
		logger.L.Warnf("call takeover error call=%d user=%d err=%v", callID, conn.GetUserID(), err)
		conn.SendPayload(protocol.CmdError, pkt.Seq, errPayload(err.Error()))
		return
	}

	// 广播 AI 状态变更（双方可见）
	aiStatePayload := protocol.CallAIStatePayload{
		CallID: payload.CallID,
		Mode:   "human_active",
		Ts:     time.Now().UnixMilli(),
	}
	conn.SendPayload(protocol.CmdCallState, pkt.Seq, aiStatePayload)
}

// HandleCallHandBack 处理 B 将通话交回给 AI（HUMAN_ACTIVE → AI_DELEGATED）。
func HandleCallHandBack(hub HubInterface, conn ConnInterface, pkt *protocol.Packet) {
	if callCtrl == nil {
		conn.SendPayload(protocol.CmdError, pkt.Seq, errPayload("call service unavailable"))
		return
	}
	var payload protocol.CallHandBackPayload
	if err := json.Unmarshal(pkt.Payload, &payload); err != nil {
		conn.SendPayload(protocol.CmdError, pkt.Seq, errPayload("invalid payload"))
		return
	}
	callID, err := strconv.ParseInt(payload.CallID, 10, 64)
	if err != nil {
		conn.SendPayload(protocol.CmdError, pkt.Seq, errPayload("invalid call_id"))
		return
	}

	logger.L.Infof("call trace: hand_back recv user=%d call=%d seq=%d", conn.GetUserID(), callID, pkt.Seq)
	if routeOrSendCallRPC(context.Background(), hub, conn, pkt.Seq, callRPCRequest{
		Action: callRPCActionHandBack,
		CallID: callID,
		UserID: conn.GetUserID(),
	}) {
		return
	}
	if err := callCtrl.HandBack(context.Background(), callID, conn.GetUserID()); err != nil {
		logger.L.Warnf("call hand_back error call=%d user=%d err=%v", callID, conn.GetUserID(), err)
		conn.SendPayload(protocol.CmdError, pkt.Seq, errPayload(err.Error()))
		return
	}

	// 广播 AI 状态变更
	aiStatePayload := protocol.CallAIStatePayload{
		CallID: payload.CallID,
		Mode:   "ai_delegated",
		Ts:     time.Now().UnixMilli(),
	}
	conn.SendPayload(protocol.CmdCallState, pkt.Seq, aiStatePayload)
}
