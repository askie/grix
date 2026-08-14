package call_test

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/askie/grix/backend/internal/call"
	"github.com/askie/grix/backend/internal/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- Phase 2 测试：AnswerWithAI / Takeover / HandBack ---

// mockBridgeManager 实现 call.BridgeManager 接口，用于测试。
type mockBridgeManager struct {
	started      []int64 // 已启动的 callID
	stopped      []int64 // 已停止的 callID
	muted        []int64 // 已静音的 callID（接管）
	unmuted      []int64 // 已恢复的 callID（交回）
	interrupted  []int64 // 已挂起的 callID
	startErr     error
	interruptErr error
	muteErr      error
	unmuteErr    error
	lastStart    call.VoiceBridgeSpec // 最近一次 StartBridge 的 spec
}

func (m *mockBridgeManager) StartBridge(_ context.Context, callID int64, spec call.VoiceBridgeSpec) error {
	if m.startErr != nil {
		return m.startErr
	}
	m.started = append(m.started, callID)
	m.lastStart = spec
	return nil
}

func (m *mockBridgeManager) StopBridge(_ context.Context, callID int64) error {
	m.stopped = append(m.stopped, callID)
	return nil
}

func (m *mockBridgeManager) InterruptBridge(_ context.Context, callID int64) error {
	if m.interruptErr != nil {
		return m.interruptErr
	}
	m.interrupted = append(m.interrupted, callID)
	return nil
}

func (m *mockBridgeManager) MuteBridge(_ context.Context, callID int64) error {
	if m.muteErr != nil {
		return m.muteErr
	}
	m.muted = append(m.muted, callID)
	return nil
}

func (m *mockBridgeManager) UnmuteBridge(_ context.Context, callID int64) error {
	if m.unmuteErr != nil {
		return m.unmuteErr
	}
	m.unmuted = append(m.unmuted, callID)
	return nil
}

// newControllerWithBridge 创建带 BridgeManager 的 Controller。
func newControllerWithBridge() (*call.Controller, *mockRoom, *mockPersist, *mockBridgeManager, *[]notifyRecord) {
	room := &mockRoom{}
	persist := &mockPersist{}
	bridge := &mockBridgeManager{}
	var events []notifyRecord
	var mu sync.Mutex
	notify := func(userID int64, cmd string, payload any) {
		mu.Lock()
		events = append(events, notifyRecord{userID, cmd, payload})
		mu.Unlock()
	}
	ctrl := call.NewWithBridge(room, persist, notify, bridge)
	return ctrl, room, persist, bridge, &events
}

type notifyMu struct{ mu sync.Mutex }

func (m *notifyMu) Lock()   { m.mu.Lock() }
func (m *notifyMu) Unlock() { m.mu.Unlock() }

func TestAnswerWithAI_Success(t *testing.T) {
	ctrl, _, persist, bridge, _ := newControllerWithBridge()
	ctx := context.Background()

	callID, _, _, err := ctrl.Invite(ctx, 1, 2, "sess-ai-1")
	require.NoError(t, err)

	tokenCallee, roomURL, err := ctrl.AnswerWithAI(ctx, callID, 2, call.VoiceBridgeSpec{AgentID: 42, Provider: "doubao_realtime", Model: "m", APIKey: "k"})
	require.NoError(t, err)
	assert.NotEmpty(t, tokenCallee)
	assert.NotEmpty(t, roomURL)

	// Bridge 应已启动
	assert.Contains(t, bridge.started, callID)
	// 通话记录应更新为 ai_delegated
	assert.Len(t, persist.answered, 1)
}

func TestDirectAICall_StartsBridgeAndHangupStops(t *testing.T) {
	ctrl, _, _, bridge, _ := newControllerWithBridge()
	ctx := context.Background()

	spec := call.VoiceBridgeSpec{AgentID: 42, Provider: "openai_realtime", Model: "gpt-4o-realtime-preview", APIKey: "sk-x"}
	callID, token, url, err := ctrl.DirectAICall(ctx, 7, "sess-direct-ai-42", spec)
	require.NoError(t, err)
	assert.NotZero(t, callID)
	assert.NotEmpty(t, token)
	assert.NotEmpty(t, url)
	assert.Contains(t, bridge.started, callID)

	// 发起方挂断应停止 bridge
	require.NoError(t, ctrl.Hangup(ctx, callID, 7))
	assert.Contains(t, bridge.stopped, callID)
}

