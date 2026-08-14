package agentapi

import (
	"context"
	"encoding/json"
	"sort"
	"strings"
	"testing"
	"time"

	agenttoolbar "github.com/askie/grix/backend/internal/agenttoolbar"
	toolruntime "github.com/askie/grix/backend/internal/agenttoolbar/runtime"
	toolstore "github.com/askie/grix/backend/internal/agenttoolbar/store"
	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/testutil"
	appstore "github.com/askie/grix/backend/internal/store"
	"github.com/askie/grix/backend/internal/ws/protocol"
)

func TestHandleLocalActionResult_ExecApprovalFailureReplies(t *testing.T) {
	testCases := []struct {
		name          string
		payload       protocol.LocalActionResultPayload
		wantStatus    string
		wantSummary   string
		wantWarning   string
		wantDetailMsg string
	}{
		{
			name: "disabled",
			payload: protocol.LocalActionResultPayload{
				ActionID:  "act-disabled",
				Status:    "failed",
				ErrorCode: "exec_approval_disabled",
			},
			wantStatus:  "approval-unavailable",
			wantSummary: "该 Agent 未启用执行审批。",
			wantWarning: "该 Agent 未配置为接受审批回复。",
		},
		{
			name: "unauthorized",
			payload: protocol.LocalActionResultPayload{
				ActionID:  "act-unauthorized",
				Status:    "failed",
				ErrorCode: "exec_approval_unauthorized",
			},
			wantStatus:  "approval-unavailable",
			wantSummary: "你没有权限审批此请求。",
			wantWarning: "当前审批人没有权限提交此审批。",
		},
		{
			name: "timeout",
			payload: protocol.LocalActionResultPayload{
				ActionID: "act-timeout",
				Status:   "timeout",
			},
			wantStatus:  "approval-unavailable",
			wantSummary: "审批提交超时。",
			wantWarning: "Agent 未在规定时间内确认该审批请求。",
		},
		{
			name: "unsupported with message",
			payload: protocol.LocalActionResultPayload{
				ActionID: "act-unsupported",
				Status:   "unsupported",
				ErrorMsg: "plugin rejected local action",
			},
			wantStatus:    "approval-unavailable",
			wantSummary:   "审批当前不可用。",
			wantWarning:   "plugin rejected local action",
			wantDetailMsg: "",
		},
		{
			name: "expired approval",
			payload: protocol.LocalActionResultPayload{
				ActionID: "act-expired",
				Status:   "failed",
				ErrorMsg: "unknown or expired approval id",
			},
			wantStatus:    "approval-expired",
			wantSummary:   "审批请求已过期。",
			wantWarning:   "该审批请求已失效。",
			wantDetailMsg: "",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			sendHandler := &mockSendMessageHandler{
				result: &SendMessageResult{
					MsgID:     92001,
					InboxSeq:  1,
					CreatedAt: time.Now().UnixMilli(),
				},
			}
			mgr := NewManager("", 30*time.Second, sendHandler.handle, nil, nil, nil)
			defer mgr.Shutdown()
			conn := &agentConn{
				agentID:      9997,
				ownerID:      1003,
				isPrimary:    true,
				clientID:     "local-action-result",
				adapterID:    "codex/base",
				capabilities: []string{"local_action_v1"},
				localActions: []string{"exec_approve", "exec_reject"},
				send:         make(chan []byte, 2),
			}
			mgr.putConnForTest(conn)
			mgr.storePendingLocalAction(&pendingLocalAction{
				actionID:          tc.payload.ActionID,
				kind:              "exec_approval",
				agentID:           conn.agentID,
				ownerID:           conn.ownerID,
				sessionID:         "sess-local-action-result",
				quotedMessageID:   18889990777,
				actionType:        "exec_approve",
				decision:          "allow-once",
				approvalCommandID: "req_456",
			})

			mgr.handleLocalActionResult(conn, makePacket(t, protocol.CmdLocalActionResult, 1, tc.payload))

			if len(sendHandler.calls) != 1 {
				t.Fatalf("send handler calls=%d want=1", len(sendHandler.calls))
			}
			if got := sendHandler.calls[0].Content; !strings.Contains(got, "grix://card/exec_status") {
				t.Fatalf("reply content=%q should contain exec_status card", got)
			}
			var extra map[string]any
			if err := json.Unmarshal(sendHandler.calls[0].Extra, &extra); err != nil {
				t.Fatalf("unmarshal extra: %v", err)
			}
			bizCard, _ := extra["biz_card"].(map[string]any)
			if bizCard == nil {
				t.Fatalf("extra=%#v missing biz_card", extra)
			}
			payload, _ := bizCard["payload"].(map[string]any)
			if payload == nil {
				t.Fatalf("biz_card=%#v missing payload", bizCard)
			}
			if got := payload["status"]; got != tc.wantStatus {
				t.Fatalf("payload status=%v want=%q", got, tc.wantStatus)
			}
			if got := payload["summary"]; got != tc.wantSummary {
				t.Fatalf("payload summary=%v want=%q", got, tc.wantSummary)
			}
			if got := payload["warning_text"]; got != tc.wantWarning {
				t.Fatalf("payload warning_text=%v want=%q", got, tc.wantWarning)
			}
			if tc.wantDetailMsg != "" {
				if got := payload["detail_text"]; got != tc.wantDetailMsg {
					t.Fatalf("payload detail_text=%v want=%q", got, tc.wantDetailMsg)
				}
			}
			if got := sendHandler.calls[0].QuotedMessageID; got != 18889990777 {
				t.Fatalf("reply quoted_message_id=%d want=%d", got, 18889990777)
			}
			if got := sendHandler.calls[0].VisibleTo; len(got) != 1 || got[0] != conn.ownerID {
				t.Fatalf("reply visible_to=%v want=[%d]", got, conn.ownerID)
			}

			select {
			case data := <-conn.send:
				var pkt protocol.Packet
				if err := json.Unmarshal(data, &pkt); err != nil {
					t.Fatalf("unmarshal ack packet: %v", err)
				}
				if pkt.Cmd != "local_action_ack" {
					t.Fatalf("ack cmd=%s want=local_action_ack", pkt.Cmd)
				}
			default:
				t.Fatal("expected local_action_ack packet")
			}
		})
	}
}

func TestIsToolbarStateRefreshActionIncludesContextActions(t *testing.T) {
	for _, kind := range []string{"session_control", "set_model", "set_mode", "set_reasoning_effort", "get_context", "get_session_usage"} {
		if !isToolbarStateRefreshAction(kind) {
			t.Fatalf("%s should force toolbar refresh", kind)
		}
	}
}

func TestHandleLocalActionResult_ExecApprovalResultReplies(t *testing.T) {
	testCases := []struct {
		name           string
		decision       string
		payload        protocol.LocalActionResultPayload
		wantCardStatus string
		wantSummary    string
		wantDecision   string
	}{
		{
			name:     "ok allow-once",
			decision: "allow-once",
			payload: protocol.LocalActionResultPayload{
				ActionID: "act-allow-once",
				Status:   "ok",
			},
			wantCardStatus: "resolved-allow-once",
			wantSummary:    "已允许执行一次。",
			wantDecision:   "allow-once",
		},
		{
			name:     "ok allow-always",
			decision: "allow-always",
			payload: protocol.LocalActionResultPayload{
				ActionID: "act-allow-always",
				Status:   "ok",
			},
			wantCardStatus: "resolved-allow-always",
			wantSummary:    "已允许永久执行。",
			wantDecision:   "allow-always",
		},
		{
			name:     "ok allow-rule",
			decision: "allow-rule",
			payload: protocol.LocalActionResultPayload{
				ActionID: "act-allow-rule",
				Status:   "ok",
			},
			wantCardStatus: "resolved-allow-rule",
			wantSummary:    "已按规则允许执行。",
			wantDecision:   "allow-rule",
		},
		{
			name:     "ok deny",
			decision: "deny",
			payload: protocol.LocalActionResultPayload{
				ActionID: "act-deny",
				Status:   "ok",
			},
			wantCardStatus: "resolved-deny",
			wantSummary:    "已拒绝执行。",
			wantDecision:   "deny",
		},
		{
			name:     "ok unknown decision",
			decision: "custom-choice",
			payload: protocol.LocalActionResultPayload{
				ActionID: "act-unknown-decision",
				Status:   "ok",
			},
			wantCardStatus: "approval-forwarded",
			wantSummary:    "审批请求已提交。",
			wantDecision:   "custom-choice",
		},
		{
			name:     "ok empty decision uses result",
			decision: "",
			payload: protocol.LocalActionResultPayload{
				ActionID: "act-result-decision",
				Status:   "ok",
				Result:   "allow-once",
			},
			wantCardStatus: "resolved-allow-once",
			wantSummary:    "已允许执行一次。",
			wantDecision:   "allow-once",
		},
		{
			name:     "failed generic fallback with error message",
			decision: "allow-once",
			payload: protocol.LocalActionResultPayload{
				ActionID:  "act-generic-failed",
				Status:    "failed",
				ErrorCode: "internal_error",
				ErrorMsg:  "something went wrong",
			},
			wantCardStatus: "approval-unavailable",
			wantSummary:    "审批提交失败。",
			wantDecision:   "",
		},
		{
			name:     "failed generic fallback without error message",
			decision: "allow-once",
			payload: protocol.LocalActionResultPayload{
				ActionID:  "act-generic-failed-no-msg",
				Status:    "failed",
				ErrorCode: "unknown_error",
			},
			wantCardStatus: "approval-unavailable",
			wantSummary:    "审批提交失败。",
			wantDecision:   "",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			sendHandler := &mockSendMessageHandler{
				result: &SendMessageResult{
					MsgID:     92300,
					InboxSeq:  1,
					CreatedAt: time.Now().UnixMilli(),
				},
			}
			mgr := NewManager("", 30*time.Second, sendHandler.handle, nil, nil, nil)
			defer mgr.Shutdown()
			conn := &agentConn{
				agentID:      9900,
				ownerID:      1030,
				isPrimary:    true,
				clientID:     "exec-approval-result-test",
				adapterID:    "codex/base",
				capabilities: []string{"local_action_v1"},
				localActions: []string{"exec_approve", "exec_reject"},
				send:         make(chan []byte, 2),
			}
			mgr.putConnForTest(conn)
			mgr.storePendingLocalAction(&pendingLocalAction{
				actionID:          tc.payload.ActionID,
				kind:              "exec_approval",
				agentID:           conn.agentID,
				ownerID:           conn.ownerID,
				sessionID:         "sess-exec-approval-result",
				quotedMessageID:   18889990800,
				actionType:        "exec_approve",
				decision:          tc.decision,
				approvalCommandID: "req_result_test",
				approvalID:        "req_result_test",
			})

			mgr.handleLocalActionResult(conn, makePacket(t, protocol.CmdLocalActionResult, 1, tc.payload))

			if len(sendHandler.calls) != 1 {
				t.Fatalf("send handler calls=%d want=1", len(sendHandler.calls))
			}
			if got := sendHandler.calls[0].Content; !strings.Contains(got, "grix://card/exec_status") {
				t.Fatalf("reply content=%q should contain exec_status card", got)
			}
			var extra map[string]any
			if err := json.Unmarshal(sendHandler.calls[0].Extra, &extra); err != nil {
				t.Fatalf("unmarshal extra: %v", err)
			}
			bizCard, _ := extra["biz_card"].(map[string]any)
			if bizCard == nil {
				t.Fatalf("extra=%#v missing biz_card", extra)
			}
			payload, _ := bizCard["payload"].(map[string]any)
			if payload == nil {
				t.Fatalf("biz_card=%#v missing payload", bizCard)
			}
			if got := payload["status"]; got != tc.wantCardStatus {
				t.Fatalf("payload status=%v want=%q", got, tc.wantCardStatus)
			}
			if got := payload["summary"]; got != tc.wantSummary {
				t.Fatalf("payload summary=%v want=%q", got, tc.wantSummary)
			}
			if tc.wantDecision != "" {
				if got := payload["decision"]; got != tc.wantDecision {
					t.Fatalf("payload decision=%v want=%q", got, tc.wantDecision)
				}
			}
			if got := sendHandler.calls[0].QuotedMessageID; got != 18889990800 {
				t.Fatalf("reply quoted_message_id=%d want=%d", got, 18889990800)
			}
			if got := sendHandler.calls[0].VisibleTo; len(got) != 1 || got[0] != conn.ownerID {
				t.Fatalf("reply visible_to=%v want=[%d]", got, conn.ownerID)
			}

			select {
			case data := <-conn.send:
				var pkt protocol.Packet
				if err := json.Unmarshal(data, &pkt); err != nil {
					t.Fatalf("unmarshal ack packet: %v", err)
				}
				if pkt.Cmd != "local_action_ack" {
					t.Fatalf("ack cmd=%s want=local_action_ack", pkt.Cmd)
				}
			default:
				t.Fatal("expected local_action_ack packet")
			}
		})
	}
}

