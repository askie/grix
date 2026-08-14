package e2e

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/askie/grix/backend/internal/agentreceive"
	"github.com/askie/grix/backend/internal/ws/protocol"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/require"
)

const (
	liveMinOpenClawVersion = "2026.3.23-1"
	liveDefaultAPIBase     = "http://127.0.0.1:27180/v1"
)

var liveNumberPattern = regexp.MustCompile(`^-?\d+(\.\d+)?$`)

type liveAgentConfig struct {
	Enabled             bool
	StrictVersion       bool
	CleanupGroups       bool
	RepoRoot            string
	ArtifactRoot        string
	APIBase             string
	UserToken           string
	UserAccount         string
	UserPassword        string
	DeviceID            string
	Platform            string
	SessionStrategy     string
	ConversationTimeout time.Duration
	OpenClawProbeMS     int
	OpenClawAgent       string
	OpenClawAccount     string
	RemoteAgentID       string
	OpenClawBin         string
	OpenClawRepo        string
}

type liveCheckCase struct {
	CaseID   string         `json:"case_id"`
	Title    string         `json:"title"`
	Status   string         `json:"status"`
	Severity string         `json:"severity"`
	Detail   string         `json:"detail"`
	Data     map[string]any `json:"data,omitempty"`
}

type livePreflightSummary struct {
	GeneratedAt     string          `json:"generated_at"`
	ArtifactsDir    string          `json:"artifacts_dir"`
	OpenClawCommand []string        `json:"openclaw_command"`
	Agent           string          `json:"agent"`
	Account         string          `json:"account"`
	RemoteAgentID   string          `json:"remote_agent_id"`
	Cases           []liveCheckCase `json:"cases"`
}

type liveProjectSession struct {
	APIBase         string         `json:"api_base"`
	WSURL           string         `json:"ws_url"`
	Token           string         `json:"-"`
	TokenPreview    string         `json:"token_preview"`
	DeviceID        string         `json:"device_id"`
	Platform        string         `json:"platform"`
	SessionID       string         `json:"session_id"`
	SessionStrategy string         `json:"session_strategy"`
	AgentID         string         `json:"agent_id"`
	Agent           map[string]any `json:"agent,omitempty"`
	User            map[string]any `json:"user,omitempty"`
}

type liveConversationProbeOptions struct {
	Message              string
	ExpectedSenderID     string
	ExpectedTextContains string
	Timeout              time.Duration
}

type liveConversationResult struct {
	TriggerMsgID     string           `json:"trigger_msg_id"`
	SendAck          map[string]any   `json:"send_ack"`
	DeliveryStatuses []map[string]any `json:"delivery_statuses"`
	OutputStatuses   []map[string]any `json:"output_statuses"`
	AgentPushes      []map[string]any `json:"agent_pushes"`
	Events           []map[string]any `json:"events"`
}

type liveSendMessageOptions struct {
	Content         string
	Extra           map[string]any
	QuotedMessageID string
}

type liveMultiConversationProbeOptions struct {
	Message              string
	Extra                map[string]any
	QuotedMessageID      string
	ExpectedSenderIDs    []string
	ForbiddenSenderIDs   []string
	ExpectedTextContains string
	Timeout              time.Duration
	PostMatchQuietWindow time.Duration
}

type liveMultiConversationResult struct {
	TriggerMsgID       string                      `json:"trigger_msg_id"`
	SendAck            map[string]any              `json:"send_ack"`
	DeliveryStatuses   []map[string]any            `json:"delivery_statuses"`
	OutputStatuses     []map[string]any            `json:"output_statuses"`
	AgentPushes        []map[string]any            `json:"agent_pushes"`
	Events             []map[string]any            `json:"events"`
	RepliesBySender    map[string][]map[string]any `json:"replies_by_sender"`
	ExpectedSenderIDs  []string                    `json:"expected_sender_ids"`
	ForbiddenSenderIDs []string                    `json:"forbidden_sender_ids,omitempty"`
}

type liveGroupSession struct {
	SessionID string           `json:"session_id"`
	GroupName string           `json:"group_name"`
	AgentID   string           `json:"agent_id,omitempty"`
	AgentIDs  []string         `json:"agent_ids,omitempty"`
	Agents    []map[string]any `json:"agents,omitempty"`
}

type liveAgentHarness struct {
	t            *testing.T
	cfg          liveAgentConfig
	artifactDir  string
	httpClient   *http.Client
	openclawCmd  []string
	openclawInit bool
	randomSeed   int64
	randomPicker *rand.Rand
}

type liveUserWSClient struct {
	harness  *liveAgentHarness
	t        *testing.T
	session  liveProjectSession
	conn     *websocket.Conn
	seq      int64
	userID   string
	userID64 int64
}

func loadLiveAgentConfig(t *testing.T) liveAgentConfig {
	t.Helper()

	repoRoot := findRepoRoot(t)
	deviceID := strings.TrimSpace(os.Getenv("GRIX_LIVE_DEVICE_ID"))
	if deviceID == "" {
		deviceID = "go-live-e2e-device"
	}
	platform := strings.TrimSpace(os.Getenv("GRIX_LIVE_PLATFORM"))
	if platform == "" {
		platform = "go-e2e"
	}
	openclawRepo := strings.TrimSpace(os.Getenv("GRIX_LIVE_OPENCLAW_REPO"))
	if openclawRepo == "" {
		openclawRepo = filepath.Join(filepath.Dir(repoRoot), "openclaw")
	}

	return liveAgentConfig{
		Enabled:             strings.TrimSpace(os.Getenv("GRIX_LIVE_E2E")) == "1",
		StrictVersion:       strings.TrimSpace(os.Getenv("GRIX_LIVE_STRICT_VERSION")) == "1",
		CleanupGroups:       strings.TrimSpace(os.Getenv("GRIX_LIVE_CLEANUP_GROUPS")) == "1",
		RepoRoot:            repoRoot,
		ArtifactRoot:        envOrDefault("GRIX_LIVE_ARTIFACT_ROOT", filepath.Join(repoRoot, "release_artifacts", "openclaw-e2e-go")),
		APIBase:             envOrDefault("GRIX_LIVE_API_BASE", liveDefaultAPIBase),
		UserToken:           strings.TrimSpace(os.Getenv("GRIX_LIVE_USER_TOKEN")),
		UserAccount:         strings.TrimSpace(os.Getenv("GRIX_LIVE_USER_ACCOUNT")),
		UserPassword:        strings.TrimSpace(os.Getenv("GRIX_LIVE_USER_PASSWORD")),
		DeviceID:            deviceID,
		Platform:            platform,
		SessionStrategy:     envOrDefault("GRIX_LIVE_SESSION_STRATEGY", "open_latest"),
		ConversationTimeout: time.Duration(envInt("GRIX_LIVE_TIMEOUT_SEC", 90)) * time.Second,
		OpenClawProbeMS:     envInt("GRIX_LIVE_OPENCLAW_PROBE_TIMEOUT_MS", 5000),
		OpenClawAgent:       strings.TrimSpace(os.Getenv("GRIX_LIVE_OPENCLAW_AGENT")),
		OpenClawAccount:     strings.TrimSpace(os.Getenv("GRIX_LIVE_GRIX_ACCOUNT")),
		RemoteAgentID:       strings.TrimSpace(os.Getenv("GRIX_LIVE_REMOTE_AGENT_ID")),
		OpenClawBin:         strings.TrimSpace(os.Getenv("GRIX_LIVE_OPENCLAW_BIN")),
		OpenClawRepo:        openclawRepo,
	}
}

func newLiveAgentHarness(t *testing.T, cfg liveAgentConfig) *liveAgentHarness {
	t.Helper()

	artifactDir := filepath.Join(
		cfg.ArtifactRoot,
		time.Now().UTC().Format("20060102T150405Z"),
		sanitizeFileName(t.Name()),
	)
	require.NoError(t, os.MkdirAll(artifactDir, 0o755))
	randomSeed := time.Now().UnixNano()

	return &liveAgentHarness{
		t:           t,
		cfg:         cfg,
		artifactDir: artifactDir,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		randomSeed:   randomSeed,
		randomPicker: rand.New(rand.NewSource(randomSeed)),
	}
}

func (h *liveAgentHarness) requireOpenClawCommand() []string {
	h.t.Helper()
	if h.openclawInit {
		return append([]string{}, h.openclawCmd...)
	}

	cmd, err := resolveOpenClawCommand(h.cfg)
	require.NoError(h.t, err)
	h.openclawCmd = cmd
	h.openclawInit = true
	return append([]string{}, h.openclawCmd...)
}

func (h *liveAgentHarness) writeJSON(name string, payload any) {
	h.t.Helper()
	writeJSONFile(h.t, filepath.Join(h.artifactDir, name), payload)
}

func (h *liveAgentHarness) writeText(name, content string) {
	h.t.Helper()
	writeTextFile(h.t, filepath.Join(h.artifactDir, name), content)
}

func (h *liveAgentHarness) apiJSON(ctx context.Context, label, method, path, token string, payload any) map[string]any {
	h.t.Helper()

	var body io.Reader
	if payload != nil {
		raw, err := json.Marshal(payload)
		require.NoError(h.t, err)
		body = bytes.NewReader(raw)
		h.writeText(label+"-request.json", string(raw))
	}

	req, err := http.NewRequestWithContext(ctx, method, h.cfg.APIBase+path, body)
	require.NoError(h.t, err)
	req.Header.Set("Accept", "application/json")
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if strings.TrimSpace(token) != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := h.httpClient.Do(req)
	require.NoError(h.t, err)
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	require.NoError(h.t, err)
	h.writeText(label+"-response.json", string(raw))

	var envelope map[string]any
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	require.NoError(h.t, dec.Decode(&envelope), "decode %s response", path)

	code, err := parseFlexibleInt64(envelope["code"])
	require.NoError(h.t, err)
	require.Equalf(h.t, int64(0), code, "api %s failed: %s", path, string(raw))

	data, _ := envelope["data"].(map[string]any)
	if data == nil {
		return map[string]any{}
	}
	return data
}

func (h *liveAgentHarness) bootstrapProjectSession(ctx context.Context) liveProjectSession {
	h.t.Helper()

	session := h.bootstrapProjectIdentity(ctx)
	selectedAgent := h.resolveDirectTargetAgent(ctx, session)
	peerID := asString(selectedAgent["id"])
	require.NotEmpty(h.t, peerID, "selected agent has empty id")

	var sessionID string
	switch h.cfg.SessionStrategy {
	case "existing":
		sessionID = asString(selectedAgent["session_id"])
		require.NotEmpty(h.t, sessionID, "selected agent has empty session_id; use open_latest or create")
	case "create":
		data := h.apiJSON(ctx, "session-create", http.MethodPost, "/sessions/create", session.Token, map[string]any{
			"peer_id":   peerID,
			"peer_type": 2,
		})
		sessionID = asString(data["session_id"])
	default:
		data := h.apiJSON(ctx, "session-open-latest", http.MethodPost, "/sessions/open_latest", session.Token, map[string]any{
			"peer_id":   peerID,
			"peer_type": 2,
		})
		sessionID = asString(data["session_id"])
	}

	require.NotEmpty(h.t, sessionID, "resolved session_id is empty")
	session.SessionID = sessionID
	session.AgentID = asString(selectedAgent["id"])
	session.Agent = cloneMap(selectedAgent)
	h.writeJSON("project-bootstrap.json", session)
	return session
}

