package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

// adapterProcessConfig defines how to start an adapter subprocess.
type adapterProcessConfig struct {
	Name       string   // adapter name (e.g. "claude", "gemini")
	Command    []string // startup command (e.g. ["node", "dist/daemon.js"])
	WorkDir    string   // working directory
	Env        []string // extra environment variables (KEY=VALUE)
	WSURL      string   // backend Agent API WebSocket URL
	AgentID    string   // agent ID
	APIKey     string   // API key
	DataDir    string   // adapter data directory (temp)
	LogPrefix  string   // prefix for log lines
	ClientType string   // auth client_type (e.g. "claude", "gemini")

	// CLI flag names — vary between adapters.
	// Defaults: WSURLFlag="--endpoint", AgentIDFlag="--agent-id", APIKeyFlag="--api-key", DataDirFlag="--runtime-dir"
	WSURLFlag   string
	AgentIDFlag string
	APIKeyFlag  string
	DataDirFlag string
}

func (c *adapterProcessConfig) wsURLFlag() string {
	if c.WSURLFlag != "" {
		return c.WSURLFlag
	}
	return "--endpoint"
}

func (c *adapterProcessConfig) agentIDFlag() string {
	if c.AgentIDFlag != "" {
		return c.AgentIDFlag
	}
	return "--agent-id"
}

func (c *adapterProcessConfig) apiKeyFlag() string {
	if c.APIKeyFlag != "" {
		return c.APIKeyFlag
	}
	return "--api-key"
}

func (c *adapterProcessConfig) dataDirFlag() string {
	if c.DataDirFlag != "" {
		return c.DataDirFlag
	}
	return "--runtime-dir"
}

// adapterProcess manages the lifecycle of an adapter subprocess.
type adapterProcess struct {
	t       *testing.T
	cfg     adapterProcessConfig
	cmd     *exec.Cmd
	stdout  *lockedBuffer
	stderr  *lockedBuffer
	started bool
}

// lockedBuffer is a concurrency-safe bytes.Buffer.
type lockedBuffer struct {
	mu  sync.Mutex
	buf strings.Builder
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// startAdapterProcess launches the adapter subprocess and returns a manager.
// It calls t.Skip if the adapter binary is not found.
func startAdapterProcess(t *testing.T, cfg adapterProcessConfig) *adapterProcess {
	t.Helper()

	if len(cfg.Command) == 0 {
		t.Fatalf("adapter command is empty")
	}

	// Check that the command entry point exists (for "node script.js" patterns).
	if len(cfg.Command) >= 2 {
		entryPoint := cfg.Command[1]
		if !filepath.IsAbs(entryPoint) && cfg.WorkDir != "" {
			entryPoint = filepath.Join(cfg.WorkDir, entryPoint)
		}
		if _, err := os.Stat(entryPoint); err != nil {
			t.Skipf("adapter entry point not found (%s), skipping %s adapter E2E", entryPoint, cfg.Name)
		}
	}

	// Build command args: append flags for ws-url, agent-id, api-key, data-dir.
	args := append([]string{}, cfg.Command...)
	args = append(args,
		cfg.wsURLFlag(), cfg.WSURL,
		cfg.agentIDFlag(), cfg.AgentID,
		cfg.apiKeyFlag(), cfg.APIKey,
	)
	if cfg.DataDir != "" {
		args = append(args, cfg.dataDirFlag(), cfg.DataDir)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(ctx, args[0], args[1:]...)
	cmd.Dir = cfg.WorkDir

	// Merge environment.
	env := os.Environ()
	env = append(env, cfg.Env...)
	cmd.Env = env

	p := &adapterProcess{
		t:      t,
		cfg:    cfg,
		cmd:    cmd,
		stdout: &lockedBuffer{},
		stderr: &lockedBuffer{},
	}

	cmd.Stdout = p.stdout
	cmd.Stderr = p.stderr

	if err := cmd.Start(); err != nil {
		cancel()
		t.Fatalf("failed to start %s adapter: %v", cfg.Name, err)
	}
	p.started = true

	// Ensure cleanup on test finish.
	t.Cleanup(func() {
		cancel()
		p.stop()
	})

	prefix := cfg.LogPrefix
	if prefix == "" {
		prefix = cfg.Name
	}
	t.Logf("[%s] adapter process started (pid=%d)", prefix, cmd.Process.Pid)
	return p
}

// waitOnline polls the backend API until an agent matching clientType appears online.
func (p *adapterProcess) waitOnline(ctx context.Context, apiBase, token, clientType string) {
	p.t.Helper()
	prefix := p.cfg.LogPrefix
	if prefix == "" {
		prefix = p.cfg.Name
	}

	deadline := time.Now().Add(120 * time.Second)
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			p.t.Fatalf("[%s] context canceled while waiting for adapter to come online: %v", prefix, ctx.Err())
		case <-ticker.C:
		}

		if time.Now().After(deadline) {
			p.dumpArtifactsToStderr()
			p.t.Fatalf("[%s] timed out waiting for %s adapter to come online (120s)", prefix, clientType)
		}

		online, err := checkAgentOnline(apiBase, token, clientType, p.cfg.AgentID)
		if err != nil {
			p.t.Logf("[%s] agent list poll error: %v", prefix, err)
			continue
		}
		if online {
			p.t.Logf("[%s] adapter is online (client_type=%s)", prefix, clientType)
			return
		}

		// Check if the process has died.
		if p.cmd.ProcessState != nil && p.cmd.ProcessState.Exited() {
			p.dumpArtifactsToStderr()
			p.t.Fatalf("[%s] adapter process exited prematurely (code=%d)", prefix, p.cmd.ProcessState.ExitCode())
		}
	}
}

