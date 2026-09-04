package agentapi

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/askie/grix/backend/internal/agentadapter"
	acpadapter "github.com/askie/grix/backend/internal/agentadapter/acp"
	"github.com/askie/grix/backend/internal/agentadapter/claude"
	"github.com/askie/grix/backend/internal/agentadapter/codex"
	"github.com/askie/grix/backend/internal/agentadapter/gemini"
	"github.com/askie/grix/backend/internal/agentadapter/hermes"
	"github.com/askie/grix/backend/internal/agentadapter/openclaw"
	"github.com/askie/grix/backend/internal/agentadapter/qwen"
	"github.com/askie/grix/backend/internal/model"
	pkgagentapi "github.com/askie/grix/backend/internal/pkg/agentapi"
	"github.com/askie/grix/backend/internal/pkg/logger"
	"github.com/askie/grix/backend/internal/pkg/testutil"
	"github.com/askie/grix/backend/internal/store"
	"github.com/askie/grix/backend/internal/ws/protocol"
	"github.com/gorilla/websocket"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

func TestServeWS_PersistsAgentClientTypeFromAuth(t *testing.T) {
	testDB := testutil.NewTestDB()
	defer testDB.Close()

	originalDB := store.DB
	store.DB = testDB.DB
	t.Cleanup(func() {
		store.DB = originalDB
	})

	const (
		agentID = int64(91011)
		ownerID = int64(82022)
		apiKey  = "ak_test_agent_ws_client_type"
	)

	agent := model.Agent{
		ID:           agentID,
		AgentName:    "ws-agent",
		SystemPrompt: "auth ack business prompt",
		OwnerID:      ownerID,
		ProviderType: model.AgentProviderAPI,
		Status:       model.AgentStatusActive,
		APIKeyHash:   pkgagentapi.HashAPIKey(apiKey),
		APIKeyHint:   pkgagentapi.APIKeyHint(apiKey),
	}
	if err := store.DB.Create(&agent).Error; err != nil {
		t.Fatalf("create agent: %v", err)
	}

	mgr := NewManager("", 30*time.Second, nil, nil, nil, nil)
	defer mgr.Shutdown()
	srv, closeSrv := newAgentWSTestServer(mgr)
	defer closeSrv()

	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(srv.URL, "http")+"/?agent_id=91011", nil)
	if err != nil {
		t.Fatalf("dial websocket: %v", err)
	}
	defer conn.Close()

	authPayload, err := json.Marshal(protocol.Packet{
		Cmd: "auth",
		Seq: 1,
		Payload: mustMarshalRawJSON(t, map[string]any{
			"agent_id":    "91011",
			"api_key":     apiKey,
			"client":      "openclaw-grix",
			"client_type": model.AgentClientTypeOpenClaw,
		}),
	})
	if err != nil {
		t.Fatalf("marshal auth packet: %v", err)
	}
	if err := conn.WriteMessage(websocket.TextMessage, authPayload); err != nil {
		t.Fatalf("write auth packet: %v", err)
	}

	_, raw, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("read auth ack: %v", err)
	}

	var ack protocol.Packet
	if err := json.Unmarshal(raw, &ack); err != nil {
		t.Fatalf("unmarshal auth ack: %v", err)
	}
	if ack.Cmd != "auth_ack" {
		t.Fatalf("expected auth_ack, got %s", ack.Cmd)
	}

	var payload AuthAckPayload
	if err := json.Unmarshal(ack.Payload, &payload); err != nil {
		t.Fatalf("unmarshal auth ack payload: %v", err)
	}
	if payload.Code != 0 {
		t.Fatalf("expected auth success, got code=%d msg=%s", payload.Code, payload.Msg)
	}
	if payload.SystemPrompt != "auth ack business prompt" {
		t.Fatalf("system_prompt=%q", payload.SystemPrompt)
	}

	var stored model.Agent
	if err := store.DB.First(&stored, agentID).Error; err != nil {
		t.Fatalf("query agent: %v", err)
	}
	if stored.AgentClientType != model.AgentClientTypeOpenClaw {
		t.Fatalf("expected agent_client_type=%q, got %q", model.AgentClientTypeOpenClaw, stored.AgentClientType)
	}
}

func TestServeWS_RejectsInvalidAgentClientType(t *testing.T) {
	testDB := testutil.NewTestDB()
	defer testDB.Close()

	originalDB := store.DB
	store.DB = testDB.DB
	t.Cleanup(func() {
		store.DB = originalDB
	})

	const (
		agentID = int64(91012)
		ownerID = int64(82023)
		apiKey  = "ak_test_agent_ws_invalid_client_type"
	)

	agent := model.Agent{
		ID:           agentID,
		AgentName:    "invalid-type-agent",
		OwnerID:      ownerID,
		ProviderType: model.AgentProviderAPI,
		Status:       model.AgentStatusActive,
		APIKeyHash:   pkgagentapi.HashAPIKey(apiKey),
		APIKeyHint:   pkgagentapi.APIKeyHint(apiKey),
	}
	if err := store.DB.Create(&agent).Error; err != nil {
		t.Fatalf("create agent: %v", err)
	}

	mgr := NewManager("", 30*time.Second, nil, nil, nil, nil)
	defer mgr.Shutdown()
	srv, closeSrv := newAgentWSTestServer(mgr)
	defer closeSrv()

	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(srv.URL, "http")+"/?agent_id=91012", nil)
	if err != nil {
		t.Fatalf("dial websocket: %v", err)
	}
	defer conn.Close()

	authPayload, err := json.Marshal(protocol.Packet{
		Cmd: "auth",
		Seq: 1,
		Payload: mustMarshalRawJSON(t, map[string]any{
			"agent_id":    "91012",
			"api_key":     apiKey,
			"client_type": "unknown-client",
		}),
	})
	if err != nil {
		t.Fatalf("marshal auth packet: %v", err)
	}
	if err := conn.WriteMessage(websocket.TextMessage, authPayload); err != nil {
		t.Fatalf("write auth packet: %v", err)
	}

	_, raw, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("read auth ack: %v", err)
	}

	var ack protocol.Packet
	if err := json.Unmarshal(raw, &ack); err != nil {
		t.Fatalf("unmarshal auth ack: %v", err)
	}

	var payload AuthAckPayload
	if err := json.Unmarshal(ack.Payload, &payload); err != nil {
		t.Fatalf("unmarshal auth ack payload: %v", err)
	}
	if payload.Code != 10003 {
		t.Fatalf("expected code=10003, got %d", payload.Code)
	}
	if payload.Msg != "invalid client_type" {
		t.Fatalf("expected invalid client_type msg, got %q", payload.Msg)
	}

	var stored model.Agent
	if err := store.DB.First(&stored, agentID).Error; err != nil {
		t.Fatalf("query agent: %v", err)
	}
	if stored.AgentClientType != "" {
		t.Fatalf("expected agent_client_type to stay empty, got %q", stored.AgentClientType)
	}
}

func TestMissingDeclaredNames(t *testing.T) {
	missing := missingDeclaredNames(
		[]string{" session_control ", "SET_MODEL", "set_mode"},
		[]string{"get_context", "set_model", "set_mode"},
	)
	if got, want := missing, []string{"get_context"}; !equalStringSlices(got, want) {
		t.Fatalf("missingDeclaredNames=%v want=%v", got, want)
	}

	none := missingDeclaredNames(
		[]string{"get_context", "set_model", "set_mode"},
		[]string{"get_context", "set_model", "set_mode"},
	)
	if len(none) != 0 {
		t.Fatalf("missingDeclaredNames=%v want=[]", none)
	}
}

