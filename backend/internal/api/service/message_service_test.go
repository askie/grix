package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/askie/grix/backend/config"
	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/logger"
	"github.com/askie/grix/backend/internal/pkg/snowflake"
	"github.com/askie/grix/backend/internal/pkg/testutil"
	"github.com/askie/grix/backend/internal/store"
	"gorm.io/datatypes"
)

func init() {
	_ = snowflake.Init(1)
}

func setupMessageTest(t *testing.T) (*testutil.TestDB, func()) {
	t.Helper()
	testDB := testutil.NewTestDB()
	store.DB = testDB.DB
	store.RDB = testutil.NewMockRedis()
	logger.Init()
	return testDB, func() {
		_ = store.RDB.Close()
		testDB.Close()
	}
}

func createTestMessageSession(t *testing.T, db *testutil.TestDB, userID int64, sessionID string) {
	t.Helper()
	now := time.Now()

	// Create session
	session := model.Session{
		SessionID:   sessionID,
		OwnerID:     userID,
		SessionType: 1,
	}
	if err := db.DB.Create(&session).Error; err != nil {
		t.Fatalf("failed to create session: %v", err)
	}

	// Create session member
	member := model.SessionMember{
		SessionID:    sessionID,
		MemberID:     userID,
		MemberType:   1,
		Role:         3,
		LastActiveAt: now,
		JoinedAt:     now,
	}
	if err := db.DB.Create(&member).Error; err != nil {
		t.Fatalf("failed to create session member: %v", err)
	}
}

func createTestMessage(db *testutil.TestDB, sessionID string, senderID int64, msgID int64, content string) {
	msg := model.Message{
		MsgID:     msgID,
		SessionID: sessionID,
		SenderID:  senderID,
		MsgType:   1,
		Content:   content,
	}
	db.DB.Create(&msg)
}

func TestMessageHistory(t *testing.T) {
	testDB, cleanup := setupMessageTest(t)
	defer cleanup()

	userID := int64(6001)
	sessionID := "msg-test-session"

	t.Run("user not in session", func(t *testing.T) {
		_, err := MessageHistory(userID, "unknown-session", 0, 10)
		if err == nil {
			t.Error("expected error for non-member user")
		}
	})

	t.Run("agent-only membership does not grant human access", func(t *testing.T) {
		agentOnlySession := "agent-only-session"
		now := time.Now()
		if err := testDB.DB.Create(&model.Session{
			SessionID:   agentOnlySession,
			OwnerID:     userID,
			SessionType: 2,
		}).Error; err != nil {
			t.Fatalf("failed to create agent-only session: %v", err)
		}
		if err := testDB.DB.Create(&model.SessionMember{
			SessionID:    agentOnlySession,
			MemberID:     userID,
			MemberType:   2,
			Role:         1,
			LastActiveAt: now,
			JoinedAt:     now,
		}).Error; err != nil {
			t.Fatalf("failed to create agent-only membership: %v", err)
		}

		_, err := MessageHistory(userID, agentOnlySession, 0, 10)
		if err == nil {
			t.Fatal("expected error for agent-only membership")
		}
	})

	t.Run("empty history", func(t *testing.T) {
		createTestMessageSession(t, testDB, userID, sessionID)

		resp, err := MessageHistory(userID, sessionID, 0, 10)
		if err != nil {
			t.Fatalf("MessageHistory() error = %v", err)
		}

		if resp.HasMore {
			t.Error("expected no hasMore for empty history")
		}
		if len(resp.Messages) != 0 {
			t.Errorf("expected empty messages, got %d", len(resp.Messages))
		}
	})

	t.Run("with messages", func(t *testing.T) {
		// Create messages
		for i := 1; i <= 5; i++ {
			createTestMessage(testDB, sessionID, userID, int64(i*100), "Message "+string(rune('0'+i)))
		}

		resp, err := MessageHistory(userID, sessionID, 0, 10)
		if err != nil {
			t.Fatalf("MessageHistory() error = %v", err)
		}

		if len(resp.Messages) != 5 {
			t.Errorf("expected 5 messages, got %d", len(resp.Messages))
		}
	})

	t.Run("pagination with beforeID", func(t *testing.T) {
		// Get messages before ID 300
		resp, err := MessageHistory(userID, sessionID, 300, 2)
		if err != nil {
			t.Fatalf("MessageHistory() error = %v", err)
		}

		// Should get messages 100 and 200 (IDs less than 300)
		if len(resp.Messages) != 2 {
			t.Errorf("expected 2 messages, got %d", len(resp.Messages))
		}

		// Verify order is descending (newest first)
		if len(resp.Messages) >= 2 {
			if resp.Messages[0].MsgID < resp.Messages[1].MsgID {
				t.Error("expected messages in descending order")
			}
		}
	})

	t.Run("hasMore flag", func(t *testing.T) {
		// Clear and create 15 messages
		testDB.DB.Where("session_id = ?", sessionID).Delete(&model.Message{})
		for i := 1; i <= 15; i++ {
			createTestMessage(testDB, sessionID, userID, int64(i*10), "New Message")
		}

		// Request 10 messages
		resp, err := MessageHistory(userID, sessionID, 0, 10)
		if err != nil {
			t.Fatalf("MessageHistory() error = %v", err)
		}

		if !resp.HasMore {
			t.Error("expected hasMore to be true")
		}
		if len(resp.Messages) != 10 {
			t.Errorf("expected 10 messages, got %d", len(resp.Messages))
		}
	})

	t.Run("deleted messages excluded", func(t *testing.T) {
		sessionID2 := "deleted-msg-session"
		createTestMessageSession(t, testDB, userID, sessionID2)

		// Create messages
		createTestMessage(testDB, sessionID2, userID, 1, "Active")
		deletedMsg := model.Message{
			MsgID:     2,
			SessionID: sessionID2,
			SenderID:  userID,
			MsgType:   1,
			Content:   "Deleted",
			IsDeleted: true,
		}
		testDB.DB.Create(&deletedMsg)
		createTestMessage(testDB, sessionID2, userID, 3, "Active 2")

		resp, err := MessageHistory(userID, sessionID2, 0, 10)
		if err != nil {
			t.Fatalf("MessageHistory() error = %v", err)
		}

		// Should only get 2 active messages
		if len(resp.Messages) != 2 {
			t.Errorf("expected 2 messages (deleted excluded), got %d", len(resp.Messages))
		}
	})

	t.Run("history reset hides older messages", func(t *testing.T) {
		sessionID3 := "history-reset-session"
		createTestMessageSession(t, testDB, userID, sessionID3)

		oldTime := time.Now().UTC().Add(-2 * time.Hour)
		newTime := time.Now().UTC().Add(-30 * time.Minute)
		if err := testDB.DB.Create(&model.Message{
			MsgID:      11,
			SessionID:  sessionID3,
			SenderID:   userID,
			SenderType: 1,
			MsgType:    1,
			Content:    "旧消息",
			CreatedAt:  oldTime,
		}).Error; err != nil {
			t.Fatalf("create old message error: %v", err)
		}
		if err := testDB.DB.Create(&model.Message{
			MsgID:      12,
			SessionID:  sessionID3,
			SenderID:   userID,
			SenderType: 1,
			MsgType:    1,
			Content:    "新消息",
			CreatedAt:  newTime,
		}).Error; err != nil {
			t.Fatalf("create new message error: %v", err)
		}
		if err := testDB.DB.Create(&model.SessionHistoryReset{
			SessionID:     sessionID3,
			UserID:        userID,
			DeletedBefore: oldTime.Add(30 * time.Minute),
		}).Error; err != nil {
			t.Fatalf("create history reset error: %v", err)
		}

		resp, err := MessageHistory(userID, sessionID3, 0, 10)
		if err != nil {
			t.Fatalf("MessageHistory() error = %v", err)
		}
		if len(resp.Messages) != 1 {
			t.Fatalf("expected 1 visible message after reset, got %d", len(resp.Messages))
		}
		if resp.Messages[0].Content != "新消息" {
			t.Fatalf("visible message=%q want=%q", resp.Messages[0].Content, "新消息")
		}
	})
}