func (h *liveAgentHarness) bootstrapProjectIdentity(ctx context.Context) liveProjectSession {
	h.t.Helper()

	token := h.cfg.UserToken
	user := map[string]any{}
	if token == "" {
		require.NotEmpty(h.t, h.cfg.UserAccount, "GRIX_LIVE_USER_ACCOUNT is required when GRIX_LIVE_USER_TOKEN is empty")
		require.NotEmpty(h.t, h.cfg.UserPassword, "GRIX_LIVE_USER_PASSWORD is required when GRIX_LIVE_USER_TOKEN is empty")
		loginData := h.apiJSON(ctx, "login", http.MethodPost, "/auth/login", "", map[string]any{
			"account":   h.cfg.UserAccount,
			"password":  h.cfg.UserPassword,
			"device_id": h.cfg.DeviceID,
			"platform":  h.cfg.Platform,
		})
		token = asString(loginData["access_token"])
		require.NotEmpty(h.t, token, "login returned empty access_token")
		user, _ = loginData["user"].(map[string]any)
	}

	wsURL, err := deriveWSURL(h.cfg.APIBase)
	require.NoError(h.t, err)

	session := liveProjectSession{
		APIBase:         h.cfg.APIBase,
		WSURL:           wsURL,
		Token:           token,
		TokenPreview:    maskSecret(token),
		DeviceID:        h.cfg.DeviceID,
		Platform:        h.cfg.Platform,
		SessionStrategy: h.cfg.SessionStrategy,
		User:            user,
	}
	h.writeJSON("project-identity.json", session)
	return session
}

func (h *liveAgentHarness) resolveDirectTargetAgent(ctx context.Context, session liveProjectSession) map[string]any {
	h.t.Helper()
	require.NotEmpty(h.t, session.Token, "project session token is required")

	agents := h.fetchOnlineOpenClawAgents(ctx, session.Token, "agents-list")
	selected := h.drawRandomEligibleAgents(agents, 1, "direct-target-agent")
	return selected[0]
}

func (h *liveAgentHarness) resolveConfiguredMainAgent(ctx context.Context, session liveProjectSession) map[string]any {
	h.t.Helper()
	require.NotEmpty(h.t, session.Token, "project session token is required")
	require.NotEmpty(h.t, h.cfg.RemoteAgentID, "GRIX_LIVE_REMOTE_AGENT_ID is required for main-agent dialogue tests")

	agents := h.fetchProjectAgents(ctx, session.Token, "agents-list-main")
	h.filterOnlineOpenClawAgents(agents, "agents-list-main-online-openclaw")
	for _, row := range agents {
		if asString(row["id"]) != h.cfg.RemoteAgentID {
			continue
		}
		require.Truef(h.t, isLiveEligibleGroupAgent(row), "configured main agent %s is not an online openclaw agent", h.cfg.RemoteAgentID)
		return cloneMap(row)
	}
	h.t.Fatalf("configured main agent %s not found under current test account", h.cfg.RemoteAgentID)
	return nil
}

func (h *liveAgentHarness) openDirectSessionForAgent(
	ctx context.Context,
	session liveProjectSession,
	agent map[string]any,
) liveProjectSession {
	h.t.Helper()
	require.NotEmpty(h.t, session.Token, "project session token is required")

	peerID := asString(agent["id"])
	require.NotEmpty(h.t, peerID, "selected agent has empty id")

	var sessionID string
	switch h.cfg.SessionStrategy {
	case "existing":
		sessionID = asString(agent["session_id"])
		require.NotEmpty(h.t, sessionID, "selected agent has empty session_id; use open_latest or create")
	case "create":
		data := h.apiJSON(ctx, "session-create", http.MethodPost, "/sessions/create", session.Token, map[string]any{
			"peer_id":   peerID,
			"peer_type": 2,
		})
		sessionID = asString(data["session_id"])
	default:
		data := h.apiJSON(ctx, "session-open-latest", http.MethodPost, "/sessions/open_latest", session.Token, map[string]any{
			"peer_id":   peerID,
			"peer_type": 2,
		})
		sessionID = asString(data["session_id"])
	}
	require.NotEmpty(h.t, sessionID, "resolved session_id is empty")

	result := session
	result.SessionID = sessionID
	result.AgentID = peerID
	result.Agent = cloneMap(agent)
	return result
}

func (h *liveAgentHarness) fetchProjectAgents(ctx context.Context, token string, label string) []map[string]any {
	h.t.Helper()
	require.NotEmpty(h.t, token, "project session token is required")

	agentsData := h.apiJSON(ctx, label, http.MethodGet, "/agents/list", token, nil)
	items, _ := agentsData["list"].([]any)
	require.NotEmpty(h.t, items, "agent list is empty")

	result := make([]map[string]any, 0, len(items))
	for _, item := range items {
		row, ok := item.(map[string]any)
		if !ok {
			continue
		}
		result = append(result, row)
	}
	return result
}

func (h *liveAgentHarness) fetchOnlineOpenClawAgents(ctx context.Context, token string, label string) []map[string]any {
	h.t.Helper()
	return h.fetchOnlineAgentsByClientType(ctx, token, "openclaw", label)
}

func (h *liveAgentHarness) fetchOnlineAgentsByClientType(
	ctx context.Context,
	token string,
	clientType string,
	label string,
) []map[string]any {
	h.t.Helper()
	agents := h.fetchProjectAgents(ctx, token, label)
	return h.filterOnlineAgentsByClientType(
		agents,
		clientType,
		fmt.Sprintf("%s-online-%s", label, sanitizeFileName(strings.TrimSpace(clientType))),
	)
}

func (h *liveAgentHarness) filterOnlineOpenClawAgents(agents []map[string]any, label string) []map[string]any {
	h.t.Helper()
	return h.filterOnlineAgentsByClientType(agents, "openclaw", label)
}

func (h *liveAgentHarness) filterOnlineAgentsByClientType(
	agents []map[string]any,
	clientType string,
	label string,
) []map[string]any {
	h.t.Helper()
	require.NotEmpty(h.t, strings.TrimSpace(clientType), "client_type is required")

	candidates := make([]map[string]any, 0, len(agents))
	rejected := make([]map[string]any, 0, len(agents))
	for _, row := range agents {
		if isLiveEligibleAgentWithClientType(row, clientType) {
			candidates = append(candidates, cloneMap(row))
			continue
		}
		rejected = append(rejected, map[string]any{
			"id":                asString(row["id"]),
			"agent_name":        asString(row["agent_name"]),
			"agent_client_type": asString(row["agent_client_type"]),
			"online":            row["online"],
			"status":            row["status"],
		})
	}

	h.writeJSON(label+".json", map[string]any{
		"generated_at":    time.Now().UTC().Format(time.RFC3339),
		"candidate_count": len(candidates),
		"candidates":      candidates,
		"rejected_count":  len(rejected),
		"rejected":        rejected,
	})
	return candidates
}

func (h *liveAgentHarness) resolveOnlineAgentByClientType(
	ctx context.Context,
	session liveProjectSession,
	clientType string,
	label string,
) map[string]any {
	h.t.Helper()
	require.NotEmpty(h.t, session.Token, "project session token is required")
	require.NotEmpty(h.t, strings.TrimSpace(clientType), "client_type is required")

	agents := h.fetchOnlineAgentsByClientType(ctx, session.Token, clientType, label)
	selected := h.drawRandomAgentCandidates(agents, 1, label+"-selected")
	return selected[0]
}

func (h *liveAgentHarness) fetchSessionList(
	ctx context.Context,
	session liveProjectSession,
	label string,
	limit int,
) []map[string]any {
	h.t.Helper()
	require.NotEmpty(h.t, session.Token, "project session token is required")
	if limit <= 0 {
		limit = 50
	}

	path := fmt.Sprintf("/sessions/list?limit=%d&offset=0", limit)
	listData := h.apiJSON(ctx, label, http.MethodGet, path, session.Token, nil)
	items, _ := listData["list"].([]any)
	if len(items) == 0 {
		return nil
	}

	result := make([]map[string]any, 0, len(items))
	for _, item := range items {
		row, ok := item.(map[string]any)
		if !ok {
			continue
		}
		result = append(result, cloneMap(row))
	}
	return result
}

func (h *liveAgentHarness) fetchFriendList(
	ctx context.Context,
	session liveProjectSession,
	label string,
) []map[string]any {
	h.t.Helper()
	require.NotEmpty(h.t, session.Token, "project session token is required")

	data := h.apiJSON(ctx, label, http.MethodGet, "/friends/list", session.Token, nil)
	items, _ := data["list"].([]any)
	if len(items) == 0 {
		return nil
	}

	result := make([]map[string]any, 0, len(items))
	for _, item := range items {
		row, ok := item.(map[string]any)
		if !ok {
			continue
		}
		result = append(result, cloneMap(row))
	}
	return result
}

func (h *liveAgentHarness) listGroupSessionsByExactTitle(
	ctx context.Context,
	session liveProjectSession,
	groupTitle string,
	label string,
) []map[string]any {
	h.t.Helper()
	require.NotEmpty(h.t, session.Token, "project session token is required")
	require.NotEmpty(h.t, strings.TrimSpace(groupTitle), "group title is required")

	items := h.fetchSessionList(ctx, session, label, 100)
	result := make([]map[string]any, 0, 1)
	for _, item := range items {
		if asString(item["title"]) != groupTitle {
			continue
		}
		sessionType, err := parseFlexibleInt64(item["session_type"])
		if err == nil && sessionType != 2 {
			continue
		}
		result = append(result, cloneMap(item))
	}
	return result
}

func (h *liveAgentHarness) fetchSessionDetail(
	ctx context.Context,
	session liveProjectSession,
	sessionID string,
	label string,
) map[string]any {
	h.t.Helper()
	require.NotEmpty(h.t, session.Token, "project session token is required")
	require.NotEmpty(h.t, strings.TrimSpace(sessionID), "session_id is required")

	path := fmt.Sprintf("/sessions/detail?session_id=%s", url.QueryEscape(strings.TrimSpace(sessionID)))
	return h.apiJSON(ctx, label, http.MethodGet, path, session.Token, nil)
}

func (h *liveAgentHarness) resolveMultiAgentGroupTargets(
	ctx context.Context,
	session liveProjectSession,
	minCount int,
) []map[string]any {
	return h.resolveEligibleGroupTargetsExcluding(ctx, session, minCount, "group-target-agents")
}

func (h *liveAgentHarness) resolveEligibleGroupTargetsExcluding(
	ctx context.Context,
	session liveProjectSession,
	minCount int,
	label string,
	excludedIDs ...string,
) []map[string]any {
	h.t.Helper()
	require.NotEmpty(h.t, session.Token, "project session token is required")
	require.GreaterOrEqual(h.t, minCount, 1, "minCount must be positive")

	agents := h.fetchOnlineOpenClawAgents(ctx, session.Token, "agents-list-group")
	return h.drawRandomEligibleAgentsExcluding(agents, minCount, label, excludedIDs...)
}

func (h *liveAgentHarness) createIsolatedGroupSession(ctx context.Context, session liveProjectSession, groupName string) liveGroupSession {
	h.t.Helper()
	primaryAgent := session.Agent
	if primaryAgent == nil {
		primaryAgent = map[string]any{
			"id": session.AgentID,
		}
	}
	return h.createIsolatedGroupSessionWithAgents(ctx, session, groupName, []map[string]any{primaryAgent})
}

