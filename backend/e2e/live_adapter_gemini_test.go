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

// TestLiveAdapterGeminiRoundtrip verifies the full outbound E2E path:
//
//	Go test → real backend → grix-gemini agent → real Gemini CLI → response → backend → Go test
//
// Prerequisites:
//   - AIBot backend running at GRIX_ADAPTER_E2E_API_BASE (default http://127.0.0.1:27180/v1)
//   - Gemini CLI installed and authenticated (GEMINI_API_KEY set)
//   - grix-gemini built (npm run build in grix-gemini repo)
//
// Enable with: GRIX_ADAPTER_E2E=1
func TestLiveAdapterGeminiRoundtrip(t *testing.T) {
	cfg := loadAdapterTestConfig(t)
	if !cfg.Enabled {
		t.Skip("set GRIX_ADAPTER_E2E=1 to enable adapter outbound E2E")
	}
	require.NotEmpty(t, cfg.GeminiAgentID, "GRIX_ADAPTER_GEMINI_AGENT_ID is required")
	require.NotEmpty(t, cfg.GeminiAPIKey, "GRIX_ADAPTER_GEMINI_API_KEY is required")

	if cfg.GeminiRepo == "" {
		t.Skip("grix-gemini repo not found")
	}
	if _, err := os.Stat(filepath.Join(cfg.GeminiRepo, "dist", "cli.js")); err != nil {
		t.Skip("grix-gemini not built (run npm run build in grix-gemini repo)")
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
		Name:       "gemini",
		Command:    []string{"node", "dist/cli.js", "agent"},
		WorkDir:    cfg.GeminiRepo,
		WSURL:      wsURL,
		AgentID:    cfg.GeminiAgentID,
		APIKey:     cfg.GeminiAPIKey,
		DataDir:    runtimeDir,
		ClientType: "gemini",
		// defaults: --endpoint, --agent-id, --api-key, --runtime-dir
	})
	defer adapterProc.dumpArtifacts(harness.artifactDir)

	session := harness.bootstrapProjectIdentity(ctx)
	adapterProc.waitOnline(ctx, cfg.BackendURL, session.Token, "gemini")

	geminiAgent := harness.resolveOnlineAgentByClientType(ctx, session, "gemini", "gemini-agent-resolve")
	directSession := harness.openDirectSessionForAgent(ctx, session, geminiAgent)

	client := newLiveUserWSClient(t, harness, directSession)
	client.connect(ctx)
	defer client.close()

	marker := fmt.Sprintf("ADAPTER_E2E_GEMINI_%d", time.Now().UnixMilli())
	probe := client.runConversationProbe(ctx, liveConversationProbeOptions{
		Message:              "Please reply with exactly this text and nothing else: " + marker,
		ExpectedSenderID:     directSession.AgentID,
		ExpectedTextContains: marker,
		Timeout:              cfg.ConversationTimeout,
	})

	require.NotEmpty(t, probe.AgentPushes, "Gemini agent should reply with a visible message")
	require.NotEmpty(t, probe.TriggerMsgID, "trigger message should have an ID")

	harness.writeJSON("gemini-outbound-roundtrip-summary.json", map[string]any{
		"generated_at":   time.Now().UTC().Format(time.RFC3339),
		"artifacts_dir":  harness.artifactDir,
		"marker":         marker,
		"session_id":     directSession.SessionID,
		"agent_id":       directSession.AgentID,
		"trigger_msg_id": probe.TriggerMsgID,
		"push_count":     len(probe.AgentPushes),
	})

	t.Logf("gemini outbound roundtrip OK: marker=%s pushes=%d", marker, len(probe.AgentPushes))
}
