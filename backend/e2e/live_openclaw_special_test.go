package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type liveOpenClawSpecialConfig struct {
	AccountID           string
	GrixDir             string
	GrixPluginID        string
	AdminSkillNames     []string
	SourceLoadActive    bool
	OpenClawConfigPath  string
	GatewayLogPath      string
	InstallProfileName  string
	InstallProfileDir   string
	SkipBuild           bool
	SkipRestart         bool
	CleanupProfile      bool
	PluginTarget        string
	PluginMessage       string
	DeleteAfterSend     bool
	ExecApprovalPrompt  string
	SkipApprovalFixture bool
}

type liveOpenClawSpecialHarness struct {
	t    *testing.T
	base *liveAgentHarness
	cfg  liveOpenClawSpecialConfig
}

func TestLiveOpenClawPluginLoadAndChannel(t *testing.T) {
	baseCfg := loadLiveAgentConfig(t)
	if !baseCfg.Enabled {
		t.Skip("set GRIX_LIVE_E2E=1 to enable live OpenClaw special E2E")
	}

	cfg := loadLiveOpenClawSpecialConfig(t, baseCfg)
	h := newLiveOpenClawSpecialHarness(t, baseCfg, cfg)
	ctx, cancel := context.WithTimeout(context.Background(), liveOpenClawSpecialTimeout(cfg, 90*time.Second))
	defer cancel()

	h.runPluginBaseline(ctx)
	h.writeSummary("openclaw-plugin-load-and-channel-summary.json", map[string]any{
		"generated_at": time.Now().UTC().Format(time.RFC3339),
		"artifacts":    h.base.artifactDir,
		"account_id":   cfg.AccountID,
		"grix_dir":     cfg.GrixDir,
		"skip_build":   cfg.SkipBuild,
		"skip_restart": cfg.SkipRestart,
	})
}

func TestLiveOpenClawPluginSendMessageSmoke(t *testing.T) {
	baseCfg := loadLiveAgentConfig(t)
	if !baseCfg.Enabled {
		t.Skip("set GRIX_LIVE_E2E=1 to enable live OpenClaw special E2E")
	}

	cfg := loadLiveOpenClawSpecialConfig(t, baseCfg)
	h := newLiveOpenClawSpecialHarness(t, baseCfg, cfg)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	h.checkChannels(ctx)
	h.checkMessageSendSkillSurface(ctx)
	h.checkMessageSendToolAllowance()

	summary := map[string]any{
		"generated_at": time.Now().UTC().Format(time.RFC3339),
		"artifacts":    h.base.artifactDir,
		"account_id":   cfg.AccountID,
		"send_surface": "grix_message_send",
		"skill_name":   "message-send",
		"note":         "current grix outbound send runs through the delegated grix_message_send skill tool, not standalone openclaw message send CLI",
	}

	h.writeSummary("openclaw-plugin-live-send-summary.json", summary)
}

func TestLiveOpenClawPluginInstallProfile(t *testing.T) {
	baseCfg := loadLiveAgentConfig(t)
	if !baseCfg.Enabled {
		t.Skip("set GRIX_LIVE_E2E=1 to enable live OpenClaw special E2E")
	}

	cfg := loadLiveOpenClawSpecialConfig(t, baseCfg)
	if cfg.SourceLoadActive {
		t.Skipf("skip real profile install on source-loaded developer environment: %s", cfg.GrixDir)
	}
	h := newLiveOpenClawSpecialHarness(t, baseCfg, cfg)
	ctx, cancel := context.WithTimeout(context.Background(), liveOpenClawSpecialTimeout(cfg, 2*time.Minute))
	defer cancel()

	grixTarball := h.packPlugin(ctx, "grix", cfg.GrixDir)

	require.NoError(t, os.RemoveAll(cfg.InstallProfileDir))
	if cfg.CleanupProfile {
		t.Cleanup(func() {
			_ = os.RemoveAll(cfg.InstallProfileDir)
		})
	}

	h.installPluginIntoProfile(ctx, h.cfg.GrixPluginID, grixTarball)
	h.enablePluginInProfile(ctx, h.cfg.GrixPluginID)
	h.checkInstallProfilePlugins(ctx)
	h.checkInstallProfileSkills(ctx)
	h.checkInstallProfileConfig()

	h.writeSummary("openclaw-plugin-install-profile-summary.json", map[string]any{
		"generated_at":    time.Now().UTC().Format(time.RFC3339),
		"artifacts":       h.base.artifactDir,
		"profile_name":    cfg.InstallProfileName,
		"profile_dir":     cfg.InstallProfileDir,
		"cleanup_profile": cfg.CleanupProfile,
		"grix_tarball":    grixTarball,
	})
}

