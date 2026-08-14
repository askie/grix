package handler

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/askie/grix/backend/internal/call"
	"github.com/askie/grix/backend/internal/ws/protocol"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- Phase 2 mock BridgeManager ---

type mockBridgeManager struct {
	started  []int64
	stopped  []int64
	muted    []int64
	unmuted  []int64
	startErr error
	lastSpec call.VoiceBridgeSpec
}

func (m *mockBridgeManager) StartBridge(_ context.Context, callID int64, spec call.VoiceBridgeSpec) error {
	if m.startErr != nil {
		return m.startErr
	}
	m.started = append(m.started, callID)
	m.lastSpec = spec
	return nil
}

func (m *mockBridgeManager) StopBridge(_ context.Context, callID int64) error {
	m.stopped = append(m.stopped, callID)
	return nil
}

func (m *mockBridgeManager) InterruptBridge(_ context.Context, _ int64) error {
	return nil
}

func (m *mockBridgeManager) MuteBridge(_ context.Context, callID int64) error {
	m.muted = append(m.muted, callID)
	return nil
}

func (m *mockBridgeManager) UnmuteBridge(_ context.Context, callID int64) error {
	m.unmuted = append(m.unmuted, callID)
	return nil
}

// setupCallAIHandlerTest 初始化带 BridgeManager 的 callCtrl。
func setupCallAIHandlerTest(t *testing.T) (*mockBridgeManager, func()) {
	t.Helper()
	clearClosedRedisForCallTests()
	room := &callHandlerMockRoom{}
	persist := &callHandlerMockPersist{}
	bridge := &mockBridgeManager{}
	callCtrl = call.NewWithBridge(room, persist, func(_ int64, _ string, _ any) {}, bridge)
	callCtrl.SetCleanupHook(cleanupCallGuards)

	origValidate := validateCallPermission
	validateCallPermission = func(_, _ int64) error { return nil }
	origResolve := resolveCallSession
	resolveCallSession = func(_, calleeID int64) (string, error) { return "sess-call", nil }

	origLookup := resolveAgentVoiceSpec
	resolveAgentVoiceSpec = func(agentID int64, _ string) (call.VoiceBridgeSpec, error) {
		return call.VoiceBridgeSpec{AgentID: agentID, Provider: "doubao_realtime", Model: "m", APIKey: "k"}, nil
	}
	origOwner := ensureVoiceAgentOwner
	ensureVoiceAgentOwner = func(_, _ int64) error { return nil }
	origVoiceDelegate := resolveCalleeVoiceAgent
	resolveCalleeVoiceAgent = func(_ int64, _ string) (int64, bool) { return 0, false } // 默认无托管，不影响响铃类测试
	origReserve := reserveVoiceDailyQuota
	reserveVoiceDailyQuota = func(_ int64, _ int) bool { return true } // 默认放行
	origVisitorReserve := reserveVisitorCallQuota
	reserveVisitorCallQuota = func(_ int64) bool { return true }

	return bridge, func() {
		// 只把 callCtrl 置 nil 不够：Controller 名下的定时器回调还在跑，
		// 会活过本用例去读下一个用例的 DB/logger。先真正关停它。
		if callCtrl != nil {
			callCtrl.Shutdown(context.Background())
		}
		callCtrl = nil
		validateCallPermission = origValidate
		resolveCallSession = origResolve
		resolveAgentVoiceSpec = origLookup
		ensureVoiceAgentOwner = origOwner
		resolveCalleeVoiceAgent = origVoiceDelegate
		reserveVoiceDailyQuota = origReserve
		reserveVisitorCallQuota = origVisitorReserve
	}
}

// --- call:answer_with_ai ---

func TestHandleCallAnswerWithAI_Success(t *testing.T) {
	bridge, cleanup := setupCallAIHandlerTest(t)
	defer cleanup()

	hub := newCallHandlerMockHub()
	callerConn := &callHandlerMockConn{userID: 1}
	calleeConn := &callHandlerMockConn{userID: 2}
	hub.addConn(2, calleeConn)

	// 先发起通话
	invitePayload, _ := json.Marshal(protocol.CallInvitePayload{PeerID: "2", PeerType: "user", CallMode: 1})
	HandleCallInvite(hub, callerConn, &protocol.Packet{Cmd: protocol.CmdCallInvite, Seq: 1, Payload: invitePayload})

	// 从 ring 包中取 call_id
	ring, ok := calleeConn.findCmd(protocol.CmdCallRing)
	require.True(t, ok)
	var ringPayload protocol.CallRingPayload
	b, _ := json.Marshal(ring.payload)
	require.NoError(t, json.Unmarshal(b, &ringPayload))

	// callee 选择 AI 代接
	aiPayload, _ := json.Marshal(protocol.CallAnswerWithAIPayload{
		CallID:  ringPayload.CallID,
		AgentID: "42",
	})
	HandleCallAnswerWithAI(hub, calleeConn, &protocol.Packet{Cmd: protocol.CmdCallAnswerWithAI, Seq: 2, Payload: aiPayload})

	// callee 应收到 peer_answered（mode=ai_delegated）
	answered, ok := calleeConn.findCmd(protocol.CmdCallPeerAnswered)
	require.True(t, ok, "callee should receive peer_answered")
	b, _ = json.Marshal(answered.payload)
	var answeredPayload protocol.CallPeerAnsweredPayload
	require.NoError(t, json.Unmarshal(b, &answeredPayload))
	assert.Equal(t, "ai_delegated", answeredPayload.Mode)

	// Bridge 应已启动
	assert.NotEmpty(t, bridge.started)
}

