package handler

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"

	"github.com/askie/grix/backend/internal/pkg/logger"
	"github.com/askie/grix/backend/internal/ws/protocol"
)

const maxCallClientDiagFieldLen = 500

// HandleCallClientDiag records client-side media connection stages for production diagnosis.
// 当客户端上报 room_connect_error 时自动触发 hangup 释放通话资源。
func HandleCallClientDiag(hub HubInterface, conn ConnInterface, pkt *protocol.Packet) {
	var payload protocol.CallClientDiagPayload
	if err := json.Unmarshal(pkt.Payload, &payload); err != nil {
		logger.L.Warnf("call client diag invalid payload user=%d device=%s err=%v", conn.GetUserID(), conn.GetDeviceID(), err)
		return
	}

	stage := sanitizeCallClientDiagField(payload.Stage)
	if stage == "" {
		return
	}
	logger.L.Infof(
		"call client diag: user=%d device=%s call=%s stage=%s detail=%s",
		conn.GetUserID(),
		conn.GetDeviceID(),
		sanitizeCallClientDiagField(payload.CallID),
		stage,
		sanitizeCallClientDiagField(payload.Detail),
	)

	// 客户端连接 LiveKit 房间失败时，主动清理通话资源
	if stage == "room_connect_error" && payload.CallID != "" {
		autoHangupOnConnectError(hub, conn, payload.CallID)
	}
}

// autoHangupOnConnectError 在客户端无法连接 LiveKit 房间时自动触发 hangup，
// 释放 busy guard 和 LiveKit 房间等资源。
func autoHangupOnConnectError(hub HubInterface, conn ConnInterface, rawCallID string) {
	if callCtrl == nil {
		return
	}
	callID, err := strconv.ParseInt(rawCallID, 10, 64)
	if err != nil {
		return
	}
	logger.L.Infof("call trace: auto_hangup room_connect_error user=%d call=%d", conn.GetUserID(), callID)

	ctx := context.Background()
	if routeOrSendCallRPC(ctx, hub, conn, 0, callRPCRequest{
		Action: callRPCActionHangup,
		CallID: callID,
		UserID: conn.GetUserID(),
	}) {
		return
	}
	if err := callCtrl.Hangup(ctx, callID, conn.GetUserID()); err != nil {
		logger.L.Warnf("call trace: auto_hangup failed call=%d user=%d err=%v", callID, conn.GetUserID(), err)
		return
	}
	forgetCallOwner(ctx, callID)
	logger.L.Infof("call trace: auto_hangup done call=%d user=%d", callID, conn.GetUserID())
}

func sanitizeCallClientDiagField(value string) string {
	value = strings.ReplaceAll(value, "\n", " ")
	value = strings.ReplaceAll(value, "\r", " ")
	if len(value) > maxCallClientDiagFieldLen {
		return value[:maxCallClientDiagFieldLen]
	}
	return value
}