func TestServeWS_CodexAuthWarnsWhenRequiredLocalActionsMissing(t *testing.T) {
	testDB := testutil.NewTestDB()
	defer testDB.Close()

	originalDB := store.DB
	store.DB = testDB.DB
	t.Cleanup(func() {
		store.DB = originalDB
	})

	observedCore, observedLogs := observer.New(zap.WarnLevel)
	originalLogger := logger.L
	logger.L = zap.New(observedCore).Sugar()
	t.Cleanup(func() {
		logger.L = originalLogger
	})

	const (
		agentID = int64(91040)
		ownerID = int64(82051)
		apiKey  = "ak_test_agent_ws_codex_missing_actions_warn"
	)

	agent := model.Agent{
		ID:           agentID,
		AgentName:    "codex-missing-actions-agent",
		OwnerID:      ownerID,
		ProviderType: model.AgentProviderAPI,
		Status:       model.AgentStatusActive,
		APIKeyHash:   pkgagentapi.HashAPIKey(apiKey),
		APIKeyHint:   pkgagentapi.APIKeyHint(apiKey),
	}
	if err := store.DB.Create(&agent).Error; err != nil {
		t.Fatalf("create agent: %v", err)
	}

	mgr := NewManager("", 30*time.Second, nil, nil, nil, nil)
	defer mgr.Shutdown()
	srv, closeSrv := newAgentWSTestServer(mgr)
	defer closeSrv()

	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(srv.URL, "http")+"/?agent_id=91040", nil)
	if err != nil {
		t.Fatalf("dial websocket: %v", err)
	}
	defer conn.Close()

	authPayload, err := json.Marshal(protocol.Packet{
		Cmd: "auth",
		Seq: 1,
		Payload: mustMarshalRawJSON(t, map[string]any{
			"agent_id":      "91040",
			"api_key":       apiKey,
			"client":        "grix-codex",
			"client_type":   model.AgentClientTypeCodex,
			"host_version":  "0.2.5",
			"capabilities":  []string{"local_action_v1"},
			"local_actions": []string{"session_control", "thread_compact"},
		}),
	})
	if err != nil {
		t.Fatalf("marshal auth packet: %v", err)
	}
	if err := conn.WriteMessage(websocket.TextMessage, authPayload); err != nil {
		t.Fatalf("write auth packet: %v", err)
	}

	_, raw, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("read auth ack: %v", err)
	}

	var ack protocol.Packet
	if err := json.Unmarshal(raw, &ack); err != nil {
		t.Fatalf("unmarshal auth ack: %v", err)
	}
	if ack.Cmd != "auth_ack" {
		t.Fatalf("expected auth_ack, got %s", ack.Cmd)
	}

	foundWarn := false
	for _, entry := range observedLogs.All() {
		if strings.Contains(entry.Message, "codex auth missing required local_actions") {
			foundWarn = true
			if !strings.Contains(entry.Message, "set_model") || !strings.Contains(entry.Message, "set_mode") {
				t.Fatalf("warning message should mention missing model/mode actions, got=%q", entry.Message)
			}
		}
	}
	if !foundWarn {
		t.Fatal("expected codex missing required local_actions warning")
	}
}

func TestServeWS_AuthAckIncludesAdapterCapabilities(t *testing.T) {
	testDB := testutil.NewTestDB()
	defer testDB.Close()

	originalDB := store.DB
	store.DB = testDB.DB
	t.Cleanup(func() {
		store.DB = originalDB
	})

	const (
		agentID = int64(91013)
		ownerID = int64(82024)
		apiKey  = "ak_test_agent_ws_adapter_caps"
	)

	agent := model.Agent{
		ID:           agentID,
		AgentName:    "adapter-cap-agent",
		OwnerID:      ownerID,
		ProviderType: model.AgentProviderAPI,
		Status:       model.AgentStatusActive,
		APIKeyHash:   pkgagentapi.HashAPIKey(apiKey),
		APIKeyHint:   pkgagentapi.APIKeyHint(apiKey),
	}
	if err := store.DB.Create(&agent).Error; err != nil {
		t.Fatalf("create agent: %v", err)
	}

	mgr := NewManager("", 30*time.Second, nil, nil, nil, nil)
	defer mgr.Shutdown()
	mgr.adapterRegistry = agentadapter.NewRegistry()
	mgr.adapterRegistry.Register(openclaw.NewAdapter())

	srv, closeSrv := newAgentWSTestServer(mgr)
	defer closeSrv()

	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(srv.URL, "http")+"/?agent_id=91013", nil)
	if err != nil {
		t.Fatalf("dial websocket: %v", err)
	}
	defer conn.Close()

	authPayload, err := json.Marshal(protocol.Packet{
		Cmd: "auth",
		Seq: 1,
		Payload: mustMarshalRawJSON(t, map[string]any{
			"agent_id":         "91013",
			"api_key":          apiKey,
			"client":           "openclaw-grix",
			"client_type":      model.AgentClientTypeOpenClaw,
			"contract_version": 1,
			"host_type":        openclaw.Family,
			"host_version":     "2026.4.2",
			"capabilities": []string{
				"stream_chunk",
				"session_route",
				"local_action_v1",
				"agent_invoke",
				"inbound_media_v1",
				"reaction_v1",
				"thread_v1",
				"unknown_cap",
			},
			"local_actions": []string{"exec_approve", "exec_reject"},
		}),
	})
	if err != nil {
		t.Fatalf("marshal auth packet: %v", err)
	}
	if err := conn.WriteMessage(websocket.TextMessage, authPayload); err != nil {
		t.Fatalf("write auth packet: %v", err)
	}

	_, raw, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("read auth ack: %v", err)
	}

	var ack protocol.Packet
	if err := json.Unmarshal(raw, &ack); err != nil {
		t.Fatalf("unmarshal auth ack: %v", err)
	}
	if ack.Cmd != "auth_ack" {
		t.Fatalf("expected auth_ack, got %s", ack.Cmd)
	}

	var payload AuthAckPayload
	if err := json.Unmarshal(ack.Payload, &payload); err != nil {
		t.Fatalf("unmarshal auth ack payload: %v", err)
	}
	if payload.Code != 0 {
		t.Fatalf("expected auth success, got code=%d msg=%s", payload.Code, payload.Msg)
	}
	if payload.ContractVersion != 1 {
		t.Fatalf("contract_version=%d want=1", payload.ContractVersion)
	}
	if payload.AdapterID != openclaw.AdapterID {
		t.Fatalf("adapter_id=%q want=%q", payload.AdapterID, openclaw.AdapterID)
	}
	if got, want := payload.SupportedCapabilities, []string{
		"stream_chunk",
		"session_route",
		"local_action_v1",
		"agent_invoke",
		"inbound_media_v1",
		"reaction_v1",
		"thread_v1",
	}; !equalStringSlices(got, want) {
		t.Fatalf("supported_capabilities=%v want=%v", got, want)
	}
	if got := payload.DegradedCapabilities; len(got) != 0 {
		t.Fatalf("degraded_capabilities=%v want=[]", got)
	}
	if got, want := payload.UnsupportedCapabilities, []string{"unknown_cap"}; !equalStringSlices(got, want) {
		t.Fatalf("unsupported_capabilities=%v want=%v", got, want)
	}
}

func TestServeWS_ClaudeAuthAckMatchesDeclaredCapabilities(t *testing.T) {
	testDB := testutil.NewTestDB()
	defer testDB.Close()

	originalDB := store.DB
	store.DB = testDB.DB
	t.Cleanup(func() {
		store.DB = originalDB
	})

	const (
		agentID = int64(91016)
		ownerID = int64(82027)
		apiKey  = "ak_test_agent_ws_claude_contract"
	)

	agent := model.Agent{
		ID:           agentID,
		AgentName:    "claude-adapter-agent",
		OwnerID:      ownerID,
		ProviderType: model.AgentProviderAPI,
		Status:       model.AgentStatusActive,
		APIKeyHash:   pkgagentapi.HashAPIKey(apiKey),
		APIKeyHint:   pkgagentapi.APIKeyHint(apiKey),
	}
	if err := store.DB.Create(&agent).Error; err != nil {
		t.Fatalf("create agent: %v", err)
	}

	mgr := NewManager("", 30*time.Second, nil, nil, nil, nil)
	defer mgr.Shutdown()
	mgr.adapterRegistry = agentadapter.NewRegistry()
	mgr.adapterRegistry.Register(claude.NewAdapter())

	srv, closeSrv := newAgentWSTestServer(mgr)
	defer closeSrv()

	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(srv.URL, "http")+"/?agent_id=91016", nil)
	if err != nil {
		t.Fatalf("dial websocket: %v", err)
	}
	defer conn.Close()

	authPayload, err := json.Marshal(protocol.Packet{
		Cmd: "auth",
		Seq: 1,
		Payload: mustMarshalRawJSON(t, map[string]any{
			"agent_id":         "91016",
			"api_key":          apiKey,
			"client":           "claude-grix",
			"client_type":      model.AgentClientTypeClaude,
			"contract_version": 1,
			"host_type":        claude.Family,
			"host_version":     "1.2.0",
			"adapter_hint":     claude.AdapterID,
			"capabilities":     []string{"session_route", "local_action_v1", "agent_invoke"},
			"local_actions":    []string{"session_control", "claude_interaction_reply"},
		}),
	})
	if err != nil {
		t.Fatalf("marshal auth packet: %v", err)
	}
	if err := conn.WriteMessage(websocket.TextMessage, authPayload); err != nil {
		t.Fatalf("write auth packet: %v", err)
	}

	_, raw, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("read auth ack: %v", err)
	}

	var ack protocol.Packet
	if err := json.Unmarshal(raw, &ack); err != nil {
		t.Fatalf("unmarshal auth ack: %v", err)
	}
	if ack.Cmd != "auth_ack" {
		t.Fatalf("expected auth_ack, got %s", ack.Cmd)
	}

	var payload AuthAckPayload
	if err := json.Unmarshal(ack.Payload, &payload); err != nil {
		t.Fatalf("unmarshal auth ack payload: %v", err)
	}
	if payload.Code != 0 {
		t.Fatalf("expected auth success, got code=%d msg=%s", payload.Code, payload.Msg)
	}
	if payload.AdapterID != claude.AdapterID {
		t.Fatalf("adapter_id=%q want=%q", payload.AdapterID, claude.AdapterID)
	}
	if got, want := payload.SupportedCapabilities, []string{"session_route", "local_action_v1", "agent_invoke"}; !equalStringSlices(got, want) {
		t.Fatalf("supported_capabilities=%v want=%v", got, want)
	}
	if got := payload.DegradedCapabilities; len(got) != 0 {
		t.Fatalf("degraded_capabilities=%v want=[]", got)
	}
	if got, want := payload.UnsupportedCapabilities, []string{}; !equalStringSlices(got, want) {
		t.Fatalf("unsupported_capabilities=%v want=%v", got, want)
	}
}

