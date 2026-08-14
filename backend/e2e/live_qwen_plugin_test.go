package e2e

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type liveQwenPluginConfig struct {
	QwenRepoDir string
	ScriptPath  string
	PluginDir   string
}

func TestLiveQwenPluginManagedAgentSmoke(t *testing.T) {
	baseCfg := loadLiveAgentConfig(t)
	if !baseCfg.Enabled {
		t.Skip("set GRIX_LIVE_E2E=1 to enable live Qwen plugin E2E")
	}

	cfg := loadLiveQwenPluginConfig(t, baseCfg)
	if strings.TrimSpace(cfg.ScriptPath) == "" {
		t.Skip("qwen live E2E script path is empty")
	}

	harness := newLiveAgentHarness(t, baseCfg)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	cmd := exec.CommandContext(ctx, "bash", cfg.ScriptPath)
	cmd.Dir = firstNonEmpty(cfg.QwenRepoDir, baseCfg.RepoRoot)
	cmd.Env = append(os.Environ(), "GRIX_LIVE_QWEN_PLUGIN_DIR="+cfg.PluginDir)
	output, err := cmd.CombinedOutput()
	harness.writeText("qwen-plugin-managed-agent-smoke.log", string(output))
	require.NoError(t, err)

	harness.writeJSON("qwen-plugin-managed-agent-smoke-summary.json", map[string]any{
		"generated_at":  time.Now().UTC().Format(time.RFC3339),
		"artifacts_dir": harness.artifactDir,
		"qwen_repo_dir": cfg.QwenRepoDir,
		"plugin_dir":    cfg.PluginDir,
		"script_path":   cfg.ScriptPath,
	})
}

func loadLiveQwenPluginConfig(t *testing.T, baseCfg liveAgentConfig) liveQwenPluginConfig {
	t.Helper()

	repoRoot := baseCfg.RepoRoot
	if strings.TrimSpace(repoRoot) == "" {
		repoRoot = findRepoRoot(t)
	}

	parentDir := filepath.Dir(repoRoot)
	qwenRepoDir := firstExistingDir(
		strings.TrimSpace(os.Getenv("GRIX_LIVE_QWEN_REPO_DIR")),
		filepath.Join(parentDir, "grix-qwen"),
	)

	scriptPath := strings.TrimSpace(os.Getenv("GRIX_LIVE_QWEN_E2E_SCRIPT"))
	pluginDir := strings.TrimSpace(os.Getenv("GRIX_LIVE_QWEN_PLUGIN_DIR"))
	if qwenRepoDir != "" {
		scriptPath = firstNonEmpty(scriptPath, filepath.Join(qwenRepoDir, "scripts", "check_qwen_plugin_e2e.sh"))
		pluginDir = firstNonEmpty(pluginDir, qwenRepoDir)
	}

	return liveQwenPluginConfig{
		QwenRepoDir: qwenRepoDir,
		ScriptPath:  scriptPath,
		PluginDir:   pluginDir,
	}
}