func TestHandleLocalActionResult_ExecApprovalUnknownStatusNoReply(t *testing.T) {
	sendHandler := &mockSendMessageHandler{
		result: &SendMessageResult{
			MsgID:     92301,
			InboxSeq:  1,
			CreatedAt: time.Now().UnixMilli(),
		},
	}
	mgr := NewManager("", 30*time.Second, sendHandler.handle, nil, nil, nil)
	defer mgr.Shutdown()
	conn := &agentConn{
		agentID:      9901,
		ownerID:      1031,
		clientID:     "exec-approval-unknown-status",
		capabilities: []string{"local_action_v1"},
		localActions: []string{"exec_approve", "exec_reject"},
		send:         make(chan []byte, 2),
	}
	mgr.putConnForTest(conn)
	mgr.storePendingLocalAction(&pendingLocalAction{
		actionID:          "act-unknown-status",
		kind:              "exec_approval",
		agentID:           conn.agentID,
		ownerID:           conn.ownerID,
		sessionID:         "sess-unknown-status",
		quotedMessageID:   18889990801,
		actionType:        "exec_approve",
		decision:          "allow-once",
		approvalCommandID: "req_unknown_status",
	})

	mgr.handleLocalActionResult(conn, makePacket(t, protocol.CmdLocalActionResult, 1, protocol.LocalActionResultPayload{
		ActionID: "act-unknown-status",
		Status:   "bogus_status",
	}))

	if len(sendHandler.calls) != 0 {
		t.Fatalf("send handler calls=%d want=0 (no reply for unknown status)", len(sendHandler.calls))
	}
}

func TestHandleLocalActionResult_HermesRejectsTimeoutAndSkipsAck(t *testing.T) {
	mgr := NewManager("", 30*time.Second, nil, nil, nil, nil)
	defer mgr.Shutdown()
	conn := &agentConn{
		agentID:    9998,
		ownerID:    1004,
		clientID:   "hermes-local-action-result",
		clientType: "hermes",
		send:       make(chan []byte, 2),
	}

	mgr.handleLocalActionResult(conn, makePacket(t, protocol.CmdLocalActionResult, 1, protocol.LocalActionResultPayload{
		ActionID: "act-hermes-timeout",
		Status:   "timeout",
	}))

	select {
	case data := <-conn.send:
		var pkt protocol.Packet
		if err := json.Unmarshal(data, &pkt); err != nil {
			t.Fatalf("unmarshal response packet: %v", err)
		}
		if pkt.Cmd != "error" {
			t.Fatalf("response cmd=%s want=error", pkt.Cmd)
		}
		var payload SendNackPayload
		if err := json.Unmarshal(pkt.Payload, &payload); err != nil {
			t.Fatalf("unmarshal error payload: %v", err)
		}
		if payload.Msg != "invalid status, expected ok|failed|unsupported" {
			t.Fatalf("error msg=%q", payload.Msg)
		}
	default:
		t.Fatal("expected error packet")
	}

	select {
	case data := <-conn.send:
		var pkt protocol.Packet
		if err := json.Unmarshal(data, &pkt); err != nil {
			t.Fatalf("unmarshal trailing packet: %v", err)
		}
		t.Fatalf("did not expect trailing packet, got cmd=%s", pkt.Cmd)
	default:
	}
}

func TestHandleLocalActionResult_ClaudeInteractionReplyFailureSendsAgentStatus(t *testing.T) {
	sendHandler := &mockSendMessageHandler{
		result: &SendMessageResult{
			MsgID:     92011,
			InboxSeq:  2,
			CreatedAt: time.Now().UnixMilli(),
		},
	}
	mgr := NewManager("", 30*time.Second, sendHandler.handle, nil, nil, nil)
	defer mgr.Shutdown()
	conn := &agentConn{
		agentID:      9996,
		ownerID:      1006,
		clientID:     "claude-local-action-result",
		capabilities: []string{"local_action_v1"},
		localActions: []string{"claude_interaction_reply"},
		send:         make(chan []byte, 2),
	}
	mgr.putConnForTest(conn)
	mgr.storePendingLocalAction(&pendingLocalAction{
		actionID:        "act-interaction-failed",
		kind:            "claude_interaction_reply_permission",
		agentID:         conn.agentID,
		ownerID:         conn.ownerID,
		sessionID:       "sess-claude-local-action-result",
		quotedMessageID: 18889990778,
		actionType:      "claude_interaction_reply",
		referenceID:     "req-interaction-1",
	})

	mgr.handleLocalActionResult(conn, makePacket(t, protocol.CmdLocalActionResult, 1, protocol.LocalActionResultPayload{
		ActionID:  "act-interaction-failed",
		Status:    "failed",
		ErrorCode: "interaction_request_not_pending",
	}))

	if len(sendHandler.calls) != 1 {
		t.Fatalf("send handler calls=%d want=1", len(sendHandler.calls))
	}
	if got := sendHandler.calls[0].Content; !strings.Contains(got, "grix://card/agent_status") {
		t.Fatalf("reply content=%q should contain agent_status card", got)
	}
}

func TestHandleLocalActionResult_ClaudeSessionControlSuccessSendsAgentStatus(t *testing.T) {
	sendHandler := &mockSendMessageHandler{
		result: &SendMessageResult{
			MsgID:     92012,
			InboxSeq:  3,
			CreatedAt: time.Now().UnixMilli(),
		},
	}
	mgr := NewManager("", 30*time.Second, sendHandler.handle, nil, nil, nil)
	defer mgr.Shutdown()
	conn := &agentConn{
		agentID:      9994,
		ownerID:      1007,
		clientID:     "claude-session-result",
		capabilities: []string{"local_action_v1"},
		localActions: []string{"session_control"},
		send:         make(chan []byte, 2),
	}
	mgr.putConnForTest(conn)
	mgr.storePendingLocalAction(&pendingLocalAction{
		actionID:        "act-session-ok",
		kind:            "session_control",
		agentID:         conn.agentID,
		ownerID:         conn.ownerID,
		sessionID:       "sess-session-ok",
		quotedMessageID: 18889990779,
		actionType:      "session_control",
		referenceID:     "open",
	})

	mgr.handleLocalActionResult(conn, makePacket(t, protocol.CmdLocalActionResult, 1, protocol.LocalActionResultPayload{
		ActionID: "act-session-ok",
		Status:   "ok",
		Result: map[string]any{
			"domain":  "session_control",
			"verb":    "open",
			"outcome": "opened",
			"binding": map[string]any{
				"aibot_session_id":  "sess-session-ok",
				"claude_session_id": "claude-xyz",
				"cwd":               "/workspace/project",
				"worker_status":     "ready",
			},
		},
	}))

	if len(sendHandler.calls) != 1 {
		t.Fatalf("send handler calls=%d want=1", len(sendHandler.calls))
	}
	if got := sendHandler.calls[0].Content; !strings.Contains(got, "grix://card/agent_status") {
		t.Fatalf("reply content=%q should contain agent_status card", got)
	}
}