func TestMessageHistoryExcludesStreamingPlaceholder(t *testing.T) {
	testDB, cleanup := setupMessageTest(t)
	defer cleanup()

	const (
		userID    = int64(7601)
		sessionID = "streaming-placeholder-session"
	)
	createTestMessageSession(t, testDB, userID, sessionID)

	// 已封板的正常 AI 消息
	if err := testDB.DB.Create(&model.Message{
		MsgID:      201,
		SessionID:  sessionID,
		SenderID:   9701,
		SenderType: 2,
		MsgType:    model.MsgTypeText,
		Content:    "已封板的回复",
		CreatedAt:  time.Now().UTC().Add(-2 * time.Minute),
	}).Error; err != nil {
		t.Fatalf("create sealed message error: %v", err)
	}
	// 孤儿流式占位（msg_type=4，内容为空，无 inbox_seq）——历史接口必须排除
	if err := testDB.DB.Create(&model.Message{
		MsgID:      202,
		SessionID:  sessionID,
		SenderID:   9701,
		SenderType: 2,
		MsgType:    model.MsgTypeAIStream,
		Content:    "",
		CreatedAt:  time.Now().UTC().Add(-1 * time.Minute),
	}).Error; err != nil {
		t.Fatalf("create streaming placeholder error: %v", err)
	}

	resp, err := MessageHistory(userID, sessionID, 0, 10)
	if err != nil {
		t.Fatalf("MessageHistory() error = %v", err)
	}
	if len(resp.Messages) != 1 {
		t.Fatalf("expected 1 message (placeholder excluded), got %d", len(resp.Messages))
	}
	if resp.Messages[0].MsgID != 201 {
		t.Fatalf("visible msg_id=%d want=201", resp.Messages[0].MsgID)
	}
}

func TestAgentMessageHistoryReturnsCleanMessagesAndDefaultsToOne(t *testing.T) {
	testDB, cleanup := setupMessageTest(t)
	defer cleanup()

	const (
		ownerID   = int64(7611)
		agentID   = int64(9711)
		sessionID = "agent-clean-history-session"
	)
	createTestMessageSession(t, testDB, ownerID, sessionID)
	now := time.Now().UTC().Add(time.Second)
	messages := []model.Message{
		{
			MsgID: 101, SessionID: sessionID, SenderID: ownerID, SenderType: 1,
			MsgType: model.MsgTypeText, Content: "请处理任务", CreatedAt: now,
		},
		{
			MsgID: 102, SessionID: sessionID, SenderID: agentID, SenderType: 2,
			MsgType: model.MsgTypeText,
			Content: "[Approve](grix://card/exec_approval?approval_id=req-1)",
			Extra:   datatypes.JSON(`{"biz_card":{"type":"exec_approval"}}`), CreatedAt: now.Add(time.Second),
		},
		{
			MsgID: 103, SessionID: sessionID, SenderID: agentID, SenderType: 2,
			MsgType: model.MsgTypeText,
			Content: "[Tool](grix://card/tool_execution?d=%7B%7D)",
			Extra:   datatypes.JSON(`{"biz_card":{"type":"tool_execution"}}`), CreatedAt: now.Add(2 * time.Second),
		},
		{
			MsgID: 104, SessionID: sessionID, SenderID: agentID, SenderType: 2,
			MsgType: model.MsgTypeText, Content: "任务最终结果", CreatedAt: now.Add(3 * time.Second),
		},
		{
			MsgID: 105, SessionID: sessionID, SenderID: agentID, SenderType: 2,
			MsgType: model.MsgTypeText,
			Content: "[Status](grix://card/agent_status?status=running)",
			Extra:   datatypes.JSON(`{"biz_card":{"type":"agent_status"}}`), CreatedAt: now.Add(4 * time.Second),
		},
		{
			MsgID: 106, SessionID: sessionID, SenderID: agentID, SenderType: 2,
			MsgType:   model.MsgTypeText,
			Content:   "[Tools](grix://card/tool_execution_group?d=%7B%7D)",
			CreatedAt: now.Add(5 * time.Second),
		},
	}
	for i := int64(0); i < 25; i++ {
		messages = append(messages, model.Message{
			MsgID: 200 + i, SessionID: sessionID, SenderID: agentID, SenderType: 2,
			MsgType:   model.MsgTypeText,
			Content:   "[Tool](grix://card/tool_execution?d=%7B%7D)",
			CreatedAt: now.Add(time.Duration(10+i) * time.Second),
		})
	}
	if err := testDB.DB.Create(&messages).Error; err != nil {
		t.Fatalf("seed clean history messages error: %v", err)
	}

	defaultPage, err := AgentMessageHistory(ownerID, sessionID, 0, 0)
	if err != nil {
		t.Fatalf("AgentMessageHistory(default) error = %v", err)
	}
	if len(defaultPage.Messages) != 1 || defaultPage.Messages[0].MsgID != 104 {
		t.Fatalf("default page=%#v want only msg 104", defaultPage.Messages)
	}
	if !defaultPage.HasMore {
		t.Fatal("default page should report older clean messages")
	}

	two, err := AgentMessageHistory(ownerID, sessionID, 0, 2)
	if err != nil {
		t.Fatalf("AgentMessageHistory(limit=2) error = %v", err)
	}
	if len(two.Messages) != 2 {
		t.Fatalf("limit=2 count=%d want=2", len(two.Messages))
	}
	if two.Messages[0].MsgID != 104 || two.Messages[1].MsgID != 102 {
		t.Fatalf("limit=2 ids=%v want [104 102]", []int64{two.Messages[0].MsgID, two.Messages[1].MsgID})
	}
	if !two.HasMore {
		t.Fatal("limit=2 should report the older owner text")
	}
}

func TestChatTaskResultReturnsOneFinalPlainText(t *testing.T) {
	testDB, cleanup := setupMessageTest(t)
	defer cleanup()

	const (
		ownerID   = int64(7621)
		agentID   = int64(9721)
		sessionID = "chat-task-final-result-session"
	)
	createTestMessageSession(t, testDB, ownerID, sessionID)
	startedAt := time.Now().UTC().Add(time.Second)
	completedAt := startedAt.Add(10 * time.Second)
	messages := []model.Message{
		{
			MsgID: 201, SessionID: sessionID, SenderID: agentID, SenderType: 2,
			MsgType: model.MsgTypeText, Content: "旧任务结果", CreatedAt: startedAt.Add(-time.Second),
		},
		{
			MsgID: 202, SessionID: sessionID, SenderID: agentID, SenderType: 2,
			MsgType: model.MsgTypeText, Content: "阶段性文本", CreatedAt: startedAt.Add(time.Second),
		},
		{
			MsgID: 203, SessionID: sessionID, SenderID: agentID, SenderType: 2,
			MsgType:   model.MsgTypeText,
			Content:   "[Tool](grix://card/tool_execution?d=%7B%7D)",
			CreatedAt: startedAt.Add(2 * time.Second),
		},
		{
			MsgID: 204, SessionID: sessionID, SenderID: agentID, SenderType: 2,
			MsgType:   model.MsgTypeText,
			Content:   "[Approve](grix://card/exec_approval?approval_id=req-2)",
			CreatedAt: startedAt.Add(3 * time.Second),
		},
		{
			MsgID: 205, SessionID: sessionID, SenderID: agentID, SenderType: 2,
			MsgType: model.MsgTypeText, Content: "唯一最终结果", CreatedAt: startedAt.Add(4 * time.Second),
		},
		{
			MsgID: 206, SessionID: sessionID, SenderID: ownerID, SenderType: 1,
			MsgType: model.MsgTypeText, Content: "主人追加内容", CreatedAt: startedAt.Add(5 * time.Second),
		},
		{
			MsgID: 207, SessionID: sessionID, SenderID: agentID, SenderType: 2,
			MsgType:   model.MsgTypeText,
			Content:   "[Status](grix://card/agent_status?status=completed)",
			CreatedAt: startedAt.Add(6 * time.Second),
		},
	}
	if err := testDB.DB.Create(&messages).Error; err != nil {
		t.Fatalf("seed task result messages error: %v", err)
	}

	result, err := ChatTaskResult(ownerID, model.SessionAgentState{
		SessionID:   sessionID,
		OwnerID:     ownerID,
		AgentID:     agentID,
		State:       model.SessionAgentStateCompleted,
		StartedAt:   &startedAt,
		CompletedAt: &completedAt,
	})
	if err != nil {
		t.Fatalf("ChatTaskResult() error = %v", err)
	}
	if result == nil || result.MsgID != 205 || result.Content != "唯一最终结果" {
		t.Fatalf("result=%#v want msg 205", result)
	}
}

