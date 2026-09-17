package agentapi

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/askie/grix/backend/internal/grixactions"
	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/agentscope"
	"github.com/askie/grix/backend/internal/pkg/snowflake"
	"github.com/askie/grix/backend/internal/pkg/testutil"
	"github.com/askie/grix/backend/internal/store"
	"github.com/askie/grix/backend/internal/ws/protocol"
)

func setupScopeApprovalTest(t *testing.T) (*Manager, *mockSendMessageHandler, func()) {
	t.Helper()
	previousDB := store.DB
	testDB := testutil.NewTestDB()
	store.DB = testDB.DB
	previousRedis := store.RDB
	store.RDB = testutil.NewMockRedis()
	_ = snowflake.Init(1)

	sendHandler := &mockSendMessageHandler{
		result: &SendMessageResult{MsgID: 99001, InboxSeq: 1, CreatedAt: time.Now().UnixMilli()},
	}
	mgr := NewManager("", 30*time.Second, sendHandler.handle, nil, nil, nil)
	SetGlobal(mgr)
	return mgr, sendHandler, func() {
		SetGlobal(nil)
		mgr.Shutdown()
		_ = store.RDB.Close()
		store.RDB = previousRedis
		testDB.Close()
		store.DB = previousDB
	}
}

func seedScopeApprovalAgent(t *testing.T, ownerID, agentID int64) {
	t.Helper()
	now := time.Now().UTC()
	if err := store.DB.Create(&model.User{
		ID: ownerID, Username: fmt.Sprintf("owner-%d", ownerID),
		Email: fmt.Sprintf("owner-%d@t.local", ownerID), Nickname: "Owner",
		Status: model.UserStatusActive, CreatedAt: now, UpdatedAt: now,
	}).Error; err != nil {
		t.Fatalf("create owner: %v", err)
	}
	if err := store.DB.Create(&model.Agent{
		ID: agentID, OwnerID: ownerID, AgentName: "ScopeAgent",
		ProviderType: model.AgentProviderAPI, AgentClientType: model.AgentClientTypeClaude,
		Status: model.AgentStatusActive, CreatedAt: now, UpdatedAt: now,
	}).Error; err != nil {
		t.Fatalf("create agent: %v", err)
	}
}

func TestScopeMissingNotifiesOwnerAndReturnsGuidance(t *testing.T) {
	_, sendHandler, cleanup := setupScopeApprovalTest(t)
	defer cleanup()
	const ownerID, agentID = int64(9101), int64(9201)
	seedScopeApprovalAgent(t, ownerID, agentID)

	_, code, msg := dispatchAgentInvoke(agentID, ownerID, "group_create", map[string]interface{}{})
	if code != 4003 {
		t.Fatalf("code=%d want 4003", code)
	}
	if !strings.Contains(msg, "Do not retry") || !strings.Contains(msg, agentscope.ScopeGroupCreate) {
		t.Fatalf("msg=%q", msg)
	}
	if len(sendHandler.calls) != 1 || !strings.Contains(sendHandler.calls[0].Content, "scope%3A") {
		t.Fatalf("expected scope approval card, calls=%d", len(sendHandler.calls))
	}
}

func TestScopeMissingDuplicateDoesNotResendCard(t *testing.T) {
	_, sendHandler, cleanup := setupScopeApprovalTest(t)
	defer cleanup()
	const ownerID, agentID = int64(9102), int64(9202)
	seedScopeApprovalAgent(t, ownerID, agentID)

	_, _, _ = dispatchAgentInvoke(agentID, ownerID, "group_create", map[string]interface{}{})
	_, _, msg2 := dispatchAgentInvoke(agentID, ownerID, "group_create", map[string]interface{}{})
	if len(sendHandler.calls) != 1 {
		t.Fatalf("send calls=%d want 1", len(sendHandler.calls))
	}
	if !strings.Contains(msg2, "Do not retry") {
		t.Fatalf("second msg=%q", msg2)
	}
}

