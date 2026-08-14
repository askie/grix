package handler

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/askie/grix/backend/internal/call"
	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/snowflake"
	"github.com/askie/grix/backend/internal/store"
	"github.com/askie/grix/backend/internal/ws/protocol"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- mock room + persist for handler tests ---

type callHandlerMockRoom struct {
	mu      sync.Mutex
	created []int64
	closed  []int64
}

func (m *callHandlerMockRoom) CreateRoom(_ context.Context, callID int64, _, _ int64) (string, string, string, error) {
	m.mu.Lock()
	m.created = append(m.created, callID)
	m.mu.Unlock()
	return "tok-caller", "tok-callee", "wss://lk.test", nil
}

func (m *callHandlerMockRoom) CloseRoom(_ context.Context, callID int64) error {
	m.mu.Lock()
	m.closed = append(m.closed, callID)
	m.mu.Unlock()
	return nil
}

type callHandlerMockPersist struct{}

func (m *callHandlerMockPersist) Create(_ context.Context, _ *model.CallRecord) error { return nil }
func (m *callHandlerMockPersist) UpdateAnswered(_ context.Context, _ int64, _ time.Time) error {
	return nil
}
func (m *callHandlerMockPersist) UpdateAnsweredWithAI(_ context.Context, _, _ int64, _ time.Time) error {
	return nil
}
func (m *callHandlerMockPersist) UpdateEnd(_ context.Context, _ int64, _ int16, _ string, _ time.Time, _ *int) error {
	return nil
}
func (m *callHandlerMockPersist) UpdateHandover(_ context.Context, _ int64, _ model.CallHandoverEvent, _ int16, _ string) error {
	return nil
}
func (m *callHandlerMockPersist) UpdateRecordingURLs(_ context.Context, _ int64, _, _, _, _ string) error {
	return nil
}

// --- mock hub for handler tests ---

type callHandlerMockHub struct {
	mu     sync.Mutex
	nodeID string
	conns  map[int64][]ConnInterface
}

func newCallHandlerMockHub() *callHandlerMockHub {
	return &callHandlerMockHub{nodeID: "test-node", conns: make(map[int64][]ConnInterface)}
}

func (h *callHandlerMockHub) Register(c ConnInterface)     {}
func (h *callHandlerMockHub) Unregister(c ConnInterface)   {}
func (h *callHandlerMockHub) RefreshAlive(c ConnInterface) {}
func (h *callHandlerMockHub) GetNodeID() string            { return h.nodeID }
func (h *callHandlerMockHub) GetUserConns(userID int64) []ConnInterface {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.conns[userID]
}
func (h *callHandlerMockHub) addConn(userID int64, c ConnInterface) {
	h.mu.Lock()
	h.conns[userID] = append(h.conns[userID], c)
	h.mu.Unlock()
}

// --- mock conn ---

type callHandlerMockConn struct {
	userID   int64
	deviceID string
	mu       sync.Mutex
	sent     []sentPayload
	seq      int64
}

func (c *callHandlerMockConn) GetUserID() int64    { return c.userID }
func (c *callHandlerMockConn) GetPlatform() string { return "" }
func (c *callHandlerMockConn) GetDeviceID() string {
	if c.deviceID != "" {
		return c.deviceID
	}
	return "dev-1"
}
func (c *callHandlerMockConn) NextSeq() int64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.seq++
	return c.seq
}
func (c *callHandlerMockConn) SendPayload(cmd string, seq int64, payload interface{}) {
	c.mu.Lock()
	c.sent = append(c.sent, sentPayload{cmd: cmd, seq: seq, payload: payload})
	c.mu.Unlock()
}
func (c *callHandlerMockConn) SendPacket(pkt *protocol.Packet) {
	c.mu.Lock()
	c.sent = append(c.sent, sentPayload{cmd: pkt.Cmd, seq: pkt.Seq, payload: pkt.Payload})
	c.mu.Unlock()
}
func (c *callHandlerMockConn) AckPush(_ int64)                 {}
func (c *callHandlerMockConn) Close()                          {}
func (c *callHandlerMockConn) SetAuth(_ int64, _, _, _ string) {}
func (c *callHandlerMockConn) IsAuthed() bool                  { return true }

func (c *callHandlerMockConn) findCmd(cmd string) (sentPayload, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, s := range c.sent {
		if s.cmd == cmd {
			return s, true
		}
	}
	return sentPayload{}, false
}

