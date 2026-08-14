package handler

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/askie/grix/backend/internal/model"
	jwtpkg "github.com/askie/grix/backend/internal/pkg/jwt"
	"github.com/askie/grix/backend/internal/pkg/snowflake"
	"github.com/askie/grix/backend/internal/pkg/testutil"
	"github.com/askie/grix/backend/internal/store"
	"github.com/gin-gonic/gin"
)

func setupFriendTest(t *testing.T) *testContext {
	t.Helper()

	testDB := testutil.NewTestDB()
	store.DB = testDB.DB

	jwtpkg.Init("test-secret-key-for-testing", 3600, 86400)
	if err := snowflake.Init(1); err != nil {
		t.Fatalf("failed to init snowflake: %v", err)
	}

	r := gin.New()

	authed := r.Group("/")
	authed.Use(func(c *gin.Context) {
		token := c.GetHeader("Authorization")
		if token == "" {
			c.AbortWithStatusJSON(401, gin.H{"error": "unauthorized"})
			return
		}
		claims, err := jwtpkg.ValidateAccessToken(token)
		if err != nil {
			c.AbortWithStatusJSON(401, gin.H{"error": "invalid token"})
			return
		}
		c.Set("user_id", claims.UserID)
		c.Next()
	})

	authed.GET("/users/search", UserSearch)
	authed.POST("/friends/request", FriendRequestSend)
	authed.GET("/friends/requests", FriendRequestList)
	authed.POST("/friends/handle", FriendRequestHandle)
	authed.GET("/friends/list", FriendList)
	authed.POST("/friends/remark", FriendRemarkUpdate)
	authed.POST("/friends/block", FriendBlock)
	authed.DELETE("/friends/:id", FriendDelete)

	return &testContext{
		router: r,
		db:     testDB,
	}
}

func TestUserSearch(t *testing.T) {
	tc := setupFriendTest(t)
	defer tc.cleanup()

	fixture := testutil.NewFixtureBuilder(tc.db.DB)
	user := fixture.CreateUserWithDefaults(1001, "alice")
	fixture.CreateUserWithDefaults(1002, "bob")
	fixture.CreateUserWithDefaults(1003, "alice_wang")
	fixture.CreateUserWithDefaults(1004, "delegate_owner_1773115756504928")
	fixture.CreateUserWithDefaults(1005, "delegate_sender_1773115756504928")

	token, _, _ := jwtpkg.GenerateAccessToken(user.ID)

	tests := []struct {
		name       string
		keyword    string
		wantStatus int
		wantCount  int
	}{
		{"search found", "alice", http.StatusOK, 1}, // alice_wang, not self
		{"search bob", "bob", http.StatusOK, 1},
		{"search hidden delegate users", "delegate", http.StatusOK, 0},
		{"search no result", "nobody", http.StatusOK, 0},
		{"empty keyword", "", http.StatusBadRequest, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			url := "/users/search?keyword=" + tt.keyword
			req, _ := http.NewRequest(http.MethodGet, url, nil)
			req.Header.Set("Authorization", token)

			w := httptest.NewRecorder()
			tc.router.ServeHTTP(w, req)

			if w.Code != tt.wantStatus {
				t.Errorf("expected status %d, got %d, body: %s", tt.wantStatus, w.Code, w.Body.String())
				return
			}

			if tt.wantStatus == http.StatusOK {
				var resp map[string]interface{}
				_ = json.Unmarshal(w.Body.Bytes(), &resp)
				data := resp["data"].(map[string]interface{})
				list := data["list"].([]interface{})
				if len(list) != tt.wantCount {
					t.Errorf("expected %d results, got %d", tt.wantCount, len(list))
				}
			}
		})
	}
}

