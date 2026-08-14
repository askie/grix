package agentapi

import (
	"testing"
	"time"

	"github.com/askie/grix/backend/internal/pkg/testutil"
	"github.com/askie/grix/backend/internal/store"
	"github.com/askie/grix/backend/internal/ws/protocol"
	"github.com/stretchr/testify/require"
)

func TestAgentStatusesUseGlobalMonotonicRevisionWithinDispatchGeneration(t *testing.T) {
	previousRDB := store.RDB
	store.RDB = testutil.NewMockRedis()
	t.Cleanup(func() {
		_ = store.RDB.Close()
		store.RDB = previousRDB
	})

	manager := NewManager("", time.Second, nil, nil, nil, nil)
	defer manager.Shutdown()
	outputs := make([]protocol.AgentOutputStatusPayload, 0, 3)
	deliveries := make([]protocol.AgentDeliveryStatusPayload, 0, 2)
	manager.SetOutputStatusHandler(func(payload protocol.AgentOutputStatusPayload) {
		outputs = append(outputs, payload)
	})
	manager.SetDeliveryStatusHandler(func(payload protocol.AgentDeliveryStatusPayload) {
		deliveries = append(deliveries, payload)
	})

	event := durableLifecycleEvent("status-revision", 6101, 6201)
	manager.registerActiveRunInternal(event, false, time.Now().UTC(), false, 17)
	stopping := manager.updateRunState(event.EventID, func(run *activeAgentRun) bool {
		run.StopRequested = true
		run.State = protocol.AgentOutputStateStopping
		run.CanStop = false
		return true
	})
	manager.emitOutputStatus(stopping)
	manager.MarkRunStopFailed(event.EventID, "connector_stop_failed")

	require.Len(t, outputs, 3)
	for index, output := range outputs {
		require.Equal(t, int64(17), output.DispatchGeneration)
		require.Positive(t, output.Revision)
		if index > 0 {
			require.Greater(t, output.Revision, outputs[index-1].Revision)
		}
	}
	require.Equal(t, protocol.AgentOutputStateStopping, outputs[1].State)
	require.Equal(t, protocol.AgentOutputStateReceived, outputs[2].State)

	manager.emitDeliveryStatus(protocol.AgentDeliveryStatusPayload{
		EventID:            event.EventID,
		SessionID:          event.SessionID,
		DispatchGeneration: 17,
		Status:             protocol.AgentDeliveryStatusQueued,
	})
	manager.emitDeliveryStatus(protocol.AgentDeliveryStatusPayload{
		EventID:            event.EventID,
		SessionID:          event.SessionID,
		DispatchGeneration: 17,
		Status:             protocol.AgentDeliveryStatusReceived,
	})
	require.Len(t, deliveries, 2)
	require.Equal(t, int64(17), deliveries[0].DispatchGeneration)
	require.Equal(t, int64(17), deliveries[1].DispatchGeneration)
	require.Greater(t, deliveries[1].Revision, deliveries[0].Revision)
}
