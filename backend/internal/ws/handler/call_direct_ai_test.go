package handler

import (
	"encoding/json"
	"testing"

	"github.com/askie/grix/backend/internal/call"
	"github.com/askie/grix/backend/internal/ws/protocol"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// owner 直拨 AI 成功：返回 invite_ack（含 room token），bridge 已启动
func TestHandleCallDirectAI_Success(t *testing.T) {
	bridge, cleanup := setupCallAIHandlerTest(t)
	defer cleanup()

	conn := &callHandlerMockConn{userID: 7}
	payload, _ := json.Marshal(protocol.CallDirectAIPayload{AgentID: "42"})
	HandleCallDirectAI(newCallHandlerMockHub(), conn, &protocol.Packet{Cmd: protocol.CmdCallDirectAI, Seq: 1, Payload: payload})

	ack, ok := conn.findCmd(protocol.CmdCallInviteAck)
	require.True(t, ok, "should receive invite_ack")
	b, _ := json.Marshal(ack.payload)
	var got protocol.CallInviteAckPayload
	require.NoError(t, json.Unmarshal(b, &got))
	assert.NotEmpty(t, got.CallID)
	assert.NotEmpty(t, bridge.started)
	// 解析出的 BYOK spec 应一路透传到 StartBridge
	assert.Equal(t, "doubao_realtime", bridge.lastSpec.Provider)
	assert.Equal(t, "m", bridge.lastSpec.Model)
	assert.Equal(t, "k", bridge.lastSpec.APIKey)
	assert.Equal(t, int64(42), bridge.lastSpec.AgentID)
}

// 非 owner 被拒：不起 bridge
func TestHandleCallDirectAI_NotOwner(t *testing.T) {
	bridge, cleanup := setupCallAIHandlerTest(t)
	defer cleanup()
	ensureVoiceAgentOwner = func(_, _ int64) error { return assertOwnerErr }

	conn := &callHandlerMockConn{userID: 9}
	payload, _ := json.Marshal(protocol.CallDirectAIPayload{AgentID: "42"})
	HandleCallDirectAI(newCallHandlerMockHub(), conn, &protocol.Packet{Cmd: protocol.CmdCallDirectAI, Seq: 1, Payload: payload})

	_, ok := conn.findCmd(protocol.CmdError)
	assert.True(t, ok, "non-owner should get error")
	assert.Empty(t, bridge.started, "bridge must not start for non-owner")
}

// 配置不全（resolve 失败）被拒
func TestHandleCallDirectAI_UnconfiguredAgent(t *testing.T) {
	bridge, cleanup := setupCallAIHandlerTest(t)
	defer cleanup()
	resolveAgentVoiceSpec = func(_ int64, _ string) (call.VoiceBridgeSpec, error) { return call.VoiceBridgeSpec{}, assertOwnerErr }

	conn := &callHandlerMockConn{userID: 7}
	payload, _ := json.Marshal(protocol.CallDirectAIPayload{AgentID: "42"})
	HandleCallDirectAI(newCallHandlerMockHub(), conn, &protocol.Packet{Cmd: protocol.CmdCallDirectAI, Seq: 1, Payload: payload})

	_, ok := conn.findCmd(protocol.CmdError)
	assert.True(t, ok)
	assert.Empty(t, bridge.started)
}

func TestHandleCallDirectAI_DailyLimitReached(t *testing.T) {
	bridge, cleanup := setupCallAIHandlerTest(t)
	defer cleanup()
	reserveVoiceDailyQuota = func(_ int64, _ int) bool { return false }

	conn := &callHandlerMockConn{userID: 7}
	payload, _ := json.Marshal(protocol.CallDirectAIPayload{AgentID: "42"})
	HandleCallDirectAI(newCallHandlerMockHub(), conn, &protocol.Packet{Cmd: protocol.CmdCallDirectAI, Seq: 1, Payload: payload})

	_, ok := conn.findCmd(protocol.CmdError)
	assert.True(t, ok)
	assert.Empty(t, bridge.started, "bridge must not start after daily limit is reached")
}

var assertOwnerErr = errStr("denied")

type errStr string

func (e errStr) Error() string { return string(e) }