func TestHandleLocalActionResult_CodexSessionControlSuccessSendsAgentStatus(t *testing.T) {
	sendHandler := &mockSendMessageHandler{
		result: &SendMessageResult{
			MsgID:     92013,
			InboxSeq:  4,
			CreatedAt: time.Now().UnixMilli(),
		},
	}
	mgr := NewManager("", 30*time.Second, sendHandler.handle, nil, nil, nil)
	defer mgr.Shutdown()
	conn := &agentConn{
		agentID:      9993,
		ownerID:      1008,
		clientID:     "codex-session-result",
		capabilities: []string{"local_action_v1"},
		localActions: []string{"session_control"},
		send:         make(chan []byte, 2),
	}
	mgr.putConnForTest(conn)
	mgr.storePendingLocalAction(&pendingLocalAction{
		actionID:        "act-codex-session-ok",
		kind:            "session_control",
		agentID:         conn.agentID,
		ownerID:         conn.ownerID,
		sessionID:       "sess-codex-session-ok",
		quotedMessageID: 18889990780,
		actionType:      "session_control",
		referenceID:     "open",
	})

	mgr.handleLocalActionResult(conn, makePacket(t, protocol.CmdLocalActionResult, 1, protocol.LocalActionResultPayload{
		ActionID: "act-codex-session-ok",
		Status:   "ok",
		Result: map[string]any{
			"domain":  "session_control",
			"verb":    "open",
			"outcome": "opened",
			"binding": map[string]any{
				"aibotSessionId": "sess-codex-session-ok",
				"codexThreadId":  "codex-thread-xyz",
				"cwd":            "/workspace/codex-project",
				"workerStatus":   "ready",
			},
		},
	}))

	if len(sendHandler.calls) != 1 {
		t.Fatalf("send handler calls=%d want=1", len(sendHandler.calls))
	}
	if got := sendHandler.calls[0].Content; !strings.Contains(got, "grix://card/agent_status") {
		t.Fatalf("reply content=%q should contain agent_status card", got)
	}
}

func TestHandleLocalActionResult_CodexContextActionsPersistToolbarMeta(t *testing.T) {
	testDB := testutil.NewTestDB()
	defer testDB.Close()

	originalDB := appstore.DB
	appstore.DB = testDB.DB
	t.Cleanup(func() {
		appstore.DB = originalDB
	})

	sendHandler := &mockSendMessageHandler{}
	mgr := NewManager("", 30*time.Second, sendHandler.handle, nil, nil, nil)
	defer mgr.Shutdown()
	conn := &agentConn{
		agentID:      9990,
		ownerID:      1011,
		clientID:     "codex-context-result",
		adapterID:    "codex/base",
		capabilities: []string{"local_action_v1"},
		localActions: []string{"get_context", "set_model", "set_mode"},
		send:         make(chan []byte, 2),
	}
	mgr.putConnForTest(conn)

	mgr.storePendingLocalAction(&pendingLocalAction{
		actionID:   "act-codex-context",
		kind:       "get_context",
		agentID:    conn.agentID,
		ownerID:    conn.ownerID,
		sessionID:  "sess-codex-context",
		actionType: "get_context",
	})

	mgr.handleLocalActionResult(conn, makePacket(t, protocol.CmdLocalActionResult, 1, protocol.LocalActionResultPayload{
		ActionID: "act-codex-context",
		Status:   "ok",
		Result: map[string]any{
			"binding": map[string]any{
				"aibotSessionId": "sess-codex-context",
				"codexThreadId":  "codex-thread-1",
				"cwd":            "/workspace/codex-project",
				"workerStatus":   "ready",
			},
			"session_context": map[string]any{
				"modelId":         "gpt-5.4",
				"modeId":          "plan",
				"reasoningEffort": "medium",
				"approvalPolicy":  "never",
				"sandboxMode":     "workspace-write",
			},
			"available_models": []any{
				map[string]any{"id": "gpt-5.4", "display_name": "gpt-5.4"},
				map[string]any{"id": "gpt-5.4-mini", "display_name": "GPT-5.4-Mini"},
			},
		},
	}))

	if len(sendHandler.calls) != 0 {
		t.Fatalf("send handler calls=%d want=0", len(sendHandler.calls))
	}

	record, ok, err := toolstore.LoadBinding(context.Background(), conn.agentID, "sess-codex-context")
	if err != nil {
		t.Fatalf("LoadBinding() error = %v", err)
	}
	if !ok {
		t.Fatal("expected toolbar binding to be persisted")
	}
	if record.BindingID != "codex-thread-1" {
		t.Fatalf("binding_id=%q want=codex-thread-1", record.BindingID)
	}
	if record.Cwd != "/workspace/codex-project" {
		t.Fatalf("cwd=%q want=/workspace/codex-project", record.Cwd)
	}
	if got := record.Meta["model_id"]; got != "gpt-5.4" {
		t.Fatalf("model_id=%v want=gpt-5.4", got)
	}
	if got := record.Meta["mode_id"]; got != "plan" {
		t.Fatalf("mode_id=%v want=plan", got)
	}
	if got := record.Meta["approval_policy"]; got != "never" {
		t.Fatalf("approval_policy=%v want=never", got)
	}
	if got := record.Meta["sandbox_mode"]; got != "workspace-write" {
		t.Fatalf("sandbox_mode=%v want=workspace-write", got)
	}
	models, ok := record.Meta["available_models"].([]any)
	if !ok || len(models) != 2 {
		t.Fatalf("available_models=%#v want len=2", record.Meta["available_models"])
	}
}

func TestHandleLocalActionResult_CodexGetContextRefreshesToolbarSnapshot(t *testing.T) {
	testDB := testutil.NewTestDB()
	defer testDB.Close()
	mockRedis := testutil.NewMockRedis()
	defer mockRedis.Close()

	originalDB := appstore.DB
	originalRDB := appstore.RDB
	appstore.DB = testDB.DB
	appstore.RDB = mockRedis
	t.Cleanup(func() {
		appstore.DB = originalDB
		appstore.RDB = originalRDB
		agenttoolbar.SetGlobal(nil)
	})

	const (
		ownerID   int64 = 1013
		agentID   int64 = 9992
		sessionID       = "sess-codex-context-refresh"
	)

	now := time.Now()
	if err := appstore.DB.Create(&model.Session{
		SessionID:        sessionID,
		OwnerID:          ownerID,
		SessionType:      model.SessionTypeDirect,
		ModerationStatus: model.SessionModerationStatusActive,
	}).Error; err != nil {
		t.Fatalf("create session: %v", err)
	}
	if err := appstore.DB.Create(&model.SessionMember{
		SessionID:    sessionID,
		MemberID:     ownerID,
		MemberType:   1,
		LastActiveAt: now,
		JoinedAt:     now,
	}).Error; err != nil {
		t.Fatalf("create human member: %v", err)
	}
	if err := appstore.DB.Create(&model.SessionMember{
		SessionID:    sessionID,
		MemberID:     agentID,
		MemberType:   2,
		LastActiveAt: now,
		JoinedAt:     now,
	}).Error; err != nil {
		t.Fatalf("create agent member: %v", err)
	}
	if err := appstore.DB.Create(&model.Agent{
		ID:              agentID,
		AgentName:       "Codex",
		OwnerID:         ownerID,
		ProviderType:    model.AgentProviderAPI,
		AgentClientType: model.AgentClientTypeCodex,
		Status:          model.AgentStatusActive,
	}).Error; err != nil {
		t.Fatalf("create agent: %v", err)
	}
	if err := toolruntime.StoreProfile(context.Background(), toolruntime.Profile{
		AgentID:      agentID,
		OwnerID:      ownerID,
		ClientType:   model.AgentClientTypeCodex,
		LocalActions: []string{"get_context", "set_model", "set_mode", "thread_compact"},
		Online:       true,
	}, 0); err != nil {
		t.Fatalf("store runtime profile: %v", err)
	}

	fanout := &toolbarFanoutRecorder{}
	agenttoolbar.SetGlobal(agenttoolbar.NewService(agenttoolbar.Dependencies{
		Fanout:   fanout.handle,
		Executor: noopToolbarExecutor{},
	}))

	sendHandler := &mockSendMessageHandler{}
	mgr := NewManager("", 30*time.Second, sendHandler.handle, nil, nil, nil)
	defer mgr.Shutdown()
	conn := &agentConn{
		agentID:      agentID,
		ownerID:      ownerID,
		clientID:     "codex-context-refresh",
		adapterID:    "codex/base",
		capabilities: []string{"local_action_v1"},
		localActions: []string{"get_context", "set_model", "set_mode"},
		send:         make(chan []byte, 2),
	}
	mgr.putConnForTest(conn)
	mgr.storePendingLocalAction(&pendingLocalAction{
		actionID:   "act-codex-context-refresh",
		kind:       "get_context",
		agentID:    conn.agentID,
		ownerID:    conn.ownerID,
		sessionID:  sessionID,
		actionType: "get_context",
	})

	mgr.handleLocalActionResult(conn, makePacket(t, protocol.CmdLocalActionResult, 1, protocol.LocalActionResultPayload{
		ActionID: "act-codex-context-refresh",
		Status:   "ok",
		Result: map[string]any{
			"binding": map[string]any{
				"codexThreadId": "codex-thread-refresh",
				"cwd":           "/workspace/codex-refresh",
				"workerStatus":  "ready",
			},
			"session_context": map[string]any{
				"modelId": "gpt-5.4",
				"modeId":  "default",
			},
			"available_models": []any{
				map[string]any{"id": "gpt-5.4", "displayName": "GPT-5.4"},
				map[string]any{"id": "gpt-5.4-mini", "displayName": "GPT-5.4 Mini"},
			},
		},
	}))

	if len(fanout.calls) == 0 {
		t.Fatal("expected toolbar refresh fanout")
	}
	last := fanout.calls[len(fanout.calls)-1]
	if last.cmd != protocol.CmdAgentToolbarSync {
		t.Fatalf("fanout cmd=%q want=%q", last.cmd, protocol.CmdAgentToolbarSync)
	}
	if last.ownerID != ownerID {
		t.Fatalf("fanout owner=%d want=%d", last.ownerID, ownerID)
	}

	raw, err := json.Marshal(last.payload)
	if err != nil {
		t.Fatalf("marshal fanout payload: %v", err)
	}
	var payloadMap map[string]any
	if err := json.Unmarshal(raw, &payloadMap); err != nil {
		t.Fatalf("decode fanout payload: %v", err)
	}
	if got := payloadMap["toolbar_id"]; got != "agent-toolbar:codex:v1" {
		t.Fatalf("toolbar_id=%v want agent-toolbar:codex:v1", got)
	}
	rawItems, _ := payloadMap["items"].([]any)
	if len(rawItems) == 0 {
		t.Fatal("expected toolbar items in sync payload")
	}
	var modelItem map[string]any
	for _, rawItem := range rawItems {
		item, _ := rawItem.(map[string]any)
		if item["item_id"] == "select_model" {
			modelItem = item
			break
		}
	}
	if modelItem == nil {
		t.Fatal("select_model item missing from sync payload")
	}
	if got := modelItem["label"]; got != "GPT-5.4" {
		t.Fatalf("model label=%v want GPT-5.4", got)
	}
	if got := modelItem["disabled"]; got != false {
		t.Fatalf("model disabled=%v want false", got)
	}
}

