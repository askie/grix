package handler

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/logger"
	"github.com/askie/grix/backend/internal/pkg/snowflake"
	"github.com/askie/grix/backend/internal/pkg/testutil"
	"github.com/askie/grix/backend/internal/store"
	"github.com/gin-gonic/gin"
)

type userAgentMutationResp struct {
	Code int    `json:"code"`
	Msg  string `json:"msg"`
	Data struct {
		ID              string `json:"id"`
		AgentName       string `json:"agent_name"`
		ProviderType    int16  `json:"provider_type"`
		AgentClientType string `json:"agent_client_type"`
	} `json:"data"`
}

func setupUserAgentMutationHandlerTest(t *testing.T) (*gin.Engine, *testutil.TestDB, int64, func()) {
	t.Helper()

	logger.Init()
	testDB := testutil.NewTestDB()
	store.DB = testDB.DB
	store.RDB = testutil.NewMockRedis()
	if err := snowflake.Init(1); err != nil {
		t.Fatalf("init snowflake error: %v", err)
	}

	const userID = int64(73001)
	fixture := testutil.NewFixtureBuilder(testDB.DB)
	fixture.CreateUser(func(u *model.User) {
		u.ID = userID
		u.Username = "agent_mutation_owner"
		u.Email = "agent_mutation_owner@example.com"
	})

	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("user_id", userID)
		c.Next()
	})
	r.POST("/agents", AgentCreate)
	r.PUT("/agents/:id", AgentUpdate)

	return r, testDB, userID, func() { testDB.Close() }
}

func TestAgentCreate_IgnoresManualAgentClientType(t *testing.T) {
	r, _, _, cleanup := setupUserAgentMutationHandlerTest(t)
	defer cleanup()

	body := []byte(`{
		"agent_name":"generic_api_agent",
		"provider_type":3,
		"agent_client_type":"hermes"
	}`)
	req, _ := http.NewRequest(http.MethodPost, "/agents", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d, body: %s", w.Code, w.Body.String())
	}

	var resp userAgentMutationResp
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response error: %v", err)
	}
	if resp.Code != 0 {
		t.Fatalf("expected code 0, got %d, msg: %s", resp.Code, resp.Msg)
	}
	if resp.Data.ProviderType != model.AgentProviderAPI {
		t.Fatalf("expected provider_type=%d, got %d", model.AgentProviderAPI, resp.Data.ProviderType)
	}
	if resp.Data.AgentClientType != "" {
		t.Fatalf("expected response agent_client_type empty, got %q", resp.Data.AgentClientType)
	}

	createdID, err := strconv.ParseInt(resp.Data.ID, 10, 64)
	if err != nil {
		t.Fatalf("parse created id error: %v", err)
	}
	var created model.Agent
	if err := store.DB.First(&created, createdID).Error; err != nil {
		t.Fatalf("query created agent error: %v", err)
	}
	if created.AgentClientType != "" {
		t.Fatalf("expected stored agent_client_type empty, got %q", created.AgentClientType)
	}
}

func TestAgentUpdate_IgnoresManualAgentClientType(t *testing.T) {
	r, testDB, userID, cleanup := setupUserAgentMutationHandlerTest(t)
	defer cleanup()

	const agentID = int64(74001)
	if err := testDB.DB.Create(&model.Agent{
		ID:              agentID,
		AgentName:       "existing_api_agent",
		OwnerID:         userID,
		ProviderType:    model.AgentProviderAPI,
		AgentClientType: model.AgentClientTypeOpenClaw,
		Status:          model.AgentStatusActive,
		CreatedAt:       time.Now(),
		UpdatedAt:       time.Now(),
	}).Error; err != nil {
		t.Fatalf("seed agent error: %v", err)
	}

	body := []byte(`{
		"agent_name":"renamed_api_agent",
		"provider_type":3,
		"agent_client_type":"hermes"
	}`)
	req, _ := http.NewRequest(
		http.MethodPut,
		"/agents/"+strconv.FormatInt(agentID, 10),
		bytes.NewReader(body),
	)
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d, body: %s", w.Code, w.Body.String())
	}

	var resp userAgentMutationResp
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response error: %v", err)
	}
	if resp.Code != 0 {
		t.Fatalf("expected code 0, got %d, msg: %s", resp.Code, resp.Msg)
	}
	if resp.Data.AgentClientType != model.AgentClientTypeOpenClaw {
		t.Fatalf("expected response agent_client_type=%q, got %q", model.AgentClientTypeOpenClaw, resp.Data.AgentClientType)
	}

	var updated model.Agent
	if err := store.DB.First(&updated, agentID).Error; err != nil {
		t.Fatalf("query updated agent error: %v", err)
	}
	if updated.AgentName != "renamed_api_agent" {
		t.Fatalf("expected updated name %q, got %q", "renamed_api_agent", updated.AgentName)
	}
	if updated.AgentClientType != model.AgentClientTypeOpenClaw {
		t.Fatalf("expected stored agent_client_type=%q, got %q", model.AgentClientTypeOpenClaw, updated.AgentClientType)
	}
}
