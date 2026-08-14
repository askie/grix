package llm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"testing"
	"time"

	"github.com/askie/grix/backend/config"
	"github.com/askie/grix/backend/internal/llm/provider"
	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/snowflake"
	"github.com/askie/grix/backend/internal/pkg/testutil"
	"github.com/askie/grix/backend/internal/store"
	"github.com/askie/grix/backend/internal/ws/agentmsg"
)

type mockStreamProvider struct {
	chunks []provider.StreamChunk
	err    error
}

func (m *mockStreamProvider) Name() string { return "mock" }

func (m *mockStreamProvider) StreamChat(
	_ context.Context,
	_ *provider.Request,
	callback provider.StreamCallback,
) error {
	for _, c := range m.chunks {
		callback(c)
	}
	return m.err
}

type scriptedStreamCall struct {
	chunks []provider.StreamChunk
	err    error
}

type scriptedStreamProvider struct {
	calls int
	plan  []scriptedStreamCall
}

func (m *scriptedStreamProvider) Name() string { return "scripted" }

func (m *scriptedStreamProvider) StreamChat(
	_ context.Context,
	_ *provider.Request,
	callback provider.StreamCallback,
) error {
	if len(m.plan) == 0 {
		return nil
	}
	index := m.calls
	if index >= len(m.plan) {
		index = len(m.plan) - 1
	}
	m.calls++
	step := m.plan[index]
	for _, c := range step.chunks {
		callback(c)
	}
	return step.err
}

type cancelAwareProvider struct {
	started chan struct{}
	stopped chan struct{}
}

func (p *cancelAwareProvider) Name() string { return "cancel-aware" }

func (p *cancelAwareProvider) StreamChat(
	ctx context.Context,
	_ *provider.Request,
	callback provider.StreamCallback,
) error {
	if p.started != nil {
		close(p.started)
	}
	callback(provider.StreamChunk{DeltaContent: "partial"})
	<-ctx.Done()
	if p.stopped != nil {
		close(p.stopped)
	}
	return ctx.Err()
}

func setupOrchestratorTest(t *testing.T) func() {
	t.Helper()

	testDB := testutil.NewTestDB()
	store.DB = testDB.DB
	store.RDB = testutil.NewMockRedis()

	if err := store.DB.AutoMigrate(&model.UserInbox{}); err != nil {
		t.Fatalf("auto migrate user_inbox error: %v", err)
	}
	if err := snowflake.Init(1); err != nil {
		t.Fatalf("snowflake init error: %v", err)
	}

	return func() {
		_ = store.RDB.Close()
		testDB.Close()
	}
}

func TestEnqueueAIInboxCreatesEntriesForHumanMembers(t *testing.T) {
	cleanup := setupOrchestratorTest(t)
	defer cleanup()

	ctx := context.Background()
	sessionID := "sess-ai-1"
	msgID := int64(9001)

	members := []model.SessionMember{
		{SessionID: sessionID, MemberID: 1001, MemberType: 1},
		{SessionID: sessionID, MemberID: 1002, MemberType: 1},
		{SessionID: sessionID, MemberID: 2001, MemberType: 2},
	}
	for _, m := range members {
		if err := store.DB.Create(&m).Error; err != nil {
			t.Fatalf("create session member error: %v", err)
		}
	}

	o := &Orchestrator{}
	_ = o // tests agentmsg.EnqueueStreamInbox directly
	agentmsg.EnqueueStreamInbox(ctx, sessionID, msgID, 0, nil)

	for _, userID := range []int64{1001, 1002} {
		var inbox model.UserInbox
		if err := store.DB.Where(
			"user_id = ? AND msg_id = ? AND session_id = ?",
			userID, msgID, sessionID,
		).First(&inbox).Error; err != nil {
			t.Fatalf("expected inbox row for user %d: %v", userID, err)
		}
		if inbox.InboxSeq != 1 {
			t.Fatalf("unexpected inbox_seq for user %d: got=%d want=1", userID, inbox.InboxSeq)
		}

		unread, err := store.RDB.HGet(ctx, fmt.Sprintf("im:unread:%d", userID), sessionID).Result()
		if err != nil {
			t.Fatalf("HGet unread error for user %d: %v", userID, err)
		}
		if unread != "1" {
			t.Fatalf("unexpected unread count for user %d: got=%s want=1", userID, unread)
		}

		var member model.SessionMember
		if err := store.DB.Where(
			"session_id = ? AND member_id = ?",
			sessionID, userID,
		).First(&member).Error; err != nil {
			t.Fatalf("query session member error user=%d: %v", userID, err)
		}
		if member.UnreadCount != 1 {
			t.Fatalf("unexpected db unread_count for user %d: got=%d want=1", userID, member.UnreadCount)
		}
	}

	var agentInboxCount int64
	if err := store.DB.Model(&model.UserInbox{}).
		Where("user_id = ?", int64(2001)).
		Count(&agentInboxCount).Error; err != nil {
		t.Fatalf("count inbox for agent member error: %v", err)
	}
	if agentInboxCount != 0 {
		t.Fatalf("unexpected inbox rows for agent member: got=%d want=0", agentInboxCount)
	}
}

