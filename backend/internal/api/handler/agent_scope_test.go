package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/testutil"
	"github.com/askie/grix/backend/internal/store"
	"github.com/gin-gonic/gin"
)

type agentScopeHandlerResp struct {
	Code int    `json:"code"`
	Msg  string `json:"msg"`
	Data struct {
		AvailableScopeItems []struct {
			Scope       string `json:"scope"`
			Label       string `json:"label"`
			Description string `json:"description"`
		} `json:"available_scope_items"`
	} `json:"data"`
}

func TestAgentScopeGetLocalizesByRequestHeader(t *testing.T) {
	testDB := testutil.NewTestDB()
	defer testDB.Close()
	store.DB = testDB.DB
	store.RDB = testutil.NewMockRedis()

	const (
		ownerID = int64(92001)
		agentID = int64(93001)
	)
	if err := store.DB.Create(&model.User{
		ID:           ownerID,
		Username:     "agent_scope_handler_owner_" + strconv.FormatInt(ownerID, 10),
		Email:        "agent_scope_handler_owner@example.com",
		PasswordHash: "x",
		AuthProvider: "local",
		Nickname:     "Owner",
		Status:       model.UserStatusActive,
	}).Error; err != nil {
		t.Fatalf("seed owner error: %v", err)
	}
	if err := store.DB.Create(&model.Agent{
		ID:           agentID,
		AgentName:    "scope_handler_agent",
		OwnerID:      ownerID,
		ProviderType: model.AgentProviderAPI,
		Status:       model.AgentStatusActive,
	}).Error; err != nil {
		t.Fatalf("seed agent error: %v", err)
	}

	r := gin.New()
	r.GET("/agents/:id/scopes", func(c *gin.Context) {
		c.Set("user_id", ownerID)
		AgentScopeGet(c)
	})

	t.Run("english", func(t *testing.T) {
		resp := requestAgentScope(t, r, agentID, "en-US")
		if len(resp.Data.AvailableScopeItems) == 0 {
			t.Fatal("expected available_scope_items")
		}
		if resp.Data.AvailableScopeItems[0].Label != "Create API Agent" {
			t.Fatalf("english label=%q want Create API Agent", resp.Data.AvailableScopeItems[0].Label)
		}
	})

	t.Run("chinese", func(t *testing.T) {
		resp := requestAgentScope(t, r, agentID, "zh-CN")
		if len(resp.Data.AvailableScopeItems) == 0 {
			t.Fatal("expected available_scope_items")
		}
		if resp.Data.AvailableScopeItems[0].Label != "创建 API Agent" {
			t.Fatalf("chinese label=%q want 创建 API Agent", resp.Data.AvailableScopeItems[0].Label)
		}
	})
}

func requestAgentScope(t *testing.T, r *gin.Engine, agentID int64, locale string) agentScopeHandlerResp {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/agents/"+strconv.FormatInt(agentID, 10)+"/scopes", nil)
	req.Header.Set("X-App-Locale", locale)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	var resp agentScopeHandlerResp
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v body=%s", err, w.Body.String())
	}
	if resp.Code != 0 {
		t.Fatalf("code=%d msg=%s", resp.Code, resp.Msg)
	}
	return resp
}
