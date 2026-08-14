package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/agentapi"
	"github.com/askie/grix/backend/internal/pkg/agentscope"
	"github.com/askie/grix/backend/internal/pkg/middleware"
	"github.com/askie/grix/backend/internal/pkg/testutil"
	"github.com/askie/grix/backend/internal/store"
	"github.com/gin-gonic/gin"
)

func setupAgentSessionHandlerTest(t *testing.T) (*gin.Engine, *testutil.TestDB, func()) {
	t.Helper()

	testDB := testutil.NewTestDB()
	store.DB = testDB.DB
	store.RDB = testutil.NewMockRedis()

	r := gin.New()
	r.Use(middleware.AgentAPIAuth())
	r.POST("/agent-api/sessions/create", AgentSessionCreate)
	r.POST("/agent-api/sessions/create_group", middleware.AgentAPIScope(agentscope.ScopeGroupCreate), AgentSessionCreateGroup)
	r.POST("/agent-api/sessions/open_latest", AgentSessionOpenLatest)
	r.POST("/agent-api/sessions/leave", AgentSessionLeave)
	r.GET("/agent-api/sessions/search", middleware.AgentAPIScope(agentscope.ScopeSessionSearch), AgentSessionSearch)
	r.GET("/agent-api/sessions/group/detail", AgentSessionGroupDetail)
	r.POST("/agent-api/sessions/members/add", AgentSessionAddMembers)
	r.POST("/agent-api/sessions/speaking/all_muted", AgentSessionUpdateAllMembersMuted)
	r.POST("/agent-api/sessions/members/speaking", AgentSessionUpdateMemberSpeaking)

	return r, testDB, func() { testDB.Close() }
}

func seedAgentAPIAuthData(t *testing.T, db *testutil.TestDB, ownerID, agentID int64, apiKey string) {
	t.Helper()

	owner := model.User{
		ID:           ownerID,
		Username:     "agent_owner",
		Email:        "agent_owner@example.com",
		PasswordHash: "x",
		AuthProvider: "local",
		Nickname:     "Owner",
	}
	if err := db.DB.Create(&owner).Error; err != nil {
		t.Fatalf("seed owner error: %v", err)
	}

	agent := model.Agent{
		ID:           agentID,
		AgentName:    "api_agent",
		OwnerID:      ownerID,
		ProviderType: model.AgentProviderAPI,
		Status:       1,
		APIKeyHash:   agentapi.HashAPIKey(apiKey),
		APIKeyHint:   agentapi.APIKeyHint(apiKey),
	}
	if err := db.DB.Create(&agent).Error; err != nil {
		t.Fatalf("seed agent error: %v", err)
	}
}

func seedAgentSessionScope(t *testing.T, db *testutil.TestDB, agentID int64, scope string) {
	t.Helper()

	if err := db.DB.Create(&model.AgentAPIScope{
		AgentID:   agentID,
		Scope:     scope,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}).Error; err != nil {
		t.Fatalf("seed agent scope error: %v", err)
	}
}

func seedAgentSessionFriendRelation(t *testing.T, db *testutil.TestDB, ownerID, peerID int64) {
	t.Helper()

	peer := model.User{
		ID:           peerID,
		Username:     "agent_peer",
		Email:        "agent_peer@example.com",
		PasswordHash: "x",
		AuthProvider: "local",
		Nickname:     "Peer",
	}
	if err := db.DB.Create(&peer).Error; err != nil {
		t.Fatalf("seed peer error: %v", err)
	}

	friend := model.Friend{
		ID:       ownerID + peerID + 1,
		UserID:   ownerID,
		FriendID: peerID,
	}
	if err := db.DB.Create(&friend).Error; err != nil {
		t.Fatalf("seed friend relation error: %v", err)
	}
}

type sessionOpenResp struct {
	Code int    `json:"code"`
	Msg  string `json:"msg"`
	Data struct {
		SessionID string `json:"session_id"`
		IsNew     bool   `json:"is_new"`
	} `json:"data"`
}

type agentSessionSearchResp struct {
	Code int    `json:"code"`
	Msg  string `json:"msg"`
	Data struct {
		HasMore bool `json:"has_more"`
		List    []struct {
			SessionID string `json:"session_id"`
			Title     string `json:"title"`
		} `json:"list"`
	} `json:"data"`
}

