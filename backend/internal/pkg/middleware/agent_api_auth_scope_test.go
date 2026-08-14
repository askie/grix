package middleware

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/agentapi"
	"github.com/askie/grix/backend/internal/pkg/agentscope"
	"github.com/askie/grix/backend/internal/pkg/testutil"
	"github.com/askie/grix/backend/internal/store"
	"github.com/gin-gonic/gin"
)

type middlewareFailResp struct {
	Code int    `json:"code"`
	Msg  string `json:"msg"`
}

func setupAgentAPIMiddlewareTest(t *testing.T) (*testutil.TestDB, func()) {
	t.Helper()

	testDB := testutil.NewTestDB()
	store.DB = testDB.DB
	store.RDB = testutil.NewMockRedis()
	return testDB, func() { testDB.Close() }
}

func seedMiddlewareOwnerAndAgent(t *testing.T, ownerID, agentID int64, providerType, status int16, apiKey string) {
	t.Helper()

	owner := model.User{
		ID:           ownerID,
		Username:     "agent_mw_owner_" + strconv.FormatInt(ownerID, 10),
		Email:        "agent_mw_owner_" + strconv.FormatInt(ownerID, 10) + "@example.com",
		PasswordHash: "x",
		AuthProvider: "local",
		Nickname:     "Owner",
		Status:       model.UserStatusActive,
	}
	if err := store.DB.Create(&owner).Error; err != nil {
		t.Fatalf("seed owner error: %v", err)
	}

	agent := model.Agent{
		ID:           agentID,
		AgentName:    "api_mw_agent_" + strconv.FormatInt(agentID, 10),
		OwnerID:      ownerID,
		ProviderType: providerType,
		Status:       status,
		APIKeyHash:   agentapi.HashAPIKey(apiKey),
		APIKeyHint:   agentapi.APIKeyHint(apiKey),
	}
	if err := store.DB.Create(&agent).Error; err != nil {
		t.Fatalf("seed agent error: %v", err)
	}
}

func requestWithBearer(r http.Handler, method, path, apiKey string) *httptest.ResponseRecorder {
	req, _ := http.NewRequest(method, path, nil)
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestAgentAPIAuthRejectsNonActiveAgent(t *testing.T) {
	tests := []struct {
		name         string
		providerType int16
		status       int16
		wantStatus   int
	}{
		{
			name:         "active api provider allowed",
			providerType: model.AgentProviderAPI,
			status:       model.AgentStatusActive,
			wantStatus:   http.StatusOK,
		},
		{
			name:         "disabled agent rejected",
			providerType: model.AgentProviderAPI,
			status:       model.AgentStatusDisabled,
			wantStatus:   http.StatusForbidden,
		},
		{
			name:         "deleted agent rejected",
			providerType: model.AgentProviderAPI,
			status:       model.AgentStatusDeleted,
			wantStatus:   http.StatusForbidden,
		},
		{
			name:         "non api provider rejected",
			providerType: model.AgentProviderRemote,
			status:       model.AgentStatusActive,
			wantStatus:   http.StatusForbidden,
		},
	}

	for idx, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, cleanup := setupAgentAPIMiddlewareTest(t)
			defer cleanup()

			ownerID := int64(91000 + idx + 1)
			agentID := int64(92000 + idx + 1)
			apiKey := "ak_auth_scope_case_" + strconv.Itoa(idx+1)
			seedMiddlewareOwnerAndAgent(t, ownerID, agentID, tt.providerType, tt.status, apiKey)

			var gotAgentID, gotOwnerID int64
			r := gin.New()
			r.Use(AgentAPIAuth())
			r.GET("/protected", func(c *gin.Context) {
				gotAgentID = GetAgentID(c)
				gotOwnerID = GetOwnerID(c)
				c.Status(http.StatusOK)
			})

			w := requestWithBearer(r, http.MethodGet, "/protected", apiKey)
			if w.Code != tt.wantStatus {
				t.Fatalf("expected status %d, got %d, body: %s", tt.wantStatus, w.Code, w.Body.String())
			}
			if tt.wantStatus == http.StatusOK {
				if gotAgentID != agentID {
					t.Fatalf("expected agent_id %d, got %d", agentID, gotAgentID)
				}
				if gotOwnerID != ownerID {
					t.Fatalf("expected owner_id %d, got %d", ownerID, gotOwnerID)
				}
			}
		})
	}
}

func TestAgentAPIScopeMiddleware(t *testing.T) {
	const (
		ownerID = int64(93001)
		agentID = int64(94001)
		apiKey  = "ak_scope_middleware_key"
	)

	t.Run("allows when scope exists", func(t *testing.T) {
		_, cleanup := setupAgentAPIMiddlewareTest(t)
		defer cleanup()
		seedMiddlewareOwnerAndAgent(t, ownerID, agentID, model.AgentProviderAPI, model.AgentStatusActive, apiKey)
		if err := store.DB.Create(&model.AgentAPIScope{
			AgentID: agentID,
			Scope:   agentscope.ScopeGroupMemberAdd,
		}).Error; err != nil {
			t.Fatalf("seed scope error: %v", err)
		}

		r := gin.New()
		r.Use(AgentAPIAuth())
		r.Use(AgentAPIScope(agentscope.ScopeGroupMemberAdd))
		r.GET("/protected", func(c *gin.Context) {
			c.Status(http.StatusOK)
		})

		w := requestWithBearer(r, http.MethodGet, "/protected", apiKey)
		if w.Code != http.StatusOK {
			t.Fatalf("expected status 200, got %d, body: %s", w.Code, w.Body.String())
		}
	})

	t.Run("rejects when scope missing", func(t *testing.T) {
		_, cleanup := setupAgentAPIMiddlewareTest(t)
		defer cleanup()
		seedMiddlewareOwnerAndAgent(t, ownerID, agentID, model.AgentProviderAPI, model.AgentStatusActive, apiKey)

		r := gin.New()
		r.Use(AgentAPIAuth())
		r.Use(AgentAPIScope(agentscope.ScopeGroupMemberAdd))
		r.GET("/protected", func(c *gin.Context) {
			c.Status(http.StatusOK)
		})

		w := requestWithBearer(r, http.MethodGet, "/protected", apiKey)
		if w.Code != http.StatusForbidden {
			t.Fatalf("expected status 403, got %d, body: %s", w.Code, w.Body.String())
		}

		var resp middlewareFailResp
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("unmarshal error: %v", err)
		}
		if resp.Code != 20011 {
			t.Fatalf("expected biz code 20011, got %d", resp.Code)
		}
	})

	t.Run("rejects invalid required scope config", func(t *testing.T) {
		_, cleanup := setupAgentAPIMiddlewareTest(t)
		defer cleanup()
		seedMiddlewareOwnerAndAgent(t, ownerID, agentID, model.AgentProviderAPI, model.AgentStatusActive, apiKey)

		r := gin.New()
		r.Use(AgentAPIAuth())
		r.Use(AgentAPIScope("group.unknown.scope"))
		r.GET("/protected", func(c *gin.Context) {
			c.Status(http.StatusOK)
		})

		w := requestWithBearer(r, http.MethodGet, "/protected", apiKey)
		if w.Code != http.StatusInternalServerError {
			t.Fatalf("expected status 500, got %d, body: %s", w.Code, w.Body.String())
		}
	})
}
