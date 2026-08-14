package context

import (
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/askie/grix/backend/internal/llm/provider"
	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/snowflake"
	"github.com/askie/grix/backend/internal/pkg/testutil"
	"github.com/askie/grix/backend/internal/store"
)

func init() {
	_ = snowflake.Init(1)
}

func TestBuildPrompt(t *testing.T) {
	testDB := testutil.NewTestDB()
	defer testDB.Close()
	store.DB = testDB.DB

	t.Run("nil agent", func(t *testing.T) {
		messages := BuildPrompt("test-session", nil, "Hello", nil)

		// Should at least have user input
		if len(messages) == 0 {
			t.Error("expected at least one message")
		}

		// Last message should be user input
		lastMsg := messages[len(messages)-1]
		if lastMsg.Role != "user" {
			t.Error("last message should be from user")
		}
		if lastMsg.Content != "Hello" {
			t.Error("content should be user input")
		}
	})

	t.Run("with agent system prompt", func(t *testing.T) {
		agent := &model.Agent{
			ID:           1,
			SystemPrompt: "You are a helpful assistant.",
		}

		messages := BuildPrompt("test-session", agent, "Hi", nil)

		// First message should be system prompt
		if messages[0].Role != "system" {
			t.Error("first message should be system")
		}
		if messages[0].Content != "You are a helpful assistant." {
			t.Error("system prompt mismatch")
		}
	})

	t.Run("with RAG context", func(t *testing.T) {
		ragContext := []string{
			"Previous conversation 1",
			"Previous conversation 2",
		}

		messages := BuildPrompt("test-session", nil, "Question", ragContext)

		// Should have RAG context
		hasRAGContext := false
		for _, msg := range messages {
			if msg.Role == "system" && len(msg.Content) > 50 {
				hasRAGContext = true
				break
			}
		}
		if !hasRAGContext {
			t.Error("expected RAG context in messages")
		}
	})

	t.Run("empty user input", func(t *testing.T) {
		_ = BuildPrompt("test-session", nil, "", nil)

		// With empty input and no history, messages may be empty
		// This is expected behavior - no user input means no message
		// The function handles empty input gracefully
	})
}

func TestBuildPromptWithHistory(t *testing.T) {
	testDB := testutil.NewTestDB()
	defer testDB.Close()
	store.DB = testDB.DB

	// Create test session and messages
	sessionID := "history-test-session"
	now := testDB.DB.Config.NowFunc()

	// Create session
	session := model.Session{
		SessionID:   sessionID,
		OwnerID:     1,
		SessionType: 1,
	}
	testDB.DB.Create(&session)

	// Create historical messages
	messages := []model.Message{
		{
			MsgID:      1,
			SessionID:  sessionID,
			SenderID:   1,
			SenderType: 1, // user
			MsgType:    1,
			Content:    "Hello from user",
			CreatedAt:  now,
		},
		{
			MsgID:      2,
			SessionID:  sessionID,
			SenderID:   2,
			SenderType: 2, // assistant
			MsgType:    1,
			Content:    "Hello from assistant",
			CreatedAt:  now,
		},
	}
	for _, msg := range messages {
		testDB.DB.Create(&msg)
	}

	// Build prompt with history
	prompt := BuildPrompt(sessionID, nil, "New question", nil)

	// Should include user input
	if len(prompt) == 0 {
		t.Error("expected messages in prompt")
	}

	// Check that user input is last
	lastMsg := prompt[len(prompt)-1]
	if lastMsg.Role != "user" || lastMsg.Content != "New question" {
		t.Error("last message should be user input")
	}
}

func TestBuildPromptMessageRoles(t *testing.T) {
	testDB := testutil.NewTestDB()
	defer testDB.Close()
	store.DB = testDB.DB

	messages := BuildPrompt("role-test-session", nil, "Test", nil)

	for i, msg := range messages {
		if msg.Role != "system" && msg.Role != "user" && msg.Role != "assistant" {
			t.Errorf("message %d has invalid role: %s", i, msg.Role)
		}
	}
}

