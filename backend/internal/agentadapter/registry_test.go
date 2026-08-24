package agentadapter_test

import (
	"context"
	"testing"

	"github.com/askie/grix/backend/internal/agentadapter"
)

type stubAdapter struct {
	family       string
	adapterID    string
	versionRange string
	requiredCaps []string
	optionalCaps []string
}

func (a stubAdapter) Family() string    { return a.family }
func (a stubAdapter) AdapterID() string { return a.adapterID }
func (a stubAdapter) Supports(meta agentadapter.AgentClientMeta) bool {
	family := meta.HostType
	if family == "" {
		family = meta.ClientType
	}
	return family == a.family
}
func (a stubAdapter) NormalizeInbound(context.Context, []byte) (*agentadapter.NormalizedInboundEvent, error) {
	return nil, nil
}
func (a stubAdapter) NormalizeOutbound(context.Context, agentadapter.DomainOutboundEvent) (*agentadapter.AdapterOutboundPacket, error) {
	return nil, nil
}
func (a stubAdapter) NormalizeApproval(context.Context, agentadapter.DomainApprovalEvent) (*agentadapter.AdapterApprovalPacket, error) {
	return nil, nil
}
func (a stubAdapter) NormalizeStatus(context.Context, agentadapter.DomainStatusEvent) (*agentadapter.AdapterStatusPacket, error) {
	return nil, nil
}
func (a stubAdapter) VersionRange() string           { return a.versionRange }
func (a stubAdapter) RequiredCapabilities() []string { return a.requiredCaps }
func (a stubAdapter) OptionalCapabilities() []string { return a.optionalCaps }
func (a stubAdapter) DegradePolicy() agentadapter.DegradePolicy {
	return agentadapter.DegradeToBasic
}

func TestRegistryLookupByFamily_SortsMostSpecificAdaptersFirst(t *testing.T) {
	registry := agentadapter.NewRegistry()
	registry.Register(stubAdapter{
		family:       "openclaw",
		adapterID:    "openclaw/base",
		versionRange: ">=2026.1",
	})
	registry.Register(stubAdapter{
		family:       "openclaw",
		adapterID:    "openclaw/v2026.4",
		versionRange: ">=2026.4 <2026.5",
	})
	registry.Register(stubAdapter{
		family:       "openclaw",
		adapterID:    "openclaw/v2026.4.1",
		versionRange: ">=2026.4.1 <2026.4.2",
	})

	adapters := registry.LookupByFamily("openclaw")
	if len(adapters) != 3 {
		t.Fatalf("LookupByFamily returned %d adapters, want 3", len(adapters))
	}

	got := []string{
		adapters[0].AdapterID(),
		adapters[1].AdapterID(),
		adapters[2].AdapterID(),
	}
	want := []string{
		"openclaw/v2026.4.1",
		"openclaw/v2026.4",
		"openclaw/base",
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("adapter order = %v, want %v", got, want)
		}
	}
}

func TestRegistryRegisterAlias_ResolvesToCanonicalAdapter(t *testing.T) {
	r := agentadapter.NewRegistry()
	a := stubAdapter{family: "deepseek", adapterID: "deepseek/jsonrpc-v1"}
	r.Register(a)
	r.RegisterAlias("deepseek/grix-bridge-v1", "deepseek/jsonrpc-v1")

	if got := r.LookupByID("deepseek/grix-bridge-v1"); got == nil || got.AdapterID() != "deepseek/jsonrpc-v1" {
		t.Fatalf("alias lookup = %v, want canonical adapter", got)
	}
	if len(r.All()) != 1 {
		t.Fatalf("alias must not add an adapter, got %d", len(r.All()))
	}
}