func TestScopeDeniedStickySilence(t *testing.T) {
	_, sendHandler, cleanup := setupScopeApprovalTest(t)
	defer cleanup()
	const ownerID, agentID = int64(9103), int64(9203)
	seedScopeApprovalAgent(t, ownerID, agentID)
	scope := agentscope.ScopeGroupCreate

	_, _, _ = dispatchAgentInvoke(agentID, ownerID, "group_create", map[string]interface{}{})
	evt := DelegateEventPayload{
		EventID: "evt-scope-deny", AgentID: agentID, OwnerID: ownerID,
		SessionID: "thread", MsgID: 1, SenderID: ownerID,
		Content: grixactions.BuildQuestionReplyURI(grixactions.QuestionReply{
			RequestID: fmt.Sprintf("scope:%d:%s", agentID, scope),
			Response:  map[string]any{"type": "single", "value": "Deny"},
		}),
	}
	mgr := GetGlobal()
	if !mgr.tryHandleScopeApprovalReply(evt) {
		t.Fatal("expected scope deny handled")
	}
	sendHandler.calls = nil
	_, code, msg := dispatchAgentInvoke(agentID, ownerID, "group_create", map[string]interface{}{})
	if code != 4003 {
		t.Fatalf("code=%d", code)
	}
	if strings.Contains(msg, "Do not retry") {
		t.Fatalf("denied window should fall back to base msg, got %q", msg)
	}
	if len(sendHandler.calls) != 0 {
		t.Fatalf("expected no card during denied window")
	}
}

func TestScopeApprovalNonOwnerRejected(t *testing.T) {
	mgr, _, cleanup := setupScopeApprovalTest(t)
	defer cleanup()
	const ownerID, agentID, otherID = int64(9104), int64(9204), int64(9105)
	seedScopeApprovalAgent(t, ownerID, agentID)
	now := time.Now().UTC()
	_ = store.DB.Create(&model.User{ID: otherID, Username: "other", Email: "other@t.local", Status: model.UserStatusActive, CreatedAt: now, UpdatedAt: now}).Error

	evt := DelegateEventPayload{
		AgentID: agentID, OwnerID: ownerID, SessionID: "t", MsgID: 2, SenderID: otherID,
		Content: grixactions.BuildQuestionReplyURI(grixactions.QuestionReply{
			RequestID: fmt.Sprintf("scope:%d:%s", agentID, agentscope.ScopeGroupCreate),
			Response:  map[string]any{"type": "single", "value": "Allow"},
		}),
	}
	if !mgr.tryHandleScopeApprovalReply(evt) {
		t.Fatal("handler should consume event")
	}
	var count int64
	store.DB.Model(&model.AgentAPIScope{}).Where("agent_id = ? AND scope = ?", agentID, agentscope.ScopeGroupCreate).Count(&count)
	if count != 0 {
		t.Fatalf("scope row count=%d want 0", count)
	}
}

func TestScopeApprovalAllowIdempotent(t *testing.T) {
	mgr, _, cleanup := setupScopeApprovalTest(t)
	defer cleanup()
	const ownerID, agentID = int64(9106), int64(9206)
	seedScopeApprovalAgent(t, ownerID, agentID)
	scope := agentscope.ScopeGroupCreate
	_, _, _ = dispatchAgentInvoke(agentID, ownerID, "group_create", map[string]interface{}{})

	allow := func() {
		evt := DelegateEventPayload{
			AgentID: agentID, OwnerID: ownerID, SessionID: "t", MsgID: time.Now().UnixNano(), SenderID: ownerID,
			Content: grixactions.BuildQuestionReplyURI(grixactions.QuestionReply{
				RequestID: fmt.Sprintf("scope:%d:%s", agentID, scope),
				Response:  map[string]any{"type": "single", "value": "Allow"},
			}),
		}
		if !mgr.tryHandleScopeApprovalReply(evt) {
			t.Fatal("allow not handled")
		}
	}
	allow()
	var count int64
	store.DB.Model(&model.AgentAPIScope{}).Where("agent_id = ? AND scope = ?", agentID, scope).Count(&count)
	if count != 1 {
		t.Fatalf("count=%d want 1", count)
	}
	allow()
	store.DB.Model(&model.AgentAPIScope{}).Where("agent_id = ? AND scope = ?", agentID, scope).Count(&count)
	if count != 1 {
		t.Fatalf("duplicate allow count=%d want 1", count)
	}
}