func TestBuildPromptTruncation(t *testing.T) {
	testDB := testutil.NewTestDB()
	defer testDB.Close()
	store.DB = testDB.DB

	t.Run("RAG context truncation", func(t *testing.T) {
		// Create very long RAG context
		longContext := make([]string, 100)
		for i := range longContext {
			longContext[i] = "This is a long piece of context information that should be truncated."
		}

		messages := BuildPrompt("trunc-session", nil, "Query", longContext)

		// Should still have messages
		if len(messages) == 0 {
			t.Error("expected messages with truncated context")
		}
	})

	t.Run("RAG context truncation keeps UTF8 valid", func(t *testing.T) {
		// Prefix byte length in BuildPrompt is 43 bytes, so this input
		// would be split in the middle of a UTF-8 rune by byte-based slicing.
		longRAG := strings.Repeat("你", 1318) + "a" + "你" + "tail"
		messages := BuildPrompt("trunc-utf8-session", nil, "Query", []string{longRAG})

		var ragMsg string
		for _, msg := range messages {
			if msg.Role == "system" {
				ragMsg = msg.Content
				break
			}
		}
		if ragMsg == "" {
			t.Fatal("expected system message for RAG context")
		}
		if !utf8.ValidString(ragMsg) {
			t.Fatalf("RAG system message must be valid UTF-8, got invalid bytes")
		}
		if len([]rune(ragMsg)) > maxRAGTokens*2 {
			t.Fatalf("RAG system message too long: got=%d max=%d", len([]rune(ragMsg)), maxRAGTokens*2)
		}
	})
}

func TestProviderMessageStructure(t *testing.T) {
	// Test that provider.Message is correctly structured
	msg := provider.Message{
		Role:    "user",
		Content: "Test message",
	}

	if msg.Role != "user" {
		t.Error("role mismatch")
	}
	if msg.Content != "Test message" {
		t.Error("content mismatch")
	}
}

func TestBuildPromptDedupsCurrentInputAlreadyInHistory(t *testing.T) {
	testDB := testutil.NewTestDB()
	defer testDB.Close()
	store.DB = testDB.DB

	sessionID := "dup-input-session"
	if err := testDB.DB.Create(&model.Session{
		SessionID:   sessionID,
		OwnerID:     1,
		SessionType: 1,
	}).Error; err != nil {
		t.Fatalf("create session error: %v", err)
	}
	if err := testDB.DB.Create(&model.Message{
		MsgID:      1001,
		SessionID:  sessionID,
		SenderID:   1,
		SenderType: 1,
		MsgType:    1,
		Content:    "重复输入",
	}).Error; err != nil {
		t.Fatalf("seed history error: %v", err)
	}

	messages := BuildPrompt(sessionID, nil, "重复输入", nil)
	dupCount := 0
	for _, m := range messages {
		if m.Role == "user" && m.Content == "重复输入" {
			dupCount++
		}
	}
	if dupCount != 1 {
		t.Fatalf("current user input should not duplicate history, got=%d want=1", dupCount)
	}
}

func TestBuildPromptTruncatesSystemPrompt(t *testing.T) {
	testDB := testutil.NewTestDB()
	defer testDB.Close()
	store.DB = testDB.DB

	agent := &model.Agent{
		ID:           1,
		SystemPrompt: strings.Repeat("S", maxSystemTokens*2+128),
	}

	messages := BuildPrompt("sys-trunc-session", agent, "hi", nil)
	if len(messages) == 0 {
		t.Fatal("expected prompt messages")
	}
	if messages[0].Role != "system" {
		t.Fatalf("first message should be system, got=%s", messages[0].Role)
	}
	if got := len([]rune(messages[0].Content)); got > maxSystemTokens*2 {
		t.Fatalf("system prompt not truncated, got=%d max=%d", got, maxSystemTokens*2)
	}
}

