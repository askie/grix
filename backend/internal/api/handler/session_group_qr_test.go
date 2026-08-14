package handler

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/systemsetting"
)

func TestSessionGroupQRCodeFlow(t *testing.T) {
	r, testDB, cleanup := setupSessionHandlerTest(t)
	defer cleanup()

	const (
		sessionID = "session-group-qr-handler-1"
		ownerID   = int64(920001)
		joinerID  = int64(920002)
	)

	ownerToken := createSessionTestUser(t, testDB, ownerID, "groupqrowner")
	joinerToken := createSessionTestUser(t, testDB, joinerID, "groupqrjoiner")

	now := time.Now()
	if err := testDB.DB.Create(&model.Session{
		SessionID:      sessionID,
		OwnerID:        ownerID,
		SessionType:    2,
		GroupName:      "QR Handler Group",
		LastMsgSummary: "QR Handler Group",
	}).Error; err != nil {
		t.Fatalf("create group session error: %v", err)
	}
	if err := testDB.DB.Create(&model.SessionMember{
		SessionID:    sessionID,
		MemberID:     ownerID,
		MemberType:   1,
		Role:         3,
		JoinedAt:     now,
		LastActiveAt: now,
	}).Error; err != nil {
		t.Fatalf("create owner membership error: %v", err)
	}

	reqGetQR, _ := http.NewRequest(http.MethodGet, "/sessions/group/qr?session_id="+sessionID, nil)
	reqGetQR.Header.Set("Authorization", ownerToken)
	wGetQR := httptest.NewRecorder()
	r.ServeHTTP(wGetQR, reqGetQR)
	if wGetQR.Code != http.StatusOK {
		t.Fatalf("expected status 200 for get group qr, got %d, body=%s", wGetQR.Code, wGetQR.Body.String())
	}

	var getQRResp map[string]interface{}
	if err := json.Unmarshal(wGetQR.Body.Bytes(), &getQRResp); err != nil {
		t.Fatalf("unmarshal get group qr response error: %v", err)
	}
	dataRaw, ok := getQRResp["data"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected data object in get group qr response, got %#v", getQRResp["data"])
	}
	code := dataRaw["code"].(string)
	if code == "" {
		t.Fatal("expected non-empty group qr code")
	}

	reqResolve, _ := http.NewRequest(http.MethodGet, "/sessions/group/qr/resolve/"+code, nil)
	reqResolve.Header.Set("Authorization", joinerToken)
	wResolve := httptest.NewRecorder()
	r.ServeHTTP(wResolve, reqResolve)
	if wResolve.Code != http.StatusOK {
		t.Fatalf("expected status 200 for resolve group qr, got %d, body=%s", wResolve.Code, wResolve.Body.String())
	}

	var resolveResp map[string]interface{}
	if err := json.Unmarshal(wResolve.Body.Bytes(), &resolveResp); err != nil {
		t.Fatalf("unmarshal resolve response error: %v", err)
	}
	resolveData := resolveResp["data"].(map[string]interface{})
	if resolveData["session_id"].(string) != sessionID {
		t.Fatalf("unexpected resolve session_id %v", resolveData["session_id"])
	}
	if resolveData["is_member"].(bool) {
		t.Fatal("expected joiner is_member=false before join")
	}

	joinBody, _ := json.Marshal(map[string]string{"code": code})
	reqJoin, _ := http.NewRequest(http.MethodPost, "/sessions/group/join_by_qr", bytes.NewReader(joinBody))
	reqJoin.Header.Set("Authorization", joinerToken)
	reqJoin.Header.Set("Content-Type", "application/json")
	wJoin := httptest.NewRecorder()
	r.ServeHTTP(wJoin, reqJoin)
	if wJoin.Code != http.StatusOK {
		t.Fatalf("expected status 200 for join group qr, got %d, body=%s", wJoin.Code, wJoin.Body.String())
	}

	var joinResp map[string]interface{}
	if err := json.Unmarshal(wJoin.Body.Bytes(), &joinResp); err != nil {
		t.Fatalf("unmarshal join response error: %v", err)
	}
	joinData := joinResp["data"].(map[string]interface{})
	if joinData["session_id"].(string) != sessionID {
		t.Fatalf("unexpected join session_id %v", joinData["session_id"])
	}
	if !joinData["joined"].(bool) {
		t.Fatal("expected joined=true on first join")
	}

	reqResolveAfterJoin, _ := http.NewRequest(http.MethodGet, "/sessions/group/qr/resolve/"+code, nil)
	reqResolveAfterJoin.Header.Set("Authorization", joinerToken)
	wResolveAfterJoin := httptest.NewRecorder()
	r.ServeHTTP(wResolveAfterJoin, reqResolveAfterJoin)
	if wResolveAfterJoin.Code != http.StatusOK {
		t.Fatalf("expected status 200 for resolve after join, got %d, body=%s", wResolveAfterJoin.Code, wResolveAfterJoin.Body.String())
	}
	var resolveAfterJoinResp map[string]interface{}
	if err := json.Unmarshal(wResolveAfterJoin.Body.Bytes(), &resolveAfterJoinResp); err != nil {
		t.Fatalf("unmarshal resolve after join response error: %v", err)
	}
	resolveAfterJoinData := resolveAfterJoinResp["data"].(map[string]interface{})
	if !resolveAfterJoinData["is_member"].(bool) {
		t.Fatal("expected joiner is_member=true after join")
	}

	reqJoinAgain, _ := http.NewRequest(http.MethodPost, "/sessions/group/join_by_qr", bytes.NewReader(joinBody))
	reqJoinAgain.Header.Set("Authorization", joinerToken)
	reqJoinAgain.Header.Set("Content-Type", "application/json")
	wJoinAgain := httptest.NewRecorder()
	r.ServeHTTP(wJoinAgain, reqJoinAgain)
	if wJoinAgain.Code != http.StatusOK {
		t.Fatalf("expected status 200 for duplicate join, got %d, body=%s", wJoinAgain.Code, wJoinAgain.Body.String())
	}
	var joinAgainResp map[string]interface{}
	if err := json.Unmarshal(wJoinAgain.Body.Bytes(), &joinAgainResp); err != nil {
		t.Fatalf("unmarshal duplicate join response error: %v", err)
	}
	if joinAgainResp["data"].(map[string]interface{})["joined"].(bool) {
		t.Fatal("expected joined=false on duplicate join")
	}
}

