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
	"github.com/askie/grix/backend/internal/pkg/testutil"
	"github.com/askie/grix/backend/internal/store"
	"github.com/gin-gonic/gin"
)

type agentAPICategoryResp struct {
	Code int    `json:"code"`
	Msg  string `json:"msg"`
	Data struct {
		ID       string `json:"id"`
		ParentID string `json:"parent_id"`
		Name     string `json:"name"`
	} `json:"data"`
}

type agentAPICategoryListResp struct {
	Code int    `json:"code"`
	Msg  string `json:"msg"`
	Data struct {
		List []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"list"`
	} `json:"data"`
}

type agentAPIAgentAssignResp struct {
	Code int    `json:"code"`
	Msg  string `json:"msg"`
	Data struct {
		ID         string `json:"id"`
		CategoryID string `json:"category_id"`
	} `json:"data"`
}

func setupAgentAPICategoryHandlerTest(t *testing.T) (*gin.Engine, *testutil.TestDB, func()) {
	t.Helper()

	testDB := testutil.NewTestDB()
	store.DB = testDB.DB
	store.RDB = testutil.NewMockRedis()

	r := gin.New()
	api := r.Group("/agent-api")
	api.Use(middleware.AgentAPIAuth())
	api.GET(
		"/agents/categories/list",
		middleware.AgentAPIScope(agentscope.ScopeAgentCategoryList),
		AgentAPICategoryList,
	)
	api.POST(
		"/agents/categories/create",
		middleware.AgentAPIScope(agentscope.ScopeAgentCategoryCreate),
		AgentAPICategoryCreate,
	)
	api.PUT(
		"/agents/categories/:id",
		middleware.AgentAPIScope(agentscope.ScopeAgentCategoryUpdate),
		AgentAPICategoryUpdate,
	)
	api.PUT(
		"/agents/:id/category",
		middleware.AgentAPIScope(agentscope.ScopeAgentCategoryAssign),
		AgentAPIAgentAssignCategory,
	)

	return r, testDB, func() { testDB.Close() }
}

func seedAgentAPICategoryAuthData(
	t *testing.T,
	db *testutil.TestDB,
	ownerID, agentID int64,
	apiKey string,
) {
	t.Helper()

	owner := model.User{
		ID:           ownerID,
		Username:     "agent_category_owner_" + strconv.FormatInt(ownerID, 10),
		Email:        "agent_category_owner_" + strconv.FormatInt(ownerID, 10) + "@example.com",
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
		AgentName:    "agent_category_actor_" + strconv.FormatInt(agentID, 10),
		OwnerID:      ownerID,
		ProviderType: model.AgentProviderAPI,
		Status:       model.AgentStatusActive,
		APIKeyHash:   agentapi.HashAPIKey(apiKey),
		APIKeyHint:   agentapi.APIKeyHint(apiKey),
	}
	if err := db.DB.Create(&agent).Error; err != nil {
		t.Fatalf("seed actor agent error: %v", err)
	}
}

func seedAgentAPICategoryScopes(t *testing.T, agentID int64, scopes ...string) {
	t.Helper()

	rows := make([]model.AgentAPIScope, 0, len(scopes))
	for _, scope := range scopes {
		rows = append(rows, model.AgentAPIScope{
			AgentID: agentID,
			Scope:   scope,
		})
	}
	if err := store.DB.Create(&rows).Error; err != nil {
		t.Fatalf("seed scopes error: %v", err)
	}
}

func TestAgentAPICategoryHandlers(t *testing.T) {
	const (
		ownerID      = int64(29101)
		actorAgentID = int64(39101)
		targetAgent  = int64(39102)
		apiKey       = "ak_test_agent_api_category_scope_key"
	)

	r, testDB, cleanup := setupAgentAPICategoryHandlerTest(t)
	defer cleanup()

	seedAgentAPICategoryAuthData(t, testDB, ownerID, actorAgentID, apiKey)
	seedAgentAPICategoryScopes(
		t,
		actorAgentID,
		agentscope.ScopeAgentCategoryList,
		agentscope.ScopeAgentCategoryCreate,
		agentscope.ScopeAgentCategoryUpdate,
		agentscope.ScopeAgentCategoryAssign,
	)
	if err := store.DB.Create(&model.Agent{
		ID:           targetAgent,
		AgentName:    "target_categorized_agent",
		OwnerID:      ownerID,
		ProviderType: model.AgentProviderRemote,
		Status:       model.AgentStatusActive,
	}).Error; err != nil {
		t.Fatalf("seed target agent error: %v", err)
	}

	createReq, _ := http.NewRequest(
		http.MethodPost,
		"/agent-api/agents/categories/create",
		bytes.NewReader([]byte(`{"name":"Workspace","parent_id":"0"}`)),
	)
	createReq.Header.Set("Authorization", "Bearer "+apiKey)
	createReq.Header.Set("Content-Type", "application/json")
	createW := httptest.NewRecorder()
	r.ServeHTTP(createW, createReq)
	if createW.Code != http.StatusOK {
		t.Fatalf("expected create status 200, got %d, body: %s", createW.Code, createW.Body.String())
	}

	var createResp agentAPICategoryResp
	if err := json.Unmarshal(createW.Body.Bytes(), &createResp); err != nil {
		t.Fatalf("unmarshal create resp error: %v", err)
	}
	if createResp.Code != 0 || createResp.Data.Name != "Workspace" {
		t.Fatalf("unexpected create response: %#v", createResp)
	}

	listReq, _ := http.NewRequest(http.MethodGet, "/agent-api/agents/categories/list", nil)
	listReq.Header.Set("Authorization", "Bearer "+apiKey)
	listW := httptest.NewRecorder()
	r.ServeHTTP(listW, listReq)
	if listW.Code != http.StatusOK {
		t.Fatalf("expected list status 200, got %d, body: %s", listW.Code, listW.Body.String())
	}

	var listResp agentAPICategoryListResp
	if err := json.Unmarshal(listW.Body.Bytes(), &listResp); err != nil {
		t.Fatalf("unmarshal list resp error: %v", err)
	}
	if len(listResp.Data.List) != 1 || listResp.Data.List[0].Name != "Workspace" {
		t.Fatalf("unexpected list response: %#v", listResp)
	}

	updateReq, _ := http.NewRequest(
		http.MethodPut,
		"/agent-api/agents/categories/"+createResp.Data.ID,
		bytes.NewReader([]byte(`{"name":"Workspace Updated","parent_id":"0"}`)),
	)
	updateReq.Header.Set("Authorization", "Bearer "+apiKey)
	updateReq.Header.Set("Content-Type", "application/json")
	updateW := httptest.NewRecorder()
	r.ServeHTTP(updateW, updateReq)
	if updateW.Code != http.StatusOK {
		t.Fatalf("expected update status 200, got %d, body: %s", updateW.Code, updateW.Body.String())
	}

	var updateResp agentAPICategoryResp
	if err := json.Unmarshal(updateW.Body.Bytes(), &updateResp); err != nil {
		t.Fatalf("unmarshal update resp error: %v", err)
	}
	if updateResp.Code != 0 || updateResp.Data.Name != "Workspace Updated" {
		t.Fatalf("unexpected update response: %#v", updateResp)
	}

	assignReq, _ := http.NewRequest(
		http.MethodPut,
		"/agent-api/agents/"+strconv.FormatInt(targetAgent, 10)+"/category",
		bytes.NewReader([]byte(`{"category_id":"`+createResp.Data.ID+`"}`)),
	)
	assignReq.Header.Set("Authorization", "Bearer "+apiKey)
	assignReq.Header.Set("Content-Type", "application/json")
	assignW := httptest.NewRecorder()
	r.ServeHTTP(assignW, assignReq)
	if assignW.Code != http.StatusOK {
		t.Fatalf("expected assign status 200, got %d, body: %s", assignW.Code, assignW.Body.String())
	}

	var assignResp agentAPIAgentAssignResp
	if err := json.Unmarshal(assignW.Body.Bytes(), &assignResp); err != nil {
		t.Fatalf("unmarshal assign resp error: %v", err)
	}
	if assignResp.Code != 0 || assignResp.Data.CategoryID != createResp.Data.ID {
		t.Fatalf("unexpected assign response: %#v", assignResp)
	}

	var stored model.Agent
	if err := store.DB.First(&stored, targetAgent).Error; err != nil {
		t.Fatalf("query assigned agent error: %v", err)
	}
	if stored.CategoryID == 0 {
		t.Fatalf("expected persisted category assignment, got %#v", stored)
	}
}
