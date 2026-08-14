package agentapi

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/askie/grix/backend/internal/pkg/testutil"
	"github.com/askie/grix/backend/internal/store"
	"github.com/askie/grix/backend/internal/ws/protocol"
	"github.com/stretchr/testify/require"
)

// 停止目标解析：连接器上报的队列快照（谁在跑、谁在排队）是唯一权威。
// 此前停止按钮用 runBySX"最新注册"指针选目标，有排队时永远指向队尾，
// 导致"点停止消掉的是排队消息，运行中的任务还在跑"。
//
// 修复后：
//  1. queue_snapshot 落 Redis 只读镜像；
//  2. RequestOutputStop 未指明 run 时优先取镜像 running 事件；
//  3. LookupActiveRunBySessionOwner（工具栏"当前任务"）同样镜像优先。

func setupMirrorTest(t *testing.T) *Manager {
	t.Helper()
	origRDB, origDB := store.RDB, store.DB
	store.RDB = testutil.NewMockRedis()
	store.DB = testutil.NewTestDB().DB
	t.Cleanup(func() { store.RDB, store.DB = origRDB, origDB })
	mgr := NewManager("", 30*time.Second, nil, nil, nil, nil)
	// 晚注册先执行：关停在还原全局 DB 之前跑完，后台协程不会活到下一个测试。
	t.Cleanup(mgr.Shutdown)
	return mgr
}

func registerRun(mgr *Manager, eventID, sessionID string, ownerID, agentID, msgID int64) {
	mgr.registerActiveRun(DelegateEventPayload{
		EventID: eventID, AgentID: agentID, OwnerID: ownerID, SenderID: ownerID,
		SessionID: sessionID, SessionType: 1, MsgID: msgID,
	})
}

func storeMirror(t *testing.T, ownerID, agentID int64, sessionID string, running []string, queued []string) {
	t.Helper()
	queuedItems := make([]map[string]any, 0, len(queued))
	for i, id := range queued {
		queuedItems = append(queuedItems, map[string]any{"event_id": id, "position": i + 1})
	}
	raw, err := json.Marshal(map[string]any{
		"session_id": sessionID,
		"running":    running,
		"queued":     queuedItems,
	})
	require.NoError(t, err)
	storeQueueSnapshotMirror(context.Background(), ownerID, agentID, raw)
}

// 1 条 running + 2 条排队：不带 run_id 的停止必须命中 running 的那条，
// 而不是 runBySX 指针指向的最后注册（队尾）事件。
func TestRequestOutputStop_PrefersMirrorRunningOverLatestRegistered(t *testing.T) {
	mgr := setupMirrorTest(t)
	const (
		owner   = int64(400)
		agent   = int64(300)
		session = "sess-mirror"
	)
	registerRun(mgr, "evt-running", session, owner, agent, 1)
	registerRun(mgr, "evt-q1", session, owner, agent, 2)
	registerRun(mgr, "evt-q2", session, owner, agent, 3) // runBySX 现在指向队尾 evt-q2
	storeMirror(t, owner, agent, session, []string{"evt-running"}, []string{"evt-q1", "evt-q2"})

	ack, run, err := mgr.RequestOutputStop(owner, session, "")
	require.NoError(t, err)
	require.NotNil(t, run)
	require.True(t, ack.Accepted)
	require.Equal(t, "evt-running", ack.RunID, "停止目标必须是镜像里 running 的事件")
	require.Equal(t, "evt-running", run.EventID)
}

// 显式指定 run_id 的停止（如任务列表逐条停排队消息）不受镜像影响，按指定执行。
func TestRequestOutputStop_ExplicitRunIDBypassesMirror(t *testing.T) {
	mgr := setupMirrorTest(t)
	const (
		owner   = int64(400)
		agent   = int64(300)
		session = "sess-mirror-explicit"
	)
	registerRun(mgr, "evt-running", session, owner, agent, 1)
	registerRun(mgr, "evt-q1", session, owner, agent, 2)
	storeMirror(t, owner, agent, session, []string{"evt-running"}, []string{"evt-q1"})

	ack, run, err := mgr.RequestOutputStop(owner, session, "evt-q1")
	require.NoError(t, err)
	require.NotNil(t, run)
	require.Equal(t, "evt-q1", ack.RunID)
}

// 无镜像（不推快照的 agent / 会话空闲后镜像已删）时保持旧行为：runBySX 指针兜底。
func TestRequestOutputStop_FallsBackWithoutMirror(t *testing.T) {
	mgr := setupMirrorTest(t)
	const (
		owner   = int64(400)
		agent   = int64(300)
		session = "sess-no-mirror"
	)
	registerRun(mgr, "evt-only", session, owner, agent, 1)

	ack, run, err := mgr.RequestOutputStop(owner, session, "")
	require.NoError(t, err)
	require.NotNil(t, run)
	require.Equal(t, "evt-only", ack.RunID)
}