func (h *liveAgentHarness) createIsolatedGroupSessionWithAgents(
	ctx context.Context,
	session liveProjectSession,
	groupName string,
	agents []map[string]any,
) liveGroupSession {
	h.t.Helper()
	require.NotEmpty(h.t, session.Token, "project session token is required")
	require.NotEmpty(h.t, agents, "group agents are required")

	memberIDs := make([]string, 0, len(agents))
	memberTypes := make([]int, 0, len(agents))
	selectedAgents := make([]map[string]any, 0, len(agents))
	for _, row := range agents {
		agentID := asString(row["id"])
		require.NotEmpty(h.t, agentID, "group agent has empty id")
		memberIDs = append(memberIDs, agentID)
		memberTypes = append(memberTypes, 2)
		selectedAgents = append(selectedAgents, cloneMap(row))
	}

	data := h.apiJSON(ctx, "group-create", http.MethodPost, "/sessions/create_group", session.Token, map[string]any{
		"name":         groupName,
		"member_ids":   memberIDs,
		"member_types": memberTypes,
	})
	group := liveGroupSession{
		SessionID: asString(data["session_id"]),
		GroupName: groupName,
		AgentID:   firstString(memberIDs),
		AgentIDs:  append([]string(nil), memberIDs...),
		Agents:    selectedAgents,
	}
	require.NotEmpty(h.t, group.SessionID, "group create returned empty session_id")
	h.writeJSON("group-session.json", group)
	return group
}

func (h *liveAgentHarness) listAgentsByExactName(
	ctx context.Context,
	session liveProjectSession,
	agentName string,
) []map[string]any {
	h.t.Helper()
	require.NotEmpty(h.t, session.Token, "project session token is required")
	require.NotEmpty(h.t, strings.TrimSpace(agentName), "agent name is required")

	agents := h.fetchProjectAgents(ctx, session.Token, "agents-list-by-name")
	result := make([]map[string]any, 0, 1)
	for _, row := range agents {
		if asString(row["agent_name"]) != agentName {
			continue
		}
		result = append(result, cloneMap(row))
	}
	return result
}

func (h *liveAgentHarness) fetchAgentByID(
	ctx context.Context,
	session liveProjectSession,
	agentID string,
	label string,
) map[string]any {
	h.t.Helper()
	require.NotEmpty(h.t, session.Token, "project session token is required")
	require.NotEmpty(h.t, strings.TrimSpace(agentID), "agent id is required")

	path := fmt.Sprintf("/agents/%s", url.PathEscape(strings.TrimSpace(agentID)))
	return h.apiJSON(ctx, label, http.MethodGet, path, session.Token, nil)
}

func (h *liveAgentHarness) fetchAgentScopes(
	ctx context.Context,
	session liveProjectSession,
	agentID string,
	label string,
) map[string]any {
	h.t.Helper()
	require.NotEmpty(h.t, session.Token, "project session token is required")
	require.NotEmpty(h.t, strings.TrimSpace(agentID), "agent id is required")

	path := fmt.Sprintf("/agents/%s/scopes", url.PathEscape(strings.TrimSpace(agentID)))
	return h.apiJSON(ctx, label, http.MethodGet, path, session.Token, nil)
}

func (h *liveAgentHarness) fetchAgentCategories(
	ctx context.Context,
	session liveProjectSession,
	label string,
) []map[string]any {
	h.t.Helper()
	require.NotEmpty(h.t, session.Token, "project session token is required")

	data := h.apiJSON(ctx, label, http.MethodGet, "/agents/categories/list", session.Token, nil)
	items, _ := data["list"].([]any)
	if len(items) == 0 {
		return nil
	}

	result := make([]map[string]any, 0, len(items))
	for _, item := range items {
		row, ok := item.(map[string]any)
		if !ok {
			continue
		}
		result = append(result, cloneMap(row))
	}
	return result
}

func (h *liveAgentHarness) listAgentCategoriesByExactName(
	ctx context.Context,
	session liveProjectSession,
	categoryName string,
	label string,
) []map[string]any {
	h.t.Helper()
	require.NotEmpty(h.t, session.Token, "project session token is required")
	require.NotEmpty(h.t, strings.TrimSpace(categoryName), "category name is required")

	items := h.fetchAgentCategories(ctx, session, label)
	result := make([]map[string]any, 0, 1)
	for _, item := range items {
		if asString(item["name"]) != categoryName {
			continue
		}
		result = append(result, cloneMap(item))
	}
	return result
}

func (h *liveAgentHarness) listAgentsByCategoryID(
	ctx context.Context,
	session liveProjectSession,
	categoryID string,
	label string,
) []map[string]any {
	h.t.Helper()
	require.NotEmpty(h.t, session.Token, "project session token is required")
	require.NotEmpty(h.t, strings.TrimSpace(categoryID), "category id is required")

	path := fmt.Sprintf("/agents/list?category_id=%s", url.QueryEscape(strings.TrimSpace(categoryID)))
	data := h.apiJSON(ctx, label, http.MethodGet, path, session.Token, nil)
	items, _ := data["list"].([]any)
	if len(items) == 0 {
		return nil
	}

	result := make([]map[string]any, 0, len(items))
	for _, item := range items {
		row, ok := item.(map[string]any)
		if !ok {
			continue
		}
		result = append(result, cloneMap(row))
	}
	return result
}

func (h *liveAgentHarness) updateAgentCategory(
	ctx context.Context,
	session liveProjectSession,
	agentID string,
	categoryID int64,
	label string,
) map[string]any {
	h.t.Helper()
	require.NotEmpty(h.t, session.Token, "project session token is required")
	require.NotEmpty(h.t, strings.TrimSpace(agentID), "agent id is required")

	path := fmt.Sprintf("/agents/%s", url.PathEscape(strings.TrimSpace(agentID)))
	return h.apiJSON(ctx, label, http.MethodPut, path, session.Token, map[string]any{
		"category_id": strconv.FormatInt(categoryID, 10),
	})
}

func (h *liveAgentHarness) deleteAgentCategory(
	ctx context.Context,
	session liveProjectSession,
	categoryID string,
	label string,
) map[string]any {
	h.t.Helper()
	require.NotEmpty(h.t, session.Token, "project session token is required")
	require.NotEmpty(h.t, strings.TrimSpace(categoryID), "category id is required")

	path := fmt.Sprintf("/agents/categories/%s", url.PathEscape(strings.TrimSpace(categoryID)))
	return h.apiJSON(ctx, label, http.MethodDelete, path, session.Token, nil)
}

func (h *liveAgentHarness) waitForAgentByExactName(
	ctx context.Context,
	session liveProjectSession,
	agentName string,
	timeout time.Duration,
) []map[string]any {
	h.t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		matches := h.listAgentsByExactName(ctx, session, agentName)
		if len(matches) > 0 {
			return matches
		}
		if time.Now().After(deadline) {
			h.t.Fatalf("timed out waiting for agent named %s to appear", agentName)
		}
		time.Sleep(2 * time.Second)
	}
}

func (h *liveAgentHarness) deleteAgentByID(ctx context.Context, session liveProjectSession, agentID string) map[string]any {
	h.t.Helper()
	require.NotEmpty(h.t, session.Token, "project session token is required")
	require.NotEmpty(h.t, strings.TrimSpace(agentID), "agent id is required")

	path := fmt.Sprintf("/agents/%s", url.PathEscape(strings.TrimSpace(agentID)))
	data := h.apiJSON(ctx, "agent-delete-"+sanitizeFileName(agentID), http.MethodDelete, path, session.Token, nil)
	return data
}

func (h *liveAgentHarness) updateGroupAgentReceiveMode(
	ctx context.Context,
	session liveProjectSession,
	groupSessionID string,
	agentID string,
	mode int16,
) map[string]any {
	h.t.Helper()
	require.NotEmpty(h.t, session.Token, "project session token is required")
	require.NotEmpty(h.t, groupSessionID, "group session_id is required")
	require.NotEmpty(h.t, agentID, "agent_id is required")

	data := h.apiJSON(ctx, "group-agent-receive", http.MethodPost, "/sessions/members/agent_receive", session.Token, map[string]any{
		"session_id":                  groupSessionID,
		"member_id":                   agentID,
		"member_type":                 2,
		"agent_receive_mode":          mode,
		"agent_receive_backlog_count": agentreceive.DefaultBacklogCount,
	})
	h.writeJSON(fmt.Sprintf("group-agent-receive-%s.json", sanitizeFileName(agentID)), data)
	return data
}

func (h *liveAgentHarness) updateGroupAgentReceiveModes(
	ctx context.Context,
	session liveProjectSession,
	groupSessionID string,
	agentIDs []string,
	mode int16,
) []map[string]any {
	h.t.Helper()

	results := make([]map[string]any, 0, len(agentIDs))
	for _, agentID := range agentIDs {
		if strings.TrimSpace(agentID) == "" {
			continue
		}
		result := h.updateGroupAgentReceiveMode(ctx, session, groupSessionID, agentID, mode)
		results = append(results, map[string]any{
			"agent_id": agentID,
			"result":   result,
		})
	}
	h.writeJSON("group-agent-receive-batch.json", results)
	return results
}

func (h *liveAgentHarness) dissolveGroupSession(ctx context.Context, session liveProjectSession, groupSessionID string) {
	h.t.Helper()
	if strings.TrimSpace(groupSessionID) == "" || strings.TrimSpace(session.Token) == "" {
		return
	}
	data := h.apiJSON(ctx, "group-dissolve", http.MethodPost, "/sessions/dissolve", session.Token, map[string]any{
		"session_id": groupSessionID,
	})
	h.writeJSON("group-dissolve.json", data)
}

func (h *liveAgentHarness) configureGroupCleanup(t *testing.T, session liveProjectSession, group liveGroupSession) string {
	t.Helper()

	if h.cfg.CleanupGroups {
		t.Cleanup(func() {
			cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cleanupCancel()
			h.dissolveGroupSession(cleanupCtx, session, group.SessionID)
		})
		return "auto_cleanup"
	}

	t.Logf("retain test group for inspection: %s (%s)", group.GroupName, group.SessionID)
	return "retain"
}

func (h *liveAgentHarness) runOpenClaw(ctx context.Context, label string, expectJSON bool, allowFailure bool, args ...string) (string, any, error) {
	cmdArgs := append(h.requireOpenClawCommand(), args...)
	cmd := exec.CommandContext(ctx, cmdArgs[0], cmdArgs[1:]...)
	output, err := cmd.CombinedOutput()
	raw := string(output)
	h.writeText(label+".log", raw)
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
	h.writeJSON(label+".json", payload)
	return raw, payload, err
}

