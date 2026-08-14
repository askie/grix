package agentapi

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/testutil"
	"github.com/askie/grix/backend/internal/store"
	"github.com/askie/grix/backend/internal/ws/protocol"
)

func TestPushProfileToPrimaryIncludesSystemPromptAndExplicitClear(t *testing.T) {
	testDB := testutil.NewTestDB()
	defer testDB.Close()
	originalDB := store.DB
	store.DB = testDB.DB
	t.Cleanup(func() { store.DB = originalDB })

	const agentID = int64(98101)
	agent := model.Agent{
		ID: agentID, AgentName: "DeepSeek", Introduction: "intro",
		SystemPrompt: "profile business prompt", OwnerID: 78101,
		ProviderType: model.AgentProviderAPI, AgentClientType: model.AgentClientTypeDeepSeek,
		Status: model.AgentStatusActive,
	}
	if err := store.DB.Create(&agent).Error; err != nil {
		t.Fatalf("create agent: %v", err)
	}

	mgr := NewManager("", 30*time.Second, nil, nil, nil, nil)
	conn := &agentConn{
		agentID: agentID, ownerID: agent.OwnerID, isPrimary: true,
		send: make(chan []byte, 2), done: make(chan struct{}),
	}
	mgr.conns[agentID] = map[int64]*agentConn{agent.OwnerID: conn}

	mgr.pushProfileToPrimary(agentID)
	payload := readProfilePush(t, conn.send)
	if payload.SystemPrompt != "profile business prompt" || payload.AgentName != "DeepSeek" {
		t.Fatalf("payload=%+v", payload)
	}

	if err := store.DB.Model(&model.Agent{}).Where("id = ?", agentID).Update("system_prompt", "").Error; err != nil {
		t.Fatalf("clear prompt: %v", err)
	}
	mgr.pushProfileToPrimary(agentID)
	raw := <-conn.send
	if !jsonContainsKey(t, raw, "system_prompt") {
		t.Fatal("agent_profile_push must include system_prompt when it is empty")
	}
	var packet protocol.Packet
	if err := json.Unmarshal(raw, &packet); err != nil {
		t.Fatalf("unmarshal packet: %v", err)
	}
	var cleared protocol.AgentProfilePushPayload
	if err := json.Unmarshal(packet.Payload, &cleared); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if cleared.SystemPrompt != "" {
		t.Fatalf("cleared system_prompt=%q", cleared.SystemPrompt)
	}
}

func readProfilePush(t *testing.T, ch <-chan []byte) protocol.AgentProfilePushPayload {
	t.Helper()
	raw := <-ch
	var packet protocol.Packet
	if err := json.Unmarshal(raw, &packet); err != nil {
		t.Fatalf("unmarshal packet: %v", err)
	}
	if packet.Cmd != protocol.CmdAgentProfilePush {
		t.Fatalf("cmd=%q", packet.Cmd)
	}
	var payload protocol.AgentProfilePushPayload
	if err := json.Unmarshal(packet.Payload, &payload); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	return payload
}

func jsonContainsKey(t *testing.T, raw []byte, key string) bool {
	t.Helper()
	var packet struct {
		Payload map[string]any `json:"payload"`
	}
	if err := json.Unmarshal(raw, &packet); err != nil {
		t.Fatalf("unmarshal envelope: %v", err)
	}
	_, ok := packet.Payload[key]
	return ok
}

func TestAuthAckSystemPromptDoesNotOmitExplicitClear(t *testing.T) {
	raw, err := json.Marshal(AuthAckPayload{Code: 0, Msg: "ok"})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if value, ok := payload["system_prompt"]; !ok || value != "" {
		t.Fatalf("system_prompt=%#v present=%v", value, ok)
	}
}