type agentSessionLeaveResp struct {
	Code int    `json:"code"`
	Msg  string `json:"msg"`
	Data struct {
		SessionID string `json:"session_id"`
		Left      bool   `json:"left"`
	} `json:"data"`
}

type agentSessionDetailResp struct {
	Code int    `json:"code"`
	Msg  string `json:"msg"`
	Data struct {
		SessionID   string `json:"session_id"`
		GroupName   string `json:"group_name"`
		SessionType int16  `json:"session_type"`
		MemberCount int    `json:"member_count"`
	} `json:"data"`
}

func TestAgentSessionCreate(t *testing.T) {
	r, testDB, cleanup := setupAgentSessionHandlerTest(t)
	defer cleanup()

	const (
		ownerID = int64(21001)
		agentID = int64(31001)
		apiKey  = "ak_test_session_create_key"
		peerID  = int64(99901)
	)
	seedAgentAPIAuthData(t, testDB, ownerID, agentID, apiKey)
	seedAgentSessionFriendRelation(t, testDB, ownerID, peerID)

	body := []byte(`{"peer_id":"99901","peer_type":1}`)
	req, _ := http.NewRequest(http.MethodPost, "/agent-api/sessions/create", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d, body: %s", w.Code, w.Body.String())
	}

	var resp sessionOpenResp
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response error: %v", err)
	}
	if resp.Code != 0 {
		t.Fatalf("expected code 0, got %d, msg: %s", resp.Code, resp.Msg)
	}
	if resp.Data.SessionID == "" {
		t.Fatalf("expected session_id")
	}
	if !resp.Data.IsNew {
		t.Fatalf("expected is_new=true")
	}
}

