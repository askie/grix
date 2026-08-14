package agentapi

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/askie/grix/backend/internal/ws/protocol"
)

// --- Input parsing guard tests ---

func TestParseExecApprovalCommand_ApproveCommandPath(t *testing.T) {
	tests := []struct {
		name              string
		input             string
		wantID            string
		wantApprovalID    string
		wantApprovalCmdID string
		wantDecision      string
		wantOK            bool
	}{
		{
			name:              "allow-once",
			input:             "/approve req_abc allow-once",
			wantID:            "req_abc",
			wantApprovalID:    "",
			wantApprovalCmdID: "req_abc",
			wantDecision:      "allow-once",
			wantOK:            true,
		},
		{
			name:              "allow-always",
			input:             "/approve req_abc always",
			wantID:            "req_abc",
			wantApprovalCmdID: "req_abc",
			wantDecision:      "allow-always",
			wantOK:            true,
		},
		{
			name:              "deny",
			input:             "/approve req_xyz deny",
			wantID:            "req_xyz",
			wantApprovalCmdID: "req_xyz",
			wantDecision:      "deny",
			wantOK:            true,
		},
		{
			name:              "reject alias",
			input:             "/approve req_123 reject",
			wantID:            "req_123",
			wantApprovalCmdID: "req_123",
			wantDecision:      "deny",
			wantOK:            true,
		},
		{
			name:              "allow-rule",
			input:             "/grix approval req_456 allow-rule 3",
			wantID:            "req_456",
			wantApprovalCmdID: "req_456",
			wantDecision:      "allow-rule",
			wantOK:            true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			parsed := parseExecApprovalCommand(tc.input)
			if !parsed.matched {
				t.Fatalf("expected matched=true for %q", tc.input)
			}
			if !parsed.ok {
				t.Fatalf("expected ok=true, err=%q", parsed.err)
			}
			if parsed.id != tc.wantID {
				t.Errorf("id=%q want=%q", parsed.id, tc.wantID)
			}
			if parsed.approvalID != tc.wantApprovalID {
				t.Errorf("approvalID=%q want=%q", parsed.approvalID, tc.wantApprovalID)
			}
			if parsed.approvalCommandID != tc.wantApprovalCmdID {
				t.Errorf("approvalCommandID=%q want=%q", parsed.approvalCommandID, tc.wantApprovalCmdID)
			}
			if parsed.decision != tc.wantDecision {
				t.Errorf("decision=%q want=%q", parsed.decision, tc.wantDecision)
			}
		})
	}
}

func TestParseExecApprovalCommand_DirectivePath(t *testing.T) {
	tests := []struct {
		name              string
		input             string
		wantApprovalID    string
		wantApprovalCmdID string
		wantDecision      string
	}{
		{
			name:              "full directive with approval_id",
			input:             "[[exec-approval-resolution|approval_id=req_full|approval_command_id=req_full|decision=allow-once]]",
			wantApprovalID:    "req_full",
			wantApprovalCmdID: "req_full",
			wantDecision:      "allow-once",
		},
		{
			name:              "directive with only approval_command_id",
			input:             "[[exec-approval-resolution|approval_command_id=req_cmd_only|decision=deny]]",
			wantApprovalID:    "",
			wantApprovalCmdID: "req_cmd_only",
			wantDecision:      "deny",
		},
		{
			name:              "directive with only approval_id falls back to approvalCommandID",
			input:             "[[exec-approval-resolution|approval_id=req_slug|decision=allow]]",
			wantApprovalID:    "req_slug",
			wantApprovalCmdID: "req_slug",
			wantDecision:      "allow",
		},
		{
			name:              "directive with reason",
			input:             "[[exec-approval-resolution|approval_id=req_reason|approval_command_id=req_reason|decision=deny|reason=unsafe+command]]",
			wantApprovalID:    "req_reason",
			wantApprovalCmdID: "req_reason",
			wantDecision:      "deny",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			parsed := parseExecApprovalCommand(tc.input)
			if !parsed.matched {
				t.Fatalf("expected matched=true")
			}
			if !parsed.ok {
				t.Fatalf("expected ok=true, err=%q", parsed.err)
			}
			if parsed.approvalID != tc.wantApprovalID {
				t.Errorf("approvalID=%q want=%q", parsed.approvalID, tc.wantApprovalID)
			}
			if parsed.approvalCommandID != tc.wantApprovalCmdID {
				t.Errorf("approvalCommandID=%q want=%q", parsed.approvalCommandID, tc.wantApprovalCmdID)
			}
			if parsed.decision != tc.wantDecision {
				t.Errorf("decision=%q want=%q", parsed.decision, tc.wantDecision)
			}
		})
	}
}

