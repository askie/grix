package e2e

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestLiveAdapterPIRoundtrip verifies the outbound E2E path through a PI agent
// connected via grix-connector. Assumes grix-connector is already running with
// a PI adapter connected to the backend.
//
// Prerequisites:
//   - AIBot backend running at GRIX_ADAPTER_E2E_API_BASE (default http://127.0.0.1:27180/v1)
//   - grix-connector running with PI adapter connected to the backend
//
// Enable with: GRIX_ADAPTER_E2E=1
func TestLiveAdapterPIRoundtrip(t *testing.T) {
	cfg := loadAdapterTestConfig(t)
	if !cfg.Enabled {
		t.Skip("set GRIX_ADAPTER_E2E=1 to enable adapter outbound E2E")
	}
	require.NotEmpty(t, cfg.PIAgentID, "GRIX_ADAPTER_PI_AGENT_ID is required")

	harness := newLiveAgentHarness(t, cfg.liveConfig())
	totalTimeout := cfg.ConversationTimeout + 30*time.Second
	ctx, cancel := context.WithTimeout(context.Background(), totalTimeout)
	defer cancel()

	// 1. Log in as user.
	session := harness.bootstrapProjectIdentity(ctx)

	// 2. Verify the PI agent is online (assumes grix-connector already running).
	piAgent := harness.resolveOnlineAgentByClientType(ctx, session, "pi", "pi-agent-resolve")
	directSession := harness.openDirectSessionForAgent(ctx, session, piAgent)

	// 3. Connect as user via WebSocket.
	client := newLiveUserWSClient(t, harness, directSession)
	client.connect(ctx)
	defer client.close()

	// 4. Send a message and wait for PI's reply (or error/timeout).
	marker := fmt.Sprintf("ADAPTER_E2E_PI_%d", time.Now().UnixMilli())
	probe := client.runConversationProbe(ctx, liveConversationProbeOptions{
		Message:              "请严格只回复以下文字，不要有任何其他内容：" + marker,
		ExpectedSenderID:     directSession.AgentID,
		ExpectedTextContains: marker,
		Timeout:              cfg.ConversationTimeout,
	})

	harness.writeJSON("pi-outbound-roundtrip-summary.json", map[string]any{
		"generated_at":   time.Now().UTC().Format(time.RFC3339),
		"artifacts_dir":  harness.artifactDir,
		"marker":         marker,
		"session_id":     directSession.SessionID,
		"agent_id":       directSession.AgentID,
		"trigger_msg_id": probe.TriggerMsgID,
		"push_count":     len(probe.AgentPushes),
		"events_count":   len(probe.Events),
	})

	t.Logf("pi outbound roundtrip: marker=%s pushes=%d events=%d", marker, len(probe.AgentPushes), len(probe.Events))

	// Log all collected events for diagnostics regardless of success.
	for i, evt := range probe.Events {
		t.Logf("  event[%d]: %v", i, evt)
	}
}
