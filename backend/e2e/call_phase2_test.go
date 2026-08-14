package e2e

import (
	"context"
	"sync"
	"testing"

	"github.com/askie/grix/backend/internal/call"
	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/snowflake"
	"github.com/askie/grix/backend/internal/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- mock BridgeManager ---

type e2eMockBridge struct {
	mu           sync.Mutex
	started      []int64
	stopped      []int64
	interrupted  []int64
	muted        []int64
	unmuted      []int64
	startErr     error
	interruptErr error
	muteErr      error
}

func (m *e2eMockBridge) StartBridge(_ context.Context, callID int64, _ call.VoiceBridgeSpec) error {
	if m.startErr != nil {
		return m.startErr
	}
	m.mu.Lock()
	m.started = append(m.started, callID)
	m.mu.Unlock()
	return nil
}

func (m *e2eMockBridge) StopBridge(_ context.Context, callID int64) error {
	m.mu.Lock()
	m.stopped = append(m.stopped, callID)
	m.mu.Unlock()
	return nil
}

func (m *e2eMockBridge) InterruptBridge(_ context.Context, callID int64) error {
	if m.interruptErr != nil {
		return m.interruptErr
	}
	m.mu.Lock()
	m.interrupted = append(m.interrupted, callID)
	m.mu.Unlock()
	return nil
}

func (m *e2eMockBridge) MuteBridge(_ context.Context, callID int64) error {
	if m.muteErr != nil {
		return m.muteErr
	}
	m.mu.Lock()
	m.muted = append(m.muted, callID)
	m.mu.Unlock()
	return nil
}

func (m *e2eMockBridge) UnmuteBridge(_ context.Context, callID int64) error {
	m.mu.Lock()
	m.unmuted = append(m.unmuted, callID)
	m.mu.Unlock()
	return nil
}

// --- Phase 2 e2e setup ---

type callPhase2E2ECtx struct {
	ctrl    *call.Controller
	room    *e2eMockRoom
	bridge  *e2eMockBridge
	events  []notifyEvent
	eventMu sync.Mutex
	db      interface{ Close() }
}

func setupCallPhase2E2E(t *testing.T) *callPhase2E2ECtx {
	t.Helper()
	_ = snowflake.Init(1)

	testDB := setupE2E(t)
	store.DB = testDB.db.DB

	room := &e2eMockRoom{}
	bridge := &e2eMockBridge{}
	persist := store.NewCallRecordStore(store.DB)

	ctx := &callPhase2E2ECtx{room: room, bridge: bridge, db: testDB.db}
	ctx.ctrl = call.NewWithBridge(room, persist, func(userID int64, cmd string, payload any) {
		ctx.eventMu.Lock()
		ctx.events = append(ctx.events, notifyEvent{userID, cmd, payload})
		ctx.eventMu.Unlock()
	}, bridge)
	return ctx
}

func (c *callPhase2E2ECtx) findEvents(cmd string) []notifyEvent {
	c.eventMu.Lock()
	defer c.eventMu.Unlock()
	var result []notifyEvent
	for _, e := range c.events {
		if e.cmd == cmd {
			result = append(result, e)
		}
	}
	return result
}

// --- Phase 2 tests ---

// TestCallPhase2_AnswerWithAI B 选择 AI 代接，验证状态机和 DB
func TestCallPhase2_AnswerWithAI(t *testing.T) {
	ctx := setupCallPhase2E2E(t)
	defer ctx.db.Close()
	bg := context.Background()

	callID, _, _, err := ctx.ctrl.Invite(bg, 1001, 1002, "sess-p2-1")
	require.NoError(t, err)

	// B 选择 AI 代接
	token, url, err := ctx.ctrl.AnswerWithAI(bg, callID, 1002, call.VoiceBridgeSpec{AgentID: 42, Provider: "doubao_realtime", Model: "m", APIKey: "k"})
	require.NoError(t, err)
	assert.NotEmpty(t, token)
	assert.NotEmpty(t, url)

	// Bridge 应已启动
	assert.Contains(t, ctx.bridge.started, callID)

	// caller 应收到 call:peer_answered 通知
	peerAnswered := ctx.findEvents("call:peer_answered")
	require.Len(t, peerAnswered, 1)
	assert.Equal(t, int64(1001), peerAnswered[0].userID)

	// DB 应写入 ai_delegated 状态
	var rec model.CallRecord
	require.NoError(t, store.DB.First(&rec, callID).Error)
	assert.Equal(t, model.CallStateAIDelegated, rec.State)
	assert.Equal(t, model.CallDelegationAIDelegated, rec.DelegationMode)
	require.NotNil(t, rec.DelegatedAgentID)
	assert.Equal(t, int64(42), *rec.DelegatedAgentID)
	assert.NotNil(t, rec.AnsweredAt)
}

// TestCallPhase2_Takeover B 接管，AI 静默
func TestCallPhase2_Takeover(t *testing.T) {
	ctx := setupCallPhase2E2E(t)
	defer ctx.db.Close()
	bg := context.Background()

	callID, _, _, err := ctx.ctrl.Invite(bg, 2001, 2002, "sess-p2-2")
	require.NoError(t, err)
	_, _, err = ctx.ctrl.AnswerWithAI(bg, callID, 2002, call.VoiceBridgeSpec{AgentID: 42, Provider: "doubao_realtime", Model: "m", APIKey: "k"})
	require.NoError(t, err)

	// B 接管
	err = ctx.ctrl.Takeover(bg, callID, 2002)
	require.NoError(t, err)

	// Bridge 应被静音（保留 session，不再 interrupt 销毁）
	assert.Contains(t, ctx.bridge.muted, callID)
	assert.NotContains(t, ctx.bridge.interrupted, callID)

	// caller 应收到 call:state(human_active) 通知
	stateEvents := ctx.findEvents("call:state")
	require.NotEmpty(t, stateEvents)
	found := false
	for _, e := range stateEvents {
		if e.userID == int64(2001) {
			found = true
		}
	}
	assert.True(t, found, "caller should receive call:state after takeover")
}

// TestCallPhase2_HandBack B 交回 AI
func TestCallPhase2_HandBack(t *testing.T) {
	ctx := setupCallPhase2E2E(t)
	defer ctx.db.Close()
	bg := context.Background()

	callID, _, _, err := ctx.ctrl.Invite(bg, 3001, 3002, "sess-p2-3")
	require.NoError(t, err)
	_, _, err = ctx.ctrl.AnswerWithAI(bg, callID, 3002, call.VoiceBridgeSpec{AgentID: 42, Provider: "doubao_realtime", Model: "m", APIKey: "k"})
	require.NoError(t, err)
	err = ctx.ctrl.Takeover(bg, callID, 3002)
	require.NoError(t, err)

	// B 交回 AI
	err = ctx.ctrl.HandBack(bg, callID, 3002)
	require.NoError(t, err)

	// 交回=恢复发声（unmute），不重建 session：started 仍 1，unmuted 含本通话
	assert.Equal(t, 1, len(ctx.bridge.started))
	assert.Contains(t, ctx.bridge.unmuted, callID)

	// caller 应收到 ai_delegated 通知
	stateEvents := ctx.findEvents("call:state")
	found := false
	for _, e := range stateEvents {
		if e.userID == int64(3001) {
			if p, ok := e.payload.(map[string]any); ok && p["mode"] == "ai_delegated" {
				found = true
			}
		}
	}
	assert.True(t, found, "caller should receive ai_delegated after hand_back")
}

// TestCallPhase2_FullCycle 完整 AI 托管生命周期：发起→AI代接→接管→交回→挂断
func TestCallPhase2_FullCycle(t *testing.T) {
	ctx := setupCallPhase2E2E(t)
	defer ctx.db.Close()
	bg := context.Background()

	// 发起
	callID, _, _, err := ctx.ctrl.Invite(bg, 4001, 4002, "sess-p2-4")
	require.NoError(t, err)

	// AI 代接
	_, _, err = ctx.ctrl.AnswerWithAI(bg, callID, 4002, call.VoiceBridgeSpec{AgentID: 42, Provider: "doubao_realtime", Model: "m", APIKey: "k"})
	require.NoError(t, err)

	// 接管
	err = ctx.ctrl.Takeover(bg, callID, 4002)
	require.NoError(t, err)

	// 交回
	err = ctx.ctrl.HandBack(bg, callID, 4002)
	require.NoError(t, err)

	// 挂断
	err = ctx.ctrl.Hangup(bg, callID, 4001)
	require.NoError(t, err)

	// Bridge 应已停止
	assert.Contains(t, ctx.bridge.stopped, callID)

	// DB 最终状态
	var rec model.CallRecord
	require.NoError(t, store.DB.First(&rec, callID).Error)
	assert.Equal(t, model.CallStateEnded, rec.State)
	assert.Equal(t, "hangup", rec.EndReason)
	assert.NotNil(t, rec.DurationSeconds)
}

// TestCallPhase2_BridgeStartFails Bridge 启动失败时状态回滚
func TestCallPhase2_BridgeStartFails(t *testing.T) {
	ctx := setupCallPhase2E2E(t)
	defer ctx.db.Close()
	bg := context.Background()

	ctx.bridge.startErr = assert.AnError

	callID, _, _, err := ctx.ctrl.Invite(bg, 5001, 5002, "sess-p2-5")
	require.NoError(t, err)

	// Bridge 失败，AnswerWithAI 应返回错误
	_, _, err = ctx.ctrl.AnswerWithAI(bg, callID, 5002, call.VoiceBridgeSpec{AgentID: 42, Provider: "doubao_realtime", Model: "m", APIKey: "k"})
	require.Error(t, err)

	// 状态应回滚为 RINGING（可以重试接听）
	ctx.bridge.startErr = nil
	_, _, err = ctx.ctrl.AnswerWithAI(bg, callID, 5002, call.VoiceBridgeSpec{AgentID: 42, Provider: "doubao_realtime", Model: "m", APIKey: "k"})
	require.NoError(t, err, "after rollback, AnswerWithAI should succeed")
}
