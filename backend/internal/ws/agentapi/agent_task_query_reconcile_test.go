package agentapi

import (
	"testing"
	"time"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/testutil"
	"github.com/askie/grix/backend/internal/store"
	"github.com/stretchr/testify/require"
)

func setupAgentTaskQueryReconcileTest(t *testing.T) func() {
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

// Missing ephemeral tracking is not evidence of success. A connector can keep
// running after a backend restart or after bounded tracking retention expires,
// so the persisted state must remain non-terminal until event_result arrives.
func TestReconcileSessionStatePreservesUntrackedRunning(t *testing.T) {
	cleanup := setupAgentTaskQueryReconcileTest(t)
	defer cleanup()
	mgr := NewManager("", 30*time.Second, nil, nil, nil, nil)
	defer mgr.Shutdown()

	store.UpsertSessionAgentStateRunning("sess-leak", 100, 200, "run-leak", time.Now().Add(-time.Hour))

	row := model.SessionAgentState{SessionID: "sess-leak", OwnerID: 100, AgentID: 200, State: model.SessionAgentStateRunning}
	got := mgr.reconcileSessionState(row)
	require.Equal(t, model.SessionAgentStateRunning, got.State, "untracked running must not be terminalized without connector evidence")

	rows, _, err := store.ListSessionAgentStatesByOwner(100, "", "", 1, 10)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, model.SessionAgentStateRunning, rows[0].State, "persisted row must remain non-terminal")
}

// A run still alive in the in-memory tracker must NEVER be reconciled away —
// this guards against the self-heal killing active tasks.
func TestReconcileSessionStateKeepsAliveRunning(t *testing.T) {
	cleanup := setupAgentTaskQueryReconcileTest(t)
	defer cleanup()
	mgr := NewManager("", 30*time.Second, nil, nil, nil, nil)
	defer mgr.Shutdown()

	mgr.registerActiveRun(DelegateEventPayload{
		EventID:   "run-alive",
		SessionID: "sess-alive",
		OwnerID:   100,
		AgentID:   200,
		SenderID:  100,
		MsgID:     1,
	})

	row := model.SessionAgentState{SessionID: "sess-alive", OwnerID: 100, AgentID: 200, State: model.SessionAgentStateRunning}
	got := mgr.reconcileSessionState(row)
	require.Equal(t, model.SessionAgentStateRunning, got.State, "alive running must be preserved")
}

// Terminal rows pass through untouched.
func TestReconcileSessionStateIgnoresTerminal(t *testing.T) {
	cleanup := setupAgentTaskQueryReconcileTest(t)
	defer cleanup()
	mgr := NewManager("", 30*time.Second, nil, nil, nil, nil)
	defer mgr.Shutdown()

	for _, st := range []string{
		model.SessionAgentStateCompleted,
		model.SessionAgentStateFailed,
		model.SessionAgentStateIdle,
	} {
		row := model.SessionAgentState{SessionID: "s", OwnerID: 1, AgentID: 2, State: st}
		require.Equal(t, st, mgr.reconcileSessionState(row).State)
	}
}
