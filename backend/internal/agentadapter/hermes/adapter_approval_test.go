package hermes

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/askie/grix/backend/internal/agentadapter"
)

// --- Inbound exec_approval via Hermes channel_data ---

func TestAdapter_NormalizeInbound_HermesExecApprovalExtractsDecisionCommands(t *testing.T) {
	a := NewAdapter()
	event, err := a.NormalizeInbound(context.Background(), []byte(`{
		"session_id":"g_4001",
		"content":"[Exec Approval] rm -rf /tmp/test (hermes)",
		"channel_data":{
			"hermes":{
				"execApprovalPending":{
					"approval_id":"req_dc_test",
					"command":"rm -rf /tmp/test",
					"host":"hermes",
					"allowed_decisions":["allow-once","deny"],
					"decision_commands":{
						"allow-once":"/approve req_dc_test allow-once",
						"deny":"/approve req_dc_test deny"
					}
				}
			}
		}
	}`))
	if err != nil {
		t.Fatalf("NormalizeInbound error: %v", err)
	}
	if !strings.Contains(event.Content, "grix://card/exec_approval") {
		t.Fatalf("Content=%q should contain exec approval card uri", event.Content)
	}
	var extra map[string]any
	if err := json.Unmarshal(event.Extra, &extra); err != nil {
		t.Fatalf("unmarshal extra: %v", err)
	}
	channelData, _ := extra["channel_data"].(map[string]any)
	grixData, _ := channelData["grix"].(map[string]any)
	execApproval, _ := grixData["execApproval"].(map[string]any)
	if execApproval == nil {
		t.Fatalf("expected grix.execApproval, got %#v", grixData)
	}
	decisions, _ := execApproval["decision_commands"].(map[string]any)
	if decisions == nil {
		t.Fatalf("expected decision_commands in grix.execApproval, got %#v", execApproval)
	}
	if got := decisions["allow-once"]; got != "/approve req_dc_test allow-once" {
		t.Fatalf("decision_commands[allow-once]=%v want=/approve req_dc_test allow-once", got)
	}
	if got := decisions["deny"]; got != "/approve req_dc_test deny" {
		t.Fatalf("decision_commands[deny]=%v want=/approve req_dc_test deny", got)
	}
}

func TestAdapter_NormalizeInbound_HermesExecApprovalPreservesFullSlug(t *testing.T) {
	a := NewAdapter()
	event, err := a.NormalizeInbound(context.Background(), []byte(`{
		"session_id":"g_4002",
		"content":"[Exec Approval] ls -la (hermes)",
		"channel_data":{
			"hermes":{
				"execApprovalPending":{
					"approval_id":"req_long_approval_id_12345678",
					"approval_slug":"req_long_approval_id_12345678",
					"command":"ls -la",
					"host":"hermes"
				}
			}
		}
	}`))
	if err != nil {
		t.Fatalf("NormalizeInbound error: %v", err)
	}
	var extra map[string]any
	if err := json.Unmarshal(event.Extra, &extra); err != nil {
		t.Fatalf("unmarshal extra: %v", err)
	}
	channelData, _ := extra["channel_data"].(map[string]any)
	replyMeta, _ := channelData["execApproval"].(map[string]any)
	if replyMeta == nil {
		t.Fatalf("expected execApproval metadata, got %#v", channelData)
	}
	slug, _ := replyMeta["approvalSlug"].(string)
	if slug != "req_long_approval_id_12345678" {
		t.Fatalf("approvalSlug=%q want=req_long_approval_id_12345678 (should not be truncated)", slug)
	}
}

func TestAdapter_NormalizeInbound_HermesExecApprovalTruncatesSlugWhenMissing(t *testing.T) {
	a := NewAdapter()
	event, err := a.NormalizeInbound(context.Background(), []byte(`{
		"session_id":"g_4003",
		"content":"[Exec Approval] ls (hermes)",
		"channel_data":{
			"hermes":{
				"execApprovalPending":{
					"approval_id":"req_truncated_slug_long_id",
					"command":"ls",
					"host":"hermes"
				}
			}
		}
	}`))
	if err != nil {
		t.Fatalf("NormalizeInbound error: %v", err)
	}
	var extra map[string]any
	if err := json.Unmarshal(event.Extra, &extra); err != nil {
		t.Fatalf("unmarshal extra: %v", err)
	}
	channelData, _ := extra["channel_data"].(map[string]any)
	replyMeta, _ := channelData["execApproval"].(map[string]any)
	slug, _ := replyMeta["approvalSlug"].(string)
	if slug != "req_trun" {
		t.Fatalf("approvalSlug=%q want=req_trun (8-char truncation fallback)", slug)
	}
}