func TestServeWS_CodexAuthAckMatchesDeclaredCapabilities(t *testing.T) {
	testDB := testutil.NewTestDB()
	defer testDB.Close()

	originalDB := store.DB
	store.DB = testDB.DB
	t.Cleanup(func() {
		store.DB = originalDB
	})

	const (
		agentID = int64(91017)
		ownerID = int64(82028)
		apiKey  = "ak_test_agent_ws_codex_contract"
	)

	agent := model.Agent{
		ID:           agentID,
		AgentName:    "codex-adapter-agent",
		OwnerID:      ownerID,
		ProviderType: model.AgentProviderAPI,
		Status:       model.AgentStatusActive,
		APIKeyHash:   pkgagentapi.HashAPIKey(apiKey),
		APIKeyHint:   pkgagentapi.APIKeyHint(apiKey),
	}
	if err := store.DB.Create(&agent).Error; err != nil {
		t.Fatalf("create agent: %v", err)
	}

	mgr := NewManager("", 30*time.Second, nil, nil, nil, nil)
	defer mgr.Shutdown()
	mgr.adapterRegistry = agentadapter.NewRegistry()
	mgr.adapterRegistry.Register(openclaw.NewAdapter())
	mgr.adapterRegistry.Register(codex.NewAdapter())

	srv, closeSrv := newAgentWSTestServer(mgr)
	defer closeSrv()

	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(srv.URL, "http")+"/?agent_id=91017", nil)
	if err != nil {
		t.Fatalf("dial websocket: %v", err)
	}
	defer conn.Close()

	authPayload, err := json.Marshal(protocol.Packet{
		Cmd: "auth",
		Seq: 1,
		Payload: mustMarshalRawJSON(t, map[string]any{
			"agent_id":         "91017",
			"api_key":          apiKey,
			"client":           "grix-codex",
			"client_type":      model.AgentClientTypeOpenClaw,
			"contract_version": 1,
			"client_version":   "0.1.0",
			"host_type":        model.AgentClientTypeOpenClaw,
			"host_version":     "0.1.0",
			"protocol_version": protocol.AgentAPIProtocolVersion,
			"adapter_hint":     codex.AdapterID,
			"capabilities":     []string{"local_action_v1"},
			"local_actions":    []string{"session_control"},
		}),
	})
	if err != nil {
		t.Fatalf("marshal auth packet: %v", err)
	}
	if err := conn.WriteMessage(websocket.TextMessage, authPayload); err != nil {
		t.Fatalf("write auth packet: %v", err)
	}

	_, raw, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("read auth ack: %v", err)
	}

	var ack protocol.Packet
	if err := json.Unmarshal(raw, &ack); err != nil {
		t.Fatalf("unmarshal auth ack: %v", err)
	}
	if ack.Cmd != "auth_ack" {
		t.Fatalf("expected auth_ack, got %s", ack.Cmd)
	}

	var payload AuthAckPayload
	if err := json.Unmarshal(ack.Payload, &payload); err != nil {
		t.Fatalf("unmarshal auth ack payload: %v", err)
	}
	if payload.Code != 0 {
		t.Fatalf("expected auth success, got code=%d msg=%s", payload.Code, payload.Msg)
	}
	if payload.AdapterID != codex.AdapterID {
		t.Fatalf("adapter_id=%q want=%q", payload.AdapterID, codex.AdapterID)
	}
	if got, want := payload.SupportedCapabilities, []string{"local_action_v1"}; !equalStringSlices(got, want) {
		t.Fatalf("supported_capabilities=%v want=%v", got, want)
	}
	if got := payload.DegradedCapabilities; len(got) != 0 {
		t.Fatalf("degraded_capabilities=%v want=[]", got)
	}
	if got, want := payload.UnsupportedCapabilities, []string{}; !equalStringSlices(got, want) {
		t.Fatalf("unsupported_capabilities=%v want=%v", got, want)
	}
}

func TestServeWS_GeminiAuthAckMatchesDeclaredCapabilities(t *testing.T) {
	testDB := testutil.NewTestDB()
	defer testDB.Close()

	originalDB := store.DB
	store.DB = testDB.DB
	t.Cleanup(func() {
		store.DB = originalDB
	})

	const (
		agentID = int64(91018)
		ownerID = int64(82029)
		apiKey  = "ak_test_agent_ws_gemini_contract"
	)

	agent := model.Agent{
		ID:           agentID,
		AgentName:    "gemini-adapter-agent",
		OwnerID:      ownerID,
		ProviderType: model.AgentProviderAPI,
		Status:       model.AgentStatusActive,
		APIKeyHash:   pkgagentapi.HashAPIKey(apiKey),
		APIKeyHint:   pkgagentapi.APIKeyHint(apiKey),
	}
	if err := store.DB.Create(&agent).Error; err != nil {
		t.Fatalf("create agent: %v", err)
	}

	mgr := NewManager("", 30*time.Second, nil, nil, nil, nil)
	defer mgr.Shutdown()
	mgr.adapterRegistry = agentadapter.NewRegistry()
	mgr.adapterRegistry.Register(gemini.NewAdapter())

	srv, closeSrv := newAgentWSTestServer(mgr)
	defer closeSrv()

	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(srv.URL, "http")+"/?agent_id=91018", nil)
	if err != nil {
		t.Fatalf("dial websocket: %v", err)
	}
	defer conn.Close()

	authPayload, err := json.Marshal(protocol.Packet{
		Cmd: "auth",
		Seq: 1,
		Payload: mustMarshalRawJSON(t, map[string]any{
			"agent_id":         "91018",
			"api_key":          apiKey,
			"client":           "grix-gemini",
			"client_type":      model.AgentClientTypeGemini,
			"contract_version": 1,
			"client_version":   "0.1.0",
			"host_type":        gemini.Family,
			"host_version":     "0.1.0",
			"protocol_version": agentAPIProtocolVersion,
			"adapter_hint":     gemini.AdapterID,
			"capabilities":     []string{"stream_chunk", "local_action_v1"},
			"local_actions":    []string{"exec_approve", "exec_reject"},
		}),
	})
	if err != nil {
		t.Fatalf("marshal auth packet: %v", err)
	}
	if err := conn.WriteMessage(websocket.TextMessage, authPayload); err != nil {
		t.Fatalf("write auth packet: %v", err)
	}

	_, raw, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("read auth ack: %v", err)
	}

	var ack protocol.Packet
	if err := json.Unmarshal(raw, &ack); err != nil {
		t.Fatalf("unmarshal auth ack: %v", err)
	}
	if ack.Cmd != "auth_ack" {
		t.Fatalf("expected auth_ack, got %s", ack.Cmd)
	}

	var payload AuthAckPayload
	if err := json.Unmarshal(ack.Payload, &payload); err != nil {
		t.Fatalf("unmarshal auth ack payload: %v", err)
	}
	if payload.Code != 0 {
		t.Fatalf("expected auth success, got code=%d msg=%s", payload.Code, payload.Msg)
	}
	if payload.AdapterID != gemini.AdapterID {
		t.Fatalf("adapter_id=%q want=%q", payload.AdapterID, gemini.AdapterID)
	}
	if got, want := payload.SupportedCapabilities, []string{"stream_chunk", "local_action_v1"}; !equalStringSlices(got, want) {
		t.Fatalf("supported_capabilities=%v want=%v", got, want)
	}
	if got := payload.DegradedCapabilities; len(got) != 0 {
		t.Fatalf("degraded_capabilities=%v want=[]", got)
	}
	if got, want := payload.UnsupportedCapabilities, []string{}; !equalStringSlices(got, want) {
		t.Fatalf("unsupported_capabilities=%v want=%v", got, want)
	}

	var stored model.Agent
	if err := store.DB.First(&stored, agentID).Error; err != nil {
		t.Fatalf("query agent: %v", err)
	}
	if stored.AgentClientType != model.AgentClientTypeGemini {
		t.Fatalf("expected agent_client_type=%q, got %q", model.AgentClientTypeGemini, stored.AgentClientType)
	}
}

