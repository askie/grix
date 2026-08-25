package agentapi

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/askie/grix/backend/internal/claudeaccess"
	"github.com/askie/grix/backend/internal/grixactions"
	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/snowflake"
	"github.com/askie/grix/backend/internal/pkg/testutil"
	"github.com/askie/grix/backend/internal/store"
)

func setupAccessApprovalTest(t *testing.T) (*Manager, *mockSendMessageHandler, func()) {
	t.Helper()
	previousDB := store.DB
	testDB := testutil.NewTestDB()
	store.DB = testDB.DB
	previousRedis := store.RDB
	store.RDB = testutil.NewMockRedis()
	_ = snowflake.Init(1)

	sendHandler := &mockSendMessageHandler{
		result: &SendMessageResult{MsgID: 88001, InboxSeq: 1, CreatedAt: time.Now().UnixMilli()},
	}
	mgr := NewManager("", 30*time.Second, sendHandler.handle, nil, nil, nil)
	return mgr, sendHandler, func() {
		mgr.Shutdown()
		_ = store.RDB.Close()
		store.RDB = previousRedis
		testDB.Close()
		store.DB = previousDB
	}
}

func seedAccessApprovalAgent(t *testing.T, ownerID, agentID, strangerID int64) {
	t.Helper()
	now := time.Now().UTC()
	if err := store.DB.Create(&model.User{ID: ownerID, Username: fmt.Sprintf("owner-%d", ownerID), Email: fmt.Sprintf("owner-%d@t.local", ownerID), Nickname: "老板", Status: model.UserStatusActive, CreatedAt: now, UpdatedAt: now}).Error; err != nil {
		t.Fatalf("create owner error: %v", err)
	}
	if err := store.DB.Create(&model.User{ID: strangerID, Username: fmt.Sprintf("stranger-%d", strangerID), Email: fmt.Sprintf("stranger-%d@t.local", strangerID), Nickname: "访客甲", Status: model.UserStatusActive, CreatedAt: now, UpdatedAt: now}).Error; err != nil {
		t.Fatalf("create stranger error: %v", err)
	}
	if err := store.DB.Create(&model.Agent{
		ID: agentID, OwnerID: ownerID, AgentName: "GateAgent",
		ProviderType: model.AgentProviderAPI, AgentClientType: model.AgentClientTypeClaude,
		Status: model.AgentStatusActive, CreatedAt: now, UpdatedAt: now,
	}).Error; err != nil {
		t.Fatalf("create agent error: %v", err)
	}
}

// pendGroupAccessRequest 通过真实门禁评估产生一条待批申请，返回申请码。
func pendGroupAccessRequest(t *testing.T, agentID int64, senderID int64, groupSessionID string) string {
	t.Helper()
	if _, err := claudeaccess.AllowSender(context.Background(), agentID, "seed-allowed"); err != nil {
		t.Fatalf("AllowSender error: %v", err)
	}
	result, err := claudeaccess.EvaluateInbound(context.Background(), agentID, fmt.Sprintf("%d", senderID), groupSessionID, 2)
	if err != nil {
		t.Fatalf("EvaluateInbound error: %v", err)
	}
	if result.Reason != "pairing_required" || result.PairingCode == "" {
		t.Fatalf("expected pairing_required with code, got %+v", result)
	}
	return result.PairingCode
}