func TestLiveOpenClawExecApprovalPreflight(t *testing.T) {
	baseCfg := loadLiveAgentConfig(t)
	if !baseCfg.Enabled {
		t.Skip("set GRIX_LIVE_E2E=1 to enable live OpenClaw special E2E")
	}

	cfg := loadLiveOpenClawSpecialConfig(t, baseCfg)
	h := newLiveOpenClawSpecialHarness(t, baseCfg, cfg)
	ctx, cancel := context.WithTimeout(context.Background(), liveOpenClawSpecialTimeout(cfg, 90*time.Second))
	defer cancel()

	if !cfg.SkipBuild {
		h.buildPlugin(ctx, "grix", cfg.GrixDir)
	}
	h.checkExecApprovalConfig()
	if !cfg.SkipApprovalFixture {
		originalApprovals := h.captureGatewayApprovalSnapshot(ctx, "openclaw-exec-approvals-before")
		t.Cleanup(func() {
			if len(originalApprovals) == 0 {
				return
			}
			cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cleanupCancel()
			h.restoreGatewayApprovalSnapshot(cleanupCtx, originalApprovals)
		})
		h.applyGatewayApprovalFixture(ctx)
	}
	h.checkGatewayApprovalFixture(ctx)
	if !cfg.SkipRestart {
		h.restartGateway(ctx, "openclaw-exec-approval-gateway-restart")
	}
	h.checkGrixPlugin(ctx)
	h.checkChannels(ctx)
	h.requireFile(cfg.GatewayLogPath)

	prompt := strings.TrimSpace(cfg.ExecApprovalPrompt)
	if prompt == "" {
		prompt = "请调用 exec 工具，在 gateway host 上执行 `/bin/echo grix exec approval smoke`。如果需要审批，不要改命令，也不要绕过审批，直接把审批请求发回聊天。"
	}
	h.base.writeText("openclaw-exec-approval-manual.txt", strings.TrimSpace(fmt.Sprintf(`
Frontend manual steps:
1. 打开 Flutter/macOS 前端，并进入绑定到当前 OpenClaw 主 agent 的 Grix 会话。
2. 发送下面这条消息触发真实 approval-pending：

%s

3. 观察聊天页：
   - 出现 exec approval 卡片，而不是普通文本
   - 卡片包含 Allow Once / Allow Always / Deny
4. 点击 Allow Once，继续观察：
   - 原卡按钮消失
   - 原卡切换到结果态
   - 后续 finished/denied 状态继续合并在同一张卡下
   - 不出现审批正文被拆成尾部散文本
5. 如需走拒绝路径，再重复一次并点击 Deny。
`, prompt)))

	h.writeSummary("openclaw-exec-approval-summary.json", map[string]any{
		"generated_at":    time.Now().UTC().Format(time.RFC3339),
		"artifacts":       h.base.artifactDir,
		"account_id":      cfg.AccountID,
		"prompt":          prompt,
		"gateway_log":     cfg.GatewayLogPath,
		"openclaw_config": cfg.OpenClawConfigPath,
	})
}

func loadLiveOpenClawSpecialConfig(t *testing.T, baseCfg liveAgentConfig) liveOpenClawSpecialConfig {
	t.Helper()

	repoRoot := baseCfg.RepoRoot
	if strings.TrimSpace(repoRoot) == "" {
		repoRoot = findRepoRoot(t)
	}
	parentDir := filepath.Dir(repoRoot)
	grandParentDir := filepath.Dir(parentDir)

	defaultGrixDir := firstExistingDir(
		filepath.Join(grandParentDir, "grix-openclaw"),
		filepath.Join(parentDir, "grix-openclaw"),
		filepath.Join(grandParentDir, "clawpool-openclaw-brand-grix"),
		filepath.Join(parentDir, "clawpool-openclaw-brand-grix"),
		filepath.Join(grandParentDir, "clawpool-openclaw"),
		filepath.Join(parentDir, "clawpool-openclaw"),
	)
	grixDir := firstNonEmpty(strings.TrimSpace(os.Getenv("GRIX_LIVE_PLUGIN_GRIX_DIR")), strings.TrimSpace(os.Getenv("OPENCLAW_GRIX_DIR")), defaultGrixDir)
	grixPluginID, grixSkillNames := readOpenClawPluginMetadata(t, grixDir)
	profileName := envOrDefault("GRIX_LIVE_PLUGIN_PROFILE", "plugin-install-e2e")
	configPath := filepath.Join(os.Getenv("HOME"), ".openclaw", "openclaw.json")
	return liveOpenClawSpecialConfig{
		AccountID:           firstNonEmpty(baseCfg.OpenClawAccount, envOrDefault("GRIX_LIVE_PLUGIN_ACCOUNT", "default"), "default"),
		GrixDir:             grixDir,
		GrixPluginID:        firstNonEmpty(grixPluginID, "grix"),
		AdminSkillNames:     grixSkillNames,
		SourceLoadActive:    isRepoPluginSourceLoaded(configPath, grixDir),
		OpenClawConfigPath:  configPath,
		GatewayLogPath:      filepath.Join(os.Getenv("HOME"), ".openclaw", "logs", "gateway.log"),
		InstallProfileName:  profileName,
		InstallProfileDir:   filepath.Join(os.Getenv("HOME"), ".openclaw-"+profileName),
		SkipBuild:           strings.TrimSpace(os.Getenv("GRIX_LIVE_PLUGIN_SKIP_BUILD")) == "1",
		SkipRestart:         strings.TrimSpace(os.Getenv("GRIX_LIVE_PLUGIN_SKIP_RESTART")) == "1",
		CleanupProfile:      strings.TrimSpace(os.Getenv("GRIX_LIVE_PLUGIN_CLEANUP_PROFILE")) == "1",
		PluginTarget:        strings.TrimSpace(os.Getenv("GRIX_LIVE_PLUGIN_TARGET")),
		PluginMessage:       strings.TrimSpace(os.Getenv("GRIX_LIVE_PLUGIN_MESSAGE")),
		DeleteAfterSend:     strings.TrimSpace(os.Getenv("GRIX_LIVE_PLUGIN_DELETE_AFTER_SEND")) == "1",
		ExecApprovalPrompt:  strings.TrimSpace(os.Getenv("GRIX_LIVE_EXEC_APPROVAL_PROMPT")),
		SkipApprovalFixture: strings.TrimSpace(os.Getenv("GRIX_LIVE_EXEC_APPROVAL_SKIP_FIXTURE")) == "1",
	}
}

