package zeroclaw

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/askie/grix/backend/internal/agentadapter"
	"github.com/askie/grix/backend/internal/ws/protocol"
)

func TestAdapter_SupportsZeroClawFamily(t *testing.T) {
	a := NewAdapter()

	if !a.Supports(agentadapter.AgentClientMeta{ClientType: Family}) {
		t.Fatal("expected zeroclaw adapter to support client_type family")
	}
	if !a.Supports(agentadapter.AgentClientMeta{HostType: Family}) {
		t.Fatal("expected zeroclaw adapter to support host_type family")
	}
	if a.Supports(agentadapter.AgentClientMeta{ClientType: "claude"}) {
		t.Fatal("expected zeroclaw adapter to reject non-zeroclaw family")
	}
}

func TestAdapter_ZeroClawAdapterID(t *testing.T) {
	a := NewAdapter()
	if a.Family() != "zeroclaw" {
		t.Fatalf("Family()=%q want=%q", a.Family(), "zeroclaw")
	}
	if a.AdapterID() != "zeroclaw/base" {
		t.Fatalf("AdapterID()=%q want=%q", a.AdapterID(), "zeroclaw/base")
	}
}

func TestAdapter_NormalizeRevoke(t *testing.T) {
	a := NewAdapter()
	ctx := context.Background()

	packet, err := a.NormalizeRevoke(ctx, agentadapter.DomainRevokeEvent{
		EventID:     "evt-zeroclaw-1",
		SessionID:   "sess-zeroclaw-1",
		ThreadID:    "topic-zeroclaw",
		SessionType: 1,
		MsgID:       18889990099,
		SenderID:    9001,
		IsRevoked:   true,
	})
	if err != nil {
		t.Fatalf("NormalizeRevoke error: %v", err)
	}
	if packet == nil {
		t.Fatal("NormalizeRevoke returned nil packet")
	}
	if packet.Cmd != "event_revoke" {
		t.Fatalf("cmd=%s want=event_revoke", packet.Cmd)
	}

	var payload protocol.AgentRevokeEventPayload
	if err := json.Unmarshal(packet.Payload, &payload); err != nil {
		t.Fatalf("unmarshal revoke payload: %v", err)
	}
	if payload.SystemEvent == nil {
		t.Fatal("expected system_event hint")
	}
	if payload.SystemEvent.Text != "ACP direct message deleted [session_id=sess-zeroclaw-1 msg_id=18889990099 sender_id=9001]" {
		t.Fatalf("system_event.text=%q", payload.SystemEvent.Text)
	}
	if payload.SystemEvent.ContextKey != "acp:revoke:sess-zeroclaw-1:18889990099" {
		t.Fatalf("system_event.context_key=%q", payload.SystemEvent.ContextKey)
	}
}
