package agentapi

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/testutil"
	"github.com/askie/grix/backend/internal/store"
	"github.com/askie/grix/backend/internal/ws/protocol"
	"github.com/stretchr/testify/require"
)

// 幽灵事件收口：客户端静默丢弃的事件永远等不到 event_result，此前 event_stop_result
// 路径不清 pending/durable、run 不在内存时也不写任何终态 —— durable 记录让工具栏
// 一直显示 running，chat_states 卡死，停止按钮无效。
//
// 修复后 handleEventStopResult 必须：
//  1. 清掉 pending 计时器，并把短期 durable 记录冻结为 settled tombstone；
//  2. 在 DB 留下不可变终态，供跨节点/超 TTL 重放判定；
//  3. in-memory run 缺失但 durable 记录存在时，把 chat_states 兜底落成 idle。
func TestHandleEventStopResult_GhostRunSettled(t *testing.T) {
	origRDB, origDB := store.RDB, store.DB
	store.RDB = testutil.NewMockRedis()
	store.DB = testutil.NewTestDB().DB
	t.Cleanup(func() { store.RDB, store.DB = origRDB, origDB })

	mgr := NewManager("", 30*time.Second, nil, nil, nil, nil)
	defer mgr.Shutdown()
	conn := &agentConn{agentID: 300, send: make(chan []byte, 8)}

	evt := DelegateEventPayload{
		EventID: "evt-ghost", AgentID: 300, OwnerID: 400, SenderID: 400,
		SessionID: "sess-ghost", SessionType: 1, MsgID: 500,
	}
	// 幽灵形态：durable 记录 + pending 计时器都在，但 in-memory run 不在
	// （对应"注册节点重启丢内存态"，或跨节点只读到 durable 的场景）。
	mgr.registerPendingEventAck(evt, 1)
	mgr.resolvePendingEventAck(evt.EventID, time.Now().UnixMilli())
	// chat_states 卡在 running（幽灵挡住了终态写入的历史结果）。
	seed, err := store.LoadAgentEventTerminalLedger(evt.EventID)
	require.NoError(t, err)
	require.NotNil(t, seed)
	store.UpsertSessionAgentStateRunningWithGeneration(
		evt.SessionID,
		evt.OwnerID,
		evt.AgentID,
		evt.EventID,
		time.Now(),
		seed.DispatchGeneration,
	)

	raw, err := json.Marshal(protocol.AgentEventStopResultPayload{
		EventID: evt.EventID, StopID: "stop-1", Status: "stopped",
	})
	require.NoError(t, err)
	mgr.handleEventStopResult(conn, &protocol.Packet{Cmd: protocol.CmdEventStopResult, Payload: raw})

	// durable 记录保留 settled tombstone；活跃态扫描会忽略它，而短期重放可直接 ACK。
	durable, ok := loadDurablePendingDelegate(context.Background(), evt.EventID)
	require.True(t, ok)
	require.Equal(t, durablePendingDelegateStageSettled, durable.Stage)
	require.NotNil(t, durable.Terminal)
	require.Equal(t, protocol.AgentEventResultCanceled, durable.Terminal.Status)
	ledger, err := store.LoadAgentEventTerminalLedger(evt.EventID)
	require.NoError(t, err)
	require.NotNil(t, ledger)
	require.Equal(t, protocol.AgentEventResultCanceled, ledger.Status)
	require.Equal(t, "owner_requested_stop", ledger.Code)
	// pending 计时器必须摘除，否则 owner 级 touch 会无限续期。
	mgr.acksMu.Lock()
	_, pendingLeft := mgr.pending[evt.EventID]
	mgr.acksMu.Unlock()
	require.False(t, pendingLeft, "pending 计时器应在 event_stop_result 收口时移除")
	// chat_states 兜底落 idle（异步写，轮询等待）。
	require.Eventually(t, func() bool {
		rows, _, err := store.ListSessionAgentStatesByOwner(evt.OwnerID, evt.SessionID, "", 1, 10)
		return err == nil && len(rows) == 1 && rows[0].State == model.SessionAgentStateIdle
	}, 3*time.Second, 20*time.Millisecond, "chat_states 应被兜底写成 idle")
}

// 正常停止（run 在内存）也必须走同一不可变终态流水线；Redis 留短期
// settled tombstone，DB ledger 则永久吸收同 event_id 重放。
func TestHandleEventStopResult_InMemoryRunStopped(t *testing.T) {
	origRDB, origDB := store.RDB, store.DB
	store.RDB = testutil.NewMockRedis()
	store.DB = testutil.NewTestDB().DB
	t.Cleanup(func() { store.RDB, store.DB = origRDB, origDB })

	mgr := NewManager("", 30*time.Second, nil, nil, nil, nil)
	defer mgr.Shutdown()
	conn := &agentConn{agentID: 301, send: make(chan []byte, 8)}

	evt := DelegateEventPayload{
		EventID: "evt-live", AgentID: 301, OwnerID: 401, SenderID: 401,
		SessionID: "sess-live", SessionType: 1, MsgID: 501,
	}
	mgr.registerPendingEventAck(evt, 1)
	mgr.registerActiveRunForDispatch(evt, time.Now(), false)
	mgr.resolvePendingEventAck(evt.EventID, time.Now().UnixMilli())
	_, _, err := mgr.RequestOutputStop(evt.OwnerID, evt.SessionID, evt.EventID)
	require.NoError(t, err)
	require.True(t, mgr.ShouldFenceEventReply(evt.EventID), "停止请求等待结果时应保持事件级 fence")

	raw, err := json.Marshal(protocol.AgentEventStopResultPayload{
		EventID: evt.EventID, StopID: "stop-2", Status: "stopped",
	})
	require.NoError(t, err)
	mgr.handleEventStopResult(conn, &protocol.Packet{Cmd: protocol.CmdEventStopResult, Payload: raw})

	require.Nil(t, mgr.LookupActiveRun(evt.EventID), "in-memory run 应被移除")
	durable, ok := loadDurablePendingDelegate(context.Background(), evt.EventID)
	require.True(t, ok)
	require.Equal(t, durablePendingDelegateStageSettled, durable.Stage)
	ledger, err := store.LoadAgentEventTerminalLedger(evt.EventID)
	require.NoError(t, err)
	require.NotNil(t, ledger)
	require.Equal(t, protocol.AgentEventResultCanceled, ledger.Status)
	require.Eventually(t, func() bool {
		rows, _, err := store.ListSessionAgentStatesByOwner(evt.OwnerID, evt.SessionID, "", 1, 10)
		return err == nil && len(rows) == 1 && rows[0].State == model.SessionAgentStateIdle
	}, 3*time.Second, 20*time.Millisecond, "chat_states 应写成 idle")
}

