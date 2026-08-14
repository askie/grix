package claude

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/askie/grix/backend/internal/agentadapter"
	"github.com/askie/grix/backend/internal/ws/protocol"
)

func TestAdapter_NormalizeInbound_NormalizesStructuredExecApprovalCard(t *testing.T) {
	a := NewAdapter()
	event, err := a.NormalizeInbound(context.Background(), []byte(`{
		"session_id":"claude_1001",
		"content":"approval text",
		"extra":{
			"channel_data":{
				"execApproval":{
					"approvalId":"74569573",
					"approvalSlug":"74569573",
					"allowedDecisions":["allow-once","allow-always","deny"]
				},
				"grix":{
					"execApproval":{
						"approval_command_id":"74569573",
						"command":"echo hi",
						"host":"Claude Grix",
						"cwd":"/tmp/demo"
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
	var extra map[string]any
	if err := json.Unmarshal(event.Extra, &extra); err != nil {
		t.Fatalf("unmarshal extra: %v", err)
	}
	if _, ok := extra["channel_data"].(map[string]any); !ok {
		t.Fatalf("expected channel_data in extra, got %#v", extra)
	}
}

func TestAdapter_NormalizeInbound_ParsesPlainTextExecApprovalFallback(t *testing.T) {
	a := NewAdapter()
	raw, err := json.Marshal(map[string]any{
		"session_id": "claude_1002",
		"content": strings.Join([]string{
			"🔒 Exec approval required",
			"ID: approval_full_123",
			"Command: `npm run deploy`",
			"CWD: /srv/app",
			"Host: Claude Grix",
			"Expires in: 120s",
			"Reply with: /approve <id> allow-once|allow-always|deny",
		}, "\n"),
	})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}

	event, err := a.NormalizeInbound(context.Background(), raw)
	if err != nil {
		t.Fatalf("NormalizeInbound error: %v", err)
	}
	if event == nil {
		t.Fatal("NormalizeInbound returned nil event")
	}
	if !strings.Contains(event.Content, "grix://card/exec_approval") {
		t.Fatalf("content=%q should contain exec approval card uri", event.Content)
	}
	var extra map[string]any
	if err := json.Unmarshal(event.Extra, &extra); err != nil {
		t.Fatalf("unmarshal extra: %v", err)
	}
	channelData, ok := extra["channel_data"].(map[string]any)
	if !ok {
		t.Fatalf("expected synthesized channel_data in extra, got %#v", extra)
	}
	if _, ok := channelData["execApproval"].(map[string]any); !ok {
		t.Fatalf("expected execApproval metadata, got %#v", channelData)
	}
}

func TestAdapter_NormalizeInbound_NormalizesStructuredAgentQuestionCard(t *testing.T) {
	a := NewAdapter()
	event, err := a.NormalizeInbound(context.Background(), []byte(`{
		"session_id":"claude_1003",
		"content":"请确认部署环境",
		"extra":{
			"biz_card":{
				"version":1,
				"type":"agent_question",
				"payload":{
					"request_id":"question_env_1",
					"questions":[
						{
							"index":1,
							"header":"Environment",
							"prompt":"Choose the deployment target.",
							"options":["prod","staging"]
						}
					]
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
	if !strings.Contains(event.Content, "grix://card/agent_question") {
		t.Fatalf("content=%q should contain agent question card uri", event.Content)
	}
	var extra map[string]any
	if err := json.Unmarshal(event.Extra, &extra); err != nil {
		t.Fatalf("unmarshal extra: %v", err)
	}
	if _, ok := extra["biz_card"].(map[string]any); !ok {
		t.Fatalf("expected biz_card in extra, got %#v", extra)
	}
}

func TestAdapter_NormalizeInbound_NormalizesRawSessionBindingRequirement(t *testing.T) {
	a := NewAdapter()
	event, err := a.NormalizeInbound(context.Background(), []byte(`{
		"session_id":"claude_1004",
		"content":"Claude session binding missing.",
		"extra":{
			"channel_data":{
				"grix-claude":{
					"sessionBinding":{
						"status":"missing",
						"reason":"binding_missing"
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
	var extra map[string]any
	if err := json.Unmarshal(event.Extra, &extra); err != nil {
		t.Fatalf("unmarshal extra: %v", err)
	}
	channelData, ok := extra["channel_data"].(map[string]any)
	if !ok {
		t.Fatalf("expected channel_data in extra, got %#v", extra)
	}
	namespace, ok := channelData["grix-claude"].(map[string]any)
	if !ok {
		t.Fatalf("expected grix-claude namespace, got %#v", channelData)
	}
	sessionBinding, ok := namespace["sessionBinding"].(map[string]any)
	if !ok {
		t.Fatalf("expected sessionBinding payload, got %#v", namespace)
	}
	if sessionBinding["status"] != "missing" {
		t.Fatalf("status=%v want=missing", sessionBinding["status"])
	}
}

func TestAdapter_NormalizeOutbound_PreservesMirrorModeAndContextMessages(t *testing.T) {
	a := NewAdapter()
	packet, err := a.NormalizeOutbound(context.Background(), agentadapter.DomainOutboundEvent{
		EventID:    "evt-claude-1",
		EventType:  "group_message",
		MirrorMode: "record_only",
		AgentID:    4101,
		OwnerID:    5101,
		SessionID:  "chat-claude-1",
		MsgID:      6101,
		SenderID:   7101,
		Content:    "hello claude",
		ContextMessages: []protocol.ContextMessagePayload{
			{
				MsgID:      6100,
				SenderID:   7100,
				SenderType: 1,
				MsgType:    1,
				Content:    "[引用消息]\nEarlier visible line",
				QuotedMessageID: 6099,
				CreatedAt:  1700000000000,
			},
		},
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

	var payload map[string]any
	if err := json.Unmarshal(packet.Payload, &payload); err != nil {
		t.Fatalf("unmarshal outbound payload: %v", err)
	}
	if got := payload["mirror_mode"]; got != "record_only" {
		t.Fatalf("mirror_mode=%v want=record_only", got)
	}
	contextMessages, ok := payload["context_messages"].([]any)
	if !ok || len(contextMessages) != 1 {
		t.Fatalf("context_messages=%#v want=1 item", payload["context_messages"])
	}
	contextMessage, ok := contextMessages[0].(map[string]any)
	if !ok {
		t.Fatalf("context_messages[0]=%#v want object", contextMessages[0])
	}
	if got := contextMessage["content"]; got != "[引用消息]\nEarlier visible line" {
		t.Fatalf("context_messages[0].content=%v want quoted visible line", got)
	}
	if got := contextMessage["quoted_message_id"]; got != "6099" {
		t.Fatalf("context_messages[0].quoted_message_id=%v want=6099", got)
	}
}
