package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/askie/grix/backend/internal/call"
	"github.com/askie/grix/backend/internal/pkg/logger"
	"github.com/askie/grix/backend/internal/pkg/snowflake"
	"github.com/askie/grix/backend/internal/ws/protocol"
)

const callStateReasonAnsweredElsewhere = "answered_elsewhere"

// HandleCallInvite 处理 A 发起呼叫。
func HandleCallInvite(hub HubInterface, conn ConnInterface, pkt *protocol.Packet) {
	if callCtrl == nil {
		conn.SendPayload(protocol.CmdError, pkt.Seq, errPayload("call service unavailable"))
		return
	}
	var payload protocol.CallInvitePayload
	if err := json.Unmarshal(pkt.Payload, &payload); err != nil {
		conn.SendPayload(protocol.CmdError, pkt.Seq, errPayload("invalid payload"))
		return
	}
	calleeID, err := strconv.ParseInt(payload.PeerID, 10, 64)
	if err != nil {
		conn.SendPayload(protocol.CmdError, pkt.Seq, errPayload("invalid peer_id"))
		return
	}
	callerID := conn.GetUserID()
	logger.L.Infof("call trace: invite recv caller=%d callee=%d seq=%d", callerID, calleeID, pkt.Seq)

	// 好友关系验证：只允许好友之间通话，且对方未拉黑自己
	if err := validateCallPermission(callerID, calleeID); err != nil {
		conn.SendPayload(protocol.CmdError, pkt.Seq, errPayload(err.Error()))
		return
	}

	// 解析真实会话 ID（通话记录与 Phase 3 内容回灌均落入此会话）
	sessionID, err := resolveCallSession(callerID, calleeID)
	if err != nil {
		logger.L.Errorf("call invite resolve session caller=%d callee=%d err=%v", callerID, calleeID, err)
		conn.SendPayload(protocol.CmdError, pkt.Seq, errPayload("internal error"))
		return
	}

	ctx := context.Background()
	callID := snowflake.GenID()
	busyGuard, err := reserveCallBusy(ctx, callID, callerID, calleeID)
	if err != nil {
		if errors.Is(err, call.ErrCalleeBusy) || errors.Is(err, call.ErrCallerBusy) {
			conn.SendPayload(protocol.CmdCallBusy, pkt.Seq, protocol.CallStatePayload{State: protocol.CallStateMissed, Reason: "busy"})
			return
		}
		logger.L.Errorf("call invite busy guard error caller=%d callee=%d call=%d err=%v", callerID, calleeID, callID, err)
		conn.SendPayload(protocol.CmdError, pkt.Seq, errPayload("internal error"))
		return
	}
	ownerCommitted := false
	defer func() {
		if !ownerCommitted {
			busyGuard.release(ctx)
		}
	}()

	_, tokenCaller, roomURL, err := callCtrl.InviteWithID(ctx, callID, callerID, calleeID, sessionID)
	if err != nil {
		if errors.Is(err, call.ErrCalleeBusy) || errors.Is(err, call.ErrCallerBusy) {
			conn.SendPayload(protocol.CmdCallBusy, pkt.Seq, protocol.CallStatePayload{State: protocol.CallStateMissed, Reason: "busy"})
			return
		}
		if errors.Is(err, call.ErrSelfCall) {
			conn.SendPayload(protocol.CmdError, pkt.Seq, errPayload("cannot call yourself"))
			return
		}
		logger.L.Errorf("call invite error caller=%d callee=%d err=%v", callerID, calleeID, err)
		conn.SendPayload(protocol.CmdError, pkt.Seq, errPayload("internal error"))
		return
	}
	if err := rememberCallOwner(ctx, callID, hub.GetNodeID()); err != nil {
		logger.L.Errorf("call invite owner guard error caller=%d callee=%d call=%d node=%s err=%v", callerID, calleeID, callID, hub.GetNodeID(), err)
		_ = callCtrl.Hangup(ctx, callID, callerID)
		conn.SendPayload(protocol.CmdError, pkt.Seq, errPayload("internal error"))
		return
	}
	ownerCommitted = true

	// 回复 caller invite_ack（含 call_id + room token + ICE 服务器）
	logger.L.Infof("call trace: invite ack caller=%d callee=%d call=%d room_url=%s", callerID, calleeID, callID, roomURL)
	conn.SendPayload(protocol.CmdCallInviteAck, pkt.Seq, protocol.CallInviteAckPayload{
		CallID:     fmt.Sprintf("%d", callID),
		RoomToken:  tokenCaller,
		RoomURL:    roomURL,
		ICEServers: callICEServers(),
	})

	// 语音自动托管（电话秘书）：callee 配置了会话级/用户级语音托管时，
	// 服务端直接 AI 代接，无需 callee 确认，离线也能接；解析/代接失败则回退到响铃。
	if agentID, ok := resolveCalleeVoiceAgent(calleeID, sessionID); ok {
		if spec, serr := resolveAgentVoiceSpec(agentID, ""); serr != nil {
			logger.L.Warnf("call auto-delegate resolve failed callee=%d agent=%d err=%v; fallback to ring", calleeID, agentID, serr)
		} else if !reserveVoiceDailyQuota(agentID, spec.DailyLimit) {
			logger.L.Warnf("call auto-delegate daily limit reached agent=%d call=%d; fallback to ring", agentID, callID)
		} else if _, _, aerr := callCtrl.AnswerWithAI(context.Background(), callID, calleeID, spec); aerr != nil {
			logger.L.Warnf("call auto-delegate answer failed callee=%d agent=%d call=%d err=%v; fallback to ring", calleeID, agentID, callID, aerr)
		} else {
			logger.L.Infof("call auto-delegated callee=%d agent=%d call=%d", calleeID, agentID, callID)
			// 通知 owner（callee）：AI 正在代接，可随时旁听接管（不预发 room token）
			notifyOwnerAiDelegated(hub, calleeID, callID, sessionID, lookupCallerName(callerID))
			return
		}
	}

	// 查询 caller 昵称，用于来电显示
	callerName := lookupCallerName(callerID)
	ringPayload := protocol.CallRingPayload{
		CallID:     fmt.Sprintf("%d", callID),
		CallerID:   fmt.Sprintf("%d", callerID),
		CallerName: callerName,
		CallMode:   payload.CallMode,
	}

	localDelivered, remoteDelivered := broadcastToUserExceptDeviceWithOptions(
		hub,
		context.Background(),
		calleeID,
		"",
		protocol.CmdCallRing,
		ringPayload,
		false,
	)
	if localDelivered == 0 && remoteDelivered == 0 {
		// callee 离线：发送普通离线推送通知（非 VoIP），让坐席知道有语音来电。
		// ForcePush=true 确保不跳过"在线"但 app 在后台的设备。
		logger.L.Infof("call invite: callee offline, enqueue offline push callee=%d call=%d", calleeID, callID)
		enqueueOfflinePushTask(calleeID, protocol.CmdPushMsg, protocol.PushMsgPayload{
			MsgID:         callID,
			SessionID:     sessionID,
			SenderID:      callerID,
			SenderType:    1,
			Content:       "[语音通话]",
			MsgType:       1,
			ForcePush:     true,
			TimeSensitive: true,
		})
		return
	}
}

