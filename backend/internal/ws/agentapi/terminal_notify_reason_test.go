package agentapi

import (
	"testing"

	"github.com/askie/grix/backend/internal/ws/protocol"
	"github.com/stretchr/testify/assert"
)

// The terminal path derives push decisions from the stop-reason code alone.
// Folding payload.Msg in (as run.StopReason still does for storage) turned the
// reason into free text that matches no guard, which is how a user-pressed stop
// produced an "unexpected stop" push and how the stale-failure window stopped
// applying to connector results.
func TestTerminalNotifyReason(t *testing.T) {
	cases := []struct {
		name    string
		payload EventResultPayload
		want    string
	}{
		{
			name:    "explicit code wins",
			payload: EventResultPayload{Status: "failed", Code: protocol.AgentDeliveryCodeEventStale, Msg: "event expired"},
			want:    protocol.AgentDeliveryCodeEventStale,
		},
		{
			// The connector's own cancel path: sendCanceledPendingEventResult
			// sends ("canceled", msg) with no code, msg being "canceled",
			// "revoked" or "stopped by user".
			name:    "code-less cancel maps to the canceled code",
			payload: EventResultPayload{Status: protocol.AgentEventResultCanceled, Msg: "canceled"},
			want:    protocol.AgentDeliveryCodeCanceled,
		},
		{
			name:    "code-less cancel with a free-text msg still maps to canceled",
			payload: EventResultPayload{Status: protocol.AgentEventResultCanceled, Msg: "stopped by user"},
			want:    protocol.AgentDeliveryCodeCanceled,
		},
		{
			// The common shape: most bridge.ts call sites send only a message.
			name:    "code-less failure falls back to the processing_failed code",
			payload: EventResultPayload{Status: "failed", Msg: "Hermes finished without producing a reply"},
			want:    protocol.AgentDeliveryCodeProcessingFailed,
		},
		{
			name:    "blank payload falls back to the processing_failed code",
			payload: EventResultPayload{Status: "failed"},
			want:    protocol.AgentDeliveryCodeProcessingFailed,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, terminalNotifyReason(tc.payload))
		})
	}
}

// Regression lock for the two guards the free-text reason bypassed.
func TestTerminalNotifyReasonFeedsSuppressionGuards(t *testing.T) {
	canceled := EventResultPayload{Status: protocol.AgentEventResultCanceled, Msg: "canceled"}
	assert.True(t, isUserInitiatedStopReason(terminalNotifyReason(canceled)),
		"a user-pressed stop must never produce an unexpected-stop push")
	assert.False(t, isUserInitiatedStopReason(canceled.Msg),
		"guarding on the raw msg is exactly the bug this replaces")

	codeless := EventResultPayload{Status: "failed", Msg: "agent shutting down"}
	_, deferred := deferredCleanupNotifyReasons[terminalNotifyReason(codeless)]
	assert.True(t, deferred,
		"a code-less failure must reach the stale-failure window, not skip it")
}

// The free-text message is not discarded — it becomes the push detail.
func TestTaskFailedDetail(t *testing.T) {
	assert.Equal(t, "", taskFailedDetail("   "))
	assert.Equal(t, "agent shutting down", taskFailedDetail("  agent shutting down  "))
	assert.Len(t, []rune(taskFailedDetail(string(make([]rune, 200)))), 80)
}
