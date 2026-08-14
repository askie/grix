package handler

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/middleware"
	"github.com/askie/grix/backend/internal/pkg/testutil"
	"github.com/askie/grix/backend/internal/store"
	"github.com/gin-gonic/gin"
	"gorm.io/datatypes"
)

func setupAgentMessageHandlerTest(t *testing.T) (*gin.Engine, *testutil.TestDB, func()) {
	t.Helper()

	testDB := testutil.NewTestDB()
	store.DB = testDB.DB
	store.RDB = testutil.NewMockRedis()

	r := gin.New()
	r.Use(middleware.AgentAPIAuth())
	r.GET("/agent-api/messages/history", AgentMessageHistory)
	r.GET("/agent-api/messages/search", AgentMessageSearch)
	r.POST("/agent-api/messages/delete", AgentMessageDelete)
	r.POST("/agent-api/messages/edit", AgentMessageEdit)
	r.POST("/agent-api/oss/presign", AgentOSSPresign)

	return r, testDB, func() { testDB.Close() }
}

func seedAgentMessageGroup(
	t *testing.T,
	db *testutil.TestDB,
	ownerID int64,
	sessionID string,
	moderationStatus int16,
) {
	t.Helper()

	now := time.Now().UTC()
	if err := db.DB.Create(&model.Session{
		SessionID:        sessionID,
		OwnerID:          ownerID,
		SessionType:      model.SessionTypeGroup,
		GroupName:        "agent-api-group",
		ModerationStatus: moderationStatus,
	}).Error; err != nil {
		t.Fatalf("seed session error: %v", err)
	}
	if err := db.DB.Create(&model.SessionMember{
		SessionID:    sessionID,
		MemberID:     ownerID,
		MemberType:   1,
		Role:         3,
		JoinedAt:     now,
		LastActiveAt: now,
	}).Error; err != nil {
		t.Fatalf("seed session member error: %v", err)
	}
}

func decodeAPIErrorCode(t *testing.T, body []byte) int {
	t.Helper()

	var resp struct {
		Code int `json:"code"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("decode response error: %v", err)
	}
	return resp.Code
}

func TestAgentMessageHistoryBannedGroup(t *testing.T) {
	r, testDB, cleanup := setupAgentMessageHandlerTest(t)
	defer cleanup()

	const (
		ownerID   = int64(21011)
		agentID   = int64(31011)
		apiKey    = "ak_test_agent_message_history_banned"
		sessionID = "agent-history-banned-group"
	)
	seedAgentAPIAuthData(t, testDB, ownerID, agentID, apiKey)
	seedAgentMessageGroup(
		t,
		testDB,
		ownerID,
		sessionID,
		model.SessionModerationStatusBanned,
	)

	req, _ := http.NewRequest(
		http.MethodGet,
		"/agent-api/messages/history?session_id="+sessionID,
		nil,
	)
	req.Header.Set("Authorization", "Bearer "+apiKey)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected status 403, got %d, body=%s", w.Code, w.Body.String())
	}
	if code := decodeAPIErrorCode(t, w.Body.Bytes()); code != 4003 {
		t.Fatalf("expected code 4003, got %d", code)
	}
}

func TestAgentMessageHistoryMissingSession(t *testing.T) {
	r, testDB, cleanup := setupAgentMessageHandlerTest(t)
	defer cleanup()

	const (
		ownerID = int64(21012)
		agentID = int64(31012)
		apiKey  = "ak_test_agent_message_history_missing"
	)
	seedAgentAPIAuthData(t, testDB, ownerID, agentID, apiKey)

	req, _ := http.NewRequest(
		http.MethodGet,
		"/agent-api/messages/history?session_id=missing-session",
		nil,
	)
	req.Header.Set("Authorization", "Bearer "+apiKey)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected status 404, got %d, body=%s", w.Code, w.Body.String())
	}
	if code := decodeAPIErrorCode(t, w.Body.Bytes()); code != 4004 {
		t.Fatalf("expected code 4004, got %d", code)
	}
}

func TestAgentMessageHistoryDefaultsToLatestCleanMessage(t *testing.T) {
	r, testDB, cleanup := setupAgentMessageHandlerTest(t)
	defer cleanup()

	const (
		ownerID   = int64(21018)
		agentID   = int64(31018)
		apiKey    = "ak_test_agent_message_history_clean"
		sessionID = "agent-history-clean-group"
	)
	seedAgentAPIAuthData(t, testDB, ownerID, agentID, apiKey)
	seedAgentMessageGroup(t, testDB, ownerID, sessionID, model.SessionModerationStatusActive)

	now := time.Now().UTC().Add(time.Second)
	messages := []model.Message{
		{
			MsgID: 201, SessionID: sessionID, SenderID: agentID, SenderType: 2,
			MsgType: model.MsgTypeText, Content: "任务最终结果", CreatedAt: now,
		},
		{
			MsgID: 202, SessionID: sessionID, SenderID: agentID, SenderType: 2,
			MsgType: model.MsgTypeText,
			Content: "[Tool](grix://card/tool_execution?d=%7B%7D)",
			Extra:   datatypes.JSON(`{"biz_card":{"type":"tool_execution"}}`), CreatedAt: now.Add(time.Second),
		},
	}
	if err := testDB.DB.Create(&messages).Error; err != nil {
		t.Fatalf("seed clean history messages error: %v", err)
	}

	req, _ := http.NewRequest(
		http.MethodGet,
		"/agent-api/messages/history?session_id="+sessionID,
		nil,
	)
	req.Header.Set("Authorization", "Bearer "+apiKey)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d, body=%s", w.Code, w.Body.String())
	}
	var resp struct {
		Code int `json:"code"`
		Data struct {
			Messages []model.Message `json:"messages"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response error: %v", err)
	}
	if resp.Code != 0 {
		t.Fatalf("code=%d want=0 body=%s", resp.Code, w.Body.String())
	}
	if len(resp.Data.Messages) != 1 || resp.Data.Messages[0].MsgID != 201 {
		t.Fatalf("messages=%#v want only msg 201", resp.Data.Messages)
	}
}

