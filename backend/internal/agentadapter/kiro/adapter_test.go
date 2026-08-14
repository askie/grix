package kiro

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/askie/grix/backend/internal/agentadapter"
)

func TestAdapter_SupportsKiroFamily(t *testing.T) {
	a := NewAdapter()

	if !a.Supports(agentadapter.AgentClientMeta{ClientType: Family}) {
		t.Fatal("expected kiro adapter to support client_type family")
	}
	if !a.Supports(agentadapter.AgentClientMeta{HostType: Family}) {
		t.Fatal("expected kiro adapter to support host_type family")
	}
	if a.Supports(agentadapter.AgentClientMeta{ClientType: "claude"}) {
		t.Fatal("expected kiro adapter to reject non-kiro family")
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
		"session_id":"kiro-session-1",
		"content":"Hello from Kiro",
		"extra":null
	}`))
	if err != nil {
		t.Fatalf("NormalizeInbound error: %v", err)
	}
	if event.Content != "Hello from Kiro" {
		t.Fatalf("content=%q want=%q", event.Content, "Hello from Kiro")
	}
}

func TestAdapter_NormalizeOutbound_UsesEventMsg(t *testing.T) {
	a := NewAdapter()
	packet, err := a.NormalizeOutbound(context.Background(), agentadapter.DomainOutboundEvent{
		EventID:   "evt-kiro-1",
		EventType: "group_message",
		AgentID:   4301,
		OwnerID:   5301,
		SessionID: "chat-kiro-1",
		MsgID:     6301,
		SenderID:  7301,
		Content:   "hello kiro",
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
		ActionID:   "act-kiro-1",
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

func TestAdapter_NormalizeInbound_SessionBindingMissing(t *testing.T) {
	a := NewAdapter()
	raw := `{
		"session_id":"kiro-binding-1",
		"content":"Session binding missing.",
		"msg_type":1,
		"extra":{
			"channel_data":{
				"acp":{
					"sessionBinding":{
						"status":"missing",
						"reason":"binding_missing"
					}
				}
			}
		}
	}`
	event, err := a.NormalizeInbound(context.Background(), []byte(raw))
	if err != nil {
		t.Fatalf("NormalizeInbound error: %v", err)
	}
	if !containsKiroCard(event.Content, "agent_open_session") {
		t.Fatalf("content=%q should contain agent_open_session card", event.Content)
	}
	if !containsKiroCard(event.Content, "当前对话还没有打开工作目录") {
		t.Fatalf("content=%q should contain binding prompt", event.Content)
	}
}

func containsKiroCard(s, substr string) bool {
	return len(s) > 0 && len(substr) > 0 && (containsStr(s, substr))
}

func containsStr(s, substr string) bool {
	for i := 0; i+len(substr) <= len(s); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
