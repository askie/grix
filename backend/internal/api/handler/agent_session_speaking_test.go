package handler

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/askie/grix/backend/internal/model"
)

func TestAgentSessionSpeakingHandlers(t *testing.T) {
	r, testDB, cleanup := setupAgentSessionHandlerTest(t)
	defer cleanup()

	const (
		ownerID   = int64(28201)
		agentID   = int64(38201)
		apiKey    = "ak_test_speaking_governance"
		memberID  = int64(28202)
		sessionID = "agent-session-speaking-handler-1"
	)

	now := time.Now()
	seedAgentAPIAuthData(t, testDB, ownerID, agentID, apiKey)
	peer := model.User{
		ID:           memberID,
		Username:     "agent_speaking_member",
		Email:        "agent_speaking_member@example.com",
		PasswordHash: "x",
		AuthProvider: "local",
		Nickname:     "Member",
	}
	if err := testDB.DB.Create(&peer).Error; err != nil {
		t.Fatalf("seed member error: %v", err)
	}

	session := model.Session{
		SessionID:      sessionID,
		OwnerID:        ownerID,
		SessionType:    model.SessionTypeGroup,
		GroupName:      "agent-group",
		LastMsgSummary: "group",
	}
	if err := testDB.DB.Create(&session).Error; err != nil {
		t.Fatalf("create session error: %v", err)
	}
	members := []model.SessionMember{
		{SessionID: sessionID, MemberID: ownerID, MemberType: 1, Role: 3, LastActiveAt: now, JoinedAt: now},
		{SessionID: sessionID, MemberID: memberID, MemberType: 1, Role: 1, LastActiveAt: now, JoinedAt: now},
	}
	if err := testDB.DB.Create(&members).Error; err != nil {
		t.Fatalf("create session members error: %v", err)
	}

	t.Run("agent api updates all members muted", func(t *testing.T) {
		req, _ := http.NewRequest(
			http.MethodPost,
			"/agent-api/sessions/speaking/all_muted",
			bytes.NewReader([]byte(`{"session_id":"agent-session-speaking-handler-1","all_members_muted":true}`)),
		)
		req.Header.Set("Authorization", "Bearer "+apiKey)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected status 200, got %d body=%s", w.Code, w.Body.String())
		}
	})

	t.Run("agent api updates member speaking", func(t *testing.T) {
		req, _ := http.NewRequest(
			http.MethodPost,
			"/agent-api/sessions/members/speaking",
			bytes.NewReader([]byte(`{"session_id":"agent-session-speaking-handler-1","member_id":"28202","is_speak_muted":true}`)),
		)
		req.Header.Set("Authorization", "Bearer "+apiKey)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected status 200, got %d body=%s", w.Code, w.Body.String())
		}
	})
}
