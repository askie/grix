package store_test

import (
	"testing"
	"time"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/logger"
	"github.com/askie/grix/backend/internal/store"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// 守卫跳过写入时会打一条 Warn；store 包的测试没有全局 logger，补一个空实现。
func requireLogger(t *testing.T) {
	t.Helper()
	if logger.L == nil {
		logger.L = zap.NewNop().Sugar()
		t.Cleanup(func() { logger.L = nil })
	}
}

func terminalState(sessionID string, ownerID, agentID int64, runID, state string) model.SessionAgentState {
	completedAt := time.Now().UTC()
	return model.SessionAgentState{
		SessionID:   sessionID,
		OwnerID:     ownerID,
		AgentID:     agentID,
		State:       state,
		LastRunID:   runID,
		CompletedAt: &completedAt,
	}
}

// 陌生 run 的终态不得埋掉还活着的 run：行归 run-a 且仍在 running/waiting_* 时，
// 带着 run-b 的终态必须整条跳过。
func TestUpsertSessionAgentStateTerminalSkipsForeignRun(t *testing.T) {
	for _, liveState := range []string{
		model.SessionAgentStateRunning,
		model.SessionAgentStateWaitingQuestion,
		model.SessionAgentStateWaitingApproval,
	} {
		t.Run(liveState, func(t *testing.T) {
			requireLogger(t)
			setupSessionAgentStateStaleTest(t)
			sessionID := "sess-foreign-" + liveState

			store.UpsertSessionAgentStateRunning(sessionID, 100, 200, "run-a", time.Now())
			if liveState != model.SessionAgentStateRunning {
				store.SetSessionAgentStateWaiting(sessionID, 100, liveState)
			}

			store.UpsertSessionAgentStateTerminal(
				terminalState(sessionID, 100, 200, "run-b", model.SessionAgentStateCompleted),
			)

			row := loadChatStateRow(t, sessionID, 100)
			require.Equal(t, liveState, row.State, "活着的 run 不能被陌生 run 的终态覆写")
			require.Equal(t, "run-a", row.LastRunID)
			require.Nil(t, row.CompletedAt)
		})
	}
}

// 守卫只挡陌生 run：同 run 的终态、终态行上的接续终态、以及首次创建都要照常落库。
func TestUpsertSessionAgentStateTerminalAllowsOwnRunAndFirstInsert(t *testing.T) {
	requireLogger(t)
	setupSessionAgentStateStaleTest(t)

	// 首次创建：行不存在，终态直接插入。
	store.UpsertSessionAgentStateTerminal(
		terminalState("sess-first", 100, 200, "run-a", model.SessionAgentStateCompleted),
	)
	first := loadChatStateRow(t, "sess-first", 100)
	require.Equal(t, model.SessionAgentStateCompleted, first.State)
	require.Equal(t, "run-a", first.LastRunID)

	// 同 run 终态：正常覆写自己的行。
	store.UpsertSessionAgentStateRunning("sess-own", 100, 200, "run-a", time.Now())
	store.UpsertSessionAgentStateTerminal(
		terminalState("sess-own", 100, 200, "run-a", model.SessionAgentStateCompleted),
	)
	own := loadChatStateRow(t, "sess-own", 100)
	require.Equal(t, model.SessionAgentStateCompleted, own.State)
	require.NotNil(t, own.CompletedAt)

	// 行已终态：不再有活着的 run 要保护，后到的其他 run 终态照常落库。
	store.UpsertSessionAgentStateTerminal(
		terminalState("sess-own", 100, 200, "run-b", model.SessionAgentStateFailed),
	)
	settled := loadChatStateRow(t, "sess-own", 100)
	require.Equal(t, model.SessionAgentStateFailed, settled.State)
	require.Equal(t, "run-b", settled.LastRunID)
}

// SetSessionAgentStateRunningFromWaiting 是 SetSessionAgentStateWaiting 的逆操作：
// 只拨动等待中的行，running 行与终态行都不动，也不插入新行。
func TestSetSessionAgentStateRunningFromWaiting(t *testing.T) {
	requireLogger(t)
	setupSessionAgentStateStaleTest(t)

	store.UpsertSessionAgentStateRunning("sess-waiting", 100, 200, "run-a", time.Now())
	store.SetSessionAgentStateWaiting("sess-waiting", 100, model.SessionAgentStateWaitingQuestion)
	require.True(t, store.SetSessionAgentStateRunningFromWaiting("sess-waiting", 100))
	row := loadChatStateRow(t, "sess-waiting", 100)
	require.Equal(t, model.SessionAgentStateRunning, row.State)
	require.Equal(t, "run-a", row.LastRunID)

	// running 行：无事可做。
	require.False(t, store.SetSessionAgentStateRunningFromWaiting("sess-waiting", 100))

	// 终态行绝不复活。
	store.UpsertSessionAgentStateTerminal(
		terminalState("sess-done", 100, 200, "run-a", model.SessionAgentStateCompleted),
	)
	require.False(t, store.SetSessionAgentStateRunningFromWaiting("sess-done", 100))
	require.Equal(t, model.SessionAgentStateCompleted, loadChatStateRow(t, "sess-done", 100).State)

	// 不存在的行不会被凭空插入。
	require.False(t, store.SetSessionAgentStateRunningFromWaiting("sess-missing", 100))
	var count int64
	require.NoError(t, store.DB.Model(&model.SessionAgentState{}).
		Where("session_id = ?", "sess-missing").Count(&count).Error)
	require.Zero(t, count)
}