func (h *liveAgentHarness) runPreflight(ctx context.Context) livePreflightSummary {
	h.t.Helper()

	accountID := firstNonEmpty(h.cfg.OpenClawAccount, h.cfg.OpenClawAgent)
	require.NotEmpty(h.t, h.cfg.OpenClawAgent, "GRIX_LIVE_OPENCLAW_AGENT is required for preflight")
	require.NotEmpty(h.t, accountID, "GRIX_LIVE_GRIX_ACCOUNT is required for preflight")

	cases := make([]liveCheckCase, 0, 8)

	versionOutput, _, err := h.runOpenClaw(ctx, "openclaw-version", false, false, "--version")
	require.NoError(h.t, err)
	versionMatch := regexp.MustCompile(`OpenClaw\s+([0-9][0-9A-Za-z.\-]*)`).FindStringSubmatch(versionOutput)
	runtimeVersion := "unknown"
	if len(versionMatch) > 1 {
		runtimeVersion = versionMatch[1]
	}
	versionOK := runtimeVersion != "unknown" && versionGE(runtimeVersion, liveMinOpenClawVersion)
	if versionOK {
		cases = append(cases, liveCheckCase{
			CaseID:   "BASE-001",
			Title:    "OpenClaw runtime version",
			Status:   "pass",
			Severity: "info",
			Detail:   fmt.Sprintf("runtime version %s meets documented floor %s", runtimeVersion, liveMinOpenClawVersion),
			Data:     map[string]any{"runtime_version": runtimeVersion},
		})
	} else {
		status := "warn"
		severity := "warn"
		if h.cfg.StrictVersion {
			status = "fail"
			severity = "critical"
		}
		cases = append(cases, liveCheckCase{
			CaseID:   "BASE-001",
			Title:    "OpenClaw runtime version",
			Status:   status,
			Severity: severity,
			Detail:   fmt.Sprintf("runtime version %s is lower than documented floor %s", runtimeVersion, liveMinOpenClawVersion),
			Data: map[string]any{
				"runtime_version":  runtimeVersion,
				"required_version": liveMinOpenClawVersion,
			},
		})
	}

	_, pluginPayloadAny, err := h.runOpenClaw(ctx, "plugin-grix-info", true, false, "plugins", "info", "grix", "--json")
	require.NoError(h.t, err)
	pluginPayload, _ := pluginPayloadAny.(map[string]any)
	pluginState := openClawPluginState(pluginPayload)
	pluginOK := asBool(pluginState["enabled"]) && asString(pluginState["status"]) == "loaded"
	cases = append(cases, liveCheckCase{
		CaseID:   "BASE-002",
		Title:    "Grix plugin load state",
		Status:   ternaryStatus(pluginOK),
		Severity: "critical",
		Detail:   fmt.Sprintf("plugin source=%s status=%s", asString(pluginState["source"]), asString(pluginState["status"])),
		Data:     pluginPayload,
	})

	_, accountPayloadAny, err := h.runOpenClaw(ctx, "account-config", true, false, "config", "get", "--json", "channels.grix.accounts."+accountID)
	require.NoError(h.t, err)
	accountPayload, _ := accountPayloadAny.(map[string]any)
	accountOK := asBool(accountPayload["enabled"])
	if h.cfg.RemoteAgentID != "" {
		accountOK = accountOK && asString(accountPayload["agentId"]) == h.cfg.RemoteAgentID
	}
	cases = append(cases, liveCheckCase{
		CaseID:   "BASE-003",
		Title:    "Target Grix account",
		Status:   ternaryStatus(accountOK),
		Severity: "critical",
		Detail:   fmt.Sprintf("account=%s enabled=%t agentId=%s", accountID, asBool(accountPayload["enabled"]), asString(accountPayload["agentId"])),
		Data:     accountPayload,
	})

	_, bindingPayloadAny, err := h.runOpenClaw(ctx, "agent-bindings", true, false, "agents", "bindings", "--agent", h.cfg.OpenClawAgent, "--json")
	require.NoError(h.t, err)
	bindings, _ := bindingPayloadAny.([]any)
	bindingOK := false
	for _, item := range bindings {
		row, ok := item.(map[string]any)
		if !ok {
			continue
		}
		match, _ := row["match"].(map[string]any)
		if asString(match["channel"]) == "grix" && asString(match["accountId"]) == accountID {
			bindingOK = true
			break
		}
	}
	cases = append(cases, liveCheckCase{
		CaseID:   "BASE-004",
		Title:    "OpenClaw agent binding",
		Status:   ternaryStatus(bindingOK),
		Severity: "critical",
		Detail:   fmt.Sprintf("agent=%s bound to grix:%s=%t", h.cfg.OpenClawAgent, accountID, bindingOK),
		Data:     map[string]any{"bindings": bindings},
	})

	_, channelPayloadAny, err := h.runOpenClaw(
		ctx,
		"channel-probe",
		true,
		false,
		"channels",
		"status",
		"--probe",
		"--timeout",
		strconv.Itoa(h.cfg.OpenClawProbeMS),
		"--json",
	)
	require.NoError(h.t, err)
	channelPayload, _ := channelPayloadAny.(map[string]any)
	channelState := map[string]any{}
	if channels, _ := channelPayload["channels"].(map[string]any); channels != nil {
		if row, _ := channels["grix"].(map[string]any); row != nil {
			channelState = row
		}
	}
	accountState := map[string]any{}
	if channelAccounts, _ := channelPayload["channelAccounts"].(map[string]any); channelAccounts != nil {
		if list, _ := channelAccounts["grix"].([]any); list != nil {
			for _, item := range list {
				row, ok := item.(map[string]any)
				if ok && asString(row["accountId"]) == accountID {
					accountState = row
					break
				}
			}
		}
	}
	channelOK := asBool(channelState["configured"]) &&
		asBool(channelState["running"]) &&
		asBool(channelState["connected"]) &&
		asBool(accountState["configured"]) &&
		asBool(accountState["running"]) &&
		asBool(accountState["connected"])
	cases = append(cases, liveCheckCase{
		CaseID:   "BASE-005",
		Title:    "Channel probe",
		Status:   ternaryStatus(channelOK),
		Severity: "critical",
		Detail:   fmt.Sprintf("channel=%v account=%v", channelState, accountState),
		Data: map[string]any{
			"channel": channelState,
			"account": accountState,
		},
	})

	_, globalToolsAny, _ := h.runOpenClaw(ctx, "tools-global", true, true, "config", "get", "--json", "tools.alsoAllow")
	_, agentsListAny, err := h.runOpenClaw(ctx, "agents-list", true, false, "config", "get", "--json", "agents.list")
	require.NoError(h.t, err)
	_, visibilityAny, _ := h.runOpenClaw(ctx, "tools-sessions-visibility", true, true, "config", "get", "--json", "tools.sessions.visibility")
	effectiveTools := buildEffectiveTools(globalToolsAny, agentsListAny, h.cfg.OpenClawAgent)
	requiredTools := []string{"message", "grix_query", "grix_group", "grix_message_send", "grix_message_unsend", "grix_admin"}
	missingTools := missingNames(requiredTools, effectiveTools)
	toolsOK := len(missingTools) == 0 && asString(visibilityAny) == "agent"
	cases = append(cases, liveCheckCase{
		CaseID:   "BASE-006",
		Title:    "Tool visibility",
		Status:   ternaryStatus(toolsOK),
		Severity: "critical",
		Detail:   fmt.Sprintf("effectiveTools=%v visibility=%s", effectiveTools, asString(visibilityAny)),
		Data: map[string]any{
			"effective_tools": effectiveTools,
			"missing_tools":   missingTools,
			"visibility":      visibilityAny,
		},
	})

	_, skillsPayloadAny, err := h.runOpenClaw(ctx, "skills-list", true, false, "skills", "list", "--json")
	require.NoError(h.t, err)
	requiredSkills := []string{"grix-admin", "grix-group", "grix-query", "message-send", "message-unsend"}
	availableSkills := make([]string, 0, len(requiredSkills))
	if skillsPayload, _ := skillsPayloadAny.(map[string]any); skillsPayload != nil {
		if items, _ := skillsPayload["skills"].([]any); items != nil {
			for _, item := range items {
				row, ok := item.(map[string]any)
				if ok && asBool(row["eligible"]) {
					availableSkills = append(availableSkills, asString(row["name"]))
				}
			}
		}
	}
	missingSkills := missingNames(requiredSkills, availableSkills)
	skillsOK := len(missingSkills) == 0
	cases = append(cases, liveCheckCase{
		CaseID:   "BASE-007",
		Title:    "Required skills",
		Status:   ternaryStatus(skillsOK),
		Severity: "high",
		Detail:   fmt.Sprintf("availableSkills=%v", availableSkills),
		Data: map[string]any{
			"available_skills": availableSkills,
			"missing_skills":   missingSkills,
		},
	})

	_, sessionsPayloadAny, err := h.runOpenClaw(ctx, "sessions-catalog", true, false, "sessions", "--agent", h.cfg.OpenClawAgent, "--json")
	require.NoError(h.t, err)
	sessionSummary := summarizeOpenClawSessions(sessionsPayloadAny, 5)
	sessionStatus := "warn"
	if total, _ := parseFlexibleInt64(sessionSummary["total"]); total > 0 {
		sessionStatus = "pass"
	}
	cases = append(cases, liveCheckCase{
		CaseID:   "BASE-008",
		Title:    "Recent session catalog",
		Status:   sessionStatus,
		Severity: "info",
		Detail:   fmt.Sprintf("recent sessions=%v", sessionSummary),
		Data:     sessionSummary,
	})

	summary := livePreflightSummary{
		GeneratedAt:     time.Now().UTC().Format(time.RFC3339),
		ArtifactsDir:    h.artifactDir,
		OpenClawCommand: h.requireOpenClawCommand(),
		Agent:           h.cfg.OpenClawAgent,
		Account:         accountID,
		RemoteAgentID:   h.cfg.RemoteAgentID,
		Cases:           cases,
	}
	h.writeJSON("preflight-summary.json", summary)
	return summary
}

func (s livePreflightSummary) hasFailures() bool {
	for _, item := range s.Cases {
		if item.Status == "fail" {
			return true
		}
	}
	return false
}

func (s livePreflightSummary) statusLine() string {
	parts := make([]string, 0, len(s.Cases))
	for _, item := range s.Cases {
		parts = append(parts, fmt.Sprintf("%s=%s", item.CaseID, item.Status))
	}
	return strings.Join(parts, ", ")
}

func newLiveUserWSClient(t *testing.T, harness *liveAgentHarness, session liveProjectSession) *liveUserWSClient {
	t.Helper()
	return &liveUserWSClient{
		harness: harness,
		t:       t,
		session: session,
		seq:     1,
	}
}

func (c *liveUserWSClient) connect(ctx context.Context) {
	c.t.Helper()

	dialer := websocket.Dialer{}
	conn, resp, err := dialer.DialContext(ctx, c.session.WSURL, http.Header{})
	require.NoError(c.t, err)
	if resp != nil {
		require.Equal(c.t, http.StatusSwitchingProtocols, resp.StatusCode)
	}
	c.conn = conn

	c.sendPacket(protocol.CmdAuth, protocol.AuthPayload{
		Token:    c.session.Token,
		DeviceID: c.session.DeviceID,
		Platform: c.session.Platform,
	})

	packet, payload := c.readPacket(10 * time.Second)
	require.Equal(c.t, protocol.CmdAuthAck, packet.Cmd)
	code, err := parseFlexibleInt64(payload["code"])
	require.NoError(c.t, err)
	require.Equal(c.t, int64(0), code)
	c.userID = asString(payload["user_id"])
	if c.userID != "" {
		c.userID64, _ = strconv.ParseInt(c.userID, 10, 64)
	}
}

func (c *liveUserWSClient) close() {
	c.t.Helper()
	if c.conn != nil {
		_ = c.conn.Close()
	}
}

func (c *liveUserWSClient) reconnect(ctx context.Context) {
	c.t.Helper()

	c.close()

	reconnectCtx := ctx
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		var cancel context.CancelFunc
		reconnectCtx, cancel = context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
	}

	c.connect(reconnectCtx)
}

