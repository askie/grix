package agentapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"testing"
	"time"

	geminiadapter "github.com/askie/grix/backend/internal/agentadapter/gemini"
	"github.com/askie/grix/backend/internal/grixactions"
	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/testutil"
	"github.com/askie/grix/backend/internal/store"
	"github.com/askie/grix/backend/internal/ws/protocol"
)

func newGeminiInteractionTestManager(t *testing.T, agentID, ownerID int64, sendResult *SendMessageResult) (*Manager, *agentConn, *mockSendMessageHandler) {
	t.Helper()

	testDB := testutil.NewTestDB()
	t.Cleanup(func() {
		testDB.Close()
	})

	originalDB := store.DB
	store.DB = testDB.DB
	t.Cleanup(func() {
		store.DB = originalDB
	})

	originalRDB := store.RDB
	mockRedis := testutil.NewMockRedis()
	store.RDB = mockRedis
	t.Cleanup(func() {
		_ = mockRedis.Close()
		store.RDB = originalRDB
	})

	handler := &mockSendMessageHandler{result: sendResult}
	mgr := NewManager("", 30*time.Second, handler.handle, nil, nil, nil)
	t.Cleanup(mgr.Shutdown)
	conn := &agentConn{
		agentID:      agentID,
		ownerID:      ownerID,
		isPrimary:    true,
		clientID:     "grix-gemini",
		clientType:   model.AgentClientTypeGemini,
		capabilities: []string{"local_action_v1"},
		localActions: []string{"session_control"},
		send:         make(chan []byte, 8),
		adapter:      geminiadapter.NewAdapter(),
		adapterID:    geminiadapter.AdapterID,
	}
	mgr.putConnForTest(conn)
	return mgr, conn, handler
}

func TestPushDelegateEvent_GeminiWorkspaceCardAndReplay(t *testing.T) {
	mgr, conn, handler := newGeminiInteractionTestManager(t, 91031, 82041, &SendMessageResult{
		MsgID:     9001,
		InboxSeq:  77,
		CreatedAt: 1704067205000,
	})

	evt := DelegateEventPayload{
		EventID:   "evt-gemini-workspace-1",
		EventType: "user_chat",
		AgentID:   conn.agentID,
		OwnerID:   conn.ownerID,
		SessionID: "chat-gemini-workspace-1",
		ThreadID:  "chat-gemini-workspace-1",
		MsgID:     18889995001,
		SenderID:  conn.ownerID,
		Content:   "请分析这个项目",
	}

	// In the new flow, the event is dispatched to the plugin directly.
	if ok := mgr.PushDelegateEvent(evt); !ok {
		t.Fatal("PushDelegateEvent should dispatch Gemini event to plugin")
	}

	// Read the dispatched event packet.
	dispatched := mustReadAgentPacket(t, conn.send)
	if dispatched.Cmd != "event_msg" {
		t.Fatalf("dispatched packet cmd=%q want=event_msg", dispatched.Cmd)
	}

	// Simulate plugin returning session_binding_missing.
	mgr.handleEventResult(conn, makePacket(t, protocol.CmdEventResult, 101, EventResultPayload{
		EventID: evt.EventID,
		Status:  protocol.AgentEventResultFailed,
		Code:    "session_binding_missing",
		Msg:     "session workspace binding is missing",
	}))

	// Verify pending workspace stored.
	if _, ok := loadGeminiPendingWorkspace(context.Background(), conn.agentID, evt.SessionID); !ok {
		t.Fatal("expected pending Gemini workspace interaction after session_binding_missing")
	}

	// Submit workspace via open session submit URI.
	submitEvt := DelegateEventPayload{
		EventID:   "evt-gemini-workspace-submit-1",
		EventType: "user_chat",
		AgentID:   conn.agentID,
		OwnerID:   conn.ownerID,
		SessionID: evt.SessionID,
		ThreadID:  evt.ThreadID,
		MsgID:     18889995002,
		SenderID:  conn.ownerID,
		Content:   grixactions.BuildOpenSessionSubmitURI(grixactions.OpenSessionSubmit{Cwd: "/workspace/gemini-demo"}),
	}
	if ok := mgr.PushDelegateEvent(submitEvt); !ok {
		t.Fatal("workspace submit should be handled")
	}

	// Expect session_control local_action sent to plugin.
	localActionPacket := mustReadAgentPacket(t, conn.send)
	if localActionPacket.Cmd != "local_action" {
		t.Fatalf("local_action packet cmd=%q want=local_action", localActionPacket.Cmd)
	}
	var localActionPayload struct {
		ActionID   string         `json:"action_id"`
		ActionType string         `json:"action_type"`
		Params     map[string]any `json:"params"`
	}
	if err := json.Unmarshal(localActionPacket.Payload, &localActionPayload); err != nil {
		t.Fatalf("unmarshal local_action payload: %v", err)
	}
	if localActionPayload.ActionType != "session_control" {
		t.Fatalf("local_action action_type=%q want=session_control", localActionPayload.ActionType)
	}
	if localActionPayload.Params["verb"] != "open" {
		t.Fatalf("local_action verb=%v want=open", localActionPayload.Params["verb"])
	}
	if localActionPayload.Params["cwd"] != "/workspace/gemini-demo" {
		t.Fatalf("local_action cwd=%v want=/workspace/gemini-demo", localActionPayload.Params["cwd"])
	}

	// The original event is replayed with CWD merged into extra.acp.
	replayPacket := mustReadAgentPacket(t, conn.send)
	if replayPacket.Cmd != "event_msg" {
		t.Fatalf("replay packet cmd=%q want=event_msg", replayPacket.Cmd)
	}

	cwd := extractACPwdFromOutboundPayload(t, replayPacket.Payload)
	if cwd != "/workspace/gemini-demo" {
		t.Fatalf("replayed extra.acp.cwd=%q want=/workspace/gemini-demo", cwd)
	}
	replayedEventID, _ := unmarshalOutboundEventIDAndExtra(t, replayPacket.Payload)
	if replayedEventID != submitEvt.EventID {
		t.Fatalf("replayed event_id=%q want submit event %q", replayedEventID, submitEvt.EventID)
	}

	// No workspace card should have been sent by backend.
	if len(handler.calls) != 0 {
		t.Fatalf("send handler calls=%d want=0 (workspace card is handled by plugin)", len(handler.calls))
	}
}

