package qwen

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/askie/grix/backend/internal/agentadapter"
	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/ws/protocol"
)

func TestAdapter_SupportsQwenFamily(t *testing.T) {
	a := NewAdapter()

	if !a.Supports(agentadapter.AgentClientMeta{ClientType: Family}) {
		t.Fatal("expected qwen adapter to support client_type family")
	}
	if !a.Supports(agentadapter.AgentClientMeta{HostType: Family}) {
		t.Fatal("expected qwen adapter to support host_type family")
	}
	if a.Supports(agentadapter.AgentClientMeta{ClientType: "codex"}) {
		t.Fatal("expected qwen adapter to reject non-qwen family")
	}
}

func TestAdapter_NormalizeOutbound_UsesMinimalQwenPayload(t *testing.T) {
	a := NewAdapter()
	packet, err := a.NormalizeOutbound(context.Background(), agentadapter.DomainOutboundEvent{
		EventID:         "evt-qwen-1",
		EventType:       "group_message",
		MirrorMode:      "record_and_reply",
		AgentID:         4101,
		OwnerID:         5101,
		SessionID:       "chat-qwen-1",
		SessionType:     model.SessionTypeGroup,
		MsgID:           6101,
		QuotedMessageID: 6201,
		SenderID:        7101,
		Content:         "hello qwen",
		MentionUserIDs:  []int64{9001},
		ContextMessages: []protocol.ContextMessagePayload{
			{
				MsgID:      6201,
				SenderID:   8001,
				SenderType: 2,
				Content:    "[引用消息]\nquoted reply target",
				QuotedMessageID: 6001,
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
		t.Fatalf("unmarshal payload: %v", err)
	}
	if got := payload["session_id"]; got != "chat-qwen-1" {
		t.Fatalf("session_id=%v want=chat-qwen-1", got)
	}
	if got := payload["sender_id"]; got != "7101" {
		t.Fatalf("sender_id=%v want=7101", got)
	}
	if got := payload["content"]; got != "hello qwen" {
		t.Fatalf("content=%v want=hello qwen", got)
	}
	if got := payload["quoted_message_id"]; got != "6201" {
		t.Fatalf("quoted_message_id=%v want=6201", got)
	}
	if got := payload["session_type"]; got != float64(model.SessionTypeGroup) {
		t.Fatalf("session_type=%v want=%d", got, model.SessionTypeGroup)
	}
	contextMessages, ok := payload["context_messages"].([]any)
	if !ok || len(contextMessages) != 1 {
		t.Fatalf("context_messages=%#v want=1 item", payload["context_messages"])
	}
	contextMessage, ok := contextMessages[0].(map[string]any)
	if !ok {
		t.Fatalf("context_messages[0]=%#v want object", contextMessages[0])
	}
	if got := contextMessage["content"]; got != "[引用消息]\nquoted reply target" {
		t.Fatalf("context_messages[0].content=%v want quoted reply target", got)
	}
	if got := contextMessage["quoted_message_id"]; got != "6001" {
		t.Fatalf("context_messages[0].quoted_message_id=%v want=6001", got)
	}
}

func TestAdapter_NormalizeInbound_PreservesThreadAndNormalizesCards(t *testing.T) {
	a := NewAdapter()
	event, err := a.NormalizeInbound(context.Background(), []byte(`{
		"session_id":"qwen_1003",
		"thread_id":"thread_1003",
		"content":"choose target",
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
	if event.ThreadID != "thread_1003" {
		t.Fatalf("ThreadID=%q want=thread_1003", event.ThreadID)
	}
	if !strings.Contains(event.Content, "grix://card/agent_question") {
		t.Fatalf("content=%q should contain agent question card uri", event.Content)
	}
}

func TestAdapter_NormalizeInbound_ParsesRawQwenRequestPermission(t *testing.T) {
	a := NewAdapter()
	event, err := a.NormalizeInbound(context.Background(), []byte(`{
		"session_id":"qwen_approval_1",
		"content":"Qwen permission request",
		"extra":{
			"channel_data":{
				"qwen":{
					"requestPermission":{
						"sessionId":"acp-session-1",
						"options":[
							{"optionId":"proceed_once","kind":"allow_once","name":"Allow once"},
							{"optionId":"proceed_always","kind":"allow_always","name":"Always allow"},
							{"optionId":"cancel","kind":"reject_once","name":"Deny"}
						],
						"toolCall":{
							"toolCallId":"req-qwen-1",
							"status":"pending",
							"title":"Run /bin/echo qwen approval",
							"kind":"execute",
							"rawInput":{
								"command":"/bin/echo qwen approval"
							}
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

	var extra map[string]any
	if err := json.Unmarshal(event.Extra, &extra); err != nil {
		t.Fatalf("unmarshal extra: %v", err)
	}

	channelData, _ := extra["channel_data"].(map[string]any)
	qwenData, _ := channelData["qwen"].(map[string]any)
	requestPermission, _ := qwenData["requestPermission"].(map[string]any)
	toolCall, _ := requestPermission["toolCall"].(map[string]any)
	if got := toolCall["toolCallId"]; got != "req-qwen-1" {
		t.Fatalf("toolCallId=%v want=req-qwen-1", got)
	}

	execApproval, _ := channelData["execApproval"].(map[string]any)
	if got := execApproval["approvalId"]; got != "req-qwen-1" {
		t.Fatalf("approvalId=%v want=req-qwen-1", got)
	}

	grixData, _ := channelData["grix"].(map[string]any)
	grixApproval, _ := grixData["execApproval"].(map[string]any)
	if got := grixApproval["approval_command_id"]; got != "req-qwen-1" {
		t.Fatalf("approval_command_id=%v want=req-qwen-1", got)
	}
	if got := grixApproval["command"]; got != "/bin/echo qwen approval" {
		t.Fatalf("command=%v want=/bin/echo qwen approval", got)
	}
	if got := grixApproval["host"]; got != "qwen" {
		t.Fatalf("host=%v want=qwen", got)
	}
}

func TestAdapter_NormalizeInbound_ParsesQwenSessionBindingCard(t *testing.T) {
	a := NewAdapter()
	event, err := a.NormalizeInbound(context.Background(), []byte(`{
		"session_id":"qwen_session_binding_1",
		"content":"Qwen needs a workspace before it can reply.",
		"extra":{
			"channel_data":{
				"qwen":{
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
		t.Fatalf("content=%q should contain agent open session card uri", event.Content)
	}
	if !strings.Contains(event.Content, "summary_text=") {
		t.Fatalf("content=%q should contain summary_text in card uri", event.Content)
	}
	if !strings.Contains(event.Content, "当前对话还没有打开工作目录") {
		t.Fatalf("content=%q should contain Chinese summary text", event.Content)
	}
}

func TestAdapter_NormalizeInbound_ParsesQwenSessionBindingCardWithErrorCode(t *testing.T) {
	a := NewAdapter()
	event, err := a.NormalizeInbound(context.Background(), []byte(`{
		"session_id":"qwen_session_binding_2",
		"content":"",
		"extra":{
			"channel_data":{
				"qwen":{
					"sessionBinding":{
						"error_code":"session_binding_missing",
						"initial_cwd":"/home/user/project"
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
		t.Fatalf("content=%q should contain agent open session card uri", event.Content)
	}
	if !strings.Contains(event.Content, "initial_cwd=%2Fhome%2Fuser%2Fproject") {
		t.Fatalf("content=%q should contain initial_cwd in card uri", event.Content)
	}
}

func TestAdapter_OptionalCapabilities_ExposeLocalAction(t *testing.T) {
	a := NewAdapter()

	got := a.OptionalCapabilities()
	if len(got) != 1 || got[0] != "local_action_v1" {
		t.Fatalf("OptionalCapabilities=%v want [local_action_v1]", got)
	}
}