func TestServeWS_AuthAckIncludesQwenAdapterContract(t *testing.T) {
	testDB := testutil.NewTestDB()
	defer testDB.Close()

	originalDB := store.DB
	store.DB = testDB.DB
	t.Cleanup(func() {
		store.DB = originalDB
	})

	const (
		agentID = int64(91028)
		ownerID = int64(82039)
		apiKey  = "ak_test_agent_ws_qwen"
	)

	agent := model.Agent{
		ID:           agentID,
		AgentName:    "qwen-agent",
		OwnerID:      ownerID,
		ProviderType: model.AgentProviderAPI,
		Status:       model.AgentStatusActive,
		APIKeyHash:   pkgagentapi.HashAPIKey(apiKey),
		APIKeyHint:   pkgagentapi.APIKeyHint(apiKey),
	}
	if err := store.DB.Create(&agent).Error; err != nil {
		t.Fatalf("create agent: %v", err)
	}

	mgr := NewManager("", 30*time.Second, nil, nil, nil, nil)
	defer mgr.Shutdown()
	mgr.adapterRegistry = agentadapter.NewRegistry()
	mgr.adapterRegistry.Register(qwen.NewAdapter())

	srv, closeSrv := newAgentWSTestServer(mgr)
	defer closeSrv()

	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(srv.URL, "http")+"/?agent_id=91028", nil)
	if err != nil {
		t.Fatalf("dial websocket: %v", err)
	}
	defer conn.Close()

	authPayload, err := json.Marshal(protocol.Packet{
		Cmd: "auth",
		Seq: 1,
		Payload: mustMarshalRawJSON(t, map[string]any{
			"agent_id":         "91028",
			"api_key":          apiKey,
			"client":           "grix-qwen",
			"client_type":      model.AgentClientTypeQwen,
			"contract_version": 1,
			"client_version":   "0.1.0",
			"host_type":        qwen.Family,
			"host_version":     "0.1.0",
			"protocol_version": agentAPIProtocolVersion,
			"adapter_hint":     qwen.AdapterID,
			"capabilities":     []string{},
		}),
	})
	if err != nil {
		t.Fatalf("marshal auth packet: %v", err)
	}
	if err := conn.WriteMessage(websocket.TextMessage, authPayload); err != nil {
		t.Fatalf("write auth packet: %v", err)
	}

	_, raw, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("read auth ack: %v", err)
	}

	var ack protocol.Packet
	if err := json.Unmarshal(raw, &ack); err != nil {
		t.Fatalf("unmarshal auth ack: %v", err)
	}
	if ack.Cmd != "auth_ack" {
		t.Fatalf("expected auth_ack, got %s", ack.Cmd)
	}

	var payload AuthAckPayload
	if err := json.Unmarshal(ack.Payload, &payload); err != nil {
		t.Fatalf("unmarshal auth ack payload: %v", err)
	}
	if payload.Code != 0 {
		t.Fatalf("expected auth success, got code=%d msg=%s", payload.Code, payload.Msg)
	}
	if payload.AdapterID != qwen.AdapterID {
		t.Fatalf("adapter_id=%q want=%q", payload.AdapterID, qwen.AdapterID)
	}
	if got, want := payload.SupportedCapabilities, []string{}; !equalStringSlices(got, want) {
		t.Fatalf("supported_capabilities=%v want=%v", got, want)
	}
	if got, want := payload.DegradedCapabilities, []string{}; !equalStringSlices(got, want) {
		t.Fatalf("degraded_capabilities=%v want=%v", got, want)
	}
	if got, want := payload.UnsupportedCapabilities, []string{}; !equalStringSlices(got, want) {
		t.Fatalf("unsupported_capabilities=%v want=%v", got, want)
	}

	var stored model.Agent
	if err := store.DB.First(&stored, agentID).Error; err != nil {
		t.Fatalf("query agent: %v", err)
	}
	if stored.AgentClientType != model.AgentClientTypeQwen {
		t.Fatalf("expected agent_client_type=%q, got %q", model.AgentClientTypeQwen, stored.AgentClientType)
	}
}

// 通用 ACP 接入：连接器上报 client_type=acp + adapter_hint=acp/base 时
// 鉴权必须通过（而不是 10003 invalid client_type），并回 adapter_id=acp/base。
func TestServeWS_AuthAckIncludesACPAdapterContract(t *testing.T) {
	testDB := testutil.NewTestDB()
	defer testDB.Close()

	originalDB := store.DB
	store.DB = testDB.DB
	t.Cleanup(func() {
		store.DB = originalDB
	})

	const (
		agentID = int64(91031)
		ownerID = int64(82041)
		apiKey  = "ak_test_agent_ws_acp"
	)

	agent := model.Agent{
		ID:           agentID,
		AgentName:    "acp-agent",
		OwnerID:      ownerID,
		ProviderType: model.AgentProviderAPI,
		Status:       model.AgentStatusActive,
		APIKeyHash:   pkgagentapi.HashAPIKey(apiKey),
		APIKeyHint:   pkgagentapi.APIKeyHint(apiKey),
	}
	if err := store.DB.Create(&agent).Error; err != nil {
		t.Fatalf("create agent: %v", err)
	}

	mgr := NewManager("", 30*time.Second, nil, nil, nil, nil)
	defer mgr.Shutdown()
	mgr.adapterRegistry = agentadapter.NewRegistry()
	mgr.adapterRegistry.Register(acpadapter.NewAdapter())

	srv, closeSrv := newAgentWSTestServer(mgr)
	defer closeSrv()

	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(srv.URL, "http")+"/?agent_id=91031", nil)
	if err != nil {
		t.Fatalf("dial websocket: %v", err)
	}
	defer conn.Close()

	authPayload, err := json.Marshal(protocol.Packet{
		Cmd: "auth",
		Seq: 1,
		Payload: mustMarshalRawJSON(t, map[string]any{
			"agent_id":         "91031",
			"api_key":          apiKey,
			"client":           "grix-connector",
			"client_type":      model.AgentClientTypeACP,
			"contract_version": 1,
			"client_version":   "0.1.0",
			"protocol_version": agentAPIProtocolVersion,
			"adapter_hint":     acpadapter.AdapterID,
			"capabilities":     []string{"stream_chunk", "local_action_v1"},
		}),
	})
	if err != nil {
		t.Fatalf("marshal auth packet: %v", err)
	}
	if err := conn.WriteMessage(websocket.TextMessage, authPayload); err != nil {
		t.Fatalf("write auth packet: %v", err)
	}

	_, raw, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("read auth ack: %v", err)
	}

	var ack protocol.Packet
	if err := json.Unmarshal(raw, &ack); err != nil {
		t.Fatalf("unmarshal auth ack: %v", err)
	}
	if ack.Cmd != "auth_ack" {
		t.Fatalf("expected auth_ack, got %s", ack.Cmd)
	}

	var payload AuthAckPayload
	if err := json.Unmarshal(ack.Payload, &payload); err != nil {
		t.Fatalf("unmarshal auth ack payload: %v", err)
	}
	if payload.Code != 0 {
		t.Fatalf("expected auth success, got code=%d msg=%s", payload.Code, payload.Msg)
	}
	if payload.AdapterID != acpadapter.AdapterID {
		t.Fatalf("adapter_id=%q want=%q", payload.AdapterID, acpadapter.AdapterID)
	}
	if got, want := payload.SupportedCapabilities, []string{"stream_chunk", "local_action_v1"}; !equalStringSlices(got, want) {
		t.Fatalf("supported_capabilities=%v want=%v", got, want)
	}
	if got, want := payload.DegradedCapabilities, []string{}; !equalStringSlices(got, want) {
		t.Fatalf("degraded_capabilities=%v want=%v", got, want)
	}

	var stored model.Agent
	if err := store.DB.First(&stored, agentID).Error; err != nil {
		t.Fatalf("query agent: %v", err)
	}
	if stored.AgentClientType != model.AgentClientTypeACP {
		t.Fatalf("expected agent_client_type=%q, got %q", model.AgentClientTypeACP, stored.AgentClientType)
	}
}