func TestAgentSessionGroupDetail(t *testing.T) {
	r, testDB, cleanup := setupAgentSessionHandlerTest(t)
	defer cleanup()

	const (
		ownerID = int64(21041)
		agentID = int64(31041)
		apiKey  = "ak_test_session_group_detail_key"
	)
	seedAgentAPIAuthData(t, testDB, ownerID, agentID, apiKey)

	t.Run("allows current agent member without scope", func(t *testing.T) {
		const (
			sessionID    = "agent-session-group-detail-direct"
			groupOwnerID = int64(21042)
			memberID     = int64(21043)
		)
		now := time.Now()
		users := []model.User{
			{ID: groupOwnerID, Username: "group_owner_direct", Email: "group_owner_direct@example.com", PasswordHash: "x", AuthProvider: "local", Nickname: "GroupOwner"},
			{ID: memberID, Username: "group_member_direct", Email: "group_member_direct@example.com", PasswordHash: "x", AuthProvider: "local", Nickname: "GroupMember"},
		}
		if err := testDB.DB.Create(&users).Error; err != nil {
			t.Fatalf("create direct users error: %v", err)
		}
		session := model.Session{
			SessionID:      sessionID,
			OwnerID:        groupOwnerID,
			SessionType:    model.SessionTypeGroup,
			GroupName:      "direct-readable-group",
			LastMsgSummary: "latest",
		}
		if err := testDB.DB.Create(&session).Error; err != nil {
			t.Fatalf("create direct session error: %v", err)
		}
		members := []model.SessionMember{
			{SessionID: sessionID, MemberID: groupOwnerID, MemberType: 1, Role: 3, LastActiveAt: now, JoinedAt: now},
			{SessionID: sessionID, MemberID: memberID, MemberType: 1, Role: 1, LastActiveAt: now, JoinedAt: now},
			{SessionID: sessionID, MemberID: agentID, MemberType: 2, Role: 1, LastActiveAt: now, JoinedAt: now},
		}
		if err := testDB.DB.Create(&members).Error; err != nil {
			t.Fatalf("create direct members error: %v", err)
		}

		req, _ := http.NewRequest(http.MethodGet, "/agent-api/sessions/group/detail?session_id="+sessionID, nil)
		req.Header.Set("Authorization", "Bearer "+apiKey)

		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("expected status 200, got %d, body: %s", w.Code, w.Body.String())
		}

		var resp agentSessionDetailResp
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("unmarshal direct detail response error: %v", err)
		}
		if resp.Code != 0 {
			t.Fatalf("expected code 0, got %d, msg: %s", resp.Code, resp.Msg)
		}
		if resp.Data.SessionID != sessionID || resp.Data.SessionType != model.SessionTypeGroup {
			t.Fatalf("unexpected direct detail payload: %#v", resp.Data)
		}
		if resp.Data.GroupName != "direct-readable-group" {
			t.Fatalf("expected group_name=%q, got %q", "direct-readable-group", resp.Data.GroupName)
		}
		if resp.Data.MemberCount != 3 {
			t.Fatalf("expected member_count=3, got %d", resp.Data.MemberCount)
		}
	})

	t.Run("allows delegated owner member without scope", func(t *testing.T) {
		const (
			sessionID    = "agent-session-group-detail-delegated"
			groupOwnerID = int64(21044)
		)
		now := time.Now()
		groupOwner := model.User{
			ID:           groupOwnerID,
			Username:     "group_owner_delegated",
			Email:        "group_owner_delegated@example.com",
			PasswordHash: "x",
			AuthProvider: "local",
			Nickname:     "GroupOwnerDelegated",
		}
		if err := testDB.DB.Create(&groupOwner).Error; err != nil {
			t.Fatalf("create delegated group owner error: %v", err)
		}
		session := model.Session{
			SessionID:      sessionID,
			OwnerID:        groupOwnerID,
			SessionType:    model.SessionTypeGroup,
			GroupName:      "delegated-readable-group",
			LastMsgSummary: "latest",
		}
		if err := testDB.DB.Create(&session).Error; err != nil {
			t.Fatalf("create delegated session error: %v", err)
		}
		members := []model.SessionMember{
			{SessionID: sessionID, MemberID: groupOwnerID, MemberType: 1, Role: 3, LastActiveAt: now, JoinedAt: now},
			{SessionID: sessionID, MemberID: ownerID, MemberType: 1, Role: 1, LastActiveAt: now, JoinedAt: now},
		}
		if err := testDB.DB.Create(&members).Error; err != nil {
			t.Fatalf("create delegated members error: %v", err)
		}
		if err := store.RDB.HSet(context.Background(), "im:delegate:"+sessionID+":21041", "agent_id", agentID).Err(); err != nil {
			t.Fatalf("seed delegated detail key error: %v", err)
		}

		req, _ := http.NewRequest(http.MethodGet, "/agent-api/sessions/group/detail?session_id="+sessionID, nil)
		req.Header.Set("Authorization", "Bearer "+apiKey)

		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("expected status 200, got %d, body: %s", w.Code, w.Body.String())
		}

		var resp agentSessionDetailResp
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("unmarshal delegated detail response error: %v", err)
		}
		if resp.Code != 0 {
			t.Fatalf("expected code 0, got %d, msg: %s", resp.Code, resp.Msg)
		}
		if resp.Data.SessionID != sessionID {
			t.Fatalf("expected delegated session_id %q, got %q", sessionID, resp.Data.SessionID)
		}
		if resp.Data.GroupName != "delegated-readable-group" {
			t.Fatalf("expected group_name=%q, got %q", "delegated-readable-group", resp.Data.GroupName)
		}
		if resp.Data.MemberCount != 2 {
			t.Fatalf("expected member_count=2, got %d", resp.Data.MemberCount)
		}
	})

	t.Run("denies unrelated agent without scope", func(t *testing.T) {
		const (
			sessionID    = "agent-session-group-detail-denied"
			groupOwnerID = int64(21045)
		)
		now := time.Now()
		groupOwner := model.User{
			ID:           groupOwnerID,
			Username:     "group_owner_denied",
			Email:        "group_owner_denied@example.com",
			PasswordHash: "x",
			AuthProvider: "local",
			Nickname:     "GroupOwnerDenied",
		}
		if err := testDB.DB.Create(&groupOwner).Error; err != nil {
			t.Fatalf("create denied group owner error: %v", err)
		}
		session := model.Session{
			SessionID:      sessionID,
			OwnerID:        groupOwnerID,
			SessionType:    model.SessionTypeGroup,
			GroupName:      "denied-group",
			LastMsgSummary: "latest",
		}
		if err := testDB.DB.Create(&session).Error; err != nil {
			t.Fatalf("create denied session error: %v", err)
		}
		members := []model.SessionMember{
			{SessionID: sessionID, MemberID: groupOwnerID, MemberType: 1, Role: 3, LastActiveAt: now, JoinedAt: now},
		}
		if err := testDB.DB.Create(&members).Error; err != nil {
			t.Fatalf("create denied members error: %v", err)
		}

		req, _ := http.NewRequest(http.MethodGet, "/agent-api/sessions/group/detail?session_id="+sessionID, nil)
		req.Header.Set("Authorization", "Bearer "+apiKey)

		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusForbidden {
			t.Fatalf("expected status 403, got %d, body: %s", w.Code, w.Body.String())
		}
	})
}