func TestEnqueueAIInboxDeduplicatesExistingRows(t *testing.T) {
	cleanup := setupOrchestratorTest(t)
	defer cleanup()

	ctx := context.Background()
	sessionID := "sess-ai-dup"
	msgID := int64(9002)

	members := []model.SessionMember{
		{SessionID: sessionID, MemberID: 1101, MemberType: 1, UnreadCount: 3},
		{SessionID: sessionID, MemberID: 1102, MemberType: 1, UnreadCount: 0},
	}
	for _, m := range members {
		if err := store.DB.Create(&m).Error; err != nil {
			t.Fatalf("create session member error: %v", err)
		}
	}

	existing := model.UserInbox{
		UserID:    1101,
		InboxSeq:  7,
		MsgID:     msgID,
		SessionID: sessionID,
	}
	if err := store.DB.Create(&existing).Error; err != nil {
		t.Fatalf("seed existing inbox row error: %v", err)
	}
	if err := store.RDB.HSet(ctx, "im:unread:1101", sessionID, 3).Err(); err != nil {
		t.Fatalf("seed unread hash error: %v", err)
	}

	o := &Orchestrator{}
	_ = o
	agentmsg.EnqueueStreamInbox(ctx, sessionID, msgID, 0, nil)

	var dupCount int64
	if err := store.DB.Model(&model.UserInbox{}).
		Where("user_id = ? AND msg_id = ? AND session_id = ?", int64(1101), msgID, sessionID).
		Count(&dupCount).Error; err != nil {
		t.Fatalf("count dedupe rows error: %v", err)
	}
	if dupCount != 1 {
		t.Fatalf("expected exactly one row for deduped user: got=%d want=1", dupCount)
	}

	unread1101, err := store.RDB.HGet(ctx, "im:unread:1101", sessionID).Result()
	if err != nil {
		t.Fatalf("HGet unread for deduped user error: %v", err)
	}
	if unread1101 != "3" {
		t.Fatalf("deduped user unread should stay unchanged: got=%s want=3", unread1101)
	}

	var member1101 model.SessionMember
	if err := store.DB.Where(
		"session_id = ? AND member_id = ?",
		sessionID, int64(1101),
	).First(&member1101).Error; err != nil {
		t.Fatalf("query member1101 error: %v", err)
	}
	if member1101.UnreadCount != 3 {
		t.Fatalf("deduped user db unread_count should stay unchanged: got=%d want=3", member1101.UnreadCount)
	}

	var inbox1102 model.UserInbox
	if err := store.DB.Where(
		"user_id = ? AND msg_id = ? AND session_id = ?",
		int64(1102), msgID, sessionID,
	).First(&inbox1102).Error; err != nil {
		t.Fatalf("expected inbox row for new user: %v", err)
	}
	if inbox1102.InboxSeq != 1 {
		t.Fatalf("unexpected inbox_seq for new user: got=%d want=1", inbox1102.InboxSeq)
	}

	unread1102, err := store.RDB.HGet(ctx, "im:unread:1102", sessionID).Result()
	if err != nil {
		t.Fatalf("HGet unread for new user error: %v", err)
	}
	if unread1102 != "1" {
		t.Fatalf("unexpected unread for new user: got=%s want=1", unread1102)
	}

	var member1102 model.SessionMember
	if err := store.DB.Where(
		"session_id = ? AND member_id = ?",
		sessionID, int64(1102),
	).First(&member1102).Error; err != nil {
		t.Fatalf("query member1102 error: %v", err)
	}
	if member1102.UnreadCount != 1 {
		t.Fatalf("new user db unread_count should be 1: got=%d", member1102.UnreadCount)
	}
}