func TestHandleEventResult_GeminiAuthQuestionAndRetry(t *testing.T) {
	mgr, conn, handler := newGeminiInteractionTestManager(t, 91032, 82042, &SendMessageResult{
		MsgID:     9002,
		InboxSeq:  78,
		CreatedAt: 1704067205001,
	})

	evt := DelegateEventPayload{
		EventID:   "evt-gemini-auth-1",
		EventType: "user_chat",
		AgentID:   conn.agentID,
		OwnerID:   conn.ownerID,
		SessionID: "chat-gemini-auth-1",
		ThreadID:  "chat-gemini-auth-1",
		MsgID:     18889996001,
		SenderID:  conn.ownerID,
		Content:   "帮我看下仓库状态",
		Extra: json.RawMessage(`{
			"acp":{"cwd":"/workspace/auth-demo"}
		}`),
	}
	if ok := mgr.PushDelegateEvent(evt); !ok {
		t.Fatal("PushDelegateEvent should dispatch original Gemini event")
	}
	firstPacket := mustReadAgentPacket(t, conn.send)
	if firstPacket.Cmd != "event_msg" {
		t.Fatalf("first plugin packet cmd=%q want=event_msg", firstPacket.Cmd)
	}

	mgr.handleEventResult(conn, makePacket(t, protocol.CmdEventResult, 101, EventResultPayload{
		EventID: evt.EventID,
		Status:  protocol.AgentEventResultFailed,
		Code:    "gemini_auth_missing",
		Msg:     "Please authenticate Gemini CLI locally",
	}))

	if len(handler.calls) != 1 {
		t.Fatalf("send handler calls=%d want=1", len(handler.calls))
	}
	if got := handler.calls[0].Content; !strings.Contains(got, "grix://card/agent_question") {
		t.Fatalf("auth card content=%q should contain agent_question card", got)
	}
	requestID := parseGeminiCardRequestID(handler.calls[0].Content)
	if requestID == "" {
		t.Fatalf("expected request_id in Gemini auth question card, content=%q", handler.calls[0].Content)
	}
	if _, ok := loadGeminiPendingRequest(context.Background(), requestID); !ok {
		t.Fatal("expected pending Gemini auth request")
	}

	replyEvt := DelegateEventPayload{
		EventID:   "evt-gemini-auth-reply-1",
		EventType: "user_chat",
		AgentID:   conn.agentID,
		OwnerID:   conn.ownerID,
		SessionID: evt.SessionID,
		ThreadID:  evt.ThreadID,
		MsgID:     18889996002,
		SenderID:  conn.ownerID,
		Content: grixactions.BuildQuestionReplyURI(grixactions.QuestionReply{
			RequestID: requestID,
			Action:    "accept",
		}),
	}
	if ok := mgr.PushDelegateEvent(replyEvt); !ok {
		t.Fatal("Gemini auth accept reply should be handled")
	}

	if len(handler.calls) != 2 {
		t.Fatalf("send handler calls=%d want=2", len(handler.calls))
	}
	if got := handler.calls[1].Content; !strings.Contains(got, "grix://card/agent_status") {
		t.Fatalf("retry status content=%q should contain agent_status card", got)
	}
	if _, ok := loadGeminiPendingRequest(context.Background(), requestID); ok {
		t.Fatal("expected pending Gemini auth request to be cleared after accept")
	}

	replayedPacket := mustReadAgentPacket(t, conn.send)
	if replayedPacket.Cmd != "event_msg" {
		t.Fatalf("replayed plugin packet cmd=%q want=event_msg", replayedPacket.Cmd)
	}
	eventID, _ := unmarshalOutboundEventIDAndExtra(t, replayedPacket.Payload)
	if eventID != replyEvt.EventID {
		t.Fatalf("replayed event_id=%q want reply event %q", eventID, replyEvt.EventID)
	}
	cwd := extractACPwdFromOutboundPayload(t, replayedPacket.Payload)
	if cwd != "/workspace/auth-demo" {
		t.Fatalf("replayed extra.acp.cwd=%q want=/workspace/auth-demo", cwd)
	}
}

