package e2e

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestLiveAdapterClaudeRoundtrip verifies the full outbound E2E path:
//
//	Go test → real backend → grix-claude daemon → real Claude Code → response → backend → Go test
//
// Prerequisites:
//   - AIBot backend running at GRIX_ADAPTER_E2E_API_BASE (default http://127.0.0.1:27180/v1)
//   - Claude Code CLI installed and authenticated
//   - grix-claude built (npm run build in grix-claude repo)
//
// Enable with: GRIX_ADAPTER_E2E=1
func TestLiveAdapterClaudeRoundtrip(t *testing.T) {
	cfg := loadAdapterTestConfig(t)
	if !cfg.Enabled {
		t.Skip("set GRIX_ADAPTER_E2E=1 to enable adapter outbound E2E")
	}
	require.NotEmpty(t, cfg.ClaudeAgentID, "GRIX_ADAPTER_CLAUDE_AGENT_ID is required")
	require.NotEmpty(t, cfg.ClaudeAPIKey, "GRIX_ADAPTER_CLAUDE_API_KEY is required")

	if cfg.ClaudeRepo == "" {
		t.Skip("grix-claude repo not found")
	}
	if _, err := os.Stat(filepath.Join(cfg.ClaudeRepo, "dist", "daemon.js")); err != nil {
		t.Skip("grix-claude not built (run npm run build in grix-claude repo)")
	}

	harness := newLiveAgentHarness(t, cfg.liveConfig())
	totalTimeout := cfg.ConversationTimeout + 120*time.Second
	ctx, cancel := context.WithTimeout(context.Background(), totalTimeout)
	defer cancel()

	// 1. Compute the Agent API WebSocket URL.
	wsURL, err := deriveAgentAPIWSURL(cfg.BackendURL)
	require.NoError(t, err, "derive agent API WS URL")
	t.Logf("agent API WS URL: %s", wsURL)

	// 2. Start the Claude daemon subprocess.
	dataDir := t.TempDir()
	adapterProc := startAdapterProcess(t, adapterProcessConfig{
		Name:        "claude",
		Command:     []string{"node", "dist/daemon.js"},
		WorkDir:     cfg.ClaudeRepo,
		WSURL:       wsURL,
		AgentID:     cfg.ClaudeAgentID,
		APIKey:      cfg.ClaudeAPIKey,
		DataDir:     dataDir,
		ClientType:  "claude",
		WSURLFlag:   "--ws-url",
		DataDirFlag: "--data-dir",
		Env: []string{
			"GRIX_CLAUDE_SHOW_CLAUDE_WINDOW=0",
			"GRIX_CLAUDE_EVENT_RESULT_TIMEOUT_MS=180000",
		},
	})
	defer adapterProc.dumpArtifacts(harness.artifactDir)

	// 3. Log in as user.
	session := harness.bootstrapProjectIdentity(ctx)

	// 4. Wait for the Claude adapter to come online.
	adapterProc.waitOnline(ctx, cfg.BackendURL, session.Token, "claude")

	// 5. Find the Claude agent and open a session.
	claudeAgent := harness.resolveOnlineAgentByClientType(ctx, session, "claude", "claude-agent-resolve")
	directSession := harness.openDirectSessionForAgent(ctx, session, claudeAgent)

	// 6. Connect as user via WebSocket.
	client := newLiveUserWSClient(t, harness, directSession)
	client.connect(ctx)
	defer client.close()

	// 7. Send a message with a unique marker and wait for Claude's reply.
	marker := fmt.Sprintf("ADAPTER_E2E_CLAUDE_%d", time.Now().UnixMilli())
	probe := client.runConversationProbe(ctx, liveConversationProbeOptions{
		Message:              "请严格只回复以下文字，不要有任何其他内容：" + marker,
		ExpectedSenderID:     directSession.AgentID,
		ExpectedTextContains: marker,
		Timeout:              cfg.ConversationTimeout,
	})

	// 8. Verify the response.
	require.NotEmpty(t, probe.AgentPushes, "Claude agent should reply with a visible message")
	require.NotEmpty(t, probe.TriggerMsgID, "trigger message should have an ID")

	harness.writeJSON("claude-outbound-roundtrip-summary.json", map[string]any{
		"generated_at":  time.Now().UTC().Format(time.RFC3339),
		"artifacts_dir": harness.artifactDir,
		"marker":        marker,
		"session_id":    directSession.SessionID,
		"agent_id":      directSession.AgentID,
		"trigger_msg_id": probe.TriggerMsgID,
		"push_count":    len(probe.AgentPushes),
	})

	t.Logf("claude outbound roundtrip OK: marker=%s pushes=%d", marker, len(probe.AgentPushes))
}
