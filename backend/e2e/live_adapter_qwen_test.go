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

// TestLiveAdapterQwenRoundtrip verifies the full outbound E2E path:
//
//	Go test → real backend → grix-qwen agent → real Qwen CLI → response → backend → Go test
//
// Prerequisites:
//   - AIBot backend running at GRIX_ADAPTER_E2E_API_BASE (default http://127.0.0.1:27180/v1)
//   - Qwen CLI installed and authenticated
//   - grix-qwen built (npm run build in grix-qwen repo)
//
// Enable with: GRIX_ADAPTER_E2E=1
func TestLiveAdapterQwenRoundtrip(t *testing.T) {
	cfg := loadAdapterTestConfig(t)
	if !cfg.Enabled {
		t.Skip("set GRIX_ADAPTER_E2E=1 to enable adapter outbound E2E")
	}
	require.NotEmpty(t, cfg.QwenAgentID, "GRIX_ADAPTER_QWEN_AGENT_ID is required")
	require.NotEmpty(t, cfg.QwenAPIKey, "GRIX_ADAPTER_QWEN_API_KEY is required")

	if cfg.QwenRepo == "" {
		t.Skip("grix-qwen repo not found")
	}
	if _, err := os.Stat(filepath.Join(cfg.QwenRepo, "dist", "cli.js")); err != nil {
		t.Skip("grix-qwen not built (run npm run build in grix-qwen repo)")
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
		Name:       "qwen",
		Command:    []string{"node", "dist/cli.js", "agent"},
		WorkDir:    cfg.QwenRepo,
		WSURL:      wsURL,
		AgentID:    cfg.QwenAgentID,
		APIKey:     cfg.QwenAPIKey,
		DataDir:    runtimeDir,
		ClientType: "qwen",
	})
	defer adapterProc.dumpArtifacts(harness.artifactDir)

	session := harness.bootstrapProjectIdentity(ctx)
	adapterProc.waitOnline(ctx, cfg.BackendURL, session.Token, "qwen")

	qwenAgent := harness.resolveOnlineAgentByClientType(ctx, session, "qwen", "qwen-agent-resolve")
	directSession := harness.openDirectSessionForAgent(ctx, session, qwenAgent)

	client := newLiveUserWSClient(t, harness, directSession)
	client.connect(ctx)
	defer client.close()

	marker := fmt.Sprintf("ADAPTER_E2E_QWEN_%d", time.Now().UnixMilli())
	probe := client.runConversationProbe(ctx, liveConversationProbeOptions{
		Message:              "请严格只回复以下文字，不要有任何其他内容：" + marker,
		ExpectedSenderID:     directSession.AgentID,
		ExpectedTextContains: marker,
		Timeout:              cfg.ConversationTimeout,
	})

	require.NotEmpty(t, probe.AgentPushes, "Qwen agent should reply with a visible message")
	require.NotEmpty(t, probe.TriggerMsgID, "trigger message should have an ID")

	harness.writeJSON("qwen-outbound-roundtrip-summary.json", map[string]any{
		"generated_at":   time.Now().UTC().Format(time.RFC3339),
		"artifacts_dir":  harness.artifactDir,
		"marker":         marker,
		"session_id":     directSession.SessionID,
		"agent_id":       directSession.AgentID,
		"trigger_msg_id": probe.TriggerMsgID,
		"push_count":     len(probe.AgentPushes),
	})

	t.Logf("qwen outbound roundtrip OK: marker=%s pushes=%d", marker, len(probe.AgentPushes))
}
