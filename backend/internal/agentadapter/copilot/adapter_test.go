package copilot

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/askie/grix/backend/internal/agentadapter"
)

func TestAdapter_SupportsCopilotFamily(t *testing.T) {
	a := NewAdapter()

	if !a.Supports(agentadapter.AgentClientMeta{ClientType: Family}) {
		t.Fatal("expected copilot adapter to support client_type family")
	}
	if !a.Supports(agentadapter.AgentClientMeta{HostType: Family}) {
		t.Fatal("expected copilot adapter to support host_type family")
	}
	if a.Supports(agentadapter.AgentClientMeta{ClientType: "claude"}) {
		t.Fatal("expected copilot adapter to reject non-copilot family")
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
		"session_id":"copilot-session-1",
		"content":"Hello from Copilot",
		"extra":null
	}`))
	if err != nil {
		t.Fatalf("NormalizeInbound error: %v", err)
	}
	if event.Content != "Hello from Copilot" {
		t.Fatalf("content=%q want=%q", event.Content, "Hello from Copilot")
	}
}

func TestAdapter_NormalizeOutbound_UsesEventMsg(t *testing.T) {
	a := NewAdapter()
	packet, err := a.NormalizeOutbound(context.Background(), agentadapter.DomainOutboundEvent{
		EventID:   "evt-copilot-1",
		EventType: "group_message",
		AgentID:   4401,
		OwnerID:   5401,
		SessionID: "chat-copilot-1",
		MsgID:     6401,
		SenderID:  7401,
		Content:   "hello copilot",
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
		ActionID:   "act-copilot-1",
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
		"session_id": "sess-copilot-1",
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