func TestAgentSessionCreateRejectsForeignAgentPeer(t *testing.T) {
	r, testDB, cleanup := setupAgentSessionHandlerTest(t)
	defer cleanup()

	const (
		ownerID        = int64(21011)
		agentID        = int64(31011)
		apiKey         = "ak_test_session_create_foreign_agent_key"
		foreignAgentID = int64(99911)
	)
	seedAgentAPIAuthData(t, testDB, ownerID, agentID, apiKey)
	foreignAgent := model.Agent{
		ID:        foreignAgentID,
		AgentName: "foreign_agent",
		OwnerID:   ownerID + 1,
		Status:    1,
	}
	if err := testDB.DB.Create(&foreignAgent).Error; err != nil {
		t.Fatalf("seed foreign agent error: %v", err)
	}

	body := []byte(`{"peer_id":"99911","peer_type":2}`)
	req, _ := http.NewRequest(http.MethodPost, "/agent-api/sessions/create", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d, body: %s", w.Code, w.Body.String())
	}
}

func TestAgentSessionCreateGroupAddsCurrentAgentMember(t *testing.T) {
	r, testDB, cleanup := setupAgentSessionHandlerTest(t)
	defer cleanup()

	const (
		ownerID  = int64(21012)
		agentID  = int64(31012)
		apiKey   = "ak_test_session_create_group_key"
		friendID = int64(99913)
	)
	seedAgentAPIAuthData(t, testDB, ownerID, agentID, apiKey)
	seedAgentSessionScope(t, testDB, agentID, agentscope.ScopeGroupCreate)
	seedAgentSessionFriendRelation(t, testDB, ownerID, friendID)

	body := []byte(`{"name":"Agent Group","member_ids":["99913"]}`)
	req, _ := http.NewRequest(http.MethodPost, "/agent-api/sessions/create_group", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d, body: %s", w.Code, w.Body.String())
	}

	var resp sessionOpenResp
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response error: %v", err)
	}
	if resp.Code != 0 {
		t.Fatalf("expected code 0, got %d, msg: %s", resp.Code, resp.Msg)
	}
	if resp.Data.SessionID == "" {
		t.Fatalf("expected session_id")
	}

	var members []model.SessionMember
	if err := testDB.DB.Where("session_id = ?", resp.Data.SessionID).Find(&members).Error; err != nil {
		t.Fatalf("query created group members error: %v", err)
	}
	if len(members) != 3 {
		t.Fatalf("expected 3 members (owner + friend + agent), got %d", len(members))
	}

	assertMemberExists := func(memberID int64, memberType int16, role int16) {
		t.Helper()
		var member model.SessionMember
		if err := testDB.DB.Where(
			"session_id = ? AND member_id = ? AND member_type = ?",
			resp.Data.SessionID,
			memberID,
			memberType,
		).First(&member).Error; err != nil {
			t.Fatalf("expected member id=%d type=%d to exist: %v", memberID, memberType, err)
		}
		if member.Role != role {
			t.Fatalf("member id=%d type=%d role=%d want=%d", memberID, memberType, member.Role, role)
		}
	}

	assertMemberExists(ownerID, 1, 3)
	assertMemberExists(friendID, 1, 1)
	assertMemberExists(agentID, 2, 1)
}