func TestNotifyGroupAccessApprovalSendsQuestionCardToOwnerThread(t *testing.T) {
	mgr, sendHandler, cleanup := setupAccessApprovalTest(t)
	defer cleanup()

	const ownerID, agentID, strangerID = int64(8901), int64(9901), int64(8902)
	seedAccessApprovalAgent(t, ownerID, agentID, strangerID)
	code := pendGroupAccessRequest(t, agentID, strangerID, "group-1")

	mgr.notifyGroupAccessApproval(agentID, ownerID, strangerID, "group-1", code)

	if len(sendHandler.calls) != 1 {
		t.Fatalf("send calls = %d, want 1", len(sendHandler.calls))
	}
	card := sendHandler.calls[0]
	if card.SessionID == "group-1" || strings.TrimSpace(card.SessionID) == "" {
		t.Fatalf("approval card must go to the owner thread, got session %q", card.SessionID)
	}
	if !strings.Contains(card.Content, "grix://card/agent_question?") {
		t.Fatalf("expected agent_question card, got %q", card.Content)
	}
	if !strings.Contains(card.Content, "access%3A") && !strings.Contains(card.Content, "access:") {
		t.Fatalf("card should carry access request_id, got %q", card.Content)
	}
	if !strings.Contains(card.Content, "%E8%AE%BF%E5%AE%A2%E7%94%B2") { // URL 编码的「访客甲」
		t.Fatalf("card should mention the requester nickname, got %q", card.Content)
	}
}

func TestAccessApprovalReplyAllowAddsAllowlist(t *testing.T) {
	mgr, sendHandler, cleanup := setupAccessApprovalTest(t)
	defer cleanup()

	const ownerID, agentID, strangerID = int64(8911), int64(9911), int64(8912)
	seedAccessApprovalAgent(t, ownerID, agentID, strangerID)
	code := pendGroupAccessRequest(t, agentID, strangerID, "group-2")

	evt := DelegateEventPayload{
		EventID:   "evt-access-allow",
		EventType: "user_chat",
		AgentID:   agentID,
		OwnerID:   ownerID,
		SessionID: "approval-thread",
		MsgID:     70001,
		SenderID:  ownerID,
		Content: grixactions.BuildQuestionReplyURI(grixactions.QuestionReply{
			RequestID: fmt.Sprintf("access:%d:%s", agentID, code),
			Response:  map[string]any{"type": "single", "value": "允许"},
		}),
	}
	if handled := mgr.tryHandleAccessApprovalReply(evt); !handled {
		t.Fatal("reply should be intercepted")
	}

	status, err := claudeaccess.GetStatus(context.Background(), agentID)
	if err != nil {
		t.Fatalf("GetStatus error: %v", err)
	}
	if status.AllowlistCount != 2 {
		t.Fatalf("AllowlistCount = %d, want 2 (seed + approved)", status.AllowlistCount)
	}
	if status.PendingPairCount != 0 {
		t.Fatalf("PendingPairCount = %d, want 0", status.PendingPairCount)
	}
	if len(sendHandler.calls) != 1 || !strings.Contains(sendHandler.calls[0].Content, "grix://card/agent_status?") {
		t.Fatalf("expected one status card, got %+v", sendHandler.calls)
	}
}

// 英文界面的主人收到的是 "Allow"/"Deny" 选项值，回传同样必须被识别。
func TestAccessApprovalReplyAcceptsEnglishOptionValue(t *testing.T) {
	mgr, sendHandler, cleanup := setupAccessApprovalTest(t)
	defer cleanup()

	const ownerID, agentID, strangerID = int64(8961), int64(9961), int64(8962)
	seedAccessApprovalAgent(t, ownerID, agentID, strangerID)
	code := pendGroupAccessRequest(t, agentID, strangerID, "group-6")

	evt := DelegateEventPayload{
		EventID:   "evt-access-allow-en",
		EventType: "user_chat",
		AgentID:   agentID,
		OwnerID:   ownerID,
		SessionID: "approval-thread",
		MsgID:     70006,
		SenderID:  ownerID,
		Content: grixactions.BuildQuestionReplyURI(grixactions.QuestionReply{
			RequestID: fmt.Sprintf("access:%d:%s", agentID, code),
			Response:  map[string]any{"type": "single", "value": "Allow"},
		}),
	}
	if handled := mgr.tryHandleAccessApprovalReply(evt); !handled {
		t.Fatal("reply should be intercepted")
	}

	status, err := claudeaccess.GetStatus(context.Background(), agentID)
	if err != nil {
		t.Fatalf("GetStatus error: %v", err)
	}
	if status.AllowlistCount != 2 {
		t.Fatalf("AllowlistCount = %d, want 2 (seed + approved)", status.AllowlistCount)
	}
	if len(sendHandler.calls) != 1 || !strings.Contains(sendHandler.calls[0].Content, "grix://card/agent_status?") {
		t.Fatalf("expected one status card, got %+v", sendHandler.calls)
	}
}

