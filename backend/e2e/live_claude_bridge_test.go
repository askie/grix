package e2e

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type liveClaudeBridgeConfig struct {
	AgentID         string
	AgentWSURL      string
	AgentAPIKey     string
	UserAccount     string
	UserPassword    string
	APIBase         string
	AgentLabel      string
	SessionStrategy string
	Scenario        string
	TimeoutSec      int
	Model           string
	Message         string
	ExpectLog       string
	PluginDir       string
	AllowRemote     bool
	SkipBuild       bool
	KeepClaude      bool
}

func TestLiveClaudeGrixClaudeBridge(t *testing.T) {
	baseCfg := loadLiveAgentConfig(t)
	if !baseCfg.Enabled {
		t.Skip("set GRIX_LIVE_E2E=1 to enable live Claude bridge E2E")
	}

	cfg := loadLiveClaudeBridgeConfig()
	required := []string{cfg.AgentID, cfg.AgentWSURL, cfg.AgentAPIKey, cfg.UserAccount, cfg.UserPassword}
	for _, value := range required {
		if strings.TrimSpace(value) == "" {
			t.Skip("set GRIX_LIVE_CLAUDE_AGENT_ID / WS_URL / API_KEY / USER_ACCOUNT / USER_PASSWORD to run the Claude bridge")
		}
	}

	repoRoot := baseCfg.RepoRoot
	if strings.TrimSpace(repoRoot) == "" {
		repoRoot = findRepoRoot(t)
	}

	harness := newLiveAgentHarness(t, baseCfg)
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(cfg.TimeoutSec+120)*time.Second)
	defer cancel()

	args := []string{
		filepath.Join(repoRoot, "scripts", "check_claude_grix-claude_e2e_impl.sh"),
		"--agent-id", cfg.AgentID,
		"--agent-ws-url", cfg.AgentWSURL,
		"--agent-api-key", cfg.AgentAPIKey,
		"--user-account", cfg.UserAccount,
		"--user-password", cfg.UserPassword,
		"--api-base", cfg.APIBase,
		"--agent-label", cfg.AgentLabel,
		"--session-strategy", cfg.SessionStrategy,
		"--scenario", cfg.Scenario,
		"--timeout-sec", strconv.Itoa(cfg.TimeoutSec),
		"--claude-model", cfg.Model,
	}
	if cfg.Message != "" {
		args = append(args, "--message", cfg.Message)
	}
	if cfg.ExpectLog != "" {
		args = append(args, "--expect-log", cfg.ExpectLog)
	}
	if cfg.AllowRemote {
		args = append(args, "--allow-remote")
	}
	if cfg.SkipBuild {
		args = append(args, "--skip-build")
	}
	if cfg.KeepClaude {
		args = append(args, "--keep-claude")
	}

	cmd := exec.CommandContext(ctx, "bash", args...)
	cmd.Dir = repoRoot
	cmd.Env = append(os.Environ(), "CLAUDE_GRIX_CLAUDE_DIR="+cfg.PluginDir)
	output, err := cmd.CombinedOutput()
	harness.writeText("claude-grix-claude-bridge.log", string(output))
	require.NoError(t, err)

	harness.writeJSON("claude-grix-claude-bridge-summary.json", map[string]any{
		"generated_at":     time.Now().UTC().Format(time.RFC3339),
		"artifacts_dir":    harness.artifactDir,
		"scenario":         cfg.Scenario,
		"timeout_sec":      cfg.TimeoutSec,
		"agent_id":         cfg.AgentID,
		"agent_ws_url":     cfg.AgentWSURL,
		"plugin_dir":       cfg.PluginDir,
		"skip_build":       cfg.SkipBuild,
		"allow_remote":     cfg.AllowRemote,
		"keep_claude":      cfg.KeepClaude,
		"session_strategy": cfg.SessionStrategy,
		"agent_label":      cfg.AgentLabel,
		"expect_log":       cfg.ExpectLog,
	})
}

func loadLiveClaudeBridgeConfig() liveClaudeBridgeConfig {
	parentDir := filepath.Dir(findRepoRootFallback())
	pluginDir := firstExistingDir(
		strings.TrimSpace(os.Getenv("GRIX_LIVE_CLAUDE_PLUGIN_DIR")),
		strings.TrimSpace(os.Getenv("CLAUDE_GRIX_CLAUDE_DIR")),
		filepath.Join(parentDir, "grix-claude"),
		filepath.Join(parentDir, "clawpool-claude"),
	)

	timeoutSec := envInt("GRIX_LIVE_CLAUDE_TIMEOUT_SEC", 120)
	return liveClaudeBridgeConfig{
		AgentID:         strings.TrimSpace(os.Getenv("GRIX_LIVE_CLAUDE_AGENT_ID")),
		AgentWSURL:      strings.TrimSpace(os.Getenv("GRIX_LIVE_CLAUDE_AGENT_WS_URL")),
		AgentAPIKey:     strings.TrimSpace(os.Getenv("GRIX_LIVE_CLAUDE_AGENT_API_KEY")),
		UserAccount:     firstNonEmpty(strings.TrimSpace(os.Getenv("GRIX_LIVE_CLAUDE_USER_ACCOUNT")), strings.TrimSpace(os.Getenv("GRIX_LIVE_USER_ACCOUNT"))),
		UserPassword:    firstNonEmpty(strings.TrimSpace(os.Getenv("GRIX_LIVE_CLAUDE_USER_PASSWORD")), strings.TrimSpace(os.Getenv("GRIX_LIVE_USER_PASSWORD"))),
		APIBase:         envOrDefault("GRIX_LIVE_CLAUDE_API_BASE", envOrDefault("GRIX_LIVE_API_BASE", "http://127.0.0.1:27180/v1")),
		AgentLabel:      envOrDefault("GRIX_LIVE_CLAUDE_AGENT_LABEL", "claude-debug"),
		SessionStrategy: envOrDefault("GRIX_LIVE_CLAUDE_SESSION_STRATEGY", "create"),
		Scenario:        envOrDefault("GRIX_LIVE_CLAUDE_SCENARIO", "reply"),
		TimeoutSec:      timeoutSec,
		Model:           envOrDefault("GRIX_LIVE_CLAUDE_MODEL", "sonnet"),
		Message:         strings.TrimSpace(os.Getenv("GRIX_LIVE_CLAUDE_MESSAGE")),
		ExpectLog:       strings.TrimSpace(os.Getenv("GRIX_LIVE_CLAUDE_EXPECT_LOG")),
		PluginDir:       pluginDir,
		AllowRemote:     strings.TrimSpace(os.Getenv("GRIX_LIVE_CLAUDE_ALLOW_REMOTE")) == "1",
		SkipBuild:       strings.TrimSpace(os.Getenv("GRIX_LIVE_CLAUDE_SKIP_BUILD")) == "1",
		KeepClaude:      strings.TrimSpace(os.Getenv("GRIX_LIVE_CLAUDE_KEEP")) == "1",
	}
}

func findRepoRootFallback() string {
	wd, err := os.Getwd()
	if err != nil {
		return ""
	}
	dir := wd
	for {
		if fileExists(filepath.Join(dir, "backend", "go.mod")) {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return wd
		}
		dir = parent
	}
}
