package handler

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/agentapi"
	"github.com/askie/grix/backend/internal/pkg/agentscope"
	"github.com/askie/grix/backend/internal/pkg/middleware"
	"github.com/askie/grix/backend/internal/pkg/snowflake"
	"github.com/askie/grix/backend/internal/pkg/testutil"
	"github.com/askie/grix/backend/internal/store"
	"github.com/gin-gonic/gin"
)

func setupAgentAPIAgentCreateHandlerTest(t *testing.T) (*gin.Engine, *testutil.TestDB, func()) {
	t.Helper()

	testDB := testutil.NewTestDB()
	store.DB = testDB.DB
	store.RDB = testutil.NewMockRedis()
	_ = snowflake.Init(1)

	r := gin.New()
	r.Use(middleware.AgentAPIAuth())
	r.Use(middleware.AgentAPIScope(agentscope.ScopeAgentAPICreate))
	r.POST("/agent-api/agents/create", AgentAPIAgentCreate)

	return r, testDB, func() { testDB.Close() }
}

func seedAgentAPIAgentCreateAuthData(
	t *testing.T,
	db *testutil.TestDB,
	ownerID, agentID int64,
	apiKey string,
) {
	t.Helper()

	owner := model.User{
		ID:           ownerID,
		Username:     "agent_create_owner_" + strconv.FormatInt(ownerID, 10),
		Email:        "agent_create_owner_" + strconv.FormatInt(ownerID, 10) + "@example.com",
		PasswordHash: "x",
		AuthProvider: "local",
		Nickname:     "Owner",
		Status:       model.UserStatusActive,
	}
	if err := db.DB.Create(&owner).Error; err != nil {
		t.Fatalf("seed owner error: %v", err)
	}

	agent := model.Agent{
		ID:           agentID,
		AgentName:    "agent_create_caller_" + strconv.FormatInt(agentID, 10),
		OwnerID:      ownerID,
		ProviderType: model.AgentProviderAPI,
		Status:       model.AgentStatusActive,
		APIKeyHash:   agentapi.HashAPIKey(apiKey),
		APIKeyHint:   agentapi.APIKeyHint(apiKey),
	}
	if err := db.DB.Create(&agent).Error; err != nil {
		t.Fatalf("seed agent error: %v", err)
	}
}

func seedAgentAPIAgentCreateScope(t *testing.T, agentID int64) {
	t.Helper()
	if err := store.DB.Create(&model.AgentAPIScope{
		AgentID: agentID,
		Scope:   agentscope.ScopeAgentAPICreate,
	}).Error; err != nil {
		t.Fatalf("seed scope error: %v", err)
	}
}

type agentAPICreateResp struct {
	Code int    `json:"code"`
	Msg  string `json:"msg"`
	Data struct {
		ID              string `json:"id"`
		AgentName       string `json:"agent_name"`
		Introduction    string `json:"introduction"`
		SystemPrompt    string `json:"system_prompt"`
		OwnerID         string `json:"owner_id"`
		ProviderType    int16  `json:"provider_type"`
		AgentClientType string `json:"agent_client_type"`
		APIKey          string `json:"api_key"`
		APIEndpoint     string `json:"api_endpoint"`
	} `json:"data"`
}

type agentAPIFailResp struct {
	Code int    `json:"code"`
	Msg  string `json:"msg"`
}

