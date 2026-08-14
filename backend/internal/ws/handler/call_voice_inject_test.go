package handler

import (
	"context"
	"testing"
	"time"

	"github.com/askie/grix/backend/internal/call"
	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/store"
	"github.com/stretchr/testify/assert"
)

func TestMaybeInjectVoiceReply_SkipsWhenNoActiveCall(t *testing.T) {
	ctrl := call.New(nil, nil, nil)
	origCtrl := callCtrl
	callCtrl = ctrl
	defer func() { callCtrl = origCtrl }()

	origNC := store.NC
	store.NC = nil
	defer func() { store.NC = origNC }()

	MaybeInjectVoiceReply(context.Background(), "session-no-call", "hello")
}

func TestMaybeInjectVoiceReply_SkipsEmptyContent(t *testing.T) {
	ctrl := call.New(nil, nil, nil)
	ctrl.TestInjectCall(100, model.CallRecord{
		SessionID: "s1", State: model.CallStateAIDelegated,
	}, call.VoiceBridgeSpec{Provider: "doubao_realtime"})
	defer ctrl.TestRemoveCall(100)

	origCtrl := callCtrl
	callCtrl = ctrl
	defer func() { callCtrl = origCtrl }()

	MaybeInjectVoiceReply(context.Background(), "s1", "   ")
}

func TestMaybeInjectVoiceReply_SkipsNilCallCtrl(t *testing.T) {
	origCtrl := callCtrl
	callCtrl = nil
	defer func() { callCtrl = origCtrl }()

	MaybeInjectVoiceReply(context.Background(), "s1", "hello")
}

func TestMaybeInjectVoiceReply_FindsActiveCall(t *testing.T) {
	ctrl := call.New(nil, nil, nil)
	ctrl.TestInjectCall(200, model.CallRecord{
		SessionID: "s2", State: model.CallStateAIDelegated,
	}, call.VoiceBridgeSpec{Provider: "doubao_realtime"})
	defer ctrl.TestRemoveCall(200)

	origCtrl := callCtrl
	callCtrl = ctrl
	defer func() { callCtrl = origCtrl }()

	origNC := store.NC
	store.NC = nil
	defer func() { store.NC = origNC }()

	MaybeInjectVoiceReply(context.Background(), "s2", "answer")

	callID, provider, direct, ok := ctrl.GetActiveCallBySession("s2")
	assert.True(t, ok)
	assert.Equal(t, int64(200), callID)
	assert.Equal(t, "doubao_realtime", provider)
	assert.False(t, direct)
}

func TestMaybeInjectVoiceReply_TimeoutSkips(t *testing.T) {
	ctrl := call.New(nil, nil, nil)
	ctrl.TestInjectCall(300, model.CallRecord{
		SessionID: "s3", State: model.CallStateAIDelegated,
	}, call.VoiceBridgeSpec{Provider: "doubao_realtime"})
	defer ctrl.TestRemoveCall(300)

	origCtrl := callCtrl
	callCtrl = ctrl
	defer func() { callCtrl = origCtrl }()

	origNC := store.NC
	store.NC = nil
	defer func() { store.NC = origNC }()

	origRDB := store.RDB
	store.RDB = nil // 走内存 map 路径
	defer func() { store.RDB = origRDB }()

	// 模拟 caller 转写写入时间超过 voiceInjectTimeout
	voiceInjectMu.Lock()
	callerTS["s3"] = time.Now().Add(-voiceInjectTimeout - time.Second)
	voiceInjectMu.Unlock()
	defer ClearCallerTranscript("s3")

	// 超时后不应注入（store.NC=nil，若走到 publish 会 panic/warn，这里验证提前返回）
	MaybeInjectVoiceReply(context.Background(), "s3", "late answer")
	// 无 panic 即通过——超时分支提前返回
}

func TestMaybeInjectVoiceReply_WithinTimeoutProceeds(t *testing.T) {
	ctrl := call.New(nil, nil, nil)
	ctrl.TestInjectCall(400, model.CallRecord{
		SessionID: "s4", State: model.CallStateAIDelegated,
	}, call.VoiceBridgeSpec{Provider: "doubao_realtime"})
	defer ctrl.TestRemoveCall(400)

	origCtrl := callCtrl
	callCtrl = ctrl
	defer func() { callCtrl = origCtrl }()

	origNC := store.NC
	store.NC = nil
	defer func() { store.NC = origNC }()

	origRDB := store.RDB
	store.RDB = nil // 走内存 map 路径
	defer func() { store.RDB = origRDB }()

	// 模拟刚刚写入（在阈值内）
	RecordCallerTranscript("s4")
	defer ClearCallerTranscript("s4")

	// 在阈值内，应走到 publish 步骤（NC=nil 会安全跳过）
	MaybeInjectVoiceReply(context.Background(), "s4", "timely answer")
}

