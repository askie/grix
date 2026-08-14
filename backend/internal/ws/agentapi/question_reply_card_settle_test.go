package agentapi

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/askie/grix/backend/internal/grixactions"
	"github.com/askie/grix/backend/internal/pkg/testutil"
	"github.com/askie/grix/backend/internal/store"
	"github.com/askie/grix/backend/internal/ws/protocol"
)

func setupQuestionCardSettleTest(t *testing.T) func() {
	t.Helper()
	previousRDB := store.RDB
	store.RDB = testutil.NewMockRedis()
	return func() {
		_ = store.RDB.Close()
		store.RDB = previousRDB
	}
}

func questionReplyEvent(eventID string) DelegateEventPayload {
	return DelegateEventPayload{
		EventID:   eventID,
		AgentID:   9101,
		OwnerID:   1101,
		SessionID: "sess-question-settle",
		MsgID:     8801,
		MsgType:   1,
		Content: grixactions.BuildQuestionReplyURI(grixactions.QuestionReply{
			RequestID: "que-settle-1",
			Response:  map[string]any{"type": "single", "value": "答案是 A"},
		}),
	}
}

func TestSettleAgentQuestionReplyCardRespondedEditsCardInPlace(t *testing.T) {
	defer setupQuestionCardSettleTest(t)()

	evt := questionReplyEvent("evt-question-responded")
	saveApprovalCardMsgID(context.Background(), evt.AgentID, evt.SessionID, "que-settle-1", 6601)

	editHandler := &mockEditMessageHandler{}
	sendHandler := &mockSendMessageHandler{result: &SendMessageResult{MsgID: 1}}
	mgr := NewManager("", 30*time.Second, sendHandler.handle, nil, nil, nil)
	defer mgr.Shutdown()
	mgr.editMsgFn = editHandler.handle

	if err := mgr.settleAgentQuestionReplyCard(evt, EventResultPayload{
		EventID: evt.EventID,
		Status:  protocol.AgentEventResultResponded,
	}); err != nil {
		t.Fatalf("settle: %v", err)
	}

	if len(editHandler.calls) != 1 {
		t.Fatalf("edit calls=%d want=1", len(editHandler.calls))
	}
	call := editHandler.calls[0]
	if call.MsgID != 6601 || call.SessionID != evt.SessionID {
		t.Fatalf("edit payload=%#v", call)
	}
	if !strings.Contains(call.Content, "grix://card/agent_status") {
		t.Fatalf("edit content missing agent_status card: %s", call.Content)
	}
	if !strings.Contains(call.Content, "success") {
		t.Fatalf("edit content missing success status: %s", call.Content)
	}
	if len(sendHandler.calls) != 0 {
		t.Fatalf("send calls=%d want=0 (in-place edit only)", len(sendHandler.calls))
	}
	if got := loadApprovalCardMsgID(context.Background(), evt.AgentID, evt.SessionID, "que-settle-1"); got != 0 {
		t.Fatalf("approval card mapping not cleaned: %d", got)
	}
}

func TestSettleAgentQuestionReplyCardFailedMarksError(t *testing.T) {
	defer setupQuestionCardSettleTest(t)()

	evt := questionReplyEvent("evt-question-failed")
	saveApprovalCardMsgID(context.Background(), evt.AgentID, evt.SessionID, "que-settle-1", 6602)

	editHandler := &mockEditMessageHandler{}
	mgr := NewManager("", 30*time.Second, (&mockSendMessageHandler{result: &SendMessageResult{MsgID: 1}}).handle, nil, nil, nil)
	defer mgr.Shutdown()
	mgr.editMsgFn = editHandler.handle

	if err := mgr.settleAgentQuestionReplyCard(evt, EventResultPayload{
		EventID: evt.EventID,
		Status:  protocol.AgentEventResultFailed,
		Code:    "interaction_request_not_pending",
	}); err != nil {
		t.Fatalf("settle: %v", err)
	}

	if len(editHandler.calls) != 1 {
		t.Fatalf("edit calls=%d want=1", len(editHandler.calls))
	}
	if !strings.Contains(editHandler.calls[0].Content, "grix://card/agent_status") ||
		!strings.Contains(editHandler.calls[0].Content, "error") {
		t.Fatalf("edit content missing error status card: %s", editHandler.calls[0].Content)
	}
}