func newLiveOpenClawSpecialHarness(t *testing.T, baseCfg liveAgentConfig, cfg liveOpenClawSpecialConfig) *liveOpenClawSpecialHarness {
	t.Helper()
	base := newLiveAgentHarness(t, baseCfg)
	return &liveOpenClawSpecialHarness{
		t:    t,
		base: base,
		cfg:  cfg,
	}
}

func liveOpenClawSpecialTimeout(cfg liveOpenClawSpecialConfig, base time.Duration) time.Duration {
	timeout := base
	if !cfg.SkipBuild {
		timeout += 6 * time.Minute
	}
	if !cfg.SkipRestart {
		timeout += 90 * time.Second
	}
	if timeout < 4*time.Minute {
		timeout = 4 * time.Minute
	}
	return timeout
}

func (h *liveOpenClawSpecialHarness) runPluginBaseline(ctx context.Context) {
	h.t.Helper()
	if !h.cfg.SkipBuild {
		h.buildPlugin(ctx, "grix", h.cfg.GrixDir)
	}
	h.checkOpenClawConfigPaths()
	if !h.cfg.SkipRestart {
		h.restartGateway(ctx, "openclaw-plugin-gateway-restart")
	}
	h.checkPlugins(ctx)
	h.checkSkills(ctx)
	h.checkChannels(ctx)
	h.checkAdminCLI(ctx)
}

func (h *liveOpenClawSpecialHarness) checkOpenClawConfigPaths() {
	h.t.Helper()
	h.requireFile(h.cfg.OpenClawConfigPath)
	raw, err := os.ReadFile(h.cfg.OpenClawConfigPath)
	require.NoError(h.t, err)

	var payload map[string]any
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.UseNumber()
	require.NoError(h.t, dec.Decode(&payload))

	plugins, _ := payload["plugins"].(map[string]any)
	loadSection, _ := plugins["load"].(map[string]any)
	loadPaths := stringsFromAny(loadSection["paths"])
	require.True(h.t, matchesPluginLoadPath(loadPaths, h.cfg.GrixDir), "plugin load paths do not include %s", h.cfg.GrixDir)
	h.base.writeJSON("openclaw-config-path-check.json", map[string]any{
		"config_path": h.cfg.OpenClawConfigPath,
		"load_paths":  loadPaths,
	})
}

func (h *liveOpenClawSpecialHarness) checkExecApprovalConfig() {
	h.t.Helper()
	h.requireFile(h.cfg.OpenClawConfigPath)
	raw, err := os.ReadFile(h.cfg.OpenClawConfigPath)
	require.NoError(h.t, err)

	var payload map[string]any
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.UseNumber()
	require.NoError(h.t, dec.Decode(&payload))

	channels, _ := payload["channels"].(map[string]any)
	grix, _ := channels["grix"].(map[string]any)
	require.NotNil(h.t, grix, "missing channels.grix")

	accountApprovals := map[string]any{}
	if accounts, _ := grix["accounts"].(map[string]any); accounts != nil {
		if accountRow, _ := accounts[h.cfg.AccountID].(map[string]any); accountRow != nil {
			accountApprovals, _ = accountRow["execApprovals"].(map[string]any)
		}
	}
	execApprovals := accountApprovals
	if execApprovals == nil {
		execApprovals, _ = grix["execApprovals"].(map[string]any)
	}
	require.True(h.t, asBool(execApprovals["enabled"]), "grix exec approvals must be enabled")
	require.NotEmpty(h.t, stringsFromAny(execApprovals["approvers"]), "grix exec approvers must be non-empty")

	approvals, _ := payload["approvals"].(map[string]any)
	execSettings, _ := approvals["exec"].(map[string]any)
	require.True(h.t, asBool(execSettings["enabled"]), "approvals.exec.enabled must be true")
	mode := asString(execSettings["mode"])
	require.Contains(h.t, []string{"session", "both"}, mode)

	tools, _ := payload["tools"].(map[string]any)
	toolExec, _ := tools["exec"].(map[string]any)
	require.Equal(h.t, "gateway", asString(toolExec["host"]))
	require.Equal(h.t, "allowlist", asString(toolExec["security"]))
	require.Equal(h.t, "always", asString(toolExec["ask"]))

	h.base.writeJSON("openclaw-exec-approval-config-check.json", payload)
}

