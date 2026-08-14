package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/askie/grix/backend/internal/model"
	jwtpkg "github.com/askie/grix/backend/internal/pkg/jwt"
	"github.com/askie/grix/backend/internal/pkg/snowflake"
	"github.com/askie/grix/backend/internal/pkg/testutil"
	"github.com/askie/grix/backend/internal/store"
	"github.com/askie/grix/backend/internal/systemsetting"
	"github.com/gin-gonic/gin"
)

func init() {
	gin.SetMode(gin.TestMode)
}

func setupSessionHandlerTest(t *testing.T) (*gin.Engine, *testutil.TestDB, func()) {
	t.Helper()

	testDB := testutil.NewTestDB()
	store.DB = testDB.DB
	store.RDB = testutil.NewMockRedis()
	systemsetting.InvalidateGroupSettingsCache()

	jwtpkg.Init("test-secret-key", 3600, 86400)
	_ = snowflake.Init(1)

	r := gin.New()

	// Auth middleware
	r.Use(func(c *gin.Context) {
		token := c.GetHeader("Authorization")
		if token != "" {
			claims, _ := jwtpkg.ValidateAccessToken(token)
			if claims != nil {
				c.Set("user_id", claims.UserID)
			}
		}
		c.Next()
	})

	r.GET("/sessions", SessionList)
	r.GET("/sessions/sync", SessionSync)
	r.GET("/sessions/detail", SessionDetail)
	r.GET("/sessions/group/qr", SessionGroupQRCodeGet)
	r.GET("/sessions/group/qr/resolve/:code", SessionGroupQRCodeResolve)
	r.POST("/sessions", SessionCreate)
	r.POST("/sessions/open_latest", SessionOpenLatest)
	r.POST("/sessions/rename", SessionRename)
	r.POST("/sessions/pin", SessionSetPinned)
	r.POST("/sessions/mute", SessionSetMuted)
	r.POST("/sessions/group", SessionCreateGroup)
	r.POST("/sessions/group/join_by_qr", SessionJoinGroupByQRCode)
	r.POST("/sessions/members/add", SessionAddMembers)
	r.POST("/sessions/members/invite_setting", SessionUpdateInviteSetting)
	r.POST("/sessions/speaking/all_muted", SessionUpdateAllMembersMuted)
	r.POST("/sessions/leave", SessionLeave)
	r.POST("/sessions/members/remove", SessionRemoveMembers)
	r.POST("/sessions/members/nickname", SessionSetGroupNickname)
	r.POST("/sessions/members/speaking", SessionUpdateMemberSpeaking)
	r.POST("/sessions/members/role", SessionUpdateMemberRole)
	r.POST("/sessions/owner/transfer", SessionTransferOwner)
	r.POST("/sessions/dissolve", SessionDissolve)

	return r, testDB, func() {
		systemsetting.InvalidateGroupSettingsCache()
		testDB.Close()
	}
}

func createSessionTestUser(t *testing.T, db *testutil.TestDB, userID int64, username string) string {
	t.Helper()
	fixture := testutil.NewFixtureBuilder(db.DB)
	user := fixture.CreateUser(func(u *model.User) {
		u.ID = userID
		u.Username = username
	})

	token, _, _ := jwtpkg.GenerateAccessToken(user.ID)
	return token
}

func createTestSessionData(t *testing.T, db *testutil.TestDB, userID int64, sessionID string) {
	t.Helper()
	now := time.Now()

	session := model.Session{
		SessionID:      sessionID,
		OwnerID:        userID,
		SessionType:    1,
		LastMsgSummary: "Test message",
	}
	db.DB.Create(&session)

	member := model.SessionMember{
		SessionID:    sessionID,
		MemberID:     userID,
		MemberType:   1,
		Role:         3,
		UnreadCount:  2,
		LastActiveAt: now,
		JoinedAt:     now,
	}
	db.DB.Create(&member)
}

func toInt64(raw interface{}) int64 {
	switch v := raw.(type) {
	case int:
		return int64(v)
	case int32:
		return int64(v)
	case int64:
		return v
	case float64:
		return int64(v)
	case json.Number:
		parsed, err := v.Int64()
		if err == nil {
			return parsed
		}
	}
	return 0
}

func seedHandlerUser(t *testing.T, db *testutil.TestDB, userID int64) {
	t.Helper()
	u := model.User{
		ID:           userID,
		Username:     fmt.Sprintf("huser_%d", userID),
		Email:        fmt.Sprintf("huser_%d@example.com", userID),
		PasswordHash: "x",
		AuthProvider: "local",
		Nickname:     fmt.Sprintf("HUser%d", userID),
	}
	if err := db.DB.Create(&u).Error; err != nil {
		t.Fatalf("seed handler user %d error: %v", userID, err)
	}
}

// handlerFriendRelationIDCounter guarantees unique friends.id values despite
// coarse Windows clock granularity and symmetric userID+friendID sums.
var handlerFriendRelationIDCounter atomic.Int64

func seedHandlerFriendRelation(t *testing.T, db *testutil.TestDB, userID int64, friendID int64) {
	t.Helper()
	rel := model.Friend{
		ID:       time.Now().UnixNano() + handlerFriendRelationIDCounter.Add(1),
		UserID:   userID,
		FriendID: friendID,
	}
	if err := db.DB.Create(&rel).Error; err != nil {
		t.Fatalf("seed handler friend relation %d->%d error: %v", userID, friendID, err)
	}
}

func seedHandlerAgent(t *testing.T, db *testutil.TestDB, agentID, ownerID int64, status int16) {
	t.Helper()
	agent := model.Agent{
		ID:        agentID,
		OwnerID:   ownerID,
		AgentName: fmt.Sprintf("agent_%d", agentID),
		Status:    status,
	}
	if err := db.DB.Create(&agent).Error; err != nil {
		t.Fatalf("seed handler agent %d error: %v", agentID, err)
	}
}

func TestSessionList(t *testing.T) {
	r, testDB, cleanup := setupSessionHandlerTest(t)
	defer cleanup()

	userID := int64(11001)
	token := createSessionTestUser(t, testDB, userID, "sessionlistuser")

	t.Run("empty list", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodGet, "/sessions", nil)
		req.Header.Set("Authorization", token)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("expected status 200, got %d, body: %s", w.Code, w.Body.String())
		}
	})

	t.Run("with sessions", func(t *testing.T) {
		createTestSessionData(t, testDB, userID, "list-session-1")

		req, _ := http.NewRequest(http.MethodGet, "/sessions", nil)
		req.Header.Set("Authorization", token)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("expected status 200, got %d", w.Code)
		}

		var resp map[string]interface{}
		json.Unmarshal(w.Body.Bytes(), &resp)
		data := resp["data"].(map[string]interface{})
		list := data["list"].([]interface{})
		if len(list) == 0 {
			t.Error("expected at least one session")
		}
	})

	t.Run("with pagination params", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodGet, "/sessions?limit=10&offset=0", nil)
		req.Header.Set("Authorization", token)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("expected status 200, got %d", w.Code)
		}
	})
}

func TestSessionSync(t *testing.T) {
	r, testDB, cleanup := setupSessionHandlerTest(t)
	defer cleanup()

	userID := int64(11002)
	token := createSessionTestUser(t, testDB, userID, "syncuser")

	t.Run("sync with since parameter", func(t *testing.T) {
		createTestSessionData(t, testDB, userID, "sync-session-1")

		since := time.Now().Add(-1 * time.Hour).Unix()
		req, _ := http.NewRequest(http.MethodGet, "/sessions/sync", nil)
		req.Header.Set("Authorization", token)
		q := req.URL.Query()
		q.Set("since", strconv.FormatInt(since, 10))
		req.URL.RawQuery = q.Encode()
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("expected status 200, got %d", w.Code)
		}
	})
}