// --- setup ---

func setupCallHandlerTest(t *testing.T) func() {
	t.Helper()
	_ = snowflake.Init(1)
	clearClosedRedisForCallTests()
	room := &callHandlerMockRoom{}
	persist := &callHandlerMockPersist{}
	var notified []notifyRecord
	var mu sync.Mutex
	notify := func(userID int64, cmd string, payload any) {
		mu.Lock()
		notified = append(notified, notifyRecord{userID, cmd, payload})
		mu.Unlock()
	}
	callCtrl = call.New(room, persist, notify)
	callCtrl.SetCleanupHook(cleanupCallGuards)

	// 测试中跳过好友关系验证
	origValidate := validateCallPermission
	validateCallPermission = func(_, _ int64) error { return nil }
	origResolve := resolveCallSession
	resolveCallSession = func(_, calleeID int64) (string, error) { return "sess-call", nil }

	return func() {
		callCtrl = nil
		validateCallPermission = origValidate
		resolveCallSession = origResolve
	}
}

func clearClosedRedisForCallTests() {
	if store.RDB == nil {
		return
	}
	if err := store.RDB.Ping(context.Background()).Err(); err != nil && strings.Contains(err.Error(), "client is closed") {
		store.RDB = nil
	}
}

type notifyRecord struct {
	userID  int64
	cmd     string
	payload any
}

// --- tests ---

func TestHandleCallInvite_CalleeOnline(t *testing.T) {
	cleanup := setupCallHandlerTest(t)
	defer cleanup()

	hub := newCallHandlerMockHub()
	callerConn := &callHandlerMockConn{userID: 1}
	calleeConn := &callHandlerMockConn{userID: 2}
	hub.addConn(2, calleeConn)

	payload, _ := json.Marshal(protocol.CallInvitePayload{PeerID: "2", PeerType: "user", CallMode: 1})
	pkt := &protocol.Packet{Cmd: protocol.CmdCallInvite, Seq: 1, Payload: payload}

	HandleCallInvite(hub, callerConn, pkt)

	// caller 收到 invite_ack（含 call_id + room token）
	ack, ok := callerConn.findCmd(protocol.CmdCallInviteAck)
	require.True(t, ok, "caller should receive invite_ack")
	_ = ack

	// callee 收到 ring
	ring, ok := calleeConn.findCmd(protocol.CmdCallRing)
	require.True(t, ok, "callee should receive ring")
	_ = ring
}

func TestHandleCallInvite_CalleeOffline(t *testing.T) {
	cleanup := setupCallHandlerTest(t)
	defer cleanup()

	var enqueuedCallee int64
	// 替换 enqueueVoIPPushTask 为 mock
	orig := enqueueVoIPPushTask
	enqueueVoIPPushTask = func(calleeID, callID, callerID int64, callerName string) {
		enqueuedCallee = calleeID
	}
	defer func() { enqueueVoIPPushTask = orig }()

	hub := newCallHandlerMockHub() // callee 不在 hub 中
	callerConn := &callHandlerMockConn{userID: 1}

	payload, _ := json.Marshal(protocol.CallInvitePayload{PeerID: "2", PeerType: "user", CallMode: 1})
	pkt := &protocol.Packet{Cmd: protocol.CmdCallInvite, Seq: 1, Payload: payload}

	HandleCallInvite(hub, callerConn, pkt)

	// callee 离线时，caller 仍收到 invite_ack；但因 iOS/Android 合规禁用语音通话，不下发来电推送
	_, ok := callerConn.findCmd(protocol.CmdCallInviteAck)
	assert.True(t, ok, "caller should receive invite_ack when callee offline")
	assert.Equal(t, int64(0), enqueuedCallee, "offline call push must NOT be enqueued (compliance)")
}

func TestHandleCallAnswer(t *testing.T) {
	cleanup := setupCallHandlerTest(t)
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

	// callee 接听
	answerPayload, _ := json.Marshal(protocol.CallAnswerPayload{CallID: ringPayload.CallID})
	HandleCallAnswer(hub, calleeConn, &protocol.Packet{Cmd: protocol.CmdCallAnswer, Seq: 2, Payload: answerPayload})

	// callee 收到 peer_answered（含 room token）
	answered, ok := calleeConn.findCmd(protocol.CmdCallPeerAnswered)
	require.True(t, ok, "callee should receive peer_answered with room token")
	b, _ = json.Marshal(answered.payload)
	var answeredPayload protocol.CallPeerAnsweredPayload
	require.NoError(t, json.Unmarshal(b, &answeredPayload))
	assert.Equal(t, "tok-callee", answeredPayload.RoomToken)
}

