package handler

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/askie/grix/backend/config"
	"github.com/askie/grix/backend/internal/model"
	jwtpkg "github.com/askie/grix/backend/internal/pkg/jwt"
	"github.com/askie/grix/backend/internal/pkg/snowflake"
	"github.com/askie/grix/backend/internal/pkg/testutil"
	"github.com/askie/grix/backend/internal/store"
	"github.com/gin-gonic/gin"
)

func setupWebhookHandlerTest(t *testing.T) (*gin.Engine, *testutil.TestDB, string) {
	t.Helper()
	tdb := testutil.NewTestDB()
	store.DB = tdb.DB
	config.C.Server.AgentAPIDomain = "https://grix.dhf.pub"
	jwtpkg.Init("test-secret-key", 3600, 86400)
	_ = snowflake.Init(1)

	user := model.User{ID: 3001, Username: "u3001", Email: "u3001@example.com", PasswordHash: "x", Nickname: "u3001"}
	if err := tdb.DB.Create(&user).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}
	session := model.Session{SessionID: "s-webhook-1", OwnerID: 3001, SessionType: 1}
	if err := tdb.DB.Create(&session).Error; err != nil {
		t.Fatalf("create session: %v", err)
	}
	member := model.SessionMember{SessionID: session.SessionID, MemberID: 3001, MemberType: 1, Role: 3, JoinedAt: time.Now().UTC()}
	if err := tdb.DB.Create(&member).Error; err != nil {
		t.Fatalf("create member: %v", err)
	}

	token, _, _ := jwtpkg.GenerateAccessToken(user.ID)

	r := gin.New()
	r.Use(func(c *gin.Context) {
		a := c.GetHeader("Authorization")
		if a != "" {
			claims, _ := jwtpkg.ValidateAccessToken(a)
			if claims != nil {
				c.Set("user_id", claims.UserID)
			}
		}
		c.Next()
	})
	r.POST("/api/webhooks", WebhookCreate)
	r.GET("/api/sessions/:session_id/webhooks", WebhookList)
	r.DELETE("/api/webhooks/:id", WebhookDelete)
	return r, tdb, token
}

func TestWebhookHandlerCRUD(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r, tdb, token := setupWebhookHandlerTest(t)
	defer tdb.Close()

	body := bytes.NewBufferString(`{"session_id":"s-webhook-1"}`)
	req, _ := http.NewRequest(http.MethodPost, "/api/webhooks", body)
	req.Header.Set("Authorization", token)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("create status=%d body=%s", w.Code, w.Body.String())
	}

	var created map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &created)
	data := created["data"].(map[string]any)
	id := data["id"].(string)
	if data["url"].(string) == "" {
		t.Fatalf("expected url in create response")
	}

	req, _ = http.NewRequest(http.MethodGet, "/api/sessions/s-webhook-1/webhooks", nil)
	req.Header.Set("Authorization", token)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("list status=%d body=%s", w.Code, w.Body.String())
	}
	var listed map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &listed)
	items := listed["data"].(map[string]any)["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("expected 1 webhook item got %d", len(items))
	}

	req, _ = http.NewRequest(http.MethodDelete, "/api/webhooks/"+id, nil)
	req.Header.Set("Authorization", token)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("delete status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestWebhookBaseURLFallbackToFriendQR(t *testing.T) {
	config.C.Server.AgentAPIDomain = ""
	config.C.Server.FriendQRBaseURL = "https://dhf.pub/u"
	if got := webhookBaseURL(nil); got != "https://dhf.pub" {
		t.Fatalf("unexpected webhook base url: %q", got)
	}
}
