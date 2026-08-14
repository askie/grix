package handler

// 访客语音通话入口失败路径回归测试。
// 验证：并发槽和每日配额在每条失败路径都被正确释放，防止资源泄漏。

import (
	"context"
	"errors"
	"strconv"
	"testing"

	"github.com/askie/grix/backend/internal/call"
	"github.com/askie/grix/backend/internal/store"
	"github.com/askie/grix/backend/internal/ws/protocol"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// failingMockRoom 模拟 LiveKit 建房失败，用于 InviteVisitorWithID 失败路径测试。
type failingMockRoom struct{ err error }

func (r *failingMockRoom) CreateRoom(_ context.Context, _ int64, _, _ int64) (string, string, string, error) {
	return "", "", "", r.err
}
func (r *failingMockRoom) CloseRoom(_ context.Context, _ int64) error { return nil }

// trackReleaseQuota 替换 releaseVoiceDailyQuota 为计数器，返回计数指针和还原函数。
func trackReleaseQuota(t *testing.T) (*int, func()) {
	t.Helper()
	count := 0
	orig := releaseVoiceDailyQuota
	releaseVoiceDailyQuota = func(_ int64, _ int) { count++ }
	return &count, func() { releaseVoiceDailyQuota = orig }
}

// assertSlotFreed 验证 agentID 并发槽已释放：以 limit=1 发起新通话，预期接通。
func assertSlotFreed(t *testing.T, hub *callHandlerMockHub, agentID, newVisitorID int64) {
	t.Helper()
	conn := &callHandlerMockConn{userID: newVisitorID}
	hub.addConn(newVisitorID, conn)
	resolveAgentVoiceSpec = func(id int64, _ string) (call.VoiceBridgeSpec, error) {
		return call.VoiceBridgeSpec{AgentID: id, Provider: "openai_realtime", Model: "m", APIKey: "k", AllowVisitor: true, MaxConcurrent: 1, DailyLimit: 10}, nil
	}
	HandleWidgetCallInvite(hub, conn, widgetInvitePkt(), 100, "s1", "V-next")
	_, gotAck := conn.findCmd(protocol.CmdCallInviteAck)
	assert.True(t, gotAck, "agentID=%d 并发槽应已释放，后续访客应能接通", agentID)
}

// AnswerWithAI 失败（Bridge 启动出错）→ 并发槽和每日配额必须释放。
func TestHandleWidgetCallInvite_AnswerWithAIFailure_SlotAndQuotaReleased(t *testing.T) {
	bridge, cleanup := setupCallAIHandlerTest(t)
	defer cleanup()
	defer withMockRedisForCall(t)()

	quotaReleased, restoreQuota := trackReleaseQuota(t)
	defer restoreQuota()

	resolveCalleeVoiceAgent = func(_ int64, _ string) (int64, bool) { return 42, true }
	resolveAgentVoiceSpec = func(id int64, _ string) (call.VoiceBridgeSpec, error) {
		return call.VoiceBridgeSpec{AgentID: id, Provider: "openai_realtime", Model: "m", APIKey: "k", AllowVisitor: true, MaxConcurrent: 1, DailyLimit: 10}, nil
	}
	bridge.startErr = errors.New("bridge unavailable")

	hub := newCallHandlerMockHub()
	v1 := &callHandlerMockConn{userID: 9001}
	HandleWidgetCallInvite(hub, v1, widgetInvitePkt(), 100, "s1", "V1")

	_, gotErr := v1.findCmd(protocol.CmdError)
	require.True(t, gotErr, "访客应收到错误")
	assert.Equal(t, 1, *quotaReleased, "AnswerWithAI 失败应退还每日配额")

	// 槽已释放：恢复 bridge 正常，第二位访客应能接通
	bridge.startErr = nil
	assertSlotFreed(t, hub, 42, 9002)
}

// InviteVisitorWithID 失败（LiveKit 建房出错）→ 并发槽和每日配额必须释放。
func TestHandleWidgetCallInvite_InviteFailure_SlotAndQuotaReleased(t *testing.T) {
	_, cleanup := setupCallAIHandlerTest(t)
	defer cleanup()
	defer withMockRedisForCall(t)()

	quotaReleased, restoreQuota := trackReleaseQuota(t)
	defer restoreQuota()

	// 替换为会失败的 room，模拟 LiveKit 故障
	failRoom := &failingMockRoom{err: errors.New("livekit unavailable")}
	persist := &callHandlerMockPersist{}
	goodBridge := &mockBridgeManager{}
	callCtrl = call.NewWithBridge(failRoom, persist, func(_ int64, _ string, _ any) {}, goodBridge)
	callCtrl.SetCleanupHook(cleanupCallGuards)

	resolveCalleeVoiceAgent = func(_ int64, _ string) (int64, bool) { return 42, true }
	resolveAgentVoiceSpec = func(id int64, _ string) (call.VoiceBridgeSpec, error) {
		return call.VoiceBridgeSpec{AgentID: id, Provider: "openai_realtime", Model: "m", APIKey: "k", AllowVisitor: true, MaxConcurrent: 1, DailyLimit: 10}, nil
	}

	hub := newCallHandlerMockHub()
	v1 := &callHandlerMockConn{userID: 9001}
	HandleWidgetCallInvite(hub, v1, widgetInvitePkt(), 100, "s1", "V1")

	_, gotErr := v1.findCmd(protocol.CmdError)
	require.True(t, gotErr, "访客应收到错误")
	assert.Equal(t, 1, *quotaReleased, "InviteVisitorWithID 失败应退还每日配额")

	// 槽已释放：换回正常 room，第二位访客应能接通
	callCtrl = call.NewWithBridge(&callHandlerMockRoom{}, persist, func(_ int64, _ string, _ any) {}, goodBridge)
	callCtrl.SetCleanupHook(cleanupCallGuards)
	assertSlotFreed(t, hub, 42, 9002)
}

// reserveCallBusy 失败（访客已在另一通话中）→ 并发槽和每日配额必须释放。
func TestHandleWidgetCallInvite_BusyVisitor_SlotAndQuotaReleased(t *testing.T) {
	_, cleanup := setupCallAIHandlerTest(t)
	defer cleanup()
	defer withMockRedisForCall(t)()

	quotaReleased, restoreQuota := trackReleaseQuota(t)
	defer restoreQuota()

	resolveCalleeVoiceAgent = func(_ int64, _ string) (int64, bool) { return 42, true }
	resolveAgentVoiceSpec = func(id int64, _ string) (call.VoiceBridgeSpec, error) {
		return call.VoiceBridgeSpec{AgentID: id, Provider: "openai_realtime", Model: "m", APIKey: "k", AllowVisitor: true, MaxConcurrent: 2, DailyLimit: 10}, nil
	}

	// 预设访客忙线标记（模拟访客已在另一通话中）
	const visitorID = int64(9001)
	ctx := context.Background()
	err := store.RDB.SetNX(ctx, callBusyKey(visitorID), strconv.FormatInt(99999, 10), callBusyTTL).Err()
	require.NoError(t, err)

	hub := newCallHandlerMockHub()
	v1 := &callHandlerMockConn{userID: visitorID}
	HandleWidgetCallInvite(hub, v1, widgetInvitePkt(), 100, "s1", "V1")

	_, gotErr := v1.findCmd(protocol.CmdError)
	require.True(t, gotErr, "访客应收到错误（已忙线）")
	assert.Equal(t, 1, *quotaReleased, "reserveCallBusy 失败应退还每日配额")

	// 并发槽也已释放：busy 前已 SADD 了 callID，busy 失败后应显式 SREM
	n, _ := store.RDB.SCard(ctx, voiceConcurrentKey(42)).Result()
	assert.Equal(t, int64(0), n, "reserveCallBusy 失败后并发槽集合应为空")
}