// 停止失败（stop_result=failed）意味着事件可能仍在执行：pending 计时器与 durable
// 记录必须保留，否则事件之后真正完成时的 event_result 会因记录已删而无法落终态。
func TestHandleEventStopResult_FailedStopKeepsTracking(t *testing.T) {
	origRDB, origDB := store.RDB, store.DB
	store.RDB = testutil.NewMockRedis()
	store.DB = testutil.NewTestDB().DB
	t.Cleanup(func() { store.RDB, store.DB = origRDB, origDB })

	mgr := NewManager("", 30*time.Second, nil, nil, nil, nil)
	defer mgr.Shutdown()
	conn := &agentConn{agentID: 302, send: make(chan []byte, 8)}

	evt := DelegateEventPayload{
		EventID: "evt-stopfail", AgentID: 302, OwnerID: 402, SenderID: 402,
		SessionID: "sess-stopfail", SessionType: 1, MsgID: 502,
	}
	mgr.registerActiveRun(evt)
	mgr.registerPendingEventAck(evt, 1)
	mgr.resolvePendingEventAck(evt.EventID, time.Now().UnixMilli())
	_, _, err := mgr.RequestOutputStop(evt.OwnerID, evt.SessionID, evt.EventID)
	require.NoError(t, err)
	require.True(t, mgr.ShouldFenceEventReply(evt.EventID), "停止请求等待结果时应保持事件级 fence")

	raw, err := json.Marshal(protocol.AgentEventStopResultPayload{
		EventID: evt.EventID, StopID: "stop-3", Status: "failed", Msg: "stop handler failed",
	})
	require.NoError(t, err)
	mgr.handleEventStopResult(conn, &protocol.Packet{Cmd: protocol.CmdEventStopResult, Payload: raw})

	if _, ok := loadDurablePendingDelegate(context.Background(), evt.EventID); !ok {
		t.Fatal("停止失败时 durable 记录必须保留，供事件真正完成时落终态")
	}
	mgr.acksMu.Lock()
	_, pendingLeft := mgr.pending[evt.EventID]
	mgr.acksMu.Unlock()
	require.True(t, pendingLeft, "停止失败时 pending 计时器必须保留")
	run := mgr.LookupActiveRun(evt.EventID)
	require.NotNil(t, run, "停止请求失败不能移除仍可能执行的 run")
	require.Equal(t, protocol.AgentOutputStateReceived, run.State)
	require.True(t, run.CanStop, "停止失败后应允许重新发起停止")
	require.False(t, mgr.ShouldFenceEventReply(evt.EventID), "停止失败后不得继续阻断后续输出")
}

// 越权防护：stop_result 只能清理归属当前连接 agent 的事件记录。
func TestCleanupEventStopResidue_AgentOwnershipGuard(t *testing.T) {
	origRDB, origDB := store.RDB, store.DB
	store.RDB = testutil.NewMockRedis()
	store.DB = testutil.NewTestDB().DB
	t.Cleanup(func() { store.RDB, store.DB = origRDB, origDB })

	mgr := NewManager("", 30*time.Second, nil, nil, nil, nil)
	defer mgr.Shutdown()
	evt := DelegateEventPayload{
		EventID: "evt-foreign", AgentID: 303, OwnerID: 403, SenderID: 403,
		SessionID: "sess-foreign", SessionType: 1, MsgID: 503,
	}
	mgr.registerPendingEventAck(evt, 1)
	mgr.resolvePendingEventAck(evt.EventID, time.Now().UnixMilli())

	// 另一个 agent(999) 的连接尝试清理：应零效果。
	record := mgr.cleanupEventStopResidue(evt.EventID, 999)
	require.Nil(t, record, "他人事件的 durable 记录不应返回")
	if _, ok := loadDurablePendingDelegate(context.Background(), evt.EventID); !ok {
		t.Fatal("他人事件的 durable 记录不应被删除")
	}
	mgr.acksMu.Lock()
	_, pendingLeft := mgr.pending[evt.EventID]
	mgr.acksMu.Unlock()
	require.True(t, pendingLeft, "他人事件的 pending 计时器不应被摘除")

	// 归属一致的清理正常生效。
	record = mgr.cleanupEventStopResidue(evt.EventID, 303)
	require.NotNil(t, record)
	if _, ok := loadDurablePendingDelegate(context.Background(), evt.EventID); ok {
		t.Fatal("归属一致时 durable 记录应被删除")
	}
}