func (c *liveUserWSClient) runConversationProbe(ctx context.Context, opts liveConversationProbeOptions) liveConversationResult {
	c.t.Helper()

	require.NotEmpty(c.t, opts.Message, "probe message is required")
	sendAck := c.sendMessage(liveSendMessageOptions{
		Content: opts.Message,
	})
	result := liveConversationResult{
		TriggerMsgID: asString(sendAck["msg_id"]),
		SendAck:      sendAck,
	}
	deadline := time.Now().Add(opts.Timeout)
	nextHistoryCheckAt := time.Now()

	for {
		select {
		case <-ctx.Done():
			c.t.Fatalf("conversation probe canceled: %v", ctx.Err())
		default:
		}
		if time.Now().After(deadline) {
			if c.captureMatchingHistoryReply(ctx, &result, opts, true) {
				return result
			}
			c.failConversationProbe("timed out waiting for agent conversation result", result, opts)
		}

		waitWindow := minDuration(5*time.Second, time.Until(deadline))
		packet, payload, err := c.readPacketMaybe(waitWindow)
		if err != nil {
			if ne, ok := err.(net.Error); ok && ne.Timeout() {
				if c.captureMatchingHistoryReply(ctx, &result, opts, time.Now().After(nextHistoryCheckAt)) {
					return result
				}
				nextHistoryCheckAt = time.Now().Add(2 * time.Second)
				if c.failConversationProbeIfDetected(result, opts) {
					return result
				}
				c.reconnect(ctx)
				continue
			}
			c.failConversationProbe(
				fmt.Sprintf("read websocket packet failed while waiting for agent conversation result: %v", err),
				result,
				opts,
			)
		}
		result.Events = append(result.Events, map[string]any{
			"cmd":         packet.Cmd,
			"seq":         packet.Seq,
			"payload":     payload,
			"observed_at": time.Now().UnixMilli(),
		})

		switch packet.Cmd {
		case protocol.CmdAgentDeliveryStatus:
			if asString(payload["session_id"]) == c.session.SessionID && asString(payload["trigger_msg_id"]) == result.TriggerMsgID {
				result.DeliveryStatuses = append(result.DeliveryStatuses, payload)
				if c.captureMatchingHistoryReply(ctx, &result, opts, time.Now().After(nextHistoryCheckAt)) {
					return result
				}
				nextHistoryCheckAt = time.Now().Add(2 * time.Second)
			}
		case protocol.CmdAgentOutputStatus:
			if asString(payload["session_id"]) == c.session.SessionID && asString(payload["trigger_msg_id"]) == result.TriggerMsgID {
				result.OutputStatuses = append(result.OutputStatuses, payload)
				if c.captureMatchingHistoryReply(ctx, &result, opts, time.Now().After(nextHistoryCheckAt)) {
					return result
				}
				nextHistoryCheckAt = time.Now().Add(2 * time.Second)
			}
		case protocol.CmdPushMsg:
			if asString(payload["session_id"]) != c.session.SessionID {
				continue
			}
			senderID := asString(payload["sender_id"])
			if senderID == c.userID {
				continue
			}
			if opts.ExpectedSenderID != "" && senderID != opts.ExpectedSenderID {
				continue
			}
			content := firstNonEmpty(asString(payload["content"]), asString(payload["final_content"]))
			if opts.ExpectedTextContains != "" && !strings.Contains(content, opts.ExpectedTextContains) {
				continue
			}
			result.AgentPushes = append(result.AgentPushes, payload)
			return result
		case protocol.CmdPushEdit:
			if asString(payload["session_id"]) != c.session.SessionID {
				continue
			}
			senderID := asString(payload["sender_id"])
			if senderID == c.userID {
				continue
			}
			if opts.ExpectedSenderID != "" && senderID != "" && senderID != opts.ExpectedSenderID {
				continue
			}
			content := firstNonEmpty(asString(payload["content"]), asString(payload["final_content"]))
			if opts.ExpectedTextContains != "" && !strings.Contains(content, opts.ExpectedTextContains) {
				continue
			}
			result.AgentPushes = append(result.AgentPushes, payload)
			return result
		case protocol.CmdStreamFinish:
			if asString(payload["session_id"]) != c.session.SessionID {
				continue
			}
			senderID := asString(payload["sender_id"])
			if senderID == c.userID {
				continue
			}
			if opts.ExpectedSenderID != "" && senderID != "" && senderID != opts.ExpectedSenderID {
				continue
			}
			content := asString(payload["final_content"])
			if opts.ExpectedTextContains != "" && !strings.Contains(content, opts.ExpectedTextContains) {
				continue
			}
			result.AgentPushes = append(result.AgentPushes, payload)
			return result
		case protocol.CmdStreamChunk:
			if asString(payload["session_id"]) != c.session.SessionID {
				continue
			}
			senderID := asString(payload["sender_id"])
			if senderID == c.userID {
				continue
			}
			if opts.ExpectedSenderID != "" && senderID != "" && senderID != opts.ExpectedSenderID {
				continue
			}
			if c.captureMatchingHistoryReply(ctx, &result, opts, time.Now().After(nextHistoryCheckAt)) {
				return result
			}
			nextHistoryCheckAt = time.Now().Add(2 * time.Second)
		}
	}
}

func (c *liveUserWSClient) runMultiAgentConversationProbe(
	ctx context.Context,
	opts liveMultiConversationProbeOptions,
) liveMultiConversationResult {
	c.t.Helper()
	require.NotEmpty(c.t, opts.Message, "probe message is required")
	expectedSenderIDs := uniqueTrimmedStrings(opts.ExpectedSenderIDs)
	forbiddenSenderIDs := uniqueTrimmedStrings(opts.ForbiddenSenderIDs)
	require.NotEmpty(c.t, expectedSenderIDs, "expected sender ids are required")

	sendAck := c.sendMessage(liveSendMessageOptions{
		Content:         opts.Message,
		Extra:           opts.Extra,
		QuotedMessageID: opts.QuotedMessageID,
	})
	result := liveMultiConversationResult{
		TriggerMsgID:       asString(sendAck["msg_id"]),
		SendAck:            sendAck,
		RepliesBySender:    make(map[string][]map[string]any, len(expectedSenderIDs)),
		ExpectedSenderIDs:  expectedSenderIDs,
		ForbiddenSenderIDs: forbiddenSenderIDs,
	}

	deadline := time.Now().Add(opts.Timeout)
	quietWindow := opts.PostMatchQuietWindow
	if quietWindow <= 0 {
		quietWindow = 8 * time.Second
	}
	quietDeadline := time.Time{}
	nextHistoryCheckAt := time.Now()
	seenMessages := make(map[string]struct{}, 16)

	recordReply := func(payload map[string]any, matchSource string) {
		key := liveMessageDedupKey(payload)
		if key != "" {
			if _, ok := seenMessages[key]; ok {
				return
			}
			seenMessages[key] = struct{}{}
		}
		row := cloneMap(payload)
		if matchSource != "" {
			row["_match_source"] = matchSource
		}
		result.AgentPushes = append(result.AgentPushes, row)
		senderID := asString(row["sender_id"])
		if senderID == "" {
			return
		}
		result.RepliesBySender[senderID] = append(result.RepliesBySender[senderID], row)
	}

	allExpectedMatched := func() bool {
		for _, senderID := range expectedSenderIDs {
			if len(result.RepliesBySender[senderID]) == 0 {
				return false
			}
		}
		return true
	}

	failOnForbidden := func(message string) {
		c.failMultiAgentConversationProbe(message, result, opts)
	}

	recordHistoryMatches := func(force bool) {
		if !force || c.harness == nil || strings.TrimSpace(result.TriggerMsgID) == "" {
			return
		}
		matches := c.findHistoryAgentMessages(ctx, result.TriggerMsgID, opts.ExpectedTextContains)
		for _, match := range matches {
			recordReply(match, "history")
		}
	}

	forbiddenObserved := func() string {
		for _, senderID := range forbiddenSenderIDs {
			if len(result.RepliesBySender[senderID]) > 0 {
				return senderID
			}
		}
		return ""
	}

	for {
		select {
		case <-ctx.Done():
			c.t.Fatalf("multi-agent conversation probe canceled: %v", ctx.Err())
		default:
		}

		now := time.Now()
		if now.After(deadline) {
			recordHistoryMatches(true)
			if senderID := forbiddenObserved(); senderID != "" {
				failOnForbidden(fmt.Sprintf("unexpected forbidden agent reply observed from %s", senderID))
			}
			if !allExpectedMatched() {
				c.failMultiAgentConversationProbe("timed out waiting for all expected agent replies", result, opts)
			}
			if !quietDeadline.IsZero() && now.Before(quietDeadline) {
				c.failMultiAgentConversationProbe("timed out before forbidden-reply quiet window completed", result, opts)
			}
			return result
		}
		if !quietDeadline.IsZero() && now.After(quietDeadline) {
			return result
		}

		waitWindow := minDuration(5*time.Second, time.Until(deadline))
		if !quietDeadline.IsZero() {
			waitWindow = minDuration(waitWindow, time.Until(quietDeadline))
		}
		packet, payload, err := c.readPacketMaybe(waitWindow)
		if err != nil {
			if ne, ok := err.(net.Error); ok && ne.Timeout() {
				recordHistoryMatches(time.Now().After(nextHistoryCheckAt))
				nextHistoryCheckAt = time.Now().Add(2 * time.Second)
				if senderID := forbiddenObserved(); senderID != "" {
					failOnForbidden(fmt.Sprintf("unexpected forbidden agent reply observed from %s", senderID))
				}
				if allExpectedMatched() && quietDeadline.IsZero() {
					quietDeadline = time.Now().Add(quietWindow)
				}
				c.reconnect(ctx)
				continue
			}
			c.failMultiAgentConversationProbe(
				fmt.Sprintf("read websocket packet failed while waiting for multi-agent replies: %v", err),
				result,
				opts,
			)
		}

		result.Events = append(result.Events, map[string]any{
			"cmd":         packet.Cmd,
			"seq":         packet.Seq,
			"payload":     payload,
			"observed_at": time.Now().UnixMilli(),
		})

		switch packet.Cmd {
		case protocol.CmdAgentDeliveryStatus:
			if asString(payload["session_id"]) == c.session.SessionID && asString(payload["trigger_msg_id"]) == result.TriggerMsgID {
				result.DeliveryStatuses = append(result.DeliveryStatuses, payload)
				recordHistoryMatches(time.Now().After(nextHistoryCheckAt))
				nextHistoryCheckAt = time.Now().Add(2 * time.Second)
			}
		case protocol.CmdAgentOutputStatus:
			if asString(payload["session_id"]) == c.session.SessionID && asString(payload["trigger_msg_id"]) == result.TriggerMsgID {
				result.OutputStatuses = append(result.OutputStatuses, payload)
				recordHistoryMatches(time.Now().After(nextHistoryCheckAt))
				nextHistoryCheckAt = time.Now().Add(2 * time.Second)
			}
		case protocol.CmdPushMsg, protocol.CmdPushEdit, protocol.CmdStreamFinish:
			if asString(payload["session_id"]) != c.session.SessionID {
				continue
			}
			senderID := asString(payload["sender_id"])
			if senderID == "" || senderID == c.userID {
				continue
			}
			content := extractPacketContent(payload)
			if opts.ExpectedTextContains != "" && !strings.Contains(content, opts.ExpectedTextContains) {
				continue
			}
			recordReply(payload, "ws")
		case protocol.CmdStreamChunk:
			if asString(payload["session_id"]) != c.session.SessionID {
				continue
			}
			senderID := asString(payload["sender_id"])
			if senderID == "" || senderID == c.userID {
				continue
			}
			recordHistoryMatches(time.Now().After(nextHistoryCheckAt))
			nextHistoryCheckAt = time.Now().Add(2 * time.Second)
		}

		if senderID := forbiddenObserved(); senderID != "" {
			failOnForbidden(fmt.Sprintf("unexpected forbidden agent reply observed from %s", senderID))
		}
		if allExpectedMatched() && quietDeadline.IsZero() {
			quietDeadline = time.Now().Add(quietWindow)
		}
	}
}

func (c *liveUserWSClient) sendText(content string, extra map[string]any) map[string]any {
	c.t.Helper()
	return c.sendMessage(liveSendMessageOptions{
		Content: content,
		Extra:   extra,
	})
}