func TestAdapter_NormalizeInbound_HermesExecApprovalMissingRequiredFields(t *testing.T) {
	a := NewAdapter()
	// Missing approval_id
	event, err := a.NormalizeInbound(context.Background(), []byte(`{
		"session_id":"g_4004",
		"content":"should stay text",
		"channel_data":{
			"hermes":{
				"execApprovalPending":{
					"command":"ls"
				}
			}
		}
	}`))
	if err != nil {
		t.Fatalf("NormalizeInbound error: %v", err)
	}
	if strings.Contains(event.Content, "grix://card/") {
		t.Fatalf("Content=%q should NOT contain card uri when approval_id is missing", event.Content)
	}
	if event.Content != "should stay text" {
		t.Fatalf("Content=%q should be preserved as-is", event.Content)
	}

	// Missing command
	event2, err2 := a.NormalizeInbound(context.Background(), []byte(`{
		"session_id":"g_4005",
		"content":"should stay text 2",
		"channel_data":{
			"hermes":{
				"execApprovalPending":{
					"approval_id":"req_abc"
				}
			}
		}
	}`))
	if err2 != nil {
		t.Fatalf("NormalizeInbound error: %v", err2)
	}
	if strings.Contains(event2.Content, "grix://card/") {
		t.Fatalf("Content=%q should NOT contain card uri when command is missing", event2.Content)
	}
}

func TestAdapter_NormalizeInbound_ExecStatusBizCard(t *testing.T) {
	a := NewAdapter()
	event, err := a.NormalizeInbound(context.Background(), []byte(`{
		"session_id":"g_4006",
		"content":"审批结果",
		"channel_data":{
			"grix":{
				"execStatus":{
					"status":"resolved-allow-once",
					"summary":"Exec approval allowed once.",
					"approval_id":"req_status_1"
				}
			}
		}
	}`))
	if err != nil {
		t.Fatalf("NormalizeInbound error: %v", err)
	}
	if !strings.Contains(event.Content, "grix://card/exec_status") {
		t.Fatalf("Content=%q should contain exec_status card uri", event.Content)
	}
}

func TestAdapter_NormalizeInbound_HermesExecApprovalWithWarningText(t *testing.T) {
	a := NewAdapter()
	event, err := a.NormalizeInbound(context.Background(), []byte(`{
		"session_id":"g_4007",
		"content":"[Exec Approval] rm (hermes)",
		"channel_data":{
			"hermes":{
				"execApprovalPending":{
					"approval_id":"req_warn",
					"command":"rm -rf /",
					"description":"This will destroy everything",
					"host":"hermes",
					"cwd":"/home/user"
				}
			}
		}
	}`))
	if err != nil {
		t.Fatalf("NormalizeInbound error: %v", err)
	}
	var extra map[string]any
	if err := json.Unmarshal(event.Extra, &extra); err != nil {
		t.Fatalf("unmarshal extra: %v", err)
	}
	channelData, _ := extra["channel_data"].(map[string]any)
	grixData, _ := channelData["grix"].(map[string]any)
	execApproval, _ := grixData["execApproval"].(map[string]any)
	if got := execApproval["warning_text"]; got != "This will destroy everything" {
		t.Fatalf("warning_text=%v want='This will destroy everything'", got)
	}
	if got := execApproval["cwd"]; got != "/home/user" {
		t.Fatalf("cwd=%v want=/home/user", got)
	}
}

// --- NormalizeApproval (outbound local_action to hermes-agent) ---

