package agentadapter_test

import (
	"reflect"
	"testing"

	"github.com/askie/grix/backend/internal/agentadapter"
	"github.com/askie/grix/backend/internal/agentadapter/claude"
	"github.com/askie/grix/backend/internal/agentadapter/codex"
	"github.com/askie/grix/backend/internal/agentadapter/gemini"
	"github.com/askie/grix/backend/internal/agentadapter/hermes"
	"github.com/askie/grix/backend/internal/agentadapter/openclaw"
	"github.com/askie/grix/backend/internal/agentadapter/qwen"
	"github.com/askie/grix/backend/internal/pkg/logger"
	"github.com/askie/grix/backend/internal/ws/protocol"
)

func TestFormatAuthAckExt_ClassifiesCapabilitiesAgainstAdapter(t *testing.T) {
	logger.Init()
	registry := agentadapter.NewRegistry()
	registry.Register(openclaw.NewAdapter())

	result := agentadapter.SelectByMeta(registry, agentadapter.AgentClientMeta{
		ClientType:   openclaw.Family,
		HostType:     openclaw.Family,
		HostVersion:  "2026.3.23-1",
		Capabilities: []string{"stream_chunk", "local_action_v1", protocol.AgentAPISessionSendQuoteCapability, "unknown_cap"},
	})
	if result == nil {
		t.Fatalf("expected adapter selection result")
	}

	ext := agentadapter.FormatAuthAckExt(result, agentadapter.AgentClientMeta{
		ContractVersion: 1,
		Capabilities:    []string{"stream_chunk", "local_action_v1", protocol.AgentAPISessionSendQuoteCapability, "unknown_cap"},
	})

	if got, want := ext["supported_capabilities"], []string{"stream_chunk", "local_action_v1", protocol.AgentAPISessionSendQuoteCapability}; !reflect.DeepEqual(got, want) {
		t.Fatalf("supported_capabilities = %#v, want %#v", got, want)
	}
	if got, want := ext["degraded_capabilities"], []string{}; !reflect.DeepEqual(got, want) {
		t.Fatalf("degraded_capabilities = %#v, want %#v", got, want)
	}
	if got, want := ext["unsupported_capabilities"], []string{"unknown_cap"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("unsupported_capabilities = %#v, want %#v", got, want)
	}
}

func TestFormatAuthAckExt_ClaudeDoesNotRequireStreamChunk(t *testing.T) {
	logger.Init()
	registry := agentadapter.NewRegistry()
	registry.Register(claude.NewAdapter())

	result := agentadapter.SelectByMeta(registry, agentadapter.AgentClientMeta{
		ClientType:   claude.Family,
		HostType:     claude.Family,
		HostVersion:  "1.2.0",
		Capabilities: []string{"local_action_v1", "agent_invoke"},
	})
	if result == nil {
		t.Fatalf("expected adapter selection result")
	}
	if result.Degraded {
		t.Fatalf("expected full compatibility without a stream_chunk requirement")
	}

	ext := agentadapter.FormatAuthAckExt(result, agentadapter.AgentClientMeta{
		ContractVersion: 1,
		Capabilities:    []string{"local_action_v1", "agent_invoke"},
	})

	if got, want := ext["supported_capabilities"], []string{"local_action_v1", "agent_invoke"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("supported_capabilities = %#v, want %#v", got, want)
	}
	if got, want := ext["degraded_capabilities"], []string{}; !reflect.DeepEqual(got, want) {
		t.Fatalf("degraded_capabilities = %#v, want %#v", got, want)
	}
	if got, want := ext["unsupported_capabilities"], []string{}; !reflect.DeepEqual(got, want) {
		t.Fatalf("unsupported_capabilities = %#v, want %#v", got, want)
	}
}

func TestSelectByMeta_PrefersFullCompatibilityOverMoreSpecificDegradedAdapter(t *testing.T) {
	logger.Init()
	registry := agentadapter.NewRegistry()
	registry.Register(stubAdapter{
		family:       "claude",
		adapterID:    "claude/base",
		versionRange: ">=1.0",
	})
	registry.Register(stubAdapter{
		family:       "claude",
		adapterID:    "claude/v1-streaming",
		versionRange: ">=1.0 <2.0",
		requiredCaps: []string{"stream_chunk"},
	})

	result := agentadapter.SelectByMeta(registry, agentadapter.AgentClientMeta{
		ClientType:   "claude",
		HostType:     "claude",
		HostVersion:  "1.5.0",
		Capabilities: []string{"local_action_v1"},
	})
	if result == nil {
		t.Fatalf("expected adapter selection result")
	}
	if result.AdapterID != "claude/base" {
		t.Fatalf("AdapterID = %q, want %q", result.AdapterID, "claude/base")
	}
	if result.Degraded {
		t.Fatalf("expected full compatibility selection, got degraded result")
	}
}