func TestFriendRequestSend(t *testing.T) {
	tc := setupFriendTest(t)
	defer tc.cleanup()

	fixture := testutil.NewFixtureBuilder(tc.db.DB)
	user1 := fixture.CreateUserWithDefaults(2001, "user1")
	fixture.CreateUserWithDefaults(2002, "user2")

	token, _, _ := jwtpkg.GenerateAccessToken(user1.ID)

	tests := []struct {
		name       string
		payload    sendFriendReq
		wantStatus int
	}{
		{"send request", sendFriendReq{ToUserID: 2002, Message: "hi"}, http.StatusOK},
		{"duplicate request", sendFriendReq{ToUserID: 2002, Message: "hi again"}, http.StatusBadRequest},
		{"add self", sendFriendReq{ToUserID: 2001, Message: ""}, http.StatusBadRequest},
		{"target not found", sendFriendReq{ToUserID: 9999, Message: ""}, http.StatusBadRequest},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body, _ := json.Marshal(tt.payload)
			req, _ := http.NewRequest(http.MethodPost, "/friends/request", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Authorization", token)

			w := httptest.NewRecorder()
			tc.router.ServeHTTP(w, req)

			if w.Code != tt.wantStatus {
				t.Errorf("expected status %d, got %d, body: %s", tt.wantStatus, w.Code, w.Body.String())
			}
		})
	}
}

func TestFriendRequestSendRespectsTargetFriendAddSetting(t *testing.T) {
	tc := setupFriendTest(t)
	defer tc.cleanup()

	fixture := testutil.NewFixtureBuilder(tc.db.DB)
	sender := fixture.CreateUserWithDefaults(2401, "sender_2401")
	needReviewUser := fixture.CreateUserWithDefaults(2402, "need_review_2402")
	autoApproveUser := fixture.CreateUserWithDefaults(2403, "auto_approve_2403")
	forbiddenUser := fixture.CreateUserWithDefaults(2404, "forbidden_2404")

	if err := tc.db.DB.Create(&model.UserSetting{
		UserID:           autoApproveUser.ID,
		FriendAddSetting: model.FriendAddSettingAutoApprove,
	}).Error; err != nil {
		t.Fatalf("seed auto approve user setting error: %v", err)
	}
	if err := tc.db.DB.Create(&model.UserSetting{
		UserID:           forbiddenUser.ID,
		FriendAddSetting: model.FriendAddSettingForbidden,
	}).Error; err != nil {
		t.Fatalf("seed forbidden user setting error: %v", err)
	}

	token, _, _ := jwtpkg.GenerateAccessToken(sender.ID)

	sendReq := func(targetID int64) *httptest.ResponseRecorder {
		body, _ := json.Marshal(sendFriendReq{
			ToUserID: targetID,
			Message:  "hello",
		})
		req, _ := http.NewRequest(http.MethodPost, "/friends/request", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", token)
		w := httptest.NewRecorder()
		tc.router.ServeHTTP(w, req)
		return w
	}

	t.Run("need review creates pending request", func(t *testing.T) {
		w := sendReq(needReviewUser.ID)
		if w.Code != http.StatusOK {
			t.Fatalf("expected status 200, got %d, body=%s", w.Code, w.Body.String())
		}

		var req model.FriendRequest
		if err := tc.db.DB.
			Where("from_user_id = ? AND to_user_id = ?", sender.ID, needReviewUser.ID).
			First(&req).Error; err != nil {
			t.Fatalf("query pending request error: %v", err)
		}
		if req.Status != 0 {
			t.Fatalf("expected pending status=0, got %d", req.Status)
		}
	})

	t.Run("auto approve creates friendship immediately", func(t *testing.T) {
		w := sendReq(autoApproveUser.ID)
		if w.Code != http.StatusOK {
			t.Fatalf("expected status 200, got %d, body=%s", w.Code, w.Body.String())
		}

		var req model.FriendRequest
		if err := tc.db.DB.
			Where("from_user_id = ? AND to_user_id = ?", sender.ID, autoApproveUser.ID).
			First(&req).Error; err != nil {
			t.Fatalf("query auto-approved request error: %v", err)
		}
		if req.Status != 1 {
			t.Fatalf("expected accepted status=1, got %d", req.Status)
		}

		var relCount int64
		if err := tc.db.DB.Model(&model.Friend{}).
			Where("user_id = ? AND friend_id = ?", sender.ID, autoApproveUser.ID).
			Count(&relCount).Error; err != nil {
			t.Fatalf("query sender->target friendship error: %v", err)
		}
		if relCount != 1 {
			t.Fatalf("expected sender->target friendship count=1, got %d", relCount)
		}
		if err := tc.db.DB.Model(&model.Friend{}).
			Where("user_id = ? AND friend_id = ?", autoApproveUser.ID, sender.ID).
			Count(&relCount).Error; err != nil {
			t.Fatalf("query target->sender friendship error: %v", err)
		}
		if relCount != 1 {
			t.Fatalf("expected target->sender friendship count=1, got %d", relCount)
		}
	})

	t.Run("forbidden rejects friend request", func(t *testing.T) {
		w := sendReq(forbiddenUser.ID)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("expected status 400, got %d, body=%s", w.Code, w.Body.String())
		}

		var reqCount int64
		if err := tc.db.DB.Model(&model.FriendRequest{}).
			Where("from_user_id = ? AND to_user_id = ?", sender.ID, forbiddenUser.ID).
			Count(&reqCount).Error; err != nil {
			t.Fatalf("count forbidden request error: %v", err)
		}
		if reqCount != 0 {
			t.Fatalf("expected forbidden request count=0, got %d", reqCount)
		}
	})

	t.Run("blocked user rejects friend request", func(t *testing.T) {
		blockedUser := fixture.CreateUserWithDefaults(2405, "blocked_2405")
		if err := tc.db.DB.Create(&model.UserBlock{
			ID:            92405,
			UserID:        blockedUser.ID,
			BlockedUserID: sender.ID,
		}).Error; err != nil {
			t.Fatalf("seed user block error: %v", err)
		}

		w := sendReq(blockedUser.ID)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("expected status 400, got %d, body=%s", w.Code, w.Body.String())
		}

		if !strings.Contains(w.Body.String(), "blocked") {
			t.Fatalf("expected blocked error message, got body=%s", w.Body.String())
		}
	})

	t.Run("blocker cannot send friend request", func(t *testing.T) {
		blockerUser := fixture.CreateUserWithDefaults(2406, "blocker_2406")
		if err := tc.db.DB.Create(&model.UserBlock{
			ID:            92406,
			UserID:        sender.ID,
			BlockedUserID: blockerUser.ID,
		}).Error; err != nil {
			t.Fatalf("seed self-side user block error: %v", err)
		}

		w := sendReq(blockerUser.ID)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("expected status 400, got %d, body=%s", w.Code, w.Body.String())
		}

		if !strings.Contains(w.Body.String(), "you have blocked") {
			t.Fatalf("expected self-block error message, got body=%s", w.Body.String())
		}
	})
}

