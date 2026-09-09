package qwenpaw

import (
	"testing"

	"github.com/askie/grix/backend/internal/agentadapter"
)

func TestAdapter_SupportsQwenPawFamily(t *testing.T) {
	a := NewAdapter()

	if !a.Supports(agentadapter.AgentClientMeta{ClientType: Family}) {
		t.Fatal("expected qwenpaw adapter to support client_type family")
	}
	if !a.Supports(agentadapter.AgentClientMeta{HostType: Family}) {
		t.Fatal("expected qwenpaw adapter to support host_type family")
	}
	if a.Supports(agentadapter.AgentClientMeta{ClientType: "claude"}) {
		t.Fatal("expected qwenpaw adapter to reject non-qwenpaw family")
	}
}

func TestAdapter_QwenPawAdapterID(t *testing.T) {
	a := NewAdapter()
	if a.Family() != "qwenpaw" {
		t.Fatalf("Family()=%q want=%q", a.Family(), "qwenpaw")
	}
	if a.AdapterID() != "qwenpaw/base" {
		t.Fatalf("AdapterID()=%q want=%q", a.AdapterID(), "qwenpaw/base")
	}
}