func TestHandleLocalActionResult_CodexSelectionFallbackPersistsToolbarMeta(t *testing.T) {
	testDB := testutil.NewTestDB()
	defer testDB.Close()

	originalDB := appstore.DB
	appstore.DB = testDB.DB
	t.Cleanup(func() {
		appstore.DB = originalDB
		agenttoolbar.SetGlobal(nil)
	})

	sendHandler := &mockSendMessageHandler{}
	mgr := NewManager("", 30*time.Second, sendHandler.handle, nil, nil, nil)
	defer mgr.Shutdown()
	conn := &agentConn{
		agentID:      9993,
		ownerID:      1014,
		clientID:     "codex-selection-fallback",
		adapterID:    "codex/base",
		capabilities: []string{"local_action_v1"},
		localActions: []string{"set_model", "set_mode", "set_reasoning_effort"},
		send:         make(chan []byte, 4),
	}
	mgr.putConnForTest(conn)

	if err := toolstore.UpsertBinding(context.Background(), toolstore.BindingRecord{
		AgentID:      conn.agentID,
		SessionID:    "sess-codex-selection-fallback",
		ProviderKey:  "codex",
		Cwd:          "/workspace/codex-selection",
		WorkerStatus: "ready",
		Meta: map[string]any{
			"available_models": []any{
				map[string]any{"id": "gpt-5.4", "displayName": "GPT-5.4"},
				map[string]any{"id": "gpt-5.4-mini", "displayName": "GPT-5.4 Mini"},
			},
		},
	}); err != nil {
		t.Fatalf("seed binding: %v", err)
	}

	mgr.storePendingLocalAction(&pendingLocalAction{
		actionID:    "act-codex-set-model",
		kind:        "set_model",
		agentID:     conn.agentID,
		ownerID:     conn.ownerID,
		sessionID:   "sess-codex-selection-fallback",
		referenceID: "gpt-5.4-mini",
	})
	mgr.handleLocalActionResult(conn, makePacket(t, protocol.CmdLocalActionResult, 1, protocol.LocalActionResultPayload{
		ActionID: "act-codex-set-model",
		Status:   "ok",
	}))

	mgr.storePendingLocalAction(&pendingLocalAction{
		actionID:    "act-codex-set-mode",
		kind:        "set_mode",
		agentID:     conn.agentID,
		ownerID:     conn.ownerID,
		sessionID:   "sess-codex-selection-fallback",
		referenceID: "plan",
	})
	mgr.handleLocalActionResult(conn, makePacket(t, protocol.CmdLocalActionResult, 2, protocol.LocalActionResultPayload{
		ActionID: "act-codex-set-mode",
		Status:   "ok",
	}))

	mgr.storePendingLocalAction(&pendingLocalAction{
		actionID:    "act-codex-set-effort",
		kind:        "set_reasoning_effort",
		agentID:     conn.agentID,
		ownerID:     conn.ownerID,
		sessionID:   "sess-codex-selection-fallback",
		referenceID: "high",
	})
	mgr.handleLocalActionResult(conn, makePacket(t, protocol.CmdLocalActionResult, 3, protocol.LocalActionResultPayload{
		ActionID: "act-codex-set-effort",
		Status:   "ok",
	}))

	record, ok, err := toolstore.LoadBinding(context.Background(), conn.agentID, "sess-codex-selection-fallback")
	if err != nil {
		t.Fatalf("LoadBinding() error = %v", err)
	}
	if !ok {
		t.Fatal("expected toolbar binding to be persisted")
	}
	if got := record.Meta["model_id"]; got != "gpt-5.4-mini" {
		t.Fatalf("model_id=%v want=gpt-5.4-mini", got)
	}
	if got := record.Meta["mode_id"]; got != "plan" {
		t.Fatalf("mode_id=%v want=plan", got)
	}
	if got := record.Meta["reasoning_effort"]; got != "high" {
		t.Fatalf("reasoning_effort=%v want=high", got)
	}
	models, ok := record.Meta["available_models"].([]any)
	if !ok || len(models) != 2 {
		t.Fatalf("available_models=%#v want len=2", record.Meta["available_models"])
	}
}

func TestHandleUpdateBindingCard_PersistsToolbarMeta(t *testing.T) {
	testDB := testutil.NewTestDB()
	defer testDB.Close()

	originalDB := appstore.DB
	appstore.DB = testDB.DB
	t.Cleanup(func() {
		appstore.DB = originalDB
	})

	sendHandler := &mockSendMessageHandler{}
	mgr := NewManager("", 30*time.Second, sendHandler.handle, nil, nil, nil)
	defer mgr.Shutdown()
	conn := &agentConn{
		agentID:   9991,
		ownerID:   1012,
		clientID:  "codex-binding-update",
		adapterID: "codex/base",
		send:      make(chan []byte, 2),
	}

	mgr.handleUpdateBindingCard(conn, makePacket(t, "update_binding_card", 1, map[string]any{
		"session_id":    "sess-codex-binding",
		"worker_status": "ready",
		"cwd":           "/workspace/codex-binding",
		"meta": map[string]any{
			"model_id": "gpt-5.5",
			"mode_id":  "default",
			"available_models": []any{
				map[string]any{"id": "gpt-5.5", "displayName": "GPT-5.5"},
				map[string]any{"id": "gpt-5.4", "displayName": "GPT-5.4"},
			},
		},
	}))

	record, ok, err := toolstore.LoadBinding(context.Background(), conn.agentID, "sess-codex-binding")
	if err != nil {
		t.Fatalf("LoadBinding() error = %v", err)
	}
	if !ok {
		t.Fatal("expected toolbar binding to be persisted")
	}
	if record.Cwd != "/workspace/codex-binding" {
		t.Fatalf("cwd=%q want=/workspace/codex-binding", record.Cwd)
	}
	if record.WorkerStatus != "ready" {
		t.Fatalf("worker_status=%q want=ready", record.WorkerStatus)
	}
	if got := record.Meta["model_id"]; got != "gpt-5.5" {
		t.Fatalf("model_id=%v want=gpt-5.5", got)
	}
	models, ok := record.Meta["available_models"].([]any)
	if !ok || len(models) != 2 {
		t.Fatalf("available_models=%#v want len=2", record.Meta["available_models"])
	}
}

func TestHandleUpdateBindingCard_HermesPersistsMetadataWithoutChatCard(t *testing.T) {
	testDB := testutil.NewTestDB()
	defer testDB.Close()

	originalDB := appstore.DB
	appstore.DB = testDB.DB
	t.Cleanup(func() {
		appstore.DB = originalDB
	})

	sendHandler := &mockSendMessageHandler{}
	mgr := NewManager("", 30*time.Second, sendHandler.handle, nil, nil, nil)
	defer mgr.Shutdown()
	conn := &agentConn{
		agentID:    9992,
		ownerID:    1013,
		clientID:   "hermes-binding-update",
		clientType: model.AgentClientTypeHermes,
		adapterID:  "hermes/base",
		send:       make(chan []byte, 2),
	}

	mgr.handleUpdateBindingCard(conn, makePacket(t, protocol.CmdUpdateBindingCard, 1, map[string]any{
		"session_id":    "sess-hermes-binding",
		"worker_status": "ready",
		"meta": map[string]any{
			"model_id": "deepseek-v3.2",
			"available_models": []any{
				map[string]any{"id": "deepseek-v3.2", "displayName": "DeepSeek V3.2"},
			},
		},
	}))

	record, ok, err := toolstore.LoadBinding(context.Background(), conn.agentID, "sess-hermes-binding")
	if err != nil {
		t.Fatalf("LoadBinding() error = %v", err)
	}
	if !ok || record.Meta["model_id"] != "deepseek-v3.2" {
		t.Fatalf("binding=%+v found=%v", record, ok)
	}
	if len(sendHandler.calls) != 0 {
		t.Fatalf("Hermes metadata update created %d chat cards", len(sendHandler.calls))
	}

	select {
	case raw := <-conn.send:
		var ack protocol.Packet
		if err := json.Unmarshal(raw, &ack); err != nil {
			t.Fatalf("unmarshal ack: %v", err)
		}
		if ack.Cmd != protocol.CmdSendAck {
			t.Fatalf("ack cmd=%q want=%q", ack.Cmd, protocol.CmdSendAck)
		}
		var payload map[string]any
		if err := json.Unmarshal(ack.Payload, &payload); err != nil {
			t.Fatalf("unmarshal ack payload: %v", err)
		}
		if payload["metadata_only"] != true {
			t.Fatalf("ack payload=%#v", payload)
		}
	default:
		t.Fatal("expected send_ack")
	}
}

func TestHandleLocalActionResult_GetRateLimitsPersistsContextWindowWithoutRateLimits(t *testing.T) {
	testDB := testutil.NewTestDB()
	defer testDB.Close()

	originalDB := appstore.DB
	appstore.DB = testDB.DB
	t.Cleanup(func() {
		appstore.DB = originalDB
	})

	mgr := NewManager("", 30*time.Second, nil, nil, nil, nil)
	defer mgr.Shutdown()
	conn := &agentConn{
		agentID:      9999,
		ownerID:      1019,
		clientID:     "codex-rate-limits-context-only",
		adapterID:    "codex/base",
		capabilities: []string{"local_action_v1"},
		localActions: []string{"get_rate_limits"},
		send:         make(chan []byte, 2),
	}
	mgr.putConnForTest(conn)
	mgr.storePendingLocalAction(&pendingLocalAction{
		actionID:   "act-codex-rate-context-only",
		kind:       "get_rate_limits",
		agentID:    conn.agentID,
		ownerID:    conn.ownerID,
		sessionID:  "sess-codex-rate-context-only",
		actionType: "get_rate_limits",
	})

	mgr.handleLocalActionResult(conn, makePacket(t, protocol.CmdLocalActionResult, 1, protocol.LocalActionResultPayload{
		ActionID: "act-codex-rate-context-only",
		Status:   "ok",
		Result: map[string]any{
			"adapterType": "codex",
			"contextWindow": map[string]any{
				"sizeTokens":          float64(258400),
				"usedTokens":          float64(10000),
				"remainingTokens":     float64(248400),
				"usedPercentage":      float64(3.9),
				"remainingPercentage": float64(96.1),
				"source":              "codex:model_context_window",
			},
		},
	}))

	record, ok, err := toolstore.LoadBinding(context.Background(), conn.agentID, "sess-codex-rate-context-only")
	if err != nil {
		t.Fatalf("LoadBinding() error = %v", err)
	}
	if !ok {
		t.Fatal("expected toolbar binding to be persisted")
	}
	ctxRaw, ok := record.Meta["context_window"].(map[string]any)
	if !ok {
		t.Fatalf("context_window=%#v want map", record.Meta["context_window"])
	}
	if got := ctxRaw["usedPercentage"]; got != float64(3.9) {
		t.Fatalf("context_window.usedPercentage=%v want=3.9", got)
	}
}