func TestChatTaskResultDoesNotQueryMessagesBeforeCompletion(t *testing.T) {
	previousDB := store.DB
	store.DB = nil
	t.Cleanup(func() { store.DB = previousDB })

	startedAt := time.Now().UTC()
	result, err := ChatTaskResult(7631, model.SessionAgentState{
		SessionID: "running-task",
		OwnerID:   7631,
		AgentID:   9731,
		State:     model.SessionAgentStateRunning,
		StartedAt: &startedAt,
	})
	if err != nil || result != nil {
		t.Fatalf("non-completed result=%#v err=%v want nil,nil", result, err)
	}
}

func TestMessageHistoryRejectsBannedGroup(t *testing.T) {
	testDB, cleanup := setupMessageTest(t)
	defer cleanup()

	const (
		userID    = int64(6601)
		sessionID = "msg-banned-group"
	)
	now := time.Now()

	if err := testDB.DB.Create(&model.Session{
		SessionID:        sessionID,
		OwnerID:          userID,
		SessionType:      model.SessionTypeGroup,
		GroupName:        "Banned Group",
		ModerationStatus: model.SessionModerationStatusBanned,
	}).Error; err != nil {
		t.Fatalf("create session: %v", err)
	}
	if err := testDB.DB.Create(&model.SessionMember{
		SessionID: sessionID, MemberID: userID, MemberType: 1, Role: 3, JoinedAt: now, LastActiveAt: now,
	}).Error; err != nil {
		t.Fatalf("create session member: %v", err)
	}
	createTestMessage(testDB, sessionID, userID, 1, "hidden")

	_, err := MessageHistory(userID, sessionID, 0, 10)
	if !errors.Is(err, ErrSessionGroupBanned) {
		t.Fatalf("expected ErrSessionGroupBanned, got %v", err)
	}
}

func TestMessageHistoryOrder(t *testing.T) {
	testDB, cleanup := setupMessageTest(t)
	defer cleanup()

	userID := int64(7001)
	sessionID := "order-test-session"
	createTestMessageSession(t, testDB, userID, sessionID)

	// Create messages with different timestamps
	msgs := []struct {
		id      int64
		content string
	}{
		{100, "First"},
		{200, "Second"},
		{300, "Third"},
	}

	for _, m := range msgs {
		createTestMessage(testDB, sessionID, userID, m.id, m.content)
		time.Sleep(10 * time.Millisecond) // Small delay for ordering
	}

	resp, err := MessageHistory(userID, sessionID, 0, 10)
	if err != nil {
		t.Fatalf("MessageHistory() error = %v", err)
	}

	// Messages should be in descending order by msg_id
	if len(resp.Messages) == 3 {
		if resp.Messages[0].MsgID < resp.Messages[2].MsgID {
			t.Error("expected messages in descending order (newest first)")
		}
	}
}

// TestMessageHistoryGroupMemberCannotFetchMessagesBeforeJoinedAt 验证群聊新成员
// 通过 /messages/history 拉取不到入群时间之前的历史消息，只能看到入群后的记录。
func TestMessageHistoryGroupMemberCannotFetchMessagesBeforeJoinedAt(t *testing.T) {
	testDB, cleanup := setupMessageTest(t)
	defer cleanup()

	const (
		ownerID   = int64(6611)
		newUserID = int64(6612)
		sessionID = "msg-group-before-joined"
	)
	now := time.Now().UTC()

	if err := testDB.DB.Create(&model.Session{
		SessionID:   sessionID,
		OwnerID:     ownerID,
		SessionType: model.SessionTypeGroup,
		GroupName:   "History Before Joined Group",
		CreatedAt:   now.Add(-2 * time.Hour),
		UpdatedAt:   now,
	}).Error; err != nil {
		t.Fatalf("create session: %v", err)
	}
	if err := testDB.DB.Create(&model.SessionMember{
		SessionID:    sessionID,
		MemberID:     ownerID,
		MemberType:   1,
		Role:         3,
		JoinedAt:     now.Add(-2 * time.Hour),
		LastActiveAt: now,
	}).Error; err != nil {
		t.Fatalf("create owner member: %v", err)
	}

	// 在 newUserID 入群之前先创建两条历史消息。
	oldMessages := []model.Message{
		{
			MsgID:      1,
			SessionID:  sessionID,
			SenderID:   ownerID,
			SenderType: 1,
			MsgType:    1,
			Content:    "old message before user joined",
			CreatedAt:  now.Add(-90 * time.Minute),
		},
		{
			MsgID:      2,
			SessionID:  sessionID,
			SenderID:   ownerID,
			SenderType: 1,
			MsgType:    1,
			Content:    "another old message before user joined",
			CreatedAt:  now.Add(-60 * time.Minute),
		},
	}
	for _, msg := range oldMessages {
		if err := testDB.DB.Create(&msg).Error; err != nil {
			t.Fatalf("create old message %d: %v", msg.MsgID, err)
		}
	}

	// newUserID 稍后才加入群聊。
	joinedAt := now.Add(-30 * time.Minute)
	if err := testDB.DB.Create(&model.SessionMember{
		SessionID:    sessionID,
		MemberID:     newUserID,
		MemberType:   1,
		Role:         1,
		JoinedAt:     joinedAt,
		LastActiveAt: now,
	}).Error; err != nil {
		t.Fatalf("create new user member: %v", err)
	}

	// 入群后再创建一条新消息。
	newMsg := model.Message{
		MsgID:      3,
		SessionID:  sessionID,
		SenderID:   ownerID,
		SenderType: 1,
		MsgType:    1,
		Content:    "new message after user joined",
		CreatedAt:  joinedAt.Add(time.Minute),
	}
	if err := testDB.DB.Create(&newMsg).Error; err != nil {
		t.Fatalf("create new message: %v", err)
	}

	resp, err := MessageHistory(newUserID, sessionID, 0, 10)
	if err != nil {
		t.Fatalf("MessageHistory() error = %v", err)
	}
	if len(resp.Messages) != 1 {
		t.Fatalf("expected 1 message after joined_at, got %d", len(resp.Messages))
	}
	if resp.Messages[0].MsgID != 3 {
		t.Fatalf("expected only msg_id=3 visible, got=%d", resp.Messages[0].MsgID)
	}
}

func TestMessageSearch(t *testing.T) {
	testDB, cleanup := setupMessageTest(t)
	defer cleanup()

	const (
		userID    = int64(7011)
		sessionID = "message-search-session"
	)
	createTestMessageSession(t, testDB, userID, sessionID)

	if err := testDB.DB.Create(&model.Message{
		MsgID:      101,
		SessionID:  sessionID,
		SenderID:   userID,
		SenderType: 1,
		MsgType:    1,
		Content:    "部署完成，等待验证",
		CreatedAt:  time.Now().UTC().Add(-2 * time.Hour),
	}).Error; err != nil {
		t.Fatalf("create message 101 error: %v", err)
	}
	if err := testDB.DB.Create(&model.Message{
		MsgID:      102,
		SessionID:  sessionID,
		SenderID:   userID,
		SenderType: 1,
		MsgType:    1,
		Content:    "今天先讨论日志方案",
		CreatedAt:  time.Now().UTC().Add(-90 * time.Minute),
	}).Error; err != nil {
		t.Fatalf("create message 102 error: %v", err)
	}
	if err := testDB.DB.Create(&model.Message{
		MsgID:      103,
		SessionID:  sessionID,
		SenderID:   userID,
		SenderType: 1,
		MsgType:    1,
		Content:    "部署日志已经补齐",
		CreatedAt:  time.Now().UTC().Add(-30 * time.Minute),
	}).Error; err != nil {
		t.Fatalf("create message 103 error: %v", err)
	}

	t.Run("keyword required", func(t *testing.T) {
		_, err := MessageSearch(userID, sessionID, "", 0, 10)
		if err == nil || err.Error() != "keyword required" {
			t.Fatalf("expected keyword required error, got %v", err)
		}
	})

	t.Run("returns newest matching messages first", func(t *testing.T) {
		resp, err := MessageSearch(userID, sessionID, "日志", 0, 10)
		if err != nil {
			t.Fatalf("MessageSearch() error = %v", err)
		}
		if len(resp.Messages) != 2 {
			t.Fatalf("expected 2 matching messages, got %d", len(resp.Messages))
		}
		if resp.Messages[0].MsgID != 103 || resp.Messages[1].MsgID != 102 {
			t.Fatalf("search order=%v want=[103 102]", []int64{resp.Messages[0].MsgID, resp.Messages[1].MsgID})
		}
	})

	t.Run("supports beforeID pagination", func(t *testing.T) {
		resp, err := MessageSearch(userID, sessionID, "日志", 103, 10)
		if err != nil {
			t.Fatalf("MessageSearch() error = %v", err)
		}
		if len(resp.Messages) != 1 {
			t.Fatalf("expected 1 matching message before 103, got %d", len(resp.Messages))
		}
		if resp.Messages[0].MsgID != 102 {
			t.Fatalf("msg_id=%d want=102", resp.Messages[0].MsgID)
		}
	})

	t.Run("respects history reset cutoff", func(t *testing.T) {
		if err := testDB.DB.Create(&model.SessionHistoryReset{
			SessionID:     sessionID,
			UserID:        userID,
			DeletedBefore: time.Now().UTC().Add(-60 * time.Minute),
		}).Error; err != nil {
			t.Fatalf("create history reset error: %v", err)
		}

		resp, err := MessageSearch(userID, sessionID, "部署", 0, 10)
		if err != nil {
			t.Fatalf("MessageSearch() error = %v", err)
		}
		if len(resp.Messages) != 1 {
			t.Fatalf("expected 1 visible deployment message after reset, got %d", len(resp.Messages))
		}
		if resp.Messages[0].MsgID != 103 {
			t.Fatalf("visible msg_id=%d want=103", resp.Messages[0].MsgID)
		}
	})
}