func TestAgentMessageSearchSuccess(t *testing.T) {
	r, testDB, cleanup := setupAgentMessageHandlerTest(t)
	defer cleanup()

	const (
		ownerID   = int64(21016)
		agentID   = int64(31016)
		apiKey    = "ak_test_agent_message_search_success"
		sessionID = "agent-search-success-group"
	)
	seedAgentAPIAuthData(t, testDB, ownerID, agentID, apiKey)
	seedAgentMessageGroup(t, testDB, ownerID, sessionID, model.SessionModerationStatusActive)

	now := time.Now().UTC()
	// 群聊历史/搜索会过滤 created_at < joined_at；消息时间必须落在入群之后。
	joinedAt := now.Add(-3 * time.Hour)
	if err := testDB.DB.Model(&model.SessionMember{}).
		Where("session_id = ? AND member_id = ?", sessionID, ownerID).
		Update("joined_at", joinedAt).Error; err != nil {
		t.Fatalf("update joined_at error: %v", err)
	}
	for _, msg := range []model.Message{
		{
			MsgID:      101,
			SessionID:  sessionID,
			SenderID:   ownerID,
			SenderType: 1,
			MsgType:    1,
			Content:    "部署完成，等待验证",
			CreatedAt:  now.Add(-2 * time.Hour),
		},
		{
			MsgID:      102,
			SessionID:  sessionID,
			SenderID:   ownerID,
			SenderType: 1,
			MsgType:    1,
			Content:    "今天先讨论日志方案",
			CreatedAt:  now.Add(-1 * time.Hour),
		},
		{
			MsgID:      103,
			SessionID:  sessionID,
			SenderID:   ownerID,
			SenderType: 1,
			MsgType:    1,
			Content:    "部署日志已经补齐",
			CreatedAt:  now.Add(-30 * time.Minute),
		},
	} {
		if err := testDB.DB.Create(&msg).Error; err != nil {
			t.Fatalf("seed message error: %v", err)
		}
	}

	req, _ := http.NewRequest(
		http.MethodGet,
		"/agent-api/messages/search?session_id="+sessionID+"&keyword=%E6%97%A5%E5%BF%97&limit=10",
		nil,
	)
	req.Header.Set("Authorization", "Bearer "+apiKey)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d, body=%s", w.Code, w.Body.String())
	}

	var resp struct {
		Code int `json:"code"`
		Data struct {
			HasMore  bool            `json:"has_more"`
			Messages []model.Message `json:"messages"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response error: %v", err)
	}
	if resp.Code != 0 {
		t.Fatalf("code=%d want=0 body=%s", resp.Code, w.Body.String())
	}
	if resp.Data.HasMore {
		t.Fatalf("has_more=%t want=false", resp.Data.HasMore)
	}
	if len(resp.Data.Messages) != 2 {
		t.Fatalf("messages=%d want=2 body=%s", len(resp.Data.Messages), w.Body.String())
	}
	if resp.Data.Messages[0].MsgID != 103 || resp.Data.Messages[1].MsgID != 102 {
		t.Fatalf(
			"search order=%v want=[103 102]",
			[]int64{resp.Data.Messages[0].MsgID, resp.Data.Messages[1].MsgID},
		)
	}
}

func TestAgentMessageSearchMissingKeyword(t *testing.T) {
	r, testDB, cleanup := setupAgentMessageHandlerTest(t)
	defer cleanup()

	const (
		ownerID   = int64(21017)
		agentID   = int64(31017)
		apiKey    = "ak_test_agent_message_search_missing_keyword"
		sessionID = "agent-search-missing-keyword"
	)
	seedAgentAPIAuthData(t, testDB, ownerID, agentID, apiKey)
	seedAgentMessageGroup(t, testDB, ownerID, sessionID, model.SessionModerationStatusActive)

	req, _ := http.NewRequest(
		http.MethodGet,
		"/agent-api/messages/search?session_id="+sessionID,
		nil,
	)
	req.Header.Set("Authorization", "Bearer "+apiKey)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d, body=%s", w.Code, w.Body.String())
	}
	if code := decodeAPIErrorCode(t, w.Body.Bytes()); code != 10003 {
		t.Fatalf("expected code 10003, got %d", code)
	}
}

func TestAgentMessageDeleteBannedGroup(t *testing.T) {
	r, testDB, cleanup := setupAgentMessageHandlerTest(t)
	defer cleanup()

	const (
		ownerID   = int64(21013)
		agentID   = int64(31013)
		apiKey    = "ak_test_agent_message_delete_banned"
		sessionID = "agent-delete-banned-group"
	)
	seedAgentAPIAuthData(t, testDB, ownerID, agentID, apiKey)
	seedAgentMessageGroup(
		t,
		testDB,
		ownerID,
		sessionID,
		model.SessionModerationStatusBanned,
	)

	body := []byte(`{"session_id":"agent-delete-banned-group","msg_id":"1"}`)
	req, _ := http.NewRequest(
		http.MethodPost,
		"/agent-api/messages/delete",
		bytes.NewReader(body),
	)
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected status 403, got %d, body=%s", w.Code, w.Body.String())
	}
	if code := decodeAPIErrorCode(t, w.Body.Bytes()); code != 4003 {
		t.Fatalf("expected code 4003, got %d", code)
	}
}

func TestAgentMessageEditBannedGroup(t *testing.T) {
	r, testDB, cleanup := setupAgentMessageHandlerTest(t)
	defer cleanup()

	const (
		ownerID   = int64(21015)
		agentID   = int64(31015)
		apiKey    = "ak_test_agent_message_edit_banned"
		sessionID = "agent-edit-banned-group"
	)
	seedAgentAPIAuthData(t, testDB, ownerID, agentID, apiKey)
	seedAgentMessageGroup(
		t,
		testDB,
		ownerID,
		sessionID,
		model.SessionModerationStatusBanned,
	)

	body := []byte(`{"session_id":"agent-edit-banned-group","msg_id":"1","content":"updated"}`)
	req, _ := http.NewRequest(
		http.MethodPost,
		"/agent-api/messages/edit",
		bytes.NewReader(body),
	)
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected status 403, got %d, body=%s", w.Code, w.Body.String())
	}
	if code := decodeAPIErrorCode(t, w.Body.Bytes()); code != 4003 {
		t.Fatalf("expected code 4003, got %d", code)
	}
}

func TestAgentOSSPresignBannedGroup(t *testing.T) {
	r, testDB, cleanup := setupAgentMessageHandlerTest(t)
	defer cleanup()

	const (
		ownerID   = int64(21014)
		agentID   = int64(31014)
		apiKey    = "ak_test_agent_oss_banned"
		sessionID = "agent-oss-banned-group"
	)
	seedAgentAPIAuthData(t, testDB, ownerID, agentID, apiKey)
	seedAgentMessageGroup(
		t,
		testDB,
		ownerID,
		sessionID,
		model.SessionModerationStatusBanned,
	)

	body := []byte(`{"session_id":"agent-oss-banned-group","filename":"a.png","content_type":"image/png"}`)
	req, _ := http.NewRequest(
		http.MethodPost,
		"/agent-api/oss/presign",
		bytes.NewReader(body),
	)
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected status 403, got %d, body=%s", w.Code, w.Body.String())
	}
	if code := decodeAPIErrorCode(t, w.Body.Bytes()); code != 4003 {
		t.Fatalf("expected code 4003, got %d", code)
	}
}