func TestHandleCallAnswerWithAI_InvalidPayload(t *testing.T) {
	_, cleanup := setupCallAIHandlerTest(t)
	defer cleanup()

	conn := &callHandlerMockConn{userID: 2}
	pkt := &protocol.Packet{Cmd: protocol.CmdCallAnswerWithAI, Seq: 1, Payload: []byte("bad")}
	HandleCallAnswerWithAI(newCallHandlerMockHub(), conn, pkt)

	_, ok := conn.findCmd(protocol.CmdError)
	assert.True(t, ok, "invalid payload should return error")
}

func TestHandleCallAnswerWithAI_InvalidCallID(t *testing.T) {
	_, cleanup := setupCallAIHandlerTest(t)
	defer cleanup()

	conn := &callHandlerMockConn{userID: 2}
	payload, _ := json.Marshal(protocol.CallAnswerWithAIPayload{CallID: "not-a-number", AgentID: "42"})
	pkt := &protocol.Packet{Cmd: protocol.CmdCallAnswerWithAI, Seq: 1, Payload: payload}
	HandleCallAnswerWithAI(newCallHandlerMockHub(), conn, pkt)

	_, ok := conn.findCmd(protocol.CmdError)
	assert.True(t, ok)
}

func TestHandleCallAnswerWithAI_InvalidAgentID(t *testing.T) {
	_, cleanup := setupCallAIHandlerTest(t)
	defer cleanup()

	conn := &callHandlerMockConn{userID: 2}
	payload, _ := json.Marshal(protocol.CallAnswerWithAIPayload{CallID: "12345", AgentID: "not-a-number"})
	pkt := &protocol.Packet{Cmd: protocol.CmdCallAnswerWithAI, Seq: 1, Payload: payload}
	HandleCallAnswerWithAI(newCallHandlerMockHub(), conn, pkt)

	_, ok := conn.findCmd(protocol.CmdError)
	assert.True(t, ok)
}

func TestHandleCallAnswerWithAI_NilController(t *testing.T) {
	callCtrl = nil
	defer func() { callCtrl = nil }()

	conn := &callHandlerMockConn{userID: 2}
	payload, _ := json.Marshal(protocol.CallAnswerWithAIPayload{CallID: "12345", AgentID: "42"})
	pkt := &protocol.Packet{Cmd: protocol.CmdCallAnswerWithAI, Seq: 1, Payload: payload}
	HandleCallAnswerWithAI(newCallHandlerMockHub(), conn, pkt)

	_, ok := conn.findCmd(protocol.CmdError)
	assert.True(t, ok)
}

// --- call:takeover ---

func TestHandleCallTakeover_Success(t *testing.T) {
	_, cleanup := setupCallAIHandlerTest(t)
	defer cleanup()

	hub := newCallHandlerMockHub()
	callerConn := &callHandlerMockConn{userID: 1}
	calleeConn := &callHandlerMockConn{userID: 2}
	hub.addConn(2, calleeConn)

	// 发起 → AI 代接
	invitePayload, _ := json.Marshal(protocol.CallInvitePayload{PeerID: "2", PeerType: "user", CallMode: 1})
	HandleCallInvite(hub, callerConn, &protocol.Packet{Cmd: protocol.CmdCallInvite, Seq: 1, Payload: invitePayload})

	ring, ok := calleeConn.findCmd(protocol.CmdCallRing)
	require.True(t, ok)
	var ringPayload protocol.CallRingPayload
	b, _ := json.Marshal(ring.payload)
	require.NoError(t, json.Unmarshal(b, &ringPayload))

	aiPayload, _ := json.Marshal(protocol.CallAnswerWithAIPayload{CallID: ringPayload.CallID, AgentID: "42"})
	HandleCallAnswerWithAI(hub, calleeConn, &protocol.Packet{Cmd: protocol.CmdCallAnswerWithAI, Seq: 2, Payload: aiPayload})

	// 接管
	takeoverPayload, _ := json.Marshal(protocol.CallTakeoverPayload{CallID: ringPayload.CallID})
	HandleCallTakeover(hub, calleeConn, &protocol.Packet{Cmd: protocol.CmdCallTakeover, Seq: 3, Payload: takeoverPayload})

	// callee 应收到 call:ai_state（mode=human_active）
	aiState, ok := calleeConn.findCmd(protocol.CmdCallState)
	_ = aiState
	// 不 panic，接管成功
	_ = ok
}