func TestHandleLocalActionResult_QwenSessionControlSuccessSendsAgentStatus(t *testing.T) {
	sendHandler := &mockSendMessageHandler{
		result: &SendMessageResult{
			MsgID:     92015,
			InboxSeq:  6,
			CreatedAt: time.Now().UnixMilli(),
		},
	}
	mgr := NewManager("", 30*time.Second, sendHandler.handle, nil, nil, nil)
	defer mgr.Shutdown()
	conn := &agentConn{
		agentID:      9991,
		ownerID:      1010,
		clientID:     "qwen-session-result",
		capabilities: []string{"local_action_v1"},
		localActions: []string{"session_control"},
		send:         make(chan []byte, 2),
	}
	mgr.putConnForTest(conn)
	mgr.storePendingLocalAction(&pendingLocalAction{
		actionID:        "act-qwen-session-ok",
		kind:            "session_control",
		agentID:         conn.agentID,
		ownerID:         conn.ownerID,
		sessionID:       "sess-qwen-session-ok",
		quotedMessageID: 18889990782,
		actionType:      "session_control",
		referenceID:     "open",
	})

	mgr.handleLocalActionResult(conn, makePacket(t, protocol.CmdLocalActionResult, 1, protocol.LocalActionResultPayload{
		ActionID: "act-qwen-session-ok",
		Status:   "ok",
		Result: map[string]any{
			"domain":  "session_control",
			"verb":    "open",
			"outcome": "opened",
			"binding": map[string]any{
				"aibot_session_id": "sess-qwen-session-ok",
				"qwen_session_id":  "qwen-session-xyz",
				"cwd":              "/workspace/qwen-project",
				"worker_status":    "ready",
			},
		},
	}))

	if len(sendHandler.calls) != 1 {
		t.Fatalf("send handler calls=%d want=1", len(sendHandler.calls))
	}
	if got := sendHandler.calls[0].Content; !strings.Contains(got, "grix://card/agent_status") {
		t.Fatalf("reply content=%q should contain agent_status card", got)
	}
}

func TestHandleLocalActionResult_CodexSessionControlInvalidCwdSendsOpenSessionCard(t *testing.T) {
	sendHandler := &mockSendMessageHandler{
		result: &SendMessageResult{
			MsgID:     92014,
			InboxSeq:  5,
			CreatedAt: time.Now().UnixMilli(),
		},
	}
	mgr := NewManager("", 30*time.Second, sendHandler.handle, nil, nil, nil)
	defer mgr.Shutdown()
	conn := &agentConn{
		agentID:      9992,
		ownerID:      1009,
		isPrimary:    true,
		clientID:     "codex-session-invalid-cwd",
		adapterID:    "codex/base",
		capabilities: []string{"local_action_v1"},
		localActions: []string{"session_control"},
		send:         make(chan []byte, 2),
	}
	mgr.putConnForTest(conn)
	mgr.storePendingLocalAction(&pendingLocalAction{
		actionID:        "act-codex-session-invalid",
		kind:            "session_control",
		agentID:         conn.agentID,
		ownerID:         conn.ownerID,
		sessionID:       "sess-codex-session-invalid",
		quotedMessageID: 18889990781,
		actionType:      "session_control",
		referenceID:     "open",
		submittedPath:   "/workspace/missing",
	})

	mgr.handleLocalActionResult(conn, makePacket(t, protocol.CmdLocalActionResult, 1, protocol.LocalActionResultPayload{
		ActionID:  "act-codex-session-invalid",
		Status:    "failed",
		ErrorCode: "session_invalid_cwd",
		ErrorMsg:  "The workspace path does not exist.",
	}))

	if len(sendHandler.calls) != 1 {
		t.Fatalf("send handler calls=%d want=1", len(sendHandler.calls))
	}
	if got := sendHandler.calls[0].Content; !strings.Contains(got, "grix://card/agent_open_session") {
		t.Fatalf("reply content=%q should contain agent_open_session card", got)
	}

	var extra map[string]any
	if err := json.Unmarshal(sendHandler.calls[0].Extra, &extra); err != nil {
		t.Fatalf("unmarshal extra: %v", err)
	}
	bizCard, _ := extra["biz_card"].(map[string]any)
	if bizCard == nil {
		t.Fatalf("extra=%#v missing biz_card", extra)
	}
	if got := bizCard["type"]; got != "agent_open_session" {
		t.Fatalf("biz_card.type=%v want=agent_open_session", got)
	}
	payload, _ := bizCard["payload"].(map[string]any)
	if payload == nil {
		t.Fatalf("biz_card=%#v missing payload", bizCard)
	}
	if got := payload["summary_text"]; got != "Codex workspace path is invalid." {
		t.Fatalf("summary_text=%v want=%q", got, "Codex workspace path is invalid.")
	}
	if got := payload["initial_cwd"]; got != "/workspace/missing" {
		t.Fatalf("initial_cwd=%v want=/workspace/missing", got)
	}
	if got := sendHandler.calls[0].VisibleTo; len(got) != 1 || got[0] != conn.ownerID {
		t.Fatalf("reply visible_to=%v want=[%d]", got, conn.ownerID)
	}
}

func TestHandleLocalActionResult_GetSessionUsageSuccessSendsAgentStatus(t *testing.T) {
	sendHandler := &mockSendMessageHandler{
		result: &SendMessageResult{
			MsgID:     92016,
			InboxSeq:  7,
			CreatedAt: time.Now().UnixMilli(),
		},
	}
	mgr := NewManager("", 30*time.Second, sendHandler.handle, nil, nil, nil)
	defer mgr.Shutdown()
	conn := &agentConn{
		agentID:      9989,
		ownerID:      1013,
		clientID:     "usage-result",
		adapterID:    "codex/base",
		capabilities: []string{"local_action_v1"},
		localActions: []string{"get_session_usage"},
		send:         make(chan []byte, 2),
	}
	mgr.putConnForTest(conn)
	mgr.storePendingLocalAction(&pendingLocalAction{
		actionID:        "act-usage-ok",
		kind:            "get_session_usage",
		agentID:         conn.agentID,
		ownerID:         conn.ownerID,
		sessionID:       "sess-usage-ok",
		quotedMessageID: 18889990783,
		actionType:      "get_session_usage",
	})

	mgr.handleLocalActionResult(conn, makePacket(t, protocol.CmdLocalActionResult, 1, protocol.LocalActionResultPayload{
		ActionID: "act-usage-ok",
		Status:   "ok",
		Result: map[string]any{
			"sessionId":   "codex-thread-usage",
			"adapterType": "codex",
			"turns":       4,
			"sampledAt":   "2026-05-14T09:10:11.000Z",
			"total": map[string]any{
				"inputTokens":              1200,
				"outputTokens":             340,
				"cacheReadInputTokens":     55,
				"cacheCreationInputTokens": 20,
			},
			"models": []any{
				map[string]any{
					"model": "gpt-5.5",
					"turns": 3,
					"total": map[string]any{
						"inputTokens":              1000,
						"outputTokens":             280,
						"cacheReadInputTokens":     40,
						"cacheCreationInputTokens": 10,
					},
				},
				map[string]any{
					"model": "gpt-5.4-mini",
					"turns": 1,
					"total": map[string]any{
						"inputTokens":              200,
						"outputTokens":             60,
						"cacheReadInputTokens":     15,
						"cacheCreationInputTokens": 10,
					},
				},
			},
		},
	}))

	if len(sendHandler.calls) != 1 {
		t.Fatalf("send handler calls=%d want=1", len(sendHandler.calls))
	}
	cardType, payload := mustParseLocalCardPayload(t, sendHandler.calls[0].Content)
	if cardType != "agent_status" {
		t.Fatalf("card_type=%q want=agent_status", cardType)
	}
	if got := strings.TrimSpace(payload["status"]); got != "success" {
		t.Fatalf("status=%q want=success payload=%v", got, payload)
	}
	if got := strings.TrimSpace(payload["category"]); got != "session" {
		t.Fatalf("category=%q want=session payload=%v", got, payload)
	}
	if got := strings.TrimSpace(payload["reference_id"]); got != "codex-thread-usage" {
		t.Fatalf("reference_id=%q want=codex-thread-usage payload=%v", got, payload)
	}
	if got := strings.TrimSpace(payload["summary"]); !strings.Contains(got, "turns") || !strings.Contains(got, "tokens") {
		t.Fatalf("summary=%q should contain turns and tokens payload=%v", got, payload)
	}
	// usage_data 应包含结构化用量数据
	if got := strings.TrimSpace(payload["usage_data"]); got == "" {
		t.Fatalf("usage_data should be present payload=%v", payload)
	}
	if !strings.Contains(payload["usage_data"], "session_usage") {
		t.Fatalf("usage_data=%q should contain session_usage type", payload["usage_data"])
	}
	if !strings.Contains(payload["usage_data"], "gpt-5.5") {
		t.Fatalf("usage_data=%q should contain model name", payload["usage_data"])
	}
}

