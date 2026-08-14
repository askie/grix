package cursor

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/askie/grix/backend/internal/agentadapter"
)

func TestAdapter_SupportsCursorFamily(t *testing.T) {
	a := NewAdapter()

	if !a.Supports(agentadapter.AgentClientMeta{ClientType: Family}) {
		t.Fatal("expected cursor adapter to support client_type family")
	}
	if !a.Supports(agentadapter.AgentClientMeta{HostType: Family}) {
		t.Fatal("expected cursor adapter to support host_type family")
	}
	if a.Supports(agentadapter.AgentClientMeta{ClientType: "claude"}) {
		t.Fatal("expected cursor adapter to reject non-cursor family")
	}
}

func TestAdapter_NormalizeInbound_ParsesRawCursorSessionBindingRequirement(t *testing.T) {
	a := NewAdapter()
	event, err := a.NormalizeInbound(context.Background(), []byte(`{
		"session_id":"chat-open-session",
		"content":"Cursor needs a workspace before it can reply.",
		"extra":{
			"channel_data":{
				"cursor":{
					"sessionBinding":{
						"status":"missing",
						"reason":"binding_missing",
						"error_code":"session_binding_missing",
						"required_user_command":"/grix open <working-directory>"
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
	cursorData, _ := channelData["cursor"].(map[string]any)
	if cursorData == nil {
		t.Fatalf("channel_data=%#v missing cursor namespace", channelData)
	}
	sessionBinding, _ := cursorData["sessionBinding"].(map[string]any)
	if sessionBinding == nil {
		t.Fatalf("cursor=%#v missing sessionBinding payload", cursorData)
	}
	if got := sessionBinding["status"]; got != "missing" {
		t.Fatalf("status=%v want=missing", got)
	}
}

func TestAdapter_NormalizeInbound_ParsesCursorRawToolUse(t *testing.T) {
	a := NewAdapter()
	event, err := a.NormalizeInbound(context.Background(), []byte(`{
		"session_id":"cursor-tool-use",
		"content":"[cursor] raw_event",
		"extra":{
			"channel_data":{
				"cursor":{
					"raw_event":{
						"type":"tool_use",
						"payload":{
							"tool_name":"Read",
							"tool_input":{"path":"/tmp/demo.txt"}
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
	if !strings.Contains(event.Content, "grix://card/tool_execution") {
		t.Fatalf("content=%q should contain tool execution card uri", event.Content)
	}
}

func TestAdapter_NormalizeInbound_ParsesCursorRawToolCallAlias(t *testing.T) {
	a := NewAdapter()
	event, err := a.NormalizeInbound(context.Background(), []byte(`{
		"session_id":"cursor-tool-call",
		"content":"[cursor] raw_event",
		"extra":{
			"channel_data":{
				"cursor":{
					"raw_event":{
						"type":"tool_call",
						"payload":{
							"tool_name":"Search",
							"tool_input":{"q":"abc"}
						}
					}
				}
			}
		}
	}`))
	if err != nil {
		t.Fatalf("NormalizeInbound error: %v", err)
	}
	if !strings.Contains(event.Content, "grix://card/tool_execution") {
		t.Fatalf("content=%q should contain tool execution card uri", event.Content)
	}
}

func TestAdapter_NormalizeInbound_ParsesCursorRawToolExecutionEndAlias(t *testing.T) {
	a := NewAdapter()
	event, err := a.NormalizeInbound(context.Background(), []byte(`{
		"session_id":"cursor-tool-end",
		"content":"[cursor] raw_event",
		"extra":{
			"channel_data":{
				"cursor":{
					"raw_event":{
						"type":"tool_execution_end",
						"payload":{
							"tool_name":"Read",
							"result":{"ok":true}
						}
					}
				}
			}
		}
	}`))
	if err != nil {
		t.Fatalf("NormalizeInbound error: %v", err)
	}
	if !strings.Contains(event.Content, "grix://card/tool_execution") {
		t.Fatalf("content=%q should contain tool execution card uri", event.Content)
	}
}

func TestAdapter_NormalizeInbound_CursorRawToolUseFallbackIsNotEmpty(t *testing.T) {
	a := NewAdapter()
	event, err := a.NormalizeInbound(context.Background(), []byte(`{
		"session_id":"cursor-tool-use-empty",
		"content":"",
		"extra":{
			"channel_data":{
				"cursor":{
					"raw_event":{
						"type":"tool_use",
						"payload":{
							"tool_input":{"x":1}
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
	if strings.TrimSpace(event.Content) == "" {
		t.Fatalf("content should not be empty, got=%q", event.Content)
	}
}

func TestAdapter_NormalizeInbound_DropsCursorRawSystemEvent(t *testing.T) {
	a := NewAdapter()
	event, err := a.NormalizeInbound(context.Background(), []byte(`{
		"session_id":"cursor-raw-system",
		"content":"[cursor] raw_event",
		"extra":{
			"channel_data":{
				"cursor":{
					"raw_event":{
						"type":"system",
						"payload":{"message":"heartbeat"}
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
	if !event.Drop {
		t.Fatalf("Drop=%v want=true", event.Drop)
	}
}

func TestAdapter_NormalizeInbound_DropsCursorRawUserEvent(t *testing.T) {
	a := NewAdapter()
	event, err := a.NormalizeInbound(context.Background(), []byte(`{
		"session_id":"cursor-raw-user",
		"content":"[cursor] raw_event",
		"extra":{
			"channel_data":{
				"cursor":{
					"raw_event":{
						"type":"user",
						"payload":{"message":"echo"}
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
	if !event.Drop {
		t.Fatalf("Drop=%v want=true", event.Drop)
	}
}

func TestAdapter_NormalizeInbound_PassesThroughPlainContent(t *testing.T) {
	a := NewAdapter()
	event, err := a.NormalizeInbound(context.Background(), []byte(`{
		"session_id":"cursor-session-1",
		"content":"Hello from Cursor",
		"extra":null
	}`))
	if err != nil {
		t.Fatalf("NormalizeInbound error: %v", err)
	}
	if event.Content != "Hello from Cursor" {
		t.Fatalf("content=%q want=%q", event.Content, "Hello from Cursor")
	}
}

func TestAdapter_NormalizeOutbound_UsesEventMsg(t *testing.T) {
	a := NewAdapter()
	packet, err := a.NormalizeOutbound(context.Background(), agentadapter.DomainOutboundEvent{
		EventID:   "evt-cursor-1",
		EventType: "group_message",
		AgentID:   4201,
		OwnerID:   5201,
		SessionID: "chat-cursor-1",
		MsgID:     6201,
		SenderID:  7201,
		Content:   "hello cursor",
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
		ActionID:   "act-cursor-1",
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

func TestAdapter_NormalizeInbound_ParsesCursorRawPermissionRequest(t *testing.T) {
	a := NewAdapter()
	event, err := a.NormalizeInbound(context.Background(), []byte(`{
		"session_id":"cursor-perm",
		"content":"[cursor] raw_event",
		"extra":{
			"channel_data":{
				"cursor":{
					"raw_event":{
						"type":"permission_request",
						"payload":{
							"tool_call_id":"tc-001",
							"tool_name":"RunCommand",
							"tool_input":{"command":"rm -rf /tmp/test"}
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
	if strings.Contains(event.Content, "[cursor] raw_event") {
		t.Fatalf("content=%q should not contain raw_event fallback", event.Content)
	}
	if !strings.Contains(event.Content, "grix://card/exec_approval") {
		t.Fatalf("content=%q should contain exec_approval card uri", event.Content)
	}
}

func TestAdapter_NormalizeInbound_ParsesCursorRawError(t *testing.T) {
	a := NewAdapter()
	event, err := a.NormalizeInbound(context.Background(), []byte(`{
		"session_id":"cursor-error",
		"content":"[cursor] raw_event",
		"extra":{
			"channel_data":{
				"cursor":{
					"raw_event":{
						"type":"error",
						"payload":{
							"message":"connection timeout",
							"detail":"failed to reach server"
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
	if strings.Contains(event.Content, "[cursor] raw_event") {
		t.Fatalf("content=%q should not contain raw_event fallback", event.Content)
	}
	if !strings.Contains(event.Content, "grix://card/agent_status") {
		t.Fatalf("content=%q should contain agent_status card uri", event.Content)
	}
}

func TestAdapter_NormalizeInbound_CursorRawResultIsNoop(t *testing.T) {
	a := NewAdapter()
	event, err := a.NormalizeInbound(context.Background(), []byte(`{
		"session_id":"cursor-result",
		"content":"done",
		"extra":{
			"channel_data":{
				"cursor":{
					"raw_event":{
						"type":"result",
						"payload":{"status":"completed"}
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
	// result类型不做synthesize，走普通流程，content保持原样
	if event.Content != "done" {
		t.Fatalf("content=%q want=%q", event.Content, "done")
	}
}

func TestAdapter_NormalizeInbound_ParsesCursorRawQuestion(t *testing.T) {
	a := NewAdapter()
	event, err := a.NormalizeInbound(context.Background(), []byte(`{
		"session_id":"cursor-question",
		"content":"[cursor] raw_event",
		"extra":{
			"channel_data":{
				"cursor":{
					"raw_event":{
						"type":"question",
						"payload":{
							"request_id":"q-001",
							"message":"Do you want to proceed?",
							"options":["Yes","No"]
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
	if strings.Contains(event.Content, "[cursor] raw_event") {
		t.Fatalf("content=%q should not contain raw_event fallback", event.Content)
	}
	if !strings.Contains(event.Content, "grix://card/agent_question") {
		t.Fatalf("content=%q should contain agent_question card uri", event.Content)
	}
}

func TestAdapter_NormalizeInbound_ParsesCursorRawQuestionRequest(t *testing.T) {
	a := NewAdapter()
	event, err := a.NormalizeInbound(context.Background(), []byte(`{
		"session_id":"cursor-question-req",
		"content":"[cursor] raw_event",
		"extra":{
			"channel_data":{
				"cursor":{
					"raw_event":{
						"type":"question_request",
						"payload":{
							"id":"qr-002",
							"prompt":"Select an option"
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
	if strings.Contains(event.Content, "[cursor] raw_event") {
		t.Fatalf("content=%q should not contain raw_event fallback", event.Content)
	}
	if !strings.Contains(event.Content, "grix://card/agent_question") {
		t.Fatalf("content=%q should contain agent_question card uri", event.Content)
	}
}