// notifyOwnerAiDelegated 通知 owner 的所有在线连接：AI 正在代接，可通过 call:listen 进房旁听。
// room token/url 不在此下发，待 owner 发 call:listen 时按需签发。
func notifyOwnerAiDelegated(hub HubInterface, ownerID, callID int64, sessionID, peerName string) {
	logger.L.Infof("call trace: notify_owner_ai_delegated owner=%d call=%d session=%s peer=%s", ownerID, callID, sessionID, peerName)
	payload := protocol.CallAiDelegatedPayload{
		CallID:    fmt.Sprintf("%d", callID),
		SessionID: sessionID,
		PeerName:  peerName,
	}
	broadcastToUser(hub, context.Background(), ownerID, protocol.CmdCallAiDelegated, payload)
}

// HandleCallAnswer 处理 B 接听。
func HandleCallAnswer(hub HubInterface, conn ConnInterface, pkt *protocol.Packet) {
	if callCtrl == nil {
		conn.SendPayload(protocol.CmdError, pkt.Seq, errPayload("call service unavailable"))
		return
	}
	var payload protocol.CallAnswerPayload
	if err := json.Unmarshal(pkt.Payload, &payload); err != nil {
		conn.SendPayload(protocol.CmdError, pkt.Seq, errPayload("invalid payload"))
		return
	}
	callID, err := strconv.ParseInt(payload.CallID, 10, 64)
	if err != nil {
		conn.SendPayload(protocol.CmdError, pkt.Seq, errPayload("invalid call_id"))
		return
	}

	logger.L.Infof("call trace: answer recv user=%d call=%d seq=%d", conn.GetUserID(), callID, pkt.Seq)
	if routeOrSendCallRPC(context.Background(), hub, conn, pkt.Seq, callRPCRequest{
		Action:   callRPCActionAnswer,
		CallID:   callID,
		UserID:   conn.GetUserID(),
		DeviceID: conn.GetDeviceID(),
	}) {
		return
	}
	tokenCallee, roomURL, err := callCtrl.Answer(context.Background(), callID, conn.GetUserID())
	if err != nil {
		logger.L.Warnf("call answer error call=%d user=%d err=%v", callID, conn.GetUserID(), err)
		conn.SendPayload(protocol.CmdError, pkt.Seq, errPayload(err.Error()))
		return
	}
	notifyCallAnsweredElsewhere(hub, conn.GetUserID(), callID, conn.GetDeviceID())

	conn.SendPayload(protocol.CmdCallPeerAnswered, pkt.Seq, protocol.CallPeerAnsweredPayload{
		CallID:     payload.CallID,
		Mode:       "human",
		RoomToken:  tokenCallee,
		RoomURL:    roomURL,
		ICEServers: callICEServers(),
	})
	logger.L.Infof("call trace: answer ack user=%d call=%d room_url=%s", conn.GetUserID(), callID, roomURL)
}