func TestHandleLocalActionResult_GetSessionUsageFailuresSendAgentStatus(t *testing.T) {
	testCases := []struct {
		name               string
		status             string
		errorCode          string
		errorMsg           string
		wantStatus         string
		wantSummaryContain string
		wantDetailContain  string
		wantDetailExcludes string
	}{
		{
			name:               "no_binding",
			status:             "failed",
			errorCode:          "no_binding",
			errorMsg:           "No Codex thread binding found",
			wantStatus:         "warning",
			wantSummaryContain: "尚未绑定",
			wantDetailContain:  "No Codex thread binding found",
		},
		{
			name:               "usage_not_found",
			status:             "failed",
			errorCode:          "usage_not_found",
			wantStatus:         "warning",
			wantSummaryContain: "未找到",
			wantDetailContain:  "token 用量记录",
		},
		{
			name:               "unsupported",
			status:             "unsupported",
			errorMsg:           "get_session_usage not supported",
			wantStatus:         "warning",
			wantSummaryContain: "暂不支持",
			wantDetailContain:  "not supported",
		},
		{
			name:               "runtime_error",
			status:             "failed",
			errorCode:          "runtime_error",
			errorMsg:           "parser crashed",
			wantStatus:         "error",
			wantSummaryContain: "用量查询失败",
			wantDetailContain:  "parser crashed",
		},
		{
			name:               "timeout",
			status:             "failed",
			errorCode:          "timeout",
			errorMsg:           "forwarded local_action timed out",
			wantStatus:         "warning",
			wantSummaryContain: "用量查询超时",
			wantDetailContain:  "请稍后重试",
			wantDetailExcludes: "forwarded local_action timed out",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			sendHandler := &mockSendMessageHandler{
				result: &SendMessageResult{
					MsgID:     92017,
					InboxSeq:  8,
					CreatedAt: time.Now().UnixMilli(),
				},
			}
			mgr := NewManager("", 30*time.Second, sendHandler.handle, nil, nil, nil)
			defer mgr.Shutdown()
			conn := &agentConn{
				agentID:      9988,
				ownerID:      1014,
				clientID:     "usage-failure-result",
				adapterID:    "codex/base",
				capabilities: []string{"local_action_v1"},
				localActions: []string{"get_session_usage"},
				send:         make(chan []byte, 2),
			}
			mgr.putConnForTest(conn)
			mgr.storePendingLocalAction(&pendingLocalAction{
				actionID:   "act-usage-" + tc.name,
				kind:       "get_session_usage",
				agentID:    conn.agentID,
				ownerID:    conn.ownerID,
				sessionID:  "sess-usage-failed",
				actionType: "get_session_usage",
			})

			mgr.handleLocalActionResult(conn, makePacket(t, protocol.CmdLocalActionResult, 1, protocol.LocalActionResultPayload{
				ActionID:  "act-usage-" + tc.name,
				Status:    tc.status,
				ErrorCode: tc.errorCode,
				ErrorMsg:  tc.errorMsg,
			}))

			if len(sendHandler.calls) != 1 {
				t.Fatalf("send handler calls=%d want=1", len(sendHandler.calls))
			}
			cardType, payload := mustParseLocalCardPayload(t, sendHandler.calls[0].Content)
			if cardType != "agent_status" {
				t.Fatalf("card_type=%q want=agent_status", cardType)
			}
			if got := strings.TrimSpace(payload["status"]); got != tc.wantStatus {
				t.Fatalf("status=%q want=%q payload=%v", got, tc.wantStatus, payload)
			}
			if got := strings.TrimSpace(payload["summary"]); !strings.Contains(got, tc.wantSummaryContain) {
				t.Fatalf("summary=%q should contain %q", got, tc.wantSummaryContain)
			}
			got := strings.TrimSpace(payload["detail_text"])
			if tc.wantDetailContain != "" && !strings.Contains(got, tc.wantDetailContain) {
				t.Fatalf("detail_text=%q should contain %q", got, tc.wantDetailContain)
			}
			if tc.wantDetailExcludes != "" && strings.Contains(got, tc.wantDetailExcludes) {
				t.Fatalf("detail_text=%q should not contain internal diagnostic %q", got, tc.wantDetailExcludes)
			}
		})
	}
}

func TestTimeoutPendingLocalAction_GetSessionUsageSendsWarningCard(t *testing.T) {
	sendHandler := &mockSendMessageHandler{
		result: &SendMessageResult{
			MsgID:     92018,
			InboxSeq:  9,
			CreatedAt: time.Now().UnixMilli(),
		},
	}
	mgr := NewManager("", 30*time.Second, sendHandler.handle, nil, nil, nil)
	defer mgr.Shutdown()
	mgr.storePendingLocalAction(&pendingLocalAction{
		actionID:   "act-usage-timeout",
		kind:       "get_session_usage",
		agentID:    9987,
		ownerID:    1015,
		sessionID:  "sess-usage-timeout",
		actionType: "get_session_usage",
	})

	mgr.timeoutPendingLocalAction("act-usage-timeout")

	if len(sendHandler.calls) != 1 {
		t.Fatalf("send handler calls=%d want=1", len(sendHandler.calls))
	}
	cardType, payload := mustParseLocalCardPayload(t, sendHandler.calls[0].Content)
	if cardType != "agent_status" {
		t.Fatalf("card_type=%q want=agent_status", cardType)
	}
	if got := strings.TrimSpace(payload["status"]); got != "warning" {
		t.Fatalf("status=%q want=warning payload=%v", got, payload)
	}
	if got := strings.TrimSpace(payload["summary"]); !strings.Contains(got, "超时") {
		t.Fatalf("summary=%q should mention timeout", got)
	}
}

// --- Protocol contract guard tests ---

func TestSessionControlProtocolContract_OutboundPayload(t *testing.T) {
	testCases := []struct {
		name     string
		content  string
		wantVerb string
		wantCwd  string
	}{
		{name: "open session submit uri", content: "grix://open/session?cwd=%2Fworkspace%2Fcard", wantVerb: "open", wantCwd: "/workspace/card"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			dummySendFn := func(_ context.Context, _ SendMessageReq) (*SendMessageResult, error) {
				return &SendMessageResult{MsgID: 1}, nil
			}
			mgr := NewManager("", 30*time.Second, dummySendFn, nil, nil, nil)
			defer mgr.Shutdown()
			conn := &agentConn{
				agentID:      88001,
				ownerID:      11001,
				clientID:     "contract-outbound",
				adapterID:    "test/base",
				capabilities: []string{"local_action_v1"},
				localActions: []string{"session_control"},
				send:         make(chan []byte, 4),
			}
			mgr.putConnForTest(conn)

			evt := DelegateEventPayload{
				EventID:   "evt-contract-001",
				AgentID:   conn.agentID,
				OwnerID:   conn.ownerID,
				SessionID: "sess-contract-out",
				ThreadID:  "sess-contract-out",
				MsgID:     77001,
				Content:   tc.content,
			}

			handled := mgr.handleSessionControlCommand(evt, sessionControlBridgeConfig{
				actionType: "session_control",
				usage:      sessionControlUsage,
				logLabel:   "contract",
			})
			if !handled {
				t.Fatalf("handleSessionControlCommand returned false for %q", tc.content)
			}

			select {
			case raw := <-conn.send:
				var pkt protocol.Packet
				if err := json.Unmarshal(raw, &pkt); err != nil {
					t.Fatalf("unmarshal outbound packet: %v", err)
				}
				if pkt.Cmd != protocol.CmdLocalAction {
					t.Fatalf("cmd=%q want=%q", pkt.Cmd, protocol.CmdLocalAction)
				}
				var payload protocol.LocalActionPayload
				if err := json.Unmarshal(pkt.Payload, &payload); err != nil {
					t.Fatalf("unmarshal local_action payload: %v", err)
				}

				if payload.ActionType != "session_control" {
					t.Errorf("action_type=%q want=%q", payload.ActionType, "session_control")
				}
				if _, ok := payload.Params["session_id"]; !ok {
					t.Errorf("params missing snake_case key %q, got keys: %v", "session_id", mapKeysSorted(payload.Params))
				}
				if _, ok := payload.Params["verb"]; !ok {
					t.Errorf("params missing snake_case key %q, got keys: %v", "verb", mapKeysSorted(payload.Params))
				}
				if got, _ := payload.Params["session_id"].(string); got != "sess-contract-out" {
					t.Errorf("params.session_id=%q want=%q", got, "sess-contract-out")
				}
				if got, _ := payload.Params["verb"].(string); got != tc.wantVerb {
					t.Errorf("params.verb=%q want=%q", got, tc.wantVerb)
				}
				if tc.wantCwd != "" {
					if got, _ := payload.Params["cwd"].(string); got != tc.wantCwd {
						t.Errorf("params.cwd=%q want=%q", got, tc.wantCwd)
					}
				} else if _, exists := payload.Params["cwd"]; exists {
					t.Errorf("params should not contain cwd for verb=%q", tc.wantVerb)
				}
				if payload.TimeoutMs != 15000 {
					t.Errorf("timeout_ms=%d want=15000", payload.TimeoutMs)
				}
				if payload.ActionID == "" {
					t.Error("action_id is empty")
				}
			default:
				t.Fatal("timed out waiting for outbound packet")
			}
		})
	}
}