func TestDeleteMessage_GroupAdminCanRevokeAnyGroupMessage(t *testing.T) {
	testCases := []struct {
		name       string
		senderID   int64
		senderType int16
	}{
		{
			name:       "human message",
			senderID:   8302,
			senderType: 1,
		},
		{
			name:       "agent message",
			senderID:   9301,
			senderType: 2,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			testDB, cleanup := setupMessageTest(t)
			defer cleanup()

			const (
				sessionID = "group-admin-delete-session"
				adminID   = int64(8301)
				msgID     = int64(7000501)
			)

			now := time.Now().UTC()
			lastMsgID := msgID
			if err := testDB.DB.Create(&model.Session{
				SessionID:      sessionID,
				OwnerID:        adminID,
				SessionType:    model.SessionTypeGroup,
				GroupName:      "admin revoke group",
				LastMsgID:      &lastMsgID,
				LastMsgSummary: "target message",
				CreatedAt:      now,
				UpdatedAt:      now,
			}).Error; err != nil {
				t.Fatalf("create session error: %v", err)
			}
			for _, member := range []model.SessionMember{
				{
					SessionID:     sessionID,
					MemberID:      adminID,
					MemberType:    1,
					Role:          2,
					UnreadCount:   1,
					LastReadMsgID: 0,
					JoinedAt:      now,
					LastActiveAt:  now,
				},
				{
					SessionID:     sessionID,
					MemberID:      8302,
					MemberType:    1,
					Role:          1,
					UnreadCount:   1,
					LastReadMsgID: 0,
					JoinedAt:      now,
					LastActiveAt:  now,
				},
				{
					SessionID:    sessionID,
					MemberID:     9301,
					MemberType:   2,
					Role:         1,
					JoinedAt:     now,
					LastActiveAt: now,
				},
			} {
				m := member
				if err := testDB.DB.Create(&m).Error; err != nil {
					t.Fatalf("create member(%d,%d) error: %v", m.MemberID, m.MemberType, err)
				}
			}
			if err := testDB.DB.Create(&model.Message{
				MsgID:      msgID,
				SessionID:  sessionID,
				SenderID:   tc.senderID,
				SenderType: tc.senderType,
				MsgType:    1,
				Content:    "target message",
				CreatedAt:  now,
			}).Error; err != nil {
				t.Fatalf("create message error: %v", err)
			}
			for _, row := range []model.UserInbox{
				{
					UserID:    adminID,
					InboxSeq:  41,
					MsgID:     msgID,
					SessionID: sessionID,
					CreatedAt: now,
				},
				{
					UserID:    8302,
					InboxSeq:  42,
					MsgID:     msgID,
					SessionID: sessionID,
					CreatedAt: now,
				},
			} {
				inboxRow := row
				if err := testDB.DB.Create(&inboxRow).Error; err != nil {
					t.Fatalf("create user_inbox row for user %d error: %v", inboxRow.UserID, err)
				}
			}

			err := DeleteMessage(context.Background(), sessionID, msgID, MessageDeleteActor{
				UserID: adminID,
			})
			if err != nil {
				t.Fatalf("DeleteMessage() error = %v", err)
			}

			var msg model.Message
			if err := testDB.DB.Where("msg_id = ? AND session_id = ?", msgID, sessionID).First(&msg).Error; err != nil {
				t.Fatalf("reload deleted message error: %v", err)
			}
			if !msg.IsDeleted || !msg.IsRevoked {
				t.Fatalf("expected message deleted+revoked, got deleted=%t revoked=%t", msg.IsDeleted, msg.IsRevoked)
			}

			// 撤回 tombstone 行应统一标记 EventKind=revoke（与无原收件人分支一致）。
			var tombstones []model.UserInbox
			if err := testDB.DB.Where("msg_id = ? AND session_id = ?", msgID, sessionID).Find(&tombstones).Error; err != nil {
				t.Fatalf("reload tombstone inbox rows error: %v", err)
			}
			if len(tombstones) == 0 {
				t.Fatalf("expected revoke tombstone inbox rows, got none")
			}
			for _, row := range tombstones {
				if row.EventKind != model.UserInboxEventKindRevoke {
					t.Fatalf("tombstone user=%d event_kind=%q want=%q", row.UserID, row.EventKind, model.UserInboxEventKindRevoke)
				}
			}
		})
	}
}

func TestDeleteMessage_GroupMemberCannotRevokeOtherUserMessage(t *testing.T) {
	testDB, cleanup := setupMessageTest(t)
	defer cleanup()

	const (
		sessionID = "group-member-delete-session"
		memberID  = int64(8401)
		ownerID   = int64(8402)
		msgID     = int64(7000601)
	)

	now := time.Now().UTC()
	lastMsgID := msgID
	if err := testDB.DB.Create(&model.Session{
		SessionID:      sessionID,
		OwnerID:        ownerID,
		SessionType:    model.SessionTypeGroup,
		GroupName:      "member revoke group",
		LastMsgID:      &lastMsgID,
		LastMsgSummary: "owner message",
		CreatedAt:      now,
		UpdatedAt:      now,
	}).Error; err != nil {
		t.Fatalf("create session error: %v", err)
	}
	for _, member := range []model.SessionMember{
		{
			SessionID:     sessionID,
			MemberID:      memberID,
			MemberType:    1,
			Role:          1,
			UnreadCount:   1,
			LastReadMsgID: 0,
			JoinedAt:      now,
			LastActiveAt:  now,
		},
		{
			SessionID:     sessionID,
			MemberID:      ownerID,
			MemberType:    1,
			Role:          3,
			UnreadCount:   1,
			LastReadMsgID: 0,
			JoinedAt:      now,
			LastActiveAt:  now,
		},
	} {
		m := member
		if err := testDB.DB.Create(&m).Error; err != nil {
			t.Fatalf("create member(%d,%d) error: %v", m.MemberID, m.MemberType, err)
		}
	}
	if err := testDB.DB.Create(&model.Message{
		MsgID:      msgID,
		SessionID:  sessionID,
		SenderID:   ownerID,
		SenderType: 1,
		MsgType:    1,
		Content:    "owner message",
		CreatedAt:  now,
	}).Error; err != nil {
		t.Fatalf("create message error: %v", err)
	}

	err := DeleteMessage(context.Background(), sessionID, msgID, MessageDeleteActor{
		UserID: memberID,
	})
	if err == nil {
		t.Fatal("expected permission error for normal group member")
	}
	if err.Error() != "20008" {
		t.Fatalf("expected error 20008, got %v", err)
	}
}