func TestParseExecApprovalCommand_InvalidInput(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{name: "empty", input: ""},
		{name: "unrelated command", input: "/help"},
		{name: "approve missing id", input: "/approve"},
		{name: "approve missing decision", input: "/approve req_abc"},
		{name: "directive missing decision", input: "[[exec-approval-resolution|approval_id=req_x]]"},
		{name: "directive malformed", input: "[[exec-approval-resolution|broken]]"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			parsed := parseExecApprovalCommand(tc.input)
			if parsed.ok {
				t.Fatalf("expected ok=false for %q, got ok=true id=%q decision=%q", tc.input, parsed.id, parsed.decision)
			}
		})
	}
}

// 正文里提到审批指令标记的普通消息不得被拦截：整条消息必须就是这条指令本身。
// 否则用户消息会被吞成审批指令、只换回一句 usage 提示，永远送不到 agent。
func TestParseExecApprovalCommand_DirectiveInsideProseNotMatched(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{
			name:  "directive quoted inside a sentence",
			input: "帮我看下这个格式 [[exec-approval-resolution|approval_id=req_x|decision=allow]] 是干嘛的",
		},
		{
			name:  "directive with trailing prose",
			input: "[[exec-approval-resolution|approval_id=req_x|decision=allow]] 顺便解释一下",
		},
		{
			name:  "directive with leading prose",
			input: "参考：[[exec-approval-resolution|approval_id=req_x|decision=allow]]",
		},
		{
			// 曾在小写串上取下标却切原串：İ 小写后字节数变短，body 被切错位。
			name:  "directive after case-folding-shrinking rune",
			input: "İİİ[[exec-approval-resolution|approval_id=req_x|decision=allow-once]]",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			parsed := parseExecApprovalCommand(tc.input)
			if parsed.matched {
				t.Fatalf("expected matched=false for %q, got matched=true err=%q", tc.input, parsed.err)
			}
		})
	}
}

// --- Output parameter construction guard tests ---

func TestBuildExecApprovalReplyAction_ApprovalIDFallback(t *testing.T) {
	tests := []struct {
		name                   string
		input                  string
		wantApprovalIDInParams string
	}{
		{
			name:                   "/approve command uses approvalCommandID as approval_id",
			input:                  "/approve req_fallback_test allow-once",
			wantApprovalIDInParams: "req_fallback_test",
		},
		{
			name:                   "directive with explicit approval_id uses that value",
			input:                  "[[exec-approval-resolution|approval_id=req_explicit|approval_command_id=req_explicit|decision=allow-once]]",
			wantApprovalIDInParams: "req_explicit",
		},
		{
			name:                   "directive without approval_id falls back to approval_command_id",
			input:                  "[[exec-approval-resolution|approval_command_id=req_cmd_fallback|decision=allow-once]]",
			wantApprovalIDInParams: "req_cmd_fallback",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dummySendFn := func(_ context.Context, _ SendMessageReq) (*SendMessageResult, error) {
				return &SendMessageResult{MsgID: 1}, nil
			}
			mgr := NewManager("", 30*time.Second, dummySendFn, nil, nil, nil)
			defer mgr.Shutdown()
			conn := &agentConn{
				agentID:      88010,
				ownerID:      11010,
				capabilities: []string{"local_action_v1"},
				localActions: []string{"exec_approve", "exec_reject"},
				send:         make(chan []byte, 2),
			}
			mgr.putConnForTest(conn)

			parsed := parseExecApprovalCommand(tc.input)
			if !parsed.ok {
				t.Fatalf("parse failed: %q", parsed.err)
			}

			evt := DelegateEventPayload{
				EventID:   "evt-guard-1",
				AgentID:   conn.agentID,
				OwnerID:   conn.ownerID,
				SessionID: "sess-guard",
				MsgID:     88001,
				SenderID:  11010,
				Content:   tc.input,
			}
			action, _, ok := mgr.buildExecApprovalReplyAction(evt, parsed)
			if !ok {
				t.Fatal("buildExecApprovalReplyAction returned false")
			}

			gotApprovalID, _ := action.Params["approval_id"].(string)
			if gotApprovalID != tc.wantApprovalIDInParams {
				t.Errorf("params[approval_id]=%q want=%q", gotApprovalID, tc.wantApprovalIDInParams)
			}
			gotCmdID, _ := action.Params["approval_command_id"].(string)
			if gotCmdID == "" {
				t.Error("params[approval_command_id] should not be empty")
			}
			gotExecCtxID, _ := action.Params["exec_context_id"].(string)
			if gotExecCtxID == "" {
				t.Error("params[exec_context_id] should not be empty")
			}
		})
	}
}