func TestScopePendingCapBlocksNewCards(t *testing.T) {
	_, sendHandler, cleanup := setupScopeApprovalTest(t)
	defer cleanup()
	const ownerID, agentID = int64(9107), int64(9207)
	seedScopeApprovalAgent(t, ownerID, agentID)
	ctx := context.Background()
	if err := store.RDB.Set(ctx, scopeApprovalCountKey(agentID), maxPendingScopeApprovalsPerAgent, scopeApprovalPendingTTL).Err(); err != nil {
		t.Fatalf("seed cap: %v", err)
	}
	sendHandler.calls = nil
	_, code, msg := dispatchAgentInvoke(agentID, ownerID, "agent_category_list", map[string]interface{}{})
	if code != 4003 {
		t.Fatalf("code=%d", code)
	}
	if strings.Contains(msg, "Do not retry") {
		t.Fatalf("cap hit should use base msg, got %q", msg)
	}
	if len(sendHandler.calls) != 0 {
		t.Fatalf("expected no new card at cap")
	}
}

func TestScopeWakeWithUniqueActiveRun(t *testing.T) {
	mgr, sendHandler, cleanup := setupScopeApprovalTest(t)
	defer cleanup()
	const ownerID, agentID = int64(9108), int64(9208)
	seedScopeApprovalAgent(t, ownerID, agentID)
	scope := agentscope.ScopeGroupCreate
	const sessionID = "group-wake"

	conn := &agentConn{
		agentID:  agentID,
		ownerID:  ownerID,
		clientID: "scope-wake-test",
		send:     make(chan []byte, 4),
	}
	mgr.putConnForTest(conn)

	mgr.registerActiveRun(DelegateEventPayload{
		EventID: "run-scope-wake", AgentID: agentID, OwnerID: ownerID,
		SessionID: sessionID, SessionType: model.SessionTypeGroup,
		MsgID: 5001, SenderID: ownerID, Content: "go",
	})
	_, _, _ = dispatchAgentInvoke(agentID, ownerID, "group_create", map[string]interface{}{})

	evt := DelegateEventPayload{
		AgentID: agentID, OwnerID: ownerID, SessionID: "approval-thread", MsgID: 9, SenderID: ownerID,
		Content: grixactions.BuildQuestionReplyURI(grixactions.QuestionReply{
			RequestID: fmt.Sprintf("scope:%d:%s", agentID, scope),
			Response:  map[string]any{"type": "single", "value": "Allow"},
		}),
	}
	if !mgr.tryHandleScopeApprovalReply(evt) {
		t.Fatal("allow not handled")
	}
	var wakeCalls int
	for _, call := range sendHandler.calls {
		if strings.Contains(call.Content, "granted") || strings.Contains(call.ClientMsgID, "scope_approval_wake") {
			wakeCalls++
		}
		if call.SessionID == sessionID && len(call.VisibleTo) == 1 && call.VisibleTo[0] == ownerID {
			// ok
		}
	}
	if wakeCalls == 0 {
		t.Fatalf("expected wake message in group, calls=%+v", sendHandler.calls)
	}

	select {
	case data := <-conn.send:
		var pkt protocol.Packet
		if err := json.Unmarshal(data, &pkt); err != nil {
			t.Fatalf("unmarshal packet: %v", err)
		}
		var payload map[string]any
		if err := json.Unmarshal(pkt.Payload, &payload); err != nil {
			t.Fatalf("unmarshal payload: %v", err)
		}
		eventID, _ := payload["event_id"].(string)
		if strings.Split(eventID, ":")[0] != sessionID {
			t.Fatalf("event_id=%q first segment want session %q", eventID, sessionID)
		}
	default:
		t.Fatal("expected scope wake delegate event on agent conn")
	}
}

