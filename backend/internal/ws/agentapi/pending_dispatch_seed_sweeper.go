package agentapi

import (
	"context"
	"time"

	"github.com/askie/grix/backend/internal/pkg/logger"
	"github.com/askie/grix/backend/internal/store"
)

const expiredPendingDispatchSweepBatch = 200

// sweepExpiredPendingDispatchSeeds removes DB recovery seeds only after their
// full tracking retention elapsed and Redis confirms that no live coordination
// record remains. It runs at startup and with the existing stale-run sweep, so
// a backend restart cannot make an old status=” seed authoritative again.
func (m *Manager) sweepExpiredPendingDispatchSeeds() {
	if m == nil || store.DB == nil || store.RDB == nil {
		return
	}
	cutoff := time.Now().Add(-m.pendingTrackingRetention())
	rows, err := store.ListExpiredPendingAgentEventDispatches(
		cutoff, expiredPendingDispatchSweepBatch,
	)
	if err != nil {
		logger.L.Warnf("pending dispatch seed sweep: list candidates failed err=%v", err)
		return
	}
	ctx := context.Background()
	for i := range rows {
		row := &rows[i]
		exists, existsErr := store.RDB.Exists(
			ctx, durablePendingDelegateRecordKey(row.EventID),
		).Result()
		if existsErr != nil {
			logger.L.Warnf(
				"pending dispatch seed sweep: check durable record failed event=%s err=%v",
				row.EventID, existsErr,
			)
			continue
		}
		if exists > 0 {
			continue
		}
		if _, deleteErr := store.DeleteAgentEventDispatchSeedIfPending(
			row.EventID,
			row.OwnerID,
			row.AgentID,
			row.DispatchGeneration,
		); deleteErr != nil {
			logger.L.Warnf(
				"pending dispatch seed sweep: delete candidate failed event=%s err=%v",
				row.EventID, deleteErr,
			)
		}
	}
}