func TestSessionCreate(t *testing.T) {
	r, testDB, cleanup := setupSessionHandlerTest(t)
	defer cleanup()

	userID := int64(11003)
	token := createSessionTestUser(t, testDB, userID, "createsessionuser")
	peerID := int64(11013)
	nonFriendID := int64(11014)
	ownedAgentID := int64(21013)
	foreignOwnerID := int64(11015)
	foreignAgentID := int64(21014)
	seedHandlerUser(t, testDB, peerID)
	seedHandlerUser(t, testDB, nonFriendID)
	seedHandlerUser(t, testDB, foreignOwnerID)
	seedHandlerFriendRelation(t, testDB, userID, peerID)
	seedHandlerAgent(t, testDB, ownedAgentID, userID, 1)
	seedHandlerAgent(t, testDB, foreignAgentID, foreignOwnerID, 1)

	t.Run("create session", func(t *testing.T) {
		body, _ := json.Marshal(createSessionReq{
			PeerID:   peerID,
			PeerType: 1,
		})
		req, _ := http.NewRequest(http.MethodPost, "/sessions", bytes.NewReader(body))
		req.Header.Set("Authorization", token)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("expected status 200, got %d, body: %s", w.Code, w.Body.String())
		}

		var resp map[string]interface{}
		json.Unmarshal(w.Body.Bytes(), &resp)
		data := resp["data"].(map[string]interface{})
		if data["session_id"] == nil {
			t.Error("expected session_id in response")
		}
	})

	t.Run("missing peer_id", func(t *testing.T) {
		body, _ := json.Marshal(map[string]interface{}{
			"peer_type": 1,
		})
		req, _ := http.NewRequest(http.MethodPost, "/sessions", bytes.NewReader(body))
		req.Header.Set("Authorization", token)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusBadRequest {
			t.Errorf("expected status 400, got %d", w.Code)
		}
	})

	t.Run("missing peer_type", func(t *testing.T) {
		body, _ := json.Marshal(map[string]interface{}{
			"peer_id": 12345,
		})
		req, _ := http.NewRequest(http.MethodPost, "/sessions", bytes.NewReader(body))
		req.Header.Set("Authorization", token)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusBadRequest {
			t.Errorf("expected status 400, got %d", w.Code)
		}
	})

	t.Run("reject non-friend human peer", func(t *testing.T) {
		body, _ := json.Marshal(createSessionReq{
			PeerID:   nonFriendID,
			PeerType: 1,
		})
		req, _ := http.NewRequest(http.MethodPost, "/sessions", bytes.NewReader(body))
		req.Header.Set("Authorization", token)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("expected status 400, got %d, body: %s", w.Code, w.Body.String())
		}
	})

	t.Run("create session with owned agent peer", func(t *testing.T) {
		body, _ := json.Marshal(createSessionReq{
			PeerID:   ownedAgentID,
			PeerType: 2,
		})
		req, _ := http.NewRequest(http.MethodPost, "/sessions", bytes.NewReader(body))
		req.Header.Set("Authorization", token)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected status 200, got %d, body: %s", w.Code, w.Body.String())
		}
	})

	t.Run("reject foreign agent peer", func(t *testing.T) {
		body, _ := json.Marshal(createSessionReq{
			PeerID:   foreignAgentID,
			PeerType: 2,
		})
		req, _ := http.NewRequest(http.MethodPost, "/sessions", bytes.NewReader(body))
		req.Header.Set("Authorization", token)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("expected status 400, got %d, body: %s", w.Code, w.Body.String())
		}
	})
}

func TestSessionOpenLatest(t *testing.T) {
	r, testDB, cleanup := setupSessionHandlerTest(t)
	defer cleanup()

	userID := int64(11031)
	token := createSessionTestUser(t, testDB, userID, "openlatestuser")
	peerID := int64(88888)
	foreignOwnerID := int64(88886)
	foreignAgentID := int64(88887)
	seedHandlerUser(t, testDB, peerID)
	seedHandlerUser(t, testDB, foreignOwnerID)
	seedHandlerAgent(t, testDB, foreignAgentID, foreignOwnerID, 1)
	seedHandlerFriendRelation(t, testDB, userID, peerID)

	t.Run("open latest creates then reuses", func(t *testing.T) {
		body, _ := json.Marshal(createSessionReq{
			PeerID:   peerID,
			PeerType: 1,
		})
		req, _ := http.NewRequest(http.MethodPost, "/sessions/open_latest", bytes.NewReader(body))
		req.Header.Set("Authorization", token)
		req.Header.Set("Content-Type", "application/json")

		w1 := httptest.NewRecorder()
		r.ServeHTTP(w1, req)
		if w1.Code != http.StatusOK {
			t.Fatalf("first open_latest expected 200, got %d, body: %s", w1.Code, w1.Body.String())
		}

		var resp1 map[string]interface{}
		_ = json.Unmarshal(w1.Body.Bytes(), &resp1)
		data1 := resp1["data"].(map[string]interface{})
		sid1 := data1["session_id"].(string)
		if sid1 == "" {
			t.Fatalf("first open_latest should return session_id")
		}
		if data1["is_new"] != true {
			t.Fatalf("first open_latest should return is_new=true")
		}

		req2, _ := http.NewRequest(http.MethodPost, "/sessions/open_latest", bytes.NewReader(body))
		req2.Header.Set("Authorization", token)
		req2.Header.Set("Content-Type", "application/json")

		w2 := httptest.NewRecorder()
		r.ServeHTTP(w2, req2)
		if w2.Code != http.StatusOK {
			t.Fatalf("second open_latest expected 200, got %d, body: %s", w2.Code, w2.Body.String())
		}

		var resp2 map[string]interface{}
		_ = json.Unmarshal(w2.Body.Bytes(), &resp2)
		data2 := resp2["data"].(map[string]interface{})
		sid2 := data2["session_id"].(string)
		if sid2 != sid1 {
			t.Fatalf("second open_latest should return same latest session_id: %s vs %s", sid1, sid2)
		}
		if data2["is_new"] != false {
			t.Fatalf("second open_latest should return is_new=false")
		}
	})

	t.Run("reject foreign agent peer", func(t *testing.T) {
		body, _ := json.Marshal(createSessionReq{
			PeerID:   foreignAgentID,
			PeerType: 2,
		})
		req, _ := http.NewRequest(http.MethodPost, "/sessions/open_latest", bytes.NewReader(body))
		req.Header.Set("Authorization", token)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("expected status 400, got %d, body: %s", w.Code, w.Body.String())
		}
	})
}

