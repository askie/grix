package e2e

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestLiveAdapterOpenClawRoundtrip verifies the outbound E2E path through an
// OpenClaw agent with the grix plugin. Unlike other adapter tests, this one
// does NOT start the adapter process — OpenClaw is a host runtime that loads
// the grix-openclaw plugin internally. The test assumes an OpenClaw process
// with the grix plugin is already running and connected to the backend.
//
// Prerequisites:
//   - AIBot backend running at GRIX_ADAPTER_E2E_API_BASE (default http://127.0.0.1:27180/v1)
//   - OpenClaw running with the grix plugin loaded and connected to the backend
//
// Enable with: GRIX_ADAPTER_E2E=1
func TestLiveAdapterOpenClawRoundtrip(t *testing.T) {
	cfg := loadAdapterTestConfig(t)
	if !cfg.Enabled {
		t.Skip("set GRIX_ADAPTER_E2E=1 to enable adapter outbound E2E")
	}
	require.NotEmpty(t, cfg.OpenClawAgentID, "GRIX_ADAPTER_OPENCLAW_AGENT_ID is required")

	harness := newLiveAgentHarness(t, cfg.liveConfig())
	totalTimeout := cfg.ConversationTimeout + 30*time.Second
	ctx, cancel := context.WithTimeout(context.Background(), totalTimeout)
	defer cancel()

	// Log in as user.
	session := harness.bootstrapProjectIdentity(ctx)

	// Verify the OpenClaw agent is online (assumes already running).
	openclawAgent := harness.resolveOnlineAgentByClientType(ctx, session, "openclaw", "openclaw-agent-resolve")
	directSession := harness.openDirectSessionForAgent(ctx, session, openclawAgent)

	client := newLiveUserWSClient(t, harness, directSession)
	client.connect(ctx)
	defer client.close()

	marker := fmt.Sprintf("ADAPTER_E2E_OPENCLAW_%d", time.Now().UnixMilli())
	probe := client.runConversationProbe(ctx, liveConversationProbeOptions{
		Message:              "请严格只回复以下文字，不要有任何其他内容：" + marker,
		ExpectedSenderID:     directSession.AgentID,
		ExpectedTextContains: marker,
		Timeout:              cfg.ConversationTimeout,
	})

	require.NotEmpty(t, probe.AgentPushes, "OpenClaw agent should reply with a visible message")
	require.NotEmpty(t, probe.TriggerMsgID, "trigger message should have an ID")

	harness.writeJSON("openclaw-outbound-roundtrip-summary.json", map[string]any{
		"generated_at":   time.Now().UTC().Format(time.RFC3339),
		"artifacts_dir":  harness.artifactDir,
		"marker":         marker,
		"session_id":     directSession.SessionID,
		"agent_id":       directSession.AgentID,
		"trigger_msg_id": probe.TriggerMsgID,
		"push_count":     len(probe.AgentPushes),
	})

	t.Logf("openclaw outbound roundtrip OK: marker=%s pushes=%d", marker, len(probe.AgentPushes))
}
