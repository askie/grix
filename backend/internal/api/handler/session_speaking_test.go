package handler

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/askie/grix/backend/internal/model"
)

func TestSessionSpeakingHandlers(t *testing.T) {
	r, testDB, cleanup := setupSessionHandlerTest(t)
	defer cleanup()

	const (
		ownerID   = int64(18101)
		adminID   = int64(18102)
		memberID  = int64(18103)
		sessionID = "session-speaking-handler-1"
	)

	now := time.Now()
	ownerToken := createSessionTestUser(t, testDB, ownerID, "speakingowner")
	adminToken := createSessionTestUser(t, testDB, adminID, "speakingadmin")
	memberToken := createSessionTestUser(t, testDB, memberID, "speakingmember")

	session := model.Session{
		SessionID:      sessionID,
		OwnerID:        ownerID,
		SessionType:    model.SessionTypeGroup,
		GroupName:      "handler-group",
		LastMsgSummary: "group",
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

	t.Run("admin updates all members muted", func(t *testing.T) {
		body, _ := json.Marshal(updateAllMembersMutedReq{
			SessionID:       sessionID,
			AllMembersMuted: boolPtr(true),
		})
		req, _ := http.NewRequest(http.MethodPost, "/sessions/speaking/all_muted", bytes.NewReader(body))
		req.Header.Set("Authorization", adminToken)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected status 200, got %d body=%s", w.Code, w.Body.String())
		}
	})

	t.Run("owner updates member speaking", func(t *testing.T) {
		body, _ := json.Marshal(updateMemberSpeakingReq{
			SessionID:            sessionID,
			MemberID:             "18103",
			MemberType:           1,
			CanSpeakWhenAllMuted: boolPtr(true),
		})
		req, _ := http.NewRequest(http.MethodPost, "/sessions/members/speaking", bytes.NewReader(body))
		req.Header.Set("Authorization", ownerToken)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected status 200, got %d body=%s", w.Code, w.Body.String())
		}
	})

	t.Run("normal member cannot update speaking setting", func(t *testing.T) {
		body, _ := json.Marshal(updateAllMembersMutedReq{
			SessionID:       sessionID,
			AllMembersMuted: boolPtr(false),
		})
		req, _ := http.NewRequest(http.MethodPost, "/sessions/speaking/all_muted", bytes.NewReader(body))
		req.Header.Set("Authorization", memberToken)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusForbidden {
			t.Fatalf("expected status 403, got %d body=%s", w.Code, w.Body.String())
		}
	})
}