func TestSelectByMeta_PrefersMostSpecificFullyCompatibleAdapter(t *testing.T) {
	logger.Init()
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

	result := agentadapter.SelectByMeta(registry, agentadapter.AgentClientMeta{
		ClientType:   "openclaw",
		HostType:     "openclaw",
		HostVersion:  "2026.4.2",
		Capabilities: []string{"stream_chunk"},
	})
	if result == nil {
		t.Fatalf("expected adapter selection result")
	}
	if result.AdapterID != "openclaw/v2026.4" {
		t.Fatalf("AdapterID = %q, want %q", result.AdapterID, "openclaw/v2026.4")
	}
	if result.Degraded {
		t.Fatalf("expected full compatibility selection, got degraded result")
	}
}

func TestSelectByMeta_UsesAdapterHintAcrossTransportFamily(t *testing.T) {
	logger.Init()
	registry := agentadapter.NewRegistry()
	registry.Register(openclaw.NewAdapter())
	registry.Register(codex.NewAdapter())

	result := agentadapter.SelectByMeta(registry, agentadapter.AgentClientMeta{
		ClientType:   openclaw.Family,
		HostType:     openclaw.Family,
		HostVersion:  "0.1.0",
		Capabilities: []string{"local_action_v1"},
		AdapterHint:  codex.AdapterID,
	})
	if result == nil {
		t.Fatalf("expected adapter selection result")
	}
	if result.AdapterID != codex.AdapterID {
		t.Fatalf("AdapterID = %q, want %q", result.AdapterID, codex.AdapterID)
	}
	if result.Degraded {
		t.Fatalf("expected full compatibility selection, got degraded result")
	}
}

func TestSelectByMeta_SelectsHermesAdapter(t *testing.T) {
	logger.Init()
	registry := agentadapter.NewRegistry()
	registry.Register(hermes.NewAdapter())

	result := agentadapter.SelectByMeta(registry, agentadapter.AgentClientMeta{
		ClientType:   hermes.Family,
		HostType:     hermes.Family,
		Capabilities: []string{"session_route", "local_action_v1"},
	})
	if result == nil {
		t.Fatalf("expected adapter selection result")
	}
	if result.AdapterID != hermes.AdapterID {
		t.Fatalf("AdapterID = %q, want %q", result.AdapterID, hermes.AdapterID)
	}
	if result.Degraded {
		t.Fatalf("expected full compatibility selection, got degraded result")
	}
}

func TestSelectByMeta_SelectsGeminiAdapter(t *testing.T) {
	logger.Init()
	registry := agentadapter.NewRegistry()
	registry.Register(gemini.NewAdapter())

	result := agentadapter.SelectByMeta(registry, agentadapter.AgentClientMeta{
		ClientType:   gemini.Family,
		HostType:     gemini.Family,
		Capabilities: []string{"stream_chunk"},
	})
	if result == nil {
		t.Fatalf("expected adapter selection result")
	}
	if result.AdapterID != gemini.AdapterID {
		t.Fatalf("AdapterID = %q, want %q", result.AdapterID, gemini.AdapterID)
	}
	if result.Degraded {
		t.Fatalf("expected full compatibility selection, got degraded result")
	}
}

func TestSelectByMeta_SelectsQwenAdapter(t *testing.T) {
	logger.Init()
	registry := agentadapter.NewRegistry()
	registry.Register(qwen.NewAdapter())

	result := agentadapter.SelectByMeta(registry, agentadapter.AgentClientMeta{
		ClientType:      qwen.Family,
		HostType:        qwen.Family,
		ContractVersion: 1,
		Capabilities:    []string{},
	})
	if result == nil {
		t.Fatalf("expected adapter selection result")
	}
	if result.AdapterID != qwen.AdapterID {
		t.Fatalf("AdapterID = %q, want %q", result.AdapterID, qwen.AdapterID)
	}
	if result.Degraded {
		t.Fatalf("expected full compatibility selection, got degraded result")
	}
}
