package e2e

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestLiveAgentCodexProgramByDialogue(t *testing.T) {
	runLiveCodingAgentProgramScenario(t, "codex")
}

func TestLiveAgentClaudeProgramByDialogue(t *testing.T) {
	runLiveCodingAgentProgramScenario(t, "claude")
}

func runLiveCodingAgentProgramScenario(t *testing.T, clientType string) {
	t.Helper()

	cfg := loadLiveAgentConfig(t)
	if !cfg.Enabled {
		t.Skip("set GRIX_LIVE_E2E=1 to enable live local agent coding E2E")
	}
	if strings.TrimSpace(cfg.UserToken) == "" && (strings.TrimSpace(cfg.UserAccount) == "" || strings.TrimSpace(cfg.UserPassword) == "") {
		t.Skip("set GRIX_LIVE_USER_TOKEN or both GRIX_LIVE_USER_ACCOUNT / GRIX_LIVE_USER_PASSWORD to run live coding scenario")
	}

	harness := newLiveAgentHarness(t, cfg)
	ctx, cancel := context.WithTimeout(context.Background(), 4*cfg.ConversationTimeout+2*time.Minute)
	defer cancel()

	identity := harness.bootstrapProjectIdentity(ctx)
	agent := harness.resolveOnlineAgentByClientType(ctx, identity, clientType, "agents-list-"+clientType)
	session := openFreshDirectSessionForAgent(ctx, harness, identity, agent, "session-create-"+clientType)

	client := newLiveUserWSClient(t, harness, session)
	client.connect(ctx)
	defer client.close()

	workspaceDir := mustCreateTempWorkspace(t, clientType)
	programPath := filepath.Join(workspaceDir, "main.go")

	openProbe := client.runConversationProbe(ctx, liveConversationProbeOptions{
		Message:              "/grix open " + workspaceDir,
		ExpectedSenderID:     session.AgentID,
		ExpectedTextContains: workspaceDir,
		Timeout:              cfg.ConversationTimeout,
	})

	whereProbe := client.runConversationProbe(ctx, liveConversationProbeOptions{
		Message:              "/grix where",
		ExpectedSenderID:     session.AgentID,
		ExpectedTextContains: workspaceDir,
		Timeout:              cfg.ConversationTimeout,
	})
	workspaceReadyTimeout := cfg.ConversationTimeout
	if workspaceReadyTimeout < 3*time.Minute {
		workspaceReadyTimeout = 3 * time.Minute
	}
	statusProbes := waitForAgentWorkspaceReady(ctx, t, harness, client, clientType, workspaceDir, workspaceReadyTimeout)

	step1Token := strings.ToUpper(clientType) + "_STEP1_DONE"
	step1Message := fmt.Sprintf(
		"请在当前工作目录创建一个 `main.go`。要求：1. 只使用 Go 标准库；2. 定义 `factorial(n int) int`；3. 直接执行 `go run main.go` 时只能输出 `720` 一行；4. 不要创建额外的源码文件；5. 不要提问，不要解释。完成后只回复 `%s`。",
		step1Token,
	)
	step1Probe := client.runConversationProbe(ctx, liveConversationProbeOptions{
		Message:              step1Message,
		ExpectedSenderID:     session.AgentID,
		ExpectedTextContains: step1Token,
		Timeout:              cfg.ConversationTimeout,
	})

	initialSource := waitForFileContent(ctx, t, programPath, 20*time.Second)
	require.Contains(t, initialSource, "factorial", "main.go should contain factorial implementation after first turn")
	initialRun := runGoProgram(t, ctx, workspaceDir)
	require.Equal(t, "720", initialRun.StdoutTrimmed, "initial program output should be 720")

	step2Token := strings.ToUpper(clientType) + "_STEP2_DONE"
	step2Message := fmt.Sprintf(
		"继续修改同一个 `main.go`。要求：1. 支持一个可选命令行参数；2. 执行 `go run main.go 5` 时只能输出 `120` 一行；3. 无参数时仍然只能输出 `720`；4. 不要改文件名，不要解释。完成后只回复 `%s`。",
		step2Token,
	)
	step2Probe := client.runConversationProbe(ctx, liveConversationProbeOptions{
		Message:              step2Message,
		ExpectedSenderID:     session.AgentID,
		ExpectedTextContains: step2Token,
		Timeout:              cfg.ConversationTimeout,
	})

	finalSource := waitForFileContent(ctx, t, programPath, 20*time.Second)
	require.Contains(t, finalSource, "os.Args", "main.go should read command line args after second turn")
	finalDefaultRun := runGoProgram(t, ctx, workspaceDir)
	require.Equal(t, "720", finalDefaultRun.StdoutTrimmed, "default program output should stay 720")
	finalArgRun := runGoProgram(t, ctx, workspaceDir, "5")
	require.Equal(t, "120", finalArgRun.StdoutTrimmed, "program output with n=5 should be 120")

	harness.writeText("workspace-main.go", finalSource)
	harness.writeJSON(clientType+"-program-summary.json", map[string]any{
		"generated_at":      time.Now().UTC().Format(time.RFC3339),
		"artifacts_dir":     harness.artifactDir,
		"client_type":       clientType,
		"workspace_dir":     workspaceDir,
		"program_path":      programPath,
		"agent":             agent,
		"session":           session,
		"open_probe":        openProbe,
		"where_probe":       whereProbe,
		"status_probes":     statusProbes,
		"step1_message":     step1Message,
		"step1_probe":       step1Probe,
		"step2_message":     step2Message,
		"step2_probe":       step2Probe,
		"initial_run":       initialRun,
		"final_default_run": finalDefaultRun,
		"final_arg_run":     finalArgRun,
	})
	t.Logf("%s coding artifacts: %s", clientType, harness.artifactDir)
}

