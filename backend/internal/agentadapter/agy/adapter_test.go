package agy

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/askie/grix/backend/internal/agentadapter"
)

func TestAdapter_SupportsAgyFamily(t *testing.T) {
	a := NewAdapter()

	if !a.Supports(agentadapter.AgentClientMeta{ClientType: Family}) {
		t.Fatal("expected agy adapter to support client_type family")
	}
	if !a.Supports(agentadapter.AgentClientMeta{HostType: Family}) {
		t.Fatal("expected agy adapter to support host_type family")
	}
	if a.Supports(agentadapter.AgentClientMeta{ClientType: "claude"}) {
		t.Fatal("expected agy adapter to reject non-agy family")
	}
}

func TestAdapter_OptionalCapabilities(t *testing.T) {
	a := NewAdapter()
	got := a.OptionalCapabilities()
	if len(got) != 2 {
		t.Fatalf("optional capabilities len=%d want=2", len(got))
	}
	if got[0] != "stream_chunk" || got[1] != "local_action_v1" {
		t.Fatalf("optional capabilities=%v want=[stream_chunk local_action_v1]", got)
	}
}

func TestAdapter_NormalizeInbound_PassesThroughPlainContent(t *testing.T) {
	a := NewAdapter()
	event, err := a.NormalizeInbound(context.Background(), []byte(`{
		"session_id":"agy-session-1",
		"content":"Hello from Antigravity",
		"extra":null
	}`))
	if err != nil {
		t.Fatalf("NormalizeInbound error: %v", err)
	}
	if event.Content != "Hello from Antigravity" {
		t.Fatalf("content=%q want=%q", event.Content, "Hello from Antigravity")
	}
}

func TestAdapter_NormalizeOutbound_UsesEventMsg(t *testing.T) {
	a := NewAdapter()
	packet, err := a.NormalizeOutbound(context.Background(), agentadapter.DomainOutboundEvent{
		EventID:   "evt-agy-1",
		EventType: "group_message",
		AgentID:   4501,
		OwnerID:   5501,
		SessionID: "chat-agy-1",
		MsgID:     6501,
		SenderID:  7501,
		Content:   "hello agy",
	})
	if err != nil {
		t.Fatalf("NormalizeOutbound error: %v", err)
	}
	if packet == nil {
		t.Fatal("NormalizeOutbound returned nil packet")
	}
	if packet.Cmd != "event_msg" {
		t.Fatalf("Cmd=%q want=event_msg", packet.Cmd)
	}
}

func TestAdapter_NormalizeApproval_UsesLocalAction(t *testing.T) {
	a := NewAdapter()
	packet, err := a.NormalizeApproval(context.Background(), agentadapter.DomainApprovalEvent{
		ActionID:   "act-agy-1",
		ActionType: "exec_approval",
		Params:     json.RawMessage(`{"command":"ls"}`),
		TimeoutMs:  30000,
	})
	if err != nil {
		t.Fatalf("NormalizeApproval error: %v", err)
	}
	if packet == nil {
		t.Fatal("NormalizeApproval returned nil packet")
	}
	if packet.Cmd != "local_action" {
		t.Fatalf("Cmd=%q want=local_action", packet.Cmd)
	}
}

func TestAdapter_NormalizeInbound_SessionBindingCard(t *testing.T) {
	a := NewAdapter()
	payload := map[string]any{
		"session_id": "sess-agy-1",
		"content":    "",
		"channel_data": map[string]any{
			"acp": map[string]any{
				"sessionBinding": map[string]any{
					"status":      "missing",
					"reason":      "binding_missing",
					"initial_cwd": "/Users/test/project",
				},
			},
		},
	}
	raw, _ := json.Marshal(payload)
	event, err := a.NormalizeInbound(context.Background(), raw)
	if err != nil {
		t.Fatalf("NormalizeInbound error: %v", err)
	}
	if event.Content == "" {
		t.Fatal("expected non-empty content for session binding card")
	}
	// 应该包含 grix://card/agent_open_session 链接
	if len(event.Content) < 10 {
		t.Fatalf("content too short: %q", event.Content)
	}
}
