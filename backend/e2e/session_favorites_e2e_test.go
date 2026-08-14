package e2e

import (
	"net/http"
	"testing"
	"time"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/store"
)

// ensureFavoriteTable is a redundant guard that explicitly migrates the
// UserSessionFavorite model. The model is already registered in autoMigrateModels
// (store/migrate.go), so this is a no-op in practice but keeps the test self-contained.
func ensureFavoriteTable(t *testing.T) {
	t.Helper()
	if err := store.DB.AutoMigrate(&model.UserSessionFavorite{}); err != nil {
		t.Fatalf("automigrate UserSessionFavorite: %v", err)
	}
}

func seedFavoriteGroupSession(t *testing.T, sessionID string, ownerID int64) {
	t.Helper()
	if err := store.DB.Create(&model.Session{
		SessionID:   sessionID,
		OwnerID:     ownerID,
		SessionType: 2,
		GroupName:   "fav-test-" + sessionID,
	}).Error; err != nil {
		t.Fatalf("create session: %v", err)
	}
	if err := store.DB.Create(&model.SessionMember{
		SessionID:    sessionID,
		MemberID:     ownerID,
		MemberType:   1,
		JoinedAt:     time.Now(),
		LastActiveAt: time.Now(),
	}).Error; err != nil {
		t.Fatalf("create member: %v", err)
	}
}

func TestHTTP_SessionFavorite_FullFlow(t *testing.T) {
	ctx := setupE2E(t)
	ensureFavoriteTable(t)

	token, userID := ctx.loginHelper(t, "favuser", "Aa123456!")
	seedFavoriteGroupSession(t, "fav_sess_1", userID)
	seedFavoriteGroupSession(t, "fav_sess_2", userID)

	// 1. status: not favorited yet
	w := ctx.doReq(t, "GET", "/v1/sessions/fav_sess_1/favorite", token, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("status code=%d body=%s", w.Code, w.Body.String())
	}
	res := parseResp(t, w)
	if data, _ := res["data"].(map[string]interface{}); data["is_favorited"] != false {
		t.Fatalf("expected is_favorited=false, got %v", res)
	}

	// 2. add
	w = ctx.doReq(t, "POST", "/v1/sessions/fav_sess_1/favorite", token, map[string]interface{}{})
	if w.Code != http.StatusOK {
		t.Fatalf("add code=%d body=%s", w.Code, w.Body.String())
	}

	// 3. add second
	w = ctx.doReq(t, "POST", "/v1/sessions/fav_sess_2/favorite", token, map[string]interface{}{})
	if w.Code != http.StatusOK {
		t.Fatalf("add2 code=%d body=%s", w.Code, w.Body.String())
	}

	// 4. status: favorited
	w = ctx.doReq(t, "GET", "/v1/sessions/fav_sess_1/favorite", token, nil)
	res = parseResp(t, w)
	if data, _ := res["data"].(map[string]interface{}); data["is_favorited"] != true {
		t.Fatalf("expected is_favorited=true, got %v", res)
	}

	// 5. ids list
	w = ctx.doReq(t, "GET", "/v1/sessions/favorites/ids", token, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("ids code=%d body=%s", w.Code, w.Body.String())
	}
	res = parseResp(t, w)
	data := res["data"].(map[string]interface{})
	ids := data["session_ids"].([]interface{})
	if len(ids) != 2 {
		t.Fatalf("expected 2 ids, got %v", ids)
	}

	// 6. favorites listing
	w = ctx.doReq(t, "GET", "/v1/sessions/favorites?limit=50", token, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("list code=%d body=%s", w.Code, w.Body.String())
	}
	res = parseResp(t, w)
	data = res["data"].(map[string]interface{})
	list := data["list"].([]interface{})
	if len(list) != 2 {
		t.Fatalf("expected 2 favorites, got %v", list)
	}

	// 7. add non-member session → 403
	if err := store.DB.Create(&model.Session{
		SessionID:   "other_sess",
		OwnerID:     userID + 999,
		SessionType: 2,
		GroupName:   "other",
	}).Error; err != nil {
		t.Fatalf("seed other: %v", err)
	}
	w = ctx.doReq(t, "POST", "/v1/sessions/other_sess/favorite", token, map[string]interface{}{})
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 non-member, got %d body=%s", w.Code, w.Body.String())
	}

	// 8. remove
	w = ctx.doReq(t, "DELETE", "/v1/sessions/fav_sess_1/favorite", token, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("remove code=%d body=%s", w.Code, w.Body.String())
	}

	// 9. remove again → 404
	w = ctx.doReq(t, "DELETE", "/v1/sessions/fav_sess_1/favorite", token, nil)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404 on second remove, got %d body=%s", w.Code, w.Body.String())
	}

	// 10. unauthenticated → 401
	w = ctx.doReq(t, "GET", "/v1/sessions/favorites/ids", "", nil)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 unauth, got %d", w.Code)
	}
}
