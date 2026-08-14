package handler

import (
	"encoding/json"
	"testing"

	"github.com/askie/grix/backend/internal/ws/protocol"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 配置了语音托管：来电被服务端自动 AI 代接，不响铃。
func TestHandleCallInvite_AutoDelegate(t *testing.T) {
	bridge, cleanup := setupCallAIHandlerTest(t)
	defer cleanup()
	resolveCalleeVoiceAgent = func(_ int64, _ string) (int64, bool) { return 42, true }

	hub := newCallHandlerMockHub()
	callerConn := &callHandlerMockConn{userID: 1}
	calleeConn := &callHandlerMockConn{userID: 2}
	hub.addConn(2, calleeConn)

	invitePayload, _ := json.Marshal(protocol.CallInvitePayload{PeerID: "2", PeerType: "user", CallMode: 1})
	HandleCallInvite(hub, callerConn, &protocol.Packet{Cmd: protocol.CmdCallInvite, Seq: 1, Payload: invitePayload})

	// 自动代接：bridge 已启动，且 callee 未收到响铃
	assert.NotEmpty(t, bridge.started, "auto-delegate should start bridge")
	_, rang := calleeConn.findCmd(protocol.CmdCallRing)
	assert.False(t, rang, "auto-delegate should NOT ring the callee")
	// caller 仍收到 invite_ack
	_, ok := callerConn.findCmd(protocol.CmdCallInviteAck)
	require.True(t, ok)
}

// 未配置语音托管：在线 callee 正常响铃，不起 bridge。
func TestHandleCallInvite_NoDelegate_Rings(t *testing.T) {
	bridge, cleanup := setupCallAIHandlerTest(t)
	defer cleanup()
	// 默认 resolveCalleeVoiceAgent 返回 (0,false)

	hub := newCallHandlerMockHub()
	callerConn := &callHandlerMockConn{userID: 1}
	calleeConn := &callHandlerMockConn{userID: 2}
	hub.addConn(2, calleeConn)

	invitePayload, _ := json.Marshal(protocol.CallInvitePayload{PeerID: "2", PeerType: "user", CallMode: 1})
	HandleCallInvite(hub, callerConn, &protocol.Packet{Cmd: protocol.CmdCallInvite, Seq: 1, Payload: invitePayload})

	_, rang := calleeConn.findCmd(protocol.CmdCallRing)
	assert.True(t, rang, "no delegate should ring the callee")
	assert.Empty(t, bridge.started, "no delegate should not start bridge")
}