func TestEnqueueDelegateInboxKeepsSenderUnreadUnchanged(t *testing.T) {
	cleanup := setupOrchestratorTest(t)
	defer cleanup()

	ctx := context.Background()
	sessionID := "sess-delegate-1"
	msgID := int64(9901)
	senderID := int64(2101)
	recipientID := int64(2102)

	seedMembers := []model.SessionMember{
		{SessionID: sessionID, MemberID: senderID, MemberType: 1, UnreadCount: 5},
		{SessionID: sessionID, MemberID: recipientID, MemberType: 1, UnreadCount: 0},
	}
	for _, m := range seedMembers {
		if err := store.DB.Create(&m).Error; err != nil {
			t.Fatalf("seed session member error: %v", err)
		}
	}

	o := &Orchestrator{}
	_ = o
	agentmsg.EnqueueStreamInbox(ctx, sessionID, msgID, senderID, nil)

	var senderInbox model.UserInbox
	if err := store.DB.Where(
		"user_id = ? AND msg_id = ? AND session_id = ?",
		senderID, msgID, sessionID,
	).First(&senderInbox).Error; err != nil {
		t.Fatalf("sender inbox row missing: %v", err)
	}

	var recipientInbox model.UserInbox
	if err := store.DB.Where(
		"user_id = ? AND msg_id = ? AND session_id = ?",
		recipientID, msgID, sessionID,
	).First(&recipientInbox).Error; err != nil {
		t.Fatalf("recipient inbox row missing: %v", err)
	}

	var senderMember model.SessionMember
	if err := store.DB.Where(
		"session_id = ? AND member_id = ?",
		sessionID, senderID,
	).First(&senderMember).Error; err != nil {
		t.Fatalf("query sender member error: %v", err)
	}
	if senderMember.UnreadCount != 5 {
		t.Fatalf("sender unread_count changed unexpectedly: got=%d want=5", senderMember.UnreadCount)
	}

	var recipientMember model.SessionMember
	if err := store.DB.Where(
		"session_id = ? AND member_id = ?",
		sessionID, recipientID,
	).First(&recipientMember).Error; err != nil {
		t.Fatalf("query recipient member error: %v", err)
	}
	if recipientMember.UnreadCount != 1 {
		t.Fatalf("recipient unread_count mismatch: got=%d want=1", recipientMember.UnreadCount)
	}

	if exists, _ := store.RDB.Exists(ctx, fmt.Sprintf("im:unread:%d", senderID)).Result(); exists != 0 {
		t.Fatalf("sender unread redis key should not be created")
	}
	if unreadRecipient, err := store.RDB.HGet(
		ctx,
		fmt.Sprintf("im:unread:%d", recipientID),
		sessionID,
	).Result(); err != nil || unreadRecipient != "1" {
		t.Fatalf("recipient unread redis mismatch got=%s err=%v", unreadRecipient, err)
	}
}

func TestProcessDelegateRequestStreamsAndPersistsMessage(t *testing.T) {
	cleanup := setupOrchestratorTest(t)
	defer cleanup()

	ctx := context.Background()
	sessionID := "sess-delegate-stream"
	ownerID := int64(3101)
	peerID := int64(3102)
	agentID := int64(4101)

	if err := store.DB.Create(&model.Session{
		SessionID:   sessionID,
		OwnerID:     ownerID,
		SessionType: 1,
	}).Error; err != nil {
		t.Fatalf("create session error: %v", err)
	}

	members := []model.SessionMember{
		{SessionID: sessionID, MemberID: ownerID, MemberType: 1, UnreadCount: 0},
		{SessionID: sessionID, MemberID: peerID, MemberType: 1, UnreadCount: 0},
	}
	for _, m := range members {
		if err := store.DB.Create(&m).Error; err != nil {
			t.Fatalf("create session member error: %v", err)
		}
	}

	if err := store.DB.Create(&model.Agent{
		ID:            agentID,
		AgentName:     "delegate-bot",
		ModelProvider: "mock",
		SystemPrompt:  "be helpful",
		OwnerID:       ownerID,
	}).Error; err != nil {
		t.Fatalf("create agent error: %v", err)
	}
	if err := store.RDB.HSet(
		ctx,
		fmt.Sprintf("im:delegate:%s:%d", sessionID, ownerID),
		"agent_id",
		fmt.Sprintf("%d", agentID),
	).Err(); err != nil {
		t.Fatalf("seed delegate state error: %v", err)
	}

	o := NewOrchestrator(map[string]provider.Provider{
		"mock": &mockStreamProvider{
			chunks: []provider.StreamChunk{
				{DeltaContent: "Hello ", PromptTokens: 12},
				{DeltaContent: "delegate", IsFinish: true, CompletionTokens: 21},
			},
		},
	})

	o.processDelegateRequest(ctx, &AIRequest{
		Cmd:          "delegate_request",
		SessionID:    sessionID,
		OwnerID:      ownerID,
		AgentID:      agentID,
		Content:      "say hi",
		TriggerMsgID: 7001,
	})

	var msg model.Message
	if err := store.DB.Where(
		"session_id = ? AND sender_id = ? AND sender_type = ?",
		sessionID, ownerID, 1,
	).Order("created_at DESC").First(&msg).Error; err != nil {
		t.Fatalf("query delegate message error: %v", err)
	}

	if msg.MsgType != 1 {
		t.Fatalf("delegate message should be finalized text, got msg_type=%d", msg.MsgType)
	}
	if msg.Content != "Hello delegate" {
		t.Fatalf("delegate message content mismatch: got=%q", msg.Content)
	}

	var extra map[string]interface{}
	if err := json.Unmarshal(msg.Extra, &extra); err != nil {
		t.Fatalf("unmarshal delegate message extra error: %v", err)
	}
	if v, ok := extra["delegate_origin"].(bool); !ok || !v {
		t.Fatalf("delegate message extra.delegate_origin should be true, got=%v", extra["delegate_origin"])
	}

	var inboxOwner model.UserInbox
	if err := store.DB.Where(
		"user_id = ? AND msg_id = ? AND session_id = ?",
		ownerID, msg.MsgID, sessionID,
	).First(&inboxOwner).Error; err != nil {
		t.Fatalf("owner inbox row missing: %v", err)
	}

	var inboxPeer model.UserInbox
	if err := store.DB.Where(
		"user_id = ? AND msg_id = ? AND session_id = ?",
		peerID, msg.MsgID, sessionID,
	).First(&inboxPeer).Error; err != nil {
		t.Fatalf("peer inbox row missing: %v", err)
	}

	var ownerMember model.SessionMember
	if err := store.DB.Where(
		"session_id = ? AND member_id = ?",
		sessionID, ownerID,
	).First(&ownerMember).Error; err != nil {
		t.Fatalf("query owner session_member error: %v", err)
	}
	if ownerMember.UnreadCount != 0 {
		t.Fatalf("owner unread_count should stay 0, got=%d", ownerMember.UnreadCount)
	}

	var peerMember model.SessionMember
	if err := store.DB.Where(
		"session_id = ? AND member_id = ?",
		sessionID, peerID,
	).First(&peerMember).Error; err != nil {
		t.Fatalf("query peer session_member error: %v", err)
	}
	if peerMember.UnreadCount != 1 {
		t.Fatalf("peer unread_count should be 1, got=%d", peerMember.UnreadCount)
	}

	if unread, err := store.RDB.HGet(ctx, fmt.Sprintf("im:unread:%d", peerID), sessionID).Result(); err != nil || unread != "1" {
		t.Fatalf("peer redis unread mismatch got=%s err=%v", unread, err)
	}

	if streak, err := store.RDB.Get(ctx, fmt.Sprintf("im:delegate:streak:%s:%d", sessionID, ownerID)).Result(); err != nil || streak != "1" {
		t.Fatalf("delegate streak mismatch got=%s err=%v", streak, err)
	}

	var usageCount int64
	if err := store.DB.Model(&model.LLMUsageLog{}).
		Where("user_id = ? AND session_id = ? AND agent_id = ?", ownerID, sessionID, agentID).
		Count(&usageCount).Error; err != nil {
		t.Fatalf("query usage logs error: %v", err)
	}
	if usageCount != 1 {
		t.Fatalf("usage log count mismatch got=%d want=1", usageCount)
	}
}