func (h *liveOpenClawSpecialHarness) applyGatewayApprovalFixture(ctx context.Context) {
	h.t.Helper()

	fixture := map[string]any{
		"version": 1,
		"defaults": map[string]any{
			"security":        "allowlist",
			"ask":             "always",
			"askFallback":     "deny",
			"autoAllowSkills": false,
		},
		"agents": map[string]any{
			"main": map[string]any{
				"security":        "allowlist",
				"ask":             "always",
				"askFallback":     "deny",
				"autoAllowSkills": false,
				"allowlist":       []any{},
			},
		},
	}

	fixturePath := filepath.Join(h.base.artifactDir, "openclaw-exec-approvals-fixture.json")
	writeJSONFile(h.t, fixturePath, fixture)

	_, payloadAny, err := h.base.runOpenClaw(
		ctx,
		"openclaw-exec-approvals-set",
		true,
		false,
		"approvals",
		"set",
		"--gateway",
		"--file", fixturePath,
		"--json",
	)
	require.NoError(h.t, err)

	payload, _ := payloadAny.(map[string]any)
	require.NotEmpty(h.t, asString(payload["path"]))
	filePayload, _ := payload["file"].(map[string]any)
	require.Equal(h.t, "allowlist", asString(filePayload["defaults"].(map[string]any)["security"]))
}

func (h *liveOpenClawSpecialHarness) checkGatewayApprovalFixture(ctx context.Context) {
	h.t.Helper()
	_, payloadAny, err := h.base.runOpenClaw(
		ctx,
		"openclaw-exec-approvals-get",
		true,
		false,
		"approvals",
		"get",
		"--gateway",
		"--json",
	)
	require.NoError(h.t, err)

	payload, _ := payloadAny.(map[string]any)
	filePayload, _ := payload["file"].(map[string]any)
	require.NotNil(h.t, filePayload, "missing approvals snapshot file")
	defaults, _ := filePayload["defaults"].(map[string]any)
	require.Equal(h.t, "allowlist", asString(defaults["security"]))
	require.Equal(h.t, "always", asString(defaults["ask"]))
	require.Equal(h.t, "deny", asString(defaults["askFallback"]))
	mainAgent, _ := filePayload["agents"].(map[string]any)
	mainAgentRow, _ := mainAgent["main"].(map[string]any)
	require.NotNil(h.t, mainAgentRow, "gateway approvals must contain agents.main")
	require.Empty(h.t, stringsFromAny(mainAgentRow["allowlist"]))
}

func (h *liveOpenClawSpecialHarness) captureGatewayApprovalSnapshot(ctx context.Context, label string) map[string]any {
	h.t.Helper()
	_, payloadAny, err := h.base.runOpenClaw(
		ctx,
		label,
		true,
		false,
		"approvals",
		"get",
		"--gateway",
		"--json",
	)
	require.NoError(h.t, err)
	payload, _ := payloadAny.(map[string]any)
	filePayload, _ := payload["file"].(map[string]any)
	return cloneMap(filePayload)
}

func (h *liveOpenClawSpecialHarness) restoreGatewayApprovalSnapshot(ctx context.Context, snapshot map[string]any) {
	h.t.Helper()
	if len(snapshot) == 0 {
		return
	}
	restorePath := filepath.Join(h.base.artifactDir, "openclaw-exec-approvals-restore.json")
	writeJSONFile(h.t, restorePath, snapshot)
	_, _, err := h.base.runOpenClaw(
		ctx,
		"openclaw-exec-approvals-restore",
		true,
		true,
		"approvals",
		"set",
		"--gateway",
		"--file", restorePath,
		"--json",
	)
	require.NoError(h.t, err)
}

func (h *liveOpenClawSpecialHarness) restartGateway(ctx context.Context, label string) {
	h.t.Helper()
	if strings.TrimSpace(label) == "" {
		label = "openclaw-gateway-restart"
	}
	raw, _, err := h.base.runOpenClaw(ctx, label, false, false, "gateway", "restart")
	require.NoError(h.t, err)
	require.Contains(h.t, raw, "Restarted LaunchAgent")
}

func (h *liveOpenClawSpecialHarness) checkPlugins(ctx context.Context) {
	h.t.Helper()

	doctorRaw, _, err := h.base.runOpenClaw(ctx, "openclaw-plugins-doctor", false, false, "plugins", "doctor")
	require.NoError(h.t, err)
	h.base.writeText("openclaw-plugins-doctor.txt", doctorRaw)
	require.Empty(h.t, blockingPluginDoctorDiagnostics(doctorRaw), "plugin doctor reported blocking diagnostics:\n%s", doctorRaw)

	_, grixInfoAny, err := h.base.runOpenClaw(ctx, "openclaw-plugin-grix-info-special", true, false, "plugins", "info", h.cfg.GrixPluginID, "--json")
	require.NoError(h.t, err)
	grixState := openClawPluginState(mapFromAny(grixInfoAny))
	require.True(h.t, asBool(grixState["enabled"]))
	require.Equal(h.t, "loaded", asString(grixState["status"]))
	require.True(h.t, matchesPluginRuntimeSource(grixState, h.cfg.GrixDir), "grix plugin runtime source is not under %s", h.cfg.GrixDir)
	require.Contains(h.t, stringsFromAny(grixState["channelIds"]), "grix")
	require.Contains(h.t, stringsFromAny(grixState["cliCommands"]), "grix")
}