func TestHandleCallAnswerNotifiesOtherCalleeDevices(t *testing.T) {
	cleanup := setupCallHandlerTest(t)
	defer cleanup()

	hub := newCallHandlerMockHub()
	callerConn := &callHandlerMockConn{userID: 1, deviceID: "caller-dev"}
	calleeFastConn := &callHandlerMockConn{userID: 2, deviceID: "callee-fast"}
	calleeOtherConn := &callHandlerMockConn{userID: 2, deviceID: "callee-other"}
	hub.addConn(2, calleeFastConn)
	hub.addConn(2, calleeOtherConn)

	invitePayload, _ := json.Marshal(protocol.CallInvitePayload{PeerID: "2", PeerType: "user", CallMode: 1})
	HandleCallInvite(hub, callerConn, &protocol.Packet{Cmd: protocol.CmdCallInvite, Seq: 1, Payload: invitePayload})

	ring, ok := calleeFastConn.findCmd(protocol.CmdCallRing)
	require.True(t, ok)
	var ringPayload protocol.CallRingPayload
	b, _ := json.Marshal(ring.payload)
	require.NoError(t, json.Unmarshal(b, &ringPayload))

	answerPayload, _ := json.Marshal(protocol.CallAnswerPayload{CallID: ringPayload.CallID})
	HandleCallAnswer(hub, calleeFastConn, &protocol.Packet{Cmd: protocol.CmdCallAnswer, Seq: 2, Payload: answerPayload})

	_, ok = calleeFastConn.findCmd(protocol.CmdCallPeerAnswered)
	require.True(t, ok, "answering device should receive room token ack")

	state, ok := calleeOtherConn.findCmd(protocol.CmdCallState)
	require.True(t, ok, "other callee device should be told to leave ringing state")
	b, _ = json.Marshal(state.payload)
	var statePayload protocol.CallStatePayload
	require.NoError(t, json.Unmarshal(b, &statePayload))
	assert.Equal(t, protocol.CallStateActive, statePayload.State)
	assert.Equal(t, callStateReasonAnsweredElsewhere, statePayload.Reason)
	assert.Equal(t, "callee-fast", statePayload.AnsweredDeviceID)
}

func TestHandleCallReject(t *testing.T) {
	cleanup := setupCallHandlerTest(t)
	defer cleanup()

	hub := newCallHandlerMockHub()
	callerConn := &callHandlerMockConn{userID: 1}
	calleeConn := &callHandlerMockConn{userID: 2}
	hub.addConn(2, calleeConn)

	invitePayload, _ := json.Marshal(protocol.CallInvitePayload{PeerID: "2", PeerType: "user", CallMode: 1})
	HandleCallInvite(hub, callerConn, &protocol.Packet{Cmd: protocol.CmdCallInvite, Seq: 1, Payload: invitePayload})

	ring, ok := calleeConn.findCmd(protocol.CmdCallRing)
	require.True(t, ok)
	var ringPayload protocol.CallRingPayload
	b, _ := json.Marshal(ring.payload)
	require.NoError(t, json.Unmarshal(b, &ringPayload))

	rejectPayload, _ := json.Marshal(protocol.CallRejectPayload{CallID: ringPayload.CallID, Reason: "busy"})
	HandleCallReject(hub, calleeConn, &protocol.Packet{Cmd: protocol.CmdCallReject, Seq: 3, Payload: rejectPayload})
	// 不 panic，双方通过 notify 收到 call:state
}