func TestProcessDelegateRequestRetriesTransientErrorBeforeSuccess(t *testing.T) {
	cleanup := setupOrchestratorTest(t)
	defer cleanup()

	ctx := context.Background()
	sessionID := "sess-delegate-retry-success"
	ownerID := int64(3111)
	peerID := int64(3112)
	agentID := int64(4111)

	if err := store.DB.Create(&model.Session{
		SessionID:   sessionID,
		OwnerID:     ownerID,
		SessionType: 1,
	}).Error; err != nil {
		t.Fatalf("create session error: %v", err)
	}

	members := []model.SessionMember{
		{SessionID: sessionID, MemberID: ownerID, MemberType: 1},
		{SessionID: sessionID, MemberID: peerID, MemberType: 1},
	}
	for _, m := range members {
		if err := store.DB.Create(&m).Error; err != nil {
			t.Fatalf("create session member error: %v", err)
		}
	}

	if err := store.DB.Create(&model.Agent{
		ID:            agentID,
		AgentName:     "delegate-bot-retry",
		ModelProvider: "scripted",
		SystemPrompt:  "be helpful",
		OwnerID:       ownerID,
	}).Error; err != nil {
		t.Fatalf("create agent error: %v", err)
	}
	if err := store.RDB.HSet(
		ctx,
		fmt.Sprintf("im:delegate:%s:%d", sessionID, ownerID),
		"agent_id",
		fmt.Sprintf("%d", agentID),
	).Err(); err != nil {
		t.Fatalf("seed delegate state error: %v", err)
	}

	providerWithRetry := &scriptedStreamProvider{
		plan: []scriptedStreamCall{
			{err: errors.New("openai API error 502: bad gateway")},
			{
				chunks: []provider.StreamChunk{
					{DeltaContent: "retry ", PromptTokens: 9},
					{DeltaContent: "success", IsFinish: true, CompletionTokens: 11},
				},
			},
		},
	}
	o := NewOrchestrator(map[string]provider.Provider{
		"scripted": providerWithRetry,
	})

	o.processDelegateRequest(ctx, &AIRequest{
		Cmd:          "delegate_request",
		SessionID:    sessionID,
		OwnerID:      ownerID,
		AgentID:      agentID,
		Content:      "say hi",
		TriggerMsgID: 7002,
	})

	if providerWithRetry.calls != 2 {
		t.Fatalf("expected provider calls=2 after one retry, got=%d", providerWithRetry.calls)
	}

	var msg model.Message
	if err := store.DB.Where(
		"session_id = ? AND sender_id = ? AND sender_type = ?",
		sessionID, ownerID, 1,
	).Order("created_at DESC").First(&msg).Error; err != nil {
		t.Fatalf("query delegate message error: %v", err)
	}
	if msg.Content != "retry success" {
		t.Fatalf("delegate message content mismatch: got=%q want=%q", msg.Content, "retry success")
	}
}

