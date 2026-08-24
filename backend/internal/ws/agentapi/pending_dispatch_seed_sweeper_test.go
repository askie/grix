package agentapi

import (
	"context"
	"testing"
	"time"

	"github.com/askie/grix/backend/internal/store"
	"github.com/stretchr/testify/require"
)

func TestPendingDispatchSeedSweeperRetiresOnlyRedisExpiredRows(t *testing.T) {
	installStructuredOutputTestStores(t)
	oldMissing := DelegateEventPayload{
		EventID: "evt-old-missing-redis", AgentID: 7701, OwnerID: 7801,
		SessionID: "sess-old-missing-redis",
	}
	oldLive := DelegateEventPayload{
		EventID: "evt-old-live-redis", AgentID: 7702, OwnerID: 7802,
		SessionID: "sess-old-live-redis",
	}
	seedExpiredOutputLedger(t, oldMissing, 1)
	seedExpiredOutputLedger(t, oldLive, 2)
	require.NoError(t, store.RDB.Set(
		context.Background(),
		durablePendingDelegateRecordKey(oldLive.EventID),
		`{"stage":"ack"}`,
		time.Hour,
	).Err())

	manager := NewManager("", time.Second, nil, nil, nil, nil)
	defer manager.Shutdown()
	manager.pendingTrackingTTL = time.Hour
	manager.sweepExpiredPendingDispatchSeeds()

	missingLedger, err := store.LoadAgentEventTerminalLedger(oldMissing.EventID)
	require.NoError(t, err)
	require.Nil(t, missingLedger)
	liveLedger, err := store.LoadAgentEventTerminalLedger(oldLive.EventID)
	require.NoError(t, err)
	require.NotNil(t, liveLedger)
}
