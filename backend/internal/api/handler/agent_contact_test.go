package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/agentscope"
	"github.com/askie/grix/backend/internal/pkg/middleware"
	"github.com/askie/grix/backend/internal/pkg/testutil"
	"github.com/askie/grix/backend/internal/store"
	"github.com/gin-gonic/gin"
)

type agentContactSearchResp struct {
	Code int    `json:"code"`
	Msg  string `json:"msg"`
	Data struct {
		HasMore bool `json:"has_more"`
		List    []struct {
			PeerID       string `json:"peer_id"`
			PeerType     int16  `json:"peer_type"`
			DisplayName  string `json:"display_name"`
			Introduction string `json:"introduction"`
			Username     string `json:"username"`
			RemarkName   string `json:"remark_name"`
			AvatarURL    string `json:"avatar_url"`
		} `json:"list"`
	} `json:"data"`
}

func setupAgentContactHandlerTest(t *testing.T) (*gin.Engine, *testutil.TestDB, func()) {
	t.Helper()

	testDB := testutil.NewTestDB()
	store.DB = testDB.DB
	store.RDB = testutil.NewMockRedis()

	r := gin.New()
	r.Use(middleware.AgentAPIAuth())
	r.GET("/agent-api/contacts/search", middleware.AgentAPIScope(agentscope.ScopeContactSearch), AgentContactSearch)

	return r, testDB, func() { testDB.Close() }
}

func TestAgentContactSearch(t *testing.T) {
	r, testDB, cleanup := setupAgentContactHandlerTest(t)
	defer cleanup()

	const (
		ownerID     = int64(21041)
		agentID     = int64(31041)
		apiKey      = "ak_test_contact_search_key"
		friendID    = int64(99941)
		ownerBotID  = int64(99942)
		disabledBot = int64(99943)
	)
	seedAgentAPIAuthData(t, testDB, ownerID, agentID, apiKey)

	friend := model.User{
		ID:           friendID,
		Username:     "atlas_user",
		Email:        "atlas_user@example.com",
		PasswordHash: "x",
		AuthProvider: "local",
		Nickname:     "Atlas Nickname",
		Introduction: "Atlas friend intro",
		AvatarURL:    "https://example.com/avatar.png",
	}
	if err := testDB.DB.Create(&friend).Error; err != nil {
		t.Fatalf("seed friend error: %v", err)
	}
	if err := testDB.DB.Create(&model.Friend{
		ID:         ownerID + friendID,
		UserID:     ownerID,
		FriendID:   friendID,
		RemarkName: "Atlas Remark",
		CreatedAt:  time.Now().Add(-time.Minute),
	}).Error; err != nil {
		t.Fatalf("seed friend relation error: %v", err)
	}
	if err := testDB.DB.Create(&model.Agent{
		ID:           ownerBotID,
		OwnerID:      ownerID,
		AgentName:    "Atlas Assistant",
		Introduction: "Atlas agent intro",
		AvatarURL:    "https://example.com/agent.png",
		ProviderType: model.AgentProviderRemote,
		Status:       model.AgentStatusActive,
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}).Error; err != nil {
		t.Fatalf("seed owner agent error: %v", err)
	}
	if err := testDB.DB.Create(&model.Agent{
		ID:           disabledBot,
		OwnerID:      ownerID,
		AgentName:    "Atlas Disabled",
		ProviderType: model.AgentProviderRemote,
		Status:       model.AgentStatusDisabled,
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}).Error; err != nil {
		t.Fatalf("seed disabled owner agent error: %v", err)
	}

	t.Run("requires scope", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodGet, "/agent-api/contacts/search?id=99941", nil)
		req.Header.Set("Authorization", "Bearer "+apiKey)

		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusForbidden {
			t.Fatalf("expected status 403, got %d, body: %s", w.Code, w.Body.String())
		}
	})

	t.Run("searches owner contacts after scope granted", func(t *testing.T) {
		seedAgentSessionScope(t, testDB, agentID, agentscope.ScopeContactSearch)

		req, _ := http.NewRequest(http.MethodGet, "/agent-api/contacts/search?id=99941", nil)
		req.Header.Set("Authorization", "Bearer "+apiKey)

		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("expected status 200, got %d, body: %s", w.Code, w.Body.String())
		}

		var resp agentContactSearchResp
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("unmarshal response error: %v", err)
		}
		if resp.Code != 0 {
			t.Fatalf("expected code 0, got %d, msg: %s", resp.Code, resp.Msg)
		}
		if len(resp.Data.List) != 1 {
			t.Fatalf("expected 1 search result, got %d", len(resp.Data.List))
		}
		if resp.Data.List[0].PeerID != "99941" || resp.Data.List[0].PeerType != 1 {
			t.Fatalf("expected result to be friend contact, got %#v", resp.Data.List[0])
		}
		if resp.Data.List[0].RemarkName != "Atlas Remark" {
			t.Fatalf("expected remark_name Atlas Remark, got %q", resp.Data.List[0].RemarkName)
		}
		if resp.Data.List[0].DisplayName != "Atlas Remark" {
			t.Fatalf("expected display_name to prefer remark, got %q", resp.Data.List[0].DisplayName)
		}
		if resp.Data.List[0].Introduction != "Atlas friend intro" {
			t.Fatalf("expected introduction Atlas friend intro, got %q", resp.Data.List[0].Introduction)
		}
	})

	t.Run("supports exact agent id lookup", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodGet, "/agent-api/contacts/search?id=99942", nil)
		req.Header.Set("Authorization", "Bearer "+apiKey)

		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("expected status 200, got %d, body: %s", w.Code, w.Body.String())
		}

		var resp agentContactSearchResp
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("unmarshal response error: %v", err)
		}
		if len(resp.Data.List) != 1 {
			t.Fatalf("expected 1 agent result, got %d", len(resp.Data.List))
		}
		if resp.Data.List[0].PeerID != "99942" || resp.Data.List[0].PeerType != 2 {
			t.Fatalf("expected active owner agent result, got %#v", resp.Data.List[0])
		}
		if resp.Data.List[0].DisplayName != "Atlas Assistant" {
			t.Fatalf("expected display_name Atlas Assistant, got %q", resp.Data.List[0].DisplayName)
		}
		if resp.Data.List[0].Introduction != "Atlas agent intro" {
			t.Fatalf("expected introduction Atlas agent intro, got %q", resp.Data.List[0].Introduction)
		}
	})

	t.Run("supports keyword fuzzy lookup", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodGet, "/agent-api/contacts/search?keyword=atlasuser", nil)
		req.Header.Set("Authorization", "Bearer "+apiKey)

		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("expected status 200, got %d, body: %s", w.Code, w.Body.String())
		}

		var resp agentContactSearchResp
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("unmarshal response error: %v", err)
		}
		if len(resp.Data.List) != 1 {
			t.Fatalf("expected 1 keyword result, got %d", len(resp.Data.List))
		}
		if resp.Data.List[0].PeerID != "99941" || resp.Data.List[0].PeerType != 1 {
			t.Fatalf("expected compact username match to return friend, got %#v", resp.Data.List[0])
		}
	})
}