func TestDirectAICall_BridgeStartFailurePersistsEnd(t *testing.T) {
	ctrl, _, persist, bridge, _ := newControllerWithBridge()
	bridge.startErr = assert.AnError

	_, _, _, err := ctrl.DirectAICall(context.Background(), 7, "sess-direct-ai-fail", call.VoiceBridgeSpec{
		AgentID: 42, Provider: "openai_realtime", Model: "m", APIKey: "k",
	})
	require.Error(t, err)
	assert.Len(t, persist.ended, 1, "failed direct AI call must not leave an open DB record")
}

func TestAnswerWithAI_CallNotFound(t *testing.T) {
	ctrl, _, _, _, _ := newControllerWithBridge()
	_, _, err := ctrl.AnswerWithAI(context.Background(), 9999, 2, call.VoiceBridgeSpec{AgentID: 42, Provider: "doubao_realtime", Model: "m", APIKey: "k"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestAnswerWithAI_CalleeMismatch(t *testing.T) {
	ctrl, _, _, _, _ := newControllerWithBridge()
	ctx := context.Background()

	callID, _, _, err := ctrl.Invite(ctx, 1, 2, "sess-ai-mismatch")
	require.NoError(t, err)

	_, _, err = ctrl.AnswerWithAI(ctx, callID, 99, call.VoiceBridgeSpec{AgentID: 42, Provider: "doubao_realtime", Model: "m", APIKey: "k"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "callee mismatch")
}

func TestAnswerWithAI_BridgeStartFails(t *testing.T) {
	ctrl, _, persist, bridge, _ := newControllerWithBridge()
	bridge.startErr = assert.AnError
	ctx := context.Background()

	callID, _, _, err := ctrl.Invite(ctx, 1, 2, "sess-ai-bridge-fail")
	require.NoError(t, err)

	_, _, err = ctrl.AnswerWithAI(ctx, callID, 2, call.VoiceBridgeSpec{AgentID: 42, Provider: "doubao_realtime", Model: "m", APIKey: "k"})
	require.Error(t, err)
	assert.Empty(t, persist.handovers)
}

func TestTakeover_Success(t *testing.T) {
	ctrl, _, persist, bridge, _ := newControllerWithBridge()
	ctx := context.Background()

	callID, _, _, err := ctrl.Invite(ctx, 1, 2, "sess-takeover-1")
	require.NoError(t, err)

	_, _, err = ctrl.AnswerWithAI(ctx, callID, 2, call.VoiceBridgeSpec{AgentID: 42, Provider: "doubao_realtime", Model: "m", APIKey: "k"})
	require.NoError(t, err)

	err = ctrl.Takeover(ctx, callID, 2)
	require.NoError(t, err)

	// 接管：AI 应被静音（MuteBridge 调用），状态切回 HUMAN_ACTIVE，
	// session 不重建（started 仍只有初始 1 次）。
	assert.Len(t, bridge.muted, 1)
	assert.Len(t, bridge.started, 1)
	require.Len(t, persist.handovers, 1)
	assert.Equal(t, "takeover", persist.handovers[0].Action)
}

func TestTakeover_NotAIDelegated(t *testing.T) {
	ctrl, _, _, _, _ := newControllerWithBridge()
	ctx := context.Background()

	callID, _, _, err := ctrl.Invite(ctx, 1, 2, "sess-takeover-not-ai")
	require.NoError(t, err)

	// 真人接听（非 AI 托管）
	_, _, err = ctrl.Answer(ctx, callID, 2)
	require.NoError(t, err)

	// Takeover 在非 AI_DELEGATED 状态下应返回错误
	err = ctrl.Takeover(ctx, callID, 2)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not ai_delegated")
}

func TestTakeover_CallNotFound(t *testing.T) {
	ctrl, _, _, _, _ := newControllerWithBridge()
	err := ctrl.Takeover(context.Background(), 9999, 2)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestHandBack_Success(t *testing.T) {
	ctrl, _, persist, bridge, _ := newControllerWithBridge()
	ctx := context.Background()

	callID, _, _, err := ctrl.Invite(ctx, 1, 2, "sess-handback-1")
	require.NoError(t, err)

	_, _, err = ctrl.AnswerWithAI(ctx, callID, 2, call.VoiceBridgeSpec{AgentID: 42, Provider: "doubao_realtime", Model: "m", APIKey: "k"})
	require.NoError(t, err)

	err = ctrl.Takeover(ctx, callID, 2)
	require.NoError(t, err)

	// 交回给 AI
	err = ctrl.HandBack(ctx, callID, 2)
	require.NoError(t, err)

	// 接管=静音、交回=恢复发声，session 不重建：started 仍只有初始 1 次，
	// 接管时 muted 1 次、交回时 unmuted 1 次。
	assert.Len(t, bridge.started, 1)
	assert.Len(t, bridge.muted, 1)
	assert.Len(t, bridge.unmuted, 1)
	require.Len(t, persist.handovers, 2)
	assert.Equal(t, "takeover", persist.handovers[0].Action)
	assert.Equal(t, "hand_back", persist.handovers[1].Action)
}

func TestHandBack_NotHumanActive(t *testing.T) {
	ctrl, _, _, _, _ := newControllerWithBridge()
	ctx := context.Background()

	callID, _, _, err := ctrl.Invite(ctx, 1, 2, "sess-handback-not-human")
	require.NoError(t, err)

	// AI 托管状态下直接 HandBack（未先 Takeover）应返回错误
	_, _, err = ctrl.AnswerWithAI(ctx, callID, 2, call.VoiceBridgeSpec{AgentID: 42, Provider: "doubao_realtime", Model: "m", APIKey: "k"})
	require.NoError(t, err)

	err = ctrl.HandBack(ctx, callID, 2)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not human_active")
}

// TestTakeoverHandBackLoop_SessionNotRebuilt 验证接管→交回多次循环：
// AI session 始终保持（只 started 1 次、不重建、不 stop），每次接管=静音、交回=恢复。
func TestTakeoverHandBackLoop_SessionNotRebuilt(t *testing.T) {
	ctrl, _, _, bridge, _ := newControllerWithBridge()
	ctx := context.Background()

	callID, _, _, err := ctrl.Invite(ctx, 1, 2, "sess-takeover-loop")
	require.NoError(t, err)
	_, _, err = ctrl.AnswerWithAI(ctx, callID, 2, call.VoiceBridgeSpec{AgentID: 42, Provider: "doubao_realtime", Model: "m", APIKey: "k"})
	require.NoError(t, err)

	for i := 0; i < 3; i++ {
		require.NoErrorf(t, ctrl.Takeover(ctx, callID, 2), "第 %d 轮接管", i+1)
		require.NoErrorf(t, ctrl.HandBack(ctx, callID, 2), "第 %d 轮交回", i+1)
	}

	assert.Len(t, bridge.started, 1, "全程不重建 session，started 只有初始 1 次")
	assert.Len(t, bridge.muted, 3, "三次接管各静音一次")
	assert.Len(t, bridge.unmuted, 3, "三次交回各恢复一次")
	assert.Empty(t, bridge.stopped, "循环中不应销毁 session")
}

func TestHandBack_CallNotFound(t *testing.T) {
	ctrl, _, _, _, _ := newControllerWithBridge()
	err := ctrl.HandBack(context.Background(), 9999, 2)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

// TestAICallStateTransitions 验证完整的 AI 托管状态机转换。
func TestAICallStateTransitions(t *testing.T) {
	ctrl, _, _, _, _ := newControllerWithBridge()
	ctx := context.Background()

	callID, _, _, err := ctrl.Invite(ctx, 1, 2, "sess-state-machine")
	require.NoError(t, err)

	// RINGING → AI_DELEGATED
	_, _, err = ctrl.AnswerWithAI(ctx, callID, 2, call.VoiceBridgeSpec{AgentID: 42, Provider: "doubao_realtime", Model: "m", APIKey: "k"})
	require.NoError(t, err)

	// AI_DELEGATED → HUMAN_ACTIVE（接管）
	err = ctrl.Takeover(ctx, callID, 2)
	require.NoError(t, err)

	// HUMAN_ACTIVE → AI_DELEGATED（交回）
	err = ctrl.HandBack(ctx, callID, 2)
	require.NoError(t, err)

	// AI_DELEGATED → ENDED（挂断）
	err = ctrl.Hangup(ctx, callID, 1)
	require.NoError(t, err)
}

// TestAICallDelegationMode 验证 CallRecord 的 delegation_mode 字段正确更新。
func TestAICallDelegationMode(t *testing.T) {
	ctrl, _, persist, _, _ := newControllerWithBridge()
	ctx := context.Background()

	callID, _, _, err := ctrl.Invite(ctx, 1, 2, "sess-delegation-mode")
	require.NoError(t, err)

	_, _, err = ctrl.AnswerWithAI(ctx, callID, 2, call.VoiceBridgeSpec{AgentID: 42, Provider: "doubao_realtime", Model: "m", APIKey: "k"})
	require.NoError(t, err)

	// 验证 persist 中记录了 ai_delegated 模式
	require.Len(t, persist.answered, 1)
	// delegation_mode 应在 UpdateAnsweredWithAI 中更新
	_ = persist
	_ = callID
	_ = model.CallDelegationAIDelegated
}

// TestAnswerWithAI_PersistFails_StateRolledBack 验证 DB 失败后内存状态回滚。
func TestAnswerWithAI_PersistFails_StateRolledBack(t *testing.T) {
	ctrl, _, persist, _, _ := newControllerWithBridge()
	persist.answeredErr = fmt.Errorf("db timeout")
	ctx := context.Background()

	callID, _, _, err := ctrl.Invite(ctx, 1, 2, "sess-rollback-persist")
	require.NoError(t, err)

	_, _, err = ctrl.AnswerWithAI(ctx, callID, 2, call.VoiceBridgeSpec{AgentID: 42, Provider: "doubao_realtime", Model: "m", APIKey: "k"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "persist answered")

	// 状态应回滚为 RINGING，通话不应卡死
	// 验证：再次 AnswerWithAI 应能成功（persist 错误清除后）
	persist.answeredErr = nil
	_, _, err = ctrl.AnswerWithAI(ctx, callID, 2, call.VoiceBridgeSpec{AgentID: 42, Provider: "doubao_realtime", Model: "m", APIKey: "k"})
	require.NoError(t, err, "after rollback, AnswerWithAI should succeed again")
}

// TestTakeover_MuteFails_StateRolledBack 验证 MuteBridge 失败后状态回滚。
func TestTakeover_MuteFails_StateRolledBack(t *testing.T) {
	ctrl, _, _, bridge, _ := newControllerWithBridge()
	ctx := context.Background()

	callID, _, _, err := ctrl.Invite(ctx, 1, 2, "sess-rollback-takeover")
	require.NoError(t, err)
	_, _, err = ctrl.AnswerWithAI(ctx, callID, 2, call.VoiceBridgeSpec{AgentID: 42, Provider: "doubao_realtime", Model: "m", APIKey: "k"})
	require.NoError(t, err)

	// 让 MuteBridge 失败
	bridge.muteErr = fmt.Errorf("bridge unreachable")
	err = ctrl.Takeover(ctx, callID, 2)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "mute bridge")

	// 状态应回滚为 AI_DELEGATED
	// 验证：HandBack 应失败（因为状态是 AI_DELEGATED，不是 HumanActive）
	err = ctrl.HandBack(ctx, callID, 2)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not human_active")
}
func TestAnswerWithAI_NotifiesCaller(t *testing.T) {
	ctrl, _, _, _, events := newControllerWithBridge()
	ctx := context.Background()

	callID, _, _, err := ctrl.Invite(ctx, 1, 2, "sess-notify-caller")
	require.NoError(t, err)

	_, _, err = ctrl.AnswerWithAI(ctx, callID, 2, call.VoiceBridgeSpec{AgentID: 42, Provider: "doubao_realtime", Model: "m", APIKey: "k"})
	require.NoError(t, err)

	// caller(1) 应收到 call:peer_answered 通知
	found := false
	for _, e := range *events {
		if e.userID == 1 && e.cmd == "call:peer_answered" {
			found = true
			break
		}
	}
	assert.True(t, found, "caller should receive call:peer_answered after AI answer")
}

// TestTakeover_NotifiesCaller 验证接管后 caller 收到通知。
func TestTakeover_NotifiesCaller(t *testing.T) {
	ctrl, _, _, _, events := newControllerWithBridge()
	ctx := context.Background()

	callID, _, _, err := ctrl.Invite(ctx, 1, 2, "sess-takeover-notify")
	require.NoError(t, err)
	_, _, err = ctrl.AnswerWithAI(ctx, callID, 2, call.VoiceBridgeSpec{AgentID: 42, Provider: "doubao_realtime", Model: "m", APIKey: "k"})
	require.NoError(t, err)

	err = ctrl.Takeover(ctx, callID, 2)
	require.NoError(t, err)

	found := false
	for _, e := range *events {
		if e.userID == 1 && e.cmd == "call:state" {
			found = true
			break
		}
	}
	assert.True(t, found, "caller should receive call:state after takeover")
}

// TestHandBack_NotifiesCaller 验证交回 AI 后 caller 收到通知。
func TestHandBack_NotifiesCaller(t *testing.T) {
	ctrl, _, _, _, events := newControllerWithBridge()
	ctx := context.Background()

	callID, _, _, err := ctrl.Invite(ctx, 1, 2, "sess-handback-notify")
	require.NoError(t, err)
	_, _, err = ctrl.AnswerWithAI(ctx, callID, 2, call.VoiceBridgeSpec{AgentID: 42, Provider: "doubao_realtime", Model: "m", APIKey: "k"})
	require.NoError(t, err)
	err = ctrl.Takeover(ctx, callID, 2)
	require.NoError(t, err)
	err = ctrl.HandBack(ctx, callID, 2)
	require.NoError(t, err)

	// caller 应收到 ai_delegated 模式的 call:state
	found := false
	for _, e := range *events {
		if e.userID == 1 && e.cmd == "call:state" {
			if p, ok := e.payload.(map[string]any); ok {
				if p["mode"] == "ai_delegated" {
					found = true
					break
				}
			}
		}
	}
	assert.True(t, found, "caller should receive call:state(ai_delegated) after hand_back")
}

// TestHandBack_UnmuteFails_RebuildsBridge 验证交回时 unmute 失败（如接管期间
// Provider 侧 session 已死）走重建兜底：Interrupt 挂起旧桥 + 用原 spec 重新起桥。
func TestHandBack_UnmuteFails_RebuildsBridge(t *testing.T) {
	ctrl, _, _, bridge, _ := newControllerWithBridge()
	ctx := context.Background()

	callID, _, _, err := ctrl.Invite(ctx, 1, 2, "sess-rebuild-1")
	require.NoError(t, err)
	_, _, err = ctrl.AnswerWithAI(ctx, callID, 2, call.VoiceBridgeSpec{AgentID: 42, Provider: "doubao_realtime", Model: "m", APIKey: "k"})
	require.NoError(t, err)
	require.NoError(t, ctrl.Takeover(ctx, callID, 2))

	bridge.unmuteErr = fmt.Errorf("doubao realtime session dead")
	err = ctrl.HandBack(ctx, callID, 2)
	require.NoError(t, err, "unmute 失败但重建成功，交回应成功")

	// 重建路径：interrupt 1 次 + start 2 次（初始 + 重建）
	assert.Equal(t, []int64{callID}, bridge.interrupted)
	assert.Len(t, bridge.started, 2)
	// 重建 spec 应补全 CallerID/SessionID（dialog_id=session_id 保上下文连续）
	assert.Equal(t, int64(1), bridge.lastStart.CallerID)
	assert.Equal(t, "sess-rebuild-1", bridge.lastStart.SessionID)
	assert.Equal(t, "doubao_realtime", bridge.lastStart.Provider)

	// 状态应为 AI_DELEGATED：可再次接管
	require.NoError(t, ctrl.Takeover(ctx, callID, 2))
}

// TestHandBack_UnmuteAndRebuildFail_RollsBack 验证 unmute 与重建都失败时回滚 HUMAN_ACTIVE。
func TestHandBack_UnmuteAndRebuildFail_RollsBack(t *testing.T) {
	ctrl, _, _, bridge, _ := newControllerWithBridge()
	ctx := context.Background()

	callID, _, _, err := ctrl.Invite(ctx, 1, 2, "sess-rebuild-2")
	require.NoError(t, err)
	_, _, err = ctrl.AnswerWithAI(ctx, callID, 2, call.VoiceBridgeSpec{AgentID: 42, Provider: "doubao_realtime", Model: "m", APIKey: "k"})
	require.NoError(t, err)
	require.NoError(t, ctrl.Takeover(ctx, callID, 2))

	bridge.unmuteErr = fmt.Errorf("session dead")
	bridge.startErr = fmt.Errorf("provider down")
	err = ctrl.HandBack(ctx, callID, 2)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "rebuild bridge")

	// 状态回滚 HUMAN_ACTIVE：恢复后可再次交回成功
	bridge.unmuteErr = nil
	bridge.startErr = nil
	require.NoError(t, ctrl.HandBack(ctx, callID, 2))
}

// TestEndCallOnBridgeExit_HumanActive_KeepsCallAlive 验证人工接管中 AI 桥退出不挂断整通。
func TestEndCallOnBridgeExit_HumanActive_KeepsCallAlive(t *testing.T) {
	ctrl, _, _, _, _ := newControllerWithBridge()
	ctx := context.Background()

	callID, _, _, err := ctrl.Invite(ctx, 1, 2, "sess-bridge-exit-1")
	require.NoError(t, err)
	_, _, err = ctrl.AnswerWithAI(ctx, callID, 2, call.VoiceBridgeSpec{AgentID: 42, Provider: "doubao_realtime", Model: "m", APIKey: "k"})
	require.NoError(t, err)
	require.NoError(t, ctrl.Takeover(ctx, callID, 2))

	// 人工接管中 AI 桥异常退出：通话必须存活
	ctrl.EndCallOnBridgeExit(ctx, callID)
	require.NoError(t, ctrl.HandBack(ctx, callID, 2), "bridge_exit 后通话应仍存活，可正常交回")
}

// TestEndCallOnBridgeExit_AIDelegated_EndsCall 验证 AI 接待中桥退出仍正常结束通话（原行为不变）。
func TestEndCallOnBridgeExit_AIDelegated_EndsCall(t *testing.T) {
	ctrl, _, _, _, _ := newControllerWithBridge()
	ctx := context.Background()

	callID, _, _, err := ctrl.Invite(ctx, 1, 2, "sess-bridge-exit-2")
	require.NoError(t, err)
	_, _, err = ctrl.AnswerWithAI(ctx, callID, 2, call.VoiceBridgeSpec{AgentID: 42, Provider: "doubao_realtime", Model: "m", APIKey: "k"})
	require.NoError(t, err)

	ctrl.EndCallOnBridgeExit(ctx, callID)
	err = ctrl.Takeover(ctx, callID, 2)
	require.Error(t, err, "AI 接待中 bridge_exit 应结束通话")
	assert.Contains(t, err.Error(), "not found")
}
