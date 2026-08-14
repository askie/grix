package e2e

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type adapterTestConfig struct {
	Enabled      bool
	BackendURL   string
	UserToken    string
	UserAccount  string
	UserPassword string

	// Claude adapter
	ClaudeRepo    string
	ClaudeAgentID string
	ClaudeAPIKey  string

	// Gemini adapter (reserved)
	GeminiRepo    string
	GeminiAgentID string
	GeminiAPIKey  string

	// Qwen adapter
	QwenRepo    string
	QwenAgentID string
	QwenAPIKey  string

	// Codex adapter
	CodexRepo    string
	CodexAgentID string
	CodexAPIKey  string

	// OpenClaw adapter
	OpenClawRepo    string
	OpenClawAgentID string
	OpenClawAPIKey  string
	OpenClawBin     string

	// PI adapter
	PIAgentID string
	PIAPIKey  string

	ConversationTimeout time.Duration
}

func loadAdapterTestConfig(t *testing.T) adapterTestConfig {
	t.Helper()

	parentDir := filepath.Dir(findRepoRootFallback())
	backendURL := envOrDefault("GRIX_ADAPTER_E2E_API_BASE", "http://127.0.0.1:27180/v1")
	timeoutSec := envInt("GRIX_ADAPTER_E2E_TIMEOUT_SEC", 180)

	return adapterTestConfig{
		Enabled:      strings.TrimSpace(os.Getenv("GRIX_ADAPTER_E2E")) == "1",
		BackendURL:   backendURL,
		UserToken:    strings.TrimSpace(os.Getenv("GRIX_ADAPTER_E2E_USER_TOKEN")),
		UserAccount:  strings.TrimSpace(os.Getenv("GRIX_ADAPTER_E2E_USER_ACCOUNT")),
		UserPassword: strings.TrimSpace(os.Getenv("GRIX_ADAPTER_E2E_USER_PASSWORD")),

		ClaudeRepo:    firstExistingDir(strings.TrimSpace(os.Getenv("GRIX_ADAPTER_CLAUDE_REPO")), filepath.Join(parentDir, "grix-claude")),
		ClaudeAgentID: strings.TrimSpace(os.Getenv("GRIX_ADAPTER_CLAUDE_AGENT_ID")),
		ClaudeAPIKey:  strings.TrimSpace(os.Getenv("GRIX_ADAPTER_CLAUDE_API_KEY")),

		GeminiRepo:    firstExistingDir(strings.TrimSpace(os.Getenv("GRIX_ADAPTER_GEMINI_REPO")), filepath.Join(parentDir, "grix-gemini")),
		GeminiAgentID: strings.TrimSpace(os.Getenv("GRIX_ADAPTER_GEMINI_AGENT_ID")),
		GeminiAPIKey:  strings.TrimSpace(os.Getenv("GRIX_ADAPTER_GEMINI_API_KEY")),

		QwenRepo:    firstExistingDir(strings.TrimSpace(os.Getenv("GRIX_ADAPTER_QWEN_REPO")), filepath.Join(parentDir, "grix-qwen")),
		QwenAgentID: strings.TrimSpace(os.Getenv("GRIX_ADAPTER_QWEN_AGENT_ID")),
		QwenAPIKey:  strings.TrimSpace(os.Getenv("GRIX_ADAPTER_QWEN_API_KEY")),

		CodexRepo:    firstExistingDir(strings.TrimSpace(os.Getenv("GRIX_ADAPTER_CODEX_REPO")), filepath.Join(parentDir, "grix-codex")),
		CodexAgentID: strings.TrimSpace(os.Getenv("GRIX_ADAPTER_CODEX_AGENT_ID")),
		CodexAPIKey:  strings.TrimSpace(os.Getenv("GRIX_ADAPTER_CODEX_API_KEY")),

		OpenClawRepo:    firstExistingDir(strings.TrimSpace(os.Getenv("GRIX_ADAPTER_OPENCLAW_REPO")), filepath.Join(parentDir, "grix-openclaw")),
		OpenClawAgentID: strings.TrimSpace(os.Getenv("GRIX_ADAPTER_OPENCLAW_AGENT_ID")),
		OpenClawAPIKey:  strings.TrimSpace(os.Getenv("GRIX_ADAPTER_OPENCLAW_API_KEY")),
		OpenClawBin:     strings.TrimSpace(os.Getenv("GRIX_ADAPTER_OPENCLAW_BIN")),

		PIAgentID: strings.TrimSpace(os.Getenv("GRIX_ADAPTER_PI_AGENT_ID")),
		PIAPIKey:  strings.TrimSpace(os.Getenv("GRIX_ADAPTER_PI_API_KEY")),

		ConversationTimeout: time.Duration(timeoutSec) * time.Second,
	}
}

// liveConfig converts adapterTestConfig into a liveAgentConfig for harness reuse.

func (c adapterTestConfig) liveConfig() liveAgentConfig {
	return liveAgentConfig{
		Enabled:             c.Enabled,
		APIBase:             c.BackendURL,
		UserToken:           c.UserToken,
		UserAccount:         c.UserAccount,
		UserPassword:        c.UserPassword,
		DeviceID:            "adapter-e2e-device",
		Platform:            "adapter-e2e",
		SessionStrategy:     "create",
		ConversationTimeout: c.ConversationTimeout,
	}
}