func (h *liveOpenClawSpecialHarness) checkGrixPlugin(ctx context.Context) {
	h.t.Helper()
	_, grixInfoAny, err := h.base.runOpenClaw(ctx, "openclaw-plugin-grix-info-approval", true, false, "plugins", "info", h.cfg.GrixPluginID, "--json")
	require.NoError(h.t, err)
	grixState := openClawPluginState(mapFromAny(grixInfoAny))
	require.True(h.t, asBool(grixState["enabled"]))
	require.Equal(h.t, "loaded", asString(grixState["status"]))
}

func (h *liveOpenClawSpecialHarness) checkSkills(ctx context.Context) {
	h.t.Helper()
	_, payloadAny, err := h.base.runOpenClaw(ctx, "openclaw-skills-list-special", true, false, "skills", "list", "--json")
	require.NoError(h.t, err)

	payload, _ := payloadAny.(map[string]any)
	items, _ := payload["skills"].([]any)
	eligible := make([]string, 0, len(items))
	for _, item := range items {
		row, ok := item.(map[string]any)
		if !ok || !asBool(row["eligible"]) {
			continue
		}
		eligible = append(eligible, asString(row["name"]))
	}
	for _, skillName := range h.cfg.AdminSkillNames {
		require.Contains(h.t, eligible, skillName)
	}
}

func (h *liveOpenClawSpecialHarness) checkChannels(ctx context.Context) {
	h.t.Helper()
	deadline := time.Now().Add(45 * time.Second)
	if ctxDeadline, ok := ctx.Deadline(); ok && ctxDeadline.Before(deadline) {
		deadline = ctxDeadline
	}

	var (
		lastErr     error
		lastPayload map[string]any
	)
	for {
		payload, err := h.readChannelStatus(ctx)
		if err == nil {
			lastPayload = payload
			if readyErr := h.validateChannelStatus(payload); readyErr == nil {
				return
			} else {
				lastErr = readyErr
			}
		} else {
			lastErr = err
		}

		if ctx.Err() != nil || !time.Now().Before(deadline) {
			break
		}
		sleepFor := 2 * time.Second
		if remaining := time.Until(deadline); remaining < sleepFor {
			sleepFor = remaining
		}
		if sleepFor > 0 {
			time.Sleep(sleepFor)
		}
	}

	if lastPayload != nil {
		h.base.writeJSON("openclaw-channel-status-special-final.json", lastPayload)
	}
	require.NoError(h.t, lastErr)
}

func (h *liveOpenClawSpecialHarness) checkMessageSendSkillSurface(ctx context.Context) {
	h.t.Helper()
	raw, _, err := h.base.runOpenClaw(
		ctx,
		"openclaw-plugin-live-send-skills",
		false,
		false,
		"skills",
		"list",
	)
	require.NoError(h.t, err)
	require.Contains(h.t, raw, "message-send")
	require.Contains(h.t, raw, "grix-admin")
}

func (h *liveOpenClawSpecialHarness) checkMessageSendToolAllowance() {
	h.t.Helper()
	h.requireFile(h.cfg.OpenClawConfigPath)
	raw, err := os.ReadFile(h.cfg.OpenClawConfigPath)
	require.NoError(h.t, err)

	var payload map[string]any
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.UseNumber()
	require.NoError(h.t, dec.Decode(&payload))

	tools, _ := payload["tools"].(map[string]any)
	allowed := stringsFromAny(tools["alsoAllow"])
	require.Contains(h.t, allowed, "message")
	require.Contains(h.t, allowed, "grix_message_send")
	require.Contains(h.t, allowed, "grix_message_unsend")

	sessions, _ := tools["sessions"].(map[string]any)
	visibility := asString(sessions["visibility"])
	if visibility != "" {
		require.Contains(h.t, []string{"agent", "all"}, visibility)
	}
}

func (h *liveOpenClawSpecialHarness) readChannelStatus(ctx context.Context) (map[string]any, error) {
	h.t.Helper()
	raw, _, err := h.base.runOpenClaw(
		ctx,
		"openclaw-channel-status-special",
		false,
		true,
		"channels",
		"status",
		"--probe",
		"--timeout",
		strconv.Itoa(h.base.cfg.OpenClawProbeMS),
		"--json",
	)
	if err != nil && strings.TrimSpace(raw) == "" {
		return nil, err
	}
	payloadAny, jsonErr := extractJSONPayload(raw)
	if jsonErr != nil {
		if err != nil {
			return nil, err
		}
		return nil, jsonErr
	}
	payload, _ := payloadAny.(map[string]any)
	if payload == nil {
		return nil, fmt.Errorf("channel status payload is not a JSON object")
	}
	h.base.writeJSON("openclaw-channel-status-special.json", payload)
	return payload, nil
}