func TestSessionControlProtocolContract_InboundResult(t *testing.T) {
	type testCase struct {
		name       string
		adapterID  string
		status     string
		result     map[string]any
		errCode    string
		errMsg     string
		wantCard   string
		wantInCard string
	}

	tests := []testCase{
		{
			name:      "claude ok snake_case binding",
			adapterID: "claude/base",
			status:    "ok",
			result: map[string]any{
				"domain":  "session_control",
				"verb":    "open",
				"outcome": "opened",
				"binding": map[string]any{
					"aibot_session_id":  "sess-claude-ok",
					"claude_session_id": "claude-xyz",
					"cwd":               "/workspace/claude",
					"worker_status":     "ready",
				},
			},
			wantCard:   "agent_status",
			wantInCard: "已绑定 /workspace/claude",
		},
		{
			name:      "codex ok camelCase binding",
			adapterID: "codex/base",
			status:    "ok",
			result: map[string]any{
				"domain":  "session_control",
				"verb":    "open",
				"outcome": "opened",
				"binding": map[string]any{
					"aibotSessionId": "sess-codex-ok",
					"codexThreadId":  "codex-thread-xyz",
					"cwd":            "/workspace/codex",
					"workerStatus":   "ready",
				},
			},
			wantCard:   "agent_status",
			wantInCard: "已绑定 /workspace/codex",
		},
		{
			name:      "gemini ok camelCase binding",
			adapterID: "gemini/base",
			status:    "ok",
			result: map[string]any{
				"domain":  "session_control",
				"verb":    "open",
				"outcome": "opened",
				"binding": map[string]any{
					"aibotSessionId":  "sess-gemini-ok",
					"geminiSessionId": "gemini-xyz",
					"cwd":             "/workspace/gemini",
					"workerStatus":    "ready",
				},
			},
			wantCard:   "agent_status",
			wantInCard: "已绑定 /workspace/gemini",
		},
		{
			name:      "qwen ok snake_case binding",
			adapterID: "qwen/base",
			status:    "ok",
			result: map[string]any{
				"domain":  "session_control",
				"verb":    "open",
				"outcome": "opened",
				"binding": map[string]any{
					"aibot_session_id": "sess-qwen-ok",
					"qwen_session_id":  "qwen-xyz",
					"cwd":              "/workspace/qwen",
					"worker_status":    "ready",
				},
			},
			wantCard:   "agent_status",
			wantInCard: "已绑定 /workspace/qwen",
		},
		{
			name:       "codex failed invalid_cwd produces retry card",
			adapterID:  "codex/base",
			status:     "failed",
			errCode:    "session_invalid_cwd",
			errMsg:     "invalid cwd",
			wantCard:   "agent_open_session",
			wantInCard: "Open Workspace",
		},
		{
			name:       "claude failed generic produces retry open card",
			adapterID:  "claude/base",
			status:     "failed",
			errCode:    "session_runtime_error",
			errMsg:     "something went wrong",
			wantCard:   "agent_open_session",
			wantInCard: "could not be opened",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			sendHandler := &mockSendMessageHandler{
				result: &SendMessageResult{
					MsgID:     99001,
					InboxSeq:  1,
					CreatedAt: time.Now().UnixMilli(),
				},
			}
			mgr := NewManager("", 30*time.Second, sendHandler.handle, nil, nil, nil)
			defer mgr.Shutdown()
			conn := &agentConn{
				agentID:      88002,
				ownerID:      11002,
				clientID:     "contract-inbound",
				adapterID:    tc.adapterID,
				capabilities: []string{"local_action_v1"},
				localActions: []string{"session_control"},
				send:         make(chan []byte, 4),
			}
			mgr.putConnForTest(conn)

			actionID := "act-contract-" + tc.name
			pending := &pendingLocalAction{
				actionID:        actionID,
				kind:            "session_control",
				agentID:         conn.agentID,
				ownerID:         conn.ownerID,
				sessionID:       "sess-contract-in",
				quotedMessageID: 77002,
				actionType:      "session_control",
				referenceID:     "open",
				submittedPath:   "/workspace/invalid",
			}
			mgr.storePendingLocalAction(pending)

			resultPayload := protocol.LocalActionResultPayload{
				ActionID:  actionID,
				Status:    tc.status,
				Result:    tc.result,
				ErrorCode: tc.errCode,
				ErrorMsg:  tc.errMsg,
			}

			mgr.handleLocalActionResult(conn, makePacket(t, protocol.CmdLocalActionResult, 1, resultPayload))

			if len(sendHandler.calls) != 1 {
				t.Fatalf("send handler calls=%d want=1", len(sendHandler.calls))
			}
			got := sendHandler.calls[0].Content
			if !strings.Contains(got, "grix://card/"+tc.wantCard) {
				t.Errorf("reply content=%q should contain grix://card/%s", got, tc.wantCard)
			}
			if !strings.Contains(got, tc.wantInCard) {
				t.Errorf("reply content=%q should contain %q", got, tc.wantInCard)
			}
		})
	}
}

// 快捷绑定（无卡片提交）场景：插件先上报 update_binding_card 生成了绑定状态卡，
// 随后 session_control 结果回复必须原地编辑该卡，而不是再新发一条重复卡片。
func TestHandleLocalActionResult_SessionControlEditsExistingBindingCard(t *testing.T) {
	mockRedis := testutil.NewMockRedis()
	defer mockRedis.Close()
	originalRDB := appstore.RDB
	appstore.RDB = mockRedis
	t.Cleanup(func() { appstore.RDB = originalRDB })

	sendHandler := &mockSendMessageHandler{
		result: &SendMessageResult{
			MsgID:     99010,
			InboxSeq:  1,
			CreatedAt: time.Now().UnixMilli(),
		},
	}
	mgr := NewManager("", 30*time.Second, sendHandler.handle, nil, nil, nil)
	defer mgr.Shutdown()
	var edits []EditMsgPayload
	mgr.SetEditMsgHandler(func(_ context.Context, _, _ int64, payload EditMsgPayload) error {
		edits = append(edits, payload)
		return nil
	})
	conn := &agentConn{
		agentID:      88003,
		ownerID:      11003,
		clientID:     "dedupe-binding-card",
		adapterID:    "claude/base",
		capabilities: []string{"local_action_v1"},
		localActions: []string{"session_control"},
		send:         make(chan []byte, 4),
	}
	mgr.putConnForTest(conn)

	const sessionID = "sess-dedupe-binding"
	const bindingCardMsgID int64 = 88011
	// 模拟插件已通过 update_binding_card 发出绑定状态卡。
	saveBindingCardMsgID(context.Background(), conn.agentID, sessionID, bindingCardMsgID)

	actionID := "act-dedupe-binding"
	pending := &pendingLocalAction{
		actionID:   actionID,
		kind:       "session_control",
		agentID:    conn.agentID,
		ownerID:    conn.ownerID,
		sessionID:  sessionID,
		actionType: "session_control",
		// 快捷绑定提交时还没有绑定卡消息，快照为 0。
		bindingCardMsgID: 0,
		referenceID:      "open",
	}
	mgr.storePendingLocalAction(pending)

	mgr.handleLocalActionResult(conn, makePacket(t, protocol.CmdLocalActionResult, 1, protocol.LocalActionResultPayload{
		ActionID: actionID,
		Status:   "ok",
		Result: map[string]any{
			"domain":  "session_control",
			"verb":    "open",
			"outcome": "opened",
			"binding": map[string]any{
				"aibot_session_id":  sessionID,
				"claude_session_id": "claude-dedupe",
				"cwd":               "/workspace/dedupe",
				"worker_status":     "ready",
			},
		},
	}))

	if len(sendHandler.calls) != 0 {
		t.Fatalf("should edit existing binding card instead of sending new message, sends=%d", len(sendHandler.calls))
	}
	if len(edits) != 1 {
		t.Fatalf("edit calls=%d want=1", len(edits))
	}
	if edits[0].MsgID != bindingCardMsgID {
		t.Fatalf("edited msg_id=%d want=%d", edits[0].MsgID, bindingCardMsgID)
	}
	if !strings.Contains(edits[0].Content, "已绑定 /workspace/dedupe") {
		t.Fatalf("edited content=%q should contain bound summary", edits[0].Content)
	}
}

func TestSessionControlProtocolContract_InboundWorkerStatusFieldNames(t *testing.T) {
	testCases := []struct {
		name    string
		binding map[string]any
	}{
		{
			name: "snake_case worker_status",
			binding: map[string]any{
				"aibot_session_id":  "sess-snake",
				"claude_session_id": "claude-1",
				"cwd":               "/ws",
				"worker_status":     "ready",
			},
		},
		{
			name: "camelCase workerStatus",
			binding: map[string]any{
				"aibotSessionId": "sess-camel",
				"codexThreadId":  "codex-1",
				"cwd":            "/ws2",
				"workerStatus":   "ready",
			},
		},
		{
			name: "both worker_status and workerStatus",
			binding: map[string]any{
				"aibot_session_id":  "sess-both",
				"claude_session_id": "claude-2",
				"cwd":               "/ws3",
				"worker_status":     "starting",
				"workerStatus":      "ready",
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			sendHandler := &mockSendMessageHandler{
				result: &SendMessageResult{
					MsgID:     99002,
					InboxSeq:  1,
					CreatedAt: time.Now().UnixMilli(),
				},
			}
			mgr := NewManager("", 30*time.Second, sendHandler.handle, nil, nil, nil)
			defer mgr.Shutdown()
			conn := &agentConn{
				agentID:      88003,
				ownerID:      11003,
				clientID:     "contract-worker-status",
				adapterID:    "claude/base",
				capabilities: []string{"local_action_v1"},
				localActions: []string{"session_control"},
				send:         make(chan []byte, 2),
			}
			mgr.putConnForTest(conn)

			actionID := "act-ws-" + tc.name
			pending := &pendingLocalAction{
				actionID:        actionID,
				kind:            "session_control",
				agentID:         conn.agentID,
				ownerID:         conn.ownerID,
				sessionID:       "sess-ws-contract",
				quotedMessageID: 77003,
				actionType:      "session_control",
				referenceID:     "open",
			}
			mgr.storePendingLocalAction(pending)

			mgr.handleLocalActionResult(conn, makePacket(t, protocol.CmdLocalActionResult, 1, protocol.LocalActionResultPayload{
				ActionID: actionID,
				Status:   "ok",
				Result: map[string]any{
					"domain":  "session_control",
					"verb":    "open",
					"outcome": "opened",
					"binding": tc.binding,
				},
			}))

			if len(sendHandler.calls) != 1 {
				t.Fatalf("send handler calls=%d want=1", len(sendHandler.calls))
			}
			got := sendHandler.calls[0].Content
			if !strings.Contains(got, "grix://card/agent_status") {
				t.Errorf("reply content should contain agent_status card, got=%q", got)
			}
		})
	}
}

func TestSessionControlProtocolContract_ProviderBindingIDKeys(t *testing.T) {
	testCases := []struct {
		adapterID string
		wantKeys  []string
	}{
		{adapterID: "claude/base", wantKeys: []string{"claude_session_id", "claudeSessionId"}},
		{adapterID: "codex/base", wantKeys: []string{"codex_thread_id", "codexThreadId"}},
		{adapterID: "gemini/base", wantKeys: []string{"gemini_session_id", "geminiSessionId"}},
		{adapterID: "qwen/base", wantKeys: []string{"qwen_session_id", "qwenSessionId", "acp_session_id", "acpSessionId"}},
		{adapterID: "unknown/base", wantKeys: nil},
	}
	for _, tc := range testCases {
		t.Run(tc.adapterID, func(t *testing.T) {
			config := inferProviderReplyConfig(tc.adapterID)
			if len(config.bindingIDKeys) != len(tc.wantKeys) {
				t.Fatalf("bindingIDKeys=%v want=%v", config.bindingIDKeys, tc.wantKeys)
			}
			for i, k := range config.bindingIDKeys {
				if k != tc.wantKeys[i] {
					t.Errorf("bindingIDKeys[%d]=%q want=%q", i, k, tc.wantKeys[i])
				}
			}
		})
	}
}