func TestHandleRequestDeduplicatesDelegateByTriggerMsgID(t *testing.T) {
	cleanup := setupOrchestratorTest(t)
	defer cleanup()

	ctx := context.Background()
	sessionID := "sess-delegate-dedupe"
	ownerID := int64(3201)
	peerID := int64(3202)
	agentID := int64(4201)

	if err := store.DB.Create(&model.Session{
		SessionID:   sessionID,
		OwnerID:     ownerID,
		SessionType: 1,
	}).Error; err != nil {
		t.Fatalf("create session error: %v", err)
	}
	members := []model.SessionMember{
		{SessionID: sessionID, MemberID: ownerID, MemberType: 1},
		{SessionID: sessionID, MemberID: peerID, MemberType: 1},
	}
	for _, m := range members {
		if err := store.DB.Create(&m).Error; err != nil {
			t.Fatalf("create session member error: %v", err)
		}
	}
	if err := store.DB.Create(&model.Agent{
		ID:            agentID,
		AgentName:     "delegate-bot-dedupe",
		ModelProvider: "mock",
		SystemPrompt:  "be helpful",
		OwnerID:       ownerID,
	}).Error; err != nil {
		t.Fatalf("create agent error: %v", err)
	}
	if err := store.RDB.HSet(
		ctx,
		fmt.Sprintf("im:delegate:%s:%d", sessionID, ownerID),
		"agent_id",
		fmt.Sprintf("%d", agentID),
	).Err(); err != nil {
		t.Fatalf("seed delegate state error: %v", err)
	}

	o := NewOrchestrator(map[string]provider.Provider{
		"mock": &mockStreamProvider{
			chunks: []provider.StreamChunk{
				{DeltaContent: "dedupe ", PromptTokens: 8},
				{DeltaContent: "once", IsFinish: true, CompletionTokens: 9},
			},
		},
	})

	req := &AIRequest{
		Cmd:          "delegate_request",
		SessionID:    sessionID,
		OwnerID:      ownerID,
		AgentID:      agentID,
		Content:      "say something",
		TriggerMsgID: 88001,
	}
	o.HandleRequest(ctx, req)
	o.HandleRequest(ctx, req)

	var msgCount int64
	if err := store.DB.Model(&model.Message{}).
		Where("session_id = ? AND sender_id = ? AND sender_type = ?", sessionID, ownerID, 1).
		Count(&msgCount).Error; err != nil {
		t.Fatalf("count delegate messages error: %v", err)
	}
	if msgCount != 1 {
		t.Fatalf("delegate dedupe failed, expected 1 message got=%d", msgCount)
	}

	var usageCount int64
	if err := store.DB.Model(&model.LLMUsageLog{}).
		Where("user_id = ? AND session_id = ? AND agent_id = ?", ownerID, sessionID, agentID).
		Count(&usageCount).Error; err != nil {
		t.Fatalf("count usage logs error: %v", err)
	}
	if usageCount != 1 {
		t.Fatalf("delegate dedupe failed, expected 1 usage log got=%d", usageCount)
	}
}

func TestProcessDelegateRequestSkipsWhenDelegationInactive(t *testing.T) {
	cleanup := setupOrchestratorTest(t)
	defer cleanup()

	ctx := context.Background()
	sessionID := "sess-delegate-inactive"
	ownerID := int64(3251)
	peerID := int64(3252)
	agentID := int64(4251)

	if err := store.DB.Create(&model.Session{
		SessionID:   sessionID,
		OwnerID:     ownerID,
		SessionType: 1,
	}).Error; err != nil {
		t.Fatalf("create session error: %v", err)
	}
	members := []model.SessionMember{
		{SessionID: sessionID, MemberID: ownerID, MemberType: 1},
		{SessionID: sessionID, MemberID: peerID, MemberType: 1},
	}
	for _, m := range members {
		if err := store.DB.Create(&m).Error; err != nil {
			t.Fatalf("create session member error: %v", err)
		}
	}
	if err := store.DB.Create(&model.Agent{
		ID:            agentID,
		AgentName:     "delegate-bot-inactive",
		ModelProvider: "mock",
		SystemPrompt:  "be helpful",
		OwnerID:       ownerID,
	}).Error; err != nil {
		t.Fatalf("create agent error: %v", err)
	}

	o := NewOrchestrator(map[string]provider.Provider{
		"mock": &mockStreamProvider{
			chunks: []provider.StreamChunk{
				{DeltaContent: "should not run", IsFinish: true},
			},
		},
	})

	o.processDelegateRequest(ctx, &AIRequest{
		Cmd:          "delegate_request",
		SessionID:    sessionID,
		OwnerID:      ownerID,
		AgentID:      agentID,
		Content:      "hello",
		TriggerMsgID: 99002,
	})

	var msgCount int64
	if err := store.DB.Model(&model.Message{}).
		Where("session_id = ? AND sender_id = ? AND sender_type = ?", sessionID, ownerID, 1).
		Count(&msgCount).Error; err != nil {
		t.Fatalf("count delegate messages error: %v", err)
	}
	if msgCount != 0 {
		t.Fatalf("inactive delegation should skip generation, got message count=%d", msgCount)
	}
}

