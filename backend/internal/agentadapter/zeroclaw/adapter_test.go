package zeroclaw

import (
	"testing"

	"github.com/askie/grix/backend/internal/agentadapter"
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