func TestFriendRequestSendAutoApproveExistingPendingRequest(t *testing.T) {
	tc := setupFriendTest(t)
	defer tc.cleanup()

	fixture := testutil.NewFixtureBuilder(tc.db.DB)
	sender := fixture.CreateUserWithDefaults(2501, "sender_2501")
	target := fixture.CreateUserWithDefaults(2502, "target_2502")
	if err := tc.db.DB.Create(&model.UserSetting{
		UserID:           target.ID,
		FriendAddSetting: model.FriendAddSettingAutoApprove,
	}).Error; err != nil {
		t.Fatalf("seed target user setting error: %v", err)
	}

	pendingReq := model.FriendRequest{
		ID:         98001,
		FromUserID: sender.ID,
		ToUserID:   target.ID,
		Status:     0,
		Message:    "pending",
	}
	if err := tc.db.DB.Create(&pendingReq).Error; err != nil {
		t.Fatalf("seed pending request error: %v", err)
	}

	token, _, _ := jwtpkg.GenerateAccessToken(sender.ID)
	body, _ := json.Marshal(sendFriendReq{
		ToUserID: target.ID,
		Message:  "resend",
	})
	req, _ := http.NewRequest(http.MethodPost, "/friends/request", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", token)
	w := httptest.NewRecorder()
	tc.router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d, body=%s", w.Code, w.Body.String())
	}

	var refreshed model.FriendRequest
	if err := tc.db.DB.First(&refreshed, pendingReq.ID).Error; err != nil {
		t.Fatalf("query refreshed request error: %v", err)
	}
	if refreshed.Status != 1 {
		t.Fatalf("expected pending request auto accepted status=1, got %d", refreshed.Status)
	}

	var relCount int64
	if err := tc.db.DB.Model(&model.Friend{}).
		Where("user_id = ? AND friend_id = ?", sender.ID, target.ID).
		Count(&relCount).Error; err != nil {
		t.Fatalf("query sender->target friendship error: %v", err)
	}
	if relCount != 1 {
		t.Fatalf("expected sender->target friendship count=1, got %d", relCount)
	}
}