func TestBuildPromptHistoryBudgetAndPerMessageTruncation(t *testing.T) {
	testDB := testutil.NewTestDB()
	defer testDB.Close()
	store.DB = testDB.DB

	sessionID := "history-budget-session"
	if err := testDB.DB.Create(&model.Session{
		SessionID:   sessionID,
		OwnerID:     1,
		SessionType: 1,
	}).Error; err != nil {
		t.Fatalf("create session error: %v", err)
	}

	longText := strings.Repeat("A", maxHistoryMsgTokens*2+800)
	for i := 0; i < 40; i++ {
		senderType := int16(1)
		if i%2 == 1 {
			senderType = 2
		}
		if err := testDB.DB.Create(&model.Message{
			MsgID:      int64(2000 + i),
			SessionID:  sessionID,
			SenderID:   int64(10 + i),
			SenderType: senderType,
			MsgType:    1,
			Content:    longText,
		}).Error; err != nil {
			t.Fatalf("seed message error: %v", err)
		}
	}

	messages, stats := BuildPromptWithStats(sessionID, nil, "", nil)
	if stats.HistoryMessages == 0 {
		t.Fatal("expected history messages in prompt")
	}
	if stats.HistoryTokens > maxHistoryTokens {
		t.Fatalf("history token budget exceeded got=%d max=%d", stats.HistoryTokens, maxHistoryTokens)
	}
	if stats.HistoryMessages > maxRecentMessages {
		t.Fatalf("history message count exceeded got=%d max=%d", stats.HistoryMessages, maxRecentMessages)
	}

	for _, m := range messages {
		if m.Role != "user" && m.Role != "assistant" {
			continue
		}
		if got := len([]rune(m.Content)); got > maxHistoryMsgTokens*2 {
			t.Fatalf("history message not truncated, got=%d max=%d", got, maxHistoryMsgTokens*2)
		}
	}
}

func TestBuildPromptForUserWithStatsRespectsHistoryReset(t *testing.T) {
	testDB := testutil.NewTestDB()
	defer testDB.Close()
	store.DB = testDB.DB

	sessionID := "history-reset-session"
	userID := int64(3001)
	base := time.Now().Add(-time.Hour)

	if err := testDB.DB.Create(&model.Session{
		SessionID:   sessionID,
		OwnerID:     userID,
		SessionType: 1,
	}).Error; err != nil {
		t.Fatalf("create session error: %v", err)
	}

	oldMsgTime := base.Add(10 * time.Minute)
	newMsgTime := base.Add(50 * time.Minute)
	if err := testDB.DB.Create(&model.Message{
		MsgID:      30001,
		SessionID:  sessionID,
		SenderID:   userID,
		SenderType: 1,
		MsgType:    1,
		Content:    "old-history",
		CreatedAt:  oldMsgTime,
	}).Error; err != nil {
		t.Fatalf("create old message error: %v", err)
	}
	if err := testDB.DB.Create(&model.Message{
		MsgID:      30002,
		SessionID:  sessionID,
		SenderID:   userID + 1,
		SenderType: 1,
		MsgType:    1,
		Content:    "new-history",
		CreatedAt:  newMsgTime,
	}).Error; err != nil {
		t.Fatalf("create new message error: %v", err)
	}

	if err := testDB.DB.Create(&model.SessionHistoryReset{
		SessionID:     sessionID,
		UserID:        userID,
		DeletedBefore: base.Add(30 * time.Minute),
		CreatedAt:     time.Now(),
		UpdatedAt:     time.Now(),
	}).Error; err != nil {
		t.Fatalf("create session history reset error: %v", err)
	}

	messages, _ := BuildPromptForUserWithStats(sessionID, userID, nil, "question", nil)
	var contents []string
	for _, m := range messages {
		contents = append(contents, m.Content)
	}
	joined := strings.Join(contents, "|")
	if strings.Contains(joined, "old-history") {
		t.Fatalf("old history before reset should be excluded, got=%v", contents)
	}
	if !strings.Contains(joined, "new-history") {
		t.Fatalf("new history after reset should remain, got=%v", contents)
	}
}