func TestPushDelegateEvent_GeminiAuthCancelSendsWarningStatus(t *testing.T) {
	mgr, conn, handler := newGeminiInteractionTestManager(t, 91033, 82043, &SendMessageResult{
		MsgID:     9003,
		InboxSeq:  79,
		CreatedAt: 1704067205002,
	})

	evt := DelegateEventPayload{
		EventID:   "evt-gemini-auth-cancel-1",
		EventType: "user_chat",
		AgentID:   conn.agentID,
		OwnerID:   conn.ownerID,
		SessionID: "chat-gemini-auth-cancel-1",
		ThreadID:  "chat-gemini-auth-cancel-1",
		MsgID:     18889997001,
		SenderID:  conn.ownerID,
		Content:   "继续刚才的任务",
		Extra: json.RawMessage(`{
			"acp":{"cwd":"/workspace/auth-cancel"}
		}`),
	}
	if ok := mgr.PushDelegateEvent(evt); !ok {
		t.Fatal("PushDelegateEvent should dispatch original Gemini event")
	}
	_ = mustReadAgentPacket(t, conn.send)

	mgr.handleEventResult(conn, makePacket(t, protocol.CmdEventResult, 102, EventResultPayload{
		EventID: evt.EventID,
		Status:  protocol.AgentEventResultFailed,
		Code:    "gemini_auth_missing",
		Msg:     "Please authenticate Gemini CLI locally",
	}))
	if len(handler.calls) != 1 {
		t.Fatalf("send handler calls=%d want=1", len(handler.calls))
	}
	requestID := parseGeminiCardRequestID(handler.calls[0].Content)
	if requestID == "" {
		t.Fatalf("expected request_id in Gemini auth question card, content=%q", handler.calls[0].Content)
	}

	cancelEvt := DelegateEventPayload{
		EventID:   "evt-gemini-auth-cancel-reply-1",
		EventType: "user_chat",
		AgentID:   conn.agentID,
		OwnerID:   conn.ownerID,
		SessionID: evt.SessionID,
		ThreadID:  evt.ThreadID,
		MsgID:     18889997002,
		SenderID:  conn.ownerID,
		Content: grixactions.BuildQuestionReplyURI(grixactions.QuestionReply{
			RequestID: requestID,
			Action:    "cancel",
		}),
	}
	if ok := mgr.PushDelegateEvent(cancelEvt); !ok {
		t.Fatal("Gemini auth cancel reply should be handled")
	}
	if len(handler.calls) != 2 {
		t.Fatalf("send handler calls=%d want=2", len(handler.calls))
	}
	if got := handler.calls[1].Content; !strings.Contains(got, "grix://card/agent_status") {
		t.Fatalf("cancel status content=%q should contain agent_status card", got)
	}
	select {
	case data := <-conn.send:
		t.Fatalf("expected no replay packet after cancel, got=%s", string(data))
	default:
	}
}