func TestFriendRequestSendByUsername(t *testing.T) {
	tc := setupFriendTest(t)
	defer tc.cleanup()

	fixture := testutil.NewFixtureBuilder(tc.db.DB)
	user1 := fixture.CreateUserWithDefaults(2101, "user_a")
	fixture.CreateUserWithDefaults(2102, "user_b")

	token, _, _ := jwtpkg.GenerateAccessToken(user1.ID)

	tests := []struct {
		name       string
		payload    sendFriendReq
		wantStatus int
	}{
		{"send by username", sendFriendReq{ToUsername: "user_b", Message: "hi"}, http.StatusOK},
		{"duplicate by username", sendFriendReq{ToUsername: "user_b", Message: "again"}, http.StatusBadRequest},
		{"username not found", sendFriendReq{ToUsername: "not_exist"}, http.StatusBadRequest},
		{"missing target", sendFriendReq{Message: "no target"}, http.StatusBadRequest},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body, _ := json.Marshal(tt.payload)
			req, _ := http.NewRequest(http.MethodPost, "/friends/request", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Authorization", token)

			w := httptest.NewRecorder()
			tc.router.ServeHTTP(w, req)

			if w.Code != tt.wantStatus {
				t.Errorf("expected status %d, got %d, body: %s", tt.wantStatus, w.Code, w.Body.String())
			}
		})
	}
}

func TestFriendRequestSendRejectsHiddenSearchUsers(t *testing.T) {
	tc := setupFriendTest(t)
	defer tc.cleanup()

	fixture := testutil.NewFixtureBuilder(tc.db.DB)
	requester := fixture.CreateUserWithDefaults(2201, "requester")
	delegateTarget := fixture.CreateUserWithDefaults(2202, "delegate_owner_1773115756504928")

	token, _, _ := jwtpkg.GenerateAccessToken(requester.ID)

	tests := []struct {
		name       string
		payload    sendFriendReq
		wantStatus int
	}{
		{
			name:       "reject hidden user by username",
			payload:    sendFriendReq{ToUsername: delegateTarget.Username, Message: "hi"},
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "reject hidden user by id",
			payload:    sendFriendReq{ToUserID: delegateTarget.ID, Message: "hi"},
			wantStatus: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body, _ := json.Marshal(tt.payload)
			req, _ := http.NewRequest(http.MethodPost, "/friends/request", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Authorization", token)

			w := httptest.NewRecorder()
			tc.router.ServeHTTP(w, req)

			if w.Code != tt.wantStatus {
				t.Errorf("expected status %d, got %d, body: %s", tt.wantStatus, w.Code, w.Body.String())
			}
		})
	}
}

func TestFriendRequestSendRejectsAgentTarget(t *testing.T) {
	tc := setupFriendTest(t)
	defer tc.cleanup()

	fixture := testutil.NewFixtureBuilder(tc.db.DB)
	requester := fixture.CreateUserWithDefaults(2301, "requester_agent_guard")
	owner := fixture.CreateUserWithDefaults(2302, "agent_owner_guard")
	agentID := int64(93001)
	if err := tc.db.DB.Create(&model.Agent{
		ID:        agentID,
		OwnerID:   owner.ID,
		AgentName: "owner_agent",
		Status:    1,
	}).Error; err != nil {
		t.Fatalf("seed foreign agent error: %v", err)
	}

	token, _, _ := jwtpkg.GenerateAccessToken(requester.ID)
	body, _ := json.Marshal(sendFriendReq{
		ToUserID: agentID,
		Message:  "hi",
	})
	req, _ := http.NewRequest(http.MethodPost, "/friends/request", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", token)

	w := httptest.NewRecorder()
	tc.router.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d, body: %s", w.Code, w.Body.String())
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response error: %v", err)
	}
	msg, _ := resp["msg"].(string)
	if !strings.Contains(msg, "agent") {
		t.Fatalf("expected agent related error, got msg=%q", msg)
	}
}