// TestBuildPromptForUserWithStatsGroupHidesMessagesBeforeJoinedAt 验证群聊新成员
// 触发 AI 时，prompt 上下文不会带入其入群之前的群消息，与 /messages/history 口径一致。
func TestBuildPromptForUserWithStatsGroupHidesMessagesBeforeJoinedAt(t *testing.T) {
	testDB := testutil.NewTestDB()
	defer testDB.Close()
	store.DB = testDB.DB

	sessionID := "group-joined-filter-session"
	ownerID := int64(3101)
	newUserID := int64(3102)
	base := time.Now().Add(-2 * time.Hour)

	if err := testDB.DB.Create(&model.Session{
		SessionID:   sessionID,
		OwnerID:     ownerID,
		SessionType: 2,
		GroupName:   "joined filter group",
	}).Error; err != nil {
		t.Fatalf("create session error: %v", err)
	}
	if err := testDB.DB.Create(&model.SessionMember{
		SessionID:    sessionID,
		MemberID:     ownerID,
		MemberType:   1,
		Role:         3,
		JoinedAt:     base,
		LastActiveAt: base,
	}).Error; err != nil {
		t.Fatalf("create owner member error: %v", err)
	}

	oldMsgTime := base.Add(10 * time.Minute)
	joinedAt := base.Add(30 * time.Minute)
	newMsgTime := base.Add(40 * time.Minute)

	if err := testDB.DB.Create(&model.Message{
		MsgID:      31001,
		SessionID:  sessionID,
		SenderID:   ownerID,
		SenderType: 1,
		MsgType:    1,
		Content:    "before-joined-history",
		CreatedAt:  oldMsgTime,
	}).Error; err != nil {
		t.Fatalf("create old message error: %v", err)
	}
	if err := testDB.DB.Create(&model.Message{
		MsgID:      31002,
		SessionID:  sessionID,
		SenderID:   ownerID,
		SenderType: 1,
		MsgType:    1,
		Content:    "after-joined-history",
		CreatedAt:  newMsgTime,
	}).Error; err != nil {
		t.Fatalf("create new message error: %v", err)
	}
	if err := testDB.DB.Create(&model.SessionMember{
		SessionID:    sessionID,
		MemberID:     newUserID,
		MemberType:   1,
		Role:         1,
		JoinedAt:     joinedAt,
		LastActiveAt: joinedAt,
	}).Error; err != nil {
		t.Fatalf("create new user member error: %v", err)
	}

	messages, _ := BuildPromptForUserWithStats(sessionID, newUserID, nil, "question", nil)
	var contents []string
	for _, m := range messages {
		contents = append(contents, m.Content)
	}
	joined := strings.Join(contents, "|")
	if strings.Contains(joined, "before-joined-history") {
		t.Fatalf("group history before joined_at should be excluded, got=%v", contents)
	}
	if !strings.Contains(joined, "after-joined-history") {
		t.Fatalf("group history after joined_at should remain, got=%v", contents)
	}
}

// TestBuildPromptForUserWithStatsDirectKeepsOldHistory 验证私聊不受 joined_at 限制，
// 私聊成员仍能看到 joined_at 之前的历史（与 /messages/history 口径一致）。
func TestBuildPromptForUserWithStatsDirectKeepsOldHistory(t *testing.T) {
	testDB := testutil.NewTestDB()
	defer testDB.Close()
	store.DB = testDB.DB

	sessionID := "direct-keep-history-session"
	ownerID := int64(3201)
	peerID := int64(3202)
	base := time.Now().Add(-2 * time.Hour)

	if err := testDB.DB.Create(&model.Session{
		SessionID:   sessionID,
		OwnerID:     ownerID,
		SessionType: 1,
	}).Error; err != nil {
		t.Fatalf("create session error: %v", err)
	}
	if err := testDB.DB.Create(&model.SessionMember{
		SessionID:    sessionID,
		MemberID:     ownerID,
		MemberType:   1,
		Role:         3,
		JoinedAt:     base,
		LastActiveAt: base,
	}).Error; err != nil {
		t.Fatalf("create owner member error: %v", err)
	}
	// peer 的 joined_at 晚于消息时间，但私聊不应被过滤。
	if err := testDB.DB.Create(&model.SessionMember{
		SessionID:    sessionID,
		MemberID:     peerID,
		MemberType:   1,
		Role:         1,
		JoinedAt:     base.Add(30 * time.Minute),
		LastActiveAt: base.Add(30 * time.Minute),
	}).Error; err != nil {
		t.Fatalf("create peer member error: %v", err)
	}
	if err := testDB.DB.Create(&model.Message{
		MsgID:      32001,
		SessionID:  sessionID,
		SenderID:   ownerID,
		SenderType: 1,
		MsgType:    1,
		Content:    "direct-old-history",
		CreatedAt:  base.Add(10 * time.Minute),
	}).Error; err != nil {
		t.Fatalf("create old message error: %v", err)
	}

	messages, _ := BuildPromptForUserWithStats(sessionID, peerID, nil, "question", nil)
	var contents []string
	for _, m := range messages {
		contents = append(contents, m.Content)
	}
	joined := strings.Join(contents, "|")
	if !strings.Contains(joined, "direct-old-history") {
		t.Fatalf("direct history before joined_at should remain, got=%v", contents)
	}
}