func TestPushDelegateEvent_GeminiFormQuestionCardAndReplay(t *testing.T) {
	mgr, conn, handler := newGeminiInteractionTestManager(t, 91034, 82044, &SendMessageResult{
		MsgID:     9004,
		InboxSeq:  80,
		CreatedAt: 1704067205003,
	})

	evt := DelegateEventPayload{
		EventID:   "evt-gemini-question-1",
		EventType: "user_chat",
		AgentID:   conn.agentID,
		OwnerID:   conn.ownerID,
		SessionID: "chat-gemini-question-1",
		ThreadID:  "chat-gemini-question-1",
		MsgID:     18889997011,
		SenderID:  conn.ownerID,
		Content:   "Deploy this release",
		Extra: json.RawMessage(`{
			"acp":{"cwd":"/workspace/question-demo"}
		}`),
		BizCard: json.RawMessage(`{
			"version":1,
			"type":"agent_question",
			"payload":{
				"request_id":"req_gemini_env_1",
				"mode":"form",
				"message":"Choose the target environment before Gemini continues.",
				"questions":[
					{
						"index":1,
						"header":"Environment",
						"prompt":"Choose the deployment environment.",
						"field_key":"environment",
						"options":["staging","prod"]
					}
				]
			}
		}`),
	}
	if ok := mgr.PushDelegateEvent(evt); !ok {
		t.Fatal("PushDelegateEvent should send Gemini form question card")
	}
	if len(handler.calls) != 1 {
		t.Fatalf("send handler calls=%d want=1", len(handler.calls))
	}
	cardType, payload := mustParseLocalCardPayload(t, handler.calls[0].Content)
	if cardType != "agent_question" {
		t.Fatalf("card_type=%q want=agent_question", cardType)
	}
	if got := payload["request_id"]; got != "req_gemini_env_1" {
		t.Fatalf("request_id=%q want=req_gemini_env_1", got)
	}
	select {
	case data := <-conn.send:
		t.Fatalf("expected no plugin dispatch before Gemini question answered, got=%s", string(data))
	default:
	}
	if _, ok := loadGeminiPendingRequest(context.Background(), "req_gemini_env_1"); !ok {
		t.Fatal("expected pending Gemini form question request")
	}

	replyEvt := DelegateEventPayload{
		EventID:   "evt-gemini-question-reply-1",
		EventType: "user_chat",
		AgentID:   conn.agentID,
		OwnerID:   conn.ownerID,
		SessionID: evt.SessionID,
		ThreadID:  evt.ThreadID,
		MsgID:     18889997012,
		SenderID:  conn.ownerID,
		Content: grixactions.BuildQuestionReplyURI(grixactions.QuestionReply{
			RequestID: "req_gemini_env_1",
			Response: map[string]any{
				"type": "map",
				"entries": []map[string]any{
					{
						"key":   "environment",
						"value": "staging",
					},
				},
			},
		}),
	}
	if ok := mgr.PushDelegateEvent(replyEvt); !ok {
		t.Fatal("Gemini form question reply should be handled")
	}
	if len(handler.calls) != 2 {
		t.Fatalf("send handler calls=%d want=2", len(handler.calls))
	}
	if got := handler.calls[1].Content; !strings.Contains(got, "grix://card/agent_status") {
		t.Fatalf("reply status content=%q should contain agent_status card", got)
	}
	if _, ok := loadGeminiPendingRequest(context.Background(), "req_gemini_env_1"); ok {
		t.Fatal("expected Gemini form question request to be cleared after reply")
	}

	packet := mustReadAgentPacket(t, conn.send)
	if packet.Cmd != "event_msg" {
		t.Fatalf("plugin packet cmd=%q want=event_msg", packet.Cmd)
	}
	var delivered struct {
		EventID string          `json:"event_id"`
		BizCard any             `json:"biz_card"`
		Extra   json.RawMessage `json:"extra"`
	}
	if err := json.Unmarshal(packet.Payload, &delivered); err != nil {
		t.Fatalf("unmarshal replay payload: %v", err)
	}
	if delivered.EventID != replyEvt.EventID {
		t.Fatalf("replayed event_id=%q want reply event %q", delivered.EventID, replyEvt.EventID)
	}
	if delivered.BizCard != nil {
		t.Fatalf("replayed payload should not keep question biz_card, got=%v", delivered.BizCard)
	}
	var acp struct {
		Cwd    string           `json:"cwd"`
		Prompt []map[string]any `json:"prompt"`
	}
	var extraWrapper struct {
		ACP *struct {
			Cwd    string           `json:"cwd"`
			Prompt []map[string]any `json:"prompt"`
		} `json:"acp"`
	}
	extraWrapper.ACP = &acp
	if err := json.Unmarshal(delivered.Extra, &extraWrapper); err != nil {
		t.Fatalf("unmarshal extra.acp: %v", err)
	}
	if acp.Cwd != "/workspace/question-demo" {
		t.Fatalf("replayed extra.acp.cwd=%q want=/workspace/question-demo", acp.Cwd)
	}
	if len(acp.Prompt) != 2 {
		t.Fatalf("prompt blocks=%d want=2", len(acp.Prompt))
	}
	if got := fmt.Sprint(acp.Prompt[0]["text"]); got != "Deploy this release" {
		t.Fatalf("prompt[0]=%q want original message block", got)
	}
	if got := fmt.Sprint(acp.Prompt[1]["text"]); !strings.Contains(got, "Environment: staging") {
		t.Fatalf("prompt[1]=%q should contain formatted answer", got)
	}
}

