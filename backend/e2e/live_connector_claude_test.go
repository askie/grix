package e2e

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestLiveConnectorClaudeRoundtrip verifies the full E2E path via grix-connector:
//
//	Go test → AIBot backend → grix-connector → Claude Code → response → backend → Go test
//
// Prerequisites:
//   - AIBot backend running at GRIX_LIVE_API_BASE (default http://127.0.0.1:27180/v1)
//   - grix-connector running with claude-local agent configured
//
// Enable with: GRIX_LIVE_E2E=1
// Target agent: GRIX_LIVE_REMOTE_AGENT_ID (defaults to claude-local agent ID)
func TestLiveConnectorClaudeRoundtrip(t *testing.T) {
	cfg := loadLiveAgentConfig(t)
	if !cfg.Enabled {
		t.Skip("set GRIX_LIVE_E2E=1 to enable connector E2E test")
	}

	// Default to claude-local agent ID if not specified
	remoteAgentID := cfg.RemoteAgentID
	if remoteAgentID == "" {
		remoteAgentID = "2053600921908154368" // claude-local
	}

	harness := newLiveAgentHarness(t, cfg)
	totalTimeout := cfg.ConversationTimeout + 30*time.Second
	ctx, cancel := context.WithTimeout(context.Background(), totalTimeout)
	defer cancel()

	// 1. Log in as user
	session := harness.bootstrapProjectIdentity(ctx)

	// 2. Find the Claude agent online
	claudeAgent := harness.resolveOnlineAgentByClientType(ctx, session, "claude", "claude-agent-resolve")
	t.Logf("resolved claude agent: id=%s name=%s", asString(claudeAgent["id"]), asString(claudeAgent["agent_name"]))

	// Override to target specific agent if requested
	if cfg.RemoteAgentID != "" {
		require.Equal(t, remoteAgentID, asString(claudeAgent["id"]),
			"expected to resolve the configured remote agent")
	}

	// 3. Open a direct session with the agent
	directSession := harness.openDirectSessionForAgent(ctx, session, claudeAgent)
	t.Logf("opened session: id=%s agent_id=%s", directSession.SessionID, directSession.AgentID)

	// 4. Connect as user via WebSocket
	client := newLiveUserWSClient(t, harness, directSession)
	client.connect(ctx)
	defer client.close()

	// 5. Send a message with a unique marker and wait for Claude's reply
	marker := fmt.Sprintf("CONNECTOR_E2E_CLAUDE_%d", time.Now().UnixMilli())
	probe := client.runConversationProbe(ctx, liveConversationProbeOptions{
		Message:              "请严格只回复以下文字，不要有任何其他内容：" + marker,
		ExpectedSenderID:     directSession.AgentID,
		ExpectedTextContains: marker,
		Timeout:              cfg.ConversationTimeout,
	})

	// 6. Verify the response
	require.NotEmpty(t, probe.AgentPushes, "Claude agent should reply with a visible message")
	require.NotEmpty(t, probe.TriggerMsgID, "trigger message should have an ID")

	harness.writeJSON("connector-claude-roundtrip-summary.json", map[string]any{
		"generated_at":   time.Now().UTC().Format(time.RFC3339),
		"artifacts_dir":  harness.artifactDir,
		"marker":         marker,
		"session_id":     directSession.SessionID,
		"agent_id":       directSession.AgentID,
		"trigger_msg_id": probe.TriggerMsgID,
		"push_count":     len(probe.AgentPushes),
	})

	t.Logf("connector claude roundtrip OK: marker=%s pushes=%d", marker, len(probe.AgentPushes))
}
