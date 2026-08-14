package agentapi

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/testutil"
	"github.com/askie/grix/backend/internal/store"
	"github.com/askie/grix/backend/internal/ws/protocol"
	"github.com/stretchr/testify/require"
)

func setupStaleRunSweeperTest(t *testing.T) func() {
	t.Helper()
	previousDB, previousRDB := store.DB, store.RDB
	testDB := testutil.NewTestDB()
	testRedis := testutil.NewMockRedis()
	store.DB, store.RDB = testDB.DB, testRedis
	return func() {
		_ = testRedis.Close()
		testDB.Close()
		store.DB, store.RDB = previousDB, previousRDB
	}
}

// seedStaleRunningRow 写入一行 running 并把 updated_at 拨到过去，模拟终态丢失的僵尸行。
func seedStaleRunningRow(t *testing.T, sessionID string, ownerID, agentID int64, runID string, updatedAt time.Time) {
	t.Helper()
	store.UpsertSessionAgentStateRunning(sessionID, ownerID, agentID, runID, updatedAt)
	require.NoError(t, store.DB.Model(&model.SessionAgentState{}).
		Where("session_id = ? AND owner_id = ?", sessionID, ownerID).
		Update("updated_at", updatedAt.UTC()).Error)
}

func mustLoadChatState(t *testing.T, sessionID string, ownerID int64) model.SessionAgentState {
	t.Helper()
	var row model.SessionAgentState
	require.NoError(t, store.DB.First(&row, "session_id = ? AND owner_id = ?", sessionID, ownerID).Error)
	return row
}

type outputStatusCapture struct {
	mu       sync.Mutex
	payloads []protocol.AgentOutputStatusPayload
}

func (c *outputStatusCapture) handler() OutputStatusHandler {
	return func(p protocol.AgentOutputStatusPayload) {
		c.mu.Lock()
		c.payloads = append(c.payloads, p)
		c.mu.Unlock()
	}
}

func (c *outputStatusCapture) all() []protocol.AgentOutputStatusPayload {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]protocol.AgentOutputStatusPayload, len(c.payloads))
	copy(out, c.payloads)
	return out
}

// 新鲜 running 行（未过阈值）不能被误收。
func TestSweepStaleRunningKeepsFreshRow(t *testing.T) {
	cleanup := setupStaleRunSweeperTest(t)
	defer cleanup()
	mgr := NewManager("", 30*time.Second, nil, nil, nil, nil)
	defer mgr.Shutdown()

	capture := &outputStatusCapture{}
	mgr.SetOutputStatusHandler(capture.handler())

	seedStaleRunningRow(t, "sess-fresh", 100, 200, "run-fresh", time.Now())
	mgr.sweepStaleRunningRuns()

	row := mustLoadChatState(t, "sess-fresh", 100)
	require.Equal(t, model.SessionAgentStateRunning, row.State, "fresh running row must not be reaped")
	require.Empty(t, capture.all(), "no terminal frame should be emitted")
}

// 超过阈值且无任何活跃证据的行被 settle 成 idle，stop_reason 标明 reaped，
// 并广播一帧 stopped 终态让前端清掉"正在输入"。
func TestSweepStaleRunningReapsZombieRow(t *testing.T) {
	cleanup := setupStaleRunSweeperTest(t)
	defer cleanup()
	mgr := NewManager("", 30*time.Second, nil, nil, nil, nil)
	defer mgr.Shutdown()

	capture := &outputStatusCapture{}
	mgr.SetOutputStatusHandler(capture.handler())

	seedStaleRunningRow(t, "sess-zombie", 100, 200, "run-zombie", time.Now().Add(-3*time.Hour))
	mgr.sweepStaleRunningRuns()

	row := mustLoadChatState(t, "sess-zombie", 100)
	require.Equal(t, model.SessionAgentStateIdle, row.State)
	require.Equal(t, staleRunningReapedStopReason, row.StopReason)
	require.NotNil(t, row.CompletedAt)

	emitted := capture.all()
	require.Len(t, emitted, 1)
	require.Equal(t, protocol.AgentOutputStateStopped, emitted[0].State)
	require.Equal(t, "run-zombie", emitted[0].RunID)
	require.Equal(t, "sess-zombie", emitted[0].SessionID)
	require.Equal(t, staleRunningReapedStopReason, emitted[0].StopReason)
}