func TestPushDelegateEvent_GeminiFormQuestionCancelSendsWarningStatus(t *testing.T) {
	mgr, conn, handler := newGeminiInteractionTestManager(t, 91035, 82045, &SendMessageResult{
		MsgID:     9005,
		InboxSeq:  81,
		CreatedAt: 1704067205004,
	})

	evt := DelegateEventPayload{
		EventID:   "evt-gemini-question-cancel-1",
		EventType: "user_chat",
		AgentID:   conn.agentID,
		OwnerID:   conn.ownerID,
		SessionID: "chat-gemini-question-cancel-1",
		ThreadID:  "chat-gemini-question-cancel-1",
		MsgID:     18889997021,
		SenderID:  conn.ownerID,
		Content:   "Deploy this release",
		Extra: json.RawMessage(`{
			"acp":{"cwd":"/workspace/question-cancel"}
		}`),
		BizCard: json.RawMessage(`{
			"version":1,
			"type":"agent_question",
			"payload":{
				"request_id":"req_gemini_env_cancel_1",
				"questions":[
					{
						"index":1,
						"header":"Environment",
						"prompt":"Choose the deployment environment."
					}
				]
			}
		}`),
	}
	if ok := mgr.PushDelegateEvent(evt); !ok {
		t.Fatal("PushDelegateEvent should send Gemini form question card")
	}
	if len(handler.calls) != 1 {
		t.Fatalf("send handler calls=%d want=1", len(handler.calls))
	}

	cancelEvt := DelegateEventPayload{
		EventID:   "evt-gemini-question-cancel-reply-1",
		EventType: "user_chat",
		AgentID:   conn.agentID,
		OwnerID:   conn.ownerID,
		SessionID: evt.SessionID,
		ThreadID:  evt.ThreadID,
		MsgID:     18889997022,
		SenderID:  conn.ownerID,
		Content: grixactions.BuildQuestionReplyURI(grixactions.QuestionReply{
			RequestID: "req_gemini_env_cancel_1",
			Action:    "cancel",
		}),
	}
	if ok := mgr.PushDelegateEvent(cancelEvt); !ok {
		t.Fatal("Gemini form question cancel reply should be handled")
	}
	if len(handler.calls) != 2 {
		t.Fatalf("send handler calls=%d want=2", len(handler.calls))
	}
	if got := handler.calls[1].Content; !strings.Contains(got, "grix://card/agent_status") {
		t.Fatalf("cancel status content=%q should contain agent_status card", got)
	}
	select {
	case data := <-conn.send:
		t.Fatalf("expected no replay packet after Gemini question cancel, got=%s", string(data))
	default:
	}
}