func TestDeleteMessage_AgentActorDeletesDirectAgentMessageAndPrunesOfflineInbox(t *testing.T) {
	testDB, cleanup := setupMessageTest(t)
	defer cleanup()

	const (
		sessionID = "delete-agent-session"
		ownerID   = int64(8101)
		agentID   = int64(9801)
		msgID     = int64(7001001)
	)

	now := time.Now().UTC()
	lastMsgID := msgID
	if err := testDB.DB.Create(&model.Session{
		SessionID:      sessionID,
		OwnerID:        ownerID,
		SessionType:    1,
		LastMsgID:      &lastMsgID,
		LastMsgSummary: "agent outbound",
		CreatedAt:      now,
		UpdatedAt:      now,
	}).Error; err != nil {
		t.Fatalf("create session error: %v", err)
	}
	for _, member := range []model.SessionMember{
		{
			SessionID:     sessionID,
			MemberID:      ownerID,
			MemberType:    1,
			UnreadCount:   1,
			LastReadMsgID: 0,
			JoinedAt:      now,
			LastActiveAt:  now,
		},
		{
			SessionID:    sessionID,
			MemberID:     agentID,
			MemberType:   2,
			JoinedAt:     now,
			LastActiveAt: now,
		},
	} {
		m := member
		if err := testDB.DB.Create(&m).Error; err != nil {
			t.Fatalf("create member(%d,%d) error: %v", m.MemberID, m.MemberType, err)
		}
	}
	if err := testDB.DB.Create(&model.Message{
		MsgID:      msgID,
		SessionID:  sessionID,
		ThreadID:   "topic-revoke-a",
		SenderID:   agentID,
		SenderType: 2,
		MsgType:    1,
		Content:    "agent outbound",
		CreatedAt:  now,
	}).Error; err != nil {
		t.Fatalf("create message error: %v", err)
	}
	if err := testDB.DB.Create(&model.UserInbox{
		UserID:    ownerID,
		InboxSeq:  51,
		MsgID:     msgID,
		SessionID: sessionID,
		CreatedAt: now,
	}).Error; err != nil {
		t.Fatalf("create user_inbox error: %v", err)
	}

	ctx := context.Background()
	if err := store.RDB.HSet(ctx, "im:ws:route:8101", "device-1", "node-delete").Err(); err != nil {
		t.Fatalf("seed ws route error: %v", err)
	}
	if err := store.RDB.Set(ctx, "im:inbox_seq:8101", 51, 0).Err(); err != nil {
		t.Fatalf("seed inbox_seq error: %v", err)
	}
	if err := store.RDB.HSet(ctx, "im:unread:8101", sessionID, 1).Err(); err != nil {
		t.Fatalf("seed unread hash error: %v", err)
	}
	pubsub := store.RDB.Subscribe(ctx, "chan:node-delete")
	defer pubsub.Close()

	err := DeleteMessage(ctx, sessionID, msgID, MessageDeleteActor{
		UserID:  ownerID,
		AgentID: agentID,
	})
	if err != nil {
		t.Fatalf("DeleteMessage() error = %v", err)
	}

	var msg model.Message
	if err := testDB.DB.Where("msg_id = ? AND session_id = ?", msgID, sessionID).First(&msg).Error; err != nil {
		t.Fatalf("reload deleted message error: %v", err)
	}
	if !msg.IsDeleted {
		t.Fatal("expected message to be marked deleted")
	}
	if !msg.IsRevoked {
		t.Fatal("expected message to be marked revoked")
	}

	var inboxRows []model.UserInbox
	if err := testDB.DB.
		Where("user_id = ? AND session_id = ? AND msg_id = ?", ownerID, sessionID, msgID).
		Order("inbox_seq ASC").
		Find(&inboxRows).Error; err != nil {
		t.Fatalf("load user_inbox rows error: %v", err)
	}
	if len(inboxRows) != 1 {
		t.Fatalf("expected one revoke inbox row, got=%d", len(inboxRows))
	}
	if inboxRows[0].InboxSeq != 52 {
		t.Fatalf("revoke inbox_seq=%d want=52", inboxRows[0].InboxSeq)
	}

	var session model.Session
	if err := testDB.DB.Where("session_id = ?", sessionID).First(&session).Error; err != nil {
		t.Fatalf("reload session error: %v", err)
	}
	if session.LastMsgID != nil && *session.LastMsgID != 0 {
		t.Fatalf("last_msg_id=%d want=0", *session.LastMsgID)
	}
	if session.LastMsgSummary != "" {
		t.Fatalf("last_msg_summary=%q want empty", session.LastMsgSummary)
	}

	var ownerMember model.SessionMember
	if err := testDB.DB.Where("session_id = ? AND member_id = ? AND member_type = 1", sessionID, ownerID).
		First(&ownerMember).Error; err != nil {
		t.Fatalf("reload owner member error: %v", err)
	}
	if ownerMember.UnreadCount != 0 {
		t.Fatalf("owner unread_count=%d want=0", ownerMember.UnreadCount)
	}
	unreadValue, err := store.RDB.HGet(ctx, "im:unread:8101", sessionID).Int64()
	if err != nil {
		t.Fatalf("redis unread lookup error: %v", err)
	}
	if unreadValue != 0 {
		t.Fatalf("redis unread=%d want=0", unreadValue)
	}

	select {
	case envelope := <-pubsub.Channel():
		var payload struct {
			UserID  int64  `json:"user_id"`
			Cmd     string `json:"cmd"`
			Payload struct {
				InboxSeq           string `json:"inbox_seq"`
				MsgID              string `json:"msg_id"`
				SessionID          string `json:"session_id"`
				ThreadID           string `json:"thread_id"`
				SessionType        int16  `json:"session_type"`
				SenderID           string `json:"sender_id"`
				SessionUnreadCount int    `json:"session_unread_count"`
				IsRevoked          bool   `json:"is_revoked"`
			} `json:"payload"`
		}
		if err := json.Unmarshal([]byte(envelope.Payload), &payload); err != nil {
			t.Fatalf("unmarshal realtime revoke error: %v", err)
		}
		if payload.UserID != ownerID {
			t.Fatalf("push user_id=%d want=%d", payload.UserID, ownerID)
		}
		if payload.Cmd != "push_revoke" {
			t.Fatalf("push cmd=%s want=push_revoke", payload.Cmd)
		}
		if payload.Payload.MsgID != "7001001" {
			t.Fatalf("push msg_id=%s want=7001001", payload.Payload.MsgID)
		}
		if payload.Payload.InboxSeq != "52" {
			t.Fatalf("push inbox_seq=%s want=52", payload.Payload.InboxSeq)
		}
		if payload.Payload.SessionID != sessionID {
			t.Fatalf("push session_id=%s want=%s", payload.Payload.SessionID, sessionID)
		}
		if payload.Payload.ThreadID != "topic-revoke-a" {
			t.Fatalf("push thread_id=%s want=topic-revoke-a", payload.Payload.ThreadID)
		}
		if payload.Payload.SessionType != 1 {
			t.Fatalf("push session_type=%d want=1", payload.Payload.SessionType)
		}
		if payload.Payload.SenderID != "9801" {
			t.Fatalf("push sender_id=%s want=9801", payload.Payload.SenderID)
		}
		if payload.Payload.SessionUnreadCount != 0 {
			t.Fatalf("push session_unread_count=%d want=0", payload.Payload.SessionUnreadCount)
		}
		if !payload.Payload.IsRevoked {
			t.Fatal("expected push_revoke is_revoked=true")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for push_revoke")
	}
}

func TestDeleteMessage_OwnerCanRevokeOwnedAgentMessageInDirectSession(t *testing.T) {
	testDB, cleanup := setupMessageTest(t)
	defer cleanup()

	const (
		sessionID = "owner-revoke-owned-agent-direct-session"
		ownerID   = int64(8501)
		agentID   = int64(9501)
		msgID     = int64(7001101)
	)

	now := time.Now().UTC()
	lastMsgID := msgID
	if err := testDB.DB.Create(&model.Agent{
		ID:        agentID,
		OwnerID:   ownerID,
		AgentName: "owned-agent",
		Status:    model.AgentStatusActive,
	}).Error; err != nil {
		t.Fatalf("create agent error: %v", err)
	}
	if err := testDB.DB.Create(&model.Session{
		SessionID:      sessionID,
		OwnerID:        ownerID,
		SessionType:    model.SessionTypeDirect,
		LastMsgID:      &lastMsgID,
		LastMsgSummary: "owned agent reply",
		CreatedAt:      now,
		UpdatedAt:      now,
	}).Error; err != nil {
		t.Fatalf("create session error: %v", err)
	}
	for _, member := range []model.SessionMember{
		{
			SessionID:     sessionID,
			MemberID:      ownerID,
			MemberType:    1,
			Role:          3,
			UnreadCount:   1,
			LastReadMsgID: 0,
			JoinedAt:      now,
			LastActiveAt:  now,
		},
		{
			SessionID:    sessionID,
			MemberID:     agentID,
			MemberType:   2,
			Role:         1,
			JoinedAt:     now,
			LastActiveAt: now,
		},
	} {
		member := member
		if err := testDB.DB.Create(&member).Error; err != nil {
			t.Fatalf("create member(%d,%d) error: %v", member.MemberID, member.MemberType, err)
		}
	}
	if err := testDB.DB.Create(&model.Message{
		MsgID:      msgID,
		SessionID:  sessionID,
		SenderID:   agentID,
		SenderType: 2,
		MsgType:    1,
		Content:    "owned agent reply",
		CreatedAt:  now,
	}).Error; err != nil {
		t.Fatalf("create message error: %v", err)
	}
	if err := testDB.DB.Create(&model.UserInbox{
		UserID:    ownerID,
		InboxSeq:  61,
		MsgID:     msgID,
		SessionID: sessionID,
		CreatedAt: now,
	}).Error; err != nil {
		t.Fatalf("create user_inbox error: %v", err)
	}

	err := DeleteMessage(context.Background(), sessionID, msgID, MessageDeleteActor{
		UserID: ownerID,
	})
	if err != nil {
		t.Fatalf("DeleteMessage() error = %v", err)
	}

	var msg model.Message
	if err := testDB.DB.Where("msg_id = ? AND session_id = ?", msgID, sessionID).First(&msg).Error; err != nil {
		t.Fatalf("reload deleted message error: %v", err)
	}
	if !msg.IsDeleted || !msg.IsRevoked {
		t.Fatalf("expected message deleted+revoked, got deleted=%t revoked=%t", msg.IsDeleted, msg.IsRevoked)
	}
}

func TestDeleteMessage_RevokedHumanMessageCleansOwnedAttachmentsSilently(t *testing.T) {
	testDB, cleanup := setupMessageTest(t)
	defer cleanup()

	const (
		sessionID = "human-revoke-attachment-cleanup"
		senderID  = int64(8701)
		msgID     = int64(7001151)
	)

	originalConfig := config.C
	originalAsync := messageAttachmentCleanupAsync
	originalEnsure := messageAttachmentEnsureOSSReady
	originalRemove := messageAttachmentRemoveObject
	t.Cleanup(func() {
		config.C = originalConfig
		messageAttachmentCleanupAsync = originalAsync
		messageAttachmentEnsureOSSReady = originalEnsure
		messageAttachmentRemoveObject = originalRemove
	})

	config.C.OSS.Media.PublicURL = "https://cdn.example.com/media"
	config.C.OSS.Media.Bucket = "media-bucket"
	config.C.OSS.Media.StorageDir = "aibot/media"
	messageAttachmentCleanupAsync = func(fn func()) { fn() }
	messageAttachmentEnsureOSSReady = func() error { return nil }

	removedKeys := make([]string, 0, 2)
	messageAttachmentRemoveObject = func(_ context.Context, bucket, objectKey string) error {
		if bucket != "media-bucket" {
			t.Fatalf("cleanup bucket=%q want=media-bucket", bucket)
		}
		removedKeys = append(removedKeys, objectKey)
		return errors.New("remove failed")
	}

	now := time.Now().UTC()
	lastMsgID := msgID
	if err := testDB.DB.Create(&model.Session{
		SessionID:      sessionID,
		OwnerID:        senderID,
		SessionType:    model.SessionTypeDirect,
		LastMsgID:      &lastMsgID,
		LastMsgSummary: "attachment message",
		CreatedAt:      now,
		UpdatedAt:      now,
	}).Error; err != nil {
		t.Fatalf("create session error: %v", err)
	}
	if err := testDB.DB.Create(&model.SessionMember{
		SessionID:     sessionID,
		MemberID:      senderID,
		MemberType:    1,
		Role:          3,
		UnreadCount:   1,
		LastReadMsgID: 0,
		JoinedAt:      now,
		LastActiveAt:  now,
	}).Error; err != nil {
		t.Fatalf("create member error: %v", err)
	}

	ownedKey := buildStorageObjectKey(config.C.OSS.Media, fmt.Sprintf("user/%d/1_demo.png", senderID))
	foreignKey := buildStorageObjectKey(config.C.OSS.Media, "user/9999/2_foreign.png")
	sessionKey := buildStorageObjectKey(config.C.OSS.Media, fmt.Sprintf("media/%s/3_agent.png", sessionID))
	extraRaw, err := json.Marshal(map[string]any{
		"attachments": []map[string]any{
			{"media_url": BuildMediaAccessURL(ownedKey)},
			{"media_url": BuildMediaAccessURL(ownedKey)},
			{"media_url": BuildMediaAccessURL(foreignKey)},
			{"media_url": BuildMediaAccessURL(sessionKey)},
			{"media_url": "https://other.example.com/not-managed.png"},
		},
	})
	if err != nil {
		t.Fatalf("marshal extra error: %v", err)
	}
	if err := testDB.DB.Create(&model.Message{
		MsgID:      msgID,
		SessionID:  sessionID,
		SenderID:   senderID,
		SenderType: 1,
		MsgType:    2,
		Content:    "attachment message",
		Extra:      extraRaw,
		CreatedAt:  now,
	}).Error; err != nil {
		t.Fatalf("create message error: %v", err)
	}
	if err := testDB.DB.Create(&model.UserInbox{
		UserID:    senderID,
		InboxSeq:  71,
		MsgID:     msgID,
		SessionID: sessionID,
		CreatedAt: now,
	}).Error; err != nil {
		t.Fatalf("create inbox error: %v", err)
	}

	if err := DeleteMessage(context.Background(), sessionID, msgID, MessageDeleteActor{
		UserID: senderID,
	}); err != nil {
		t.Fatalf("DeleteMessage() error = %v", err)
	}

	if len(removedKeys) != 1 {
		t.Fatalf("removed attachment count=%d want=1 keys=%v", len(removedKeys), removedKeys)
	}
	if removedKeys[0] != ownedKey {
		t.Fatalf("removed key=%q want=%q", removedKeys[0], ownedKey)
	}
}

func TestDeleteMessage_RevokedAgentMessageCleansSessionAttachments(t *testing.T) {
	testDB, cleanup := setupMessageTest(t)
	defer cleanup()

	const (
		sessionID = "agent-revoke-attachment-cleanup"
		ownerID   = int64(8702)
		agentID   = int64(9702)
		msgID     = int64(7001152)
	)

	originalConfig := config.C
	originalAsync := messageAttachmentCleanupAsync
	originalEnsure := messageAttachmentEnsureOSSReady
	originalRemove := messageAttachmentRemoveObject
	t.Cleanup(func() {
		config.C = originalConfig
		messageAttachmentCleanupAsync = originalAsync
		messageAttachmentEnsureOSSReady = originalEnsure
		messageAttachmentRemoveObject = originalRemove
	})

	config.C.OSS.Media.Bucket = "media-bucket"
	config.C.OSS.Media.StorageDir = "prod"
	messageAttachmentCleanupAsync = func(fn func()) { fn() }
	messageAttachmentEnsureOSSReady = func() error { return nil }

	removedKeys := make([]string, 0, 2)
	messageAttachmentRemoveObject = func(_ context.Context, bucket, objectKey string) error {
		if bucket != "media-bucket" {
			t.Fatalf("cleanup bucket=%q want=media-bucket", bucket)
		}
		removedKeys = append(removedKeys, objectKey)
		return nil
	}

	now := time.Now().UTC()
	lastMsgID := msgID
	if err := testDB.DB.Create(&model.Session{
		SessionID:      sessionID,
		OwnerID:        ownerID,
		SessionType:    model.SessionTypeDirect,
		LastMsgID:      &lastMsgID,
		LastMsgSummary: "agent attachment message",
		CreatedAt:      now,
		UpdatedAt:      now,
	}).Error; err != nil {
		t.Fatalf("create session error: %v", err)
	}
	for _, member := range []model.SessionMember{
		{
			SessionID:     sessionID,
			MemberID:      ownerID,
			MemberType:    1,
			Role:          3,
			UnreadCount:   1,
			LastReadMsgID: 0,
			JoinedAt:      now,
			LastActiveAt:  now,
		},
		{
			SessionID:    sessionID,
			MemberID:     agentID,
			MemberType:   2,
			Role:         1,
			JoinedAt:     now,
			LastActiveAt: now,
		},
	} {
		member := member
		if err := testDB.DB.Create(&member).Error; err != nil {
			t.Fatalf("create member(%d,%d) error: %v", member.MemberID, member.MemberType, err)
		}
	}

	sessionObjectKey := buildStorageObjectKey(config.C.OSS.Media, fmt.Sprintf("media/%s/1_agent.png", sessionID))
	foreignObjectKey := buildStorageObjectKey(config.C.OSS.Media, "media/other-session/2_agent.png")
	extraRaw, err := json.Marshal(map[string]any{
		"attachments": []map[string]any{
			{"media_url": sessionObjectKey},
			{"media_url": foreignObjectKey},
		},
	})
	if err != nil {
		t.Fatalf("marshal extra error: %v", err)
	}
	if err := testDB.DB.Create(&model.Message{
		MsgID:      msgID,
		SessionID:  sessionID,
		SenderID:   agentID,
		SenderType: 2,
		MsgType:    2,
		Content:    "agent attachment message",
		Extra:      extraRaw,
		CreatedAt:  now,
	}).Error; err != nil {
		t.Fatalf("create message error: %v", err)
	}
	if err := testDB.DB.Create(&model.UserInbox{
		UserID:    ownerID,
		InboxSeq:  72,
		MsgID:     msgID,
		SessionID: sessionID,
		CreatedAt: now,
	}).Error; err != nil {
		t.Fatalf("create inbox error: %v", err)
	}

	if err := DeleteMessage(context.Background(), sessionID, msgID, MessageDeleteActor{
		UserID:  ownerID,
		AgentID: agentID,
	}); err != nil {
		t.Fatalf("DeleteMessage() error = %v", err)
	}

	if len(removedKeys) != 1 {
		t.Fatalf("removed attachment count=%d want=1 keys=%v", len(removedKeys), removedKeys)
	}
	if removedKeys[0] != sessionObjectKey {
		t.Fatalf("removed key=%q want=%q", removedKeys[0], sessionObjectKey)
	}
}

func TestEditMessage_CreatesEditInboxAndBroadcastsPushEdit(t *testing.T) {
	testDB, cleanup := setupMessageTest(t)
	defer cleanup()

	const (
		sessionID = "agent-edit-direct-session"
		ownerID   = int64(8801)
		agentID   = int64(9801)
		msgID     = int64(7002101)
	)

	now := time.Now().UTC()
	lastMsgID := msgID
	if err := testDB.DB.Create(&model.Session{
		SessionID:      sessionID,
		OwnerID:        ownerID,
		SessionType:    model.SessionTypeDirect,
		LastMsgID:      &lastMsgID,
		LastMsgSummary: "old content",
		CreatedAt:      now,
		UpdatedAt:      now,
	}).Error; err != nil {
		t.Fatalf("create session error: %v", err)
	}
	for _, member := range []model.SessionMember{
		{
			SessionID:    sessionID,
			MemberID:     ownerID,
			MemberType:   1,
			Role:         3,
			JoinedAt:     now,
			LastActiveAt: now,
		},
		{
			SessionID:    sessionID,
			MemberID:     agentID,
			MemberType:   2,
			Role:         1,
			JoinedAt:     now,
			LastActiveAt: now,
		},
	} {
		member := member
		if err := testDB.DB.Create(&member).Error; err != nil {
			t.Fatalf("create member(%d,%d) error: %v", member.MemberID, member.MemberType, err)
		}
	}
	if err := testDB.DB.Create(&model.Message{
		MsgID:      msgID,
		SessionID:  sessionID,
		SenderID:   agentID,
		SenderType: 2,
		MsgType:    1,
		Content:    "old content",
		CreatedAt:  now,
	}).Error; err != nil {
		t.Fatalf("create message error: %v", err)
	}

	ctx := context.Background()
	if err := store.RDB.HSet(ctx, "im:ws:route:8801", "device-1", "node-edit").Err(); err != nil {
		t.Fatalf("seed ws route error: %v", err)
	}
	pubsub := store.RDB.Subscribe(ctx, "chan:node-edit")
	defer pubsub.Close()

	if err := EditMessage(ctx, sessionID, msgID, MessageEditActor{
		AgentID: agentID,
	}, "updated content"); err != nil {
		t.Fatalf("EditMessage() error = %v", err)
	}

	var msg model.Message
	if err := testDB.DB.Where("msg_id = ? AND session_id = ?", msgID, sessionID).First(&msg).Error; err != nil {
		t.Fatalf("reload edited message error: %v", err)
	}
	if msg.Content != "updated content" {
		t.Fatalf("message content=%q want updated content", msg.Content)
	}

	var session model.Session
	if err := testDB.DB.Where("session_id = ?", sessionID).First(&session).Error; err != nil {
		t.Fatalf("reload session error: %v", err)
	}
	if session.LastMsgSummary != "updated content" {
		t.Fatalf("last_msg_summary=%q want updated content", session.LastMsgSummary)
	}

	var inboxRows []model.UserInbox
	if err := testDB.DB.Where("user_id = ? AND session_id = ? AND msg_id = ?", ownerID, sessionID, msgID).
		Order("inbox_seq ASC").
		Find(&inboxRows).Error; err != nil {
		t.Fatalf("load edit inbox rows error: %v", err)
	}
	if len(inboxRows) != 1 {
		t.Fatalf("expected one edit inbox row, got=%d", len(inboxRows))
	}
	if inboxRows[0].EventKind != model.UserInboxEventKindEdit {
		t.Fatalf("event_kind=%q want=%q", inboxRows[0].EventKind, model.UserInboxEventKindEdit)
	}

	select {
	case envelope := <-pubsub.Channel():
		var payload struct {
			UserID  int64  `json:"user_id"`
			Cmd     string `json:"cmd"`
			Payload struct {
				InboxSeq  string `json:"inbox_seq"`
				MsgID     string `json:"msg_id"`
				SessionID string `json:"session_id"`
				Content   string `json:"content"`
				SyncEvent string `json:"sync_event"`
			} `json:"payload"`
		}
		if err := json.Unmarshal([]byte(envelope.Payload), &payload); err != nil {
			t.Fatalf("unmarshal realtime edit error: %v", err)
		}
		if payload.UserID != ownerID {
			t.Fatalf("push user_id=%d want=%d", payload.UserID, ownerID)
		}
		if payload.Cmd != "push_edit" {
			t.Fatalf("push cmd=%s want=push_edit", payload.Cmd)
		}
		if payload.Payload.MsgID != "7002101" {
			t.Fatalf("push msg_id=%s want=7002101", payload.Payload.MsgID)
		}
		if payload.Payload.SessionID != sessionID {
			t.Fatalf("push session_id=%s want=%s", payload.Payload.SessionID, sessionID)
		}
		if payload.Payload.Content != "updated content" {
			t.Fatalf("push content=%q want updated content", payload.Payload.Content)
		}
		if payload.Payload.SyncEvent != model.UserInboxEventKindEdit {
			t.Fatalf("push sync_event=%q want=%q", payload.Payload.SyncEvent, model.UserInboxEventKindEdit)
		}
		if payload.Payload.InboxSeq == "" {
			t.Fatal("expected push_edit inbox_seq")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for push_edit")
	}
}

func TestDeleteMessage_OwnerCannotRevokeForeignAgentMessageInDirectSession(t *testing.T) {
	testDB, cleanup := setupMessageTest(t)
	defer cleanup()

	const (
		sessionID = "owner-revoke-foreign-agent-direct-session"
		ownerID   = int64(8601)
		agentID   = int64(9601)
		msgID     = int64(7001201)
	)

	now := time.Now().UTC()
	lastMsgID := msgID
	if err := testDB.DB.Create(&model.Agent{
		ID:        agentID,
		OwnerID:   ownerID + 1,
		AgentName: "foreign-agent",
		Status:    model.AgentStatusActive,
	}).Error; err != nil {
		t.Fatalf("create agent error: %v", err)
	}
	if err := testDB.DB.Create(&model.Session{
		SessionID:      sessionID,
		OwnerID:        ownerID,
		SessionType:    model.SessionTypeDirect,
		LastMsgID:      &lastMsgID,
		LastMsgSummary: "foreign agent reply",
		CreatedAt:      now,
		UpdatedAt:      now,
	}).Error; err != nil {
		t.Fatalf("create session error: %v", err)
	}
	for _, member := range []model.SessionMember{
		{
			SessionID:     sessionID,
			MemberID:      ownerID,
			MemberType:    1,
			Role:          3,
			UnreadCount:   1,
			LastReadMsgID: 0,
			JoinedAt:      now,
			LastActiveAt:  now,
		},
		{
			SessionID:    sessionID,
			MemberID:     agentID,
			MemberType:   2,
			Role:         1,
			JoinedAt:     now,
			LastActiveAt: now,
		},
	} {
		member := member
		if err := testDB.DB.Create(&member).Error; err != nil {
			t.Fatalf("create member(%d,%d) error: %v", member.MemberID, member.MemberType, err)
		}
	}
	if err := testDB.DB.Create(&model.Message{
		MsgID:      msgID,
		SessionID:  sessionID,
		SenderID:   agentID,
		SenderType: 2,
		MsgType:    1,
		Content:    "foreign agent reply",
		CreatedAt:  now,
	}).Error; err != nil {
		t.Fatalf("create message error: %v", err)
	}

	err := DeleteMessage(context.Background(), sessionID, msgID, MessageDeleteActor{
		UserID: ownerID,
	})
	if err == nil {
		t.Fatal("expected permission error for foreign agent message")
	}
	if err.Error() != "20008" {
		t.Fatalf("expected error 20008, got %v", err)
	}
}

func TestRevokeMessageForStop_CreatesSyntheticTombstoneWithoutOriginalInbox(t *testing.T) {
	testDB, cleanup := setupMessageTest(t)
	defer cleanup()

	const (
		sessionID = "stop-revoke-session"
		ownerID   = int64(8201)
		agentID   = int64(9901)
		msgID     = int64(7002001)
	)

	now := time.Now().UTC()
	lastMsgID := msgID
	if err := testDB.DB.Create(&model.Session{
		SessionID:      sessionID,
		OwnerID:        ownerID,
		SessionType:    1,
		LastMsgID:      &lastMsgID,
		LastMsgSummary: "",
		CreatedAt:      now,
		UpdatedAt:      now,
	}).Error; err != nil {
		t.Fatalf("create session error: %v", err)
	}
	for _, member := range []model.SessionMember{
		{
			SessionID:    sessionID,
			MemberID:     ownerID,
			MemberType:   1,
			UnreadCount:  4,
			JoinedAt:     now,
			LastActiveAt: now,
		},
		{
			SessionID:    sessionID,
			MemberID:     agentID,
			MemberType:   2,
			JoinedAt:     now,
			LastActiveAt: now,
		},
	} {
		m := member
		if err := testDB.DB.Create(&m).Error; err != nil {
			t.Fatalf("create member(%d,%d) error: %v", m.MemberID, m.MemberType, err)
		}
	}
	if err := testDB.DB.Create(&model.Message{
		MsgID:      msgID,
		SessionID:  sessionID,
		ThreadID:   "topic-revoke-b",
		SenderID:   agentID,
		SenderType: 2,
		MsgType:    4,
		Content:    "",
		CreatedAt:  now,
	}).Error; err != nil {
		t.Fatalf("create placeholder message error: %v", err)
	}

	ctx := context.Background()
	if err := store.RDB.HSet(ctx, "im:ws:route:8201", "device-1", "node-stop-revoke").Err(); err != nil {
		t.Fatalf("seed ws route error: %v", err)
	}
	if err := store.RDB.Set(ctx, "im:inbox_seq:8201", 0, 0).Err(); err != nil {
		t.Fatalf("seed inbox_seq error: %v", err)
	}
	if err := store.RDB.HSet(ctx, "im:unread:8201", sessionID, 4).Err(); err != nil {
		t.Fatalf("seed unread hash error: %v", err)
	}
	pubsub := store.RDB.Subscribe(ctx, "chan:node-stop-revoke")
	defer pubsub.Close()

	if err := RevokeMessageForStop(ctx, sessionID, msgID); err != nil {
		t.Fatalf("RevokeMessageForStop() error = %v", err)
	}

	var msg model.Message
	if err := testDB.DB.Where("msg_id = ? AND session_id = ?", msgID, sessionID).First(&msg).Error; err != nil {
		t.Fatalf("reload revoked message error: %v", err)
	}
	if !msg.IsDeleted || !msg.IsRevoked {
		t.Fatalf("expected message deleted+revoked, got deleted=%t revoked=%t", msg.IsDeleted, msg.IsRevoked)
	}

	var ownerMember model.SessionMember
	if err := testDB.DB.Where("session_id = ? AND member_id = ? AND member_type = 1", sessionID, ownerID).First(&ownerMember).Error; err != nil {
		t.Fatalf("reload owner session member error: %v", err)
	}
	if ownerMember.UnreadCount != 4 {
		t.Fatalf("owner unread_count=%d want=4", ownerMember.UnreadCount)
	}
	if unread, err := store.RDB.HGet(ctx, "im:unread:8201", sessionID).Result(); err != nil {
		t.Fatalf("reload unread hash error: %v", err)
	} else if unread != "4" {
		t.Fatalf("redis unread=%s want=4", unread)
	}

	var inboxRows []model.UserInbox
	if err := testDB.DB.
		Where("user_id = ? AND session_id = ? AND msg_id = ?", ownerID, sessionID, msgID).
		Order("inbox_seq ASC").
		Find(&inboxRows).Error; err != nil {
		t.Fatalf("load synthetic revoke inbox rows error: %v", err)
	}
	if len(inboxRows) != 1 {
		t.Fatalf("expected one synthetic revoke inbox row, got=%d", len(inboxRows))
	}
	if inboxRows[0].InboxSeq != 1 {
		t.Fatalf("synthetic revoke inbox_seq=%d want=1", inboxRows[0].InboxSeq)
	}

	select {
	case envelope := <-pubsub.Channel():
		var payload struct {
			UserID  int64  `json:"user_id"`
			Cmd     string `json:"cmd"`
			Payload struct {
				InboxSeq           string `json:"inbox_seq"`
				MsgID              string `json:"msg_id"`
				SessionID          string `json:"session_id"`
				ThreadID           string `json:"thread_id"`
				SessionUnreadCount int    `json:"session_unread_count"`
				IsRevoked          bool   `json:"is_revoked"`
			} `json:"payload"`
		}
		if err := json.Unmarshal([]byte(envelope.Payload), &payload); err != nil {
			t.Fatalf("unmarshal synthetic realtime revoke error: %v", err)
		}
		if payload.UserID != ownerID {
			t.Fatalf("push user_id=%d want=%d", payload.UserID, ownerID)
		}
		if payload.Cmd != "push_revoke" {
			t.Fatalf("push cmd=%s want=push_revoke", payload.Cmd)
		}
		if payload.Payload.InboxSeq != "1" {
			t.Fatalf("push inbox_seq=%s want=1", payload.Payload.InboxSeq)
		}
		if payload.Payload.MsgID != "7002001" {
			t.Fatalf("push msg_id=%s want=7002001", payload.Payload.MsgID)
		}
		if payload.Payload.SessionID != sessionID {
			t.Fatalf("push session_id=%s want=%s", payload.Payload.SessionID, sessionID)
		}
		if payload.Payload.ThreadID != "topic-revoke-b" {
			t.Fatalf("push thread_id=%s want=topic-revoke-b", payload.Payload.ThreadID)
		}
		if payload.Payload.SessionUnreadCount != 4 {
			t.Fatalf("push session_unread_count=%d want=4", payload.Payload.SessionUnreadCount)
		}
		if !payload.Payload.IsRevoked {
			t.Fatal("expected synthetic push_revoke is_revoked=true")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for synthetic push_revoke")
	}
}