func TestBuildExecApprovalReplyAction_DenyUsesExecReject(t *testing.T) {
	dummySendFn := func(_ context.Context, _ SendMessageReq) (*SendMessageResult, error) {
		return &SendMessageResult{MsgID: 1}, nil
	}
	mgr := NewManager("", 30*time.Second, dummySendFn, nil, nil, nil)
	defer mgr.Shutdown()
	conn := &agentConn{
		agentID:      88011,
		ownerID:      11011,
		capabilities: []string{"local_action_v1"},
		localActions: []string{"exec_approve", "exec_reject"},
		send:         make(chan []byte, 2),
	}
	mgr.putConnForTest(conn)

	parsed := parseExecApprovalCommand("/approve req_deny_test deny")
	evt := DelegateEventPayload{
		EventID:   "evt-deny-1",
		AgentID:   conn.agentID,
		OwnerID:   conn.ownerID,
		SessionID: "sess-deny",
		MsgID:     88002,
		SenderID:  11011,
		Content:   "/approve req_deny_test deny",
	}
	action, _, ok := mgr.buildExecApprovalReplyAction(evt, parsed)
	if !ok {
		t.Fatal("buildExecApprovalReplyAction returned false")
	}
	if action.ActionType != "exec_reject" {
		t.Errorf("action_type=%q want=exec_reject", action.ActionType)
	}
	if got, _ := action.Params["decision"].(string); got != "deny" {
		t.Errorf("params[decision]=%q want=deny", got)
	}
}

// --- Result reply output guard tests ---

func TestBuildExecApprovalResultReply_NoDuplicateErrorFields(t *testing.T) {
	pending := &pendingLocalAction{
		actionID:          "act-no-dup",
		kind:              "exec_approval",
		agentID:           88020,
		ownerID:           11020,
		sessionID:         "sess-no-dup",
		quotedMessageID:   88020,
		actionType:        "exec_approve",
		decision:          "allow-once",
		approvalCommandID: "req_no_dup",
		approvalID:        "req_no_dup",
	}

	tests := []struct {
		name        string
		payload     protocol.LocalActionResultPayload
		wantStatus  string
		wantSummary string
		wantWarning string
	}{
		{
			name: "failed with error message - no duplicate",
			payload: protocol.LocalActionResultPayload{
				ActionID:  "act-no-dup",
				Status:    "failed",
				ErrorCode: "missing_approval_id",
				ErrorMsg:  "approval_id is required",
			},
			wantStatus:  "approval-unavailable",
			wantSummary: "审批提交失败。",
			wantWarning: "approval_id is required",
		},
		{
			name: "failed with expired error",
			payload: protocol.LocalActionResultPayload{
				ActionID: "act-no-dup",
				Status:   "failed",
				ErrorMsg: "unknown or expired approval id req_x",
			},
			wantStatus:  "approval-expired",
			wantSummary: "审批请求已过期。",
			wantWarning: "该审批请求已失效。",
		},
		{
			name: "unsupported with custom message",
			payload: protocol.LocalActionResultPayload{
				ActionID: "act-no-dup",
				Status:   "unsupported",
				ErrorMsg: "plugin does not support exec_approve",
			},
			wantStatus:  "approval-unavailable",
			wantSummary: "审批当前不可用。",
			wantWarning: "plugin does not support exec_approve",
		},
		{
			name: "timeout",
			payload: protocol.LocalActionResultPayload{
				ActionID: "act-no-dup",
				Status:   "timeout",
			},
			wantStatus:  "approval-unavailable",
			wantSummary: "审批提交超时。",
			wantWarning: "Agent 未在规定时间内确认该审批请求。",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			reply := buildExecApprovalResultReply(pending, tc.payload)
			if reply.content == "" {
				t.Fatal("reply content is empty")
			}
			if !strings.Contains(reply.content, "grix://card/exec_status") {
				t.Fatalf("content=%q should contain exec_status card", reply.content)
			}

			var extra map[string]any
			if err := json.Unmarshal(reply.extra, &extra); err != nil {
				t.Fatalf("unmarshal extra: %v", err)
			}
			bizCard, _ := extra["biz_card"].(map[string]any)
			payload, _ := bizCard["payload"].(map[string]any)
			if payload == nil {
				t.Fatalf("biz_card=%#v missing payload", bizCard)
			}

			if got := payload["status"]; got != tc.wantStatus {
				t.Errorf("status=%v want=%q", got, tc.wantStatus)
			}
			if got := payload["summary"]; got != tc.wantSummary {
				t.Errorf("summary=%v want=%q", got, tc.wantSummary)
			}
			if got := payload["warning_text"]; got != tc.wantWarning {
				t.Errorf("warning_text=%v want=%q", got, tc.wantWarning)
			}
			// Guard: detail_text must NOT duplicate warning_text
			if detailText, exists := payload["detail_text"]; exists && detailText != "" {
				if detailText == payload["warning_text"] {
					t.Errorf("detail_text=%v MUST NOT duplicate warning_text", detailText)
				}
			}
		})
	}
}