func TestServeWS_GeminiRoundTripHandlesAckActivityStreamAndResult(t *testing.T) {
	testDB := testutil.NewTestDB()
	defer testDB.Close()

	originalDB := store.DB
	store.DB = testDB.DB
	t.Cleanup(func() {
		store.DB = originalDB
	})

	const (
		agentID     = int64(91019)
		ownerID     = int64(82030)
		apiKey      = "ak_test_agent_ws_gemini_roundtrip"
		streamMsgID = int64(33001)
	)

	agent := model.Agent{
		ID:           agentID,
		AgentName:    "gemini-roundtrip-agent",
		OwnerID:      ownerID,
		ProviderType: model.AgentProviderAPI,
		Status:       model.AgentStatusActive,
		APIKeyHash:   pkgagentapi.HashAPIKey(apiKey),
		APIKeyHint:   pkgagentapi.APIKeyHint(apiKey),
	}
	if err := store.DB.Create(&agent).Error; err != nil {
		t.Fatalf("create agent: %v", err)
	}

	statusCh := make(chan protocol.AgentDeliveryStatusPayload, 8)
	outputCh := make(chan protocol.AgentOutputStatusPayload, 8)
	activityCh := make(chan protocol.SessionActivitySetPayload, 4)
	streamCh := make(chan AgentStreamChunkPayload, 4)

	var mgr *Manager
	streamHandler := func(_ context.Context, gotAgentID, gotOwnerID int64, payload AgentStreamChunkPayload) error {
		if gotAgentID != agentID {
			t.Fatalf("stream handler agent_id=%d want=%d", gotAgentID, agentID)
		}
		if gotOwnerID != ownerID {
			t.Fatalf("stream handler owner_id=%d want=%d", gotOwnerID, ownerID)
		}
		if payload.ChunkSeq == 1 {
			mgr.MarkRunClientStreamStarted(payload.EventID, streamMsgID)
		}
		streamCh <- payload
		return nil
	}

	mgr = NewManager("", 30*time.Second, nil, streamHandler, nil, nil)
	defer mgr.Shutdown() // 重新赋值后的 Manager 也要关停（defer 的接收者在语句执行时求值）
	mgr.adapterRegistry = agentadapter.NewRegistry()
	mgr.adapterRegistry.Register(gemini.NewAdapter())
	mgr.SetDeliveryStatusHandler(func(payload protocol.AgentDeliveryStatusPayload) {
		statusCh <- payload
	})
	mgr.SetOutputStatusHandler(func(payload protocol.AgentOutputStatusPayload) {
		outputCh <- payload
	})
	mgr.SetSessionActivityHandler(func(_ context.Context, gotAgentID, gotOwnerID int64, payload protocol.SessionActivitySetPayload) error {
		if gotAgentID != agentID {
			t.Fatalf("activity handler agent_id=%d want=%d", gotAgentID, agentID)
		}
		if gotOwnerID != ownerID {
			t.Fatalf("activity handler owner_id=%d want=%d", gotOwnerID, ownerID)
		}
		activityCh <- payload
		return nil
	})

	srv, closeSrv := newAgentWSTestServer(mgr)
	defer closeSrv()

	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(srv.URL, "http")+"/?agent_id=91019", nil)
	if err != nil {
		t.Fatalf("dial websocket: %v", err)
	}
	defer conn.Close()

	mustReadPacket := func() protocol.Packet {
		t.Helper()
		_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
		_, raw, err := conn.ReadMessage()
		if err != nil {
			t.Fatalf("read websocket packet: %v", err)
		}
		var pkt protocol.Packet
		if err := json.Unmarshal(raw, &pkt); err != nil {
			t.Fatalf("unmarshal websocket packet: %v", err)
		}
		return pkt
	}

	mustWritePacket := func(cmd string, seq int64, payload any) {
		t.Helper()
		raw, err := json.Marshal(protocol.Packet{
			Cmd:     cmd,
			Seq:     seq,
			Payload: mustMarshalRawJSON(t, payload),
		})
		if err != nil {
			t.Fatalf("marshal websocket packet: %v", err)
		}
		if err := conn.WriteMessage(websocket.TextMessage, raw); err != nil {
			t.Fatalf("write websocket packet: %v", err)
		}
	}

	readStatus := func() protocol.AgentDeliveryStatusPayload {
		t.Helper()
		select {
		case payload := <-statusCh:
			return payload
		case <-time.After(2 * time.Second):
			t.Fatal("timed out waiting for delivery status")
			return protocol.AgentDeliveryStatusPayload{}
		}
	}

	readOutput := func() protocol.AgentOutputStatusPayload {
		t.Helper()
		select {
		case payload := <-outputCh:
			return payload
		case <-time.After(2 * time.Second):
			t.Fatal("timed out waiting for output status")
			return protocol.AgentOutputStatusPayload{}
		}
	}

	readActivity := func() protocol.SessionActivitySetPayload {
		t.Helper()
		select {
		case payload := <-activityCh:
			return payload
		case <-time.After(2 * time.Second):
			t.Fatal("timed out waiting for session activity")
			return protocol.SessionActivitySetPayload{}
		}
	}

	readStream := func() AgentStreamChunkPayload {
		t.Helper()
		select {
		case payload := <-streamCh:
			return payload
		case <-time.After(2 * time.Second):
			t.Fatal("timed out waiting for stream chunk")
			return AgentStreamChunkPayload{}
		}
	}

	mustWritePacket("auth", 1, map[string]any{
		"agent_id":         "91019",
		"api_key":          apiKey,
		"client":           "grix-gemini",
		"client_type":      model.AgentClientTypeGemini,
		"contract_version": 1,
		"client_version":   "0.1.0",
		"host_type":        gemini.Family,
		"host_version":     "0.1.0",
		"protocol_version": agentAPIProtocolVersion,
		"adapter_hint":     gemini.AdapterID,
		"capabilities":     []string{"stream_chunk"},
	})

	authAck := mustReadPacket()
	if authAck.Cmd != "auth_ack" {
		t.Fatalf("expected auth_ack, got %s", authAck.Cmd)
	}

	var authAckPayload AuthAckPayload
	if err := json.Unmarshal(authAck.Payload, &authAckPayload); err != nil {
		t.Fatalf("unmarshal auth ack payload: %v", err)
	}
	if authAckPayload.Code != 0 {
		t.Fatalf("expected auth success, got code=%d msg=%s", authAckPayload.Code, authAckPayload.Msg)
	}

	delegate := DelegateEventPayload{
		EventID:         "evt-gemini-roundtrip-1",
		EventType:       "user_chat",
		AgentID:         agentID,
		OwnerID:         ownerID,
		SessionID:       "session-gemini-roundtrip-1",
		ThreadID:        "session-gemini-roundtrip-1",
		SessionType:     1,
		MsgID:           18889993001,
		QuotedMessageID: 18889992000,
		SenderID:        ownerID,
		Content:         "pong",
		Extra: mustMarshalRawJSON(t, map[string]any{
			"acp": map[string]any{
				"cwd":      "/workspace/gemini-roundtrip",
				"mode_id":  "plan",
				"model_id": "gemini-2.5-flash",
			},
		}),
		CreatedAt: 1704067204000,
	}
	if ok := mgr.PushDelegateEvent(delegate); !ok {
		t.Fatal("PushDelegateEvent should succeed")
	}

	if status := readStatus(); status.Status != protocol.AgentDeliveryStatusQueued {
		t.Fatalf("queued status=%q want=%q", status.Status, protocol.AgentDeliveryStatusQueued)
	}
	if output := readOutput(); output.State != protocol.AgentOutputStateQueued {
		t.Fatalf("queued output state=%q want=%q", output.State, protocol.AgentOutputStateQueued)
	}

	eventPacket := mustReadPacket()
	if eventPacket.Cmd != "event_msg" {
		t.Fatalf("expected event_msg, got %s", eventPacket.Cmd)
	}

	var delivered struct {
		EventID         string          `json:"event_id"`
		SessionID       string          `json:"session_id"`
		ThreadID        string          `json:"thread_id"`
		MsgID           int64           `json:"msg_id,string"`
		QuotedMessageID int64           `json:"quoted_message_id,string,omitempty"`
		Content         string          `json:"content"`
		Extra           json.RawMessage `json:"extra"`
	}
	if err := json.Unmarshal(eventPacket.Payload, &delivered); err != nil {
		t.Fatalf("unmarshal delivered event: %v", err)
	}
	if delivered.EventID != delegate.EventID {
		t.Fatalf("event_id=%q want=%q", delivered.EventID, delegate.EventID)
	}
	if delivered.MsgID != delegate.MsgID {
		t.Fatalf("msg_id=%d want=%d", delivered.MsgID, delegate.MsgID)
	}
	if delivered.QuotedMessageID != delegate.QuotedMessageID {
		t.Fatalf("quoted_message_id=%d want=%d", delivered.QuotedMessageID, delegate.QuotedMessageID)
	}

	var acp struct {
		Cwd     string `json:"cwd"`
		ModeID  string `json:"mode_id"`
		ModelID string `json:"model_id"`
	}
	var extraWrapper struct {
		ACP *struct {
			Cwd     string `json:"cwd"`
			ModeID  string `json:"mode_id"`
			ModelID string `json:"model_id"`
		} `json:"acp"`
	}
	extraWrapper.ACP = &acp
	if err := json.Unmarshal(delivered.Extra, &extraWrapper); err != nil {
		t.Fatalf("unmarshal extra.acp: %v", err)
	}
	if acp.Cwd != "/workspace/gemini-roundtrip" {
		t.Fatalf("acp.cwd=%q want=/workspace/gemini-roundtrip", acp.Cwd)
	}
	if acp.ModeID != "plan" {
		t.Fatalf("acp.mode_id=%q want=plan", acp.ModeID)
	}
	if acp.ModelID != "gemini-2.5-flash" {
		t.Fatalf("acp.model_id=%q want=gemini-2.5-flash", acp.ModelID)
	}
	mustWritePacket(protocol.CmdEventAck, 2, EventAckPayload{
		EventID:    delivered.EventID,
		SessionID:  delivered.SessionID,
		MsgID:      delivered.MsgID,
		ReceivedAt: 1704067204100,
	})

	if status := readStatus(); status.Status != protocol.AgentDeliveryStatusReceived {
		t.Fatalf("received status=%q want=%q", status.Status, protocol.AgentDeliveryStatusReceived)
	}
	if output := readOutput(); output.State != protocol.AgentOutputStateReceived {
		t.Fatalf("received output state=%q want=%q", output.State, protocol.AgentOutputStateReceived)
	}

	mustWritePacket(protocol.CmdSessionActivitySet, 3, protocol.SessionActivitySetPayload{
		SessionID:  delivered.SessionID,
		Kind:       protocol.SessionActivityKindComposing,
		Active:     true,
		TTLMS:      30_000,
		RefEventID: delivered.EventID,
		RefMsgID:   "18889993001",
	})

	startedActivity := readActivity()
	if !startedActivity.Active {
		t.Fatal("expected active typing state")
	}
	if startedActivity.Kind != protocol.SessionActivityKindComposing {
		t.Fatalf("activity kind=%q want=%q", startedActivity.Kind, protocol.SessionActivityKindComposing)
	}
	if startedActivity.RefEventID != delivered.EventID {
		t.Fatalf("activity ref_event_id=%q want=%q", startedActivity.RefEventID, delivered.EventID)
	}

	mustWritePacket("client_stream_chunk", 4, AgentStreamChunkPayload{
		EventID:         delivered.EventID,
		SessionID:       delivered.SessionID,
		ThreadID:        delivered.ThreadID,
		ClientMsgID:     "gemini_18889993001_0",
		DeltaContent:    "PO",
		ChunkSeq:        1,
		IsFinish:        false,
		QuotedMessageID: delivered.QuotedMessageID,
	})
	mustWritePacket("client_stream_chunk", 5, AgentStreamChunkPayload{
		EventID:         delivered.EventID,
		SessionID:       delivered.SessionID,
		ThreadID:        delivered.ThreadID,
		ClientMsgID:     "gemini_18889993001_0",
		DeltaContent:    "NG",
		ChunkSeq:        2,
		IsFinish:        false,
		QuotedMessageID: delivered.QuotedMessageID,
	})
	mustWritePacket("client_stream_chunk", 6, AgentStreamChunkPayload{
		EventID:         delivered.EventID,
		SessionID:       delivered.SessionID,
		ThreadID:        delivered.ThreadID,
		ClientMsgID:     "gemini_18889993001_0",
		DeltaContent:    "!",
		ChunkSeq:        3,
		IsFinish:        true,
		QuotedMessageID: delivered.QuotedMessageID,
	})

	firstChunk := readStream()
	if firstChunk.DeltaContent != "PO" || firstChunk.ChunkSeq != 1 {
		t.Fatalf("first chunk=%#v", firstChunk)
	}
	secondChunk := readStream()
	if secondChunk.DeltaContent != "NG" || secondChunk.ChunkSeq != 2 {
		t.Fatalf("second chunk=%#v", secondChunk)
	}
	finishChunk := readStream()
	if !finishChunk.IsFinish || finishChunk.ChunkSeq != 3 {
		t.Fatalf("finish chunk=%#v", finishChunk)
	}
	if finishChunk.QuotedMessageID != delivered.QuotedMessageID {
		t.Fatalf("finish chunk quoted_message_id=%d want=%d", finishChunk.QuotedMessageID, delivered.QuotedMessageID)
	}

	if output := readOutput(); output.State != protocol.AgentOutputStateStreaming {
		t.Fatalf("streaming output state=%q want=%q", output.State, protocol.AgentOutputStateStreaming)
	} else if output.StreamMsgID != streamMsgID {
		t.Fatalf("streaming output stream_msg_id=%d want=%d", output.StreamMsgID, streamMsgID)
	}

	sendAck := mustReadPacket()
	if sendAck.Cmd != protocol.CmdSendAck {
		t.Fatalf("expected send_ack, got %s", sendAck.Cmd)
	}
	var sendAckPayload protocol.SendAckPayload
	if err := json.Unmarshal(sendAck.Payload, &sendAckPayload); err != nil {
		t.Fatalf("unmarshal send ack payload: %v", err)
	}
	if sendAckPayload.ClientMsgID != "gemini_18889993001_0" {
		t.Fatalf("send_ack client_msg_id=%q want=%q", sendAckPayload.ClientMsgID, "gemini_18889993001_0")
	}

	mustWritePacket(protocol.CmdEventResult, 7, EventResultPayload{
		EventID:   delivered.EventID,
		Status:    protocol.AgentEventResultResponded,
		UpdatedAt: 1704067204300,
	})
	mustWritePacket(protocol.CmdSessionActivitySet, 8, protocol.SessionActivitySetPayload{
		SessionID:  delivered.SessionID,
		Kind:       protocol.SessionActivityKindComposing,
		Active:     false,
		RefEventID: delivered.EventID,
		RefMsgID:   "18889993001",
	})

	if status := readStatus(); status.Status != protocol.AgentDeliveryStatusResponded {
		t.Fatalf("responded status=%q want=%q", status.Status, protocol.AgentDeliveryStatusResponded)
	}
	if output := readOutput(); output.State != protocol.AgentOutputStateCompleted {
		t.Fatalf("completed output state=%q want=%q", output.State, protocol.AgentOutputStateCompleted)
	}

	stoppedActivity := readActivity()
	if stoppedActivity.Active {
		t.Fatal("expected inactive typing state")
	}
	if stoppedActivity.RefEventID != delivered.EventID {
		t.Fatalf("stop activity ref_event_id=%q want=%q", stoppedActivity.RefEventID, delivered.EventID)
	}

	if snapshot := mgr.LookupActiveRunBySessionOwner(ownerID, delivered.SessionID); snapshot != nil {
		t.Fatalf("expected active run to be cleared, got=%+v", snapshot)
	}
}