func mapKeysSorted(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// ── thread_compact result 处理测试 ──

func TestHandleLocalActionResult_ThreadCompactSuccessSendsAgentStatus(t *testing.T) {
	sendHandler := &mockSendMessageHandler{
		result: &SendMessageResult{
			MsgID:     92020,
			InboxSeq:  10,
			CreatedAt: time.Now().UnixMilli(),
		},
	}
	mgr := NewManager("", 30*time.Second, sendHandler.handle, nil, nil, nil)
	defer mgr.Shutdown()
	conn := &agentConn{
		agentID:      9988,
		ownerID:      1016,
		clientID:     "thread-compact-result",
		adapterID:    "codex/base",
		capabilities: []string{"local_action_v1"},
		localActions: []string{"thread_compact"},
		send:         make(chan []byte, 2),
	}
	mgr.putConnForTest(conn)
	mgr.storePendingLocalAction(&pendingLocalAction{
		actionID:        "act-compact-ok",
		kind:            "thread_compact",
		agentID:         conn.agentID,
		ownerID:         conn.ownerID,
		sessionID:       "sess-compact-ok",
		quotedMessageID: 18889990790,
		actionType:      "thread_compact",
	})

	mgr.handleLocalActionResult(conn, makePacket(t, protocol.CmdLocalActionResult, 1, protocol.LocalActionResultPayload{
		ActionID: "act-compact-ok",
		Status:   "ok",
	}))

	if len(sendHandler.calls) != 1 {
		t.Fatalf("send handler calls=%d want=1", len(sendHandler.calls))
	}
	cardType, payload := mustParseLocalCardPayload(t, sendHandler.calls[0].Content)
	if cardType != "agent_status" {
		t.Fatalf("card_type=%q want=agent_status", cardType)
	}
	if got := strings.TrimSpace(payload["status"]); got != "success" {
		t.Fatalf("status=%q want=success payload=%v", got, payload)
	}
	if got := strings.TrimSpace(payload["category"]); got != "session" {
		t.Fatalf("category=%q want=session payload=%v", got, payload)
	}
	if got := strings.TrimSpace(payload["summary"]); !strings.Contains(got, "压缩完成") {
		t.Fatalf("summary=%q should contain 压缩完成 payload=%v", got, payload)
	}
	if got := strings.TrimSpace(payload["reference_id"]); got != "sess-compact-ok" {
		t.Fatalf("reference_id=%q want=sess-compact-ok payload=%v", got, payload)
	}
}

func TestHandleLocalActionResult_ThreadCompactFailureSendsAgentStatus(t *testing.T) {
	sendHandler := &mockSendMessageHandler{
		result: &SendMessageResult{
			MsgID:     92021,
			InboxSeq:  11,
			CreatedAt: time.Now().UnixMilli(),
		},
	}
	mgr := NewManager("", 30*time.Second, sendHandler.handle, nil, nil, nil)
	defer mgr.Shutdown()
	conn := &agentConn{
		agentID:      9988,
		ownerID:      1016,
		clientID:     "thread-compact-fail",
		adapterID:    "codex/base",
		capabilities: []string{"local_action_v1"},
		localActions: []string{"thread_compact"},
		send:         make(chan []byte, 2),
	}
	mgr.putConnForTest(conn)
	mgr.storePendingLocalAction(&pendingLocalAction{
		actionID:        "act-compact-fail",
		kind:            "thread_compact",
		agentID:         conn.agentID,
		ownerID:         conn.ownerID,
		sessionID:       "sess-compact-fail",
		quotedMessageID: 18889990791,
		actionType:      "thread_compact",
	})

	mgr.handleLocalActionResult(conn, makePacket(t, protocol.CmdLocalActionResult, 1, protocol.LocalActionResultPayload{
		ActionID: "act-compact-fail",
		Status:   "failed",
		ErrorMsg: "No active thread",
	}))

	if len(sendHandler.calls) != 1 {
		t.Fatalf("send handler calls=%d want=1", len(sendHandler.calls))
	}
	cardType, payload := mustParseLocalCardPayload(t, sendHandler.calls[0].Content)
	if cardType != "agent_status" {
		t.Fatalf("card_type=%q want=agent_status", cardType)
	}
	if got := strings.TrimSpace(payload["status"]); got != "error" {
		t.Fatalf("status=%q want=error payload=%v", got, payload)
	}
	if got := strings.TrimSpace(payload["summary"]); !strings.Contains(got, "压缩失败") {
		t.Fatalf("summary=%q should contain 压缩失败 payload=%v", got, payload)
	}
	if got := strings.TrimSpace(payload["detail_text"]); !strings.Contains(got, "No active thread") {
		t.Fatalf("detail_text=%q should contain error message payload=%v", got, payload)
	}
}

func TestTimeoutPendingLocalAction_ThreadCompactSendsWarningCard(t *testing.T) {
	sendHandler := &mockSendMessageHandler{
		result: &SendMessageResult{
			MsgID:     92022,
			InboxSeq:  12,
			CreatedAt: time.Now().UnixMilli(),
		},
	}
	mgr := NewManager("", 30*time.Second, sendHandler.handle, nil, nil, nil)
	defer mgr.Shutdown()
	mgr.storePendingLocalAction(&pendingLocalAction{
		actionID:   "act-compact-timeout",
		kind:       "thread_compact",
		agentID:    9988,
		ownerID:    1016,
		sessionID:  "sess-compact-timeout",
		actionType: "thread_compact",
	})

	mgr.timeoutPendingLocalAction("act-compact-timeout")

	if len(sendHandler.calls) != 1 {
		t.Fatalf("send handler calls=%d want=1", len(sendHandler.calls))
	}
	cardType, payload := mustParseLocalCardPayload(t, sendHandler.calls[0].Content)
	if cardType != "agent_status" {
		t.Fatalf("card_type=%q want=agent_status", cardType)
	}
	if got := strings.TrimSpace(payload["status"]); got != "warning" {
		t.Fatalf("status=%q want=warning payload=%v", got, payload)
	}
	if got := strings.TrimSpace(payload["summary"]); !strings.Contains(got, "压缩") && !strings.Contains(got, "超时") {
		t.Fatalf("summary=%q should mention compact timeout payload=%v", got, payload)
	}
}

func TestHandleLocalActionResult_ThreadCompactRefreshesToolbar(t *testing.T) {
	testDB := testutil.NewTestDB()
	defer testDB.Close()
	mockRedis := testutil.NewMockRedis()
	defer mockRedis.Close()

	originalDB := appstore.DB
	originalRDB := appstore.RDB
	appstore.DB = testDB.DB
	appstore.RDB = mockRedis
	t.Cleanup(func() {
		appstore.DB = originalDB
		appstore.RDB = originalRDB
		agenttoolbar.SetGlobal(nil)
	})

	const (
		ownerID   int64 = 1017
		agentID   int64 = 9986
		sessionID       = "sess-compact-refresh"
	)

	now := time.Now()
	if err := appstore.DB.Create(&model.Session{
		SessionID:        sessionID,
		OwnerID:          ownerID,
		SessionType:      model.SessionTypeDirect,
		ModerationStatus: model.SessionModerationStatusActive,
	}).Error; err != nil {
		t.Fatalf("create session: %v", err)
	}
	if err := appstore.DB.Create(&model.SessionMember{
		SessionID:    sessionID,
		MemberID:     ownerID,
		MemberType:   1,
		LastActiveAt: now,
		JoinedAt:     now,
	}).Error; err != nil {
		t.Fatalf("create human member: %v", err)
	}
	if err := appstore.DB.Create(&model.SessionMember{
		SessionID:    sessionID,
		MemberID:     agentID,
		MemberType:   2,
		LastActiveAt: now,
		JoinedAt:     now,
	}).Error; err != nil {
		t.Fatalf("create agent member: %v", err)
	}
	if err := appstore.DB.Create(&model.Agent{
		ID:              agentID,
		AgentName:       "Codex",
		OwnerID:         ownerID,
		ProviderType:    model.AgentProviderAPI,
		AgentClientType: model.AgentClientTypeCodex,
		Status:          model.AgentStatusActive,
	}).Error; err != nil {
		t.Fatalf("create agent: %v", err)
	}
	if err := toolruntime.StoreProfile(context.Background(), toolruntime.Profile{
		AgentID:      agentID,
		OwnerID:      ownerID,
		ClientType:   model.AgentClientTypeCodex,
		LocalActions: []string{"thread_compact", "set_model", "set_mode"},
		Online:       true,
	}, 0); err != nil {
		t.Fatalf("store runtime profile: %v", err)
	}
	if err := toolstore.UpsertBinding(context.Background(), toolstore.BindingRecord{
		AgentID:      agentID,
		SessionID:    sessionID,
		ProviderKey:  "codex",
		BindingID:    "codex-thread-compact",
		Cwd:          "/workspace/compact-project",
		WorkerStatus: "ready",
		Meta: map[string]any{
			"model_id": "gpt-5.4",
			"mode_id":  "default",
		},
	}); err != nil {
		t.Fatalf("seed binding: %v", err)
	}

	fanout := &toolbarFanoutRecorder{}
	agenttoolbar.SetGlobal(agenttoolbar.NewService(agenttoolbar.Dependencies{
		Fanout:   fanout.handle,
		Executor: noopToolbarExecutor{},
	}))

	sendHandler := &mockSendMessageHandler{
		result: &SendMessageResult{MsgID: 92023, InboxSeq: 13, CreatedAt: time.Now().UnixMilli()},
	}
	mgr := NewManager("", 30*time.Second, sendHandler.handle, nil, nil, nil)
	defer mgr.Shutdown()
	conn := &agentConn{
		agentID:      agentID,
		ownerID:      ownerID,
		clientID:     "codex-compact-refresh",
		adapterID:    "codex/base",
		capabilities: []string{"local_action_v1"},
		localActions: []string{"thread_compact"},
		send:         make(chan []byte, 2),
	}
	mgr.putConnForTest(conn)
	mgr.storePendingLocalAction(&pendingLocalAction{
		actionID:   "act-compact-refresh",
		kind:       "thread_compact",
		agentID:    conn.agentID,
		ownerID:    conn.ownerID,
		sessionID:  sessionID,
		actionType: "thread_compact",
	})

	mgr.handleLocalActionResult(conn, makePacket(t, protocol.CmdLocalActionResult, 1, protocol.LocalActionResultPayload{
		ActionID: "act-compact-refresh",
		Status:   "ok",
	}))

	// thread_compact 成功后应触发 toolbar refresh
	if len(fanout.calls) == 0 {
		t.Fatal("expected toolbar refresh fanout after thread_compact success")
	}
	last := fanout.calls[len(fanout.calls)-1]
	if last.cmd != protocol.CmdAgentToolbarSync {
		t.Fatalf("fanout cmd=%q want=%q", last.cmd, protocol.CmdAgentToolbarSync)
	}
	if last.ownerID != ownerID {
		t.Fatalf("fanout owner=%d want=%d", last.ownerID, ownerID)
	}
}