func openFreshDirectSessionForAgent(
	ctx context.Context,
	harness *liveAgentHarness,
	session liveProjectSession,
	agent map[string]any,
	label string,
) liveProjectSession {
	harness.t.Helper()
	peerID := asString(agent["id"])
	require.NotEmpty(harness.t, peerID, "selected agent has empty id")

	data := harness.apiJSON(ctx, label, http.MethodPost, "/sessions/create", session.Token, map[string]any{
		"peer_id":   peerID,
		"peer_type": 2,
	})
	sessionID := asString(data["session_id"])
	require.NotEmpty(harness.t, sessionID, "session create returned empty session_id")

	result := session
	result.SessionID = sessionID
	result.AgentID = peerID
	result.Agent = cloneMap(agent)
	result.SessionStrategy = "create"
	return result
}

func mustCreateTempWorkspace(t *testing.T, clientType string) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "grix-live-"+sanitizeFileName(clientType)+"-")
	require.NoError(t, err)
	initCmd := exec.Command("git", "init", "-q", dir)
	output, initErr := initCmd.CombinedOutput()
	require.NoError(t, initErr, "git init temp workspace failed: %s", string(output))
	t.Cleanup(func() {
		_ = os.RemoveAll(dir)
	})
	return dir
}

func waitForAgentWorkspaceReady(
	ctx context.Context,
	t *testing.T,
	harness *liveAgentHarness,
	client *liveUserWSClient,
	clientType string,
	workspaceDir string,
	timeout time.Duration,
) []liveConversationResult {
	t.Helper()
	require.NotEmpty(t, strings.TrimSpace(clientType), "client type is required")
	require.NotEmpty(t, strings.TrimSpace(workspaceDir), "workspace dir is required")

	deadline := time.Now().Add(timeout)
	results := make([]liveConversationResult, 0, 3)
	for {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			if harness != nil {
				harness.writeJSON("workspace-status-probes.json", map[string]any{
					"workspace_dir": workspaceDir,
					"status_probes": results,
					"failure":       "workspace did not become ready before timeout",
				})
			}
			t.Fatalf("timed out waiting for workspace %s to become ready", workspaceDir)
		}

		probe := client.runConversationProbe(ctx, liveConversationProbeOptions{
			Message:              "/grix status",
			ExpectedSenderID:     client.session.AgentID,
			ExpectedTextContains: "grix://card/agent_status",
			Timeout:              minDuration(remaining, 25*time.Second),
		})
		results = append(results, probe)
		if detailText := extractAgentStatusDetailText(firstProbeContent(probe)); isAgentWorkspaceReady(clientType, workspaceDir, detailText) {
			return results
		}

		select {
		case <-ctx.Done():
			if harness != nil {
				harness.writeJSON("workspace-status-probes.json", map[string]any{
					"workspace_dir": workspaceDir,
					"status_probes": results,
					"failure":       ctx.Err().Error(),
				})
			}
			t.Fatalf("waiting for workspace %s readiness canceled: %v", workspaceDir, ctx.Err())
		case <-time.After(3 * time.Second):
		}
	}
}