func TestHandleRequestDelegateStopCancelsActiveStream(t *testing.T) {
	cleanup := setupOrchestratorTest(t)
	defer cleanup()

	ctx := context.Background()
	sessionID := "sess-delegate-stop"
	ownerID := int64(3401)
	peerID := int64(3402)
	agentID := int64(4401)

	if err := store.DB.Create(&model.Session{
		SessionID:   sessionID,
		OwnerID:     ownerID,
		SessionType: 1,
	}).Error; err != nil {
		t.Fatalf("create session error: %v", err)
	}
	members := []model.SessionMember{
		{SessionID: sessionID, MemberID: ownerID, MemberType: 1},
		{SessionID: sessionID, MemberID: peerID, MemberType: 1},
	}
	for _, m := range members {
		if err := store.DB.Create(&m).Error; err != nil {
			t.Fatalf("create session member error: %v", err)
		}
	}
	if err := store.DB.Create(&model.Agent{
		ID:            agentID,
		AgentName:     "delegate-bot-stop",
		ModelProvider: "cancel-aware",
		SystemPrompt:  "be concise",
		OwnerID:       ownerID,
	}).Error; err != nil {
		t.Fatalf("create agent error: %v", err)
	}
	if err := store.RDB.HSet(
		ctx,
		fmt.Sprintf("im:delegate:%s:%d", sessionID, ownerID),
		"agent_id",
		fmt.Sprintf("%d", agentID),
	).Err(); err != nil {
		t.Fatalf("seed delegate state error: %v", err)
	}

	started := make(chan struct{})
	stopped := make(chan struct{})
	o := NewOrchestrator(map[string]provider.Provider{
		"cancel-aware": &cancelAwareProvider{
			started: started,
			stopped: stopped,
		},
	})

	done := make(chan struct{})
	go func() {
		defer close(done)
		o.HandleRequest(ctx, &AIRequest{
			Cmd:          "delegate_request",
			SessionID:    sessionID,
			OwnerID:      ownerID,
			AgentID:      agentID,
			Content:      "say something",
			TriggerMsgID: 9901,
		})
	}()

	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatalf("delegate stream did not start in time")
	}

	o.HandleRequest(ctx, &AIRequest{
		Cmd:       "delegate_stop",
		SessionID: sessionID,
		OwnerID:   ownerID,
	})

	select {
	case <-stopped:
	case <-time.After(2 * time.Second):
		t.Fatalf("delegate stream did not stop in time")
	}
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatalf("delegate handler did not return in time")
	}

	var msg model.Message
	if err := store.DB.Where(
		"session_id = ? AND sender_id = ? AND sender_type = ?",
		sessionID, ownerID, 1,
	).Order("created_at DESC").First(&msg).Error; err != nil {
		t.Fatalf("query canceled delegate message error: %v", err)
	}
	if msg.MsgType != 1 {
		t.Fatalf("canceled delegate message should be finalized text, got=%d", msg.MsgType)
	}
	if msg.Content != "partial" {
		t.Fatalf("canceled delegate message content mismatch got=%q want=%q", msg.Content, "partial")
	}
}

func TestHandleRequestDeduplicatesChatBySourceMsgID(t *testing.T) {
	cleanup := setupOrchestratorTest(t)
	defer cleanup()

	ctx := context.Background()
	sessionID := "sess-chat-dedupe"
	userID := int64(3301)
	agentID := int64(4301)

	if err := store.DB.Create(&model.Session{
		SessionID:   sessionID,
		OwnerID:     userID,
		SessionType: 1,
	}).Error; err != nil {
		t.Fatalf("create session error: %v", err)
	}
	members := []model.SessionMember{
		{SessionID: sessionID, MemberID: userID, MemberType: 1},
		{SessionID: sessionID, MemberID: agentID, MemberType: 2},
	}
	for _, m := range members {
		if err := store.DB.Create(&m).Error; err != nil {
			t.Fatalf("create session member error: %v", err)
		}
	}
	if err := store.DB.Create(&model.Agent{
		ID:            agentID,
		AgentName:     "chat-bot-dedupe",
		ModelProvider: "mock",
		SystemPrompt:  "be helpful",
		OwnerID:       userID,
	}).Error; err != nil {
		t.Fatalf("create agent error: %v", err)
	}

	o := NewOrchestrator(map[string]provider.Provider{
		"mock": &mockStreamProvider{
			chunks: []provider.StreamChunk{
				{DeltaContent: "chat ", PromptTokens: 5},
				{DeltaContent: "once", IsFinish: true, CompletionTokens: 6},
			},
		},
	})

	req := &AIRequest{
		Cmd:          "client_stream_chunk",
		SessionID:    sessionID,
		SenderID:     userID,
		UserID:       userID,
		DeltaContent: "hello",
		IsFinish:     true,
		MsgID:        9900001,
	}
	o.HandleRequest(ctx, req)
	o.HandleRequest(ctx, req)

	var aiMsgCount int64
	if err := store.DB.Model(&model.Message{}).
		Where("session_id = ? AND sender_id = ? AND sender_type = ?", sessionID, agentID, 2).
		Count(&aiMsgCount).Error; err != nil {
		t.Fatalf("count ai messages error: %v", err)
	}
	if aiMsgCount != 1 {
		t.Fatalf("chat dedupe failed, expected 1 ai message got=%d", aiMsgCount)
	}
}

