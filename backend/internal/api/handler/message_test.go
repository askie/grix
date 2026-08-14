package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/askie/grix/backend/internal/model"
	jwtpkg "github.com/askie/grix/backend/internal/pkg/jwt"
	"github.com/askie/grix/backend/internal/pkg/logger"
	"github.com/askie/grix/backend/internal/pkg/snowflake"
	"github.com/askie/grix/backend/internal/pkg/testutil"
	"github.com/askie/grix/backend/internal/store"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

func TestMessageHistorySyncsOnlyLatestPageAndKeepsExistingHistoryOnFailure(t *testing.T) {
	r, testDB, cleanup := setupMessageHandlerTest(t)
	defer cleanup()

	userID := int64(12011)
	token := createMessageTestUser(t, testDB, userID, "historysyncuser")
	sessionID := "message-history-sync-session"
	createMessageTestSession(t, testDB, userID, sessionID)
	createTestMessageData(testDB, sessionID, userID, 100, "existing")

	previousSync := syncBoundSessionHistory
	previousLogger := logger.L
	logger.L = zap.NewNop().Sugar()
	calls := 0
	syncBoundSessionHistory = func(_ context.Context, gotUserID int64, gotSessionID string) (int, error) {
		calls++
		if gotUserID != userID || gotSessionID != sessionID {
			t.Fatalf("sync args user=%d session=%s", gotUserID, gotSessionID)
		}
		return 0, errors.New("connector offline")
	}
	t.Cleanup(func() {
		syncBoundSessionHistory = previousSync
		logger.L = previousLogger
	})

	req, _ := http.NewRequest(http.MethodGet, "/messages?session_id="+sessionID, nil)
	req.Header.Set("Authorization", token)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("latest history status=%d body=%s", w.Code, w.Body.String())
	}
	if calls != 1 {
		t.Fatalf("latest history sync calls=%d want=1", calls)
	}

	req, _ = http.NewRequest(http.MethodGet, "/messages?session_id="+sessionID+"&before_id=100", nil)
	req.Header.Set("Authorization", token)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("older history status=%d body=%s", w.Code, w.Body.String())
	}
	if calls != 1 {
		t.Fatalf("older history unexpectedly synced, calls=%d", calls)
	}
}

func TestMessageHistorySkipsNativeSyncForNonOwner(t *testing.T) {
	r, testDB, cleanup := setupMessageHandlerTest(t)
	defer cleanup()

	ownerID := int64(12021)
	memberID := int64(12022)
	_ = createMessageTestUser(t, testDB, ownerID, "historyowner")
	memberToken := createMessageTestUser(t, testDB, memberID, "historymember")
	sessionID := "message-history-non-owner-session"
	now := time.Now()
	if err := testDB.DB.Create(&model.Session{
		SessionID: sessionID, OwnerID: ownerID, SessionType: model.SessionTypeGroup,
	}).Error; err != nil {
		t.Fatalf("create group: %v", err)
	}
	if err := testDB.DB.Create(&model.SessionMember{
		SessionID: sessionID, MemberID: memberID, MemberType: 1, Role: 1, JoinedAt: now, LastActiveAt: now,
	}).Error; err != nil {
		t.Fatalf("create member: %v", err)
	}

	previousSync := syncBoundSessionHistory
	calls := 0
	syncBoundSessionHistory = func(context.Context, int64, string) (int, error) {
		calls++
		return 0, nil
	}
	t.Cleanup(func() { syncBoundSessionHistory = previousSync })

	req, _ := http.NewRequest(http.MethodGet, "/messages?session_id="+sessionID, nil)
	req.Header.Set("Authorization", memberToken)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("history status=%d body=%s", w.Code, w.Body.String())
	}
	if calls != 0 {
		t.Fatalf("non-owner native sync calls=%d want=0", calls)
	}
}

func TestMessageHistoryReturnsWhenNativeSyncExceedsWaitWindow(t *testing.T) {
	r, testDB, cleanup := setupMessageHandlerTest(t)
	defer cleanup()

	userID := int64(12031)
	token := createMessageTestUser(t, testDB, userID, "historytimeout")
	sessionID := "message-history-timeout-session"
	createMessageTestSession(t, testDB, userID, sessionID)

	previousSync := syncBoundSessionHistory
	previousWait := nativeHistorySyncWait
	release := make(chan struct{})
	completed := make(chan struct{})
	syncBoundSessionHistory = func(context.Context, int64, string) (int, error) {
		<-release
		close(completed)
		return 0, nil
	}
	nativeHistorySyncWait = 10 * time.Millisecond
	t.Cleanup(func() {
		close(release)
		<-completed
		syncBoundSessionHistory = previousSync
		nativeHistorySyncWait = previousWait
	})

	started := time.Now()
	req, _ := http.NewRequest(http.MethodGet, "/messages?session_id="+sessionID, nil)
	req.Header.Set("Authorization", token)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("history status=%d body=%s", w.Code, w.Body.String())
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("history request blocked for %s", elapsed)
	}
}

func init() {
	gin.SetMode(gin.TestMode)
}

func setupMessageHandlerTest(t *testing.T) (*gin.Engine, *testutil.TestDB, func()) {
	t.Helper()

	testDB := testutil.NewTestDB()
	store.DB = testDB.DB
	store.RDB = testutil.NewMockRedis()

	jwtpkg.Init("test-secret-key", 3600, 86400)
	_ = snowflake.Init(1)

	r := gin.New()

	// Auth middleware
	r.Use(func(c *gin.Context) {
		token := c.GetHeader("Authorization")
		if token != "" {
			claims, _ := jwtpkg.ValidateAccessToken(token)
			if claims != nil {
				c.Set("user_id", claims.UserID)
			}
		}
		c.Next()
	})

	r.GET("/messages", MessageHistory)

	return r, testDB, func() { testDB.Close() }
}

