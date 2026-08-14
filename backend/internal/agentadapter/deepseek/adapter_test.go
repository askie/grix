package deepseek

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/askie/grix/backend/internal/agentadapter"
)

func TestAdapterContract(t *testing.T) {
	adapter := NewAdapter()
	if adapter.AdapterID() != AdapterID || adapter.Family() != Family {
		t.Fatalf("identity=%s/%s", adapter.Family(), adapter.AdapterID())
	}
	if !adapter.Supports(agentadapter.AgentClientMeta{ClientType: Family}) ||
		!adapter.Supports(agentadapter.AgentClientMeta{HostType: Family}) {
		t.Fatal("adapter should support DeepSeek client and host family")
	}
	if adapter.Supports(agentadapter.AgentClientMeta{ClientType: "codex"}) {
		t.Fatal("adapter should reject a different family")
	}
	got := adapter.OptionalCapabilities()
	if len(got) != 2 || got[0] != "stream_chunk" || got[1] != "local_action_v1" {
		t.Fatalf("optional capabilities=%v", got)
	}
}

func TestNormalizeInboundPreservesEnvelope(t *testing.T) {
	event, err := NewAdapter().NormalizeInbound(context.Background(), []byte(`{
		"session_id":" sess-deepseek ",
		"thread_id":" thread-1 ",
		"content":"hello",
		"extra":{"trace":"kept"},
		"channel_data":{"deepseek":{"generation":2}}
	}`))
	if err != nil {
		t.Fatalf("NormalizeInbound() error=%v", err)
	}
	if event.SessionID != "sess-deepseek" || event.ThreadID != "thread-1" || event.Content != "hello" {
		t.Fatalf("event=%+v", event)
	}
	var extra map[string]any
	if err := json.Unmarshal(event.Extra, &extra); err != nil {
		t.Fatalf("unmarshal extra: %v", err)
	}
	if extra["trace"] != "kept" || extra["channel_data"] == nil {
		t.Fatalf("extra=%#v", extra)
	}
}

func TestNormalizeInboundUsesCommonCardNormalizers(t *testing.T) {
	event, err := NewAdapter().NormalizeInbound(context.Background(), []byte(`{
		"session_id":"sess-card",
		"content":"choose target",
		"biz_card":{"version":1,"type":"agent_question","payload":{"request_id":"q1","questions":[{"index":1,"header":"Target","prompt":"Choose","options":["prod","stage"]}]}}
	}`))
	if err != nil {
		t.Fatalf("NormalizeInbound() error=%v", err)
	}
	if !strings.Contains(event.Content, "grix://card/agent_question") {
		t.Fatalf("content=%q", event.Content)
	}
}

func TestNormalizeInboundParsesSessionBindingMissing(t *testing.T) {
	event, err := NewAdapter().NormalizeInbound(context.Background(), []byte(`{
		"session_id":"sess-deepseek-bind",
		"content":"Session binding missing.",
		"extra":{
			"channel_data":{
				"deepseek":{
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
		t.Fatalf("NormalizeInbound() error=%v", err)
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
	deepseekData, _ := channelData["deepseek"].(map[string]any)
	sessionBinding, _ := deepseekData["sessionBinding"].(map[string]any)
	if sessionBinding == nil {
		t.Fatalf("extra=%#v missing deepseek.sessionBinding", extra)
	}
	if got := sessionBinding["status"]; got != "missing" {
		t.Fatalf("status=%v want=missing", got)
	}
}

func TestNormalizeOutboundApprovalAndStatus(t *testing.T) {
	adapter := NewAdapter()
	out, err := adapter.NormalizeOutbound(context.Background(), agentadapter.DomainOutboundEvent{
		EventID: "evt-1", SessionID: "sess-1", ContextMessages: nil,
	})
	if err != nil || out.Cmd != "event_msg" || !strings.Contains(string(out.Payload), `"event_id":"evt-1"`) {
		t.Fatalf("out=%+v err=%v", out, err)
	}
	approval, err := adapter.NormalizeApproval(context.Background(), agentadapter.DomainApprovalEvent{
		ActionID: "action-1", ActionType: "set_mode", Params: json.RawMessage(`{"mode_id":"approval"}`), TimeoutMs: 15000,
	})
	if err != nil || approval.Cmd != "local_action" || !strings.Contains(string(approval.Payload), `"action_type":"set_mode"`) {
		t.Fatalf("approval=%+v err=%v", approval, err)
	}
	status, err := adapter.NormalizeStatus(context.Background(), agentadapter.DomainStatusEvent{SessionID: "sess-1", Status: "idle"})
	if err != nil || status.Cmd != "agent_state_sync" {
		t.Fatalf("status=%+v err=%v", status, err)
	}
}
