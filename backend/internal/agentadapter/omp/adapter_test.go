package omp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/askie/grix/backend/internal/agentadapter"
)

func TestFamilyAndID(t *testing.T) {
	a := NewAdapter()
	if a.Family() != "omp" {
		t.Fatalf("Family() = %q, want %q", a.Family(), "omp")
	}
	if a.AdapterID() != "omp/base" {
		t.Fatalf("AdapterID() = %q, want %q", a.AdapterID(), "omp/base")
	}
}

func TestSupports(t *testing.T) {
	a := NewAdapter()

	tests := []struct {
		name  string
		meta  agentadapter.AgentClientMeta
		match bool
	}{
		{"client_type match", agentadapter.AgentClientMeta{ClientType: "omp"}, true},
		{"host_type match", agentadapter.AgentClientMeta{HostType: "omp"}, true},
		{"host_type priority", agentadapter.AgentClientMeta{ClientType: "pi", HostType: "omp"}, true},
		{"pi client_type does not match omp", agentadapter.AgentClientMeta{ClientType: "pi"}, false},
		{"empty", agentadapter.AgentClientMeta{}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := a.Supports(tt.meta); got != tt.match {
				t.Errorf("Supports(%+v) = %v, want %v", tt.meta, got, tt.match)
			}
		})
	}
}

func TestNormalizeOutbound_IncludesConnectorThinkingDropByDefault(t *testing.T) {
	a := NewAdapter()
	packet, err := a.NormalizeOutbound(context.Background(), agentadapter.DomainOutboundEvent{
		EventID:   "evt-omp-1",
		SessionID: "sess-omp-1",
		Content:   "hello",
	})
	if err != nil {
		t.Fatalf("NormalizeOutbound error: %v", err)
	}
	if packet.Cmd != "event_msg" {
		t.Fatalf("cmd=%q want=event_msg", packet.Cmd)
	}

	var payload struct {
		Extra map[string]any `json:"extra"`
	}
	if err := json.Unmarshal(packet.Payload, &payload); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	connector, _ := payload.Extra["connector"].(map[string]any)
	if got := connector["thinking_events"]; got != "drop" {
		t.Fatalf("connector.thinking_events=%v want=drop", got)
	}
}

func TestNormalizeOutbound_PreservesExistingConnectorAndForcesThinkingDrop(t *testing.T) {
	a := NewAdapter()
	packet, err := a.NormalizeOutbound(context.Background(), agentadapter.DomainOutboundEvent{
		EventID:   "evt-omp-2",
		SessionID: "sess-omp-2",
		Content:   "hello",
		Extra: json.RawMessage(`{
			"foo":"bar",
			"connector":{
				"tool_events":"send",
				"thinking_events":"send"
			}
		}`),
	})
	if err != nil {
		t.Fatalf("NormalizeOutbound error: %v", err)
	}

	var payload struct {
		Extra map[string]any `json:"extra"`
	}
	if err := json.Unmarshal(packet.Payload, &payload); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if got := payload.Extra["foo"]; got != "bar" {
		t.Fatalf("extra.foo=%v want=bar", got)
	}
	connector, _ := payload.Extra["connector"].(map[string]any)
	if got := connector["tool_events"]; got != "send" {
		t.Fatalf("connector.tool_events=%v want=send", got)
	}
	if got := connector["thinking_events"]; got != "drop" {
		t.Fatalf("connector.thinking_events=%v want=drop", got)
	}
}

func TestNormalizeInbound_PlainText(t *testing.T) {
	a := NewAdapter()
	raw, _ := json.Marshal(map[string]any{
		"session_id": "sess_123",
		"content":    "Hello world",
	})

	evt, err := a.NormalizeInbound(context.Background(), raw)
	if err != nil {
		t.Fatal(err)
	}
	if evt.SessionID != "sess_123" {
		t.Errorf("SessionID = %q, want %q", evt.SessionID, "sess_123")
	}
	if evt.Content != "Hello world" {
		t.Errorf("Content = %q, want %q", evt.Content, "Hello world")
	}
}

func TestNormalizeInbound_SessionBindingMissingCard(t *testing.T) {
	a := NewAdapter()
	channelData, _ := json.Marshal(map[string]any{
		"pi": map[string]any{
			"sessionBinding": map[string]any{
				"status":     "missing",
				"reason":     "binding_missing",
				"error_code": "session_binding_missing",
			},
		},
	})
	raw, _ := json.Marshal(map[string]any{
		"session_id":   "sess_1",
		"content":      "hi",
		"channel_data": json.RawMessage(channelData),
	})

	evt, err := a.NormalizeInbound(context.Background(), raw)
	if err != nil {
		t.Fatal(err)
	}
	if evt.Content == "hi" {
		t.Errorf("Content should have been rewritten to the open-workspace card, got %q", evt.Content)
	}
	if !strings.Contains(evt.Content, "grix://card/agent_open_session") {
		t.Errorf("Content = %q, want it to contain a grix://card/agent_open_session link", evt.Content)
	}
}