func (c *liveUserWSClient) sendMessage(opts liveSendMessageOptions) map[string]any {
	c.t.Helper()
	require.NotEmpty(c.t, opts.Content, "content is required")

	clientMsgID := uuid.NewString()
	payload := map[string]any{
		"session_id":    c.session.SessionID,
		"client_msg_id": clientMsgID,
		"msg_type":      1,
		"content":       opts.Content,
	}
	if len(opts.Extra) > 0 {
		payload["extra"] = opts.Extra
	}
	if strings.TrimSpace(opts.QuotedMessageID) != "" {
		payload["quoted_message_id"] = opts.QuotedMessageID
	}

	deadline := time.Now().Add(15 * time.Second)
	shouldResend := true
	var lastErr error
	for {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			if lastErr != nil {
				c.t.Fatalf("timed out waiting for send_ack after websocket retry: %v", lastErr)
			}
			c.t.Fatalf("timed out waiting for send_ack for client_msg_id=%s", clientMsgID)
		}

		if shouldResend {
			if err := c.sendPacketMaybe(protocol.CmdSendMsg, payload); err != nil {
				if isRecoverableWebsocketError(err) {
					lastErr = err
					c.reconnect(context.Background())
					continue
				}
				require.NoError(c.t, err)
			}
			shouldResend = false
		}

		packet, packetPayload, err := c.readPacketMaybe(minDuration(5*time.Second, remaining))
		if err != nil {
			if isRecoverableWebsocketError(err) {
				lastErr = err
				c.reconnect(context.Background())
				shouldResend = true
				continue
			}
			require.NoError(c.t, err)
		}
		switch packet.Cmd {
		case protocol.CmdSendAck:
			if asString(packetPayload["client_msg_id"]) == clientMsgID {
				return packetPayload
			}
		case protocol.CmdSendNack:
			if asString(packetPayload["client_msg_id"]) == clientMsgID {
				c.t.Fatalf("send_nack: %v", packetPayload)
			}
		}
	}
}

func (c *liveUserWSClient) assertNoAgentPushWithin(
	ctx context.Context,
	triggerMsgID string,
	timeout time.Duration,
) liveConversationResult {
	c.t.Helper()
	result := liveConversationResult{
		TriggerMsgID: triggerMsgID,
	}
	deadline := time.Now().Add(timeout)
	nextHistoryCheckAt := time.Now()
	for {
		select {
		case <-ctx.Done():
			c.t.Fatalf("observation canceled: %v", ctx.Err())
		default:
		}
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return result
		}

		packet, payload, err := c.readPacketMaybe(remaining)
		if err != nil {
			if ne, ok := err.(net.Error); ok && ne.Timeout() {
				if c.captureUnexpectedHistoryReply(ctx, &result, triggerMsgID, time.Now().After(nextHistoryCheckAt)) {
					return result
				}
				c.reconnect(ctx)
				return result
			}
			c.t.Fatalf("read websocket packet failed while asserting no agent push: %v", err)
		}

		result.Events = append(result.Events, map[string]any{
			"cmd":         packet.Cmd,
			"seq":         packet.Seq,
			"payload":     payload,
			"observed_at": time.Now().UnixMilli(),
		})

		switch packet.Cmd {
		case protocol.CmdAgentDeliveryStatus:
			if asString(payload["session_id"]) == c.session.SessionID && asString(payload["trigger_msg_id"]) == triggerMsgID {
				result.DeliveryStatuses = append(result.DeliveryStatuses, payload)
				if c.captureUnexpectedHistoryReply(ctx, &result, triggerMsgID, time.Now().After(nextHistoryCheckAt)) {
					return result
				}
				nextHistoryCheckAt = time.Now().Add(2 * time.Second)
			}
		case protocol.CmdAgentOutputStatus:
			if asString(payload["session_id"]) == c.session.SessionID && asString(payload["trigger_msg_id"]) == triggerMsgID {
				result.OutputStatuses = append(result.OutputStatuses, payload)
				if c.captureUnexpectedHistoryReply(ctx, &result, triggerMsgID, time.Now().After(nextHistoryCheckAt)) {
					return result
				}
				nextHistoryCheckAt = time.Now().Add(2 * time.Second)
			}
		case protocol.CmdPushMsg:
			if asString(payload["session_id"]) != c.session.SessionID {
				continue
			}
			if asString(payload["sender_id"]) == c.userID {
				continue
			}
			result.AgentPushes = append(result.AgentPushes, payload)
			c.t.Fatalf("unexpected agent push observed within quiet window: %v", payload)
		case protocol.CmdStreamChunk, protocol.CmdStreamFinish, protocol.CmdPushEdit:
			if asString(payload["session_id"]) != c.session.SessionID {
				continue
			}
			if asString(payload["sender_id"]) == c.userID {
				continue
			}
			if c.captureUnexpectedHistoryReply(ctx, &result, triggerMsgID, true) {
				return result
			}
		}
	}
}

func (c *liveUserWSClient) captureMatchingHistoryReply(
	ctx context.Context,
	result *liveConversationResult,
	opts liveConversationProbeOptions,
	force bool,
) bool {
	c.t.Helper()
	if !force || c.harness == nil || strings.TrimSpace(result.TriggerMsgID) == "" {
		return false
	}

	match := c.findHistoryAgentMessage(ctx, result.TriggerMsgID, opts.ExpectedSenderID, opts.ExpectedTextContains)
	if match == nil {
		return false
	}

	match["_match_source"] = "history"
	result.AgentPushes = append(result.AgentPushes, match)
	return true
}

func (c *liveUserWSClient) captureUnexpectedHistoryReply(
	ctx context.Context,
	result *liveConversationResult,
	triggerMsgID string,
	force bool,
) bool {
	c.t.Helper()
	if !force || c.harness == nil || strings.TrimSpace(triggerMsgID) == "" {
		return false
	}

	match := c.findHistoryAgentMessage(ctx, triggerMsgID, c.session.AgentID, "")
	if match == nil {
		return false
	}

	match["_match_source"] = "history"
	result.AgentPushes = append(result.AgentPushes, match)
	c.t.Fatalf("unexpected agent reply observed within quiet window via history: %v", match)
	return true
}

func (c *liveUserWSClient) findHistoryAgentMessage(
	ctx context.Context,
	triggerMsgID string,
	expectedSenderID string,
	expectedTextContains string,
) map[string]any {
	c.t.Helper()
	matches := c.findHistoryAgentMessages(ctx, triggerMsgID, expectedTextContains)
	for _, row := range matches {
		if expectedSenderID != "" && asString(row["sender_id"]) != expectedSenderID {
			continue
		}
		return row
	}
	return nil
}

func (c *liveUserWSClient) findHistoryAgentMessages(
	ctx context.Context,
	triggerMsgID string,
	expectedTextContains string,
) []map[string]any {
	c.t.Helper()
	if c.harness == nil {
		return nil
	}

	historyCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	history := c.harness.fetchMessageHistory(historyCtx, c.session, 80)
	messages := extractHistoryMessages(history)
	messagesAfterTrigger := filterMessagesAfterTrigger(messages, triggerMsgID)
	agentMessages := filterAgentMessages(messagesAfterTrigger, c.session, "")
	result := make([]map[string]any, 0, len(agentMessages))
	for _, row := range agentMessages {
		content := extractPacketContent(row)
		if expectedTextContains != "" && !strings.Contains(content, expectedTextContains) {
			continue
		}
		result = append(result, cloneMap(row))
	}
	return result
}

func (c *liveUserWSClient) sendPacket(cmd string, payload any) {
	c.t.Helper()
	require.NoError(c.t, c.sendPacketMaybe(cmd, payload))
}

func (c *liveUserWSClient) sendPacketMaybe(cmd string, payload any) error {
	c.t.Helper()
	require.NotNil(c.t, c.conn)
	packet := protocol.Packet{
		Cmd:     cmd,
		Seq:     c.seq,
		Payload: mustMarshalRaw(c.t, payload),
	}
	if err := c.conn.WriteJSON(packet); err != nil {
		return err
	}
	c.seq++
	return nil
}

func (c *liveUserWSClient) sendPacketWithSeq(cmd string, seq int64, payload any) {
	c.t.Helper()
	require.NotNil(c.t, c.conn)
	packet := protocol.Packet{
		Cmd:     cmd,
		Seq:     seq,
		Payload: mustMarshalRaw(c.t, payload),
	}
	require.NoError(c.t, c.conn.WriteJSON(packet))
}

func (c *liveUserWSClient) readPacket(timeout time.Duration) (protocol.Packet, map[string]any) {
	c.t.Helper()
	require.NotNil(c.t, c.conn)
	require.Greater(c.t, timeout, time.Duration(0), "read timeout must be positive")

	for {
		require.NoError(c.t, c.conn.SetReadDeadline(time.Now().Add(timeout)))
		var packet protocol.Packet
		require.NoError(c.t, c.conn.ReadJSON(&packet))
		payload := decodeRawMap(c.t, packet.Payload)

		switch packet.Cmd {
		case protocol.CmdPing:
			c.sendPacketWithSeq(protocol.CmdPong, packet.Seq, map[string]any{
				"server_time": payload["server_time"],
			})
			continue
		case protocol.CmdPushMsg:
			c.sendPacket(protocol.CmdPushAck, map[string]any{
				"session_id": asString(payload["session_id"]),
				"msg_id":     payload["msg_id"],
				"inbox_seq":  payload["inbox_seq"],
			})
		case protocol.CmdKicked:
			c.t.Fatalf("websocket kicked: %v", payload)
		}

		return packet, payload
	}
}

func (c *liveUserWSClient) readPacketMaybe(timeout time.Duration) (protocol.Packet, map[string]any, error) {
	c.t.Helper()
	require.NotNil(c.t, c.conn)
	require.Greater(c.t, timeout, time.Duration(0), "read timeout must be positive")

	for {
		if err := c.conn.SetReadDeadline(time.Now().Add(timeout)); err != nil {
			return protocol.Packet{}, nil, err
		}
		var packet protocol.Packet
		if err := c.conn.ReadJSON(&packet); err != nil {
			return protocol.Packet{}, nil, err
		}
		payload := decodeRawMap(c.t, packet.Payload)

		switch packet.Cmd {
		case protocol.CmdPing:
			c.sendPacketWithSeq(protocol.CmdPong, packet.Seq, map[string]any{
				"server_time": payload["server_time"],
			})
			continue
		case protocol.CmdPushMsg:
			c.sendPacket(protocol.CmdPushAck, map[string]any{
				"session_id": asString(payload["session_id"]),
				"msg_id":     payload["msg_id"],
				"inbox_seq":  payload["inbox_seq"],
			})
		case protocol.CmdKicked:
			return protocol.Packet{}, nil, fmt.Errorf("websocket kicked: %v", payload)
		}

		return packet, payload, nil
	}
}

func isRecoverableWebsocketError(err error) bool {
	if err == nil {
		return false
	}
	if ne, ok := err.(net.Error); ok && ne.Timeout() {
		return true
	}
	if errors.Is(err, io.EOF) || errors.Is(err, net.ErrClosed) {
		return true
	}
	var closeErr *websocket.CloseError
	if errors.As(err, &closeErr) {
		switch closeErr.Code {
		case websocket.CloseAbnormalClosure, websocket.CloseGoingAway, websocket.CloseNoStatusReceived:
			return true
		}
	}
	return websocket.IsUnexpectedCloseError(
		err,
		websocket.CloseAbnormalClosure,
		websocket.CloseGoingAway,
		websocket.CloseNoStatusReceived,
	)
}

