package acp

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/askie/grix/backend/internal/agentadapter"
)

func newAdapter() *Adapter { return NewAdapter() }

func TestFamilyAndID(t *testing.T) {
	a := newAdapter()
	if a.Family() != "acp" {
		t.Fatalf("Family() = %q, want %q", a.Family(), "acp")
	}
	if a.AdapterID() != "acp/base" {
		t.Fatalf("AdapterID() = %q, want %q", a.AdapterID(), "acp/base")
	}
}

func TestSupports(t *testing.T) {
	a := newAdapter()

	tests := []struct {
		name  string
		meta  agentadapter.AgentClientMeta
		match bool
	}{
		{"client_type match", agentadapter.AgentClientMeta{ClientType: "acp"}, true},
		{"host_type match", agentadapter.AgentClientMeta{HostType: "acp"}, true},
		{"host_type priority", agentadapter.AgentClientMeta{ClientType: "hermes", HostType: "acp"}, true},
		{"no match", agentadapter.AgentClientMeta{ClientType: "hermes"}, false},
		{"empty", agentadapter.AgentClientMeta{}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := a.Supports(tt.meta); got != tt.match {
				t.Errorf("Supports(%+v) = %v, want %v", tt.meta, got, tt.match)
			}
		})
	}
}

func TestRequiredCapabilities(t *testing.T) {
	a := newAdapter()
	rc := a.RequiredCapabilities()
	if len(rc) < 2 {
		t.Fatalf("RequiredCapabilities() = %v, want at least 2", rc)
	}
	hasStream := false
	hasLocalAction := false
	for _, c := range rc {
		if c == "stream_chunk" {
			hasStream = true
		}
		if c == "local_action_v1" {
			hasLocalAction = true
		}
	}
	if !hasStream || !hasLocalAction {
		t.Errorf("RequiredCapabilities() must include stream_chunk and local_action_v1, got %v", rc)
	}
}

func TestNormalizeInbound_PlainText(t *testing.T) {
	a := newAdapter()
	raw, _ := json.Marshal(map[string]any{
		"session_id": "sess_123",
		"content":    "Hello world",
	})

	evt, err := a.NormalizeInbound(context.Background(), raw)
	if err != nil {
		t.Fatal(err)
	}
	if evt.SessionID != "sess_123" {
		t.Errorf("SessionID = %q, want %q", evt.SessionID, "sess_123")
	}
	if evt.Content != "Hello world" {
		t.Errorf("Content = %q, want %q", evt.Content, "Hello world")
	}
	if evt.Drop {
		t.Error("Drop should be false for plain text")
	}
}

func TestNormalizeInbound_WithThread(t *testing.T) {
	a := newAdapter()
	raw, _ := json.Marshal(map[string]any{
		"session_id": "sess_123",
		"thread_id":  "thread_456",
		"content":    "reply",
	})

	evt, err := a.NormalizeInbound(context.Background(), raw)
	if err != nil {
		t.Fatal(err)
	}
	if evt.ThreadID != "thread_456" {
		t.Errorf("ThreadID = %q, want %q", evt.ThreadID, "thread_456")
	}
}

func TestNormalizeOutbound(t *testing.T) {
	a := newAdapter()
	event := agentadapter.DomainOutboundEvent{
		EventID:   "evt_1",
		SessionID: "sess_1",
		Content:   "test message",
	}

	pkt, err := a.NormalizeOutbound(context.Background(), event)
	if err != nil {
		t.Fatal(err)
	}
	if pkt.Cmd != "event_msg" {
		t.Errorf("Cmd = %q, want %q", pkt.Cmd, "event_msg")
	}

	var payload map[string]any
	if err := json.Unmarshal(pkt.Payload, &payload); err != nil {
		t.Fatal(err)
	}
	if payload["event_id"] != "evt_1" {
		t.Errorf("payload event_id = %v, want evt_1", payload["event_id"])
	}
	if payload["content"] != "test message" {
		t.Errorf("payload content = %v, want test message", payload["content"])
	}
}

func TestNormalizeApproval(t *testing.T) {
	a := newAdapter()
	event := agentadapter.DomainApprovalEvent{
		ActionID:   "action_1",
		ActionType: "exec_approve",
		Params:     json.RawMessage(`{"command":"ls"}`),
		TimeoutMs:  20000,
	}

	pkt, err := a.NormalizeApproval(context.Background(), event)
	if err != nil {
		t.Fatal(err)
	}
	if pkt.Cmd != "local_action" {
		t.Errorf("Cmd = %q, want %q", pkt.Cmd, "local_action")
	}

	var payload map[string]any
	if err := json.Unmarshal(pkt.Payload, &payload); err != nil {
		t.Fatal(err)
	}
	if payload["action_id"] != "action_1" {
		t.Errorf("action_id = %v, want action_1", payload["action_id"])
	}
	if payload["action_type"] != "exec_approve" {
		t.Errorf("action_type = %v, want exec_approve", payload["action_type"])
	}
}

func TestNormalizeStatus(t *testing.T) {
	a := newAdapter()
	event := agentadapter.DomainStatusEvent{
		EventType: "state_sync",
		SessionID: "sess_1",
		Status:    "ready",
	}

	pkt, err := a.NormalizeStatus(context.Background(), event)
	if err != nil {
		t.Fatal(err)
	}
	if pkt.Cmd != "agent_state_sync" {
		t.Errorf("Cmd = %q, want %q", pkt.Cmd, "agent_state_sync")
	}
}

func TestNormalizeRevoke(t *testing.T) {
	a := newAdapter()
	event := agentadapter.DomainRevokeEvent{
		EventID:   "evt_1",
		SessionID: "sess_1",
		MsgID:     42,
		IsRevoked: true,
	}

	pkt, err := a.NormalizeRevoke(context.Background(), event)
	if err != nil {
		t.Fatal(err)
	}
	if pkt.Cmd != "event_revoke" {
		t.Errorf("Cmd = %q, want %q", pkt.Cmd, "event_revoke")
	}
}