func TestHandleCallHangup(t *testing.T) {
	cleanup := setupCallHandlerTest(t)
	defer cleanup()

	hub := newCallHandlerMockHub()
	callerConn := &callHandlerMockConn{userID: 1}
	calleeConn := &callHandlerMockConn{userID: 2}
	hub.addConn(2, calleeConn)

	invitePayload, _ := json.Marshal(protocol.CallInvitePayload{PeerID: "2", PeerType: "user", CallMode: 1})
	HandleCallInvite(hub, callerConn, &protocol.Packet{Cmd: protocol.CmdCallInvite, Seq: 1, Payload: invitePayload})

	ring, ok := calleeConn.findCmd(protocol.CmdCallRing)
	require.True(t, ok)
	var ringPayload protocol.CallRingPayload
	b, _ := json.Marshal(ring.payload)
	require.NoError(t, json.Unmarshal(b, &ringPayload))

	hangupPayload, _ := json.Marshal(protocol.CallHangupPayload{CallID: ringPayload.CallID})
	HandleCallHangup(hub, callerConn, &protocol.Packet{Cmd: protocol.CmdCallHangup, Seq: 4, Payload: hangupPayload})
	// 不 panic，双方通过 notify 收到 call:state
}

// --- 错误路径测试 ---

func TestHandleCallInvite_InvalidPayload(t *testing.T) {
	cleanup := setupCallHandlerTest(t)
	defer cleanup()

	conn := &callHandlerMockConn{userID: 1}
	pkt := &protocol.Packet{Cmd: protocol.CmdCallInvite, Seq: 1, Payload: []byte("not-json")}
	HandleCallInvite(newCallHandlerMockHub(), conn, pkt)

	_, ok := conn.findCmd(protocol.CmdError)
	assert.True(t, ok, "invalid payload should return error")
}

func TestHandleCallInvite_InvalidPeerID(t *testing.T) {
	cleanup := setupCallHandlerTest(t)
	defer cleanup()

	conn := &callHandlerMockConn{userID: 1}
	payload, _ := json.Marshal(protocol.CallInvitePayload{PeerID: "not-a-number", PeerType: "user", CallMode: 1})
	pkt := &protocol.Packet{Cmd: protocol.CmdCallInvite, Seq: 1, Payload: payload}
	HandleCallInvite(newCallHandlerMockHub(), conn, pkt)

	_, ok := conn.findCmd(protocol.CmdError)
	assert.True(t, ok, "invalid peer_id should return error")
}

func TestHandleCallAnswer_InvalidPayload(t *testing.T) {
	cleanup := setupCallHandlerTest(t)
	defer cleanup()

	conn := &callHandlerMockConn{userID: 2}
	pkt := &protocol.Packet{Cmd: protocol.CmdCallAnswer, Seq: 1, Payload: []byte("bad")}
	HandleCallAnswer(newCallHandlerMockHub(), conn, pkt)

	_, ok := conn.findCmd(protocol.CmdError)
	assert.True(t, ok)
}

func TestHandleCallAnswer_InvalidCallID(t *testing.T) {
	cleanup := setupCallHandlerTest(t)
	defer cleanup()

	conn := &callHandlerMockConn{userID: 2}
	payload, _ := json.Marshal(protocol.CallAnswerPayload{CallID: "not-a-number"})
	pkt := &protocol.Packet{Cmd: protocol.CmdCallAnswer, Seq: 1, Payload: payload}
	HandleCallAnswer(newCallHandlerMockHub(), conn, pkt)

	_, ok := conn.findCmd(protocol.CmdError)
	assert.True(t, ok)
}

func TestHandleCallAnswer_CallNotFound(t *testing.T) {
	cleanup := setupCallHandlerTest(t)
	defer cleanup()

	conn := &callHandlerMockConn{userID: 2}
	payload, _ := json.Marshal(protocol.CallAnswerPayload{CallID: "9999999999"})
	pkt := &protocol.Packet{Cmd: protocol.CmdCallAnswer, Seq: 1, Payload: payload}
	HandleCallAnswer(newCallHandlerMockHub(), conn, pkt)

	_, ok := conn.findCmd(protocol.CmdError)
	assert.True(t, ok, "answering non-existent call should return error")
}

func TestHandleCallHangup_InvalidPayload(t *testing.T) {
	cleanup := setupCallHandlerTest(t)
	defer cleanup()

	conn := &callHandlerMockConn{userID: 1}
	pkt := &protocol.Packet{Cmd: protocol.CmdCallHangup, Seq: 1, Payload: []byte("bad")}
	HandleCallHangup(newCallHandlerMockHub(), conn, pkt)
	// 不 panic，错误被 log
}