func TestAgentSessionOpenLatest(t *testing.T) {
	r, testDB, cleanup := setupAgentSessionHandlerTest(t)
	defer cleanup()

	const (
		ownerID        = int64(21002)
		agentID        = int64(31002)
		apiKey         = "ak_test_open_latest_key"
		peerID         = int64(99902)
		foreignAgentID = int64(99912)
	)
	seedAgentAPIAuthData(t, testDB, ownerID, agentID, apiKey)
	seedAgentSessionFriendRelation(t, testDB, ownerID, peerID)
	foreignAgent := model.Agent{
		ID:        foreignAgentID,
		AgentName: "foreign_agent_open_latest",
		OwnerID:   ownerID + 1,
		Status:    1,
	}
	if err := testDB.DB.Create(&foreignAgent).Error; err != nil {
		t.Fatalf("seed foreign agent error: %v", err)
	}

	t.Run("open latest creates then reuses", func(t *testing.T) {
		body := []byte(`{"peer_id":"99902","peer_type":1}`)

		req1, _ := http.NewRequest(http.MethodPost, "/agent-api/sessions/open_latest", bytes.NewReader(body))
		req1.Header.Set("Authorization", "Bearer "+apiKey)
		req1.Header.Set("Content-Type", "application/json")

		w1 := httptest.NewRecorder()
		r.ServeHTTP(w1, req1)
		if w1.Code != http.StatusOK {
			t.Fatalf("first open_latest expected 200, got %d, body: %s", w1.Code, w1.Body.String())
		}

		var resp1 sessionOpenResp
		if err := json.Unmarshal(w1.Body.Bytes(), &resp1); err != nil {
			t.Fatalf("unmarshal first response error: %v", err)
		}
		if resp1.Code != 0 {
			t.Fatalf("first open_latest expected code 0, got %d, msg: %s", resp1.Code, resp1.Msg)
		}
		if resp1.Data.SessionID == "" {
			t.Fatalf("first open_latest should return session_id")
		}
		if !resp1.Data.IsNew {
			t.Fatalf("first open_latest should return is_new=true")
		}

		req2, _ := http.NewRequest(http.MethodPost, "/agent-api/sessions/open_latest", bytes.NewReader(body))
		req2.Header.Set("Authorization", "Bearer "+apiKey)
		req2.Header.Set("Content-Type", "application/json")

		w2 := httptest.NewRecorder()
		r.ServeHTTP(w2, req2)
		if w2.Code != http.StatusOK {
			t.Fatalf("second open_latest expected 200, got %d, body: %s", w2.Code, w2.Body.String())
		}

		var resp2 sessionOpenResp
		if err := json.Unmarshal(w2.Body.Bytes(), &resp2); err != nil {
			t.Fatalf("unmarshal second response error: %v", err)
		}
		if resp2.Code != 0 {
			t.Fatalf("second open_latest expected code 0, got %d, msg: %s", resp2.Code, resp2.Msg)
		}
		if resp2.Data.SessionID != resp1.Data.SessionID {
			t.Fatalf("second open_latest should return same session_id: %s vs %s", resp1.Data.SessionID, resp2.Data.SessionID)
		}
		if resp2.Data.IsNew {
			t.Fatalf("second open_latest should return is_new=false")
		}
	})

	t.Run("rejects foreign agent peer", func(t *testing.T) {
		body := []byte(`{"peer_id":"99912","peer_type":2}`)
		req, _ := http.NewRequest(http.MethodPost, "/agent-api/sessions/open_latest", bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+apiKey)
		req.Header.Set("Content-Type", "application/json")

		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("expected status 400, got %d, body: %s", w.Code, w.Body.String())
		}
	})
}

func TestAgentSessionAddMembersRejectsForeignAgentTarget(t *testing.T) {
	r, testDB, cleanup := setupAgentSessionHandlerTest(t)
	defer cleanup()

	const (
		ownerID        = int64(21021)
		agentID        = int64(31021)
		apiKey         = "ak_test_add_members_foreign_agent_key"
		friendID       = int64(99921)
		foreignAgentID = int64(99922)
	)
	seedAgentAPIAuthData(t, testDB, ownerID, agentID, apiKey)
	seedAgentSessionFriendRelation(t, testDB, ownerID, friendID)
	foreignAgent := model.Agent{
		ID:        foreignAgentID,
		AgentName: "foreign_agent_add_member",
		OwnerID:   ownerID + 1,
		Status:    1,
	}
	if err := testDB.DB.Create(&foreignAgent).Error; err != nil {
		t.Fatalf("seed foreign agent error: %v", err)
	}

	session := model.Session{
		SessionID:      "agent-session-add-members-group",
		OwnerID:        ownerID,
		SessionType:    2,
		GroupName:      "group",
		LastMsgSummary: "group",
	}
	if err := testDB.DB.Create(&session).Error; err != nil {
		t.Fatalf("create group session error: %v", err)
	}
	members := []model.SessionMember{
		{SessionID: "agent-session-add-members-group", MemberID: ownerID, MemberType: 1, Role: 3},
		{SessionID: "agent-session-add-members-group", MemberID: friendID, MemberType: 1, Role: 1},
	}
	if err := testDB.DB.Create(&members).Error; err != nil {
		t.Fatalf("create session members error: %v", err)
	}

	body := []byte(`{"session_id":"agent-session-add-members-group","member_ids":["99922"],"member_types":[2]}`)
	req, _ := http.NewRequest(http.MethodPost, "/agent-api/sessions/members/add", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d, body: %s", w.Code, w.Body.String())
	}
}

