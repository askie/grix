package handler

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"time"

	"github.com/askie/grix/backend/internal/pkg/logger"
	"github.com/askie/grix/backend/internal/ws/protocol"
)

// HandleCallListen 处理 owner 请求进房旁听某通 AI 代接通话（AI_DELEGATED → owner 旁听）。
// 抢"参与锁"实现"人单线 + 多设备互斥"：若 owner 已在其它设备/其它通话中则拒绝。
// 成功则签发 callee room token，owner 静音进房旁听，可再点接管。
func HandleCallListen(hub HubInterface, conn ConnInterface, pkt *protocol.Packet) {
	if callCtrl == nil {
		conn.SendPayload(protocol.CmdError, pkt.Seq, errPayload("call service unavailable"))
		return
	}
	var payload protocol.CallListenPayload
	if err := json.Unmarshal(pkt.Payload, &payload); err != nil {
		conn.SendPayload(protocol.CmdError, pkt.Seq, errPayload("invalid payload"))
		return
	}
	callID, err := strconv.ParseInt(payload.CallID, 10, 64)
	if err != nil {
		conn.SendPayload(protocol.CmdError, pkt.Seq, errPayload("invalid call_id"))
		return
	}

	logger.L.Infof("call trace: listen recv user=%d call=%d device=%s seq=%d", conn.GetUserID(), callID, conn.GetDeviceID(), pkt.Seq)
	if routeOrSendCallRPC(context.Background(), hub, conn, pkt.Seq, callRPCRequest{
		Action:   callRPCActionListen,
		CallID:   callID,
		UserID:   conn.GetUserID(),
		DeviceID: conn.GetDeviceID(),
	}) {
		return
	}
	resp := doCallListen(context.Background(), callID, conn.GetUserID(), conn.GetDeviceID())
	deliverCallRPCResponse(conn, pkt.Seq, resp)
}

// doCallListen 在通话 owner 节点执行旁听准入：抢参与锁 + 签发 callee token。
func doCallListen(ctx context.Context, callID, ownerID int64, deviceID string) callRPCResponse {
	ok, holder := acquireParticipateLock(ctx, ownerID, callID, deviceID)
	if !ok {
		logger.L.Infof("call trace: listen rejected_busy owner=%d call=%d holder=%s", ownerID, callID, holder)
		return callRPCResponse{OK: false, Error: "您正在其他设备或通话中，请先结束当前通话"}
	}
	token, roomURL, err := callCtrl.ListenToken(callID, ownerID)
	if err != nil {
		releaseParticipateLock(ctx, ownerID, callID, deviceID)
		return callRPCError(err)
	}
	logger.L.Infof("call trace: listen ok owner=%d call=%d room_url=%s", ownerID, callID, roomURL)
	return callRPCPacket(protocol.CmdCallListenAck, protocol.CallListenAckPayload{
		CallID:     strconv.FormatInt(callID, 10),
		RoomToken:  token,
		RoomURL:    roomURL,
		ICEServers: callICEServers(),
	})
}

// HandleCallLeave 处理 owner 离开通话但不结束（访客继续与 AI 通话）。
// 若 owner 已接管(HUMAN_ACTIVE)则先交回 AI（AI 恢复应答），再释放参与锁；
// 若仅旁听(AI_DELEGATED)则只释放参与锁。与 call:hangup（结束整通）语义区分。
func HandleCallLeave(hub HubInterface, conn ConnInterface, pkt *protocol.Packet) {
	if callCtrl == nil {
		conn.SendPayload(protocol.CmdError, pkt.Seq, errPayload("call service unavailable"))
		return
	}
	var payload protocol.CallLeavePayload
	if err := json.Unmarshal(pkt.Payload, &payload); err != nil {
		conn.SendPayload(protocol.CmdError, pkt.Seq, errPayload("invalid payload"))
		return
	}
	callID, err := strconv.ParseInt(payload.CallID, 10, 64)
	if err != nil {
		conn.SendPayload(protocol.CmdError, pkt.Seq, errPayload("invalid call_id"))
		return
	}

	logger.L.Infof("call trace: leave recv user=%d call=%d device=%s seq=%d", conn.GetUserID(), callID, conn.GetDeviceID(), pkt.Seq)
	if routeOrSendCallRPC(context.Background(), hub, conn, pkt.Seq, callRPCRequest{
		Action:   callRPCActionLeave,
		CallID:   callID,
		UserID:   conn.GetUserID(),
		DeviceID: conn.GetDeviceID(),
	}) {
		return
	}
	resp := doCallLeave(context.Background(), callID, conn.GetUserID(), conn.GetDeviceID())
	deliverCallRPCResponse(conn, pkt.Seq, resp)
}

// doCallLeave 在通话 owner 节点执行离开：已接管则交回 AI，释放参与锁。
func doCallLeave(ctx context.Context, callID, ownerID int64, deviceID string) callRPCResponse {
	// 已接管(HUMAN_ACTIVE)则交回 AI，让访客继续与 AI 通话；
	// 仅旁听(AI_DELEGATED)或通话已结束时 HandBack 返回良性错误，忽略即可。
	if err := callCtrl.HandBack(ctx, callID, ownerID); err != nil && !isBenignLeaveErr(err) {
		releaseParticipateLock(ctx, ownerID, callID, deviceID)
		return callRPCError(err)
	}
	releaseParticipateLock(ctx, ownerID, callID, deviceID)
	logger.L.Infof("call trace: leave done owner=%d call=%d", ownerID, callID)
	return callRPCPacket(protocol.CmdCallState, protocol.CallAIStatePayload{
		CallID: strconv.FormatInt(callID, 10),
		Mode:   "ai_delegated",
		Ts:     time.Now().UnixMilli(),
	})
}

// ReleaseParticipateOnDisconnect 供 ws.Hub 在设备断开时调用，
// 释放该设备可能持有的 owner 参与锁，避免设备崩溃后锁悬挂阻塞其它设备。
func ReleaseParticipateOnDisconnect(userID int64, deviceID string) {
	releaseParticipateLockByDevice(context.Background(), userID, deviceID)
}

// isBenignLeaveErr 判断 leave 时 HandBack 的错误是否可忽略：
// owner 仅旁听（非 human_active）或通话已结束（not found）时，离开不应报错。
func isBenignLeaveErr(err error) bool {
	if err == nil {
		return true
	}
	msg := err.Error()
	return strings.Contains(msg, "not human_active") || strings.Contains(msg, "not found")
}
