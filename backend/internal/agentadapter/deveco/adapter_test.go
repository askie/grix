package deveco

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/askie/grix/backend/internal/agentadapter"
)

func TestAdapter_SupportsDevecoFamily(t *testing.T) {
	a := NewAdapter()

	if !a.Supports(agentadapter.AgentClientMeta{ClientType: Family}) {
		t.Fatal("expected deveco adapter to support client_type family")
	}
	if !a.Supports(agentadapter.AgentClientMeta{HostType: Family}) {
		t.Fatal("expected deveco adapter to support host_type family")
	}
	if a.Supports(agentadapter.AgentClientMeta{ClientType: "opencode"}) {
		t.Fatal("expected deveco adapter to reject the plain opencode family (separate client_type)")
	}
	if a.Supports(agentadapter.AgentClientMeta{ClientType: "claude"}) {
		t.Fatal("expected deveco adapter to reject non-deveco family")
	}
}

func TestAdapter_DevecoAdapterID(t *testing.T) {
	a := NewAdapter()
	if a.Family() != "deveco" {
		t.Fatalf("Family()=%q want=%q", a.Family(), "deveco")
	}
	if a.AdapterID() != "deveco/base" {
		t.Fatalf("AdapterID()=%q want=%q", a.AdapterID(), "deveco/base")
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
		"session_id":"deveco-session-1",
		"content":"Hello from DevEco Code",
		"extra":null
	}`))
	if err != nil {
		t.Fatalf("NormalizeInbound error: %v", err)
	}
	if event.Content != "Hello from DevEco Code" {
		t.Fatalf("content=%q want=%q", event.Content, "Hello from DevEco Code")
	}
}

func TestAdapter_NormalizeInbound_SessionBindingMissingCard(t *testing.T) {
	a := NewAdapter()
	event, err := a.NormalizeInbound(context.Background(), []byte(`{
		"session_id":"deveco-session-2",
		"content":"raw fallback text",
		"channel_data": {"opencode": {"sessionBinding": {"status": "missing"}}}
	}`))
	if err != nil {
		t.Fatalf("NormalizeInbound error: %v", err)
	}
	if event.Content == "raw fallback text" {
		t.Fatal("expected session-binding-missing card to replace the raw content")
	}
}

func TestAdapter_NormalizeOutbound_UsesEventMsg(t *testing.T) {
	a := NewAdapter()
	packet, err := a.NormalizeOutbound(context.Background(), agentadapter.DomainOutboundEvent{
		EventID:   "evt-deveco-1",
		EventType: "group_message",
		AgentID:   4302,
		OwnerID:   5302,
		SessionID: "chat-deveco-1",
		MsgID:     6302,
		SenderID:  7302,
		Content:   "hello deveco",
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
		ActionID:   "act-deveco-1",
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

// No TestNormalizeRevoke here: deveco does not implement agentadapter.RevokeEventAdapter,
// matching upstream opencode's own adapter (see opencode/adapter.go and opencode/adapter_test.go —
// neither has one either). Adding revoke support here without connector-side evidence that it
// works differently from opencode would be inventing an unverified capability.