func TestAdapter_NormalizeApproval_UsesLocalAction(t *testing.T) {
	a := NewAdapter()
	params := map[string]interface{}{
		"exec_context_id": "req_123",
		"decision":        "allow-once",
		"actor_id":        "1001",
	}
	paramsJSON, _ := json.Marshal(params)
	packet, err := a.NormalizeApproval(context.Background(), agentadapter.DomainApprovalEvent{
		ActionID:   "exec_approval:evt-1:55",
		ActionType: "exec_approve",
		Params:     paramsJSON,
		TimeoutMs:  15000,
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
	var payload map[string]any
	if err := json.Unmarshal(packet.Payload, &payload); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if got := payload["action_id"]; got != "exec_approval:evt-1:55" {
		t.Fatalf("action_id=%v want=exec_approval:evt-1:55", got)
	}
	if got := payload["action_type"]; got != "exec_approve" {
		t.Fatalf("action_type=%v want=exec_approve", got)
	}
	if got := payload["event_id"]; got != "" {
		t.Fatalf("event_id=%v want=empty (hermes profile always empty)", got)
	}
	if got := payload["timeout_ms"]; got != float64(15000) {
		t.Fatalf("timeout_ms=%v want=15000", got)
	}
	var resultParams map[string]any
	if raw, ok := payload["params"]; ok {
		b, _ := json.Marshal(raw)
		json.Unmarshal(b, &resultParams)
	}
	if resultParams["exec_context_id"] != "req_123" {
		t.Fatalf("params.exec_context_id=%v want=req_123", resultParams["exec_context_id"])
	}
	if resultParams["decision"] != "allow-once" {
		t.Fatalf("params.decision=%v want=allow-once", resultParams["decision"])
	}
}

func TestAdapter_NormalizeApproval_ExecReject(t *testing.T) {
	a := NewAdapter()
	params := map[string]interface{}{
		"exec_context_id": "req_456",
		"decision":        "deny",
	}
	paramsJSON, _ := json.Marshal(params)
	packet, err := a.NormalizeApproval(context.Background(), agentadapter.DomainApprovalEvent{
		ActionID:   "exec_approval:evt-2:60",
		ActionType: "exec_reject",
		Params:     paramsJSON,
		TimeoutMs:  15000,
	})
	if err != nil {
		t.Fatalf("NormalizeApproval error: %v", err)
	}
	var payload map[string]any
	if err := json.Unmarshal(packet.Payload, &payload); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if got := payload["action_type"]; got != "exec_reject" {
		t.Fatalf("action_type=%v want=exec_reject", got)
	}
}

// --- Approval result reply coverage ---
// Tests for buildExecApprovalResultReply live in local_action_handler_test.go
// Here we test the Hermes adapter's role in the round-trip.

func TestAdapter_NormalizeInbound_HermesExecApproval_NoApprovalPending(t *testing.T) {
	a := NewAdapter()
	// channel_data with hermes but no execApprovalPending
	event, err := a.NormalizeInbound(context.Background(), []byte(`{
		"session_id":"g_4008",
		"content":"plain message",
		"channel_data":{
			"hermes":{
				"otherData":true
			}
		}
	}`))
	if err != nil {
		t.Fatalf("NormalizeInbound error: %v", err)
	}
	if strings.Contains(event.Content, "grix://card/") {
		t.Fatalf("Content=%q should NOT contain card uri", event.Content)
	}
	if event.Content != "plain message" {
		t.Fatalf("Content=%q want=plain message", event.Content)
	}
}

func TestAdapter_NormalizeInbound_HermesExecApproval_WithExpiresAt(t *testing.T) {
	a := NewAdapter()
	event, err := a.NormalizeInbound(context.Background(), []byte(`{
		"session_id":"g_4009",
		"content":"[Exec Approval] cmd (hermes)",
		"channel_data":{
			"hermes":{
				"execApprovalPending":{
					"approval_id":"req_expire",
					"command":"cmd",
					"host":"hermes",
					"expires_in_seconds":120,
					"expires_at_ms":1700000000000
				}
			}
		}
	}`))
	if err != nil {
		t.Fatalf("NormalizeInbound error: %v", err)
	}
	var extra map[string]any
	if err := json.Unmarshal(event.Extra, &extra); err != nil {
		t.Fatalf("unmarshal extra: %v", err)
	}
	channelData, _ := extra["channel_data"].(map[string]any)
	grixData, _ := channelData["grix"].(map[string]any)
	execApproval, _ := grixData["execApproval"].(map[string]any)
	if got := execApproval["expires_in_seconds"]; got != float64(120) {
		t.Fatalf("expires_in_seconds=%v want=120", got)
	}
	if got := execApproval["expires_at_ms"]; got != float64(1700000000000) {
		t.Fatalf("expires_at_ms=%v want=1700000000000", got)
	}
}