func TestHandleCallReject_InvalidCallID(t *testing.T) {
	cleanup := setupCallHandlerTest(t)
	defer cleanup()

	conn := &callHandlerMockConn{userID: 2}
	payload, _ := json.Marshal(protocol.CallRejectPayload{CallID: "not-a-number", Reason: "busy"})
	pkt := &protocol.Packet{Cmd: protocol.CmdCallReject, Seq: 1, Payload: payload}
	HandleCallReject(newCallHandlerMockHub(), conn, pkt)

	_, ok := conn.findCmd(protocol.CmdError)
	assert.True(t, ok, "invalid call_id should return error")
}

func TestHandleCallHangup_InvalidCallID(t *testing.T) {
	cleanup := setupCallHandlerTest(t)
	defer cleanup()

	conn := &callHandlerMockConn{userID: 1}
	payload, _ := json.Marshal(protocol.CallHangupPayload{CallID: "not-a-number"})
	pkt := &protocol.Packet{Cmd: protocol.CmdCallHangup, Seq: 1, Payload: payload}
	HandleCallHangup(newCallHandlerMockHub(), conn, pkt)

	_, ok := conn.findCmd(protocol.CmdError)
	assert.True(t, ok, "invalid call_id should return error")
}

func TestHandleCallInvite_NilController(t *testing.T) {
	// callCtrl 未注入时不应 panic
	callCtrl = nil
	defer func() { callCtrl = nil }()

	conn := &callHandlerMockConn{userID: 1}
	payload, _ := json.Marshal(protocol.CallInvitePayload{PeerID: "2", PeerType: "user", CallMode: 1})
	pkt := &protocol.Packet{Cmd: protocol.CmdCallInvite, Seq: 1, Payload: payload}
	HandleCallInvite(newCallHandlerMockHub(), conn, pkt)

	_, ok := conn.findCmd(protocol.CmdError)
	assert.True(t, ok, "nil controller should return error, not panic")
}

func TestHandleCallInvite_SelfCall(t *testing.T) {
	cleanup := setupCallHandlerTest(t)
	defer cleanup()

	conn := &callHandlerMockConn{userID: 1}
	// peer_id == caller userID → 自呼叫
	payload, _ := json.Marshal(protocol.CallInvitePayload{PeerID: "1", PeerType: "user", CallMode: 1})
	pkt := &protocol.Packet{Cmd: protocol.CmdCallInvite, Seq: 1, Payload: payload}
	HandleCallInvite(newCallHandlerMockHub(), conn, pkt)

	_, ok := conn.findCmd(protocol.CmdError)
	assert.True(t, ok, "self-call should return error")
}

func TestHandleCallInvite_NotFriend(t *testing.T) {
	cleanup := setupCallHandlerTest(t)
	defer cleanup()

	// 覆盖 validateCallPermission 返回好友验证失败
	validateCallPermission = func(_, _ int64) error {
		return errors.New("can only call friends")
	}

	conn := &callHandlerMockConn{userID: 1}
	payload, _ := json.Marshal(protocol.CallInvitePayload{PeerID: "2", PeerType: "user", CallMode: 1})
	pkt := &protocol.Packet{Cmd: protocol.CmdCallInvite, Seq: 1, Payload: payload}
	HandleCallInvite(newCallHandlerMockHub(), conn, pkt)

	_, ok := conn.findCmd(protocol.CmdError)
	assert.True(t, ok, "non-friend call should return error")
}

func TestHandleCallInvite_CalleeBusy(t *testing.T) {
	cleanup := setupCallHandlerTest(t)
	defer cleanup()

	hub := newCallHandlerMockHub()
	callerConn1 := &callHandlerMockConn{userID: 1}
	callerConn2 := &callHandlerMockConn{userID: 3}
	calleeConn := &callHandlerMockConn{userID: 2}
	hub.addConn(2, calleeConn)

	// 第一次 invite 成功
	p1, _ := json.Marshal(protocol.CallInvitePayload{PeerID: "2", PeerType: "user", CallMode: 1})
	HandleCallInvite(hub, callerConn1, &protocol.Packet{Cmd: protocol.CmdCallInvite, Seq: 1, Payload: p1})

	// 第二次 invite 同一 callee → busy
	p2, _ := json.Marshal(protocol.CallInvitePayload{PeerID: "2", PeerType: "user", CallMode: 1})
	HandleCallInvite(hub, callerConn2, &protocol.Packet{Cmd: protocol.CmdCallInvite, Seq: 2, Payload: p2})

	_, ok := callerConn2.findCmd(protocol.CmdCallBusy)
	assert.True(t, ok, "second caller should receive busy")
}