func (h *liveOpenClawSpecialHarness) validateChannelStatus(payload map[string]any) error {
	channels, _ := payload["channels"].(map[string]any)
	grix, _ := channels["grix"].(map[string]any)
	if grix == nil {
		return fmt.Errorf("missing channels.grix")
	}
	if !asBool(grix["configured"]) {
		return fmt.Errorf("grix channel is not configured")
	}
	if !asBool(grix["running"]) {
		return fmt.Errorf("grix channel is not running")
	}
	if !asBool(grix["connected"]) {
		return fmt.Errorf("grix channel is not connected")
	}

	channelAccounts, _ := payload["channelAccounts"].(map[string]any)
	accounts, _ := channelAccounts["grix"].([]any)
	for _, item := range accounts {
		row, ok := item.(map[string]any)
		if !ok || asString(row["accountId"]) != h.cfg.AccountID {
			continue
		}
		if !asBool(row["enabled"]) {
			return fmt.Errorf("grix account %s is not enabled", h.cfg.AccountID)
		}
		if !asBool(row["configured"]) {
			return fmt.Errorf("grix account %s is not configured", h.cfg.AccountID)
		}
		if !asBool(row["running"]) {
			return fmt.Errorf("grix account %s is not running", h.cfg.AccountID)
		}
		if !asBool(row["connected"]) {
			return fmt.Errorf("grix account %s is not connected", h.cfg.AccountID)
		}
		return nil
	}
	return fmt.Errorf("missing grix account %s", h.cfg.AccountID)
}

func (h *liveOpenClawSpecialHarness) checkAdminCLI(ctx context.Context) {
	h.t.Helper()
	_, payloadAny, err := h.base.runOpenClaw(ctx, "openclaw-grix-admin-doctor-special", true, false, "grix", "doctor")
	require.NoError(h.t, err)

	payload, _ := payloadAny.(map[string]any)
	require.NotEmpty(h.t, asString(payload["defaultAccountId"]))
	accounts, _ := payload["accounts"].([]any)
	require.NotEmpty(h.t, accounts)
	for _, item := range accounts {
		row, ok := item.(map[string]any)
		if !ok {
			continue
		}
		require.True(h.t, asBool(row["configured"]))
		require.True(h.t, asBool(row["enabled"]))
	}
}

func (h *liveOpenClawSpecialHarness) buildPlugin(ctx context.Context, name, dir string) {
	h.t.Helper()
	h.requireDir(dir)
	h.runExternal(ctx, "build-"+name+"-npm-ci", dir, nil, false, false, "npm", "ci")
	h.runExternal(ctx, "build-"+name+"-npm-run-build", dir, nil, false, false, "npm", "run", "build")
	require.True(h.t, fileExists(filepath.Join(dir, "dist", "index.js")), "missing built entry for %s", name)
}

func (h *liveOpenClawSpecialHarness) packPlugin(ctx context.Context, name, dir string) string {
	h.t.Helper()
	h.requireDir(dir)
	if !h.cfg.SkipBuild {
		h.runExternal(ctx, "pack-"+name+"-npm-ci", dir, nil, false, false, "npm", "ci")
		h.runExternal(ctx, "pack-"+name+"-npm-run-build", dir, nil, false, false, "npm", "run", "build")
	}
	raw, _, err := h.runExternal(ctx, "pack-"+name+"-npm-pack", dir, nil, false, false, "npm", "pack", "--ignore-scripts")
	require.NoError(h.t, err)
	tarballName := lastNonEmptyLine(raw)
	require.NotEmpty(h.t, tarballName, "pack %s did not emit tarball name", name)
	tarballPath := filepath.Join(dir, tarballName)
	require.True(h.t, fileExists(tarballPath), "missing tarball %s", tarballPath)
	return tarballPath
}

func (h *liveOpenClawSpecialHarness) installPluginIntoProfile(ctx context.Context, name, tarball string) {
	h.t.Helper()
	raw, _, err := h.base.runOpenClaw(
		ctx,
		"openclaw-profile-install-"+name,
		false,
		false,
		"--profile", h.cfg.InstallProfileName,
		"plugins", "install", "--force", tarball,
	)
	if err != nil && strings.Contains(raw, "plugin already exists:") {
		h.base.writeText("openclaw-profile-install-"+name+"-reused.txt", raw)
		return
	}
	require.NoError(h.t, err)
	require.Contains(h.t, raw, "Installed plugin: "+name)
}

func (h *liveOpenClawSpecialHarness) enablePluginInProfile(ctx context.Context, name string) {
	h.t.Helper()
	raw, _, err := h.base.runOpenClaw(
		ctx,
		"openclaw-profile-enable-"+name,
		false,
		false,
		"--profile", h.cfg.InstallProfileName,
		"plugins", "enable", name,
	)
	require.NoError(h.t, err)
	require.Contains(h.t, raw, `Enabled plugin "`+name+`"`)
}

func (h *liveOpenClawSpecialHarness) checkInstallProfilePlugins(ctx context.Context) {
	h.t.Helper()

	doctorRaw, _, err := h.base.runOpenClaw(
		ctx,
		"openclaw-profile-plugins-doctor",
		false,
		false,
		"--profile", h.cfg.InstallProfileName,
		"plugins", "doctor",
	)
	require.NoError(h.t, err)
	require.Contains(h.t, doctorRaw, "No plugin issues detected.")

	listRaw, _, err := h.base.runOpenClaw(
		ctx,
		"openclaw-profile-plugins-list",
		false,
		false,
		"--profile", h.cfg.InstallProfileName,
		"plugins", "list",
	)
	require.NoError(h.t, err)
	require.Contains(h.t, listRaw, "global:"+h.cfg.GrixPluginID+"/dist/index.js")

	grixRaw, _, err := h.base.runOpenClaw(
		ctx,
		"openclaw-profile-grix-info",
		false,
		false,
		"--profile", h.cfg.InstallProfileName,
		"plugins", "info", h.cfg.GrixPluginID,
	)
	require.NoError(h.t, err)
	require.Contains(h.t, grixRaw, "Status: loaded")
	require.Contains(h.t, grixRaw, "Origin: global")
	require.Contains(h.t, grixRaw, "Install: archive")
}