// 镜像 running 是自驱虚拟项（selfdrive_*，无真实 run）时穿透到旧逻辑，不误伤。
func TestRequestOutputStop_SelfDrivenVirtualEntryFallsThrough(t *testing.T) {
	mgr := setupMirrorTest(t)
	const (
		owner   = int64(400)
		agent   = int64(300)
		session = "sess-selfdrive"
	)
	registerRun(mgr, "evt-real", session, owner, agent, 1)
	storeMirror(t, owner, agent, session, []string{"selfdrive_" + session}, nil)

	ack, run, err := mgr.RequestOutputStop(owner, session, "")
	require.NoError(t, err)
	require.NotNil(t, run)
	require.Equal(t, "evt-real", ack.RunID, "虚拟项应穿透，落到 runBySX 兜底")
}

// 工具栏"当前任务"同样镜像优先：有排队时显示 running 的 run，而非最新注册的。
func TestLookupActiveRunBySessionOwner_PrefersMirrorRunning(t *testing.T) {
	mgr := setupMirrorTest(t)
	const (
		owner   = int64(400)
		agent   = int64(300)
		session = "sess-toolbar"
	)
	registerRun(mgr, "evt-running", session, owner, agent, 1)
	registerRun(mgr, "evt-q1", session, owner, agent, 2)
	storeMirror(t, owner, agent, session, []string{"evt-running"}, []string{"evt-q1"})

	snap := mgr.LookupActiveRunBySessionOwner(owner, session)
	require.NotNil(t, snap)
	require.Equal(t, "evt-running", snap.EventID)

	// 无镜像时回到旧行为（最新注册）。
	require.NoError(t, store.RDB.Del(context.Background(), queueSnapshotMirrorKey(owner, session)).Err())
	snap = mgr.LookupActiveRunBySessionOwner(owner, session)
	require.NotNil(t, snap)
	require.Equal(t, "evt-q1", snap.EventID)
}

// 跨节点：镜像 running 的 run 不在本节点内存，但 durable 记录存在 → 用 durable 只读快照。
func TestResolveRunFromQueueMirror_CrossNodeDurable(t *testing.T) {
	mgr := setupMirrorTest(t)
	const (
		owner   = int64(400)
		agent   = int64(300)
		session = "sess-crossnode"
	)
	evt := DelegateEventPayload{
		EventID: "evt-remote", AgentID: agent, OwnerID: owner, SenderID: owner,
		SessionID: session, SessionType: 1, MsgID: 9,
	}
	require.True(t, persistDurablePendingDelegate(context.Background(), durablePendingDelegateRecord{Event: evt}))
	storeMirror(t, owner, agent, session, []string{"evt-remote"}, nil)

	snap := mgr.resolveRunFromQueueMirror(owner, session)
	require.NotNil(t, snap)
	require.Equal(t, "evt-remote", snap.EventID)
}

// 空快照（running+queued 均空）删除镜像。
func TestStoreQueueSnapshotMirror_EmptySnapshotDeletes(t *testing.T) {
	setupMirrorTest(t)
	const (
		owner   = int64(400)
		agent   = int64(300)
		session = "sess-empty"
	)
	storeMirror(t, owner, agent, session, []string{"evt-a"}, nil)
	require.NotNil(t, loadQueueSnapshotMirror(context.Background(), owner, session))

	storeMirror(t, owner, agent, session, nil, nil)
	require.Nil(t, loadQueueSnapshotMirror(context.Background(), owner, session), "空快照应删除镜像")
}

func TestEmptyQueueSnapshotClearsComposing(t *testing.T) {
	mgr := setupMirrorTest(t)
	const (
		owner   = int64(400)
		agent   = int64(300)
		session = "sess-empty-clears-composing"
	)
	var calls []protocol.SessionActivitySetPayload
	mgr.SetSessionActivityHandler(func(_ context.Context, gotAgentID int64, gotOwnerID int64, payload protocol.SessionActivitySetPayload) error {
		require.Equal(t, agent, gotAgentID)
		require.Equal(t, owner, gotOwnerID)
		calls = append(calls, payload)
		return nil
	})

	storeMirror(t, owner, agent, session, []string{"evt-a"}, nil)
	raw, err := json.Marshal(map[string]any{
		"session_id": session,
		"running":    []string{},
		"queued":     []any{},
	})
	require.NoError(t, err)
	idleSession, empty := storeQueueSnapshotMirror(context.Background(), owner, agent, raw)
	require.True(t, empty)
	require.Equal(t, session, idleSession)
	require.True(t, IsSessionQueueIdle(context.Background(), owner, session), "empty snapshot should mark queue idle")

	mgr.clearComposingForEmptyQueueSnapshot(&agentConn{agentID: agent, ownerID: owner}, idleSession)
	require.Len(t, calls, 1)
	require.Equal(t, protocol.SessionActivityKindComposing, calls[0].Kind)
	require.False(t, calls[0].Active)
	require.Equal(t, session, calls[0].SessionID)
}