func TestBuildExecApprovalResultReply_OkDecisionStatuses(t *testing.T) {
	tests := []struct {
		decision   string
		wantStatus string
		wantSumm   string
	}{
		{"allow-once", "resolved-allow-once", "已允许执行一次。"},
		{"allow-always", "resolved-allow-always", "已允许永久执行。"},
		{"allow-rule", "resolved-allow-rule", "已按规则允许执行。"},
		{"deny", "resolved-deny", "已拒绝执行。"},
		{"custom", "approval-forwarded", "审批请求已提交。"},
	}
	for _, tc := range tests {
		t.Run(tc.decision, func(t *testing.T) {
			pending := &pendingLocalAction{
				actionID:          "act-ok-" + tc.decision,
				kind:              "exec_approval",
				agentID:           88021,
				ownerID:           11021,
				sessionID:         "sess-ok-status",
				quotedMessageID:   88021,
				actionType:        "exec_approve",
				decision:          tc.decision,
				approvalCommandID: "req_ok_test",
				approvalID:        "req_ok_test",
			}
			reply := buildExecApprovalResultReply(pending, protocol.LocalActionResultPayload{
				ActionID: pending.actionID,
				Status:   "ok",
			})
			var extra map[string]any
			if err := json.Unmarshal(reply.extra, &extra); err != nil {
				t.Fatalf("unmarshal extra: %v", err)
			}
			bizCard, _ := extra["biz_card"].(map[string]any)
			payload, _ := bizCard["payload"].(map[string]any)

			if got := payload["status"]; got != tc.wantStatus {
				t.Errorf("status=%v want=%q", got, tc.wantStatus)
			}
			if got := payload["summary"]; got != tc.wantSumm {
				t.Errorf("summary=%v want=%q", got, tc.wantSumm)
			}
			// ok result must not have warning_text
			if _, exists := payload["warning_text"]; exists {
				t.Error("ok result should not have warning_text")
			}
			// ok result must not have detail_text
			if _, exists := payload["detail_text"]; exists {
				t.Error("ok result should not have detail_text")
			}
		})
	}
}

func TestBuildExecApprovalResultReply_ApprovalIDPropagation(t *testing.T) {
	tests := []struct {
		name       string
		approvalID string
		commandID  string
		wantField  string
		wantValue  string
	}{
		{
			name:       "both set uses approval_id",
			approvalID: "req_aid",
			commandID:  "req_cid",
			wantField:  "approval_id",
			wantValue:  "req_aid",
		},
		{
			name:       "only commandID fills approval_id fallback",
			approvalID: "",
			commandID:  "req_cmd_only",
			wantField:  "approval_id",
			wantValue:  "req_cmd_only",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			pending := &pendingLocalAction{
				actionID:          "act-prop-" + tc.name,
				kind:              "exec_approval",
				agentID:           88022,
				ownerID:           11022,
				sessionID:         "sess-prop",
				quotedMessageID:   88022,
				actionType:        "exec_approve",
				decision:          "allow-once",
				approvalID:        tc.approvalID,
				approvalCommandID: tc.commandID,
			}
			reply := buildExecApprovalResultReply(pending, protocol.LocalActionResultPayload{
				ActionID: pending.actionID,
				Status:   "ok",
			})
			var extra map[string]any
			if err := json.Unmarshal(reply.extra, &extra); err != nil {
				t.Fatalf("unmarshal extra: %v", err)
			}
			bizCard, _ := extra["biz_card"].(map[string]any)
			payload, _ := bizCard["payload"].(map[string]any)

			if got := payload[tc.wantField]; got != tc.wantValue {
				t.Errorf("payload[%q]=%v want=%q", tc.wantField, got, tc.wantValue)
			}
			if tc.commandID != "" {
				if got := payload["approval_command_id"]; got != tc.commandID {
					t.Errorf("payload[approval_command_id]=%v want=%q", got, tc.commandID)
				}
			}
		})
	}
}