func TestHandleEventResult_GeminiFailureStatusCards(t *testing.T) {
	tests := []struct {
		name               string
		code               string
		msg                string
		wantStatus         string
		wantSummary        string
		wantDetailContains string
	}{
		{
			name:               "timeout",
			code:               "gemini_prompt_timeout",
			msg:                "Gemini turn timed out.",
			wantStatus:         "warning",
			wantSummary:        "Gemini 请求超时。",
			wantDetailContains: "timed out",
		},
		{
			name:               "process exit",
			code:               "gemini_process_exit",
			msg:                "gemini process exited with code 1",
			wantStatus:         "error",
			wantSummary:        "Gemini 本地进程已停止。",
			wantDetailContains: "code 1",
		},
		{
			name:               "prompt failed",
			code:               "gemini_prompt_failed",
			msg:                "prompt request failed",
			wantStatus:         "error",
			wantSummary:        "Gemini 请求失败。",
			wantDetailContains: "prompt request failed",
		},
		{
			name:               "empty output",
			code:               "gemini_empty_output",
			msg:                "Gemini completed without visible output.",
			wantStatus:         "warning",
			wantSummary:        "Gemini 未返回可见输出。",
			wantDetailContains: "without visible output",
		},
		{
			name:               "invalid payload generic",
			code:               "gemini_invalid_payload",
			msg:                "prompt blocks are required",
			wantStatus:         "error",
			wantSummary:        "Gemini 请求负载无效。",
			wantDetailContains: "prompt blocks are required",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			mgr, conn, handler := newGeminiInteractionTestManager(t, 91040, 82050, &SendMessageResult{
				MsgID:     9010,
				InboxSeq:  88,
				CreatedAt: 1704067205010,
			})

			evt := DelegateEventPayload{
				EventID:   "evt-gemini-failure-card-1",
				EventType: "user_chat",
				AgentID:   conn.agentID,
				OwnerID:   conn.ownerID,
				SessionID: "chat-gemini-failure-card-1",
				ThreadID:  "chat-gemini-failure-card-1",
				MsgID:     18889998001,
				SenderID:  conn.ownerID,
				Content:   "继续处理这个问题",
				Extra: json.RawMessage(`{
					"acp":{"cwd":"/workspace/failure-card"}
				}`),
			}
			if ok := mgr.PushDelegateEvent(evt); !ok {
				t.Fatal("PushDelegateEvent should dispatch original Gemini event")
			}
			_ = mustReadAgentPacket(t, conn.send)

			mgr.handleEventResult(conn, makePacket(t, protocol.CmdEventResult, 103, EventResultPayload{
				EventID: evt.EventID,
				Status:  protocol.AgentEventResultFailed,
				Code:    tc.code,
				Msg:     tc.msg,
			}))

			if len(handler.calls) != 1 {
				t.Fatalf("send handler calls=%d want=1", len(handler.calls))
			}
			cardType, payload := mustParseLocalCardPayload(t, handler.calls[0].Content)
			if cardType != "agent_status" {
				t.Fatalf("card_type=%q want=agent_status", cardType)
			}
			if got := strings.TrimSpace(payload["status"]); got != tc.wantStatus {
				t.Fatalf("status=%q want=%q payload=%v", got, tc.wantStatus, payload)
			}
			if got := strings.TrimSpace(payload["summary"]); got != tc.wantSummary {
				t.Fatalf("summary=%q want=%q payload=%v", got, tc.wantSummary, payload)
			}
			if got := strings.TrimSpace(payload["category"]); got != "runtime" {
				t.Fatalf("category=%q want=runtime payload=%v", got, payload)
			}
			if got := strings.TrimSpace(payload["reference_id"]); got != evt.EventID {
				t.Fatalf("reference_id=%q want=%q payload=%v", got, evt.EventID, payload)
			}
			if got := strings.TrimSpace(payload["detail_text"]); !strings.Contains(got, tc.wantDetailContains) {
				t.Fatalf("detail_text=%q should contain %q", got, tc.wantDetailContains)
			}
		})
	}
}