func TestServeWS_AcceptsHermesClientTypeAndSelectsHermesAdapter(t *testing.T) {
	testDB := testutil.NewTestDB()
	defer testDB.Close()

	originalDB := store.DB
	store.DB = testDB.DB
	t.Cleanup(func() {
		store.DB = originalDB
	})

	const (
		agentID = int64(91014)
		ownerID = int64(82025)
		apiKey  = "ak_test_agent_ws_hermes"
	)

	agent := model.Agent{
		ID:           agentID,
		AgentName:    "hermes-agent",
		OwnerID:      ownerID,
		ProviderType: model.AgentProviderAPI,
		Status:       model.AgentStatusActive,
		APIKeyHash:   pkgagentapi.HashAPIKey(apiKey),
		APIKeyHint:   pkgagentapi.APIKeyHint(apiKey),
	}
	if err := store.DB.Create(&agent).Error; err != nil {
		t.Fatalf("create agent: %v", err)
	}

	mgr := NewManager("", 30*time.Second, nil, nil, nil, nil)
	defer mgr.Shutdown()
	mgr.adapterRegistry = agentadapter.NewRegistry()
	mgr.adapterRegistry.Register(hermes.NewAdapter())

	srv, closeSrv := newAgentWSTestServer(mgr)
	defer closeSrv()

	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(srv.URL, "http")+"/?agent_id=91014", nil)
	if err != nil {
		t.Fatalf("dial websocket: %v", err)
	}
	defer conn.Close()

	authPayload, err := json.Marshal(protocol.Packet{
		Cmd: "auth",
		Seq: 1,
		Payload: mustMarshalRawJSON(t, map[string]any{
			"agent_id":         "91014",
			"api_key":          apiKey,
			"client":           "grix-hermes",
			"client_type":      model.AgentClientTypeHermes,
			"contract_version": 1,
			"protocol_version": agentAPIProtocolVersion,
			"host_type":        hermes.Family,
			"capabilities":     []string{"stream_chunk", "session_route", "thread_v1", "local_action_v1"},
		}),
	})
	if err != nil {
		t.Fatalf("marshal auth packet: %v", err)
	}
	if err := conn.WriteMessage(websocket.TextMessage, authPayload); err != nil {
		t.Fatalf("write auth packet: %v", err)
	}

	_, raw, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("read auth ack: %v", err)
	}

	var ack protocol.Packet
	if err := json.Unmarshal(raw, &ack); err != nil {
		t.Fatalf("unmarshal auth ack: %v", err)
	}
	if ack.Cmd != "auth_ack" {
		t.Fatalf("expected auth_ack, got %s", ack.Cmd)
	}

	var payload AuthAckPayload
	if err := json.Unmarshal(ack.Payload, &payload); err != nil {
		t.Fatalf("unmarshal auth ack payload: %v", err)
	}
	if payload.Code != 0 {
		t.Fatalf("expected auth success, got code=%d msg=%s", payload.Code, payload.Msg)
	}
	if payload.AdapterID != hermes.AdapterID {
		t.Fatalf("adapter_id=%q want=%q", payload.AdapterID, hermes.AdapterID)
	}
	if got, want := payload.SupportedCapabilities, []string{"stream_chunk", "session_route", "thread_v1", "local_action_v1"}; !equalStringSlices(got, want) {
		t.Fatalf("supported_capabilities=%v want=%v", got, want)
	}
	if got, want := payload.UnsupportedCapabilities, []string{}; !equalStringSlices(got, want) {
		t.Fatalf("unsupported_capabilities=%v want=%v", got, want)
	}

	var stored model.Agent
	if err := store.DB.First(&stored, agentID).Error; err != nil {
		t.Fatalf("query agent: %v", err)
	}
	if stored.AgentClientType != model.AgentClientTypeHermes {
		t.Fatalf("expected agent_client_type=%q, got %q", model.AgentClientTypeHermes, stored.AgentClientType)
	}
}