func TestFriendRequestHandle(t *testing.T) {
	tc := setupFriendTest(t)
	defer tc.cleanup()

	fixture := testutil.NewFixtureBuilder(tc.db.DB)
	user1 := fixture.CreateUserWithDefaults(3001, "requester")
	user2 := fixture.CreateUserWithDefaults(3002, "receiver")

	// Create a pending request
	friendReq := model.FriendRequest{
		ID:         99001,
		FromUserID: user1.ID,
		ToUserID:   user2.ID,
		Status:     0,
		Message:    "hello",
	}
	tc.db.DB.Create(&friendReq)

	// Create another request for rejection test
	friendReq2 := model.FriendRequest{
		ID:         99002,
		FromUserID: user1.ID,
		ToUserID:   user2.ID,
		Status:     0,
		Message:    "please add me",
	}
	tc.db.DB.Create(&friendReq2)

	token2, _, _ := jwtpkg.GenerateAccessToken(user2.ID)
	token1, _, _ := jwtpkg.GenerateAccessToken(user1.ID)

	tests := []struct {
		name       string
		token      string
		payload    handleFriendReq
		wantStatus int
	}{
		{"accept request", token2, handleFriendReq{RequestID: 99001, Accept: true}, http.StatusOK},
		{"already handled", token2, handleFriendReq{RequestID: 99001, Accept: true}, http.StatusBadRequest},
		{"not authorized", token1, handleFriendReq{RequestID: 99002, Accept: true}, http.StatusBadRequest},
		{"reject request", token2, handleFriendReq{RequestID: 99002, Accept: false}, http.StatusOK},
		{"not found", token2, handleFriendReq{RequestID: 99999, Accept: true}, http.StatusBadRequest},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body, _ := json.Marshal(tt.payload)
			req, _ := http.NewRequest(http.MethodPost, "/friends/handle", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Authorization", tt.token)

			w := httptest.NewRecorder()
			tc.router.ServeHTTP(w, req)

			if w.Code != tt.wantStatus {
				t.Errorf("expected status %d, got %d, body: %s", tt.wantStatus, w.Code, w.Body.String())
			}
		})
	}

	// Verify that accept created bidirectional friendship
	var count int64
	tc.db.DB.Model(&model.Friend{}).Where("user_id = ? AND friend_id = ?", user1.ID, user2.ID).Count(&count)
	if count != 1 {
		t.Errorf("expected friend record from %d to %d, got count=%d", user1.ID, user2.ID, count)
	}
	tc.db.DB.Model(&model.Friend{}).Where("user_id = ? AND friend_id = ?", user2.ID, user1.ID).Count(&count)
	if count != 1 {
		t.Errorf("expected reverse friend record, got count=%d", count)
	}

	t.Run("reject accept when target has blocked requester", func(t *testing.T) {
		blockedReq := model.FriendRequest{
			ID:         99003,
			FromUserID: user1.ID,
			ToUserID:   user2.ID,
			Status:     0,
			Message:    "hello again",
		}
		if err := tc.db.DB.Create(&blockedReq).Error; err != nil {
			t.Fatalf("create blocked request error: %v", err)
		}
		if err := tc.db.DB.Create(&model.UserBlock{
			ID:            99004,
			UserID:        user2.ID,
			BlockedUserID: user1.ID,
		}).Error; err != nil {
			t.Fatalf("create user block error: %v", err)
		}

		body, _ := json.Marshal(handleFriendReq{RequestID: 99003, Accept: true})
		req, _ := http.NewRequest(http.MethodPost, "/friends/handle", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", token2)

		w := httptest.NewRecorder()
		tc.router.ServeHTTP(w, req)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("expected status 400, got %d, body: %s", w.Code, w.Body.String())
		}

		var count int64
		tc.db.DB.Model(&model.Friend{}).
			Where("user_id = ? AND friend_id = ?", user1.ID, user2.ID).
			Count(&count)
		if count != 1 {
			t.Fatalf("blocked acceptance should not create extra friendship, got count=%d", count)
		}
	})
}