func TestAgentAPIAgentCreate(t *testing.T) {
	const (
		ownerID = int64(21101)
		agentID = int64(31101)
		apiKey  = "ak_test_agent_api_create_scope_key"
	)

	t.Run("creates provider_type=3 agent when scope granted", func(t *testing.T) {
		r, testDB, cleanup := setupAgentAPIAgentCreateHandlerTest(t)
		defer cleanup()

		seedAgentAPIAgentCreateAuthData(t, testDB, ownerID, agentID, apiKey)
		seedAgentAPIAgentCreateScope(t, agentID)

		body := []byte(`{"agent_name":"child_api_agent","introduction":"child api introduction","system_prompt":"child business prompt","agent_client_type":"deepseek"}`)
		req, _ := http.NewRequest(http.MethodPost, "/agent-api/agents/create", bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+apiKey)
		req.Header.Set("Content-Type", "application/json")

		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("expected status 200, got %d, body: %s", w.Code, w.Body.String())
		}

		var resp agentAPICreateResp
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("unmarshal response error: %v", err)
		}
		if resp.Code != 0 {
			t.Fatalf("expected code 0, got %d, msg: %s", resp.Code, resp.Msg)
		}
		if resp.Data.ProviderType != model.AgentProviderAPI {
			t.Fatalf("expected provider_type=%d, got %d", model.AgentProviderAPI, resp.Data.ProviderType)
		}
		if resp.Data.APIKey == "" {
			t.Fatalf("expected api_key in response")
		}
		if resp.Data.APIEndpoint == "" {
			t.Fatalf("expected api_endpoint in response")
		}

		createdID, err := strconv.ParseInt(resp.Data.ID, 10, 64)
		if err != nil {
			t.Fatalf("parse created id error: %v", err)
		}
		var created model.Agent
		if err := store.DB.First(&created, createdID).Error; err != nil {
			t.Fatalf("query created agent error: %v", err)
		}
		if created.OwnerID != ownerID {
			t.Fatalf("expected owner_id=%d, got %d", ownerID, created.OwnerID)
		}
		if created.ProviderType != model.AgentProviderAPI {
			t.Fatalf("expected provider_type=%d, got %d", model.AgentProviderAPI, created.ProviderType)
		}
		if created.Introduction != "child api introduction" {
			t.Fatalf("expected introduction persisted, got %q", created.Introduction)
		}
		if resp.Data.SystemPrompt != "child business prompt" || created.SystemPrompt != "child business prompt" {
			t.Fatalf("system_prompt response=%q stored=%q", resp.Data.SystemPrompt, created.SystemPrompt)
		}
		if created.AgentClientType != model.AgentClientTypeDeepSeek {
			t.Fatalf("agent_client_type=%q", created.AgentClientType)
		}
		var scopeCount int64
		if err := store.DB.Model(&model.AgentAPIScope{}).
			Where("agent_id = ?", createdID).
			Count(&scopeCount).Error; err != nil {
			t.Fatalf("count scopes error: %v", err)
		}
		if scopeCount != 0 {
			t.Fatalf("expected no scopes by default, got %d", scopeCount)
		}

		var audit model.AuditLog
		if err := store.DB.Where("event_type = ?", "agent_api_agent_create").Order("id DESC").First(&audit).Error; err != nil {
			t.Fatalf("query audit log error: %v", err)
		}
		if audit.UserID == nil || *audit.UserID != ownerID {
			t.Fatalf("expected audit user_id=%d, got %#v", ownerID, audit.UserID)
		}
	})

	t.Run("grants all scopes when is_main is true", func(t *testing.T) {
		r, testDB, cleanup := setupAgentAPIAgentCreateHandlerTest(t)
		defer cleanup()

		seedAgentAPIAgentCreateAuthData(t, testDB, ownerID+10, agentID+10, apiKey+"_main")
		seedAgentAPIAgentCreateScope(t, agentID+10)

		body := []byte(`{"agent_name":"main_api_agent","is_main":true}`)
		req, _ := http.NewRequest(http.MethodPost, "/agent-api/agents/create", bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+apiKey+"_main")
		req.Header.Set("Content-Type", "application/json")

		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("expected status 200, got %d, body: %s", w.Code, w.Body.String())
		}

		var resp agentAPICreateResp
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("unmarshal response error: %v", err)
		}
		if resp.Code != 0 {
			t.Fatalf("expected code 0, got %d, msg: %s", resp.Code, resp.Msg)
		}

		createdID, err := strconv.ParseInt(resp.Data.ID, 10, 64)
		if err != nil {
			t.Fatalf("parse created id error: %v", err)
		}

		var rows []model.AgentAPIScope
		if err := store.DB.
			Where("agent_id = ?", createdID).
			Order("scope ASC").
			Find(&rows).Error; err != nil {
			t.Fatalf("query scopes error: %v", err)
		}
		allowed := agentscope.AllowedScopes()
		if len(rows) != len(allowed) {
			t.Fatalf("expected %d scopes, got %d", len(allowed), len(rows))
		}
		gotScopeSet := make(map[string]struct{}, len(rows))
		for _, row := range rows {
			gotScopeSet[row.Scope] = struct{}{}
		}
		for _, expectedScope := range allowed {
			if _, ok := gotScopeSet[expectedScope]; !ok {
				t.Fatalf("expected scope %q to be granted, got %#v", expectedScope, rows)
			}
		}
	})

	t.Run("persists requested hermes client type", func(t *testing.T) {
		r, testDB, cleanup := setupAgentAPIAgentCreateHandlerTest(t)
		defer cleanup()

		seedAgentAPIAgentCreateAuthData(t, testDB, ownerID+20, agentID+20, apiKey+"_hermes")
		seedAgentAPIAgentCreateScope(t, agentID+20)

		body := []byte(`{"agent_name":"hermes_api_agent","agent_client_type":"Hermes"}`)
		req, _ := http.NewRequest(http.MethodPost, "/agent-api/agents/create", bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+apiKey+"_hermes")
		req.Header.Set("Content-Type", "application/json")

		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("expected status 200, got %d, body: %s", w.Code, w.Body.String())
		}

		var resp agentAPICreateResp
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("unmarshal response error: %v", err)
		}
		if resp.Code != 0 {
			t.Fatalf("expected code 0, got %d, msg: %s", resp.Code, resp.Msg)
		}
		if resp.Data.AgentClientType != model.AgentClientTypeHermes {
			t.Fatalf("expected response agent_client_type=%q, got %q", model.AgentClientTypeHermes, resp.Data.AgentClientType)
		}

		createdID, err := strconv.ParseInt(resp.Data.ID, 10, 64)
		if err != nil {
			t.Fatalf("parse created id error: %v", err)
		}
		var created model.Agent
		if err := store.DB.First(&created, createdID).Error; err != nil {
			t.Fatalf("query created agent error: %v", err)
		}
		if created.AgentClientType != model.AgentClientTypeHermes {
			t.Fatalf("expected stored agent_client_type=%q, got %q", model.AgentClientTypeHermes, created.AgentClientType)
		}
	})

	t.Run("persists requested gemini client type", func(t *testing.T) {
		r, testDB, cleanup := setupAgentAPIAgentCreateHandlerTest(t)
		defer cleanup()

		seedAgentAPIAgentCreateAuthData(t, testDB, ownerID+21, agentID+21, apiKey+"_gemini")
		seedAgentAPIAgentCreateScope(t, agentID+21)

		body := []byte(`{"agent_name":"gemini_api_agent","agent_client_type":"Gemini"}`)
		req, _ := http.NewRequest(http.MethodPost, "/agent-api/agents/create", bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+apiKey+"_gemini")
		req.Header.Set("Content-Type", "application/json")

		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("expected status 200, got %d, body: %s", w.Code, w.Body.String())
		}

		var resp agentAPICreateResp
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("unmarshal response error: %v", err)
		}
		if resp.Code != 0 {
			t.Fatalf("expected code 0, got %d, msg: %s", resp.Code, resp.Msg)
		}
		if resp.Data.AgentClientType != model.AgentClientTypeGemini {
			t.Fatalf("expected response agent_client_type=%q, got %q", model.AgentClientTypeGemini, resp.Data.AgentClientType)
		}

		createdID, err := strconv.ParseInt(resp.Data.ID, 10, 64)
		if err != nil {
			t.Fatalf("parse created id error: %v", err)
		}
		var created model.Agent
		if err := store.DB.First(&created, createdID).Error; err != nil {
			t.Fatalf("query created agent error: %v", err)
		}
		if created.AgentClientType != model.AgentClientTypeGemini {
			t.Fatalf("expected stored agent_client_type=%q, got %q", model.AgentClientTypeGemini, created.AgentClientType)
		}
	})

	t.Run("rejects direct avatar url input", func(t *testing.T) {
		r, testDB, cleanup := setupAgentAPIAgentCreateHandlerTest(t)
		defer cleanup()

		seedAgentAPIAgentCreateAuthData(t, testDB, ownerID, agentID, apiKey)
		seedAgentAPIAgentCreateScope(t, agentID)

		body := []byte(`{"agent_name":"child_api_agent","avatar_url":"https://example.com/a.png"}`)
		req, _ := http.NewRequest(http.MethodPost, "/agent-api/agents/create", bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+apiKey)
		req.Header.Set("Content-Type", "application/json")

		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("expected status 400, got %d, body: %s", w.Code, w.Body.String())
		}

		var resp agentAPIFailResp
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("unmarshal response error: %v", err)
		}
		if resp.Code != 10003 {
			t.Fatalf("expected code 10003, got %d, msg: %s", resp.Code, resp.Msg)
		}
		if resp.Msg != "头像必须通过上传接口设置" {
			t.Fatalf("expected avatar validation msg, got %q", resp.Msg)
		}
	})

	t.Run("resolves owner by actor agent even if owner context is tampered", func(t *testing.T) {
		testDB := testutil.NewTestDB()
		defer testDB.Close()
		store.DB = testDB.DB
		store.RDB = testutil.NewMockRedis()
		_ = snowflake.Init(1)

		r := gin.New()
		r.Use(middleware.AgentAPIAuth())
		r.Use(func(c *gin.Context) {
			// Simulate a broken middleware overwriting owner_id with agent_id.
			c.Set("owner_id", middleware.GetAgentID(c))
			c.Next()
		})
		r.Use(middleware.AgentAPIScope(agentscope.ScopeAgentAPICreate))
		r.POST("/agent-api/agents/create", AgentAPIAgentCreate)

		realOwnerID := ownerID + 40
		actorAgentID := agentID + 40
		actorAPIKey := apiKey + "_tampered"
		seedAgentAPIAgentCreateAuthData(t, testDB, realOwnerID, actorAgentID, actorAPIKey)
		seedAgentAPIAgentCreateScope(t, actorAgentID)

		body := []byte(`{"agent_name":"child_agent_owner_resolved"}`)
		req, _ := http.NewRequest(http.MethodPost, "/agent-api/agents/create", bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+actorAPIKey)
		req.Header.Set("Content-Type", "application/json")

		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("expected status 200, got %d, body: %s", w.Code, w.Body.String())
		}

		var resp agentAPICreateResp
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("unmarshal response error: %v", err)
		}
		if resp.Code != 0 {
			t.Fatalf("expected code 0, got %d, msg: %s", resp.Code, resp.Msg)
		}

		createdID, err := strconv.ParseInt(resp.Data.ID, 10, 64)
		if err != nil {
			t.Fatalf("parse created id error: %v", err)
		}

		var created model.Agent
		if err := store.DB.First(&created, createdID).Error; err != nil {
			t.Fatalf("query created agent error: %v", err)
		}
		if created.OwnerID != realOwnerID {
			t.Fatalf("expected owner_id=%d, got %d", realOwnerID, created.OwnerID)
		}
	})

	t.Run("rejects when scope missing", func(t *testing.T) {
		r, testDB, cleanup := setupAgentAPIAgentCreateHandlerTest(t)
		defer cleanup()

		seedAgentAPIAgentCreateAuthData(t, testDB, ownerID+1, agentID+1, apiKey+"2")

		body := []byte(`{"agent_name":"child_api_agent_denied"}`)
		req, _ := http.NewRequest(http.MethodPost, "/agent-api/agents/create", bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+apiKey+"2")
		req.Header.Set("Content-Type", "application/json")

		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusForbidden {
			t.Fatalf("expected status 403, got %d, body: %s", w.Code, w.Body.String())
		}

		var resp agentAPIFailResp
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("unmarshal fail response error: %v", err)
		}
		if resp.Code != 20011 {
			t.Fatalf("expected biz code 20011, got %d", resp.Code)
		}
	})

	t.Run("rejects invalid payload", func(t *testing.T) {
		r, testDB, cleanup := setupAgentAPIAgentCreateHandlerTest(t)
		defer cleanup()

		seedAgentAPIAgentCreateAuthData(t, testDB, ownerID+2, agentID+2, apiKey+"3")
		seedAgentAPIAgentCreateScope(t, agentID+2)

		body := []byte(`{"agent_name":""}`)
		req, _ := http.NewRequest(http.MethodPost, "/agent-api/agents/create", bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+apiKey+"3")
		req.Header.Set("Content-Type", "application/json")

		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("expected status 400, got %d, body: %s", w.Code, w.Body.String())
		}

		var count int64
		if err := store.DB.Model(&model.AuditLog{}).
			Where("event_type = ? AND user_id = ?", "agent_api_agent_create_failed", ownerID+2).
			Count(&count).Error; err != nil {
			t.Fatalf("count audit logs error: %v", err)
		}
		if count == 0 {
			t.Fatalf("expected failed audit log")
		}
	})

	t.Run("rejects whitespace-only name", func(t *testing.T) {
		r, testDB, cleanup := setupAgentAPIAgentCreateHandlerTest(t)
		defer cleanup()

		seedAgentAPIAgentCreateAuthData(t, testDB, ownerID+3, agentID+3, apiKey+"4")
		seedAgentAPIAgentCreateScope(t, agentID+3)

		body := []byte(`{"agent_name":"   "}`)
		req, _ := http.NewRequest(http.MethodPost, "/agent-api/agents/create", bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+apiKey+"4")
		req.Header.Set("Content-Type", "application/json")

		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("expected status 400, got %d, body: %s", w.Code, w.Body.String())
		}
	})
}