func TestSessionCreateGroup(t *testing.T) {
	r, testDB, cleanup := setupSessionHandlerTest(t)
	defer cleanup()

	userID := int64(11004)
	token := createSessionTestUser(t, testDB, userID, "creategroupuser")
	seedHandlerUser(t, testDB, 1001)
	seedHandlerUser(t, testDB, 1002)
	seedHandlerFriendRelation(t, testDB, userID, 1001)
	seedHandlerFriendRelation(t, testDB, userID, 1002)

	t.Run("create group", func(t *testing.T) {
		body, _ := json.Marshal(createGroupReq{
			Name:        "Test Group",
			MemberIDs:   []string{"1001", "1002"},
			MemberTypes: []int16{1, 1},
		})
		req, _ := http.NewRequest(http.MethodPost, "/sessions/group", bytes.NewReader(body))
		req.Header.Set("Authorization", token)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("expected status 200, got %d, body: %s", w.Code, w.Body.String())
		}

		var resp map[string]interface{}
		json.Unmarshal(w.Body.Bytes(), &resp)
		data := resp["data"].(map[string]interface{})
		if data["session_id"] == nil {
			t.Error("expected session_id in response")
		}
	})

	t.Run("missing name", func(t *testing.T) {
		body, _ := json.Marshal(map[string]interface{}{
			"member_ids":   []int64{1001},
			"member_types": []int16{1},
		})
		req, _ := http.NewRequest(http.MethodPost, "/sessions/group", bytes.NewReader(body))
		req.Header.Set("Authorization", token)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusBadRequest {
			t.Errorf("expected status 400, got %d", w.Code)
		}
	})

	t.Run("member_types length mismatch", func(t *testing.T) {
		body, _ := json.Marshal(createGroupReq{
			Name:        "Mismatch Group",
			MemberIDs:   []string{"1001", "1002"},
			MemberTypes: []int16{1},
		})
		req, _ := http.NewRequest(http.MethodPost, "/sessions/group", bytes.NewReader(body))
		req.Header.Set("Authorization", token)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusBadRequest {
			t.Errorf("expected status 400, got %d, body: %s", w.Code, w.Body.String())
		}
	})

	t.Run("rejects non-friend target", func(t *testing.T) {
		seedHandlerUser(t, testDB, 1010)
		body, _ := json.Marshal(createGroupReq{
			Name:        "Non Friend Group",
			MemberIDs:   []string{"1010"},
			MemberTypes: []int16{1},
		})
		req, _ := http.NewRequest(http.MethodPost, "/sessions/group", bytes.NewReader(body))
		req.Header.Set("Authorization", token)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusBadRequest {
			t.Errorf("expected status 400, got %d, body: %s", w.Code, w.Body.String())
		}
	})

	t.Run("rejects foreign agent target", func(t *testing.T) {
		seedHandlerUser(t, testDB, 1011)
		seedHandlerAgent(t, testDB, 2011, 1011, 1)
		body, _ := json.Marshal(createGroupReq{
			Name:        "Foreign Agent Group",
			MemberIDs:   []string{"2011"},
			MemberTypes: []int16{2},
		})
		req, _ := http.NewRequest(http.MethodPost, "/sessions/group", bytes.NewReader(body))
		req.Header.Set("Authorization", token)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusBadRequest {
			t.Errorf("expected status 400, got %d, body: %s", w.Code, w.Body.String())
		}
	})

	t.Run("rejects target user who disallows group invite", func(t *testing.T) {
		seedHandlerUser(t, testDB, 1012)
		seedHandlerFriendRelation(t, testDB, userID, 1012)
		if err := testDB.DB.Create(&model.UserSetting{
			UserID:           1012,
			FriendAddSetting: model.FriendAddSettingNeedApproval,
			AllowGroupInvite: true,
		}).Error; err != nil {
			t.Fatalf("seed blocked user setting error: %v", err)
		}
		if err := testDB.DB.Model(&model.UserSetting{}).
			Where("user_id = ?", 1012).
			Update("allow_group_invite", false).Error; err != nil {
			t.Fatalf("disable blocked user group invite error: %v", err)
		}

		body, _ := json.Marshal(createGroupReq{
			Name:        "Blocked Group",
			MemberIDs:   []string{"1012"},
			MemberTypes: []int16{1},
		})
		req, _ := http.NewRequest(http.MethodPost, "/sessions/group", bytes.NewReader(body))
		req.Header.Set("Authorization", token)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusForbidden {
			t.Fatalf("expected status 403, got %d, body: %s", w.Code, w.Body.String())
		}

		var resp map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("unmarshal response error: %v", err)
		}
		if code, ok := resp["code"].(float64); !ok || int(code) != 40033 {
			t.Fatalf("expected code=40033, got %#v", resp["code"])
		}
	})
}

