package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/askie/grix/backend/internal/liveactivity"
	"github.com/askie/grix/backend/internal/model"
	jwtpkg "github.com/askie/grix/backend/internal/pkg/jwt"
	"github.com/askie/grix/backend/internal/pkg/logger"
	"github.com/askie/grix/backend/internal/pkg/middleware"
	"github.com/askie/grix/backend/internal/pkg/snowflake"
	"github.com/askie/grix/backend/internal/pkg/testutil"
	"github.com/askie/grix/backend/internal/store"
	"github.com/askie/grix/backend/internal/ws/protocol"
	"github.com/gin-gonic/gin"
	"github.com/nats-io/nats.go"
)

// liveActivityFakeJS 只实现 Publish：实时活动的补发帧从这里出去。
type liveActivityFakeJS struct {
	nats.JetStreamContext
	mu   sync.Mutex
	msgs [][]byte
}

func (f *liveActivityFakeJS) Publish(_ string, data []byte, _ ...nats.PubOpt) (*nats.PubAck, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	cloned := make([]byte, len(data))
	copy(cloned, data)
	f.msgs = append(f.msgs, cloned)
	return &nats.PubAck{}, nil
}

var liveActivityJS *liveActivityFakeJS

func catchUpUpdates(sessionID string) []protocol.LiveActivityPayload {
	liveActivityJS.mu.Lock()
	defer liveActivityJS.mu.Unlock()
	var out []protocol.LiveActivityPayload
	for _, raw := range liveActivityJS.msgs {
		var task struct {
			Cmd     string                       `json:"cmd"`
			Payload protocol.LiveActivityPayload `json:"payload"`
		}
		if err := json.Unmarshal(raw, &task); err != nil {
			continue
		}
		if task.Cmd == protocol.CmdLiveActivity &&
			task.Payload.SessionID == sessionID &&
			task.Payload.Event == protocol.LiveActivityEventUpdate {
			out = append(out, task.Payload)
		}
	}
	return out
}

func setupLiveActivityHandlerTest(t *testing.T) (*gin.Engine, map[int64]string) {
	t.Helper()
	// 补发帧跑在后台协程里，会打日志——没初始化 logger 会直接空指针崩掉测试进程。
	logger.Init()

	testDB := testutil.NewTestDB()
	store.DB = testDB.DB
	store.RDB = testutil.NewMockRedis()
	liveActivityJS = &liveActivityFakeJS{}
	store.JS = liveActivityJS
	t.Cleanup(func() {
		testDB.Close()
		store.JS = nil
	})

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
// token 一落地就按当前状态补一帧：start 推出去到设备把 token 报回来之间的空窗里，
// 状态变化没有任何 token 可发，全丢了。
func TestLiveActivityTokenBindTriggersCatchUp(t *testing.T) {
	r, tokens := setupLiveActivityHandlerTest(t)
	const ownerID = int64(13001)
	const sessionID = "session-catch-up"
	seedLiveActivityChatState(t, sessionID, ownerID)

	// 备一张已经在锁屏上的卡，否则补发无从谈起。
	liveactivity.OnWaiting(
		liveactivity.Run{UserID: ownerID, AgentID: 777, SessionID: sessionID},
		protocol.LiveActivityPhaseWaitingApproval,
		"要删除生产数据库",
	)

	w := postActivityToken(t, r, tokens[ownerID], map[string]any{
		"session_id":  sessionID,
		"activity_id": "activity-catch-up",
		"token":       "activity-token-catch-up",
		"device_id":   "device-catch-up",
	})
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}

	// 补发在后台跑，客户端不必等一次 NATS 发布。
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if len(catchUpUpdates(sessionID)) > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	updates := catchUpUpdates(sessionID)
	if len(updates) != 1 {
		t.Fatalf("expected exactly 1 catch-up frame, got %d", len(updates))
	}
	// chat_states 现在是 running，补的这一帧必须和它一致，而不是卡上那个等待阶段。
	if updates[0].ContentState.Phase != protocol.LiveActivityPhaseRunning {
		t.Fatalf("catch-up phase = %s, want the current chat_states phase", updates[0].ContentState.Phase)
	}
	if updates[0].Alert != nil {
		t.Fatal("a catch-up frame must not alert")
	}
}

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