func (c *liveUserWSClient) failConversationProbeIfDetected(
	result liveConversationResult,
	opts liveConversationProbeOptions,
) bool {
	c.t.Helper()
	if c.harness == nil {
		return false
	}

	diagnostic := c.harness.collectConversationDiagnostic(c.session, result, opts)
	if diagnostic == nil {
		return false
	}

	failureKind := asString(diagnostic["failure_kind"])
	if failureKind != "openclaw_rate_limit" {
		return false
	}

	c.harness.writeJSON("conversation-diagnostic.json", diagnostic)
	c.t.Fatalf(
		"conversation probe failed early: %s (%s); artifacts=%s",
		asString(diagnostic["failure_summary"]),
		failureKind,
		c.harness.artifactDir,
	)
	return true
}

func (c *liveUserWSClient) failConversationProbe(
	message string,
	result liveConversationResult,
	opts liveConversationProbeOptions,
) {
	c.t.Helper()

	if c.harness != nil {
		diagnostic := c.harness.collectConversationDiagnostic(c.session, result, opts)
		if diagnostic != nil {
			diagnostic["terminal_error"] = message
			c.harness.writeJSON("conversation-diagnostic.json", diagnostic)
			c.t.Fatalf("%s; artifacts=%s", message, c.harness.artifactDir)
		}
	}

	c.t.Fatalf("%s", message)
}

func (c *liveUserWSClient) failMultiAgentConversationProbe(
	message string,
	result liveMultiConversationResult,
	opts liveMultiConversationProbeOptions,
) {
	c.t.Helper()

	if c.harness != nil {
		diagnostic := c.harness.collectMultiConversationDiagnostic(c.session, result, opts)
		diagnostic["terminal_error"] = message
		c.harness.writeJSON("multi-agent-conversation-diagnostic.json", diagnostic)
		c.t.Fatalf("%s; artifacts=%s", message, c.harness.artifactDir)
	}

	c.t.Fatalf("%s", message)
}

func (h *liveAgentHarness) collectConversationDiagnostic(
	session liveProjectSession,
	result liveConversationResult,
	opts liveConversationProbeOptions,
) map[string]any {
	h.t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	history := h.fetchMessageHistory(ctx, session, 20)
	historyMessages := extractHistoryMessages(history)
	messagesAfterTrigger := filterMessagesAfterTrigger(historyMessages, result.TriggerMsgID)
	agentMessagesAfterTrigger := filterAgentMessages(messagesAfterTrigger, session, opts.ExpectedSenderID)
	logMatches := h.collectOpenClawLogMatches(session.SessionID, result.TriggerMsgID)

	failureKind := ""
	failureSummary := ""
	switch {
	case containsAnyLine(logMatches, "rate limit", "429", "FailoverError"):
		failureKind = "openclaw_rate_limit"
		failureSummary = "OpenClaw hit upstream model rate limits before producing a reply"
	case len(agentMessagesAfterTrigger) > 0:
		failureKind = "agent_reply_unmatched"
		failureSummary = "agent replied, but the reply did not match the current probe filters"
	}

	return map[string]any{
		"generated_at":                 time.Now().UTC().Format(time.RFC3339),
		"session_id":                   session.SessionID,
		"trigger_msg_id":               result.TriggerMsgID,
		"expected_sender_id":           opts.ExpectedSenderID,
		"expected_text_contains":       opts.ExpectedTextContains,
		"failure_kind":                 failureKind,
		"failure_summary":              failureSummary,
		"send_ack":                     result.SendAck,
		"delivery_statuses":            result.DeliveryStatuses,
		"output_statuses":              result.OutputStatuses,
		"observed_events":              result.Events,
		"history_messages":             historyMessages,
		"messages_after_trigger":       messagesAfterTrigger,
		"agent_messages_after_trigger": agentMessagesAfterTrigger,
		"openclaw_log_matches":         logMatches,
	}
}

func (h *liveAgentHarness) collectMultiConversationDiagnostic(
	session liveProjectSession,
	result liveMultiConversationResult,
	opts liveMultiConversationProbeOptions,
) map[string]any {
	h.t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	history := h.fetchMessageHistory(ctx, session, 50)
	historyMessages := extractHistoryMessages(history)
	messagesAfterTrigger := filterMessagesAfterTrigger(historyMessages, result.TriggerMsgID)
	agentMessagesAfterTrigger := filterAgentMessages(messagesAfterTrigger, session, "")
	logMatches := h.collectOpenClawLogMatches(session.SessionID, result.TriggerMsgID)

	return map[string]any{
		"generated_at":                 time.Now().UTC().Format(time.RFC3339),
		"session_id":                   session.SessionID,
		"trigger_msg_id":               result.TriggerMsgID,
		"expected_sender_ids":          opts.ExpectedSenderIDs,
		"forbidden_sender_ids":         opts.ForbiddenSenderIDs,
		"expected_text_contains":       opts.ExpectedTextContains,
		"send_ack":                     result.SendAck,
		"delivery_statuses":            result.DeliveryStatuses,
		"output_statuses":              result.OutputStatuses,
		"observed_events":              result.Events,
		"replies_by_sender":            result.RepliesBySender,
		"history_messages":             historyMessages,
		"messages_after_trigger":       messagesAfterTrigger,
		"agent_messages_after_trigger": agentMessagesAfterTrigger,
		"openclaw_log_matches":         logMatches,
	}
}

func (h *liveAgentHarness) fetchMessageHistory(
	ctx context.Context,
	session liveProjectSession,
	limit int,
) map[string]any {
	h.t.Helper()
	if strings.TrimSpace(session.Token) == "" || strings.TrimSpace(session.SessionID) == "" {
		return map[string]any{}
	}

	label := fmt.Sprintf("messages-history-%s", sanitizeFileName(session.SessionID))
	path := fmt.Sprintf("/messages/history?session_id=%s&limit=%d", url.QueryEscape(session.SessionID), limit)
	return h.apiJSON(ctx, label, http.MethodGet, path, session.Token, nil)
}

func (h *liveAgentHarness) collectOpenClawLogMatches(sessionID, triggerMsgID string) []string {
	h.t.Helper()

	patterns := make([]string, 0, 4)
	if sessionID != "" {
		patterns = append(patterns, sessionID)
	}
	if triggerMsgID != "" {
		patterns = append(patterns, triggerMsgID)
	}
	if h.cfg.OpenClawAgent != "" {
		patterns = append(patterns, "session:agent:"+h.cfg.OpenClawAgent)
	}
	if len(patterns) == 0 {
		return nil
	}

	files := []string{
		filepath.Join(os.TempDir(), "openclaw", "openclaw-"+time.Now().Format("2006-01-02")+".log"),
		filepath.Join(os.Getenv("HOME"), ".openclaw", "logs", "gateway.log"),
	}

	matches := make([]string, 0, 12)
	for _, path := range files {
		matches = append(matches, scanFileMatches(path, patterns, 6)...)
	}
	return trimTail(matches, 12)
}

func extractHistoryMessages(history map[string]any) []map[string]any {
	items, _ := history["messages"].([]any)
	result := make([]map[string]any, 0, len(items))
	for _, item := range items {
		row, ok := item.(map[string]any)
		if !ok {
			continue
		}
		result = append(result, row)
	}
	return result
}

func filterMessagesAfterTrigger(messages []map[string]any, triggerMsgID string) []map[string]any {
	if len(messages) == 0 {
		return nil
	}
	if strings.TrimSpace(triggerMsgID) == "" {
		return messages
	}

	triggerID, err := parseFlexibleInt64(triggerMsgID)
	if err != nil {
		return messages
	}

	result := make([]map[string]any, 0, len(messages))
	for _, row := range messages {
		msgID, err := parseFlexibleInt64(row["msg_id"])
		if err != nil {
			continue
		}
		if msgID > triggerID {
			result = append(result, row)
		}
	}
	return result
}

func filterAgentMessages(
	messages []map[string]any,
	session liveProjectSession,
	expectedSenderID string,
) []map[string]any {
	result := make([]map[string]any, 0, len(messages))
	for _, row := range messages {
		senderID := asString(row["sender_id"])
		if senderID == "" || senderID == asString(session.User["id"]) {
			continue
		}
		if expectedSenderID != "" && senderID != expectedSenderID {
			continue
		}
		result = append(result, row)
	}
	return result
}

func containsAnyLine(lines []string, needles ...string) bool {
	for _, line := range lines {
		lower := strings.ToLower(line)
		for _, needle := range needles {
			if strings.Contains(lower, strings.ToLower(needle)) {
				return true
			}
		}
	}
	return false
}

func scanFileMatches(path string, patterns []string, maxMatches int) []string {
	if !fileExists(path) {
		return nil
	}

	file, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer file.Close()

	matches := make([]string, 0, maxMatches)
	scanner := bufio.NewScanner(file)
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		if !containsAnyPattern(line, patterns) {
			continue
		}
		matches = append(matches, line)
		if len(matches) > maxMatches {
			matches = matches[len(matches)-maxMatches:]
		}
	}
	return matches
}

func containsAnyPattern(line string, patterns []string) bool {
	for _, pattern := range patterns {
		if pattern != "" && strings.Contains(line, pattern) {
			return true
		}
	}
	return false
}

func trimTail[T any](items []T, max int) []T {
	if len(items) <= max {
		return items
	}
	return items[len(items)-max:]
}

func cloneMap(input map[string]any) map[string]any {
	if input == nil {
		return nil
	}
	result := make(map[string]any, len(input))
	for key, value := range input {
		result[key] = value
	}
	return result
}

func extractPacketContent(payload map[string]any) string {
	return firstNonEmpty(asString(payload["content"]), asString(payload["final_content"]))
}

func liveMessageDedupKey(payload map[string]any) string {
	msgID := asString(payload["msg_id"])
	if msgID != "" {
		return msgID
	}
	return fmt.Sprintf("%s|%s|%s", asString(payload["sender_id"]), asString(payload["cmd"]), extractPacketContent(payload))
}

func minDuration(left, right time.Duration) time.Duration {
	if left < right {
		return left
	}
	return right
}

func openClawPluginState(payload map[string]any) map[string]any {
	if plugin, _ := payload["plugin"].(map[string]any); plugin != nil {
		return plugin
	}
	return payload
}

func findRepoRoot(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	require.NoError(t, err)

	dir := wd
	for {
		if fileExists(filepath.Join(dir, "backend", "go.mod")) {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("unable to locate repo root from %s", wd)
		}
		dir = parent
	}
}

func resolveOpenClawCommand(cfg liveAgentConfig) ([]string, error) {
	if cfg.OpenClawBin != "" {
		if resolved, err := exec.LookPath(cfg.OpenClawBin); err == nil {
			return []string{resolved}, nil
		}
		if fileExists(cfg.OpenClawBin) {
			return []string{cfg.OpenClawBin}, nil
		}
		return nil, fmt.Errorf("openclaw binary not found: %s", cfg.OpenClawBin)
	}

	if resolved, err := exec.LookPath("openclaw"); err == nil {
		return []string{resolved}, nil
	}

	nodePath, err := exec.LookPath("node")
	if err != nil {
		return nil, fmt.Errorf("node not found while resolving openclaw.mjs fallback: %w", err)
	}
	entry := filepath.Join(cfg.OpenClawRepo, "openclaw.mjs")
	if !fileExists(entry) {
		return nil, fmt.Errorf("openclaw.mjs not found at %s", entry)
	}
	return []string{nodePath, entry}, nil
}