func TestHandleEventResult_GeminiInvalidPayloadWorkspaceReopensWorkspaceCard(t *testing.T) {
	mgr, conn, handler := newGeminiInteractionTestManager(t, 91041, 82051, &SendMessageResult{
		MsgID:     9011,
		InboxSeq:  89,
		CreatedAt: 1704067205011,
	})

	evt := DelegateEventPayload{
		EventID:   "evt-gemini-invalid-workspace-1",
		EventType: "user_chat",
		AgentID:   conn.agentID,
		OwnerID:   conn.ownerID,
		SessionID: "chat-gemini-invalid-workspace-1",
		ThreadID:  "chat-gemini-invalid-workspace-1",
		MsgID:     18889998002,
		SenderID:  conn.ownerID,
		Content:   "继续处理",
		Extra: json.RawMessage(`{
			"acp":{"cwd":"relative/path"}
		}`),
	}
	if ok := mgr.PushDelegateEvent(evt); !ok {
		t.Fatal("PushDelegateEvent should dispatch original Gemini event")
	}
	_ = mustReadAgentPacket(t, conn.send)

	mgr.handleEventResult(conn, makePacket(t, protocol.CmdEventResult, 104, EventResultPayload{
		EventID: evt.EventID,
		Status:  protocol.AgentEventResultFailed,
		Code:    "gemini_invalid_payload",
		Msg:     "cwd must be an absolute path",
	}))

	if len(handler.calls) != 1 {
		t.Fatalf("send handler calls=%d want=1", len(handler.calls))
	}
	cardType, payload := mustParseLocalCardPayload(t, handler.calls[0].Content)
	if cardType != "agent_status" {
		t.Fatalf("card_type=%q want=agent_status", cardType)
	}
	if got := strings.TrimSpace(payload["status"]); got != "error" {
		t.Fatalf("status=%q want=error", got)
	}
	if got := strings.TrimSpace(payload["summary"]); got != "Gemini 请求负载无效。" {
		t.Fatalf("summary=%q want=Gemini request payload is invalid.", got)
	}
	if got := strings.TrimSpace(payload["detail_text"]); !strings.Contains(got, "cwd must be an absolute path") {
		t.Fatalf("detail_text=%q should contain error detail", got)
	}
}

func mustReadAgentPacket(t *testing.T, ch <-chan []byte) protocol.Packet {
	t.Helper()
	select {
	case raw := <-ch:
		var pkt protocol.Packet
		if err := json.Unmarshal(raw, &pkt); err != nil {
			t.Fatalf("unmarshal agent packet: %v", err)
		}
		return pkt
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for agent packet")
		return protocol.Packet{}
	}
}

func mustParseLocalCardPayload(t *testing.T, content string) (string, map[string]string) {
	t.Helper()

	trimmed := strings.TrimSpace(content)
	if start := strings.Index(trimmed, "(grix://card/"); start >= 0 {
		trimmed = trimmed[start+1:]
		if end := strings.Index(trimmed, ")"); end >= 0 {
			trimmed = trimmed[:end]
		}
	}

	parsed, err := url.Parse(trimmed)
	if err != nil {
		t.Fatalf("parse local card uri: %v", err)
	}
	if !strings.EqualFold(parsed.Scheme, "grix") || !strings.EqualFold(parsed.Host, "card") {
		t.Fatalf("content=%q is not a grix card uri", content)
	}

	cardType := strings.Trim(strings.TrimSpace(parsed.Path), "/")
	payload := map[string]string{}
	if raw := strings.TrimSpace(parsed.Query().Get("d")); raw != "" {
		var decoded map[string]any
		if err := json.Unmarshal([]byte(raw), &decoded); err != nil {
			t.Fatalf("decode local card payload: %v", err)
		}
		for key, value := range decoded {
			payload[key] = valueToString(value)
		}
		return cardType, payload
	}
	for key, values := range parsed.Query() {
		if len(values) == 0 {
			continue
		}
		payload[key] = strings.TrimSpace(values[0])
	}
	return cardType, payload
}

func valueToString(value any) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(value))
}

func extractACPwdFromOutboundPayload(t *testing.T, raw json.RawMessage) string {
	t.Helper()
	var wrapper struct {
		Extra json.RawMessage `json:"extra"`
	}
	if err := json.Unmarshal(raw, &wrapper); err != nil {
		t.Fatalf("unmarshal outbound wrapper: %v", err)
	}
	if len(wrapper.Extra) == 0 {
		return ""
	}
	var extra struct {
		ACP struct {
			Cwd string `json:"cwd"`
		} `json:"acp"`
	}
	if err := json.Unmarshal(wrapper.Extra, &extra); err != nil {
		t.Fatalf("unmarshal extra.acp: %v", err)
	}
	return extra.ACP.Cwd
}

func unmarshalOutboundEventIDAndExtra(t *testing.T, raw json.RawMessage) (string, json.RawMessage) {
	t.Helper()
	var wrapper struct {
		EventID string          `json:"event_id"`
		Extra   json.RawMessage `json:"extra"`
	}
	if err := json.Unmarshal(raw, &wrapper); err != nil {
		t.Fatalf("unmarshal outbound wrapper: %v", err)
	}
	return wrapper.EventID, wrapper.Extra
}