func TestAgentSessionLeave(t *testing.T) {
	r, testDB, cleanup := setupAgentSessionHandlerTest(t)
	defer cleanup()

	const (
		ownerID = int64(21025)
		agentID = int64(31025)
		apiKey  = "ak_test_session_leave_key"
	)
	seedAgentAPIAuthData(t, testDB, ownerID, agentID, apiKey)

	now := time.Now()
	groupSession := model.Session{
		SessionID:      "agent-session-leave-group",
		OwnerID:        ownerID,
		SessionType:    2,
		GroupName:      "leave-group",
		LastMsgSummary: "leave-group",
	}
	if err := testDB.DB.Create(&groupSession).Error; err != nil {
		t.Fatalf("create leave group session error: %v", err)
	}
	groupMembers := []model.SessionMember{
		{SessionID: groupSession.SessionID, MemberID: ownerID, MemberType: 1, Role: 3, LastActiveAt: now, JoinedAt: now},
		{SessionID: groupSession.SessionID, MemberID: agentID, MemberType: 2, Role: 1, LastActiveAt: now, JoinedAt: now},
	}
	if err := testDB.DB.Create(&groupMembers).Error; err != nil {
		t.Fatalf("create leave group members error: %v", err)
	}

	directSession := model.Session{
		SessionID:      "agent-session-leave-direct",
		OwnerID:        ownerID,
		SessionType:    1,
		LastMsgSummary: "direct",
	}
	if err := testDB.DB.Create(&directSession).Error; err != nil {
		t.Fatalf("create leave direct session error: %v", err)
	}
	directMembers := []model.SessionMember{
		{SessionID: directSession.SessionID, MemberID: ownerID, MemberType: 1, Role: 3, LastActiveAt: now, JoinedAt: now},
		{SessionID: directSession.SessionID, MemberID: agentID, MemberType: 2, Role: 1, LastActiveAt: now, JoinedAt: now},
	}
	if err := testDB.DB.Create(&directMembers).Error; err != nil {
		t.Fatalf("create leave direct members error: %v", err)
	}

	t.Run("delegated leave removes delegated user and agent member", func(t *testing.T) {
		delegatedSessionID := "agent-session-leave-delegated"
		groupOwnerID := ownerID + 1
		delegateKey := "im:delegate:" + delegatedSessionID + ":21025"

		delegatedSession := model.Session{
			SessionID:      delegatedSessionID,
			OwnerID:        groupOwnerID,
			SessionType:    2,
			GroupName:      "delegated-leave-group",
			LastMsgSummary: "delegated-leave-group",
		}
		if err := testDB.DB.Create(&delegatedSession).Error; err != nil {
			t.Fatalf("create delegated leave group session error: %v", err)
		}
		delegatedMembers := []model.SessionMember{
			{SessionID: delegatedSessionID, MemberID: groupOwnerID, MemberType: 1, Role: 3, LastActiveAt: now, JoinedAt: now},
			{SessionID: delegatedSessionID, MemberID: ownerID, MemberType: 1, Role: 1, LastActiveAt: now, JoinedAt: now},
			{SessionID: delegatedSessionID, MemberID: agentID, MemberType: 2, Role: 1, LastActiveAt: now, JoinedAt: now},
		}
		if err := testDB.DB.Create(&delegatedMembers).Error; err != nil {
			t.Fatalf("create delegated leave group members error: %v", err)
		}
		if err := store.RDB.HSet(context.Background(), delegateKey, "agent_id", agentID).Err(); err != nil {
			t.Fatalf("seed delegated leave key error: %v", err)
		}

		body := []byte(`{"session_id":"agent-session-leave-delegated"}`)
		req, _ := http.NewRequest(http.MethodPost, "/agent-api/sessions/leave", bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+apiKey)
		req.Header.Set("Content-Type", "application/json")

		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("expected status 200, got %d, body: %s", w.Code, w.Body.String())
		}

		var resp agentSessionLeaveResp
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("unmarshal delegated leave response error: %v", err)
		}
		if resp.Code != 0 {
			t.Fatalf("expected code 0, got %d, msg: %s", resp.Code, resp.Msg)
		}
		if !resp.Data.Left {
			t.Fatalf("expected left=true for delegated leave")
		}

		var delegatedUserCount int64
		if err := testDB.DB.Model(&model.SessionMember{}).
			Where("session_id = ? AND member_id = ? AND member_type = 1", delegatedSessionID, ownerID).
			Count(&delegatedUserCount).Error; err != nil {
			t.Fatalf("count delegated user membership error: %v", err)
		}
		if delegatedUserCount != 0 {
			t.Fatalf("expected delegated user removed, got %d", delegatedUserCount)
		}

		var delegatedAgentCount int64
		if err := testDB.DB.Model(&model.SessionMember{}).
			Where("session_id = ? AND member_id = ? AND member_type = 2", delegatedSessionID, agentID).
			Count(&delegatedAgentCount).Error; err != nil {
			t.Fatalf("count delegated agent membership error: %v", err)
		}
		if delegatedAgentCount != 0 {
			t.Fatalf("expected delegated agent removed, got %d", delegatedAgentCount)
		}

		if exists, err := store.RDB.Exists(context.Background(), delegateKey).Result(); err != nil {
			t.Fatalf("check delegated leave key error: %v", err)
		} else if exists != 0 {
			t.Fatalf("expected delegated leave key removed, got exists=%d", exists)
		}
	})

	t.Run("agent can leave group without scope", func(t *testing.T) {
		body := []byte(`{"session_id":"agent-session-leave-group"}`)
		req, _ := http.NewRequest(http.MethodPost, "/agent-api/sessions/leave", bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+apiKey)
		req.Header.Set("Content-Type", "application/json")

		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("expected status 200, got %d, body: %s", w.Code, w.Body.String())
		}

		var resp agentSessionLeaveResp
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("unmarshal leave response error: %v", err)
		}
		if resp.Code != 0 {
			t.Fatalf("expected code 0, got %d, msg: %s", resp.Code, resp.Msg)
		}
		if !resp.Data.Left {
			t.Fatalf("expected left=true")
		}

		var memberCount int64
		if err := testDB.DB.Model(&model.SessionMember{}).
			Where("session_id = ? AND member_id = ? AND member_type = 2", groupSession.SessionID, agentID).
			Count(&memberCount).Error; err != nil {
			t.Fatalf("count leave membership error: %v", err)
		}
		if memberCount != 0 {
			t.Fatalf("expected agent membership removed, got %d", memberCount)
		}
	})

	t.Run("repeat leave is idempotent", func(t *testing.T) {
		body := []byte(`{"session_id":"agent-session-leave-group"}`)
		req, _ := http.NewRequest(http.MethodPost, "/agent-api/sessions/leave", bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+apiKey)
		req.Header.Set("Content-Type", "application/json")

		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("expected status 200, got %d, body: %s", w.Code, w.Body.String())
		}

		var resp agentSessionLeaveResp
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("unmarshal repeat leave response error: %v", err)
		}
		if resp.Code != 0 {
			t.Fatalf("expected code 0, got %d, msg: %s", resp.Code, resp.Msg)
		}
		if resp.Data.Left {
			t.Fatalf("expected left=false on repeated leave")
		}
	})

	t.Run("rejects non-group session", func(t *testing.T) {
		body := []byte(`{"session_id":"agent-session-leave-direct"}`)
		req, _ := http.NewRequest(http.MethodPost, "/agent-api/sessions/leave", bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+apiKey)
		req.Header.Set("Content-Type", "application/json")

		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("expected status 400, got %d, body: %s", w.Code, w.Body.String())
		}
	})
}