func deriveWSURL(apiBase string) (string, error) {
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
	if strings.HasSuffix(path, "/v1") {
		path = strings.TrimSuffix(path, "/v1")
	}
	if path == "" {
		parsed.Path = "/ws"
	} else {
		parsed.Path = path + "/ws"
	}
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return parsed.String(), nil
}

func extractJSONPayload(raw string) (any, error) {
	lines := strings.Split(raw, "\n")
	for idx, line := range lines {
		text := strings.TrimSpace(line)
		if text == "" {
			continue
		}
		if !looksLikeJSON(text) {
			continue
		}
		candidate := strings.TrimSpace(strings.Join(lines[idx:], "\n"))
		var payload any
		dec := json.NewDecoder(strings.NewReader(candidate))
		dec.UseNumber()
		if err := dec.Decode(&payload); err == nil {
			return payload, nil
		}
	}
	return nil, fmt.Errorf("output does not contain JSON payload")
}

func looksLikeJSON(text string) bool {
	if text == "" {
		return false
	}
	switch text[0] {
	case '{', '[', '"':
		return true
	}
	if text == "true" || text == "false" || text == "null" {
		return true
	}
	return liveNumberPattern.MatchString(text)
}

func versionGE(left, right string) bool {
	return compareVersion(parseVersion(left), parseVersion(right)) >= 0
}

func parseVersion(input string) []int {
	matches := regexp.MustCompile(`\d+`).FindAllString(input, -1)
	values := make([]int, 0, 4)
	for _, item := range matches {
		num, _ := strconv.Atoi(item)
		values = append(values, num)
		if len(values) == 4 {
			return values
		}
	}
	for len(values) < 4 {
		values = append(values, 0)
	}
	return values
}

func compareVersion(left, right []int) int {
	size := len(left)
	if len(right) > size {
		size = len(right)
	}
	for i := 0; i < size; i++ {
		lv := 0
		if i < len(left) {
			lv = left[i]
		}
		rv := 0
		if i < len(right) {
			rv = right[i]
		}
		switch {
		case lv > rv:
			return 1
		case lv < rv:
			return -1
		}
	}
	return 0
}

func buildEffectiveTools(globalTools any, agentsList any, agentID string) []string {
	names := make([]string, 0, 16)
	if items, ok := globalTools.([]any); ok {
		for _, item := range items {
			names = append(names, asString(item))
		}
	}
	if items, ok := agentsList.([]any); ok {
		for _, item := range items {
			row, ok := item.(map[string]any)
			if !ok {
				continue
			}
			matchID := firstNonEmpty(asString(row["id"]), asString(row["name"]))
			if matchID != agentID {
				continue
			}
			tools, _ := row["tools"].(map[string]any)
			if tools == nil {
				break
			}
			if alsoAllow, _ := tools["alsoAllow"].([]any); alsoAllow != nil {
				for _, tool := range alsoAllow {
					names = append(names, asString(tool))
				}
			}
			break
		}
	}

	seen := map[string]struct{}{}
	result := make([]string, 0, len(names))
	for _, item := range names {
		name := strings.TrimSpace(item)
		if name == "" {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		result = append(result, name)
	}
	return result
}

func summarizeOpenClawSessions(payload any, limit int) map[string]any {
	result := map[string]any{
		"total":  0,
		"direct": []map[string]any{},
		"group":  []map[string]any{},
	}
	root, _ := payload.(map[string]any)
	if root == nil {
		return result
	}
	items, _ := root["sessions"].([]any)
	if items == nil {
		return result
	}

	filtered := make([]map[string]any, 0, len(items))
	for _, item := range items {
		row, ok := item.(map[string]any)
		if !ok {
			continue
		}
		filtered = append(filtered, map[string]any{
			"key":       row["key"],
			"kind":      asString(row["kind"]),
			"sessionId": row["sessionId"],
			"updatedAt": row["updatedAt"],
			"ageMs":     row["ageMs"],
			"model":     row["model"],
			"agentId":   row["agentId"],
		})
	}
	result["total"] = len(filtered)

	direct := make([]map[string]any, 0, limit)
	group := make([]map[string]any, 0, limit)
	for _, row := range filtered {
		switch asString(row["kind"]) {
		case "direct":
			if len(direct) < limit {
				direct = append(direct, row)
			}
		case "group":
			if len(group) < limit {
				group = append(group, row)
			}
		}
	}
	result["direct"] = direct
	result["group"] = group
	return result
}

func missingNames(required []string, available []string) []string {
	seen := map[string]struct{}{}
	for _, item := range available {
		seen[item] = struct{}{}
	}
	result := make([]string, 0, len(required))
	for _, item := range required {
		if _, ok := seen[item]; !ok {
			result = append(result, item)
		}
	}
	return result
}

func decodeRawMap(t *testing.T, raw json.RawMessage) map[string]any {
	t.Helper()
	if len(raw) == 0 {
		return map[string]any{}
	}
	var payload map[string]any
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	require.NoError(t, dec.Decode(&payload))
	return payload
}

func mustMarshalRaw(t *testing.T, payload any) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(payload)
	require.NoError(t, err)
	return raw
}

func writeJSONFile(t *testing.T, path string, payload any) {
	t.Helper()
	raw, err := json.MarshalIndent(payload, "", "  ")
	require.NoError(t, err)
	writeTextFile(t, path, string(raw))
}

func writeTextFile(t *testing.T, path string, content string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
}

func envOrDefault(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

func envInt(key string, fallback int) int {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	num, err := strconv.Atoi(value)
	if err != nil {
		return fallback
	}
	return num
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func sanitizeFileName(name string) string {
	name = strings.ReplaceAll(name, "/", "_")
	name = strings.ReplaceAll(name, " ", "_")
	return name
}

func maskSecret(value string) string {
	value = strings.TrimSpace(value)
	if len(value) <= 8 {
		return value
	}
	return value[:4] + "..." + value[len(value)-4:]
}

func asString(value any) string {
	switch v := value.(type) {
	case nil:
		return ""
	case string:
		return strings.TrimSpace(v)
	case json.Number:
		return v.String()
	case fmt.Stringer:
		return strings.TrimSpace(v.String())
	default:
		return strings.TrimSpace(fmt.Sprintf("%v", v))
	}
}

func asBool(value any) bool {
	switch v := value.(type) {
	case bool:
		return v
	case string:
		return strings.EqualFold(strings.TrimSpace(v), "true")
	case json.Number:
		return v.String() == "1"
	default:
		return false
	}
}

func parseFlexibleInt64(value any) (int64, error) {
	switch v := value.(type) {
	case nil:
		return 0, fmt.Errorf("empty value")
	case int64:
		return v, nil
	case int:
		return int64(v), nil
	case float64:
		return int64(v), nil
	case json.Number:
		return v.Int64()
	case string:
		return strconv.ParseInt(strings.TrimSpace(v), 10, 64)
	default:
		return 0, fmt.Errorf("unsupported int value %T", value)
	}
}

func firstString(values []string) string {
	if len(values) == 0 {
		return ""
	}
	return strings.TrimSpace(values[0])
}

func firstNonEmpty(values ...string) string {
	for _, item := range values {
		if strings.TrimSpace(item) != "" {
			return strings.TrimSpace(item)
		}
	}
	return ""
}

func uniqueTrimmedStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	result := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		result = append(result, trimmed)
	}
	if len(result) == 0 {
		return nil
	}
	return result
}

func isLiveEligibleGroupAgent(row map[string]any) bool {
	return isLiveEligibleAgentWithClientType(row, "openclaw")
}

func isLiveEligibleAgentWithClientType(row map[string]any, clientType string) bool {
	if row == nil {
		return false
	}
	if asString(row["id"]) == "" {
		return false
	}
	if !strings.EqualFold(asString(row["agent_client_type"]), strings.TrimSpace(clientType)) {
		return false
	}
	if !asBool(row["online"]) {
		return false
	}
	if status, err := parseFlexibleInt64(row["status"]); err == nil && status != 1 {
		return false
	}
	return true
}

func (h *liveAgentHarness) drawRandomAgentCandidates(
	agents []map[string]any,
	count int,
	label string,
) []map[string]any {
	h.t.Helper()
	require.GreaterOrEqual(h.t, count, 1, "count must be positive")

	candidates := make([]map[string]any, 0, len(agents))
	for _, row := range agents {
		if row == nil || asString(row["id"]) == "" {
			continue
		}
		candidates = append(candidates, cloneMap(row))
	}

	sort.SliceStable(candidates, func(i, j int) bool {
		return asString(candidates[i]["id"]) < asString(candidates[j]["id"])
	})

	require.GreaterOrEqualf(
		h.t,
		len(candidates),
		count,
		"need at least %d eligible agents after filtering for %s",
		count,
		label,
	)

	perm := h.randomPicker.Perm(len(candidates))
	selected := make([]map[string]any, 0, count)
	for _, idx := range perm[:count] {
		selected = append(selected, cloneMap(candidates[idx]))
	}

	h.writeJSON(label+".json", map[string]any{
		"generated_at":    time.Now().UTC().Format(time.RFC3339),
		"random_seed":     h.randomSeed,
		"candidate_count": len(candidates),
		"candidates":      candidates,
		"selected":        selected,
	})
	return selected
}

func (h *liveAgentHarness) drawRandomEligibleAgents(
	agents []map[string]any,
	count int,
	label string,
) []map[string]any {
	return h.drawRandomEligibleAgentsExcluding(agents, count, label)
}

func (h *liveAgentHarness) drawRandomEligibleAgentsExcluding(
	agents []map[string]any,
	count int,
	label string,
	excludedIDs ...string,
) []map[string]any {
	h.t.Helper()
	require.GreaterOrEqual(h.t, count, 1, "count must be positive")

	excluded := make(map[string]struct{}, len(excludedIDs))
	for _, id := range excludedIDs {
		trimmed := strings.TrimSpace(id)
		if trimmed == "" {
			continue
		}
		excluded[trimmed] = struct{}{}
	}

	candidates := make([]map[string]any, 0, len(agents))
	for _, row := range agents {
		if !isLiveEligibleGroupAgent(row) {
			continue
		}
		if _, skip := excluded[asString(row["id"])]; skip {
			continue
		}
		candidates = append(candidates, cloneMap(row))
	}

	sort.SliceStable(candidates, func(i, j int) bool {
		return asString(candidates[i]["id"]) < asString(candidates[j]["id"])
	})

	require.GreaterOrEqualf(
		h.t,
		len(candidates),
		count,
		"need at least %d online openclaw agents under the test account after exclusions; offline or non-openclaw agents are intentionally excluded",
		count,
	)

	perm := h.randomPicker.Perm(len(candidates))
	selected := make([]map[string]any, 0, count)
	for _, idx := range perm[:count] {
		selected = append(selected, cloneMap(candidates[idx]))
	}

	h.writeJSON(label+".json", map[string]any{
		"generated_at":    time.Now().UTC().Format(time.RFC3339),
		"random_seed":     h.randomSeed,
		"candidate_count": len(candidates),
		"excluded_ids":    uniqueTrimmedStrings(excludedIDs),
		"candidates":      candidates,
		"selected":        selected,
	})
	return selected
}

func findSessionDetailMember(
	detail map[string]any,
	memberID string,
	memberType int64,
) map[string]any {
	items, _ := detail["members"].([]any)
	for _, item := range items {
		row, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if asString(row["member_id"]) != strings.TrimSpace(memberID) {
			continue
		}
		if gotType, err := parseFlexibleInt64(row["member_type"]); err == nil && gotType != memberType {
			continue
		}
		return cloneMap(row)
	}
	return nil
}

func ternaryStatus(ok bool) string {
	if ok {
		return "pass"
	}
	return "fail"
}