func TestFriendList(t *testing.T) {
	tc := setupFriendTest(t)
	defer tc.cleanup()

	fixture := testutil.NewFixtureBuilder(tc.db.DB)
	user1 := fixture.CreateUserWithDefaults(4001, "lister")
	user2 := fixture.CreateUserWithDefaults(4002, "friend1")

	// Create friendship
	tc.db.DB.Create(&model.Friend{ID: 88001, UserID: user1.ID, FriendID: user2.ID})
	tc.db.DB.Create(&model.Friend{ID: 88002, UserID: user2.ID, FriendID: user1.ID})

	token, _, _ := jwtpkg.GenerateAccessToken(user1.ID)

	req, _ := http.NewRequest(http.MethodGet, "/friends/list", nil)
	req.Header.Set("Authorization", token)

	w := httptest.NewRecorder()
	tc.router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d, body: %s", w.Code, w.Body.String())
	}

	var resp map[string]interface{}
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	data := resp["data"].(map[string]interface{})
	list := data["list"].([]interface{})
	if len(list) != 1 {
		t.Errorf("expected 1 friend, got %d", len(list))
	}
}

func TestFriendDelete(t *testing.T) {
	tc := setupFriendTest(t)
	defer tc.cleanup()

	fixture := testutil.NewFixtureBuilder(tc.db.DB)
	user1 := fixture.CreateUserWithDefaults(5001, "deleter")
	user2 := fixture.CreateUserWithDefaults(5002, "deletee")

	tc.db.DB.Create(&model.Friend{ID: 77001, UserID: user1.ID, FriendID: user2.ID})
	tc.db.DB.Create(&model.Friend{ID: 77002, UserID: user2.ID, FriendID: user1.ID})

	token, _, _ := jwtpkg.GenerateAccessToken(user1.ID)

	url := fmt.Sprintf("/friends/%d", user2.ID)
	req, _ := http.NewRequest(http.MethodDelete, url, nil)
	req.Header.Set("Authorization", token)

	w := httptest.NewRecorder()
	tc.router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d, body: %s", w.Code, w.Body.String())
	}

	// Verify both directions deleted
	var count int64
	tc.db.DB.Model(&model.Friend{}).Where("user_id = ? AND friend_id = ?", user1.ID, user2.ID).Count(&count)
	if count != 0 {
		t.Errorf("expected friend record to be deleted, got count=%d", count)
	}
	tc.db.DB.Model(&model.Friend{}).Where("user_id = ? AND friend_id = ?", user2.ID, user1.ID).Count(&count)
	if count != 0 {
		t.Errorf("expected reverse friend record to be deleted, got count=%d", count)
	}
}

