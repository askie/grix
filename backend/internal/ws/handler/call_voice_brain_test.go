package handler

import (
	"encoding/json"
	"testing"

	"github.com/askie/grix/backend/internal/ws/protocol"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setupVoiceBrainTest 在 direct_ai 测试桩基础上补两个语音大脑专用 seam：
// loadVoiceBrainAgentID 返回固定语音大脑 id；ensureTextBrainAgent 放行。
func setupVoiceBrainTest(t *testing.T) (*mockBridgeManager, func()) {
	t.Helper()
	bridge, base := setupCallAIHandlerTest(t)
	origLoad := loadVoiceBrainAgentID
	loadVoiceBrainAgentID = func(_ int64) (int64, bool) { return 99, true }
	origText := ensureTextBrainAgent
	ensureTextBrainAgent = func(_, _ int64) error { return nil }
	origRealtime := loadVoiceBrainRealtime
	loadVoiceBrainRealtime = func(_ int64) bool { return true } // 默认实时互动
	return bridge, func() {
		loadVoiceBrainAgentID = origLoad
		ensureTextBrainAgent = origText
		loadVoiceBrainRealtime = origRealtime
		base()
	}
}

// owner 在文字 agent 会话里发起语音大脑通话：媒体 spec 取自语音大脑(99)，
// 通话锚定到前端传入的 session_id；返回 invite_ack 且 bridge 已启动。
func TestHandleCallVoiceBrain_Success(t *testing.T) {
	bridge, cleanup := setupVoiceBrainTest(t)
	defer cleanup()

	origEnsure := ensureAgentInSession
	ensureAgentInSession = func(_ string, _ int64) error { return nil }
	defer func() { ensureAgentInSession = origEnsure }()

	conn := &callHandlerMockConn{userID: 7}
	payload, _ := json.Marshal(protocol.CallVoiceBrainPayload{AgentID: "42", SessionID: "sess-work"})
	HandleCallVoiceBrain(newCallHandlerMockHub(), conn, &protocol.Packet{Cmd: protocol.CmdCallVoiceBrain, Seq: 1, Payload: payload})

	ack, ok := conn.findCmd(protocol.CmdCallInviteAck)
	require.True(t, ok, "should receive invite_ack")
	b, _ := json.Marshal(ack.payload)
	var got protocol.CallInviteAckPayload
	require.NoError(t, json.Unmarshal(b, &got))
	assert.NotEmpty(t, got.CallID)
	assert.NotEmpty(t, bridge.started, "bridge must start")
	assert.Equal(t, int64(99), bridge.lastSpec.AgentID, "media spec must come from voice brain agent")
	assert.Equal(t, "doubao_realtime", bridge.lastSpec.Provider)
	assert.Equal(t, "sess-work", bridge.lastSpec.SessionID)
	assert.False(t, bridge.lastSpec.RelayMode, "voice brain default runs realtime (RelayMode=false)")
	assert.Empty(t, bridge.lastSpec.SystemPrompt, "voice brain must NOT override agent persona; it is defined by the external agent config")
}

// 念稿兜底模式（voice_brain_realtime=false）：spec.RelayMode=true → 走 STT+TTS 念稿。
func TestHandleCallVoiceBrain_PipelineFallback(t *testing.T) {
	bridge, cleanup := setupVoiceBrainTest(t)
	defer cleanup()
	loadVoiceBrainRealtime = func(_ int64) bool { return false }

	origEnsure := ensureAgentInSession
	ensureAgentInSession = func(_ string, _ int64) error { return nil }
	defer func() { ensureAgentInSession = origEnsure }()

	conn := &callHandlerMockConn{userID: 7}
	payload, _ := json.Marshal(protocol.CallVoiceBrainPayload{AgentID: "42", SessionID: "sess-work"})
	HandleCallVoiceBrain(newCallHandlerMockHub(), conn, &protocol.Packet{Cmd: protocol.CmdCallVoiceBrain, Seq: 1, Payload: payload})

	_, ok := conn.findCmd(protocol.CmdCallInviteAck)
	require.True(t, ok, "should receive invite_ack")
	assert.NotEmpty(t, bridge.started, "bridge must start")
	assert.True(t, bridge.lastSpec.RelayMode, "pipeline fallback runs relay mode (RelayMode=true)")
}

// session_id 为空直接报错，不存在 fallback。
func TestHandleCallVoiceBrain_MissingSessionID(t *testing.T) {
	bridge, cleanup := setupVoiceBrainTest(t)
	defer cleanup()

	conn := &callHandlerMockConn{userID: 7}
	payload, _ := json.Marshal(protocol.CallVoiceBrainPayload{AgentID: "42"})
	HandleCallVoiceBrain(newCallHandlerMockHub(), conn, &protocol.Packet{Cmd: protocol.CmdCallVoiceBrain, Seq: 1, Payload: payload})

	_, ok := conn.findCmd(protocol.CmdError)
	assert.True(t, ok, "missing session_id should error")
	assert.Empty(t, bridge.started)
}

// 未设置语音大脑：报错，不起 bridge
func TestHandleCallVoiceBrain_NoBrainConfigured(t *testing.T) {
	bridge, cleanup := setupVoiceBrainTest(t)
	defer cleanup()
	loadVoiceBrainAgentID = func(_ int64) (int64, bool) { return 0, false }

	conn := &callHandlerMockConn{userID: 7}
	payload, _ := json.Marshal(protocol.CallVoiceBrainPayload{AgentID: "42"})
	HandleCallVoiceBrain(newCallHandlerMockHub(), conn, &protocol.Packet{Cmd: protocol.CmdCallVoiceBrain, Seq: 1, Payload: payload})

	_, ok := conn.findCmd(protocol.CmdError)
	assert.True(t, ok, "missing voice brain should error")
	assert.Empty(t, bridge.started)
}

// 目标 agent 是语音大模型（应走 direct_ai）：被拒，不起 bridge
func TestHandleCallVoiceBrain_TargetIsVoiceAgent(t *testing.T) {
	bridge, cleanup := setupVoiceBrainTest(t)
	defer cleanup()
	ensureTextBrainAgent = func(_, _ int64) error { return errStr("agent is a voice model") }

	conn := &callHandlerMockConn{userID: 7}
	payload, _ := json.Marshal(protocol.CallVoiceBrainPayload{AgentID: "42"})
	HandleCallVoiceBrain(newCallHandlerMockHub(), conn, &protocol.Packet{Cmd: protocol.CmdCallVoiceBrain, Seq: 1, Payload: payload})

	_, ok := conn.findCmd(protocol.CmdError)
	assert.True(t, ok)
	assert.Empty(t, bridge.started)
}

// 语音大脑非 owner 所有：被拒，不起 bridge
func TestHandleCallVoiceBrain_BrainNotOwned(t *testing.T) {
	bridge, cleanup := setupVoiceBrainTest(t)
	defer cleanup()
	ensureVoiceAgentOwner = func(_, _ int64) error { return errStr("denied") }

	conn := &callHandlerMockConn{userID: 7}
	payload, _ := json.Marshal(protocol.CallVoiceBrainPayload{AgentID: "42"})
	HandleCallVoiceBrain(newCallHandlerMockHub(), conn, &protocol.Packet{Cmd: protocol.CmdCallVoiceBrain, Seq: 1, Payload: payload})

	_, ok := conn.findCmd(protocol.CmdError)
	assert.True(t, ok)
	assert.Empty(t, bridge.started)
}