func TestSessionDetail(t *testing.T) {
	r, testDB, cleanup := setupSessionHandlerTest(t)
	defer cleanup()

	userID := int64(11005)
	token := createSessionTestUser(t, testDB, userID, "detailuser")
	sessionID := "session-detail-handler-1"
	now := time.Now()

	session := model.Session{
		SessionID:      sessionID,
		OwnerID:        userID,
		SessionType:    2,
		GroupName:      "handler-detail-group",
		LastMsgSummary: "detail",
	}
	if err := testDB.DB.Create(&session).Error; err != nil {
		t.Fatalf("create session error: %v", err)
	}
	members := []model.SessionMember{
		{
			SessionID:    sessionID,
			MemberID:     userID,
			MemberType:   1,
			Role:         3,
			LastActiveAt: now,
			JoinedAt:     now,
		},
		{
			SessionID:    sessionID,
			MemberID:     11006,
			MemberType:   1,
			Role:         1,
			LastActiveAt: now,
			JoinedAt:     now,
		},
	}
	if err := testDB.DB.Create(&members).Error; err != nil {
		t.Fatalf("create members error: %v", err)
	}

	t.Run("returns session detail", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodGet, "/sessions/detail?session_id="+sessionID, nil)
		req.Header.Set("Authorization", token)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected status 200, got %d, body: %s", w.Code, w.Body.String())
		}

		var resp struct {
			Code int    `json:"code"`
			Msg  string `json:"msg"`
			Data struct {
				SessionID string `json:"session_id"`
				GroupName string `json:"group_name"`
			} `json:"data"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("unmarshal session detail response error: %v", err)
		}
		if resp.Code != 0 {
			t.Fatalf("expected code 0, got %d, msg: %s", resp.Code, resp.Msg)
		}
		if resp.Data.SessionID != sessionID {
			t.Fatalf("expected session_id=%q, got %q", sessionID, resp.Data.SessionID)
		}
		if resp.Data.GroupName != "handler-detail-group" {
			t.Fatalf("expected group_name=%q, got %q", "handler-detail-group", resp.Data.GroupName)
		}
	})

	t.Run("missing session_id", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodGet, "/sessions/detail", nil)
		req.Header.Set("Authorization", token)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("expected status 400, got %d", w.Code)
		}
	})
}

func TestSessionRename(t *testing.T) {
	r, testDB, cleanup := setupSessionHandlerTest(t)
	defer cleanup()

	userID := int64(11051)
	peerID := int64(11052)
	token := createSessionTestUser(t, testDB, userID, "renameuser")
	_ = createSessionTestUser(t, testDB, peerID, "renamepeer")
	sessionID := "session-rename-handler-1"
	now := time.Now()

	session := model.Session{
		SessionID:      sessionID,
		OwnerID:        userID,
		SessionType:    1,
		LastMsgSummary: "rename",
	}
	if err := testDB.DB.Create(&session).Error; err != nil {
		t.Fatalf("create session error: %v", err)
	}
	members := []model.SessionMember{
		{
			SessionID:    sessionID,
			MemberID:     userID,
			MemberType:   1,
			Role:         3,
			LastActiveAt: now,
			JoinedAt:     now,
		},
		{
			SessionID:    sessionID,
			MemberID:     peerID,
			MemberType:   1,
			Role:         1,
			LastActiveAt: now,
			JoinedAt:     now,
		},
	}
	if err := testDB.DB.Create(&members).Error; err != nil {
		t.Fatalf("create members error: %v", err)
	}

	t.Run("rename success", func(t *testing.T) {
		body, _ := json.Marshal(renameSessionReq{
			SessionID: sessionID,
			Title:     "  Topic 1 ",
		})
		req, _ := http.NewRequest(http.MethodPost, "/sessions/rename", bytes.NewReader(body))
		req.Header.Set("Authorization", token)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected status 200, got %d, body: %s", w.Code, w.Body.String())
		}

		var resp map[string]interface{}
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("unmarshal response error: %v", err)
		}
		data, _ := resp["data"].(map[string]interface{})
		if data["session_id"] != sessionID {
			t.Fatalf("expected session_id %s, got %#v", sessionID, data["session_id"])
		}
		if data["title"] != "Topic 1" {
			t.Fatalf("expected normalized title Topic 1, got %#v", data["title"])
		}
	})

	t.Run("missing session_id", func(t *testing.T) {
		body, _ := json.Marshal(renameSessionReq{
			Title: "x",
		})
		req, _ := http.NewRequest(http.MethodPost, "/sessions/rename", bytes.NewReader(body))
		req.Header.Set("Authorization", token)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("expected status 400, got %d, body: %s", w.Code, w.Body.String())
		}
	})

	t.Run("permission denied for non-member", func(t *testing.T) {
		outsiderToken := createSessionTestUser(t, testDB, 11999, "renameoutsider")
		body, _ := json.Marshal(renameSessionReq{
			SessionID: sessionID,
			Title:     "x",
		})
		req, _ := http.NewRequest(http.MethodPost, "/sessions/rename", bytes.NewReader(body))
		req.Header.Set("Authorization", outsiderToken)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusForbidden {
			t.Fatalf("expected status 403, got %d, body: %s", w.Code, w.Body.String())
		}
	})
}

func TestSessionSetPinned(t *testing.T) {
	r, testDB, cleanup := setupSessionHandlerTest(t)
	defer cleanup()

	userID := int64(11061)
	peerID := int64(11062)
	token := createSessionTestUser(t, testDB, userID, "pinuser")
	_ = createSessionTestUser(t, testDB, peerID, "pinpeer")
	sessionID := "session-pin-handler-1"
	now := time.Now()

	session := model.Session{
		SessionID:      sessionID,
		OwnerID:        userID,
		SessionType:    1,
		LastMsgSummary: "pin",
	}
	if err := testDB.DB.Create(&session).Error; err != nil {
		t.Fatalf("create session error: %v", err)
	}
	members := []model.SessionMember{
		{
			SessionID:    sessionID,
			MemberID:     userID,
			MemberType:   1,
			Role:         3,
			LastActiveAt: now,
			JoinedAt:     now,
		},
		{
			SessionID:    sessionID,
			MemberID:     peerID,
			MemberType:   1,
			Role:         1,
			LastActiveAt: now,
			JoinedAt:     now,
		},
	}
	if err := testDB.DB.Create(&members).Error; err != nil {
		t.Fatalf("create members error: %v", err)
	}

	t.Run("pin success", func(t *testing.T) {
		body, _ := json.Marshal(map[string]interface{}{
			"session_id": sessionID,
			"is_pinned":  true,
		})
		req, _ := http.NewRequest(http.MethodPost, "/sessions/pin", bytes.NewReader(body))
		req.Header.Set("Authorization", token)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected status 200, got %d, body: %s", w.Code, w.Body.String())
		}

		var resp map[string]interface{}
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("unmarshal response error: %v", err)
		}
		data, _ := resp["data"].(map[string]interface{})
		if data["session_id"] != sessionID {
			t.Fatalf("expected session_id %s, got %#v", sessionID, data["session_id"])
		}
		if pinned, _ := data["is_pinned"].(bool); !pinned {
			t.Fatalf("expected is_pinned=true, got %#v", data["is_pinned"])
		}
		if toInt64(data["pinned_at"]) <= 0 {
			t.Fatalf("expected pinned_at > 0, got %#v", data["pinned_at"])
		}
	})

	t.Run("missing session_id", func(t *testing.T) {
		body, _ := json.Marshal(map[string]interface{}{
			"is_pinned": true,
		})
		req, _ := http.NewRequest(http.MethodPost, "/sessions/pin", bytes.NewReader(body))
		req.Header.Set("Authorization", token)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("expected status 400, got %d, body: %s", w.Code, w.Body.String())
		}
	})

	t.Run("missing is_pinned", func(t *testing.T) {
		body, _ := json.Marshal(map[string]interface{}{
			"session_id": sessionID,
		})
		req, _ := http.NewRequest(http.MethodPost, "/sessions/pin", bytes.NewReader(body))
		req.Header.Set("Authorization", token)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("expected status 400, got %d, body: %s", w.Code, w.Body.String())
		}
	})

	t.Run("permission denied for non-member", func(t *testing.T) {
		outsiderToken := createSessionTestUser(t, testDB, 11998, "pinoutsider")
		body, _ := json.Marshal(map[string]interface{}{
			"session_id": sessionID,
			"is_pinned":  true,
		})
		req, _ := http.NewRequest(http.MethodPost, "/sessions/pin", bytes.NewReader(body))
		req.Header.Set("Authorization", outsiderToken)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusForbidden {
			t.Fatalf("expected status 403, got %d, body: %s", w.Code, w.Body.String())
		}
	})
}

func TestSessionSetMuted(t *testing.T) {
	r, testDB, cleanup := setupSessionHandlerTest(t)
	defer cleanup()

	userID := int64(11071)
	peerID := int64(11072)
	token := createSessionTestUser(t, testDB, userID, "muteuser")
	_ = createSessionTestUser(t, testDB, peerID, "mutepeer")
	sessionID := "session-mute-handler-1"
	now := time.Now()

	session := model.Session{
		SessionID:      sessionID,
		OwnerID:        userID,
		SessionType:    1,
		LastMsgSummary: "mute",
	}
	if err := testDB.DB.Create(&session).Error; err != nil {
		t.Fatalf("create session error: %v", err)
	}
	members := []model.SessionMember{
		{
			SessionID:    sessionID,
			MemberID:     userID,
			MemberType:   1,
			Role:         3,
			LastActiveAt: now,
			JoinedAt:     now,
		},
		{
			SessionID:    sessionID,
			MemberID:     peerID,
			MemberType:   1,
			Role:         1,
			LastActiveAt: now,
			JoinedAt:     now,
		},
	}
	if err := testDB.DB.Create(&members).Error; err != nil {
		t.Fatalf("create members error: %v", err)
	}

	t.Run("mute success", func(t *testing.T) {
		body, _ := json.Marshal(map[string]interface{}{
			"session_id": sessionID,
			"is_muted":   true,
		})
		req, _ := http.NewRequest(http.MethodPost, "/sessions/mute", bytes.NewReader(body))
		req.Header.Set("Authorization", token)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected status 200, got %d, body: %s", w.Code, w.Body.String())
		}

		var resp map[string]interface{}
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("unmarshal response error: %v", err)
		}
		data, _ := resp["data"].(map[string]interface{})
		if data["session_id"] != sessionID {
			t.Fatalf("expected session_id %s, got %#v", sessionID, data["session_id"])
		}
		if muted, _ := data["is_muted"].(bool); !muted {
			t.Fatalf("expected is_muted=true, got %#v", data["is_muted"])
		}
	})

	t.Run("missing session_id", func(t *testing.T) {
		body, _ := json.Marshal(map[string]interface{}{
			"is_muted": true,
		})
		req, _ := http.NewRequest(http.MethodPost, "/sessions/mute", bytes.NewReader(body))
		req.Header.Set("Authorization", token)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("expected status 400, got %d, body: %s", w.Code, w.Body.String())
		}
	})

	t.Run("missing is_muted", func(t *testing.T) {
		body, _ := json.Marshal(map[string]interface{}{
			"session_id": sessionID,
		})
		req, _ := http.NewRequest(http.MethodPost, "/sessions/mute", bytes.NewReader(body))
		req.Header.Set("Authorization", token)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("expected status 400, got %d, body: %s", w.Code, w.Body.String())
		}
	})

	t.Run("permission denied for non-member", func(t *testing.T) {
		outsiderToken := createSessionTestUser(t, testDB, 11997, "muteoutsider")
		body, _ := json.Marshal(map[string]interface{}{
			"session_id": sessionID,
			"is_muted":   true,
		})
		req, _ := http.NewRequest(http.MethodPost, "/sessions/mute", bytes.NewReader(body))
		req.Header.Set("Authorization", outsiderToken)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusForbidden {
			t.Fatalf("expected status 403, got %d, body: %s", w.Code, w.Body.String())
		}
	})
}

func TestSessionSetGroupNickname(t *testing.T) {
	r, testDB, cleanup := setupSessionHandlerTest(t)
	defer cleanup()

	userID := int64(11151)
	peerID := int64(11152)
	token := createSessionTestUser(t, testDB, userID, "groupnickuser")
	_ = createSessionTestUser(t, testDB, peerID, "groupnickpeer")
	sessionID := "session-group-nickname-handler-1"
	now := time.Now()

	session := model.Session{
		SessionID:      sessionID,
		OwnerID:        userID,
		SessionType:    2,
		LastMsgSummary: "group nickname",
	}
	if err := testDB.DB.Create(&session).Error; err != nil {
		t.Fatalf("create session error: %v", err)
	}
	members := []model.SessionMember{
		{
			SessionID:    sessionID,
			MemberID:     userID,
			MemberType:   1,
			Role:         3,
			LastActiveAt: now,
			JoinedAt:     now,
		},
		{
			SessionID:    sessionID,
			MemberID:     peerID,
			MemberType:   1,
			Role:         1,
			LastActiveAt: now,
			JoinedAt:     now,
		},
	}
	if err := testDB.DB.Create(&members).Error; err != nil {
		t.Fatalf("create members error: %v", err)
	}

	t.Run("set group nickname success", func(t *testing.T) {
		body, _ := json.Marshal(setGroupNicknameReq{
			SessionID: sessionID,
			Nickname:  "  Team   Lead  ",
		})
		req, _ := http.NewRequest(http.MethodPost, "/sessions/members/nickname", bytes.NewReader(body))
		req.Header.Set("Authorization", token)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected status 200, got %d, body: %s", w.Code, w.Body.String())
		}

		var resp map[string]interface{}
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("unmarshal response error: %v", err)
		}
		data, _ := resp["data"].(map[string]interface{})
		if data["session_id"] != sessionID {
			t.Fatalf("expected session_id %s, got %#v", sessionID, data["session_id"])
		}
		if data["group_nickname"] != "Team Lead" {
			t.Fatalf("expected normalized group nickname Team Lead, got %#v", data["group_nickname"])
		}
	})

	t.Run("missing session_id", func(t *testing.T) {
		body, _ := json.Marshal(setGroupNicknameReq{
			Nickname: "x",
		})
		req, _ := http.NewRequest(http.MethodPost, "/sessions/members/nickname", bytes.NewReader(body))
		req.Header.Set("Authorization", token)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("expected status 400, got %d, body: %s", w.Code, w.Body.String())
		}
	})

	t.Run("permission denied for non-member", func(t *testing.T) {
		outsiderToken := createSessionTestUser(t, testDB, 11998, "groupnickoutsider")
		body, _ := json.Marshal(setGroupNicknameReq{
			SessionID: sessionID,
			Nickname:  "x",
		})
		req, _ := http.NewRequest(http.MethodPost, "/sessions/members/nickname", bytes.NewReader(body))
		req.Header.Set("Authorization", outsiderToken)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusForbidden {
			t.Fatalf("expected status 403, got %d, body: %s", w.Code, w.Body.String())
		}
	})

	t.Run("reject private session", func(t *testing.T) {
		privateSessionID := "session-group-nickname-handler-private"
		privateSession := model.Session{
			SessionID:      privateSessionID,
			OwnerID:        userID,
			SessionType:    1,
			LastMsgSummary: "private",
		}
		if err := testDB.DB.Create(&privateSession).Error; err != nil {
			t.Fatalf("create private session error: %v", err)
		}
		privateMembers := []model.SessionMember{
			{
				SessionID:    privateSessionID,
				MemberID:     userID,
				MemberType:   1,
				Role:         3,
				LastActiveAt: now,
				JoinedAt:     now,
			},
			{
				SessionID:    privateSessionID,
				MemberID:     peerID,
				MemberType:   1,
				Role:         1,
				LastActiveAt: now,
				JoinedAt:     now,
			},
		}
		if err := testDB.DB.Create(&privateMembers).Error; err != nil {
			t.Fatalf("create private members error: %v", err)
		}

		body, _ := json.Marshal(setGroupNicknameReq{
			SessionID: privateSessionID,
			Nickname:  "x",
		})
		req, _ := http.NewRequest(http.MethodPost, "/sessions/members/nickname", bytes.NewReader(body))
		req.Header.Set("Authorization", token)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("expected status 400, got %d, body: %s", w.Code, w.Body.String())
		}
	})
}

func TestSessionAddMembers(t *testing.T) {
	r, testDB, cleanup := setupSessionHandlerTest(t)
	defer cleanup()

	userID := int64(11007)
	token := createSessionTestUser(t, testDB, userID, "addmemberuser")
	seedHandlerUser(t, testDB, 12001)
	seedHandlerUser(t, testDB, 12002)
	seedHandlerFriendRelation(t, testDB, userID, 12001)
	seedHandlerFriendRelation(t, testDB, userID, 12002)
	sessionID := "session-add-members-handler-1"
	now := time.Now()

	session := model.Session{
		SessionID:         sessionID,
		OwnerID:           userID,
		SessionType:       2,
		AllowMemberInvite: true,
		LastMsgSummary:    "group",
	}
	if err := testDB.DB.Create(&session).Error; err != nil {
		t.Fatalf("create session error: %v", err)
	}
	ownerMember := model.SessionMember{
		SessionID:    sessionID,
		MemberID:     userID,
		MemberType:   1,
		Role:         3,
		LastActiveAt: now,
		JoinedAt:     now,
	}
	if err := testDB.DB.Create(&ownerMember).Error; err != nil {
		t.Fatalf("create owner member error: %v", err)
	}

	t.Run("adds members", func(t *testing.T) {
		body, _ := json.Marshal(addMembersReq{
			SessionID:   sessionID,
			MemberIDs:   []string{"12001", "12002"},
			MemberTypes: []int16{1, 1},
		})
		req, _ := http.NewRequest(http.MethodPost, "/sessions/members/add", bytes.NewReader(body))
		req.Header.Set("Authorization", token)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected status 200, got %d, body: %s", w.Code, w.Body.String())
		}
	})

	t.Run("rejects missing member ids", func(t *testing.T) {
		body, _ := json.Marshal(map[string]interface{}{
			"session_id": sessionID,
		})
		req, _ := http.NewRequest(http.MethodPost, "/sessions/members/add", bytes.NewReader(body))
		req.Header.Set("Authorization", token)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("expected status 400, got %d", w.Code)
		}
	})

	t.Run("allows normal member role", func(t *testing.T) {
		memberUserID := int64(11008)
		memberToken := createSessionTestUser(t, testDB, memberUserID, "normalmember")
		targetUserID := int64(12003)
		seedHandlerUser(t, testDB, targetUserID)
		seedHandlerFriendRelation(t, testDB, memberUserID, targetUserID)
		member := model.SessionMember{
			SessionID:    sessionID,
			MemberID:     memberUserID,
			MemberType:   1,
			Role:         1,
			LastActiveAt: now,
			JoinedAt:     now,
		}
		if err := testDB.DB.Create(&member).Error; err != nil {
			t.Fatalf("create normal member error: %v", err)
		}

		body, _ := json.Marshal(addMembersReq{
			SessionID:   sessionID,
			MemberIDs:   []string{"12003"},
			MemberTypes: []int16{1},
		})
		req, _ := http.NewRequest(http.MethodPost, "/sessions/members/add", bytes.NewReader(body))
		req.Header.Set("Authorization", memberToken)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected status 200, got %d, body: %s", w.Code, w.Body.String())
		}
	})

	t.Run("rejects normal member when member invite is disabled", func(t *testing.T) {
		memberUserID := int64(11009)
		memberToken := createSessionTestUser(t, testDB, memberUserID, "disabledmember")
		targetUserID := int64(12004)
		seedHandlerUser(t, testDB, targetUserID)
		seedHandlerFriendRelation(t, testDB, memberUserID, targetUserID)

		disabledSessionID := "session-add-members-handler-disabled"
		disabledSession := model.Session{
			SessionID:         disabledSessionID,
			OwnerID:           userID,
			SessionType:       2,
			AllowMemberInvite: true,
			LastMsgSummary:    "group",
		}
		if err := testDB.DB.Create(&disabledSession).Error; err != nil {
			t.Fatalf("create disabled session error: %v", err)
		}
		if err := testDB.DB.Model(&model.Session{}).
			Where("session_id = ?", disabledSessionID).
			Update("allow_member_invite", false).Error; err != nil {
			t.Fatalf("disable member invite error: %v", err)
		}
		disabledMembers := []model.SessionMember{
			{
				SessionID:    disabledSessionID,
				MemberID:     userID,
				MemberType:   1,
				Role:         3,
				LastActiveAt: now,
				JoinedAt:     now,
			},
			{
				SessionID:    disabledSessionID,
				MemberID:     memberUserID,
				MemberType:   1,
				Role:         1,
				LastActiveAt: now,
				JoinedAt:     now,
			},
		}
		if err := testDB.DB.Create(&disabledMembers).Error; err != nil {
			t.Fatalf("create disabled members error: %v", err)
		}

		body, _ := json.Marshal(addMembersReq{
			SessionID:   disabledSessionID,
			MemberIDs:   []string{"12004"},
			MemberTypes: []int16{1},
		})
		req, _ := http.NewRequest(http.MethodPost, "/sessions/members/add", bytes.NewReader(body))
		req.Header.Set("Authorization", memberToken)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusForbidden {
			t.Fatalf("expected status 403, got %d, body: %s", w.Code, w.Body.String())
		}
	})

	t.Run("rejects normal member when group size exceeds threshold", func(t *testing.T) {
		memberUserID := int64(11010)
		memberToken := createSessionTestUser(t, testDB, memberUserID, "thresholdmember")
		targetUserID := int64(12005)
		seedHandlerUser(t, testDB, targetUserID)
		seedHandlerUser(t, testDB, 12006)
		seedHandlerFriendRelation(t, testDB, memberUserID, targetUserID)
		if err := systemsetting.SaveGroupSettings(systemsetting.GroupSettings{
			MemberInviteThreshold: 2,
		}, nil); err != nil {
			t.Fatalf("save group settings error: %v", err)
		}
		t.Cleanup(func() {
			systemsetting.InvalidateGroupSettingsCache()
			if err := systemsetting.SaveGroupSettings(systemsetting.DefaultGroupSettings(), nil); err != nil {
				t.Fatalf("restore group settings error: %v", err)
			}
		})

		thresholdSessionID := "session-add-members-handler-threshold"
		thresholdSession := model.Session{
			SessionID:         thresholdSessionID,
			OwnerID:           userID,
			SessionType:       2,
			AllowMemberInvite: true,
			LastMsgSummary:    "group",
		}
		if err := testDB.DB.Create(&thresholdSession).Error; err != nil {
			t.Fatalf("create threshold session error: %v", err)
		}
		thresholdMembers := []model.SessionMember{
			{
				SessionID:    thresholdSessionID,
				MemberID:     userID,
				MemberType:   1,
				Role:         3,
				LastActiveAt: now,
				JoinedAt:     now,
			},
			{
				SessionID:    thresholdSessionID,
				MemberID:     memberUserID,
				MemberType:   1,
				Role:         1,
				LastActiveAt: now,
				JoinedAt:     now,
			},
			{
				SessionID:    thresholdSessionID,
				MemberID:     12006,
				MemberType:   1,
				Role:         1,
				LastActiveAt: now,
				JoinedAt:     now,
			},
		}
		if err := testDB.DB.Create(&thresholdMembers).Error; err != nil {
			t.Fatalf("create threshold members error: %v", err)
		}

		body, _ := json.Marshal(addMembersReq{
			SessionID:   thresholdSessionID,
			MemberIDs:   []string{"12005"},
			MemberTypes: []int16{1},
		})
		req, _ := http.NewRequest(http.MethodPost, "/sessions/members/add", bytes.NewReader(body))
		req.Header.Set("Authorization", memberToken)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusForbidden {
			t.Fatalf("expected status 403, got %d, body: %s", w.Code, w.Body.String())
		}
	})

	t.Run("rejects non-friend target", func(t *testing.T) {
		seedHandlerUser(t, testDB, 13005)
		body, _ := json.Marshal(addMembersReq{
			SessionID:   sessionID,
			MemberIDs:   []string{"13005"},
			MemberTypes: []int16{1},
		})
		req, _ := http.NewRequest(http.MethodPost, "/sessions/members/add", bytes.NewReader(body))
		req.Header.Set("Authorization", token)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("expected status 400, got %d, body: %s", w.Code, w.Body.String())
		}
	})

	t.Run("rejects target user who disallows group invite", func(t *testing.T) {
		seedHandlerUser(t, testDB, 13006)
		seedHandlerFriendRelation(t, testDB, userID, 13006)
		if err := testDB.DB.Create(&model.UserSetting{
			UserID:           13006,
			FriendAddSetting: model.FriendAddSettingNeedApproval,
			AllowGroupInvite: true,
		}).Error; err != nil {
			t.Fatalf("seed blocked user setting error: %v", err)
		}
		if err := testDB.DB.Model(&model.UserSetting{}).
			Where("user_id = ?", 13006).
			Update("allow_group_invite", false).Error; err != nil {
			t.Fatalf("disable blocked user group invite error: %v", err)
		}

		body, _ := json.Marshal(addMembersReq{
			SessionID:   sessionID,
			MemberIDs:   []string{"13006"},
			MemberTypes: []int16{1},
		})
		req, _ := http.NewRequest(http.MethodPost, "/sessions/members/add", bytes.NewReader(body))
		req.Header.Set("Authorization", token)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusForbidden {
			t.Fatalf("expected status 403, got %d, body: %s", w.Code, w.Body.String())
		}

		var resp map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("unmarshal response error: %v", err)
		}
		if code, ok := resp["code"].(float64); !ok || int(code) != 40033 {
			t.Fatalf("expected code=40033, got %#v", resp["code"])
		}
	})

	t.Run("rejects private session", func(t *testing.T) {
		privateID := "session-add-members-private-handler"
		privateSession := model.Session{
			SessionID:      privateID,
			OwnerID:        userID,
			SessionType:    1,
			LastMsgSummary: "private",
		}
		if err := testDB.DB.Create(&privateSession).Error; err != nil {
			t.Fatalf("create private session error: %v", err)
		}
		privateOwner := model.SessionMember{
			SessionID:    privateID,
			MemberID:     userID,
			MemberType:   1,
			Role:         3,
			LastActiveAt: now,
			JoinedAt:     now,
		}
		if err := testDB.DB.Create(&privateOwner).Error; err != nil {
			t.Fatalf("create private owner member error: %v", err)
		}

		body, _ := json.Marshal(addMembersReq{
			SessionID:   privateID,
			MemberIDs:   []string{"13001"},
			MemberTypes: []int16{1},
		})
		req, _ := http.NewRequest(http.MethodPost, "/sessions/members/add", bytes.NewReader(body))
		req.Header.Set("Authorization", token)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("expected status 400, got %d, body: %s", w.Code, w.Body.String())
		}
	})
}

func TestSessionUpdateInviteSetting(t *testing.T) {
	r, testDB, cleanup := setupSessionHandlerTest(t)
	defer cleanup()

	ownerID := int64(13001)
	adminID := int64(13002)
	memberID := int64(13003)
	sessionID := "session-invite-setting-handler-1"
	now := time.Now()

	ownerToken := createSessionTestUser(t, testDB, ownerID, "inviteowner")
	adminToken := createSessionTestUser(t, testDB, adminID, "inviteadmin")
	memberToken := createSessionTestUser(t, testDB, memberID, "invitemember")

	session := model.Session{
		SessionID:         sessionID,
		OwnerID:           ownerID,
		SessionType:       2,
		AllowMemberInvite: true,
		LastMsgSummary:    "group",
	}
	if err := testDB.DB.Create(&session).Error; err != nil {
		t.Fatalf("create session error: %v", err)
	}
	members := []model.SessionMember{
		{SessionID: sessionID, MemberID: ownerID, MemberType: 1, Role: 3, LastActiveAt: now, JoinedAt: now},
		{SessionID: sessionID, MemberID: adminID, MemberType: 1, Role: 2, LastActiveAt: now, JoinedAt: now},
		{SessionID: sessionID, MemberID: memberID, MemberType: 1, Role: 1, LastActiveAt: now, JoinedAt: now},
	}
	if err := testDB.DB.Create(&members).Error; err != nil {
		t.Fatalf("create members error: %v", err)
	}

	t.Run("admin can disable member invite", func(t *testing.T) {
		body, _ := json.Marshal(updateInviteSettingReq{
			SessionID:         sessionID,
			AllowMemberInvite: boolPtr(false),
		})
		req, _ := http.NewRequest(http.MethodPost, "/sessions/members/invite_setting", bytes.NewReader(body))
		req.Header.Set("Authorization", adminToken)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected status 200, got %d, body: %s", w.Code, w.Body.String())
		}

		var updated model.Session
		if err := testDB.DB.Select("allow_member_invite").First(&updated, "session_id = ?", sessionID).Error; err != nil {
			t.Fatalf("load updated session error: %v", err)
		}
		if updated.AllowMemberInvite {
			t.Fatal("expected allow_member_invite=false")
		}
	})

	t.Run("normal member cannot update invite setting", func(t *testing.T) {
		body, _ := json.Marshal(updateInviteSettingReq{
			SessionID:         sessionID,
			AllowMemberInvite: boolPtr(true),
		})
		req, _ := http.NewRequest(http.MethodPost, "/sessions/members/invite_setting", bytes.NewReader(body))
		req.Header.Set("Authorization", memberToken)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusForbidden {
			t.Fatalf("expected status 403, got %d, body: %s", w.Code, w.Body.String())
		}
	})

	t.Run("owner can enable member invite", func(t *testing.T) {
		body, _ := json.Marshal(updateInviteSettingReq{
			SessionID:         sessionID,
			AllowMemberInvite: boolPtr(true),
		})
		req, _ := http.NewRequest(http.MethodPost, "/sessions/members/invite_setting", bytes.NewReader(body))
		req.Header.Set("Authorization", ownerToken)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected status 200, got %d, body: %s", w.Code, w.Body.String())
		}
	})
}

func boolPtr(value bool) *bool {
	return &value
}

func TestSessionRemoveMembers(t *testing.T) {
	r, testDB, cleanup := setupSessionHandlerTest(t)
	defer cleanup()

	ownerID := int64(11101)
	adminID := int64(11102)
	memberID := int64(11103)
	sessionID := "session-remove-members-handler-1"
	now := time.Now()

	ownerToken := createSessionTestUser(t, testDB, ownerID, "removeowner")
	adminToken := createSessionTestUser(t, testDB, adminID, "removeadmin")
	_ = createSessionTestUser(t, testDB, memberID, "removemember")

	session := model.Session{
		SessionID:      sessionID,
		OwnerID:        ownerID,
		SessionType:    2,
		LastMsgSummary: "group",
	}
	if err := testDB.DB.Create(&session).Error; err != nil {
		t.Fatalf("create session error: %v", err)
	}
	members := []model.SessionMember{
		{
			SessionID:    sessionID,
			MemberID:     ownerID,
			MemberType:   1,
			Role:         3,
			LastActiveAt: now,
			JoinedAt:     now,
		},
		{
			SessionID:    sessionID,
			MemberID:     adminID,
			MemberType:   1,
			Role:         2,
			LastActiveAt: now,
			JoinedAt:     now,
		},
		{
			SessionID:    sessionID,
			MemberID:     memberID,
			MemberType:   1,
			Role:         1,
			LastActiveAt: now,
			JoinedAt:     now,
		},
	}
	if err := testDB.DB.Create(&members).Error; err != nil {
		t.Fatalf("create members error: %v", err)
	}

	t.Run("owner removes normal member", func(t *testing.T) {
		body, _ := json.Marshal(removeMembersReq{
			SessionID:   sessionID,
			MemberIDs:   []string{strconv.FormatInt(memberID, 10)},
			MemberTypes: []int16{1},
		})
		req, _ := http.NewRequest(http.MethodPost, "/sessions/members/remove", bytes.NewReader(body))
		req.Header.Set("Authorization", ownerToken)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected status 200, got %d, body: %s", w.Code, w.Body.String())
		}
	})

	t.Run("admin cannot remove owner", func(t *testing.T) {
		body, _ := json.Marshal(removeMembersReq{
			SessionID:   sessionID,
			MemberIDs:   []string{strconv.FormatInt(ownerID, 10)},
			MemberTypes: []int16{1},
		})
		req, _ := http.NewRequest(http.MethodPost, "/sessions/members/remove", bytes.NewReader(body))
		req.Header.Set("Authorization", adminToken)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("expected status 400, got %d, body: %s", w.Code, w.Body.String())
		}
	})
}

func TestSessionLeave(t *testing.T) {
	r, testDB, cleanup := setupSessionHandlerTest(t)
	defer cleanup()

	ownerID := int64(11111)
	memberID := int64(11112)
	sessionID := "session-leave-handler-1"
	now := time.Now()

	ownerToken := createSessionTestUser(t, testDB, ownerID, "leaveowner")
	memberToken := createSessionTestUser(t, testDB, memberID, "leavemember")

	session := model.Session{
		SessionID:      sessionID,
		OwnerID:        ownerID,
		SessionType:    2,
		LastMsgSummary: "group",
	}
	if err := testDB.DB.Create(&session).Error; err != nil {
		t.Fatalf("create session error: %v", err)
	}
	members := []model.SessionMember{
		{
			SessionID:    sessionID,
			MemberID:     ownerID,
			MemberType:   1,
			Role:         3,
			LastActiveAt: now,
			JoinedAt:     now,
		},
		{
			SessionID:    sessionID,
			MemberID:     memberID,
			MemberType:   1,
			Role:         1,
			LastActiveAt: now,
			JoinedAt:     now,
		},
	}
	if err := testDB.DB.Create(&members).Error; err != nil {
		t.Fatalf("create members error: %v", err)
	}

	delegateKey := fmt.Sprintf("im:delegate:%s:%d", sessionID, memberID)
	streakKey := fmt.Sprintf("im:delegate:streak:%s:%d", sessionID, memberID)
	if err := store.RDB.HSet(context.Background(), delegateKey, "agent_id", 9301).Err(); err != nil {
		t.Fatalf("seed delegate key error: %v", err)
	}
	if err := store.RDB.Set(context.Background(), streakKey, "2", time.Minute).Err(); err != nil {
		t.Fatalf("seed delegate streak error: %v", err)
	}

	t.Run("member can leave group", func(t *testing.T) {
		body, _ := json.Marshal(leaveGroupReq{SessionID: sessionID})
		req, _ := http.NewRequest(http.MethodPost, "/sessions/leave", bytes.NewReader(body))
		req.Header.Set("Authorization", memberToken)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected status 200, got %d, body: %s", w.Code, w.Body.String())
		}

		var count int64
		if err := testDB.DB.Model(&model.SessionMember{}).
			Where("session_id = ? AND member_id = ? AND member_type = 1", sessionID, memberID).
			Count(&count).Error; err != nil {
			t.Fatalf("count left member error: %v", err)
		}
		if count != 0 {
			t.Fatalf("expected member removed, got count=%d", count)
		}

		exists, err := store.RDB.Exists(context.Background(), delegateKey, streakKey).Result()
		if err != nil {
			t.Fatalf("check delegate state error: %v", err)
		}
		if exists != 0 {
			t.Fatalf("expected delegate state cleared, exists=%d", exists)
		}
	})

	t.Run("owner cannot leave group", func(t *testing.T) {
		body, _ := json.Marshal(leaveGroupReq{SessionID: sessionID})
		req, _ := http.NewRequest(http.MethodPost, "/sessions/leave", bytes.NewReader(body))
		req.Header.Set("Authorization", ownerToken)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusForbidden {
			t.Fatalf("expected status 403, got %d, body: %s", w.Code, w.Body.String())
		}
	})
}

func TestSessionUpdateMemberRole(t *testing.T) {
	r, testDB, cleanup := setupSessionHandlerTest(t)
	defer cleanup()

	ownerID := int64(11201)
	adminID := int64(11202)
	memberID := int64(11203)
	sessionID := "session-update-role-handler-1"
	now := time.Now()

	ownerToken := createSessionTestUser(t, testDB, ownerID, "roleowner")
	adminToken := createSessionTestUser(t, testDB, adminID, "roleadmin")
	_ = createSessionTestUser(t, testDB, memberID, "rolemember")

	session := model.Session{
		SessionID:      sessionID,
		OwnerID:        ownerID,
		SessionType:    2,
		LastMsgSummary: "group",
	}
	if err := testDB.DB.Create(&session).Error; err != nil {
		t.Fatalf("create session error: %v", err)
	}
	members := []model.SessionMember{
		{
			SessionID:    sessionID,
			MemberID:     ownerID,
			MemberType:   1,
			Role:         3,
			LastActiveAt: now,
			JoinedAt:     now,
		},
		{
			SessionID:    sessionID,
			MemberID:     adminID,
			MemberType:   1,
			Role:         2,
			LastActiveAt: now,
			JoinedAt:     now,
		},
		{
			SessionID:    sessionID,
			MemberID:     memberID,
			MemberType:   1,
			Role:         1,
			LastActiveAt: now,
			JoinedAt:     now,
		},
	}
	if err := testDB.DB.Create(&members).Error; err != nil {
		t.Fatalf("create members error: %v", err)
	}

	t.Run("owner can promote member", func(t *testing.T) {
		body, _ := json.Marshal(updateMemberRoleReq{
			SessionID: sessionID,
			MemberID:  strconv.FormatInt(memberID, 10),
			Role:      2,
		})
		req, _ := http.NewRequest(http.MethodPost, "/sessions/members/role", bytes.NewReader(body))
		req.Header.Set("Authorization", ownerToken)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected status 200, got %d, body: %s", w.Code, w.Body.String())
		}
	})

	t.Run("admin cannot update role", func(t *testing.T) {
		body, _ := json.Marshal(updateMemberRoleReq{
			SessionID: sessionID,
			MemberID:  strconv.FormatInt(memberID, 10),
			Role:      1,
		})
		req, _ := http.NewRequest(http.MethodPost, "/sessions/members/role", bytes.NewReader(body))
		req.Header.Set("Authorization", adminToken)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusForbidden {
			t.Fatalf("expected status 403, got %d, body: %s", w.Code, w.Body.String())
		}
	})
}

func TestSessionTransferOwner(t *testing.T) {
	r, testDB, cleanup := setupSessionHandlerTest(t)
	defer cleanup()

	ownerID := int64(11301)
	targetID := int64(11302)
	adminID := int64(11303)
	sessionID := "session-transfer-owner-handler-1"
	now := time.Now()

	ownerToken := createSessionTestUser(t, testDB, ownerID, "transferowner")
	_ = createSessionTestUser(t, testDB, targetID, "transfertarget")
	adminToken := createSessionTestUser(t, testDB, adminID, "transferadmin")

	session := model.Session{
		SessionID:      sessionID,
		OwnerID:        ownerID,
		SessionType:    2,
		LastMsgSummary: "group",
	}
	if err := testDB.DB.Create(&session).Error; err != nil {
		t.Fatalf("create session error: %v", err)
	}
	members := []model.SessionMember{
		{
			SessionID:    sessionID,
			MemberID:     ownerID,
			MemberType:   1,
			Role:         3,
			LastActiveAt: now,
			JoinedAt:     now,
		},
		{
			SessionID:    sessionID,
			MemberID:     targetID,
			MemberType:   1,
			Role:         1,
			LastActiveAt: now,
			JoinedAt:     now,
		},
		{
			SessionID:    sessionID,
			MemberID:     adminID,
			MemberType:   1,
			Role:         2,
			LastActiveAt: now,
			JoinedAt:     now,
		},
	}
	if err := testDB.DB.Create(&members).Error; err != nil {
		t.Fatalf("create members error: %v", err)
	}

	t.Run("owner can transfer", func(t *testing.T) {
		body, _ := json.Marshal(transferOwnerReq{
			SessionID: sessionID,
			MemberID:  strconv.FormatInt(targetID, 10),
		})
		req, _ := http.NewRequest(http.MethodPost, "/sessions/owner/transfer", bytes.NewReader(body))
		req.Header.Set("Authorization", ownerToken)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected status 200, got %d, body: %s", w.Code, w.Body.String())
		}
	})

	t.Run("admin cannot transfer", func(t *testing.T) {
		body, _ := json.Marshal(transferOwnerReq{
			SessionID: sessionID,
			MemberID:  strconv.FormatInt(ownerID, 10),
		})
		req, _ := http.NewRequest(http.MethodPost, "/sessions/owner/transfer", bytes.NewReader(body))
		req.Header.Set("Authorization", adminToken)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusForbidden {
			t.Fatalf("expected status 403, got %d, body: %s", w.Code, w.Body.String())
		}
	})
}

func TestSessionDissolve(t *testing.T) {
	r, testDB, cleanup := setupSessionHandlerTest(t)
	defer cleanup()

	ownerID := int64(11401)
	memberID := int64(11402)
	sessionID := "session-dissolve-handler-1"
	now := time.Now()

	ownerToken := createSessionTestUser(t, testDB, ownerID, "dissolveowner")
	memberToken := createSessionTestUser(t, testDB, memberID, "dissolvemember")

	session := model.Session{
		SessionID:      sessionID,
		OwnerID:        ownerID,
		SessionType:    2,
		LastMsgSummary: "group",
	}
	if err := testDB.DB.Create(&session).Error; err != nil {
		t.Fatalf("create session error: %v", err)
	}
	members := []model.SessionMember{
		{
			SessionID:    sessionID,
			MemberID:     ownerID,
			MemberType:   1,
			Role:         3,
			LastActiveAt: now,
			JoinedAt:     now,
		},
		{
			SessionID:    sessionID,
			MemberID:     memberID,
			MemberType:   1,
			Role:         1,
			LastActiveAt: now,
			JoinedAt:     now,
		},
	}
	if err := testDB.DB.Create(&members).Error; err != nil {
		t.Fatalf("create members error: %v", err)
	}

	t.Run("owner can dissolve group", func(t *testing.T) {
		body, _ := json.Marshal(dissolveGroupReq{SessionID: sessionID})
		req, _ := http.NewRequest(http.MethodPost, "/sessions/dissolve", bytes.NewReader(body))
		req.Header.Set("Authorization", ownerToken)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected status 200, got %d, body: %s", w.Code, w.Body.String())
		}

		var sess model.Session
		if err := testDB.DB.Where("session_id = ?", sessionID).First(&sess).Error; err != nil {
			t.Fatalf("query session error: %v", err)
		}
		if !sess.IsDeleted {
			t.Fatalf("expected session is_deleted=true")
		}

		var memberCount int64
		if err := testDB.DB.Model(&model.SessionMember{}).
			Where("session_id = ?", sessionID).
			Count(&memberCount).Error; err != nil {
			t.Fatalf("count members error: %v", err)
		}
		if memberCount != 0 {
			t.Fatalf("expected no members after dissolve, got %d", memberCount)
		}
	})

	t.Run("non-owner cannot dissolve group", func(t *testing.T) {
		session2 := model.Session{
			SessionID:      "session-dissolve-handler-2",
			OwnerID:        ownerID,
			SessionType:    2,
			LastMsgSummary: "group2",
		}
		if err := testDB.DB.Create(&session2).Error; err != nil {
			t.Fatalf("create session2 error: %v", err)
		}
		members2 := []model.SessionMember{
			{
				SessionID:    session2.SessionID,
				MemberID:     ownerID,
				MemberType:   1,
				Role:         3,
				LastActiveAt: now,
				JoinedAt:     now,
			},
			{
				SessionID:    session2.SessionID,
				MemberID:     memberID,
				MemberType:   1,
				Role:         1,
				LastActiveAt: now,
				JoinedAt:     now,
			},
		}
		if err := testDB.DB.Create(&members2).Error; err != nil {
			t.Fatalf("create members2 error: %v", err)
		}

		body, _ := json.Marshal(dissolveGroupReq{SessionID: session2.SessionID})
		req, _ := http.NewRequest(http.MethodPost, "/sessions/dissolve", bytes.NewReader(body))
		req.Header.Set("Authorization", memberToken)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusForbidden {
			t.Fatalf("expected status 403, got %d, body: %s", w.Code, w.Body.String())
		}
	})
}