func TestHandleRequestRejectsMutedChatRequester(t *testing.T) {
	cleanup := setupOrchestratorTest(t)
	defer cleanup()

	ctx := context.Background()
	sessionID := "sess-chat-muted"
	userID := int64(3401)
	agentID := int64(4401)
	nodeID := "node-chat-muted"

	if err := store.DB.Create(&model.Session{
		SessionID:      sessionID,
		OwnerID:        userID,
		SessionType:    model.SessionTypeGroup,
		LastMsgSummary: "group",
	}).Error; err != nil {
		t.Fatalf("create session error: %v", err)
	}
	members := []model.SessionMember{
		{SessionID: sessionID, MemberID: userID, MemberType: 1, Role: 1, IsSpeakMuted: true},
		{SessionID: sessionID, MemberID: agentID, MemberType: 2, Role: 1},
	}
	for _, m := range members {
		member := m
		if err := store.DB.Create(&member).Error; err != nil {
			t.Fatalf("create session member error: %v", err)
		}
	}
	if err := store.DB.Create(&model.Agent{
		ID:            agentID,
		AgentName:     "chat-bot-muted",
		ModelProvider: "mock",
		SystemPrompt:  "be helpful",
		OwnerID:       userID,
		Status:        model.AgentStatusActive,
	}).Error; err != nil {
		t.Fatalf("create agent error: %v", err)
	}
	if err := store.RDB.HSet(
		ctx,
		fmt.Sprintf("im:ws:route:%d", userID),
		"dev-a",
		nodeID,
	).Err(); err != nil {
		t.Fatalf("seed route hash error: %v", err)
	}

	sub := store.RDB.Subscribe(ctx, fmt.Sprintf("chan:%s", nodeID))
	defer sub.Close()
	_, _ = sub.ReceiveTimeout(ctx, 200*time.Millisecond)

	o := NewOrchestrator(map[string]provider.Provider{
		"mock": &mockStreamProvider{
			chunks: []provider.StreamChunk{
				{DeltaContent: "should not run", IsFinish: true},
			},
		},
	})

	o.HandleRequest(ctx, &AIRequest{
		Cmd:          "client_stream_chunk",
		SessionID:    sessionID,
		SenderID:     userID,
		UserID:       userID,
		DeltaContent: "hello",
		IsFinish:     true,
		MsgID:        9900002,
	})

	msg, err := sub.ReceiveMessage(ctx)
	if err != nil {
		t.Fatalf("receive stream_error message error: %v", err)
	}

	var envelope map[string]interface{}
	if err := json.Unmarshal([]byte(msg.Payload), &envelope); err != nil {
		t.Fatalf("unmarshal envelope error: %v", err)
	}
	if envelope["cmd"] != "stream_error" {
		t.Fatalf("expected cmd=stream_error, got=%v", envelope["cmd"])
	}
	rawPayload, ok := envelope["payload"].(map[string]interface{})
	if !ok {
		t.Fatalf("payload type mismatch: %T", envelope["payload"])
	}
	if rawPayload["session_id"] != sessionID {
		t.Fatalf("session_id mismatch: got=%v want=%s", rawPayload["session_id"], sessionID)
	}
	if rawPayload["error_code"] != float64(4003) {
		t.Fatalf("error_code mismatch: got=%v want=4003", rawPayload["error_code"])
	}
	if rawPayload["error_msg"] != "member is muted" {
		t.Fatalf("error_msg mismatch: got=%v want=%s", rawPayload["error_msg"], "member is muted")
	}

	var aiMsgCount int64
	if err := store.DB.Model(&model.Message{}).
		Where("session_id = ? AND sender_id = ? AND sender_type = ?", sessionID, agentID, 2).
		Count(&aiMsgCount).Error; err != nil {
		t.Fatalf("count ai messages error: %v", err)
	}
	if aiMsgCount != 0 {
		t.Fatalf("muted requester should not trigger ai messages, got=%d", aiMsgCount)
	}
}