func TestSessionGroupQRCodeInvalidCode(t *testing.T) {
	r, testDB, cleanup := setupSessionHandlerTest(t)
	defer cleanup()

	userID := int64(920101)
	token := createSessionTestUser(t, testDB, userID, "groupqrinvalid")

	reqResolve, _ := http.NewRequest(http.MethodGet, "/sessions/group/qr/resolve/not_exists", nil)
	reqResolve.Header.Set("Authorization", token)
	wResolve := httptest.NewRecorder()
	r.ServeHTTP(wResolve, reqResolve)
	if wResolve.Code != http.StatusNotFound {
		t.Fatalf("expected status 404 for invalid resolve code, got %d, body=%s", wResolve.Code, wResolve.Body.String())
	}

	joinBody, _ := json.Marshal(map[string]string{"code": "not_exists"})
	reqJoin, _ := http.NewRequest(http.MethodPost, "/sessions/group/join_by_qr", bytes.NewReader(joinBody))
	reqJoin.Header.Set("Authorization", token)
	reqJoin.Header.Set("Content-Type", "application/json")
	wJoin := httptest.NewRecorder()
	r.ServeHTTP(wJoin, reqJoin)
	if wJoin.Code != http.StatusNotFound {
		t.Fatalf("expected status 404 for invalid join code, got %d, body=%s", wJoin.Code, wJoin.Body.String())
	}
}