func notifyCallAnsweredElsewhere(hub HubInterface, userID, callID int64, answeredDeviceID string) {
	if hub == nil || userID <= 0 || callID <= 0 || answeredDeviceID == "" {
		return
	}
	broadcastToUserExceptDevice(
		hub,
		context.Background(),
		userID,
		answeredDeviceID,
		protocol.CmdCallState,
		protocol.CallStatePayload{
			CallID:           fmt.Sprintf("%d", callID),
			State:            protocol.CallStateActive,
			Reason:           callStateReasonAnsweredElsewhere,
			Ts:               time.Now().UnixMilli(),
			AnsweredDeviceID: answeredDeviceID,
		},
	)
}

// HandleCallReject 处理 B 拒接。
func HandleCallReject(hub HubInterface, conn ConnInterface, pkt *protocol.Packet) {
	if callCtrl == nil {
		return
	}
	var payload protocol.CallRejectPayload
	if err := json.Unmarshal(pkt.Payload, &payload); err != nil {
		conn.SendPayload(protocol.CmdError, pkt.Seq, errPayload("invalid payload"))
		return
	}
	callID, err := strconv.ParseInt(payload.CallID, 10, 64)
	if err != nil {
		conn.SendPayload(protocol.CmdError, pkt.Seq, errPayload("invalid call_id"))
		return
	}
	logger.L.Infof("call trace: reject recv user=%d call=%d seq=%d", conn.GetUserID(), callID, pkt.Seq)
	if routeOrSendCallRPC(context.Background(), hub, conn, pkt.Seq, callRPCRequest{
		Action: callRPCActionReject,
		CallID: callID,
		UserID: conn.GetUserID(),
		Reason: payload.Reason,
	}) {
		return
	}
	if err := callCtrl.Reject(context.Background(), callID, conn.GetUserID(), payload.Reason); err != nil {
		logger.L.Warnf("call reject error call=%d user=%d err=%v", callID, conn.GetUserID(), err)
		return
	}
	forgetCallOwner(context.Background(), callID)
}

// HandleCallHangup 处理任一方挂断。
func HandleCallHangup(hub HubInterface, conn ConnInterface, pkt *protocol.Packet) {
	if callCtrl == nil {
		return
	}
	var payload protocol.CallHangupPayload
	if err := json.Unmarshal(pkt.Payload, &payload); err != nil {
		conn.SendPayload(protocol.CmdError, pkt.Seq, errPayload("invalid payload"))
		return
	}
	callID, err := strconv.ParseInt(payload.CallID, 10, 64)
	if err != nil {
		logger.L.Warnf("call trace: hangup invalid_call_id user=%d raw_call_id=%q seq=%d", conn.GetUserID(), payload.CallID, pkt.Seq)
		conn.SendPayload(protocol.CmdError, pkt.Seq, errPayload("invalid call_id"))
		return
	}
	logger.L.Infof("call trace: hangup recv user=%d call=%d raw_call_id=%q seq=%d", conn.GetUserID(), callID, payload.CallID, pkt.Seq)
	if routeOrSendCallRPC(context.Background(), hub, conn, pkt.Seq, callRPCRequest{
		Action: callRPCActionHangup,
		CallID: callID,
		UserID: conn.GetUserID(),
	}) {
		return
	}
	if err := callCtrl.Hangup(context.Background(), callID, conn.GetUserID()); err != nil {
		logger.L.Warnf("call hangup error call=%d user=%d err=%v", callID, conn.GetUserID(), err)
		// 可能是排队中的 callID（尚未进入活跃通话），尝试取消队列条目
		cancelQueueByCallID(context.Background(), callID, hub)
		return
	}
	forgetCallOwner(context.Background(), callID)
	logger.L.Infof("call trace: hangup done user=%d call=%d seq=%d", conn.GetUserID(), callID, pkt.Seq)
}

func errPayload(msg string) map[string]string { return map[string]string{"msg": msg} }
