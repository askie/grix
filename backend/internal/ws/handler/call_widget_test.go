package handler

import (
	"encoding/json"
	"testing"

	"github.com/askie/grix/backend/internal/call"
	"github.com/askie/grix/backend/internal/ws/protocol"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func widgetInvitePkt() *protocol.Packet {
	p, _ := json.Marshal(protocol.CallInvitePayload{CallMode: 1})
	return &protocol.Packet{Cmd: protocol.CmdCallInvite, Seq: 1, Payload: p}
}

// 访客通话：owner 配置了语音托管且 agent 允许访客 → 自动代接，访客收 invite_ack，owner 收可接管通知。
func TestHandleWidgetCallInvite_Success(t *testing.T) {
	bridge, cleanup := setupCallAIHandlerTest(t)
	defer cleanup()
	resolveCalleeVoiceAgent = func(_ int64, _ string) (int64, bool) { return 42, true }
	resolveAgentVoiceSpec = func(id int64, _ string) (call.VoiceBridgeSpec, error) {
		return call.VoiceBridgeSpec{AgentID: id, Provider: "openai_realtime", Model: "m", APIKey: "k", AllowVisitor: true}, nil
	}

	hub := newCallHandlerMockHub()
	visitorConn := &callHandlerMockConn{userID: 9001}
	ownerConn := &callHandlerMockConn{userID: 100}
	hub.addConn(100, ownerConn)

	HandleWidgetCallInvite(hub, visitorConn, widgetInvitePkt(), 100, "s_widget_1", "Visitor")

	_, gotAck := visitorConn.findCmd(protocol.CmdCallInviteAck)
	assert.True(t, gotAck, "visitor should get invite_ack")
	assert.NotEmpty(t, bridge.started, "auto-delegate should start bridge")
	_, ownerNotified := ownerConn.findCmd(protocol.CmdCallAiDelegated)
	assert.True(t, ownerNotified, "owner should be notified for takeover")
}

// agent 未开放访客通话 → 拒绝。
func TestHandleWidgetCallInvite_VisitorNotAllowed(t *testing.T) {
	bridge, cleanup := setupCallAIHandlerTest(t)
	defer cleanup()
	resolveCalleeVoiceAgent = func(_ int64, _ string) (int64, bool) { return 42, true }
	resolveAgentVoiceSpec = func(id int64, _ string) (call.VoiceBridgeSpec, error) {
		return call.VoiceBridgeSpec{AgentID: id, Provider: "openai_realtime", Model: "m", APIKey: "k", AllowVisitor: false}, nil
	}
	conn := &callHandlerMockConn{userID: 9001}
	HandleWidgetCallInvite(newCallHandlerMockHub(), conn, widgetInvitePkt(), 100, "s1", "V")
	_, gotErr := conn.findCmd(protocol.CmdError)
	require.True(t, gotErr)
	assert.Empty(t, bridge.started)
}

// owner 未配置语音托管 → 拒绝。
func TestHandleWidgetCallInvite_NoDelegate(t *testing.T) {
	bridge, cleanup := setupCallAIHandlerTest(t)
	defer cleanup() // 默认 resolveCalleeVoiceAgent=(0,false)
	conn := &callHandlerMockConn{userID: 9001}
	HandleWidgetCallInvite(newCallHandlerMockHub(), conn, widgetInvitePkt(), 100, "s1", "V")
	_, gotErr := conn.findCmd(protocol.CmdError)
	require.True(t, gotErr)
	assert.Empty(t, bridge.started)
}

// 频率超限 → 拒绝。
func TestHandleWidgetCallInvite_RateLimited(t *testing.T) {
	bridge, cleanup := setupCallAIHandlerTest(t)
	defer cleanup()
	resolveCalleeVoiceAgent = func(_ int64, _ string) (int64, bool) { return 42, true }
	reserveVisitorCallQuota = func(_ int64) bool { return false }
	conn := &callHandlerMockConn{userID: 9001}
	HandleWidgetCallInvite(newCallHandlerMockHub(), conn, widgetInvitePkt(), 100, "s1", "V")
	_, gotErr := conn.findCmd(protocol.CmdError)
	require.True(t, gotErr)
	assert.Empty(t, bridge.started)
}