func TestBuildPromptDelegateOriginGetsAssistantRole(t *testing.T) {
	testDB := testutil.NewTestDB()
	defer testDB.Close()
	store.DB = testDB.DB

	sessionID := "delegate-origin-session"
	if err := testDB.DB.Create(&model.Session{
		SessionID:   sessionID,
		OwnerID:     1,
		SessionType: 1,
	}).Error; err != nil {
		t.Fatalf("create session error: %v", err)
	}

	now := time.Now()
	// User message
	testDB.DB.Create(&model.Message{
		MsgID:      40001,
		SessionID:  sessionID,
		SenderID:   100,
		SenderType: 1,
		MsgType:    1,
		Content:    "Hello from user",
		CreatedAt:  now,
	})
	// Delegate message: sender_type=1 but delegate_origin=true
	testDB.DB.Create(&model.Message{
		MsgID:      40002,
		SessionID:  sessionID,
		SenderID:   200,
		SenderType: 1, // human sender_type (the bug scenario)
		MsgType:    1,
		Content:    "Hello from delegate AI",
		Extra:      []byte(`{"delegate_origin":true}`),
		CreatedAt:  now,
	})

	messages, _ := BuildPromptForUserWithStats(sessionID, 0, nil, "New question", nil)
	// Find delegate message in prompt
	foundAssistant := false
	for _, m := range messages {
		if m.Content == "Hello from delegate AI" {
			if m.Role != "assistant" {
				t.Fatalf("delegate_origin message should be assistant, got=%s", m.Role)
			}
			foundAssistant = true
		}
	}
	if !foundAssistant {
		t.Fatal("delegate_origin message not found in prompt")
	}
}

func TestMergeConsecutiveRoles(t *testing.T) {
	tests := []struct {
		name     string
		input    []provider.Message
		wantLen  int
		wantLast string
	}{
		{
			name:    "empty",
			input:   nil,
			wantLen: 0,
		},
		{
			name: "no consecutive same role",
			input: []provider.Message{
				{Role: "user", Content: "A"},
				{Role: "assistant", Content: "B"},
				{Role: "user", Content: "C"},
			},
			wantLen:  3,
			wantLast: "C",
		},
		{
			name: "consecutive user merged",
			input: []provider.Message{
				{Role: "user", Content: "A"},
				{Role: "user", Content: "B"},
				{Role: "assistant", Content: "C"},
			},
			wantLen:  2,
			wantLast: "C",
		},
		{
			name: "system not merged",
			input: []provider.Message{
				{Role: "system", Content: "S1"},
				{Role: "system", Content: "S2"},
				{Role: "user", Content: "U"},
			},
			wantLen: 3,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := mergeConsecutiveRoles(tc.input)
			if len(got) != tc.wantLen {
				t.Fatalf("len=%d want=%d msgs=%+v", len(got), tc.wantLen, got)
			}
			if tc.wantLast != "" && got[len(got)-1].Content != tc.wantLast {
				t.Fatalf("last content=%q want=%q", got[len(got)-1].Content, tc.wantLast)
			}
		})
	}

	// Verify merged content
	merged := mergeConsecutiveRoles([]provider.Message{
		{Role: "user", Content: "line1"},
		{Role: "user", Content: "line2"},
	})
	if merged[0].Content != "line1\nline2" {
		t.Fatalf("merged content=%q want=%q", merged[0].Content, "line1\nline2")
	}
}
