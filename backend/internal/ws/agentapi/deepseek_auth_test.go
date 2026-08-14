package agentapi

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/askie/grix/backend/internal/agentadapter"
	"github.com/askie/grix/backend/internal/agentadapter/deepseek"
	"github.com/askie/grix/backend/internal/model"
	pkgagentapi "github.com/askie/grix/backend/internal/pkg/agentapi"
	"github.com/askie/grix/backend/internal/pkg/testutil"
	"github.com/askie/grix/backend/internal/store"
	"github.com/askie/grix/backend/internal/ws/protocol"
	"github.com/gorilla/websocket"
	"gorm.io/gorm"
)

func TestServeWSSelectsDeepSeekJSONRPCAdapter(t *testing.T) {
	testDB := testutil.NewTestDB()
	defer testDB.Close()
	originalDB := store.DB
	store.DB = testDB.DB
	t.Cleanup(func() { store.DB = originalDB })

	const (
		agentID = int64(91061)
		ownerID = int64(82061)
		apiKey  = "ak_test_deepseek_jsonrpc_adapter"
	)
	if err := store.DB.Create(&model.Agent{
		ID: agentID, AgentName: "deepseek-agent", OwnerID: ownerID,
		ProviderType: model.AgentProviderAPI, AgentClientType: model.AgentClientTypeDeepSeek,
		SystemPrompt: "deepseek auth business prompt", Status: model.AgentStatusActive,
		APIKeyHash: pkgagentapi.HashAPIKey(apiKey), APIKeyHint: pkgagentapi.APIKeyHint(apiKey),
	}).Error; err != nil {
		t.Fatalf("create agent: %v", err)
	}

	mgr := NewManager("", 30*time.Second, nil, nil, nil, nil)
	mgr.adapterRegistry = agentadapter.NewRegistry()
	mgr.adapterRegistry.Register(deepseek.NewAdapter())
	defer mgr.Shutdown()
	srv, closeSrv := newAgentWSTestServer(mgr)
	defer closeSrv()

	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(srv.URL, "http")+"/?agent_id=91061", nil)
	if err != nil {
		t.Fatalf("dial websocket: %v", err)
	}
	defer conn.Close()
	auth, _ := json.Marshal(protocol.Packet{Cmd: "auth", Seq: 1, Payload: mustMarshalRawJSON(t, map[string]any{
		"agent_id": "91061", "api_key": apiKey, "client": "grix-connector",
		"client_type": model.AgentClientTypeDeepSeek, "host_type": deepseek.Family,
		"host_version": "0.1.0-rc.5", "adapter_hint": deepseek.AdapterID,
		"contract_version": 1, "capabilities": []string{"stream_chunk", "local_action_v1"},
	})})
	if err := conn.WriteMessage(websocket.TextMessage, auth); err != nil {
		t.Fatalf("write auth: %v", err)
	}
	_, raw, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("read auth ack: %v", err)
	}
	var packet protocol.Packet
	if err := json.Unmarshal(raw, &packet); err != nil {
		t.Fatalf("unmarshal packet: %v", err)
	}
	var ack AuthAckPayload
	if err := json.Unmarshal(packet.Payload, &ack); err != nil {
		t.Fatalf("unmarshal auth ack: %v", err)
	}
	if ack.Code != 0 || ack.AdapterID != deepseek.AdapterID || ack.SystemPrompt != "deepseek auth business prompt" {
		t.Fatalf("ack code=%d adapter=%q prompt=%q", ack.Code, ack.AdapterID, ack.SystemPrompt)
	}
}

func TestServeWSDeepSeekAuthRejectsWhenProfileLoadFails(t *testing.T) {
	testDB := testutil.NewTestDB()
	defer testDB.Close()
	originalDB := store.DB
	store.DB = testDB.DB
	t.Cleanup(func() { store.DB = originalDB })

	const (
		agentID = int64(91062)
		ownerID = int64(82062)
		apiKey  = "ak_test_deepseek_profile_failure"
	)
	if err := store.DB.Create(&model.Agent{
		ID: agentID, AgentName: "deepseek-agent", OwnerID: ownerID,
		ProviderType: model.AgentProviderAPI, AgentClientType: model.AgentClientTypeDeepSeek,
		SystemPrompt: "must not be replaced by an unknown value", Status: model.AgentStatusActive,
		APIKeyHash: pkgagentapi.HashAPIKey(apiKey), APIKeyHint: pkgagentapi.APIKeyHint(apiKey),
	}).Error; err != nil {
		t.Fatalf("create agent: %v", err)
	}

	const callbackName = "test:fail_deepseek_auth_profile_load"
	if err := store.DB.Callback().Query().Before("gorm:query").Register(callbackName, func(tx *gorm.DB) {
		for _, field := range tx.Statement.Selects {
			if field == "system_prompt" {
				tx.AddError(errors.New("injected profile query failure"))
				return
			}
		}
	}); err != nil {
		t.Fatalf("register query callback: %v", err)
	}
	t.Cleanup(func() { _ = store.DB.Callback().Query().Remove(callbackName) })

	mgr := NewManager("", 30*time.Second, nil, nil, nil, nil)
	mgr.adapterRegistry = agentadapter.NewRegistry()
	mgr.adapterRegistry.Register(deepseek.NewAdapter())
	defer mgr.Shutdown()
	srv, closeSrv := newAgentWSTestServer(mgr)
	defer closeSrv()

	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(srv.URL, "http")+"/?agent_id=91062", nil)
	if err != nil {
		t.Fatalf("dial websocket: %v", err)
	}
	defer conn.Close()
	auth, _ := json.Marshal(protocol.Packet{Cmd: "auth", Seq: 1, Payload: mustMarshalRawJSON(t, map[string]any{
		"agent_id": "91062", "api_key": apiKey, "client": "grix-connector",
		"client_type": model.AgentClientTypeDeepSeek, "host_type": deepseek.Family,
		"contract_version": 1,
	})})
	if err := conn.WriteMessage(websocket.TextMessage, auth); err != nil {
		t.Fatalf("write auth: %v", err)
	}
	_, raw, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("read auth ack: %v", err)
	}
	var packet protocol.Packet
	if err := json.Unmarshal(raw, &packet); err != nil {
		t.Fatalf("unmarshal packet: %v", err)
	}
	var ack AuthAckPayload
	if err := json.Unmarshal(packet.Payload, &ack); err != nil {
		t.Fatalf("unmarshal auth ack: %v", err)
	}
	if ack.Code == 0 {
		t.Fatalf("profile load failure must not produce successful auth_ack: %+v", ack)
	}
	if mgr.CountConns() != 0 {
		t.Fatal("failed profile load must not attach the agent connection")
	}
}
