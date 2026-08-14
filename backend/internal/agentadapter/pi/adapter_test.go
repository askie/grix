package pi

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/askie/grix/backend/internal/agentadapter"
)

func TestNormalizeOutbound_IncludesConnectorThinkingDropByDefault(t *testing.T) {
	a := NewAdapter()
	packet, err := a.NormalizeOutbound(context.Background(), agentadapter.DomainOutboundEvent{
		EventID:   "evt-pi-1",
		SessionID: "sess-pi-1",
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
		EventID:   "evt-pi-2",
		SessionID: "sess-pi-2",
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
