package grok

import (
	"testing"

	"github.com/askie/grix/backend/internal/agentadapter"
)

func TestAdapter_SupportsGrokFamily(t *testing.T) {
	a := NewAdapter()

	if !a.Supports(agentadapter.AgentClientMeta{ClientType: Family}) {
		t.Fatal("expected grok adapter to support client_type family")
	}
	if !a.Supports(agentadapter.AgentClientMeta{HostType: Family}) {
		t.Fatal("expected grok adapter to support host_type family")
	}
	if a.Supports(agentadapter.AgentClientMeta{ClientType: "claude"}) {
		t.Fatal("expected grok adapter to reject non-grok family")
	}
}

func TestAdapter_GrokAdapterID(t *testing.T) {
	a := NewAdapter()
	if a.Family() != "grok" {
		t.Fatalf("Family()=%q want=%q", a.Family(), "grok")
	}
	if a.AdapterID() != "grok/base" {
		t.Fatalf("AdapterID()=%q want=%q", a.AdapterID(), "grok/base")
	}
}