func TestSettleAgentQuestionReplyCardIgnoresNonQuestionContent(t *testing.T) {
	defer setupQuestionCardSettleTest(t)()

	evt := questionReplyEvent("evt-plain-text")
	evt.Content = "普通用户消息，不是问答卡回包"

	editHandler := &mockEditMessageHandler{}
	sendHandler := &mockSendMessageHandler{result: &SendMessageResult{MsgID: 1}}
	mgr := NewManager("", 30*time.Second, sendHandler.handle, nil, nil, nil)
	defer mgr.Shutdown()
	mgr.editMsgFn = editHandler.handle

	if err := mgr.settleAgentQuestionReplyCard(evt, EventResultPayload{
		EventID: evt.EventID,
		Status:  protocol.AgentEventResultResponded,
	}); err != nil {
		t.Fatalf("settle: %v", err)
	}
	if len(editHandler.calls) != 0 || len(sendHandler.calls) != 0 {
		t.Fatalf("unexpected card activity: edits=%d sends=%d", len(editHandler.calls), len(sendHandler.calls))
	}
}

func TestSettleAgentQuestionReplyCardFallsBackToNewMessageWhenMappingMissing(t *testing.T) {
	defer setupQuestionCardSettleTest(t)()

	evt := questionReplyEvent("evt-question-no-mapping")

	editHandler := &mockEditMessageHandler{}
	sendHandler := &mockSendMessageHandler{result: &SendMessageResult{MsgID: 9901}}
	mgr := NewManager("", 30*time.Second, sendHandler.handle, nil, nil, nil)
	defer mgr.Shutdown()
	mgr.editMsgFn = editHandler.handle

	if err := mgr.settleAgentQuestionReplyCard(evt, EventResultPayload{
		EventID: evt.EventID,
		Status:  protocol.AgentEventResultResponded,
	}); err != nil {
		t.Fatalf("settle: %v", err)
	}

	if len(editHandler.calls) != 0 {
		t.Fatalf("edit calls=%d want=0 (no card mapping)", len(editHandler.calls))
	}
	if len(sendHandler.calls) != 1 {
		t.Fatalf("send calls=%d want=1", len(sendHandler.calls))
	}
	req := sendHandler.calls[0]
	if !strings.Contains(req.Content, "grix://card/agent_status") {
		t.Fatalf("send content missing agent_status card: %s", req.Content)
	}
	if req.ClientMsgID != "local_action_question_reply_event:evt-question-no-mapping_reply" {
		t.Fatalf("client_msg_id=%q not derived from event id", req.ClientMsgID)
	}
}

func TestMarkQuestionReplyForwardedEditsCardWithWarning(t *testing.T) {
	defer setupQuestionCardSettleTest(t)()

	evt := questionReplyEvent("evt-question-forwarded")
	saveApprovalCardMsgID(context.Background(), evt.AgentID, evt.SessionID, "que-settle-1", 6603)

	editHandler := &mockEditMessageHandler{}
	mgr := NewManager("", 30*time.Second, nil, nil, nil, nil)
	defer mgr.Shutdown()
	mgr.editMsgFn = editHandler.handle

	mgr.markQuestionReplyForwarded(evt, "que-settle-1")

	if len(editHandler.calls) != 1 {
		t.Fatalf("edit calls=%d want=1", len(editHandler.calls))
	}
	if !strings.Contains(editHandler.calls[0].Content, "grix://card/agent_status") ||
		!strings.Contains(editHandler.calls[0].Content, "warning") {
		t.Fatalf("edit content missing warning status card: %s", editHandler.calls[0].Content)
	}
	if got := loadApprovalCardMsgID(context.Background(), evt.AgentID, evt.SessionID, "que-settle-1"); got != 0 {
		t.Fatalf("approval card mapping not cleaned: %d", got)
	}
}