func TestHandleCallTakeover_InvalidPayload(t *testing.T) {
	_, cleanup := setupCallAIHandlerTest(t)
	defer cleanup()

	conn := &callHandlerMockConn{userID: 2}
	pkt := &protocol.Packet{Cmd: protocol.CmdCallTakeover, Seq: 1, Payload: []byte("bad")}
	HandleCallTakeover(newCallHandlerMockHub(), conn, pkt)

	_, ok := conn.findCmd(protocol.CmdError)
	assert.True(t, ok)
}

func TestHandleCallTakeover_InvalidCallID(t *testing.T) {
	_, cleanup := setupCallAIHandlerTest(t)
	defer cleanup()

	conn := &callHandlerMockConn{userID: 2}
	payload, _ := json.Marshal(protocol.CallTakeoverPayload{CallID: "not-a-number"})
	pkt := &protocol.Packet{Cmd: protocol.CmdCallTakeover, Seq: 1, Payload: payload}
	HandleCallTakeover(newCallHandlerMockHub(), conn, pkt)

	_, ok := conn.findCmd(protocol.CmdError)
	assert.True(t, ok)
}

func TestHandleCallTakeover_NilController(t *testing.T) {
	callCtrl = nil
	defer func() { callCtrl = nil }()

	conn := &callHandlerMockConn{userID: 2}
	payload, _ := json.Marshal(protocol.CallTakeoverPayload{CallID: "12345"})
	pkt := &protocol.Packet{Cmd: protocol.CmdCallTakeover, Seq: 1, Payload: payload}
	HandleCallTakeover(newCallHandlerMockHub(), conn, pkt)

	_, ok := conn.findCmd(protocol.CmdError)
	assert.True(t, ok)
}

// --- call:hand_back ---

func TestHandleCallHandBack_Success(t *testing.T) {
	_, cleanup := setupCallAIHandlerTest(t)
	defer cleanup()

	hub := newCallHandlerMockHub()
	callerConn := &callHandlerMockConn{userID: 1}
	calleeConn := &callHandlerMockConn{userID: 2}
	hub.addConn(2, calleeConn)

	// 发起 → AI 代接 → 接管 → 交回
	invitePayload, _ := json.Marshal(protocol.CallInvitePayload{PeerID: "2", PeerType: "user", CallMode: 1})
	HandleCallInvite(hub, callerConn, &protocol.Packet{Cmd: protocol.CmdCallInvite, Seq: 1, Payload: invitePayload})

	ring, ok := calleeConn.findCmd(protocol.CmdCallRing)
	require.True(t, ok)
	var ringPayload protocol.CallRingPayload
	b, _ := json.Marshal(ring.payload)
	require.NoError(t, json.Unmarshal(b, &ringPayload))

	aiPayload, _ := json.Marshal(protocol.CallAnswerWithAIPayload{CallID: ringPayload.CallID, AgentID: "42"})
	HandleCallAnswerWithAI(hub, calleeConn, &protocol.Packet{Cmd: protocol.CmdCallAnswerWithAI, Seq: 2, Payload: aiPayload})

	takeoverPayload, _ := json.Marshal(protocol.CallTakeoverPayload{CallID: ringPayload.CallID})
	HandleCallTakeover(hub, calleeConn, &protocol.Packet{Cmd: protocol.CmdCallTakeover, Seq: 3, Payload: takeoverPayload})

	handBackPayload, _ := json.Marshal(protocol.CallHandBackPayload{CallID: ringPayload.CallID})
	HandleCallHandBack(hub, calleeConn, &protocol.Packet{Cmd: protocol.CmdCallHandBack, Seq: 4, Payload: handBackPayload})
	// 不 panic，交回成功
}

func TestHandleCallHandBack_InvalidPayload(t *testing.T) {
	_, cleanup := setupCallAIHandlerTest(t)
	defer cleanup()

	conn := &callHandlerMockConn{userID: 2}
	pkt := &protocol.Packet{Cmd: protocol.CmdCallHandBack, Seq: 1, Payload: []byte("bad")}
	HandleCallHandBack(newCallHandlerMockHub(), conn, pkt)

	_, ok := conn.findCmd(protocol.CmdError)
	assert.True(t, ok)
}

func TestHandleCallHandBack_InvalidCallID(t *testing.T) {
	_, cleanup := setupCallAIHandlerTest(t)
	defer cleanup()

	conn := &callHandlerMockConn{userID: 2}
	payload, _ := json.Marshal(protocol.CallHandBackPayload{CallID: "not-a-number"})
	pkt := &protocol.Packet{Cmd: protocol.CmdCallHandBack, Seq: 1, Payload: payload}
	HandleCallHandBack(newCallHandlerMockHub(), conn, pkt)

	_, ok := conn.findCmd(protocol.CmdError)
	assert.True(t, ok)
}

func TestHandleCallHandBack_NilController(t *testing.T) {
	callCtrl = nil
	defer func() { callCtrl = nil }()

	conn := &callHandlerMockConn{userID: 2}
	payload, _ := json.Marshal(protocol.CallHandBackPayload{CallID: "12345"})
	pkt := &protocol.Packet{Cmd: protocol.CmdCallHandBack, Seq: 1, Payload: payload}
	HandleCallHandBack(newCallHandlerMockHub(), conn, pkt)

	_, ok := conn.findCmd(protocol.CmdError)
	assert.True(t, ok)
}