func TestAccessApprovalReplyDenyKeepsAllowlist(t *testing.T) {
	mgr, sendHandler, cleanup := setupAccessApprovalTest(t)
	defer cleanup()

	const ownerID, agentID, strangerID = int64(8921), int64(9921), int64(8922)
	seedAccessApprovalAgent(t, ownerID, agentID, strangerID)
	code := pendGroupAccessRequest(t, agentID, strangerID, "group-3")

	evt := DelegateEventPayload{
		EventID: "evt-access-deny", EventType: "user_chat",
		AgentID: agentID, OwnerID: ownerID, SessionID: "approval-thread",
		MsgID: 70002, SenderID: ownerID,
		Content: grixactions.BuildQuestionReplyURI(grixactions.QuestionReply{
			RequestID: fmt.Sprintf("access:%d:%s", agentID, code),
			Response:  map[string]any{"type": "single", "value": "拒绝"},
		}),
	}
	if handled := mgr.tryHandleAccessApprovalReply(evt); !handled {
		t.Fatal("reply should be intercepted")
	}

	status, _ := claudeaccess.GetStatus(context.Background(), agentID)
	if status.AllowlistCount != 1 {
		t.Fatalf("AllowlistCount = %d, want 1 (seed only)", status.AllowlistCount)
	}
	if status.PendingPairCount != 0 {
		t.Fatalf("PendingPairCount = %d, want 0 (denied removes pending)", status.PendingPairCount)
	}
	if len(sendHandler.calls) != 1 {
		t.Fatalf("expected one status card, got %d", len(sendHandler.calls))
	}
}

func TestAccessApprovalReplyRejectsNonOwner(t *testing.T) {
	mgr, _, cleanup := setupAccessApprovalTest(t)
	defer cleanup()

	const ownerID, agentID, strangerID = int64(8931), int64(9931), int64(8932)
	seedAccessApprovalAgent(t, ownerID, agentID, strangerID)
	code := pendGroupAccessRequest(t, agentID, strangerID, "group-4")

	evt := DelegateEventPayload{
		EventID: "evt-access-forge", EventType: "user_chat",
		AgentID: agentID, OwnerID: ownerID, SessionID: "approval-thread",
		MsgID: 70003, SenderID: strangerID, // 非主人伪造回传
		Content: grixactions.BuildQuestionReplyURI(grixactions.QuestionReply{
			RequestID: fmt.Sprintf("access:%d:%s", agentID, code),
			Response:  map[string]any{"type": "single", "value": "允许"},
		}),
	}
	if handled := mgr.tryHandleAccessApprovalReply(evt); !handled {
		t.Fatal("forged reply should still be swallowed")
	}

	status, _ := claudeaccess.GetStatus(context.Background(), agentID)
	if status.AllowlistCount != 1 {
		t.Fatalf("AllowlistCount = %d, want 1 (forged approval must not apply)", status.AllowlistCount)
	}
	if status.PendingPairCount != 1 {
		t.Fatalf("PendingPairCount = %d, want 1 (request stays pending)", status.PendingPairCount)
	}
}