func waitForFileContent(ctx context.Context, t *testing.T, path string, timeout time.Duration) string {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		select {
		case <-ctx.Done():
			t.Fatalf("waiting for file %s canceled: %v", path, ctx.Err())
		default:
		}

		content, err := os.ReadFile(path)
		if err == nil && len(bytes.TrimSpace(content)) > 0 {
			return string(content)
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for file %s to be populated", path)
		}
		time.Sleep(1500 * time.Millisecond)
	}
}

func firstProbeContent(result liveConversationResult) string {
	for _, payload := range result.AgentPushes {
		if content := firstNonEmpty(asString(payload["content"]), asString(payload["final_content"])); content != "" {
			return content
		}
	}
	for _, item := range result.Events {
		payload, _ := item["payload"].(map[string]any)
		if content := firstNonEmpty(asString(payload["content"]), asString(payload["final_content"])); content != "" {
			return content
		}
	}
	return ""
}

func extractAgentStatusDetailText(markdown string) string {
	raw := strings.TrimSpace(markdown)
	if raw == "" {
		return ""
	}
	start := strings.Index(raw, "(grix://card/agent_status?")
	if start < 0 {
		return ""
	}
	start++
	end := strings.Index(raw[start:], ")")
	if end < 0 {
		return ""
	}
	cardURI := raw[start : start+end]
	parsed, err := url.Parse(cardURI)
	if err != nil {
		return ""
	}
	return parsed.Query().Get("detail_text")
}

func isAgentWorkspaceReady(clientType, workspaceDir, detailText string) bool {
	if !strings.Contains(detailText, workspaceDir) {
		return false
	}

	workerStatus := extractAgentStatusWorkerStatus(detailText)
	if strings.EqualFold(workerStatus, "ready") {
		return true
	}

	// Codex marks the bridge "ready" only after the first real JSON-RPC roundtrip.
	// After /grix open succeeds, a matching workspace plus an active "starting"
	// bridge is already usable for the first turn, so waiting for "ready" here
	// would deadlock the live scenario before that first turn is sent.
	return strings.EqualFold(strings.TrimSpace(clientType), "codex") && strings.EqualFold(workerStatus, "starting")
}

func extractAgentStatusWorkerStatus(detailText string) string {
	for _, line := range strings.Split(detailText, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "Worker:") {
			continue
		}
		return strings.TrimSpace(strings.TrimPrefix(line, "Worker:"))
	}
	return ""
}

type localRunResult struct {
	Command       []string `json:"command"`
	Dir           string   `json:"dir"`
	Stdout        string   `json:"stdout"`
	Stderr        string   `json:"stderr"`
	StdoutTrimmed string   `json:"stdout_trimmed"`
}

func runGoProgram(t *testing.T, ctx context.Context, workdir string, args ...string) localRunResult {
	t.Helper()

	runCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	cmdArgs := append([]string{"run", "main.go"}, args...)
	cmd := exec.CommandContext(runCtx, "go", cmdArgs...)
	cmd.Dir = workdir
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	require.NoError(t, err, "go run main.go %v failed: %s", args, stderr.String())

	return localRunResult{
		Command:       append([]string{"go"}, cmdArgs...),
		Dir:           workdir,
		Stdout:        stdout.String(),
		Stderr:        stderr.String(),
		StdoutTrimmed: strings.TrimSpace(stdout.String()),
	}
}