func TestScopeApprovalAllowRejectsInvalidScope(t *testing.T) {
	mgr, _, cleanup := setupScopeApprovalTest(t)
	defer cleanup()
	const ownerID, agentID = int64(9110), int64(9210)
	seedScopeApprovalAgent(t, ownerID, agentID)
	const invalidScope = "group.unknown.scope"

	evt := DelegateEventPayload{
		AgentID: agentID, OwnerID: ownerID, SessionID: "t", MsgID: 12, SenderID: ownerID,
		Content: grixactions.BuildQuestionReplyURI(grixactions.QuestionReply{
			RequestID: fmt.Sprintf("scope:%d:%s", agentID, invalidScope),
			Response:  map[string]any{"type": "single", "value": "Allow"},
		}),
	}
	if !mgr.tryHandleScopeApprovalReply(evt) {
		t.Fatal("expected handler to consume reply")
	}
	var count int64
	store.DB.Model(&model.AgentAPIScope{}).Where("agent_id = ? AND scope = ?", agentID, invalidScope).Count(&count)
	if count != 0 {
		t.Fatalf("invalid scope must not be granted, count=%d", count)
	}
}

func TestScopeApprovalCardClientMsgIDUniquePerPending(t *testing.T) {
	_, sendHandler, cleanup := setupScopeApprovalTest(t)
	defer cleanup()
	const ownerID, agentID = int64(9111), int64(9211)
	seedScopeApprovalAgent(t, ownerID, agentID)
	scope := agentscope.ScopeGroupCreate

	_, _, _ = dispatchAgentInvoke(agentID, ownerID, "group_create", map[string]interface{}{})
	if len(sendHandler.calls) != 1 {
		t.Fatalf("calls=%d want 1", len(sendHandler.calls))
	}
	firstID := sendHandler.calls[0].ClientMsgID

	ctx := context.Background()
	clearScopeApprovalPending(ctx, agentID, scope)
	sendHandler.calls = nil

	_, _, _ = dispatchAgentInvoke(agentID, ownerID, "group_create", map[string]interface{}{})
	if len(sendHandler.calls) != 1 {
		t.Fatalf("second calls=%d want 1", len(sendHandler.calls))
	}
	secondID := sendHandler.calls[0].ClientMsgID
	if firstID == secondID {
		t.Fatalf("ClientMsgID must differ across pending rounds: %q", firstID)
	}
}

func TestScopeAllowWithoutUniqueRunDoesNotWake(t *testing.T) {
	mgr, sendHandler, cleanup := setupScopeApprovalTest(t)
	defer cleanup()
	const ownerID, agentID = int64(9109), int64(9209)
	seedScopeApprovalAgent(t, ownerID, agentID)
	scope := agentscope.ScopeGroupCreate

	_, _, _ = dispatchAgentInvoke(agentID, ownerID, "group_create", map[string]interface{}{})
	before := len(sendHandler.calls)

	evt := DelegateEventPayload{
		AgentID: agentID, OwnerID: ownerID, SessionID: "t", MsgID: 11, SenderID: ownerID,
		Content: grixactions.BuildQuestionReplyURI(grixactions.QuestionReply{
			RequestID: fmt.Sprintf("scope:%d:%s", agentID, scope),
			Response:  map[string]any{"type": "single", "value": "Allow"},
		}),
	}
	if !mgr.tryHandleScopeApprovalReply(evt) {
		t.Fatal("allow not handled")
	}
	for _, call := range sendHandler.calls[before:] {
		if strings.Contains(call.ClientMsgID, "scope_approval_wake") {
			t.Fatalf("unexpected wake without active run")
		}
	}
}