func TestSessionGroupQRCodeGetRequiresAdminOrOwner(t *testing.T) {
	r, testDB, cleanup := setupSessionHandlerTest(t)
	defer cleanup()

	const (
		sessionID = "session-group-qr-handler-role-1"
		ownerID   = int64(920201)
		memberID  = int64(920202)
	)

	ownerToken := createSessionTestUser(t, testDB, ownerID, "groupqrowner2")
	memberToken := createSessionTestUser(t, testDB, memberID, "groupqrmember2")

	now := time.Now()
	if err := testDB.DB.Create(&model.Session{
		SessionID:      sessionID,
		OwnerID:        ownerID,
		SessionType:    2,
		GroupName:      "QR Role Group",
		LastMsgSummary: "QR Role Group",
	}).Error; err != nil {
		t.Fatalf("create group session error: %v", err)
	}
	if err := testDB.DB.Create([]model.SessionMember{
		{SessionID: sessionID, MemberID: ownerID, MemberType: 1, Role: 3, JoinedAt: now, LastActiveAt: now},
		{SessionID: sessionID, MemberID: memberID, MemberType: 1, Role: 1, JoinedAt: now, LastActiveAt: now},
	}).Error; err != nil {
		t.Fatalf("create group members error: %v", err)
	}

	reqMember, _ := http.NewRequest(http.MethodGet, "/sessions/group/qr?session_id="+sessionID, nil)
	reqMember.Header.Set("Authorization", memberToken)
	wMember := httptest.NewRecorder()
	r.ServeHTTP(wMember, reqMember)
	if wMember.Code != http.StatusForbidden {
		t.Fatalf("expected status 403 for normal member get qr, got %d, body=%s", wMember.Code, wMember.Body.String())
	}

	reqOwner, _ := http.NewRequest(http.MethodGet, "/sessions/group/qr?session_id="+sessionID, nil)
	reqOwner.Header.Set("Authorization", ownerToken)
	wOwner := httptest.NewRecorder()
	r.ServeHTTP(wOwner, reqOwner)
	if wOwner.Code != http.StatusOK {
		t.Fatalf("expected status 200 for owner get qr, got %d, body=%s", wOwner.Code, wOwner.Body.String())
	}
}