// 本节点内存里还有该 session+owner 的活跃 run 时，即便行已超时也不收。
func TestSweepStaleRunningKeepsRowWithLocalActiveRun(t *testing.T) {
	cleanup := setupStaleRunSweeperTest(t)
	defer cleanup()
	mgr := NewManager("", 30*time.Second, nil, nil, nil, nil)
	defer mgr.Shutdown()

	// 走 registerActiveRunForDispatch：只登记内存 run，不触发后台 running 落库，
	// 避免异步 upsert 把手动拨回的 updated_at 又刷新掉。
	mgr.registerActiveRunForDispatch(DelegateEventPayload{
		EventID:   "run-local",
		SessionID: "sess-local",
		OwnerID:   100,
		AgentID:   200,
		SenderID:  100,
		MsgID:     1,
	}, time.Now().Add(-3*time.Hour), false)

	capture := &outputStatusCapture{}
	mgr.SetOutputStatusHandler(capture.handler())

	seedStaleRunningRow(t, "sess-local", 100, 200, "run-local", time.Now().Add(-3*time.Hour))
	mgr.sweepStaleRunningRuns()

	row := mustLoadChatState(t, "sess-local", 100)
	require.Equal(t, model.SessionAgentStateRunning, row.State, "row with a local active run must not be reaped")
	require.Empty(t, capture.all(), "no terminal frame should be emitted")
}

// durable 记录未过期（说明 run 可能在别处存活）时同样不收。
func TestSweepStaleRunningKeepsRowWithFreshDurableRecord(t *testing.T) {
	cleanup := setupStaleRunSweeperTest(t)
	defer cleanup()
	mgr := NewManager("", 30*time.Second, nil, nil, nil, nil)
	defer mgr.Shutdown()

	seedStaleRunningRow(t, "sess-durable", 100, 200, "run-durable", time.Now().Add(-3*time.Hour))
	ok := persistDurablePendingDelegate(context.Background(), durablePendingDelegateRecord{
		Event: DelegateEventPayload{
			EventID:   "run-durable",
			SessionID: "sess-durable",
			OwnerID:   100,
			AgentID:   200,
			SenderID:  100,
			MsgID:     1,
		},
		Stage:     durablePendingDelegateStageResult,
		UpdatedAt: time.Now().UnixMilli(),
	})
	require.True(t, ok)

	mgr.sweepStaleRunningRuns()

	row := mustLoadChatState(t, "sess-durable", 100)
	require.Equal(t, model.SessionAgentStateRunning, row.State, "row with a fresh durable record must not be reaped")
}

// durable 索引超过一个批次时，目标记录按 score 排在截断点之外但 UpdatedAt
// 新鲜：全量扫描必须仍能找到它，不能误收活 run（回归：ZRevRange 截断 128 条
// 的实现会漏掉该记录）。
func TestSweepStaleRunningKeepsRowWithDurableRecordBeyondFirstBatch(t *testing.T) {
	cleanup := setupStaleRunSweeperTest(t)
	defer cleanup()
	mgr := NewManager("", 30*time.Second, nil, nil, nil, nil)
	defer mgr.Shutdown()

	seedStaleRunningRow(t, "sess-deep", 100, 200, "run-deep", time.Now().Add(-3*time.Hour))
	ctx := context.Background()

	// 目标记录：score（Event.CreatedAt）取最小值，使其按分排序排在最后——
	// 截断式扫描（只取最新 128 条）一定漏掉它；但 UpdatedAt 是新鲜的。
	ok := persistDurablePendingDelegate(ctx, durablePendingDelegateRecord{
		Event: DelegateEventPayload{
			EventID:   "run-deep",
			SessionID: "sess-deep",
			OwnerID:   100,
			AgentID:   200,
			SenderID:  100,
			MsgID:     1,
			CreatedAt: 1,
		},
		Stage:     durablePendingDelegateStageResult,
		UpdatedAt: time.Now().UnixMilli(),
	})
	require.True(t, ok)

	// 用同一 agent 的其他会话填满索引，数量超过一个批次，score 都比目标大。
	fillerCount := durablePendingDelegateDrainBatch + 50
	for i := 0; i < fillerCount; i++ {
		ok := persistDurablePendingDelegate(ctx, durablePendingDelegateRecord{
			Event: DelegateEventPayload{
				EventID:   fmt.Sprintf("run-filler-%d", i),
				SessionID: fmt.Sprintf("sess-filler-%d", i),
				OwnerID:   100,
				AgentID:   200,
				SenderID:  100,
				MsgID:     1,
				CreatedAt: int64(1000 + i),
			},
			Stage:     durablePendingDelegateStageResult,
			UpdatedAt: time.Now().UnixMilli(),
		})
		require.True(t, ok)
	}

	mgr.sweepStaleRunningRuns()

	row := mustLoadChatState(t, "sess-deep", 100)
	require.Equal(t, model.SessionAgentStateRunning, row.State,
		"fresh durable record beyond the first index batch must still protect the row")
}