func createMessageTestUser(t *testing.T, db *testutil.TestDB, userID int64, username string) string {
	t.Helper()
	fixture := testutil.NewFixtureBuilder(db.DB)
	user := fixture.CreateUser(func(u *model.User) {
		u.ID = userID
		u.Username = username
	})

	token, _, _ := jwtpkg.GenerateAccessToken(user.ID)
	return token
}

func createMessageTestSession(t *testing.T, db *testutil.TestDB, userID int64, sessionID string) {
	t.Helper()
	now := time.Now()

	session := model.Session{
		SessionID:   sessionID,
		OwnerID:     userID,
		SessionType: 1,
	}
	db.DB.Create(&session)

	member := model.SessionMember{
		SessionID:    sessionID,
		MemberID:     userID,
		MemberType:   1,
		Role:         3,
		LastActiveAt: now,
		JoinedAt:     now,
	}
	db.DB.Create(&member)
}

func createTestMessageData(db *testutil.TestDB, sessionID string, senderID int64, msgID int64, content string) {
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
	r, testDB, cleanup := setupMessageHandlerTest(t)
	defer cleanup()

	userID := int64(12001)
	token := createMessageTestUser(t, testDB, userID, "messageuser")
	sessionID := "test-message-session"

	t.Run("missing session_id", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodGet, "/messages", nil)
		req.Header.Set("Authorization", token)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusBadRequest {
			t.Errorf("expected status 400, got %d", w.Code)
		}
	})

	t.Run("empty history", func(t *testing.T) {
		createMessageTestSession(t, testDB, userID, sessionID)

		req, _ := http.NewRequest(http.MethodGet, "/messages?session_id="+sessionID, nil)
		req.Header.Set("Authorization", token)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("expected status 200, got %d", w.Code)
		}
	})

	t.Run("with messages", func(t *testing.T) {
		// Create test messages
		for i := 1; i <= 5; i++ {
			createTestMessageData(testDB, sessionID, userID, int64(i*100), "Test message")
		}

		req, _ := http.NewRequest(http.MethodGet, "/messages?session_id="+sessionID, nil)
		req.Header.Set("Authorization", token)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("expected status 200, got %d", w.Code)
		}

		// Verify messages are returned
		var resp map[string]interface{}
		json.Unmarshal(w.Body.Bytes(), &resp)
		data := resp["data"].(map[string]interface{})
		messages := data["messages"].([]interface{})
		if len(messages) != 5 {
			t.Errorf("expected 5 messages, got %d", len(messages))
		}
	})

	t.Run("with pagination", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodGet, "/messages?session_id="+sessionID+"&limit=2", nil)
		req.Header.Set("Authorization", token)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("expected status 200, got %d", w.Code)
		}

		var resp map[string]interface{}
		json.Unmarshal(w.Body.Bytes(), &resp)
		data := resp["data"].(map[string]interface{})
		hasMore := data["has_more"].(bool)
		if !hasMore {
			t.Error("expected has_more to be true")
		}
	})

	t.Run("with before_id", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodGet, "/messages?session_id="+sessionID+"&before_id=300", nil)
		req.Header.Set("Authorization", token)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("expected status 200, got %d", w.Code)
		}

		var resp map[string]interface{}
		json.Unmarshal(w.Body.Bytes(), &resp)
		data := resp["data"].(map[string]interface{})
		messages := data["messages"].([]interface{})
		// Should only get messages with ID < 300
		if len(messages) != 2 {
			t.Errorf("expected 2 messages (IDs < 300), got %d", len(messages))
		}
	})

	t.Run("user not in session", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodGet, "/messages?session_id=unknown-session", nil)
		req.Header.Set("Authorization", token)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusNotFound {
			t.Errorf("expected status 404, got %d", w.Code)
		}
	})

	t.Run("banned group returns forbidden", func(t *testing.T) {
		const bannedSessionID = "banned-message-session"
		now := time.Now()
		if err := testDB.DB.Create(&model.Session{
			SessionID:        bannedSessionID,
			OwnerID:          userID,
			SessionType:      model.SessionTypeGroup,
			GroupName:        "Banned Group",
			ModerationStatus: model.SessionModerationStatusBanned,
		}).Error; err != nil {
			t.Fatalf("create banned session: %v", err)
		}
		if err := testDB.DB.Create(&model.SessionMember{
			SessionID: bannedSessionID, MemberID: userID, MemberType: 1, Role: 3, JoinedAt: now, LastActiveAt: now,
		}).Error; err != nil {
			t.Fatalf("create banned session member: %v", err)
		}

		req, _ := http.NewRequest(http.MethodGet, "/messages?session_id="+bannedSessionID, nil)
		req.Header.Set("Authorization", token)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusForbidden {
			t.Errorf("expected status 403, got %d", w.Code)
		}
	})
}

func TestMessageHistoryDefaultParams(t *testing.T) {
	r, testDB, cleanup := setupMessageHandlerTest(t)
	defer cleanup()

	userID := int64(12002)
	token := createMessageTestUser(t, testDB, userID, "defaultuser")
	sessionID := "default-params-session"
	createMessageTestSession(t, testDB, userID, sessionID)

	// Test with no pagination params (should use defaults)
	req, _ := http.NewRequest(http.MethodGet, "/messages?session_id="+sessionID, nil)
	req.Header.Set("Authorization", token)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}
}