func (h *liveOpenClawSpecialHarness) checkInstallProfileSkills(ctx context.Context) {
	h.t.Helper()
	raw, _, err := h.base.runOpenClaw(
		ctx,
		"openclaw-profile-skills-list",
		false,
		false,
		"--profile", h.cfg.InstallProfileName,
		"skills", "list",
	)
	require.NoError(h.t, err)
	for _, skillName := range h.cfg.AdminSkillNames {
		require.Contains(h.t, raw, skillName)
	}
}

func (h *liveOpenClawSpecialHarness) checkInstallProfileConfig() {
	h.t.Helper()
	configPath := filepath.Join(h.cfg.InstallProfileDir, "openclaw.json")
	h.requireFile(configPath)

	raw, err := os.ReadFile(configPath)
	require.NoError(h.t, err)

	var payload map[string]any
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.UseNumber()
	require.NoError(h.t, dec.Decode(&payload))

	plugins, _ := payload["plugins"].(map[string]any)
	installs, _ := plugins["installs"].(map[string]any)
	entries, _ := plugins["entries"].(map[string]any)
	for _, id := range []string{h.cfg.GrixPluginID} {
		install, _ := installs[id].(map[string]any)
		require.NotNil(h.t, install, "missing plugin install record %s", id)
		require.Equal(h.t, "archive", asString(install["source"]))
		installPath := asString(install["installPath"])
		profileExtensionsDir := filepath.Join(h.cfg.InstallProfileDir, "extensions") + string(os.PathSeparator)
		globalExtensionsDir := filepath.Join(os.Getenv("HOME"), ".openclaw", "extensions") + string(os.PathSeparator)
		require.True(
			h.t,
			strings.HasPrefix(installPath, profileExtensionsDir) || strings.HasPrefix(installPath, globalExtensionsDir),
			"install path %s is outside profile/global extension roots",
			installPath,
		)
		entry, _ := entries[id].(map[string]any)
		require.True(h.t, asBool(entry["enabled"]))
	}
	h.base.writeJSON("openclaw-profile-config-check.json", payload)
}

func (h *liveOpenClawSpecialHarness) runExternal(
	ctx context.Context,
	label string,
	dir string,
	env map[string]string,
	expectJSON bool,
	allowFailure bool,
	command string,
	args ...string,
) (string, any, error) {
	h.t.Helper()

	cmd := exec.CommandContext(ctx, command, args...)
	cmd.Dir = dir
	if len(env) > 0 {
		cmd.Env = append(os.Environ(), envMapToList(env)...)
	}
	output, err := cmd.CombinedOutput()
	raw := string(output)
	h.base.writeText(label+".log", raw)
	if err != nil && !allowFailure {
		return raw, nil, fmt.Errorf("%s failed: %w", label, err)
	}
	if !expectJSON {
		return raw, nil, err
	}
	payload, jsonErr := extractJSONPayload(raw)
	if jsonErr != nil {
		return raw, nil, jsonErr
	}
	h.base.writeJSON(label+".json", payload)
	return raw, payload, err
}

func (h *liveOpenClawSpecialHarness) requireDir(path string) {
	h.t.Helper()
	info, err := os.Stat(path)
	require.NoError(h.t, err)
	require.True(h.t, info.IsDir(), "%s is not a directory", path)
}

func (h *liveOpenClawSpecialHarness) requireFile(path string) {
	h.t.Helper()
	info, err := os.Stat(path)
	require.NoError(h.t, err)
	require.False(h.t, info.IsDir(), "%s is a directory", path)
}

func (h *liveOpenClawSpecialHarness) fileLineCount(path string) int {
	h.t.Helper()
	raw, err := os.ReadFile(path)
	require.NoError(h.t, err)
	if len(raw) == 0 {
		return 0
	}
	return len(strings.Split(string(raw), "\n"))
}

func (h *liveOpenClawSpecialHarness) fileTailFromLine(path string, lineNumber int) string {
	h.t.Helper()
	raw, err := os.ReadFile(path)
	require.NoError(h.t, err)
	lines := strings.Split(string(raw), "\n")
	if lineNumber < 0 || lineNumber >= len(lines) {
		return ""
	}
	return strings.Join(lines[lineNumber:], "\n")
}

func (h *liveOpenClawSpecialHarness) writeSummary(name string, payload any) {
	h.t.Helper()
	h.base.writeJSON(name, payload)
}

func mapFromAny(value any) map[string]any {
	if row, _ := value.(map[string]any); row != nil {
		return row
	}
	return map[string]any{}
}

func stringsFromAny(value any) []string {
	switch v := value.(type) {
	case nil:
		return nil
	case []string:
		return uniqueTrimmedStrings(v)
	case []any:
		result := make([]string, 0, len(v))
		for _, item := range v {
			if text := asString(item); text != "" {
				result = append(result, text)
			}
		}
		return uniqueTrimmedStrings(result)
	default:
		if text := asString(v); text != "" {
			return []string{text}
		}
		return nil
	}
}

