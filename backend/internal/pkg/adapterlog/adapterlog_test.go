package adapterlog

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestDisabledManager(t *testing.T) {
	cfg := Config{Enabled: false}
	m := NewManager(cfg)
	defer m.Close()

	err := m.WriteEntry("openclaw", "sess_1", LogEntry{
		Dir:    "inbound",
		Method: "NormalizeInbound",
	})
	if err != nil {
		t.Fatalf("WriteEntry on disabled manager should be no-op: %v", err)
	}
}

func TestWriteEntryCreatesFile(t *testing.T) {
	dir := t.TempDir()
	m := NewManager(Config{
		Enabled:    true,
		LogRoot:    dir,
		MaxSizeMB:  100,
		MaxAgeDays: 7,
	})
	defer m.Close()

	entry := LogEntry{
		Dir:       "inbound",
		Method:    "NormalizeInbound",
		Family:    "hermes",
		AdapterID: "hermes/base",
		SessionID: "sess_123",
		Input:     json.RawMessage(`{"content":"hello"}`),
		Output:    json.RawMessage(`{"content":"hello","drop":false}`),
	}
	if err := m.WriteEntry("hermes", "sess_123", entry); err != nil {
		t.Fatal(err)
	}

	p := filepath.Join(dir, "hermes", "session_sess_123.jsonl")
	f, err := os.Open(p)
	if err != nil {
		t.Fatalf("log file not created: %v", err)
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	if !scanner.Scan() {
		t.Fatal("log file is empty")
	}

	var got LogEntry
	if err := json.Unmarshal(scanner.Bytes(), &got); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if got.Dir != "inbound" || got.Method != "NormalizeInbound" || got.SessionID != "sess_123" {
		t.Errorf("unexpected entry: %+v", got)
	}
}

func TestWriteEntryMultipleFamilies(t *testing.T) {
	dir := t.TempDir()
	m := NewManager(Config{
		Enabled:    true,
		LogRoot:    dir,
		MaxSizeMB:  100,
		MaxAgeDays: 7,
	})
	defer m.Close()

	m.WriteEntry("openclaw", "s1", LogEntry{Dir: "inbound", Method: "NormalizeInbound"})
	m.WriteEntry("hermes", "s2", LogEntry{Dir: "outbound", Method: "NormalizeOutbound"})

	if _, err := os.Stat(filepath.Join(dir, "openclaw", "session_s1.jsonl")); err != nil {
		t.Errorf("openclaw log missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "hermes", "session_s2.jsonl")); err != nil {
		t.Errorf("hermes log missing: %v", err)
	}
}

func TestWriteEntryUnknownSession(t *testing.T) {
	dir := t.TempDir()
	m := NewManager(Config{
		Enabled:    true,
		LogRoot:    dir,
		MaxSizeMB:  100,
		MaxAgeDays: 7,
	})
	defer m.Close()

	err := m.WriteEntry("claude", "", LogEntry{Dir: "inbound", Method: "NormalizeInbound"})
	if err != nil {
		t.Fatal(err)
	}

	p := filepath.Join(dir, "claude", "session__unknown.jsonl")
	if _, err := os.Stat(p); err != nil {
		t.Errorf("fallback log file not created: %v", err)
	}
}

func TestConfigFromEnv(t *testing.T) {
	os.Setenv("AIBOT_ADAPTER_LOG_DIR", "/tmp/test-adapter-logs")
	defer os.Unsetenv("AIBOT_ADAPTER_LOG_DIR")

	cfg := ConfigFromEnv()
	if !cfg.Enabled || cfg.LogRoot != "/tmp/test-adapter-logs" {
		t.Errorf("expected enabled config with env dir, got %+v", cfg)
	}
	if cfg.MaxSizeMB != 100 || cfg.MaxAgeDays != 7 {
		t.Errorf("expected defaults, got MaxSizeMB=%d MaxAgeDays=%d", cfg.MaxSizeMB, cfg.MaxAgeDays)
	}

	os.Unsetenv("AIBOT_ADAPTER_LOG_DIR")
	cfg = ConfigFromEnv()
	if !cfg.Enabled {
		t.Error("should be enabled by default")
	}
	if cfg.LogRoot != "adapter-logs" {
		t.Errorf("expected default dir 'adapter-logs', got %q", cfg.LogRoot)
	}
}