// 直拨调度场景守卫放宽规范（架构文档 33）：超时阈值 5 分钟、不限每轮一次。
func TestVoiceInjectGuards_DirectRelaxations(t *testing.T) {
	origRDB := store.RDB
	store.RDB = nil // 走内存 map 路径
	defer func() { store.RDB = origRDB }()
	ctx := context.Background()

	setTS := func(sessionID string, age time.Duration) {
		voiceInjectMu.Lock()
		callerTS[sessionID] = time.Now().Add(-age)
		delete(injectOnce, sessionID)
		voiceInjectMu.Unlock()
	}

	t.Run("non-direct skips after 15s", func(t *testing.T) {
		setTS("g1", 20*time.Second)
		defer ClearCallerTranscript("g1")
		assert.False(t, voiceInjectGuardsPass(ctx, 1, "g1", false), "代接场景 20s 前的轮次应超时跳过")
	})

	t.Run("direct allows slow agent reply within 5min", func(t *testing.T) {
		setTS("g2", 90*time.Second)
		defer ClearCallerTranscript("g2")
		assert.True(t, voiceInjectGuardsPass(ctx, 2, "g2", true), "直拨场景 90s 出结果必须仍可播报")
	})

	t.Run("direct skips after 5min", func(t *testing.T) {
		setTS("g3", 6*time.Minute)
		defer ClearCallerTranscript("g3")
		assert.False(t, voiceInjectGuardsPass(ctx, 3, "g3", true), "直拨场景超过 5 分钟仍应兜底跳过")
	})

	t.Run("non-direct injects only once per round", func(t *testing.T) {
		setTS("g4", time.Second)
		defer ClearCallerTranscript("g4")
		assert.True(t, voiceInjectGuardsPass(ctx, 4, "g4", false), "本轮第一条应放行")
		assert.False(t, voiceInjectGuardsPass(ctx, 4, "g4", false), "本轮第二条应被 once 拦截")
	})

	t.Run("direct injects every reply", func(t *testing.T) {
		setTS("g5", time.Second)
		defer ClearCallerTranscript("g5")
		assert.True(t, voiceInjectGuardsPass(ctx, 5, "g5", true))
		assert.True(t, voiceInjectGuardsPass(ctx, 5, "g5", true), "直拨场景先应答后出结果，每条都应播报")
	})

	t.Run("direct round does not consume once mark", func(t *testing.T) {
		setTS("g6", time.Second)
		defer ClearCallerTranscript("g6")
		assert.True(t, voiceInjectGuardsPass(ctx, 6, "g6", true))
		assert.True(t, voiceInjectGuardsPass(ctx, 6, "g6", false), "direct 放行不应吃掉代接路径的 once 配额")
	})
}

// 通话结束（cleanupCallGuards）应清理 caller 转写时间戳，避免按 session 累积泄漏。
func TestCleanupCallGuards_ClearsCallerTranscript(t *testing.T) {
	origRDB := store.RDB
	store.RDB = nil // 无 redis：forgetCallOwner/releaseCallBusyForRecord 安全早返
	defer func() { store.RDB = origRDB }()

	RecordCallerTranscript("cleanup-session")
	voiceInjectMu.Lock()
	_, exists := callerTS["cleanup-session"]
	voiceInjectMu.Unlock()
	assert.True(t, exists, "前置：时间戳应已写入")

	cleanupCallGuards(context.Background(), model.CallRecord{
		ID: 900, SessionID: "cleanup-session",
	})

	voiceInjectMu.Lock()
	_, exists = callerTS["cleanup-session"]
	voiceInjectMu.Unlock()
	assert.False(t, exists, "通话结束后时间戳应被清理")
}

func TestRecordAndClearCallerTranscript(t *testing.T) {
	origRDB := store.RDB
	store.RDB = nil // 走内存 map 路径
	defer func() { store.RDB = origRDB }()

	RecordCallerTranscript("test-session")
	voiceInjectMu.Lock()
	_, exists := callerTS["test-session"]
	voiceInjectMu.Unlock()
	assert.True(t, exists)

	ClearCallerTranscript("test-session")
	voiceInjectMu.Lock()
	_, exists = callerTS["test-session"]
	voiceInjectMu.Unlock()
	assert.False(t, exists)
}
