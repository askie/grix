package codex

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/askie/grix/backend/internal/agentadapter"
)

func TestAdapter_SupportsCodexFamily(t *testing.T) {
	a := NewAdapter()

	if !a.Supports(agentadapter.AgentClientMeta{ClientType: Family}) {
		t.Fatal("expected codex adapter to support client_type family")
	}
	if !a.Supports(agentadapter.AgentClientMeta{HostType: Family}) {
		t.Fatal("expected codex adapter to support host_type family")
	}
	if a.Supports(agentadapter.AgentClientMeta{ClientType: "claude"}) {
		t.Fatal("expected codex adapter to reject non-codex family")
	}
}

func TestAdapter_NormalizeInbound_NormalizesStructuredAgentQuestionCard(t *testing.T) {
	a := NewAdapter()
	event, err := a.NormalizeInbound(context.Background(), []byte(`{
		"session_id":"codex_1003",
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

func TestAdapter_NormalizeInbound_ParsesRawCodexExecApprovalRequest(t *testing.T) {
	a := NewAdapter()
	event, err := a.NormalizeInbound(context.Background(), []byte(`{
		"session_id":"chat-approval",
		"content":"Approval required.",
		"extra":{
			"channel_data":{
				"codex":{
					"execApprovalRequest":{
						"method":"item/commandExecution/requestApproval",
						"id":"req-approval-1",
						"params":{
							"command":"pwd",
							"availableDecisions":["accept","acceptForSession","deny"]
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
		t.Fatalf("extra=%#v missing channel_data", extra)
	}
	codexData, _ := channelData["codex"].(map[string]any)
	if codexData == nil {
		t.Fatalf("channel_data=%#v missing codex namespace", channelData)
	}
	if _, ok := codexData["execApprovalRequest"].(map[string]any); !ok {
		t.Fatalf("codex=%#v missing raw execApprovalRequest", codexData)
	}
	execApproval, _ := channelData["execApproval"].(map[string]any)
	if execApproval == nil {
		t.Fatalf("channel_data=%#v missing execApproval metadata", channelData)
	}
	if got := execApproval["approvalId"]; got != "codex_chat-approval_req-approval-1" {
		t.Fatalf("approvalId=%v want=codex_chat-approval_req-approval-1", got)
	}
	grixData, _ := channelData["grix"].(map[string]any)
	if grixData == nil {
		t.Fatalf("channel_data=%#v missing grix namespace", channelData)
	}
	structured, _ := grixData["execApproval"].(map[string]any)
	if structured == nil {
		t.Fatalf("grix=%#v missing execApproval payload", grixData)
	}
	if got := structured["command"]; got != "pwd" {
		t.Fatalf("command=%v want=pwd", got)
	}
}

func TestAdapter_NormalizeInbound_ParsesGrixExecApprovalPending(t *testing.T) {
	a := NewAdapter()
	event, err := a.NormalizeInbound(context.Background(), []byte(`{
		"session_id":"chat-approval-pending",
		"content":"Permission required: ExitPlanMode",
		"extra":{
			"channel_data":{
				"grix":{
					"execApprovalPending":{
						"approval_id":"perm-123",
						"approval_command_id":"perm-123",
						"command":"ExitPlanMode",
						"host":"Codex CLI",
						"description":"Exit plan mode requires user confirmation.",
						"allowed_decisions":["allow-once","deny"]
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
		t.Fatalf("extra=%#v missing channel_data", extra)
	}
	execApproval, _ := channelData["execApproval"].(map[string]any)
	if execApproval == nil {
		t.Fatalf("channel_data=%#v missing execApproval metadata", channelData)
	}
	if got := execApproval["approvalId"]; got != "perm-123" {
		t.Fatalf("approvalId=%v want=perm-123", got)
	}
}

func TestAdapter_NormalizeInbound_ParsesRawCodexSessionBindingRequirement(t *testing.T) {
	a := NewAdapter()
	event, err := a.NormalizeInbound(context.Background(), []byte(`{
		"session_id":"chat-open-session",
		"content":"Codex needs a workspace before it can reply.",
		"extra":{
			"channel_data":{
				"codex":{
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
	codexData, _ := channelData["codex"].(map[string]any)
	if codexData == nil {
		t.Fatalf("channel_data=%#v missing codex namespace", channelData)
	}
	sessionBinding, _ := codexData["sessionBinding"].(map[string]any)
	if sessionBinding == nil {
		t.Fatalf("codex=%#v missing sessionBinding payload", codexData)
	}
	if got := sessionBinding["status"]; got != "missing" {
		t.Fatalf("status=%v want=missing", got)
	}
}

func TestExtractCodexJSONError_ErrorObjectWithMessage(t *testing.T) {
	content, ok := extractCodexJSONError(`{"error":{"message":"Rate limit exceeded"}}`)
	if !ok {
		t.Fatal("expected error to be detected")
	}
	if content != "Error: Rate limit exceeded" {
		t.Fatalf("content=%q want=%q", content, "Error: Rate limit exceeded")
	}
}

func TestExtractCodexJSONError_ErrorObjectWithCodeAndMessage(t *testing.T) {
	content, ok := extractCodexJSONError(`{"error":{"code":"rate_limit","message":"Too many requests"}}`)
	if !ok {
		t.Fatal("expected error to be detected")
	}
	if content != "Error (rate_limit): Too many requests" {
		t.Fatalf("content=%q want=%q", content, "Error (rate_limit): Too many requests")
	}
}

func TestExtractCodexJSONError_ErrorObjectWithCodeOnly(t *testing.T) {
	content, ok := extractCodexJSONError(`{"error":{"code":"auth_failed"}}`)
	if !ok {
		t.Fatal("expected error to be detected")
	}
	if content != "Error: auth_failed" {
		t.Fatalf("content=%q want=%q", content, "Error: auth_failed")
	}
}

func TestExtractCodexJSONError_ErrorString(t *testing.T) {
	content, ok := extractCodexJSONError(`{"error":"Something went wrong"}`)
	if !ok {
		t.Fatal("expected error to be detected")
	}
	if content != "Error: Something went wrong" {
		t.Fatalf("content=%q want=%q", content, "Error: Something went wrong")
	}
}

func TestExtractCodexJSONError_ErrorWithTopLevelMessage(t *testing.T) {
	content, ok := extractCodexJSONError(`{"error":{"code":"1210"},"message":"图片输入格式/解析错误"}`)
	if !ok {
		t.Fatal("expected error to be detected")
	}
	if content != "Error (1210): 图片输入格式/解析错误" {
		t.Fatalf("content=%q want=%q", content, "Error (1210): 图片输入格式/解析错误")
	}
}

func TestExtractCodexJSONError_NonErrorJSONPassthrough(t *testing.T) {
	content, ok := extractCodexJSONError(`{"status":"ok","data":"hello"}`)
	if ok {
		t.Fatalf("expected non-error JSON to pass through, got content=%q", content)
	}
}

func TestExtractCodexJSONError_PlainTextPassthrough(t *testing.T) {
	content, ok := extractCodexJSONError("Hello, this is a normal message.")
	if ok {
		t.Fatalf("expected plain text to pass through, got content=%q", content)
	}
}

func TestExtractCodexJSONError_EmptyPassthrough(t *testing.T) {
	content, ok := extractCodexJSONError("")
	if ok {
		t.Fatalf("expected empty content to pass through, got content=%q", content)
	}
}

func TestAdapter_NormalizeInbound_ExtractsJSONErrorMessage(t *testing.T) {
	a := NewAdapter()
	event, err := a.NormalizeInbound(context.Background(), []byte(`{
		"session_id":"codex_err",
		"content":"{\"error\":{\"code\":\"rate_limit\",\"message\":\"Too many requests\"}}"
	}`))
	if err != nil {
		t.Fatalf("NormalizeInbound error: %v", err)
	}
	if event == nil {
		t.Fatal("NormalizeInbound returned nil event")
	}
	if event.Content != "Error (rate_limit): Too many requests" {
		t.Fatalf("content=%q want=%q", event.Content, "Error (rate_limit): Too many requests")
	}
}

func TestAdapter_NormalizeInbound_PlainTextPassthrough(t *testing.T) {
	a := NewAdapter()
	event, err := a.NormalizeInbound(context.Background(), []byte(`{
		"session_id":"codex_plain",
		"content":"Hello, this is a normal message."
	}`))
	if err != nil {
		t.Fatalf("NormalizeInbound error: %v", err)
	}
	if event == nil {
		t.Fatal("NormalizeInbound returned nil event")
	}
	if event.Content != "Hello, this is a normal message." {
		t.Fatalf("content=%q want=%q", event.Content, "Hello, this is a normal message.")
	}
}

func TestAdapter_NormalizeOutbound_UsesEventMsg(t *testing.T) {
	a := NewAdapter()
	packet, err := a.NormalizeOutbound(context.Background(), agentadapter.DomainOutboundEvent{
		EventID:   "evt-codex-1",
		EventType: "group_message",
		AgentID:   4101,
		OwnerID:   5101,
		SessionID: "chat-codex-1",
		MsgID:     6101,
		SenderID:  7101,
		Content:   "hello codex",
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
