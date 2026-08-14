package gemini

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/askie/grix/backend/internal/agentadapter"
	"github.com/askie/grix/backend/internal/ws/protocol"
)

func TestAdapter_SupportsGeminiFamily(t *testing.T) {
	a := NewAdapter()

	if !a.Supports(agentadapter.AgentClientMeta{ClientType: Family}) {
		t.Fatal("expected gemini adapter to support client_type family")
	}
	if !a.Supports(agentadapter.AgentClientMeta{HostType: Family}) {
		t.Fatal("expected gemini adapter to support host_type family")
	}
	if a.Supports(agentadapter.AgentClientMeta{ClientType: "claude"}) {
		t.Fatal("expected gemini adapter to reject non-gemini family")
	}
}

func TestAdapter_OptionalCapabilities_IncludeLocalActionApproval(t *testing.T) {
	a := NewAdapter()
	got := a.OptionalCapabilities()
	if len(got) != 2 {
		t.Fatalf("optional capabilities len=%d want=2", len(got))
	}
	if got[0] != "stream_chunk" || got[1] != "local_action_v1" {
		t.Fatalf("optional capabilities=%v want=[stream_chunk local_action_v1]", got)
	}
}

func TestAdapter_NormalizeInbound_NormalizesStructuredAgentQuestionCard(t *testing.T) {
	a := NewAdapter()
	event, err := a.NormalizeInbound(context.Background(), []byte(`{
		"session_id":"gemini_1003",
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

func TestAdapter_NormalizeInbound_ParsesRawGeminiExecApprovalPending(t *testing.T) {
	a := NewAdapter()
	event, err := a.NormalizeInbound(context.Background(), []byte(`{
		"session_id":"gemini_approval_1",
		"content":"Gemini requested approval before continuing this step.\n/approve gemini:bridge:tool allow-once",
		"extra":{
			"channel_data":{
				"gemini":{
					"execApprovalPending":{
						"approvalId":"gemini:bridge:tool",
						"approvalSlug":"gemini:bridge:tool",
						"approvalCommandId":"gemini:bridge:tool",
						"request":{
							"toolCall":{
								"toolCallId":"tool-chat-1",
								"title":"Run shell command",
								"kind":"execute",
								"rawInput":{"command":"echo pong"}
							},
							"options":[
								{"kind":"allow_once","optionId":"allow-once"},
								{"kind":"allow_always","optionId":"allow-always"},
								{"kind":"reject_once","optionId":"deny"}
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

	var extra map[string]any
	if err := json.Unmarshal(event.Extra, &extra); err != nil {
		t.Fatalf("unmarshal extra: %v", err)
	}
	channelData, ok := extra["channel_data"].(map[string]any)
	if !ok {
		t.Fatalf("expected channel_data in extra, got %#v", extra)
	}
	if _, ok := channelData["gemini"].(map[string]any); !ok {
		t.Fatalf("expected gemini namespace to be preserved, got %#v", channelData)
	}
	replyMeta, ok := channelData["execApproval"].(map[string]any)
	if !ok {
		t.Fatalf("expected synthesized execApproval metadata, got %#v", channelData)
	}
	if got := replyMeta["approvalId"]; got != "gemini:bridge:tool" {
		t.Fatalf("approvalId=%v want gemini:bridge:tool", got)
	}
	grixData, ok := channelData["grix"].(map[string]any)
	if !ok {
		t.Fatalf("expected synthesized grix namespace, got %#v", channelData)
	}
	execApproval, ok := grixData["execApproval"].(map[string]any)
	if !ok {
		t.Fatalf("expected synthesized grix execApproval, got %#v", grixData)
	}
	if got := execApproval["command"]; got != "Run shell command: echo pong" {
		t.Fatalf("command=%v want Run shell command: echo pong", got)
	}
}

func TestAdapter_NormalizeInbound_ParsesRawAcpPermissionEventEnvelope(t *testing.T) {
	a := NewAdapter()
	event, err := a.NormalizeInbound(context.Background(), []byte(`{
		"session_id":"gemini_raw_permission_1",
		"content":"permission request",
		"extra":{
			"channel_data":{
				"acp":{
					"raw_event":{
						"type":"permission_request",
						"payload":{
							"request_id":"req-1",
							"tool_call_id":"tool-call-1",
							"tool_name":"bash",
							"tool_title":"Run shell command",
							"tool_input":{"command":"ls -la"},
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

	var extra map[string]any
	if err := json.Unmarshal(event.Extra, &extra); err != nil {
		t.Fatalf("unmarshal extra: %v", err)
	}
	channelData, _ := extra["channel_data"].(map[string]any)
	if channelData == nil {
		t.Fatalf("expected channel_data in extra, got %#v", extra)
	}
	replyMeta, _ := channelData["execApproval"].(map[string]any)
	if replyMeta == nil || replyMeta["approvalId"] != "tool-call-1" {
		t.Fatalf("execApproval=%#v want approvalId=tool-call-1", replyMeta)
	}
	grixData, _ := channelData["grix"].(map[string]any)
	execApproval, _ := grixData["execApproval"].(map[string]any)
	if execApproval == nil {
		t.Fatalf("expected grix execApproval, got %#v", grixData)
	}
	if got := execApproval["command"]; got != "Run shell command: ls -la" {
		t.Fatalf("command=%v want Run shell command: ls -la", got)
	}
}

func TestAdapter_NormalizeInbound_ParsesRawAcpToolUseEventEnvelope(t *testing.T) {
	a := NewAdapter()
	event, err := a.NormalizeInbound(context.Background(), []byte(`{
		"session_id":"gemini_raw_tool_1",
		"content":"tool use",
		"extra":{
			"channel_data":{
				"acp":{
					"raw_event":{
						"type":"tool_use",
						"payload":{
							"tool_name":"filesystem.read",
							"tool_input":{"path":"/workspace/README.md"}
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

func TestAdapter_NormalizeInbound_ParsesRawAcpErrorEventEnvelope(t *testing.T) {
	a := NewAdapter()
	event, err := a.NormalizeInbound(context.Background(), []byte(`{
		"session_id":"gemini_raw_error_1",
		"content":"raw error",
		"extra":{
			"channel_data":{
				"acp":{
					"raw_event":{
						"type":"error",
						"payload":{
							"message":"agent crashed while running the task"
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
	if !strings.Contains(event.Content, "grix://card/agent_status") {
		t.Fatalf("content=%q should contain agent status card uri", event.Content)
	}
}

func TestAdapter_NormalizeInbound_RawAcpEnvelopeMalformedFallsBackToPlainText(t *testing.T) {
	a := NewAdapter()
	event, err := a.NormalizeInbound(context.Background(), []byte(`{
		"session_id":"gemini_raw_bad_1",
		"content":"plain fallback content",
		"extra":{
			"channel_data":{
				"acp":{
					"raw_event":{
						"type":"permission_request",
						"payload":"invalid payload shape"
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
	if event.Content != "plain fallback content" {
		t.Fatalf("content=%q want plain fallback content", event.Content)
	}
}

func TestAdapter_NormalizeInbound_RawAndLegacyCoexist_PrefersRawEnvelope(t *testing.T) {
	a := NewAdapter()
	event, err := a.NormalizeInbound(context.Background(), []byte(`{
		"session_id":"gemini_raw_legacy_1",
		"content":"coexist payload",
		"extra":{
			"channel_data":{
				"acp":{
					"raw_event":{
						"type":"tool_use",
						"payload":{
							"tool_name":"filesystem.read",
							"tool_input":{"path":"/workspace/a.txt"}
						}
					}
				},
				"gemini":{
					"execApprovalPending":{
						"approvalId":"legacy-approval",
						"approvalSlug":"legacy-approval",
						"approvalCommandId":"legacy-approval",
						"request":{
							"toolCall":{
								"title":"Legacy Approval",
								"kind":"execute",
								"rawInput":{"command":"echo legacy"}
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
	if !strings.Contains(event.Content, "grix://card/tool_execution") {
		t.Fatalf("content=%q should contain tool execution card uri from raw_event", event.Content)
	}
}

func TestAdapter_NormalizeOutbound_UsesGeminiACPPayload(t *testing.T) {
	a := NewAdapter()
	packet, err := a.NormalizeOutbound(context.Background(), agentadapter.DomainOutboundEvent{
		EventID:   "evt-gemini-1",
		EventType: "group_message",
		AgentID:   4101,
		OwnerID:   5101,
		SessionID: "chat-gemini-1",
		ThreadID:  "chat-gemini-1",
		MsgID:     6101,
		SenderID:  7101,
		Content:   "hello gemini",
		Extra: mustRawJSON(t, map[string]any{
			"acp": map[string]any{
				"cwd":                    "/workspace/demo",
				"additional_directories": []string{"/workspace/demo/docs"},
				"mcp_servers": []map[string]any{
					{
						"name": "filesystem",
					},
				},
				"mode_id":    "plan",
				"model_id":   "gemini-2.5-flash",
				"timeout_ms": 45_000,
				"prompt": []map[string]any{
					{
						"type": "text",
						"text": "contract prompt",
					},
				},
			},
		}),
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

	// Verify pass-through: the raw DomainOutboundEvent is serialized as-is.
	// ACP data stays inside extra.acp, not promoted to top-level.
	var payload struct {
		EventID   string          `json:"event_id"`
		SessionID string          `json:"session_id"`
		ThreadID  string          `json:"thread_id"`
		Content   string          `json:"content"`
		Extra     json.RawMessage `json:"extra"`
	}
	if err := json.Unmarshal(packet.Payload, &payload); err != nil {
		t.Fatalf("unmarshal normalized payload: %v", err)
	}
	if payload.EventID != "evt-gemini-1" {
		t.Fatalf("event_id=%q want=evt-gemini-1", payload.EventID)
	}
	if payload.SessionID != "chat-gemini-1" {
		t.Fatalf("session_id=%q want=chat-gemini-1", payload.SessionID)
	}
	if payload.ThreadID != "chat-gemini-1" {
		t.Fatalf("thread_id=%q want=chat-gemini-1", payload.ThreadID)
	}
	if payload.Content != "hello gemini" {
		t.Fatalf("content=%q want=hello gemini", payload.Content)
	}

	// Verify ACP data is preserved inside extra
	var extra struct {
		ACP struct {
			Cwd                   string           `json:"cwd"`
			Prompt                []map[string]any `json:"prompt"`
			AdditionalDirectories []string         `json:"additional_directories"`
			MCPServers            []map[string]any `json:"mcp_servers"`
			ModeID                string           `json:"mode_id"`
			ModelID               string           `json:"model_id"`
			TimeoutMS             int              `json:"timeout_ms"`
		} `json:"acp"`
	}
	if err := json.Unmarshal(payload.Extra, &extra); err != nil {
		t.Fatalf("unmarshal extra: %v", err)
	}
	if extra.ACP.Cwd != "/workspace/demo" {
		t.Fatalf("extra.acp.cwd=%q want=/workspace/demo", extra.ACP.Cwd)
	}
	if got := extra.ACP.ModeID; got != "plan" {
		t.Fatalf("extra.acp.mode_id=%q want=plan", got)
	}
	if got := extra.ACP.ModelID; got != "gemini-2.5-flash" {
		t.Fatalf("extra.acp.model_id=%q want=gemini-2.5-flash", got)
	}
	if got := extra.ACP.TimeoutMS; got != 45_000 {
		t.Fatalf("extra.acp.timeout_ms=%d want=45000", got)
	}
	if got := extra.ACP.AdditionalDirectories; len(got) != 1 || got[0] != "/workspace/demo/docs" {
		t.Fatalf("extra.acp.additional_directories=%v want=[/workspace/demo/docs]", got)
	}
	if got := extra.ACP.MCPServers; len(got) != 1 || got[0]["name"] != "filesystem" {
		t.Fatalf("extra.acp.mcp_servers=%v want single filesystem server", got)
	}
	if got := extra.ACP.Prompt; len(got) != 1 || got[0]["text"] != "contract prompt" {
		t.Fatalf("extra.acp.prompt=%v want single contract prompt block", got)
	}
}

func TestAdapter_NormalizeOutbound_PassesThroughContextMessages(t *testing.T) {
	a := NewAdapter()
	packet, err := a.NormalizeOutbound(context.Background(), agentadapter.DomainOutboundEvent{
		EventID:   "evt-gemini-2",
		EventType: "group_message",
		AgentID:   4201,
		OwnerID:   5201,
		SessionID: "chat-gemini-2",
		MsgID:     6201,
		SenderID:  7201,
		Content:   "请继续处理刚才的问题",
		ContextMessages: []protocol.ContextMessagePayload{
			{MsgID: 6301, SenderType: 1, Content: "[引用消息]\n先看这个错误日志", QuotedMessageID: 6001},
			{SenderType: 2, Content: "我已经定位到配置文件"},
		},
	})
	if err != nil {
		t.Fatalf("NormalizeOutbound error: %v", err)
	}

	// Verify pass-through: context_messages and content are preserved as-is.
	// The adapter no longer synthesizes acp.prompt from context_messages.
	var payload struct {
		Content         string                           `json:"content"`
		ContextMessages []protocol.ContextMessagePayload `json:"context_messages"`
	}
	if err := json.Unmarshal(packet.Payload, &payload); err != nil {
		t.Fatalf("unmarshal normalized payload: %v", err)
	}
	if payload.Content != "请继续处理刚才的问题" {
		t.Fatalf("content=%q want=请继续处理刚才的问题", payload.Content)
	}
	if len(payload.ContextMessages) != 2 {
		t.Fatalf("context_messages len=%d want=2", len(payload.ContextMessages))
	}
	if payload.ContextMessages[0].MsgID != 6301 {
		t.Fatalf("context_messages[0].msg_id=%d want=6301", payload.ContextMessages[0].MsgID)
	}
	if payload.ContextMessages[0].Content != "[引用消息]\n先看这个错误日志" {
		t.Fatalf("context_messages[0].content=%q want quoted context", payload.ContextMessages[0].Content)
	}
	if payload.ContextMessages[0].QuotedMessageID != 6001 {
		t.Fatalf("context_messages[0].quoted_message_id=%d want=6001", payload.ContextMessages[0].QuotedMessageID)
	}
	if payload.ContextMessages[1].Content != "我已经定位到配置文件" {
		t.Fatalf("context_messages[1].content=%q want=我已经定位到配置文件", payload.ContextMessages[1].Content)
	}
}

func TestAppendPromptText_AppendsSupplementToConfiguredPrompt(t *testing.T) {
	extra := mustRawJSON(t, map[string]any{
		"acp": map[string]any{
			"prompt": []map[string]any{
				{
					"type": "text",
					"text": "original prompt",
				},
			},
		},
	})

	merged := AppendPromptText(extra, "ignored", nil, "User provided additional details:\nEnvironment: staging")

	var decoded struct {
		ACP struct {
			Prompt []map[string]any `json:"prompt"`
		} `json:"acp"`
	}
	if err := json.Unmarshal(merged, &decoded); err != nil {
		t.Fatalf("unmarshal merged extra: %v", err)
	}
	if len(decoded.ACP.Prompt) != 2 {
		t.Fatalf("prompt blocks=%d want=2", len(decoded.ACP.Prompt))
	}
	if got := decoded.ACP.Prompt[0]["text"]; got != "original prompt" {
		t.Fatalf("prompt[0]=%v want original prompt", got)
	}
	if got := decoded.ACP.Prompt[1]["text"]; got != "User provided additional details:\nEnvironment: staging" {
		t.Fatalf("prompt[1]=%v want appended answer block", got)
	}
}

func mustRawJSON(t *testing.T, value any) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal raw json: %v", err)
	}
	return raw
}
