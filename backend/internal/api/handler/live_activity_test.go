package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/askie/grix/backend/internal/liveactivity"
	"github.com/askie/grix/backend/internal/model"
	jwtpkg "github.com/askie/grix/backend/internal/pkg/jwt"
	"github.com/askie/grix/backend/internal/pkg/middleware"
	"github.com/askie/grix/backend/internal/pkg/snowflake"
	"github.com/askie/grix/backend/internal/pkg/testutil"
	"github.com/askie/grix/backend/internal/store"
	"github.com/gin-gonic/gin"
)

func setupLiveActivityHandlerTest(t *testing.T) (*gin.Engine, map[int64]string) {
	t.Helper()

	testDB := testutil.NewTestDB()
	store.DB = testDB.DB
	store.RDB = testutil.NewMockRedis()
	t.Cleanup(func() { testDB.Close() })

	jwtpkg.Init("test-secret-key", 3600, 86400)
	_ = snowflake.Init(1)

	fixture := testutil.NewFixtureBuilder(testDB.DB)
	tokens := make(map[int64]string, 2)
	for i, id := range []int64{13001, 13002} {
		user := fixture.CreateUser(func(u *model.User) {
			u.ID = id
			u.Username = "live_activity_user_" + string(rune('a'+i))
			u.Email = u.Username + "@example.com"
		})
		token, _, err := jwtpkg.GenerateAccessToken(user.ID)
		if err != nil {
			t.Fatalf("GenerateAccessToken() error = %v", err)
		}
		tokens[user.ID] = token
	}

	r := gin.New()
	r.Use(middleware.Auth())
	r.POST("/live_activities/token", LiveActivityTokenBind)
	return r, tokens
}

func seedLiveActivityChatState(t *testing.T, sessionID string, ownerID int64) {
	t.Helper()
	now := time.Now().UTC()
	row := model.SessionAgentState{
		SessionID: sessionID,
		OwnerID:   ownerID,
		AgentID:   777,
		State:     model.SessionAgentStateRunning,
		StartedAt: &now,
		UpdatedAt: now,
	}
	if err := store.DB.Save(&row).Error; err != nil {
		t.Fatalf("seed chat_states: %v", err)
	}
}

func postActivityToken(t *testing.T, r *gin.Engine, token string, body map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	raw, _ := json.Marshal(body)
	req, _ := http.NewRequest(http.MethodPost, "/live_activities/token", bytes.NewReader(raw))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestLiveActivityTokenBindStoresUnderCallerKey(t *testing.T) {
	r, tokens := setupLiveActivityHandlerTest(t)
	const ownerID = int64(13001)
	const sessionID = "session-owned"
	seedLiveActivityChatState(t, sessionID, ownerID)

	w := postActivityToken(t, r, tokens[ownerID], map[string]any{
		"session_id":  sessionID,
		"activity_id": "activity-1",
		"token":       "activity-token-1",
		"device_id":   "device-1",
	})
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}

	raw, err := store.RDB.HGet(
		context.Background(),
		liveactivity.TokenKey(ownerID, sessionID),
		"device-1",
	).Result()
	if err != nil {
		t.Fatalf("token not stored under the caller's key: %v", err)
	}
	var entry liveactivity.TokenEntry
	if err := json.Unmarshal([]byte(raw), &entry); err != nil {
		t.Fatalf("stored entry is not decodable: %v", err)
	}
	if entry.ActivityID != "activity-1" || entry.Token != "activity-token-1" {
		t.Fatalf("stored entry = %+v", entry)
	}

	ttl, err := store.RDB.TTL(context.Background(), liveactivity.TokenKey(ownerID, sessionID)).Result()
	if err != nil {
		t.Fatalf("ttl: %v", err)
	}
	if ttl <= 0 || ttl > liveactivity.TokenTTLSeconds*time.Second {
		t.Fatalf("ttl = %s, want a bounded 24h expiry", ttl)
	}
}

// 会话归属看 chat_states 的 (session_id, owner_id)，与离线操作回调同款。
// 别人的会话一律 403，绝不 404——会话存不存在不该由未授权的调用者试探出来。
func TestLiveActivityTokenBindRejectsForeignSession(t *testing.T) {
	r, tokens := setupLiveActivityHandlerTest(t)
	const ownerID = int64(13001)
	const strangerID = int64(13002)
	const sessionID = "session-owned"
	seedLiveActivityChatState(t, sessionID, ownerID)

	w := postActivityToken(t, r, tokens[strangerID], map[string]any{
		"session_id":  sessionID,
		"activity_id": "activity-2",
		"token":       "activity-token-2",
		"device_id":   "device-2",
	})
	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403; body = %s", w.Code, w.Body.String())
	}

	count, err := store.RDB.Exists(context.Background(), liveactivity.TokenKey(strangerID, sessionID)).Result()
	if err != nil {
		t.Fatalf("exists: %v", err)
	}
	if count != 0 {
		t.Fatal("a rejected request must not store anything")
	}
}

func TestLiveActivityTokenBindRejectsUnknownSession(t *testing.T) {
	r, tokens := setupLiveActivityHandlerTest(t)

	w := postActivityToken(t, r, tokens[13001], map[string]any{
		"session_id":  "session-never-ran",
		"activity_id": "activity-3",
		"token":       "activity-token-3",
		"device_id":   "device-3",
	})
	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403; body = %s", w.Code, w.Body.String())
	}
}

func TestLiveActivityTokenBindRejectsMissingFields(t *testing.T) {
	r, tokens := setupLiveActivityHandlerTest(t)

	w := postActivityToken(t, r, tokens[13001], map[string]any{
		"session_id": "session-owned",
		"token":      "activity-token-4",
		"device_id":  "device-4",
	})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body = %s", w.Code, w.Body.String())
	}
}