func TestStoreQueueSnapshotMirror_NonEmptyClearsQueueIdle(t *testing.T) {
	setupMirrorTest(t)
	const (
		owner   = int64(400)
		agent   = int64(300)
		session = "sess-idle-clear"
	)
	storeMirror(t, owner, agent, session, nil, nil)
	require.True(t, IsSessionQueueIdle(context.Background(), owner, session))

	storeMirror(t, owner, agent, session, []string{"evt-run"}, nil)
	require.False(t, IsSessionQueueIdle(context.Background(), owner, session), "non-empty snapshot should clear idle")
}

func TestRegisterActiveRunClearsQueueIdle(t *testing.T) {
	mgr := setupMirrorTest(t)
	const (
		owner   = int64(400)
		agent   = int64(300)
		session = "sess-idle-new-run"
	)
	storeMirror(t, owner, agent, session, nil, nil)
	require.True(t, IsSessionQueueIdle(context.Background(), owner, session))

	registerRun(mgr, "evt-new", session, owner, agent, 1)
	require.False(t, IsSessionQueueIdle(context.Background(), owner, session), "new active run should clear idle")
}

// 通用事件列表格式（items/events/queue）也必须正确落镜像，与前端解析对齐。
func TestStoreQueueSnapshotMirror_GenericEventsFormat(t *testing.T) {
	mgr := setupMirrorTest(t)
	const (
		owner   = int64(400)
		agent   = int64(300)
		session = "sess-generic"
	)
	registerRun(mgr, "evt-running", session, owner, agent, 1)

	raw, err := json.Marshal(map[string]any{
		"session_id": session,
		"events": []map[string]any{
			{"event_id": "evt-running", "state": "running"},
			{"event_id": "evt-q1", "state": "queued"},
			{"event_id": "evt-done", "state": "completed"},
		},
	})
	require.NoError(t, err)
	storeQueueSnapshotMirror(context.Background(), owner, agent, raw)

	mirror := loadQueueSnapshotMirror(context.Background(), owner, session)
	require.NotNil(t, mirror)
	require.Equal(t, []string{"evt-running"}, mirror.Running)
	require.Equal(t, []string{"evt-q1"}, mirror.Queued)

	// 镜像优先解析到 running 的真实 run。
	snap := mgr.LookupActiveRunBySessionOwner(owner, session)
	require.NotNil(t, snap)
	require.Equal(t, "evt-running", snap.EventID)
}

// items 字段同样支持，state 仅识别 running/queued，其余忽略。
func TestStoreQueueSnapshotMirror_ItemsFormat(t *testing.T) {
	setupMirrorTest(t)
	const (
		owner   = int64(400)
		agent   = int64(300)
		session = "sess-items"
	)
	raw, err := json.Marshal(map[string]any{
		"session_id": session,
		"items": []map[string]any{
			{"event_id": "evt-run", "state": "running"},
			{"event_id": "evt-q", "state": "queued"},
			{"event_id": "evt-canceled", "state": "canceled"},
		},
	})
	require.NoError(t, err)
	storeQueueSnapshotMirror(context.Background(), owner, agent, raw)

	mirror := loadQueueSnapshotMirror(context.Background(), owner, session)
	require.NotNil(t, mirror)
	require.Equal(t, []string{"evt-run"}, mirror.Running)
	require.Equal(t, []string{"evt-q"}, mirror.Queued)
}

// queue 字段同样支持，与 items/events 对称。
func TestStoreQueueSnapshotMirror_QueueFormat(t *testing.T) {
	setupMirrorTest(t)
	const (
		owner   = int64(400)
		agent   = int64(300)
		session = "sess-queue"
	)
	raw, err := json.Marshal(map[string]any{
		"session_id": session,
		"queue": []map[string]any{
			{"event_id": "evt-run", "state": "running"},
			{"event_id": "evt-q", "state": "queued"},
		},
	})
	require.NoError(t, err)
	storeQueueSnapshotMirror(context.Background(), owner, agent, raw)

	mirror := loadQueueSnapshotMirror(context.Background(), owner, session)
	require.NotNil(t, mirror)
	require.Equal(t, []string{"evt-run"}, mirror.Running)
	require.Equal(t, []string{"evt-q"}, mirror.Queued)
}