func TestAccessApprovalReplyIgnoresForeignRequestIDs(t *testing.T) {
	mgr, _, cleanup := setupAccessApprovalTest(t)
	defer cleanup()

	evt := DelegateEventPayload{
		EventID: "evt-foreign", EventType: "user_chat",
		AgentID: 9941, OwnerID: 8941, SessionID: "some-thread",
		MsgID: 70004, SenderID: 8941,
		Content: grixactions.BuildQuestionReplyURI(grixactions.QuestionReply{
			RequestID: "gemini-question-1",
			Response:  map[string]any{"type": "single", "value": "yes"},
		}),
	}
	if handled := mgr.tryHandleAccessApprovalReply(evt); handled {
		t.Fatal("non-access request_id must pass through to other handlers")
	}
}

func TestAccessApprovalReplyExactMatchRejectsNegatedAnswer(t *testing.T) {
	mgr, sendHandler, cleanup := setupAccessApprovalTest(t)
	defer cleanup()

	const ownerID, agentID, strangerID = int64(8951), int64(9951), int64(8952)
	seedAccessApprovalAgent(t, ownerID, agentID, strangerID)
	code := pendGroupAccessRequest(t, agentID, strangerID, "group-5")

	// 「不允许」含「允许」子串：必须不被当成批准，也不当成拒绝，回提示卡请重选。
	evt := DelegateEventPayload{
		EventID: "evt-access-negated", EventType: "user_chat",
		AgentID: agentID, OwnerID: ownerID, SessionID: "approval-thread",
		MsgID: 70005, SenderID: ownerID,
		Content: grixactions.BuildQuestionReplyURI(grixactions.QuestionReply{
			RequestID: fmt.Sprintf("access:%d:%s", agentID, code),
			Response:  map[string]any{"type": "single", "value": "不允许"},
		}),
	}
	if handled := mgr.tryHandleAccessApprovalReply(evt); !handled {
		t.Fatal("reply should be intercepted")
	}

	status, _ := claudeaccess.GetStatus(context.Background(), agentID)
	if status.AllowlistCount != 1 {
		t.Fatalf("AllowlistCount = %d, want 1 (negated answer must NOT approve)", status.AllowlistCount)
	}
	if status.PendingPairCount != 1 {
		t.Fatalf("PendingPairCount = %d, want 1 (request stays pending)", status.PendingPairCount)
	}
	if len(sendHandler.calls) != 1 || !strings.Contains(sendHandler.calls[0].Content, "grix://card/agent_status?") {
		t.Fatalf("expected a warning status card, got %+v", sendHandler.calls)
	}
}

func TestAccessApprovalMalformedReplySwallowedNotLeaked(t *testing.T) {
	mgr, sendHandler, cleanup := setupAccessApprovalTest(t)
	defer cleanup()

	// matched=true 但缺 response（err!=nil），且内容属于 access 命名空间：必须就地吞掉，
	// 不能漏给 gemini/claude 的 question 流。
	evt := DelegateEventPayload{
		EventID: "evt-access-malformed", EventType: "user_chat",
		AgentID: 9961, OwnerID: 8961, SessionID: "approval-thread",
		MsgID: 70006, SenderID: 8961,
		Content: `grix://card/agent_question_reply?d=%7B%22request_id%22%3A%22access%3A9961%3AABCDEF%22%7D`,
	}
	if handled := mgr.tryHandleAccessApprovalReply(evt); !handled {
		t.Fatal("malformed access reply must be swallowed, not leaked to other question flows")
	}
	if len(sendHandler.calls) != 1 || !strings.Contains(sendHandler.calls[0].Content, "grix://card/agent_status?") {
		t.Fatalf("expected a warning status card, got %+v", sendHandler.calls)
	}
}

func TestNotifyGroupAccessApprovalReturnsFalseWithoutManagerSendFn(t *testing.T) {
	_, _, cleanup := setupAccessApprovalTest(t)
	defer cleanup()

	// 全局 Manager 缺失时必须返回 false，让门禁层回滚待批记录。
	previous := GetGlobal()
	SetGlobal(nil)
	defer SetGlobal(previous)
	if NotifyGroupAccessApproval(1, 2, 3, "g", "CODE12") {
		t.Fatal("notify without manager must report failure")
	}
}