func TestSendDelegateStreamErrorUsesUnifiedStreamProtocol(t *testing.T) {
	cleanup := setupOrchestratorTest(t)
	defer cleanup()

	ctx := context.Background()
	sessionID := "sess-delegate-stream-error"
	ownerID := int64(5101)
	nodeID := "node-test-1"
	msgID := int64(99001)

	if err := store.RDB.HSet(
		ctx,
		fmt.Sprintf("im:ws:route:%d", ownerID),
		"dev-a",
		nodeID,
	).Err(); err != nil {
		t.Fatalf("seed route hash error: %v", err)
	}

	sub := store.RDB.Subscribe(ctx, fmt.Sprintf("chan:%s", nodeID))
	defer sub.Close()
	_, _ = sub.ReceiveTimeout(ctx, 200*time.Millisecond)

	o := &Orchestrator{}
	o.sendDelegateStreamError(ctx, sessionID, msgID, ownerID, 5004, "Agent not found")

	msg, err := sub.ReceiveMessage(ctx)
	if err != nil {
		t.Fatalf("receive pubsub message error: %v", err)
	}

	var envelope map[string]interface{}
	if err := json.Unmarshal([]byte(msg.Payload), &envelope); err != nil {
		t.Fatalf("unmarshal envelope error: %v", err)
	}
	if envelope["cmd"] != "stream_error" {
		t.Fatalf("expected cmd=stream_error, got=%v", envelope["cmd"])
	}
	if envelope["user_id"] != float64(ownerID) {
		t.Fatalf("expected user_id=%d, got=%v", ownerID, envelope["user_id"])
	}

	rawPayload, ok := envelope["payload"].(map[string]interface{})
	if !ok {
		t.Fatalf("payload type mismatch: %T", envelope["payload"])
	}
	if rawPayload["session_id"] != sessionID {
		t.Fatalf("session_id mismatch: got=%v want=%s", rawPayload["session_id"], sessionID)
	}
	if rawPayload["msg_id"] != strconv.FormatInt(msgID, 10) {
		t.Fatalf("msg_id mismatch: got=%v want=%d", rawPayload["msg_id"], msgID)
	}
	if rawPayload["sender_id"] != strconv.FormatInt(ownerID, 10) {
		t.Fatalf("sender_id mismatch: got=%v want=%d", rawPayload["sender_id"], ownerID)
	}
	if rawPayload["error_code"] != float64(5004) {
		t.Fatalf("error_code mismatch: got=%v want=5004", rawPayload["error_code"])
	}
	if rawPayload["error_msg"] != "Agent not found" {
		t.Fatalf("error_msg mismatch: got=%v want=%s", rawPayload["error_msg"], "Agent not found")
	}
}

func TestEnqueueAIInboxInvalidInputNoop(t *testing.T) {
	cleanup := setupOrchestratorTest(t)
	defer cleanup()

	ctx := context.Background()
	sessionID := "sess-ai-noop"

	if err := store.DB.Create(&model.SessionMember{
		SessionID:  sessionID,
		MemberID:   1201,
		MemberType: 1,
	}).Error; err != nil {
		t.Fatalf("create session member error: %v", err)
	}

	o := &Orchestrator{}
	_ = o
	agentmsg.EnqueueStreamInbox(ctx, "", 9003, 0, nil)
	agentmsg.EnqueueStreamInbox(ctx, sessionID, 0, 0, nil)

	var count int64
	if err := store.DB.Model(&model.UserInbox{}).Count(&count).Error; err != nil {
		t.Fatalf("count user_inbox error: %v", err)
	}
	if count != 0 {
		t.Fatalf("invalid input should not create inbox rows: got=%d want=0", count)
	}

	unreadKeyExists, err := store.RDB.Exists(ctx, "im:unread:1201").Result()
	if err != nil {
		t.Fatalf("exists unread key error: %v", err)
	}
	if unreadKeyExists != 0 {
		t.Fatalf("invalid input should not touch unread hash, key exists=%d", unreadKeyExists)
	}
}

func TestClassifyLLMErrorTimeout(t *testing.T) {
	o := &Orchestrator{}
	code, msg := o.classifyLLMError(context.Background(), context.DeadlineExceeded)
	if code != 5003 {
		t.Fatalf("error_code mismatch: got=%d want=5003", code)
	}
	if msg != "上游服务响应超时" {
		t.Fatalf("error_msg mismatch: got=%q want=%q", msg, "上游服务响应超时")
	}
}

func TestDelegateRequestTimeoutUsesConfigAndClamp(t *testing.T) {
	o := &Orchestrator{}
	orig := config.C.LLM.DelegateTimeoutSec
	defer func() {
		config.C.LLM.DelegateTimeoutSec = orig
	}()

	config.C.LLM.DelegateTimeoutSec = 0
	if got := o.delegateRequestTimeout(); got != defaultDelegateTimeout {
		t.Fatalf("default timeout mismatch: got=%s want=%s", got, defaultDelegateTimeout)
	}

	config.C.LLM.DelegateTimeoutSec = 1
	if got := o.delegateRequestTimeout(); got != minDelegateTimeout {
		t.Fatalf("min clamp mismatch: got=%s want=%s", got, minDelegateTimeout)
	}

	config.C.LLM.DelegateTimeoutSec = 999999
	if got := o.delegateRequestTimeout(); got != maxDelegateTimeout {
		t.Fatalf("max clamp mismatch: got=%s want=%s", got, maxDelegateTimeout)
	}

	config.C.LLM.DelegateTimeoutSec = 420
	if got := o.delegateRequestTimeout(); got != 420*time.Second {
		t.Fatalf("custom timeout mismatch: got=%s want=%s", got, 420*time.Second)
	}
}
