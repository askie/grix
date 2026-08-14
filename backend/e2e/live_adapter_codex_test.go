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

// TestLiveAdapterCodexRoundtrip verifies the full outbound E2E path:
//
//	Go test → real backend → grix-codex agent → real Codex CLI → response → backend → Go test
//
// Prerequisites:
//   - AIBot backend running at GRIX_ADAPTER_E2E_API_BASE (default http://127.0.0.1:27180/v1)
//   - OpenAI Codex CLI installed and authenticated
//   - grix-codex built (npm run build in grix-codex repo)
//
// Enable with: GRIX_ADAPTER_E2E=1
func TestLiveAdapterCodexRoundtrip(t *testing.T) {
	cfg := loadAdapterTestConfig(t)
	if !cfg.Enabled {
		t.Skip("set GRIX_ADAPTER_E2E=1 to enable adapter outbound E2E")
	}
	require.NotEmpty(t, cfg.CodexAgentID, "GRIX_ADAPTER_CODEX_AGENT_ID is required")
	require.NotEmpty(t, cfg.CodexAPIKey, "GRIX_ADAPTER_CODEX_API_KEY is required")

	if cfg.CodexRepo == "" {
		t.Skip("grix-codex repo not found")
	}
	if _, err := os.Stat(filepath.Join(cfg.CodexRepo, "dist", "cli.js")); err != nil {
		t.Skip("grix-codex not built (run npm run build in grix-codex repo)")
	}

	harness := newLiveAgentHarness(t, cfg.liveConfig())
	totalTimeout := cfg.ConversationTimeout + 120*time.Second
	ctx, cancel := context.WithTimeout(context.Background(), totalTimeout)
	defer cancel()

	wsURL, err := deriveAgentAPIWSURL(cfg.BackendURL)
	require.NoError(t, err, "derive agent API WS URL")
	t.Logf("agent API WS URL: %s", wsURL)

	runtimeDir := t.TempDir()
	adapterProc := startAdapterProcess(t, adapterProcessConfig{
		Name:       "codex",
		Command:    []string{"node", "dist/cli.js", "agent"},
		WorkDir:    cfg.CodexRepo,
		WSURL:      wsURL,
		AgentID:    cfg.CodexAgentID,
		APIKey:     cfg.CodexAPIKey,
		DataDir:    runtimeDir,
		ClientType: "codex",
	})
	defer adapterProc.dumpArtifacts(harness.artifactDir)

	session := harness.bootstrapProjectIdentity(ctx)
	adapterProc.waitOnline(ctx, cfg.BackendURL, session.Token, "codex")

	codexAgent := harness.resolveOnlineAgentByClientType(ctx, session, "codex", "codex-agent-resolve")
	directSession := harness.openDirectSessionForAgent(ctx, session, codexAgent)

	client := newLiveUserWSClient(t, harness, directSession)
	client.connect(ctx)
	defer client.close()

	marker := fmt.Sprintf("ADAPTER_E2E_CODEX_%d", time.Now().UnixMilli())
	probe := client.runConversationProbe(ctx, liveConversationProbeOptions{
		Message:              "Reply with exactly this text and nothing else: " + marker,
		ExpectedSenderID:     directSession.AgentID,
		ExpectedTextContains: marker,
		Timeout:              cfg.ConversationTimeout,
	})

	require.NotEmpty(t, probe.AgentPushes, "Codex agent should reply with a visible message")
	require.NotEmpty(t, probe.TriggerMsgID, "trigger message should have an ID")

	harness.writeJSON("codex-outbound-roundtrip-summary.json", map[string]any{
		"generated_at":   time.Now().UTC().Format(time.RFC3339),
		"artifacts_dir":  harness.artifactDir,
		"marker":         marker,
		"session_id":     directSession.SessionID,
		"agent_id":       directSession.AgentID,
		"trigger_msg_id": probe.TriggerMsgID,
		"push_count":     len(probe.AgentPushes),
	})

	t.Logf("codex outbound roundtrip OK: marker=%s pushes=%d", marker, len(probe.AgentPushes))
}
