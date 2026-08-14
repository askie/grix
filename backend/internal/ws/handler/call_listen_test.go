package handler

import (
	"encoding/json"
	"testing"

	"github.com/askie/grix/backend/internal/call"
	"github.com/askie/grix/backend/internal/ws/protocol"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setupWidgetDelegatedCall 走 widget 邀请流程建立一通 AI_DELEGATED 通话，返回 callID。
func setupWidgetDelegatedCall(t *testing.T, hub *callHandlerMockHub, visitorID, ownerID int64) string {
	t.Helper()
	resolveCalleeVoiceAgent = func(_ int64, _ string) (int64, bool) { return 42, true }
	resolveAgentVoiceSpec = func(id int64, _ string) (call.VoiceBridgeSpec, error) {
		return call.VoiceBridgeSpec{AgentID: id, Provider: "openai_realtime", Model: "m", APIKey: "k", AllowVisitor: true, MaxConcurrent: 5}, nil
	}
	visitorConn := &callHandlerMockConn{userID: visitorID}
	HandleWidgetCallInvite(hub, visitorConn, widgetInvitePkt(), ownerID, "s_widget_1", "Visitor")
	ack, ok := visitorConn.findCmd(protocol.CmdCallInviteAck)
	require.True(t, ok, "widget invite should ack")
	return ack.payload.(protocol.CallInviteAckPayload).CallID
}

func listenPkt(callID string, seq int64) *protocol.Packet {
	p, _ := json.Marshal(protocol.CallListenPayload{CallID: callID})
	return &protocol.Packet{Cmd: protocol.CmdCallListen, Seq: seq, Payload: p}
}

func leavePkt(callID string, seq int64) *protocol.Packet {
	p, _ := json.Marshal(protocol.CallLeavePayload{CallID: callID})
	return &protocol.Packet{Cmd: protocol.CmdCallLeave, Seq: seq, Payload: p}
}

// owner 旁听：抢参与锁成功并拿到 callee token；第二台设备被参与锁拒绝（多设备互斥）。
func TestHandleCallListen_LockAndMultiDeviceExclusion(t *testing.T) {
	_, cleanup := setupCallAIHandlerTest(t)
	defer cleanup()
	defer withMockRedisForCall(t)()

	hub := newCallHandlerMockHub()
	callID := setupWidgetDelegatedCall(t, hub, 9001, 100)

	devA := &callHandlerMockConn{userID: 100, deviceID: "devA"}
	HandleCallListen(hub, devA, listenPkt(callID, 1))
	_, gotAck := devA.findCmd(protocol.CmdCallListenAck)
	assert.True(t, gotAck, "设备A旁听应拿到 listen_ack")

	devB := &callHandlerMockConn{userID: 100, deviceID: "devB"}
	HandleCallListen(hub, devB, listenPkt(callID, 2))
	_, gotAckB := devB.findCmd(protocol.CmdCallListenAck)
	_, gotErrB := devB.findCmd(protocol.CmdError)
	assert.False(t, gotAckB, "设备B不应拿到 listen_ack")
	assert.True(t, gotErrB, "设备B应被参与锁拒绝")
}

// owner 离开后释放参与锁，另一设备可再次进入。
func TestHandleCallLeave_ReleasesParticipateLock(t *testing.T) {
	_, cleanup := setupCallAIHandlerTest(t)
	defer cleanup()
	defer withMockRedisForCall(t)()

	hub := newCallHandlerMockHub()
	callID := setupWidgetDelegatedCall(t, hub, 9001, 100)

	devA := &callHandlerMockConn{userID: 100, deviceID: "devA"}
	HandleCallListen(hub, devA, listenPkt(callID, 1))
	_, gotAck := devA.findCmd(protocol.CmdCallListenAck)
	require.True(t, gotAck)

	// devA 离开（仅旁听，AI 继续）→ 释放参与锁
	HandleCallLeave(hub, devA, leavePkt(callID, 2))
	_, gotState := devA.findCmd(protocol.CmdCallState)
	assert.True(t, gotState, "leave 应回 call:state")

	// devB 现在可以进入
	devB := &callHandlerMockConn{userID: 100, deviceID: "devB"}
	HandleCallListen(hub, devB, listenPkt(callID, 3))
	_, gotAckB := devB.findCmd(protocol.CmdCallListenAck)
	assert.True(t, gotAckB, "devA 离开后 devB 应能旁听")
}

// owner 已接管(HUMAN_ACTIVE)后离开 → 交回 AI（重启 bridge），访客继续与 AI 通话。
func TestHandleCallLeave_AfterTakeoverHandsBackToAI(t *testing.T) {
	bridge, cleanup := setupCallAIHandlerTest(t)
	defer cleanup()
	defer withMockRedisForCall(t)()

	hub := newCallHandlerMockHub()
	callID := setupWidgetDelegatedCall(t, hub, 9001, 100)
	startedAfterInvite := len(bridge.started)

	devA := &callHandlerMockConn{userID: 100, deviceID: "devA"}
	HandleCallListen(hub, devA, listenPkt(callID, 1))

	// 接管：AI_DELEGATED → HUMAN_ACTIVE
	takeoverPayload, _ := json.Marshal(protocol.CallTakeoverPayload{CallID: callID})
	HandleCallTakeover(hub, devA, &protocol.Packet{Cmd: protocol.CmdCallTakeover, Seq: 2, Payload: takeoverPayload})

	// 离开：HUMAN_ACTIVE → 交回 AI，恢复 AI 发声（unmute，不重建 session）
	HandleCallLeave(hub, devA, leavePkt(callID, 3))
	assert.Len(t, bridge.unmuted, 1, "leave 后应恢复 AI 发声（交回 AI）")
	assert.Equal(t, startedAfterInvite, len(bridge.started), "交回不重建 session，started 不变")
}