func TestServeWS_RejectsHermesAuthWithoutProtocolVersion(t *testing.T) {
	testDB := testutil.NewTestDB()
	defer testDB.Close()

	originalDB := store.DB
	store.DB = testDB.DB
	t.Cleanup(func() {
		store.DB = originalDB
	})

	const (
		agentID = int64(91016)
		ownerID = int64(82027)
		apiKey  = "ak_test_agent_ws_hermes_missing_protocol"
	)

	agent := model.Agent{
		ID:           agentID,
		AgentName:    "hermes-auth-agent",
		OwnerID:      ownerID,
		ProviderType: model.AgentProviderAPI,
		Status:       model.AgentStatusActive,
		APIKeyHash:   pkgagentapi.HashAPIKey(apiKey),
		APIKeyHint:   pkgagentapi.APIKeyHint(apiKey),
	}
	if err := store.DB.Create(&agent).Error; err != nil {
		t.Fatalf("create agent: %v", err)
	}

	mgr := NewManager("", 30*time.Second, nil, nil, nil, nil)
	defer mgr.Shutdown()
	srv, closeSrv := newAgentWSTestServer(mgr)
	defer closeSrv()

	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(srv.URL, "http")+"/?agent_id=91016", nil)
	if err != nil {
		t.Fatalf("dial websocket: %v", err)
	}
	defer conn.Close()

	authPayload, err := json.Marshal(protocol.Packet{
		Cmd: "auth",
		Seq: 1,
		Payload: mustMarshalRawJSON(t, map[string]any{
			"agent_id":         "91016",
			"api_key":          apiKey,
			"client_type":      model.AgentClientTypeHermes,
			"host_type":        hermes.Family,
			"contract_version": 1,
			"capabilities":     []string{"local_action_v1"},
		}),
	})
	if err != nil {
		t.Fatalf("marshal auth packet: %v", err)
	}
	if err := conn.WriteMessage(websocket.TextMessage, authPayload); err != nil {
		t.Fatalf("write auth packet: %v", err)
	}

	_, raw, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("read auth ack: %v", err)
	}

	var ack protocol.Packet
	if err := json.Unmarshal(raw, &ack); err != nil {
		t.Fatalf("unmarshal auth ack: %v", err)
	}

	var payload AuthAckPayload
	if err := json.Unmarshal(ack.Payload, &payload); err != nil {
		t.Fatalf("unmarshal auth ack payload: %v", err)
	}
	if payload.Code != 10003 {
		t.Fatalf("expected code=10003, got %d", payload.Code)
	}
	if payload.Msg != "protocol_version must be aibot-agent-api-v1" {
		t.Fatalf("unexpected msg=%q", payload.Msg)
	}
}

func TestServeWS_RejectsHermesAuthWithoutLocalActionCapability(t *testing.T) {
	testDB := testutil.NewTestDB()
	defer testDB.Close()

	originalDB := store.DB
	store.DB = testDB.DB
	t.Cleanup(func() {
		store.DB = originalDB
	})

	const (
		agentID = int64(91017)
		ownerID = int64(82028)
		apiKey  = "ak_test_agent_ws_hermes_missing_cap"
	)

	agent := model.Agent{
		ID:           agentID,
		AgentName:    "hermes-cap-agent",
		OwnerID:      ownerID,
		ProviderType: model.AgentProviderAPI,
		Status:       model.AgentStatusActive,
		APIKeyHash:   pkgagentapi.HashAPIKey(apiKey),
		APIKeyHint:   pkgagentapi.APIKeyHint(apiKey),
	}
	if err := store.DB.Create(&agent).Error; err != nil {
		t.Fatalf("create agent: %v", err)
	}

	mgr := NewManager("", 30*time.Second, nil, nil, nil, nil)
	defer mgr.Shutdown()
	srv, closeSrv := newAgentWSTestServer(mgr)
	defer closeSrv()

	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(srv.URL, "http")+"/?agent_id=91017", nil)
	if err != nil {
		t.Fatalf("dial websocket: %v", err)
	}
	defer conn.Close()

	authPayload, err := json.Marshal(protocol.Packet{
		Cmd: "auth",
		Seq: 1,
		Payload: mustMarshalRawJSON(t, map[string]any{
			"agent_id":         "91017",
			"api_key":          apiKey,
			"client_type":      model.AgentClientTypeHermes,
			"host_type":        hermes.Family,
			"contract_version": 1,
			"protocol_version": agentAPIProtocolVersion,
			"capabilities":     []string{"session_route"},
		}),
	})
	if err != nil {
		t.Fatalf("marshal auth packet: %v", err)
	}
	if err := conn.WriteMessage(websocket.TextMessage, authPayload); err != nil {
		t.Fatalf("write auth packet: %v", err)
	}

	_, raw, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("read auth ack: %v", err)
	}

	var ack protocol.Packet
	if err := json.Unmarshal(raw, &ack); err != nil {
		t.Fatalf("unmarshal auth ack: %v", err)
	}

	var payload AuthAckPayload
	if err := json.Unmarshal(ack.Payload, &payload); err != nil {
		t.Fatalf("unmarshal auth ack payload: %v", err)
	}
	if payload.Code != 10003 {
		t.Fatalf("expected code=10003, got %d", payload.Code)
	}
	if payload.Msg != "local_action_v1 capability required for hermes" {
		t.Fatalf("unexpected msg=%q", payload.Msg)
	}
}

