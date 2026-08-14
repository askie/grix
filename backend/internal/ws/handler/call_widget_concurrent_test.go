package handler

import (
	"context"
	"testing"

	"github.com/askie/grix/backend/internal/call"
	"github.com/askie/grix/backend/internal/pkg/testutil"
	"github.com/askie/grix/backend/internal/store"
	"github.com/askie/grix/backend/internal/ws/protocol"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// withMockRedisForCall 临时挂载 miniredis，返回还原函数。
func withMockRedisForCall(t *testing.T) func() {
	t.Helper()
	orig := store.RDB
	store.RDB = testutil.NewMockRedis()
	return func() { store.RDB = orig }
}

// reserveVoiceConcurrent：上限内放行、超限拒绝、释放后腾出名额、limit<=0 不限。
func TestReserveVoiceConcurrent_LimitAndRelease(t *testing.T) {
	defer withMockRedisForCall(t)()
	const agentID = int64(42)

	assert.True(t, reserveVoiceConcurrent(agentID, 1001, 2), "第1通应放行")
	assert.True(t, reserveVoiceConcurrent(agentID, 1002, 2), "第2通应放行")
	assert.False(t, reserveVoiceConcurrent(agentID, 1003, 2), "第3通超限应拒绝")

	// 释放一通后应再次放行
	releaseVoiceConcurrent(agentID, 1001)
	assert.True(t, reserveVoiceConcurrent(agentID, 1003, 2), "释放名额后应放行")

	// limit<=0 表示不限
	assert.True(t, reserveVoiceConcurrent(agentID, 2001, 0))
	assert.True(t, reserveVoiceConcurrent(agentID, 2002, 0))
}

// releaseVoiceConcurrent 对非成员调用应幂等无副作用（不会把计数压成负、不影响他通）。
func TestReleaseVoiceConcurrent_Idempotent(t *testing.T) {
	defer withMockRedisForCall(t)()
	const agentID = int64(7)
	assert.True(t, reserveVoiceConcurrent(agentID, 100, 1))
	releaseVoiceConcurrent(agentID, 999) // 非成员
	// 100 仍占用，limit=1 时新通应被拒
	assert.False(t, reserveVoiceConcurrent(agentID, 101, 1))
}

// 参与锁：同一 owner 第二台设备/第二通被拒，释放后可再获取；重入放行。
func TestParticipateLock_SingleLine(t *testing.T) {
	defer withMockRedisForCall(t)()
	ctx := context.Background()
	const owner = int64(100)

	ok, _ := acquireParticipateLock(ctx, owner, 500, "devA")
	require.True(t, ok, "首次获取应成功")

	// 同设备同通话重入应放行
	ok, _ = acquireParticipateLock(ctx, owner, 500, "devA")
	assert.True(t, ok, "重入应放行")

	// 另一设备应被拒
	ok, holder := acquireParticipateLock(ctx, owner, 500, "devB")
	assert.False(t, ok, "另一设备应被拒")
	assert.Equal(t, "500:devA", holder)

	// 同设备另一通话应被拒（人单线）
	ok, _ = acquireParticipateLock(ctx, owner, 600, "devA")
	assert.False(t, ok, "同设备另一通应被拒")

	// 释放后另一通可获取
	releaseParticipateLock(ctx, owner, 500, "devA")
	ok, _ = acquireParticipateLock(ctx, owner, 600, "devA")
	assert.True(t, ok, "释放后应可获取新通")
}

// 断连兜底：按设备释放参与锁。
func TestParticipateLock_ReleaseByDevice(t *testing.T) {
	defer withMockRedisForCall(t)()
	ctx := context.Background()
	const owner = int64(101)

	ok, _ := acquireParticipateLock(ctx, owner, 700, "devX")
	require.True(t, ok)

	// 设备不匹配不释放
	releaseParticipateLockByDevice(ctx, owner, "devOther")
	ok, _ = acquireParticipateLock(ctx, owner, 701, "devX")
	assert.False(t, ok, "未匹配设备不应释放")

	// 匹配设备释放
	releaseParticipateLockByDevice(ctx, owner, "devX")
	ok, _ = acquireParticipateLock(ctx, owner, 701, "devY")
	assert.True(t, ok, "匹配设备释放后应可重新获取")
}

// 多访客并发：同一 owner 配置语音托管后，两个不同访客都能 AI 代接（owner 不再被忙线锁占用）。
func TestHandleWidgetCallInvite_ConcurrentVisitorsSameOwner(t *testing.T) {
	bridge, cleanup := setupCallAIHandlerTest(t)
	defer cleanup()
	defer withMockRedisForCall(t)()

	resolveCalleeVoiceAgent = func(_ int64, _ string) (int64, bool) { return 42, true }
	resolveAgentVoiceSpec = func(id int64, _ string) (call.VoiceBridgeSpec, error) {
		return call.VoiceBridgeSpec{AgentID: id, Provider: "openai_realtime", Model: "m", APIKey: "k", AllowVisitor: true, MaxConcurrent: 5}, nil
	}

	hub := newCallHandlerMockHub()
	ownerConn := &callHandlerMockConn{userID: 100}
	hub.addConn(100, ownerConn)

	v1 := &callHandlerMockConn{userID: 9001}
	v2 := &callHandlerMockConn{userID: 9002}
	HandleWidgetCallInvite(hub, v1, widgetInvitePkt(), 100, "s_widget_1", "V1")
	HandleWidgetCallInvite(hub, v2, widgetInvitePkt(), 100, "s_widget_1", "V2")

	_, ack1 := v1.findCmd(protocol.CmdCallInviteAck)
	_, ack2 := v2.findCmd(protocol.CmdCallInviteAck)
	assert.True(t, ack1, "访客1应收到 invite_ack")
	assert.True(t, ack2, "访客2也应收到 invite_ack（owner 不被忙线锁占用）")
	assert.Len(t, bridge.started, 2, "两通都应启动 AI 桥接")
}

// 并发上限：超过 voice_max_concurrent_calls 的访客进入排队等待队列。
func TestHandleWidgetCallInvite_ConcurrentLimitReached(t *testing.T) {
	bridge, cleanup := setupCallAIHandlerTest(t)
	defer cleanup()
	defer withMockRedisForCall(t)()

	resolveCalleeVoiceAgent = func(_ int64, _ string) (int64, bool) { return 42, true }
	resolveAgentVoiceSpec = func(id int64, _ string) (call.VoiceBridgeSpec, error) {
		return call.VoiceBridgeSpec{AgentID: id, Provider: "openai_realtime", Model: "m", APIKey: "k", AllowVisitor: true, MaxConcurrent: 1}, nil
	}

	hub := newCallHandlerMockHub()
	hub.addConn(9001, &callHandlerMockConn{userID: 9001})
	hub.addConn(9002, &callHandlerMockConn{userID: 9002})

	v1 := &callHandlerMockConn{userID: 9001}
	v2 := &callHandlerMockConn{userID: 9002}
	HandleWidgetCallInvite(hub, v1, widgetInvitePkt(), 100, "s_widget_1", "V1")
	HandleWidgetCallInvite(hub, v2, widgetInvitePkt(), 100, "s_widget_1", "V2")

	_, ack1 := v1.findCmd(protocol.CmdCallInviteAck)
	_, queued2 := v2.findCmd(protocol.CmdCallQueued)
	assert.True(t, ack1, "访客1应接通")
	assert.True(t, queued2, "访客2超并发上限应进入排队")
	assert.Len(t, bridge.started, 1, "仅第1通启动桥接")
}
