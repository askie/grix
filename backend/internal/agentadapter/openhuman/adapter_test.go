package openhuman

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/askie/grix/backend/internal/agentadapter"
)

func TestAdapter_SupportsOpenHumanFamily(t *testing.T) {
	a := NewAdapter()

	if !a.Supports(agentadapter.AgentClientMeta{ClientType: Family}) {
		t.Fatal("expected openhuman adapter to support client_type family")
	}
	if !a.Supports(agentadapter.AgentClientMeta{HostType: Family}) {
		t.Fatal("expected openhuman adapter to support host_type family")
	}
	if a.Supports(agentadapter.AgentClientMeta{ClientType: "claude"}) {
		t.Fatal("expected openhuman adapter to reject non-openhuman family")
	}
}

func TestAdapter_NormalizeInbound_ParsesSessionBindingMissing(t *testing.T) {
	a := NewAdapter()
	event, err := a.NormalizeInbound(context.Background(), []byte(`{
		"session_id":"oh-session-1",
		"content":"Session binding missing.",
		"extra":{
			"channel_data":{
				"openhuman":{
					"sessionBinding":{
						"status":"missing",
						"reason":"binding_missing",
						"error_code":"session_binding_missing"
					}
				}
			}
		}
	}`))
	if err != nil {
		t.Fatalf("NormalizeInbound error: %v", err)
	}
	if event == nil {
		t.Fatal("NormalizeInbound returned nil event")
	}
	if !strings.Contains(event.Content, "grix://card/agent_open_session") {
		t.Fatalf("content=%q should contain open session card uri", event.Content)
	}
	if !strings.Contains(event.Content, "当前对话还没有打开工作目录") {
		t.Fatalf("content=%q should contain Chinese summary text", event.Content)
	}

	var extra map[string]any
	if err := json.Unmarshal(event.Extra, &extra); err != nil {
		t.Fatalf("unmarshal extra: %v", err)
	}
	channelData, _ := extra["channel_data"].(map[string]any)
	if channelData == nil {
		t.Fatalf("extra=%#v missing channel_data", extra)
	}
	ohData, _ := channelData["openhuman"].(map[string]any)
	if ohData == nil {
		t.Fatalf("channel_data=%#v missing openhuman namespace", channelData)
	}
	sessionBinding, _ := ohData["sessionBinding"].(map[string]any)
	if sessionBinding == nil {
		t.Fatalf("openhuman=%#v missing sessionBinding payload", ohData)
	}
	if got := sessionBinding["status"]; got != "missing" {
		t.Fatalf("status=%v want=missing", got)
	}
}

func TestAdapter_NormalizeInbound_PassesThroughPlainContent(t *testing.T) {
	a := NewAdapter()
	event, err := a.NormalizeInbound(context.Background(), []byte(`{
		"session_id":"oh-session-2",
		"content":"Hello from OpenHuman",
		"extra":null
	}`))
	if err != nil {
		t.Fatalf("NormalizeInbound error: %v", err)
	}
	if event.Content != "Hello from OpenHuman" {
		t.Fatalf("content=%q want=%q", event.Content, "Hello from OpenHuman")
	}
}

func TestAdapter_NormalizeOutbound_UsesEventMsg(t *testing.T) {
	a := NewAdapter()
	packet, err := a.NormalizeOutbound(context.Background(), agentadapter.DomainOutboundEvent{
		EventID:   "evt-oh-1",
		EventType: "group_message",
		AgentID:   4201,
		OwnerID:   5201,
		SessionID: "chat-oh-1",
		MsgID:     6201,
		SenderID:  7201,
		Content:   "hello openhuman",
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
		ActionID:   "act-oh-1",
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
