package agentapi

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/askie/grix/backend/internal/grixactions"
	"github.com/askie/grix/backend/internal/ws/protocol"
)

func TestPushDelegateEvent_InterceptsExecApprovalCommandAsLocalAction(t *testing.T) {
	sendHandler := &mockSendMessageHandler{
		result: &SendMessageResult{
			MsgID:     91001,
			InboxSeq:  1,
			CreatedAt: time.Now().UnixMilli(),
		},
	}
	mgr := NewManager("", 30*time.Second, sendHandler.handle, nil, nil, nil)
	defer mgr.Shutdown()
	conn := &agentConn{
		agentID:      9995,
		ownerID:      1001,
		clientID:     "exec-approval",
		capabilities: []string{"local_action_v1"},
		localActions: []string{"exec_approve", "exec_reject"},
		send:         make(chan []byte, 4),
	}
	mgr.putConnForTest(conn)

	event := DelegateEventPayload{
		EventID:   "evt-approval-1",
		EventType: "user_chat",
		AgentID:   conn.agentID,
		OwnerID:   conn.ownerID,
		SessionID: "sess-approval-1",
		MsgID:     18889990555,
		SenderID:  1001,
		Content:   "/approve req_123 allow-always",
	}
	if ok := mgr.PushDelegateEvent(event); !ok {
		t.Fatal("PushDelegateEvent should intercept exec approval command")
	}

	select {
	case data := <-conn.send:
		var pkt protocol.Packet
		if err := json.Unmarshal(data, &pkt); err != nil {
			t.Fatalf("unmarshal packet: %v", err)
		}
		if pkt.Cmd != protocol.CmdLocalAction {
			t.Fatalf("packet cmd=%s want=%s", pkt.Cmd, protocol.CmdLocalAction)
		}
		var payload protocol.LocalActionPayload
		if err := json.Unmarshal(pkt.Payload, &payload); err != nil {
			t.Fatalf("unmarshal local_action payload: %v", err)
		}
		if payload.ActionType != "exec_approve" {
			t.Fatalf("action_type=%s want=exec_approve", payload.ActionType)
		}
		if got := payload.Params["exec_context_id"]; got != "req_123" {
			t.Fatalf("exec_context_id=%v want=req_123", got)
		}
		if got := payload.Params["decision"]; got != "allow-always" {
			t.Fatalf("decision=%v want=allow-always", got)
		}
		if got := payload.Params["actor_id"]; got != "1001" {
			t.Fatalf("actor_id=%v want=1001", got)
		}
		if got := payload.Params["session_id"]; got != "sess-approval-1" {
			t.Fatalf("session_id=%v want=sess-approval-1", got)
		}
		if got := payload.Params["msg_id"]; got != "18889990555" {
			t.Fatalf("msg_id=%v want=18889990555", got)
		}

		mgr.handleLocalActionResult(conn, makePacket(t, protocol.CmdLocalActionResult, 1, protocol.LocalActionResultPayload{
			ActionID: payload.ActionID,
			Status:   "ok",
			Result: map[string]any{
				"decision": "allow-always",
			},
		}))
	default:
		t.Fatal("expected local_action packet")
	}

	if len(sendHandler.calls) != 1 {
		t.Fatalf("send handler calls=%d want=1", len(sendHandler.calls))
	}
	reply := sendHandler.calls[0]
	if reply.SessionID != event.SessionID {
		t.Fatalf("reply session_id=%s want=%s", reply.SessionID, event.SessionID)
	}
	if reply.QuotedMessageID != event.MsgID {
		t.Fatalf("reply quoted_message_id=%d want=%d", reply.QuotedMessageID, event.MsgID)
	}
	if !strings.Contains(reply.Content, "grix://card/exec_status") {
		t.Fatalf("reply content=%q", reply.Content)
	}
	var extra map[string]any
	if err := json.Unmarshal(reply.Extra, &extra); err != nil {
		t.Fatalf("unmarshal reply extra: %v", err)
	}
	bizCard, _ := extra["biz_card"].(map[string]any)
	if bizCard == nil {
		t.Fatalf("reply extra=%#v missing biz_card", extra)
	}
	payload, _ := bizCard["payload"].(map[string]any)
	if payload == nil {
		t.Fatalf("biz_card=%#v missing payload", bizCard)
	}
	if got := payload["status"]; got != "resolved-allow-always" {
		t.Fatalf("payload status=%v want=resolved-allow-always", got)
	}
	if got := payload["summary"]; got != "已允许永久执行。" {
		t.Fatalf("payload summary=%v want=已允许永久执行。", got)
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
}

func TestPushDelegateEvent_InterceptsGrixApprovalCommandAsLocalAction(t *testing.T) {
	sendHandler := &mockSendMessageHandler{
		result: &SendMessageResult{
			MsgID:     91011,
			InboxSeq:  1,
			CreatedAt: time.Now().UnixMilli(),
		},
	}
	mgr := NewManager("", 30*time.Second, sendHandler.handle, nil, nil, nil)
	defer mgr.Shutdown()
	conn := &agentConn{
		agentID:      9998,
		ownerID:      1004,
		clientID:     "exec-approval-grix",
		capabilities: []string{"local_action_v1"},
		localActions: []string{"exec_approve", "exec_reject"},
		send:         make(chan []byte, 2),
	}
	mgr.putConnForTest(conn)

	event := DelegateEventPayload{
		EventID:   "evt-approval-grix-1",
		EventType: "user_chat",
		AgentID:   conn.agentID,
		OwnerID:   conn.ownerID,
		SessionID: "sess-approval-grix-1",
		MsgID:     18889990556,
		SenderID:  1004,
		Content:   "/grix approval req_234 allow",
	}
	if ok := mgr.PushDelegateEvent(event); !ok {
		t.Fatal("PushDelegateEvent should intercept /grix approval command")
	}

	select {
	case data := <-conn.send:
		var pkt protocol.Packet
		if err := json.Unmarshal(data, &pkt); err != nil {
			t.Fatalf("unmarshal packet: %v", err)
		}
		var payload protocol.LocalActionPayload
		if err := json.Unmarshal(pkt.Payload, &payload); err != nil {
			t.Fatalf("unmarshal local_action payload: %v", err)
		}
		if got := payload.Params["exec_context_id"]; got != "req_234" {
			t.Fatalf("exec_context_id=%v want=req_234", got)
		}
		if got := payload.Params["decision"]; got != "allow-once" {
			t.Fatalf("decision=%v want=allow-once", got)
		}
	default:
		t.Fatal("expected local_action packet")
	}
}

func TestPushDelegateEvent_InvalidExecApprovalCommandSendsUsageReply(t *testing.T) {
	sendHandler := &mockSendMessageHandler{
		result: &SendMessageResult{
			MsgID:     91002,
			InboxSeq:  1,
			CreatedAt: time.Now().UnixMilli(),
		},
	}
	mgr := NewManager("", 30*time.Second, sendHandler.handle, nil, nil, nil)
	defer mgr.Shutdown()
	conn := &agentConn{
		agentID:      9996,
		ownerID:      1002,
		clientID:     "exec-approval-usage",
		capabilities: []string{"local_action_v1"},
		localActions: []string{"exec_approve", "exec_reject"},
		send:         make(chan []byte, 2),
	}
	mgr.putConnForTest(conn)

	event := DelegateEventPayload{
		EventID:   "evt-approval-usage-1",
		EventType: "user_chat",
		AgentID:   conn.agentID,
		OwnerID:   conn.ownerID,
		SessionID: "sess-approval-usage-1",
		MsgID:     18889990666,
		SenderID:  1002,
		Content:   "/approve req_123",
	}
	if ok := mgr.PushDelegateEvent(event); !ok {
		t.Fatal("PushDelegateEvent should handle invalid exec approval command")
	}
	if len(sendHandler.calls) != 1 {
		t.Fatalf("send handler calls=%d want=1", len(sendHandler.calls))
	}
	if sendHandler.calls[0].Content != execApprovalUsage {
		t.Fatalf("reply content=%q want=%q", sendHandler.calls[0].Content, execApprovalUsage)
	}
	select {
	case data := <-conn.send:
		t.Fatalf("unexpected packet queued: %s", string(data))
	default:
	}
}

func TestPushDelegateEvent_InterceptsGrixApprovalRuleDirectiveAsLocalAction(t *testing.T) {
	sendHandler := &mockSendMessageHandler{
		result: &SendMessageResult{
			MsgID:     91012,
			InboxSeq:  1,
			CreatedAt: time.Now().UnixMilli(),
		},
	}
	mgr := NewManager("", 30*time.Second, sendHandler.handle, nil, nil, nil)
	defer mgr.Shutdown()
	conn := &agentConn{
		agentID:      9999,
		ownerID:      1005,
		clientID:     "exec-approval-grix-rule",
		capabilities: []string{"local_action_v1"},
		localActions: []string{"exec_approve", "exec_reject"},
		send:         make(chan []byte, 2),
	}
	mgr.putConnForTest(conn)

	event := DelegateEventPayload{
		EventID:   "evt-approval-grix-rule-1",
		EventType: "user_chat",
		AgentID:   conn.agentID,
		OwnerID:   conn.ownerID,
		SessionID: "sess-approval-grix-rule-1",
		MsgID:     18889990557,
		SenderID:  1005,
		Content:   "/grix approval req_345 allow-rule 2",
	}
	if ok := mgr.PushDelegateEvent(event); !ok {
		t.Fatal("PushDelegateEvent should intercept /grix approval allow-rule command")
	}

	select {
	case data := <-conn.send:
		var pkt protocol.Packet
		if err := json.Unmarshal(data, &pkt); err != nil {
			t.Fatalf("unmarshal packet: %v", err)
		}
		var payload protocol.LocalActionPayload
		if err := json.Unmarshal(pkt.Payload, &payload); err != nil {
			t.Fatalf("unmarshal local_action payload: %v", err)
		}
		if got := payload.Params["decision"]; got != "allow-rule" {
			t.Fatalf("decision=%v want=allow-rule", got)
		}
		if got := payload.Params["rule_index"]; got != "2" {
			t.Fatalf("rule_index=%v want=2", got)
		}
	default:
		t.Fatal("expected local_action packet")
	}
}

func TestPushDelegateEvent_InterceptsClaudePermissionDirectiveAsLocalAction(t *testing.T) {
	sendHandler := &mockSendMessageHandler{
		result: &SendMessageResult{
			MsgID:     91003,
			InboxSeq:  1,
			CreatedAt: time.Now().UnixMilli(),
		},
	}
	mgr := NewManager("", 30*time.Second, sendHandler.handle, nil, nil, nil)
	defer mgr.Shutdown()
	conn := &agentConn{
		agentID:      9997,
		ownerID:      1003,
		clientID:     "claude-permission",
		clientType:   "claude",
		capabilities: []string{"local_action_v1"},
		localActions: []string{"claude_interaction_reply"},
		send:         make(chan []byte, 4),
	}
	mgr.putConnForTest(conn)

	event := DelegateEventPayload{
		EventID:   "evt-approval-claude-1",
		EventType: "user_chat",
		AgentID:   conn.agentID,
		OwnerID:   conn.ownerID,
		SessionID: "sess-approval-claude-1",
		MsgID:     18889990777,
		SenderID:  1003,
		Content:   "[[exec-approval-resolution|approval_id=req_456|approval_command_id=req_456|decision=allow]]",
	}
	if ok := mgr.PushDelegateEvent(event); !ok {
		t.Fatal("PushDelegateEvent should intercept Claude permission directive")
	}

	select {
	case data := <-conn.send:
		var pkt protocol.Packet
		if err := json.Unmarshal(data, &pkt); err != nil {
			t.Fatalf("unmarshal packet: %v", err)
		}
		if pkt.Cmd != protocol.CmdLocalAction {
			t.Fatalf("packet cmd=%s want=%s", pkt.Cmd, protocol.CmdLocalAction)
		}
		var payload protocol.LocalActionPayload
		if err := json.Unmarshal(pkt.Payload, &payload); err != nil {
			t.Fatalf("unmarshal local_action payload: %v", err)
		}
		if payload.ActionType != "claude_interaction_reply" {
			t.Fatalf("action_type=%s want=claude_interaction_reply", payload.ActionType)
		}
		if got := payload.Params["session_id"]; got != event.SessionID {
			t.Fatalf("session_id=%v want=%s", got, event.SessionID)
		}
		if got := payload.Params["kind"]; got != "permission" {
			t.Fatalf("kind=%v want=permission", got)
		}
		if got := payload.Params["request_id"]; got != "req_456" {
			t.Fatalf("request_id=%v want=req_456", got)
		}
		resolution, ok := payload.Params["resolution"].(map[string]interface{})
		if !ok {
			t.Fatalf("resolution=%#v", payload.Params["resolution"])
		}
		if got := resolution["type"]; got != "decision" {
			t.Fatalf("resolution type=%v want=decision", got)
		}
		if got := resolution["value"]; got != "allow" {
			t.Fatalf("resolution value=%v want=allow", got)
		}

		mgr.handleLocalActionResult(conn, makePacket(t, protocol.CmdLocalActionResult, 1, protocol.LocalActionResultPayload{
			ActionID: payload.ActionID,
			Status:   "ok",
			Result: map[string]any{
				"domain":     "interaction_reply",
				"kind":       "permission",
				"request_id": "req_456",
				"outcome":    "resolved",
			},
		}))
	default:
		t.Fatal("expected local_action packet")
	}

	if len(sendHandler.calls) != 1 {
		t.Fatalf("send handler calls=%d want=1", len(sendHandler.calls))
	}
	reply := sendHandler.calls[0]
	if reply.SessionID != event.SessionID || reply.QuotedMessageID != event.MsgID {
		t.Fatalf("reply=%#v", reply)
	}
	if !strings.Contains(reply.Content, "grix://card/agent_status") {
		t.Fatalf("reply content=%q", reply.Content)
	}
}

func TestPushDelegateEvent_InterceptsClaudeQuestionCommandAsLocalAction(t *testing.T) {
	sendHandler := &mockSendMessageHandler{
		result: &SendMessageResult{
			MsgID:     91004,
			InboxSeq:  1,
			CreatedAt: time.Now().UnixMilli(),
		},
	}
	mgr := NewManager("", 30*time.Second, sendHandler.handle, nil, nil, nil)
	defer mgr.Shutdown()
	conn := &agentConn{
		agentID:      9998,
		ownerID:      1004,
		clientID:     "claude-question",
		clientType:   "claude",
		capabilities: []string{"local_action_v1"},
		localActions: []string{"claude_interaction_reply"},
		send:         make(chan []byte, 4),
	}
	mgr.putConnForTest(conn)

	event := DelegateEventPayload{
		EventID:   "evt-question-1",
		EventType: "user_chat",
		AgentID:   conn.agentID,
		OwnerID:   conn.ownerID,
		SessionID: "sess-question-1",
		MsgID:     18889990888,
		SenderID:  1004,
		Content: grixactions.BuildQuestionReplyURI(grixactions.QuestionReply{
			RequestID: "req-question-1",
			Response: map[string]any{
				"type":  "single",
				"value": "production",
			},
		}),
	}
	if ok := mgr.PushDelegateEvent(event); !ok {
		t.Fatal("PushDelegateEvent should intercept Claude question command")
	}

	select {
	case data := <-conn.send:
		var pkt protocol.Packet
		if err := json.Unmarshal(data, &pkt); err != nil {
			t.Fatalf("unmarshal packet: %v", err)
		}
		if pkt.Cmd != protocol.CmdLocalAction {
			t.Fatalf("packet cmd=%s want=%s", pkt.Cmd, protocol.CmdLocalAction)
		}
		var payload protocol.LocalActionPayload
		if err := json.Unmarshal(pkt.Payload, &payload); err != nil {
			t.Fatalf("unmarshal local_action payload: %v", err)
		}
		if payload.ActionType != "claude_interaction_reply" {
			t.Fatalf("action_type=%s want=claude_interaction_reply", payload.ActionType)
		}
		if got := payload.Params["session_id"]; got != event.SessionID {
			t.Fatalf("session_id=%v want=%s", got, event.SessionID)
		}
		if got := payload.Params["kind"]; got != "elicitation" {
			t.Fatalf("kind=%v want=elicitation", got)
		}
		if got := payload.Params["request_id"]; got != "req-question-1" {
			t.Fatalf("request_id=%v want=req-question-1", got)
		}
		response, ok := payload.Params["resolution"].(map[string]interface{})
		if !ok {
			t.Fatalf("resolution payload=%#v", payload.Params["resolution"])
		}
		if got := response["type"]; got != "text" {
			t.Fatalf("resolution type=%v want=text", got)
		}
		if got := response["value"]; got != "production" {
			t.Fatalf("resolution value=%v want=production", got)
		}

		mgr.handleLocalActionResult(conn, makePacket(t, protocol.CmdLocalActionResult, 1, protocol.LocalActionResultPayload{
			ActionID: payload.ActionID,
			Status:   "ok",
			Result: map[string]any{
				"domain":     "interaction_reply",
				"kind":       "elicitation",
				"request_id": "req-question-1",
				"outcome":    "resolved",
			},
		}))
	default:
		t.Fatal("expected local_action packet")
	}

	if len(sendHandler.calls) != 1 {
		t.Fatalf("send handler calls=%d want=1", len(sendHandler.calls))
	}
	reply := sendHandler.calls[0]
	if reply.SessionID != event.SessionID || reply.QuotedMessageID != event.MsgID {
		t.Fatalf("reply=%#v", reply)
	}
	if !strings.Contains(reply.Content, "grix://card/agent_status") {
		t.Fatalf("reply content=%q", reply.Content)
	}
	if !strings.Contains(reply.Content, "req-question-1") {
		t.Fatalf("reply content=%q", reply.Content)
	}
}
