package ws

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/askie/grix/backend/internal/agenttoolbar/core"
	"github.com/askie/grix/backend/internal/ws/protocol"
	"github.com/stretchr/testify/require"
)

func TestBuildComposingStopPayloadUsesSessionScopeWithoutEventID(t *testing.T) {
	now := time.Unix(1_724_000_000, 123_000_000)
	payload := buildComposingStopPayload(core.StopOutputRequest{
		AgentID:   200,
		OwnerID:   100,
		SessionID: "session-composing",
	}, 200, now)

	require.Equal(t, protocol.AgentEventStopScopeSession, payload.Scope)
	require.Empty(t, payload.EventID)
	require.Equal(t, "session-composing", payload.SessionID)
	require.Equal(t, int64(100), payload.OwnerID)
	require.Equal(t, int64(200), payload.AgentID)
	require.Equal(t, "owner_requested_stop", payload.Reason)
	require.Equal(t, now.UnixMilli(), payload.RequestedAt)
	require.NotEmpty(t, payload.StopID)

	raw, err := json.Marshal(payload)
	require.NoError(t, err)
	require.NotContains(t, string(raw), `"event_id"`)
}