func TestServeWS_SendsAuthAckBeforeQueuedDelegateEvents(t *testing.T) {
	testDB := testutil.NewTestDB()
	defer testDB.Close()

	originalDB := store.DB
	store.DB = testDB.DB
	t.Cleanup(func() {
		store.DB = originalDB
	})

	originalRDB := store.RDB
	mockRedis := testutil.NewMockRedis()
	store.RDB = mockRedis
	t.Cleanup(func() {
		_ = mockRedis.Close()
		store.RDB = originalRDB
	})

	const (
		agentID = int64(91015)
		ownerID = int64(82026)
		apiKey  = "ak_test_agent_ws_auth_ack_first"
	)

	agent := model.Agent{
		ID:           agentID,
		AgentName:    "queued-event-agent",
		OwnerID:      ownerID,
		ProviderType: model.AgentProviderAPI,
		Status:       model.AgentStatusActive,
		APIKeyHash:   pkgagentapi.HashAPIKey(apiKey),
		APIKeyHint:   pkgagentapi.APIKeyHint(apiKey),
	}
	if err := store.DB.Create(&agent).Error; err != nil {
		t.Fatalf("create agent: %v", err)
	}

	mgr := NewManager("", 30*time.Second, nil, nil, nil, nil)
	defer mgr.Shutdown()
	queuedEvent := DelegateEventPayload{
		EventID:     "evt-auth-ack-first-1",
		EventType:   "user_chat",
		AgentID:     agentID,
		OwnerID:     ownerID,
		SessionID:   "session-auth-ack-first-1",
		SessionType: 1,
		MsgID:       22001,
		SenderID:    ownerID,
		Content:     "deliver after auth",
		CreatedAt:   1704067203000,
	}
	if ok := mgr.PushDelegateEvent(queuedEvent); !ok {
		t.Fatal("PushDelegateEvent should queue while the agent is offline")
	}

	srv, closeSrv := newAgentWSTestServer(mgr)
	defer closeSrv()

	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(srv.URL, "http")+"/?agent_id=91015", nil)
	if err != nil {
		t.Fatalf("dial websocket: %v", err)
	}
	defer conn.Close()

	authPayload, err := json.Marshal(protocol.Packet{
		Cmd: "auth",
		Seq: 1,
		Payload: mustMarshalRawJSON(t, map[string]any{
			"agent_id":         "91015",
			"api_key":          apiKey,
			"client":           "grix-hermes",
			"client_type":      model.AgentClientTypeHermes,
			"contract_version": 1,
			"protocol_version": agentAPIProtocolVersion,
			"capabilities":     []string{"local_action_v1"},
		}),
	})
	if err != nil {
		t.Fatalf("marshal auth packet: %v", err)
	}
	if err := conn.WriteMessage(websocket.TextMessage, authPayload); err != nil {
		t.Fatalf("write auth packet: %v", err)
	}

	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))

	_, rawAuthAck, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("read auth ack: %v", err)
	}

	var authAck protocol.Packet
	if err := json.Unmarshal(rawAuthAck, &authAck); err != nil {
		t.Fatalf("unmarshal auth ack packet: %v", err)
	}
	if authAck.Cmd != "auth_ack" {
		t.Fatalf("first packet cmd=%s want=auth_ack", authAck.Cmd)
	}

	var authAckPayload AuthAckPayload
	if err := json.Unmarshal(authAck.Payload, &authAckPayload); err != nil {
		t.Fatalf("unmarshal auth ack payload: %v", err)
	}
	if authAckPayload.Code != 0 {
		t.Fatalf("expected auth success, got code=%d msg=%s", authAckPayload.Code, authAckPayload.Msg)
	}

	_, rawEvent, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("read queued event: %v", err)
	}

	var eventPacket protocol.Packet
	if err := json.Unmarshal(rawEvent, &eventPacket); err != nil {
		t.Fatalf("unmarshal queued event packet: %v", err)
	}
	if eventPacket.Cmd != "event_msg" {
		t.Fatalf("second packet cmd=%s want=event_msg", eventPacket.Cmd)
	}

	var deliveredEvent DelegateEventPayload
	if err := json.Unmarshal(eventPacket.Payload, &deliveredEvent); err != nil {
		t.Fatalf("unmarshal queued event payload: %v", err)
	}
	if deliveredEvent.EventID != queuedEvent.EventID {
		t.Fatalf("event_id=%q want=%q", deliveredEvent.EventID, queuedEvent.EventID)
	}

	eventAck, err := json.Marshal(protocol.Packet{
		Cmd: protocol.CmdEventAck,
		Seq: 2,
		Payload: mustMarshalRawJSON(t, EventAckPayload{
			EventID:    deliveredEvent.EventID,
			SessionID:  deliveredEvent.SessionID,
			MsgID:      deliveredEvent.MsgID,
			ReceivedAt: time.Now().UnixMilli(),
		}),
	})
	if err != nil {
		t.Fatalf("marshal event ack: %v", err)
	}
	if err := conn.WriteMessage(websocket.TextMessage, eventAck); err != nil {
		t.Fatalf("write event ack: %v", err)
	}

	eventResult, err := json.Marshal(protocol.Packet{
		Cmd: protocol.CmdEventResult,
		Seq: 3,
		Payload: mustMarshalRawJSON(t, EventResultPayload{
			EventID:   deliveredEvent.EventID,
			Status:    protocol.AgentEventResultResponded,
			UpdatedAt: time.Now().UnixMilli(),
		}),
	})
	if err != nil {
		t.Fatalf("marshal event result: %v", err)
	}
	if err := conn.WriteMessage(websocket.TextMessage, eventResult); err != nil {
		t.Fatalf("write event result: %v", err)
	}
}

func TestServeWS_HermesRejectsUnsupportedInboundCommand(t *testing.T) {
	testDB := testutil.NewTestDB()
	defer testDB.Close()

	originalDB := store.DB
	store.DB = testDB.DB
	t.Cleanup(func() {
		store.DB = originalDB
	})

	const (
		agentID = int64(91018)
		ownerID = int64(82029)
		apiKey  = "ak_test_agent_ws_hermes_unsupported_cmd"
	)

	agent := model.Agent{
		ID:           agentID,
		AgentName:    "hermes-unsupported-cmd-agent",
		OwnerID:      ownerID,
		ProviderType: model.AgentProviderAPI,
		Status:       model.AgentStatusActive,
		APIKeyHash:   pkgagentapi.HashAPIKey(apiKey),
		APIKeyHint:   pkgagentapi.APIKeyHint(apiKey),
	}
	if err := store.DB.Create(&agent).Error; err != nil {
		t.Fatalf("create agent: %v", err)
	}

	mgr := NewManager("", 30*time.Second, nil, nil, nil, nil)
	defer mgr.Shutdown()
	mgr.adapterRegistry = agentadapter.NewRegistry()
	mgr.adapterRegistry.Register(hermes.NewAdapter())

	srv, closeSrv := newAgentWSTestServer(mgr)
	defer closeSrv()

	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(srv.URL, "http")+"/?agent_id=91018", nil)
	if err != nil {
		t.Fatalf("dial websocket: %v", err)
	}
	defer conn.Close()

	authPayload, err := json.Marshal(protocol.Packet{
		Cmd: "auth",
		Seq: 1,
		Payload: mustMarshalRawJSON(t, map[string]any{
			"agent_id":         "91018",
			"api_key":          apiKey,
			"client_type":      model.AgentClientTypeHermes,
			"host_type":        hermes.Family,
			"contract_version": 1,
			"protocol_version": agentAPIProtocolVersion,
			"capabilities":     []string{"local_action_v1"},
		}),
	})
	if err != nil {
		t.Fatalf("marshal auth packet: %v", err)
	}
	if err := conn.WriteMessage(websocket.TextMessage, authPayload); err != nil {
		t.Fatalf("write auth packet: %v", err)
	}
	if _, _, err := conn.ReadMessage(); err != nil {
		t.Fatalf("read auth ack: %v", err)
	}

	unsupportedPayload, err := json.Marshal(protocol.Packet{
		Cmd: "delete_msg",
		Seq: 2,
		Payload: mustMarshalRawJSON(t, map[string]any{
			"session_id": "sess-1",
			"msg_id":     "msg-1",
		}),
	})
	if err != nil {
		t.Fatalf("marshal unsupported packet: %v", err)
	}
	if err := conn.WriteMessage(websocket.TextMessage, unsupportedPayload); err != nil {
		t.Fatalf("write unsupported packet: %v", err)
	}

	_, raw, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("read unsupported response: %v", err)
	}

	var pkt protocol.Packet
	if err := json.Unmarshal(raw, &pkt); err != nil {
		t.Fatalf("unmarshal unsupported response: %v", err)
	}
	if pkt.Cmd != "error" {
		t.Fatalf("expected error cmd, got %s", pkt.Cmd)
	}

	var payload SendNackPayload
	if err := json.Unmarshal(pkt.Payload, &payload); err != nil {
		t.Fatalf("unmarshal error payload: %v", err)
	}
	if payload.Code != 4004 {
		t.Fatalf("error code=%d want=4004", payload.Code)
	}
	if payload.Msg != "unsupported cmd for hermes" {
		t.Fatalf("error msg=%q", payload.Msg)
	}
}

func equalStringSlices(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range want {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

func mustMarshalRawJSON(t *testing.T, value any) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal raw json: %v", err)
	}
	return raw
}
