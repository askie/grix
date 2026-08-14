package store_test

import (
	"testing"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/testutil"
	"github.com/askie/grix/backend/internal/store"
	"github.com/stretchr/testify/require"
)

func TestTerminalCommitTokenMatchingIsCaseSensitive(t *testing.T) {
	testDB := testutil.NewTestDB()
	defer testDB.Close()
	previousDB := store.DB
	store.DB = testDB.DB
	t.Cleanup(func() { store.DB = previousDB })

	seed := model.AgentEventTerminalLedger{
		EventID:             "evt-case-sensitive-token",
		OwnerID:             101,
		AgentID:             202,
		TerminalCommitToken: "CaseSensitiveToken",
		DelegateEvent:       []byte(`{"event_id":"evt-case-sensitive-token"}`),
		DispatchGeneration:  1,
	}
	disposition, _, created, err := store.SeedAgentEventDispatchLedger(seed)
	require.NoError(t, err)
	require.Equal(t, store.AgentTerminalLedgerCreated, disposition)
	require.True(t, created)

	disposition, _, err = store.ResolveAgentEventTerminalLedger(
		seed.EventID,
		seed.OwnerID,
		seed.AgentID,
		"failed",
		"test_failure",
		"",
		"casesensitivetoken",
	)
	require.NoError(t, err)
	require.Equal(t, store.AgentTerminalLedgerForeign, disposition)

	conflictingSeed := seed
	conflictingSeed.TerminalCommitToken = "casesensitivetoken"
	disposition, _, created, err = store.SeedAgentEventDispatchLedger(conflictingSeed)
	require.NoError(t, err)
	require.Equal(t, store.AgentTerminalLedgerForeign, disposition)
	require.False(t, created)

	conflictingCommit := seed
	conflictingCommit.TerminalCommitToken = "casesensitivetoken"
	conflictingCommit.Status = "failed"
	conflictingCommit.Code = "test_failure"
	disposition, _, _, err = store.CommitAgentEventTerminalLedger(conflictingCommit, nil)
	require.NoError(t, err)
	require.Equal(t, store.AgentTerminalLedgerForeign, disposition)
}

// record_only 镜像行永远停在 status=''，绝不能被栅栏当成"更新的 pending
// dispatch"——否则任何遗留镜像行都会永久卡住 composing 终态清理与 stale
// run 清扫。真实派发行（record_only=false）仍然参与判定。
func TestHasNewerPendingAgentEventDispatchIgnoresRecordOnlyMirror(t *testing.T) {
	testDB := testutil.NewTestDB()
	defer testDB.Close()
	previousDB := store.DB
	store.DB = testDB.DB
	t.Cleanup(func() { store.DB = previousDB })

	const (
		sessionID = "sess-mirror-fence"
		ownerID   = int64(101)
		agentID   = int64(202)
	)
	baseEventID := "evt-mirror-fence-base"

	seedRow := func(eventID string, generation int64, status string, recordOnly bool) {
		disposition, _, created, err := store.SeedAgentEventDispatchLedger(
			model.AgentEventTerminalLedger{
				EventID:            eventID,
				OwnerID:            ownerID,
				AgentID:            agentID,
				SessionID:          sessionID,
				Status:             status,
				RecordOnly:         recordOnly,
				DelegateEvent:      []byte(`{"event_id":"` + eventID + `"}`),
				DispatchGeneration: generation,
			},
		)
		require.NoError(t, err)
		require.True(t, created)
		require.Equal(t, store.AgentTerminalLedgerCreated, disposition)
	}

	// 目标 run 已到终态（generation 4）。
	seedRow(baseEventID, 4, "responded", false)

	// 只有更新的 record_only 镜像 pending 行：栅栏必须放行。
	seedRow(baseEventID+":mirror", 5, "", true)
	newer, err := store.HasNewerPendingAgentEventDispatch(baseEventID, sessionID, ownerID, agentID)
	require.NoError(t, err)
	require.False(t, newer, "record_only 镜像行不得阻断栅栏")

	// 存在更新的真实 pending 派发行：栅栏必须拦住。
	seedRow("evt-mirror-fence-newer-real", 6, "", false)
	newer, err = store.HasNewerPendingAgentEventDispatch(baseEventID, sessionID, ownerID, agentID)
	require.NoError(t, err)
	require.True(t, newer, "真实 pending 派发行必须阻断栅栏")
}