func envMapToList(values map[string]string) []string {
	result := make([]string, 0, len(values))
	for key, value := range values {
		result = append(result, key+"="+value)
	}
	return result
}

func lastNonEmptyLine(raw string) string {
	lines := strings.Split(raw, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if trimmed := strings.TrimSpace(lines[i]); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func firstExistingDir(values ...string) string {
	for _, value := range values {
		info, err := os.Stat(value)
		if err == nil && info.IsDir() {
			return value
		}
	}
	return firstNonEmpty(values...)
}

func matchesPluginLoadPath(loadPaths []string, repoDir string) bool {
	repoDir = strings.TrimSpace(repoDir)
	if repoDir == "" {
		return false
	}
	cleanRepoDir := canonicalPath(repoDir)
	if cleanRepoDir == "" {
		cleanRepoDir = filepath.Clean(repoDir)
	}
	for _, item := range loadPaths {
		cleanItem := canonicalPath(strings.TrimSpace(item))
		if cleanItem == "" {
			cleanItem = filepath.Clean(strings.TrimSpace(item))
		}
		if cleanItem == cleanRepoDir {
			return true
		}
		if strings.HasPrefix(cleanItem, cleanRepoDir+string(os.PathSeparator)) {
			return true
		}
	}
	return false
}

func matchesPluginRuntimeSource(pluginState map[string]any, repoDir string) bool {
	repoDir = strings.TrimSpace(repoDir)
	if repoDir == "" {
		return false
	}
	for _, candidate := range []string{asString(pluginState["source"]), asString(pluginState["rootDir"])} {
		if candidate == "" {
			continue
		}
		if matchesPluginLoadPath([]string{candidate}, repoDir) {
			return true
		}
	}
	return false
}

func canonicalPath(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	absValue, err := filepath.Abs(value)
	if err != nil {
		absValue = value
	}
	if resolved, err := filepath.EvalSymlinks(absValue); err == nil {
		return filepath.Clean(resolved)
	}
	return filepath.Clean(absValue)
}

func blockingPluginDoctorDiagnostics(raw string) []string {
	lines := strings.Split(raw, "\n")
	diagnostics := make([]string, 0, len(lines))
	inDiagnostics := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if strings.HasPrefix(trimmed, "Diagnostics:") {
			inDiagnostics = true
			continue
		}
		if !inDiagnostics {
			continue
		}
		if strings.HasPrefix(trimmed, "Docs: ") {
			break
		}
		normalized := strings.TrimSpace(strings.TrimPrefix(trimmed, "-"))
		if normalized == "" || isNonBlockingPluginDoctorDiagnostic(normalized) {
			continue
		}
		diagnostics = append(diagnostics, normalized)
	}
	return diagnostics
}

func isNonBlockingPluginDoctorDiagnostic(line string) bool {
	return strings.Contains(line, "duplicate plugin id detected; global plugin will be overridden by config plugin") ||
		strings.Contains(line, "memory embedding provider already registered: ollama (owner: ollama)")
}

func isRepoPluginSourceLoaded(configPath, repoDir string) bool {
	if strings.TrimSpace(configPath) == "" || strings.TrimSpace(repoDir) == "" {
		return false
	}
	raw, err := os.ReadFile(configPath)
	if err != nil {
		return false
	}
	var payload map[string]any
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.UseNumber()
	if err := dec.Decode(&payload); err != nil {
		return false
	}
	plugins, _ := payload["plugins"].(map[string]any)
	loadSection, _ := plugins["load"].(map[string]any)
	return matchesPluginLoadPath(stringsFromAny(loadSection["paths"]), repoDir)
}

func readOpenClawPluginMetadata(t *testing.T, repoDir string) (string, []string) {
	t.Helper()

	type pluginManifest struct {
		ID string `json:"id"`
	}

	manifestPath := filepath.Join(repoDir, "openclaw.plugin.json")
	raw, err := os.ReadFile(manifestPath)
	require.NoError(t, err)

	var manifest pluginManifest
	require.NoError(t, json.Unmarshal(raw, &manifest))

	skillRoot := filepath.Join(repoDir, "skills")
	entries, err := os.ReadDir(skillRoot)
	if err != nil {
		if os.IsNotExist(err) {
			return strings.TrimSpace(manifest.ID), nil
		}
		require.NoError(t, err)
	}

	skills := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			skills = append(skills, entry.Name())
		}
	}
	return strings.TrimSpace(manifest.ID), uniqueTrimmedStrings(skills)
}

func findMessageID(payload any) string {
	switch v := payload.(type) {
	case map[string]any:
		for _, key := range []string{"messageId", "msg_id", "client_msg_id"} {
			if text := asString(v[key]); text != "" {
				return text
			}
		}
		for _, value := range v {
			if result := findMessageID(value); result != "" {
				return result
			}
		}
	case []any:
		for _, item := range v {
			if result := findMessageID(item); result != "" {
				return result
			}
		}
	}
	return ""
}

func findMessageIDInMap(payload any, key string) string {
	row, _ := payload.(map[string]any)
	if row == nil {
		return ""
	}
	return asString(row[key])
}