// stop sends SIGTERM and waits; kills on timeout.
func (p *adapterProcess) stop() {
	if p.cmd == nil || p.cmd.Process == nil {
		return
	}

	// Already exited.
	if p.cmd.ProcessState != nil && p.cmd.ProcessState.Exited() {
		return
	}

	prefix := p.cfg.LogPrefix
	if prefix == "" {
		prefix = p.cfg.Name
	}

	_ = p.cmd.Process.Signal(syscall.SIGTERM)
	done := make(chan error, 1)
	go func() {
		done <- p.cmd.Wait()
	}()

	select {
	case <-done:
		// Process exited cleanly.
	case <-time.After(10 * time.Second):
		p.t.Logf("[%s] adapter did not exit after SIGTERM, sending SIGKILL", prefix)
		_ = p.cmd.Process.Kill()
		<-done
	}
}

// dumpArtifacts writes stdout/stderr to the artifact directory.
func (p *adapterProcess) dumpArtifacts(artifactDir string) {
	p.t.Helper()
	prefix := p.cfg.LogPrefix
	if prefix == "" {
		prefix = p.cfg.Name
	}

	if artifactDir == "" {
		return
	}

	if stdout := p.stdout.String(); stdout != "" {
		writeTextFile(p.t, filepath.Join(artifactDir, prefix+"-adapter-stdout.log"), stdout)
	}
	if stderr := p.stderr.String(); stderr != "" {
		writeTextFile(p.t, filepath.Join(artifactDir, prefix+"-adapter-stderr.log"), stderr)
	}
}

// dumpArtifactsToStderr writes adapter output to test log for diagnostics.
func (p *adapterProcess) dumpArtifactsToStderr() {
	p.t.Helper()
	prefix := p.cfg.LogPrefix
	if prefix == "" {
		prefix = p.cfg.Name
	}
	if stderr := p.stderr.String(); stderr != "" {
		p.t.Logf("[%s] adapter stderr (last 4KB):\n%s", prefix, tailString(stderr, 4096))
	}
	if stdout := p.stdout.String(); stdout != "" {
		p.t.Logf("[%s] adapter stdout (last 4KB):\n%s", prefix, tailString(stdout, 4096))
	}
}

// checkAgentOnline queries the backend API for an online agent matching clientType and agentID.
func checkAgentOnline(apiBase, token, clientType, agentID string) (bool, error) {
	reqURL := strings.TrimRight(apiBase, "/") + "/agents/list"
	req, err := http.NewRequest(http.MethodGet, reqURL, nil)
	if err != nil {
		return false, err
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return false, err
	}

	var envelope map[string]any
	if err := json.Unmarshal(body, &envelope); err != nil {
		return false, err
	}

	data, _ := envelope["data"].(map[string]any)
	if data == nil {
		return false, nil
	}
	items, _ := data["list"].([]any)
	for _, item := range items {
		row, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if asString(row["id"]) != agentID {
			continue
		}
		if asString(row["agent_client_type"]) != clientType {
			continue
		}
		online, _ := row["online"].(bool)
		if online {
			return true, nil
		}
	}
	return false, nil
}

// deriveAgentAPIWSURL converts an HTTP API base URL to the Agent API WebSocket URL.
// e.g. "http://127.0.0.1:27180/v1" → "ws://127.0.0.1:27180/v1/agent-api/ws"
func deriveAgentAPIWSURL(apiBase string) (string, error) {
	parsed, err := url.Parse(apiBase)
	if err != nil {
		return "", err
	}
	switch parsed.Scheme {
	case "http":
		parsed.Scheme = "ws"
	case "https":
		parsed.Scheme = "wss"
	default:
		return "", fmt.Errorf("unsupported api base scheme: %s", parsed.Scheme)
	}
	path := strings.TrimRight(parsed.Path, "/")
	parsed.Path = path + "/agent-api/ws"
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return parsed.String(), nil
}

// tailString returns the last n bytes of s.
func tailString(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[len(s)-n:]
}
