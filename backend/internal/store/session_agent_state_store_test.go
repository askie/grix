package store_test

import (
	"testing"
	"time"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/testutil"
	"github.com/askie/grix/backend/internal/store"
	"github.com/stretchr/testify/require"
)

func setupSessionAgentStateStaleTest(t *testing.T) {
	t.Helper()
	testDB := testutil.NewTestDB()
	t.Cleanup(testDB.Close)
	previousDB := store.DB
	store.DB = testDB.DB
	t.Cleanup(func() { store.DB = previousDB })
}

// backdateChatStateUpdatedAt 直接把行的 updated_at 改到指定时间，模拟长期无人触碰。
func backdateChatStateUpdatedAt(t *testing.T, sessionID string, ownerID int64, updatedAt time.Time) {
	t.Helper()
	require.NoError(t, store.DB.Model(&model.SessionAgentState{}).
		Where("session_id = ? AND owner_id = ?", sessionID, ownerID).
		Update("updated_at", updatedAt.UTC()).Error)
}

func loadChatStateRow(t *testing.T, sessionID string, ownerID int64) model.SessionAgentState {
	t.Helper()
	var row model.SessionAgentState
	require.NoError(t, store.DB.First(&row, "session_id = ? AND owner_id = ?", sessionID, ownerID).Error)
	return row
}

// 扫描只捞出"running 且 updated_at 早于 cutoff"的行：新鲜 running、
// 非 running 的行都必须排除。
func TestListStaleRunningSessionAgentStates(t *testing.T) {
	setupSessionAgentStateStaleTest(t)

	now := time.Now()
	stale := now.Add(-3 * time.Hour)

	store.UpsertSessionAgentStateRunning("sess-stale", 100, 200, "run-stale", stale)
	backdateChatStateUpdatedAt(t, "sess-stale", 100, stale)

	store.UpsertSessionAgentStateRunning("sess-fresh", 100, 200, "run-fresh", now)

	store.UpsertSessionAgentStateRunning("sess-idle", 100, 200, "run-idle", stale)
	backdateChatStateUpdatedAt(t, "sess-idle", 100, stale)
	changed, err := store.SettleSessionAgentStateByRun(model.SessionAgentState{
		SessionID: "sess-idle",
		OwnerID:   100,
		AgentID:   200,
		State:     model.SessionAgentStateCompleted,
		LastRunID: "run-idle",
	})
	require.NoError(t, err)
	require.True(t, changed)
	// settle 会把 updated_at 刷成 now，再拨回过去：即便是陈旧行，非 running 也不能捞出。
	backdateChatStateUpdatedAt(t, "sess-idle", 100, stale)

	rows, err := store.ListStaleRunningSessionAgentStates(now.Add(-2*time.Hour), 100)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, "sess-stale", rows[0].SessionID)
	require.Equal(t, "run-stale", rows[0].LastRunID)
	require.Equal(t, model.SessionAgentStateRunning, rows[0].State)
}

// 僵尸行被结算为 idle 且 stop_reason 标明来源；对已正常终态化、或已被新 run
// 接管的行，同款守卫必须让它成为 no-op。
func TestSettleStaleRunningSessionAgentState(t *testing.T) {
	setupSessionAgentStateStaleTest(t)

	stale := time.Now().Add(-3 * time.Hour)
	store.UpsertSessionAgentStateRunning("sess-zombie", 100, 200, "run-zombie", stale)
	backdateChatStateUpdatedAt(t, "sess-zombie", 100, stale)
	scanned := loadChatStateRow(t, "sess-zombie", 100)

	changed, err := store.SettleStaleRunningSessionAgentState(scanned, "stale_running_reaped")
	require.NoError(t, err)
	require.True(t, changed)

	row := loadChatStateRow(t, "sess-zombie", 100)
	require.Equal(t, model.SessionAgentStateIdle, row.State)
	require.Equal(t, "stale_running_reaped", row.StopReason)
	require.NotNil(t, row.CompletedAt)

	// 重复结算同一快照：行已终态化，守卫拒绝覆盖。
	changed, err = store.SettleStaleRunningSessionAgentState(scanned, "stale_running_reaped")
	require.NoError(t, err)
	require.False(t, changed)
	row = loadChatStateRow(t, "sess-zombie", 100)
	require.Equal(t, model.SessionAgentStateIdle, row.State)
}

// 扫描后、结算前行被新 run 接管（last_run_id 变了）：旧快照的结算不得生效。
func TestSettleStaleRunningSessionAgentStateSkipsNewerRun(t *testing.T) {
	setupSessionAgentStateStaleTest(t)

	stale := time.Now().Add(-3 * time.Hour)
	store.UpsertSessionAgentStateRunningWithGeneration("sess-taken", 100, 200, "run-old", stale, 1)
	backdateChatStateUpdatedAt(t, "sess-taken", 100, stale)
	scanned := loadChatStateRow(t, "sess-taken", 100)

	// 新 run 以更高 generation 接管该行。
	store.UpsertSessionAgentStateRunningWithGeneration("sess-taken", 100, 200, "run-new", time.Now(), 2)

	changed, err := store.SettleStaleRunningSessionAgentState(scanned, "stale_running_reaped")
	require.NoError(t, err)
	require.False(t, changed)

	row := loadChatStateRow(t, "sess-taken", 100)
	require.Equal(t, model.SessionAgentStateRunning, row.State)
	require.Equal(t, "run-new", row.LastRunID)
}
