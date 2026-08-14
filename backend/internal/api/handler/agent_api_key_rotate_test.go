package handler

import (
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

func setupAgentAPIKeyRotateHandlerTest(t *testing.T) (*gin.Engine, *testutil.TestDB, func()) {
	t.Helper()

	testDB := testutil.NewTestDB()
	store.DB = testDB.DB
	store.RDB = testutil.NewMockRedis()
	_ = snowflake.Init(1)

	r := gin.New()
	r.Use(middleware.AgentAPIAuth())
	r.Use(middleware.AgentAPIScope(agentscope.ScopeAgentAPICreate))
	r.POST("/agent-api/agents/:id/api/key/rotate", AgentAPIAgentRotateAPIKey)

	return r, testDB, func() { testDB.Close() }
}

func seedKeyRotateAuthData(t *testing.T, db *testutil.TestDB, ownerID, actorAgentID int64) (string, string) {
	t.Helper()

	owner := model.User{ID: ownerID, Username: "rotate-owner-" + strconv.FormatInt(ownerID, 10), Nickname: "owner"}
	if err := db.DB.Create(&owner).Error; err != nil {
		t.Fatalf("create owner: %v", err)
	}

	plain, hash, _, err := agentapi.GenerateAPIKey(actorAgentID)
	if err != nil {
		t.Fatalf("generate api key: %v", err)
	}

	actorAgent := model.Agent{
		ID:           actorAgentID,
		OwnerID:      ownerID,
		ProviderType: model.AgentProviderAPI,
		Status:       model.AgentStatusActive,
		APIKeyHash:   hash,
	}
	if err := db.DB.Create(&actorAgent).Error; err != nil {
		t.Fatalf("create actor agent: %v", err)
	}

	scope := model.AgentAPIScope{
		AgentID: actorAgentID,
		Scope:   agentscope.ScopeAgentAPICreate,
	}
	if err := db.DB.Create(&scope).Error; err != nil {
		t.Fatalf("create scope: %v", err)
	}

	return plain, hash
}

func TestAgentAPIAgentRotateAPIKey_Success(t *testing.T) {
	r, testDB, cleanup := setupAgentAPIKeyRotateHandlerTest(t)
	defer cleanup()

	ownerID := int64(50001)
	actorAgentID := int64(60001)
	targetAgentID := int64(60002)

	plain, _ := seedKeyRotateAuthData(t, testDB, ownerID, actorAgentID)

	_, origHash, origHint, err := agentapi.GenerateAPIKey(targetAgentID)
	if err != nil {
		t.Fatalf("generate orig key: %v", err)
	}
	targetAgent := model.Agent{
		ID:              targetAgentID,
		OwnerID:         ownerID,
		ProviderType:    model.AgentProviderAPI,
		Status:          model.AgentStatusActive,
		AgentClientType: "hermes",
		AgentName:       "test-sub-agent",
		APIKeyHash:      origHash,
		APIKeyHint:      origHint,
	}
	if err := testDB.DB.Create(&targetAgent).Error; err != nil {
		t.Fatalf("create target agent: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/agent-api/agents/"+strconv.FormatInt(targetAgentID, 10)+"/api/key/rotate", nil)
	req.Header.Set("Authorization", "Bearer "+plain)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}

	var resp struct {
		Data struct {
			APIKey     string `json:"api_key"`
			APIKeyHint string `json:"api_key_hint"`
			ID         int64  `json:"id,string"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Data.APIKey == "" {
		t.Fatal("api_key should not be empty after rotation")
	}
	if resp.Data.APIKeyHint == "" {
		t.Fatal("api_key_hint should not be empty")
	}
	if resp.Data.APIKeyHint == origHint {
		t.Fatal("api_key_hint should differ after rotation")
	}
	if resp.Data.ID != targetAgentID {
		t.Fatalf("id=%d want=%d", resp.Data.ID, targetAgentID)
	}
}

func TestAgentAPIAgentRotateAPIKey_WrongOwner(t *testing.T) {
	r, testDB, cleanup := setupAgentAPIKeyRotateHandlerTest(t)
	defer cleanup()

	ownerID := int64(50002)
	otherOwnerID := int64(50003)
	actorAgentID := int64(60003)
	targetAgentID := int64(60004)

	plain, _ := seedKeyRotateAuthData(t, testDB, ownerID, actorAgentID)

	otherOwner := model.User{ID: otherOwnerID, Username: "other-owner", Email: "other-" + strconv.FormatInt(otherOwnerID, 10) + "@test", Nickname: "other"}
	if err := testDB.DB.Create(&otherOwner).Error; err != nil {
		t.Fatalf("create other owner: %v", err)
	}
	_, origHash, origHint, _ := agentapi.GenerateAPIKey(targetAgentID)
	targetAgent := model.Agent{
		ID:           targetAgentID,
		OwnerID:      otherOwnerID,
		ProviderType: model.AgentProviderAPI,
		Status:       model.AgentStatusActive,
		APIKeyHash:   origHash,
		APIKeyHint:   origHint,
	}
	if err := testDB.DB.Create(&targetAgent).Error; err != nil {
		t.Fatalf("create target agent: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/agent-api/agents/"+strconv.FormatInt(targetAgentID, 10)+"/api/key/rotate", nil)
	req.Header.Set("Authorization", "Bearer "+plain)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("status=%d want=403 body=%s", w.Code, w.Body.String())
	}
}

func TestAgentAPIAgentRotateAPIKey_NonAPIAgent(t *testing.T) {
	r, testDB, cleanup := setupAgentAPIKeyRotateHandlerTest(t)
	defer cleanup()

	ownerID := int64(50004)
	actorAgentID := int64(60005)
	targetAgentID := int64(60006)

	plain, _ := seedKeyRotateAuthData(t, testDB, ownerID, actorAgentID)

	targetAgent := model.Agent{
		ID:           targetAgentID,
		OwnerID:      ownerID,
		ProviderType: model.AgentProviderLocal,
		Status:       model.AgentStatusActive,
	}
	if err := testDB.DB.Create(&targetAgent).Error; err != nil {
		t.Fatalf("create target agent: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/agent-api/agents/"+strconv.FormatInt(targetAgentID, 10)+"/api/key/rotate", nil)
	req.Header.Set("Authorization", "Bearer "+plain)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status=%d want=400 body=%s", w.Code, w.Body.String())
	}
}