func TestSessionGroupQRCodeJoinRespectsInviteRestrictions(t *testing.T) {
	r, testDB, cleanup := setupSessionHandlerTest(t)
	defer cleanup()

	const (
		ownerID  = int64(920301)
		joinerID = int64(920302)
		memberID = int64(920303)
	)

	ownerToken := createSessionTestUser(t, testDB, ownerID, "groupqrowner3")
	joinerToken := createSessionTestUser(t, testDB, joinerID, "groupqrjoiner3")
	createSessionTestUser(t, testDB, memberID, "groupqrmember3")

	now := time.Now()

	t.Run("returns 40031 when invite disabled", func(t *testing.T) {
		const sessionID = "session-group-qr-handler-disabled-1"
		if err := testDB.DB.Create(&model.Session{
			SessionID:      sessionID,
			OwnerID:        ownerID,
			SessionType:    2,
			GroupName:      "QR Disabled Group",
			LastMsgSummary: "QR Disabled Group",
		}).Error; err != nil {
			t.Fatalf("create session error: %v", err)
		}
		if err := testDB.DB.Model(&model.Session{}).
			Where("session_id = ?", sessionID).
			Update("allow_member_invite", false).Error; err != nil {
			t.Fatalf("disable group invite error: %v", err)
		}
		if err := testDB.DB.Create(&model.SessionMember{
			SessionID:    sessionID,
			MemberID:     ownerID,
			MemberType:   1,
			Role:         3,
			JoinedAt:     now,
			LastActiveAt: now,
		}).Error; err != nil {
			t.Fatalf("create owner membership error: %v", err)
		}

		reqGetQR, _ := http.NewRequest(http.MethodGet, "/sessions/group/qr?session_id="+sessionID, nil)
		reqGetQR.Header.Set("Authorization", ownerToken)
		wGetQR := httptest.NewRecorder()
		r.ServeHTTP(wGetQR, reqGetQR)
		if wGetQR.Code != http.StatusOK {
			t.Fatalf("expected status 200 for get qr, got %d, body=%s", wGetQR.Code, wGetQR.Body.String())
		}

		var getQRResp map[string]interface{}
		if err := json.Unmarshal(wGetQR.Body.Bytes(), &getQRResp); err != nil {
			t.Fatalf("unmarshal get qr response error: %v", err)
		}
		code := getQRResp["data"].(map[string]interface{})["code"].(string)

		joinBody, _ := json.Marshal(map[string]string{"code": code})
		reqJoin, _ := http.NewRequest(http.MethodPost, "/sessions/group/join_by_qr", bytes.NewReader(joinBody))
		reqJoin.Header.Set("Authorization", joinerToken)
		reqJoin.Header.Set("Content-Type", "application/json")
		wJoin := httptest.NewRecorder()
		r.ServeHTTP(wJoin, reqJoin)
		if wJoin.Code != http.StatusForbidden {
			t.Fatalf("expected status 403 for invite disabled join, got %d, body=%s", wJoin.Code, wJoin.Body.String())
		}

		var joinResp map[string]interface{}
		if err := json.Unmarshal(wJoin.Body.Bytes(), &joinResp); err != nil {
			t.Fatalf("unmarshal join response error: %v", err)
		}
		if toInt64(joinResp["code"]) != 40031 {
			t.Fatalf("expected error code 40031, got %v", joinResp["code"])
		}
	})

	t.Run("returns 40032 when threshold reached", func(t *testing.T) {
		if err := systemsetting.SaveGroupSettings(systemsetting.GroupSettings{
			MemberInviteThreshold: 1,
		}, nil); err != nil {
			t.Fatalf("save group settings error: %v", err)
		}
		t.Cleanup(func() {
			systemsetting.InvalidateGroupSettingsCache()
			if err := systemsetting.SaveGroupSettings(systemsetting.DefaultGroupSettings(), nil); err != nil {
				t.Fatalf("restore group settings error: %v", err)
			}
		})

		const sessionID = "session-group-qr-handler-threshold-1"
		if err := testDB.DB.Create(&model.Session{
			SessionID:         sessionID,
			OwnerID:           ownerID,
			SessionType:       2,
			GroupName:         "QR Threshold Group",
			AllowMemberInvite: true,
			LastMsgSummary:    "QR Threshold Group",
		}).Error; err != nil {
			t.Fatalf("create session error: %v", err)
		}
		if err := testDB.DB.Create([]model.SessionMember{
			{SessionID: sessionID, MemberID: ownerID, MemberType: 1, Role: 3, JoinedAt: now, LastActiveAt: now},
			{SessionID: sessionID, MemberID: memberID, MemberType: 1, Role: 1, JoinedAt: now, LastActiveAt: now},
		}).Error; err != nil {
			t.Fatalf("create members error: %v", err)
		}

		reqGetQR, _ := http.NewRequest(http.MethodGet, "/sessions/group/qr?session_id="+sessionID, nil)
		reqGetQR.Header.Set("Authorization", ownerToken)
		wGetQR := httptest.NewRecorder()
		r.ServeHTTP(wGetQR, reqGetQR)
		if wGetQR.Code != http.StatusOK {
			t.Fatalf("expected status 200 for get qr, got %d, body=%s", wGetQR.Code, wGetQR.Body.String())
		}

		var getQRResp map[string]interface{}
		if err := json.Unmarshal(wGetQR.Body.Bytes(), &getQRResp); err != nil {
			t.Fatalf("unmarshal get qr response error: %v", err)
		}
		code := getQRResp["data"].(map[string]interface{})["code"].(string)

		joinBody, _ := json.Marshal(map[string]string{"code": code})
		reqJoin, _ := http.NewRequest(http.MethodPost, "/sessions/group/join_by_qr", bytes.NewReader(joinBody))
		reqJoin.Header.Set("Authorization", joinerToken)
		reqJoin.Header.Set("Content-Type", "application/json")
		wJoin := httptest.NewRecorder()
		r.ServeHTTP(wJoin, reqJoin)
		if wJoin.Code != http.StatusForbidden {
			t.Fatalf("expected status 403 for threshold join, got %d, body=%s", wJoin.Code, wJoin.Body.String())
		}

		var joinResp map[string]interface{}
		if err := json.Unmarshal(wJoin.Body.Bytes(), &joinResp); err != nil {
			t.Fatalf("unmarshal join response error: %v", err)
		}
		if toInt64(joinResp["code"]) != 40032 {
			t.Fatalf("expected error code 40032, got %v", joinResp["code"])
		}
	})
}