func TestAgentSessionSearch(t *testing.T) {
	r, testDB, cleanup := setupAgentSessionHandlerTest(t)
	defer cleanup()

	const (
		ownerID = int64(21031)
		agentID = int64(31031)
		apiKey  = "ak_test_session_search_key"
	)
	seedAgentAPIAuthData(t, testDB, ownerID, agentID, apiKey)

	t.Run("requires scope", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodGet, "/agent-api/sessions/search?id=agent-session-search-group", nil)
		req.Header.Set("Authorization", "Bearer "+apiKey)

		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusForbidden {
			t.Fatalf("expected status 403, got %d, body: %s", w.Code, w.Body.String())
		}
	})

	t.Run("list all on empty query", func(t *testing.T) {
		seedAgentSessionScope(t, testDB, agentID, agentscope.ScopeSessionSearch)

		req, _ := http.NewRequest(http.MethodGet, "/agent-api/sessions/search", nil)
		req.Header.Set("Authorization", "Bearer "+apiKey)

		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("expected status 200, got %d, body: %s", w.Code, w.Body.String())
		}
	})

	t.Run("returns matched sessions by exact id", func(t *testing.T) {
		now := time.Now()
		session := model.Session{
			SessionID:      "agent-session-search-group",
			OwnerID:        ownerID,
			SessionType:    2,
			GroupName:      "Project Atlas",
			LastMsgSummary: "latest",
		}
		if err := testDB.DB.Create(&session).Error; err != nil {
			t.Fatalf("create session error: %v", err)
		}
		member := model.SessionMember{
			SessionID:    session.SessionID,
			MemberID:     ownerID,
			MemberType:   1,
			Role:         3,
			LastActiveAt: now,
			JoinedAt:     now,
		}
		if err := testDB.DB.Create(&member).Error; err != nil {
			t.Fatalf("create session member error: %v", err)
		}

		req, _ := http.NewRequest(http.MethodGet, "/agent-api/sessions/search?id=agent-session-search-group", nil)
		req.Header.Set("Authorization", "Bearer "+apiKey)

		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("expected status 200, got %d, body: %s", w.Code, w.Body.String())
		}

		var resp agentSessionSearchResp
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("unmarshal response error: %v", err)
		}
		if resp.Code != 0 {
			t.Fatalf("expected code 0, got %d, msg: %s", resp.Code, resp.Msg)
		}
		if resp.Data.HasMore {
			t.Fatalf("expected has_more=false")
		}
		if len(resp.Data.List) != 1 {
			t.Fatalf("expected 1 search result, got %d", len(resp.Data.List))
		}
		if resp.Data.List[0].SessionID != session.SessionID {
			t.Fatalf("expected session_id %q, got %q", session.SessionID, resp.Data.List[0].SessionID)
		}
		if resp.Data.List[0].Title != session.GroupName {
			t.Fatalf("expected title %q, got %q", session.GroupName, resp.Data.List[0].Title)
		}
	})

	t.Run("supports keyword fuzzy search", func(t *testing.T) {
		now := time.Now().Add(time.Minute)
		session := model.Session{
			SessionID:      "task_room_9083",
			OwnerID:        ownerID,
			SessionType:    2,
			GroupName:      "Task Room 9083",
			LastMsgSummary: "latest",
		}
		if err := testDB.DB.Create(&session).Error; err != nil {
			t.Fatalf("create fuzzy session error: %v", err)
		}
		member := model.SessionMember{
			SessionID:    session.SessionID,
			MemberID:     ownerID,
			MemberType:   1,
			Role:         3,
			LastActiveAt: now,
			JoinedAt:     now,
		}
		if err := testDB.DB.Create(&member).Error; err != nil {
			t.Fatalf("create fuzzy session member error: %v", err)
		}

		req, _ := http.NewRequest(http.MethodGet, "/agent-api/sessions/search?keyword=taskroom9083", nil)
		req.Header.Set("Authorization", "Bearer "+apiKey)

		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("expected status 200, got %d, body: %s", w.Code, w.Body.String())
		}

		var resp agentSessionSearchResp
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("unmarshal response error: %v", err)
		}
		if len(resp.Data.List) != 1 {
			t.Fatalf("expected 1 fuzzy search result, got %d", len(resp.Data.List))
		}
		if resp.Data.List[0].SessionID != session.SessionID || resp.Data.List[0].Title != session.GroupName {
			t.Fatalf("unexpected fuzzy search result: %#v", resp.Data.List[0])
		}
	})
}
