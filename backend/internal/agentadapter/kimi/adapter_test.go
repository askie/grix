package kimi

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/askie/grix/backend/internal/agentadapter"
)

func TestAdapter_SupportsKimiFamily(t *testing.T) {
	a := NewAdapter()

	if !a.Supports(agentadapter.AgentClientMeta{ClientType: Family}) {
		t.Fatal("expected kimi adapter to support client_type family")
	}
	if !a.Supports(agentadapter.AgentClientMeta{HostType: Family}) {
		t.Fatal("expected kimi adapter to support host_type family")
	}
	if a.Supports(agentadapter.AgentClientMeta{ClientType: "claude"}) {
		t.Fatal("expected kimi adapter to reject non-kimi family")
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
		"session_id":"kimi-session-1",
		"content":"Hello from Kimi",
		"extra":null
	}`))
	if err != nil {
		t.Fatalf("NormalizeInbound error: %v", err)
	}
	if event.Content != "Hello from Kimi" {
		t.Fatalf("content=%q want=%q", event.Content, "Hello from Kimi")
	}
}

func TestAdapter_NormalizeOutbound_UsesEventMsg(t *testing.T) {
	a := NewAdapter()
	packet, err := a.NormalizeOutbound(context.Background(), agentadapter.DomainOutboundEvent{
		EventID:   "evt-kimi-1",
		EventType: "group_message",
		AgentID:   4401,
		OwnerID:   5401,
		SessionID: "chat-kimi-1",
		MsgID:     6401,
		SenderID:  7401,
		Content:   "hello kimi",
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
		ActionID:   "act-kimi-1",
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

// Real bug that triggered this fix: Kimi's ACP permission RPC used to reach
// the server as an opaque plain-text `Permission required:` message with
// only `channel_data.acp.raw_event = {type:"permission_request", ...}`. No
// `execApproval` metadata was ever extracted, so
// `packet_handlers.go:extractApprovalIDFromCard` returned "" and
// `saveApprovalCardMsgID` was never called — the user's later `/approve
// <id> allow` had nothing to resolve, the ACP `session/request_permission`
// RPC waited forever on the connector, and the chat_state hung at
// `running` for the entire turn. This test freezes the exact ACP shape the
// connector's `sendPermissionCard` (bridge.ts) emits and asserts we
// synthesize an `exec_approval` card that later card-store lookups can
// key off.
func TestAdapter_NormalizeInbound_ParsesRawAcpPermissionEventEnvelope(t *testing.T) {
	a := NewAdapter()
	event, err := a.NormalizeInbound(context.Background(), []byte(`{
		"session_id":"kimi-acp-permission-1",
		"content":"Permission required: Run shell command",
		"extra":{
			"channel_data":{
				"acp":{
					"raw_event":{
						"type":"permission_request",
						"payload":{
							"request_id":"req-kimi-1",
							"tool_call_id":"tool_kimi_bash_1",
							"tool_name":"Bash",
							"tool_title":"Run shell command",
							"tool_input":{"command":"npm test -- tests/foo.test.ts"},
							"options":[
								{"kind":"allow_once"},
								{"kind":"allow_always"},
								{"kind":"reject_once"}
							]
						}
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
	if !strings.Contains(event.Content, "grix://card/exec_approval") {
		t.Fatalf("content=%q should contain exec approval card uri", event.Content)
	}
	// The tool_call_id must round-trip verbatim as approval_command_id so
	// the connector's `pendingApprovals.get(toolCallId)` lookup resolves.
	if !strings.Contains(event.Content, "tool_kimi_bash_1") {
		t.Fatalf("content=%q should carry tool_call_id verbatim so pendingApprovals resolves", event.Content)
	}
}

func TestAdapter_NormalizeInbound_SessionBindingMissing(t *testing.T) {
	a := NewAdapter()
	raw := `{
		"session_id":"kimi-binding-1",
		"content":"Session binding missing.",
		"msg_type":1,
		"extra":{
			"channel_data":{
				"acp":{
					"sessionBinding":{
						"status":"missing",
						"reason":"binding_missing"
					}
				}
			}
		}
	}`
	event, err := a.NormalizeInbound(context.Background(), []byte(raw))
	if err != nil {
		t.Fatalf("NormalizeInbound error: %v", err)
	}
	if !strings.Contains(event.Content, "agent_open_session") {
		t.Fatalf("content=%q should contain agent_open_session card", event.Content)
	}
	if !strings.Contains(event.Content, "当前对话还没有打开工作目录") {
		t.Fatalf("content=%q should contain binding prompt", event.Content)
	}
}