func TestFriendBlock(t *testing.T) {
	tc := setupFriendTest(t)
	defer tc.cleanup()

	fixture := testutil.NewFixtureBuilder(tc.db.DB)
	user1 := fixture.CreateUserWithDefaults(5101, "blocker")
	user2 := fixture.CreateUserWithDefaults(5102, "blocked")

	tc.db.DB.Create(&model.Friend{ID: 77101, UserID: user1.ID, FriendID: user2.ID})
	tc.db.DB.Create(&model.Friend{ID: 77102, UserID: user2.ID, FriendID: user1.ID})
	tc.db.DB.Create(&model.FriendRequest{
		ID:         77103,
		FromUserID: user2.ID,
		ToUserID:   user1.ID,
		Status:     0,
		Message:    "please add me",
	})

	token, _, _ := jwtpkg.GenerateAccessToken(user1.ID)

	body, _ := json.Marshal(map[string]interface{}{
		"blocked_user_id": fmt.Sprintf("%d", user2.ID),
	})
	req, _ := http.NewRequest(http.MethodPost, "/friends/block", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", token)

	w := httptest.NewRecorder()
	tc.router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d, body: %s", w.Code, w.Body.String())
	}

	var blockCount int64
	tc.db.DB.Model(&model.UserBlock{}).
		Where("user_id = ? AND blocked_user_id = ?", user1.ID, user2.ID).
		Count(&blockCount)
	if blockCount != 1 {
		t.Fatalf("expected user block created, got count=%d", blockCount)
	}

	var friendCount int64
	tc.db.DB.Model(&model.Friend{}).
		Where("user_id = ? AND friend_id = ?", user1.ID, user2.ID).
		Count(&friendCount)
	if friendCount != 0 {
		t.Fatalf("expected friend relation deleted for blocker, got count=%d", friendCount)
	}
	tc.db.DB.Model(&model.Friend{}).
		Where("user_id = ? AND friend_id = ?", user2.ID, user1.ID).
		Count(&friendCount)
	if friendCount != 0 {
		t.Fatalf("expected reverse friend relation deleted, got count=%d", friendCount)
	}

	var requestCount int64
	tc.db.DB.Model(&model.FriendRequest{}).
		Where("status = ? AND ((from_user_id = ? AND to_user_id = ?) OR (from_user_id = ? AND to_user_id = ?))", 0, user1.ID, user2.ID, user2.ID, user1.ID).
		Count(&requestCount)
	if requestCount != 0 {
		t.Fatalf("expected pending friend requests deleted, got count=%d", requestCount)
	}
}

func TestFriendRemarkUpdate(t *testing.T) {
	tc := setupFriendTest(t)
	defer tc.cleanup()

	fixture := testutil.NewFixtureBuilder(tc.db.DB)
	user1 := fixture.CreateUserWithDefaults(6001, "remarker")
	user2 := fixture.CreateUserWithDefaults(6002, "target")

	tc.db.DB.Create(&model.Friend{ID: 66001, UserID: user1.ID, FriendID: user2.ID})
	tc.db.DB.Create(&model.Friend{ID: 66002, UserID: user2.ID, FriendID: user1.ID})

	token, _, _ := jwtpkg.GenerateAccessToken(user1.ID)

	t.Run("set remark success", func(t *testing.T) {
		body, _ := json.Marshal(map[string]interface{}{
			"friend_user_id": fmt.Sprintf("%d", user2.ID),
			"remark_name":    "Best Friend",
		})
		req, _ := http.NewRequest(http.MethodPost, "/friends/remark", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", token)

		w := httptest.NewRecorder()
		tc.router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected status 200, got %d, body: %s", w.Code, w.Body.String())
		}

		var rel model.Friend
		if err := tc.db.DB.Where("user_id = ? AND friend_id = ?", user1.ID, user2.ID).First(&rel).Error; err != nil {
			t.Fatalf("query friend relation error: %v", err)
		}
		if rel.RemarkName != "Best Friend" {
			t.Fatalf("expected remark_name=Best Friend, got %q", rel.RemarkName)
		}
	})

	t.Run("clear remark success", func(t *testing.T) {
		body, _ := json.Marshal(map[string]interface{}{
			"friend_user_id": fmt.Sprintf("%d", user2.ID),
			"remark_name":    "   ",
		})
		req, _ := http.NewRequest(http.MethodPost, "/friends/remark", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", token)

		w := httptest.NewRecorder()
		tc.router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected status 200, got %d, body: %s", w.Code, w.Body.String())
		}

		var rel model.Friend
		if err := tc.db.DB.Where("user_id = ? AND friend_id = ?", user1.ID, user2.ID).First(&rel).Error; err != nil {
			t.Fatalf("query friend relation error: %v", err)
		}
		if rel.RemarkName != "" {
			t.Fatalf("expected remark_name cleared, got %q", rel.RemarkName)
		}
	})

	t.Run("reject non-friend", func(t *testing.T) {
		body, _ := json.Marshal(map[string]interface{}{
			"friend_user_id": "6999",
			"remark_name":    "x",
		})
		req, _ := http.NewRequest(http.MethodPost, "/friends/remark", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", token)

		w := httptest.NewRecorder()
		tc.router.ServeHTTP(w, req)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("expected status 400, got %d, body: %s", w.Code, w.Body.String())
		}
	})
}
