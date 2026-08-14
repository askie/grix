package e2e

import (
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/askie/grix/backend/config"
	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/store"
)

func TestWidgetSessionMarkerAndDetail(t *testing.T) {
	ctx := setupE2E(t)
	defer ctx.cleanup()

	orig := config.C.Server.WidgetEnabled
	config.C.Server.WidgetEnabled = true
	t.Cleanup(func() {
		config.C.Server.WidgetEnabled = orig
	})

	token, userID := ctx.loginHelper(t, "widget_owner_e2e", "Password123", "device_widget_owner")
	now := time.Now().UTC()
	sessionID := "widget-e2e-session-1"

	if err := store.DB.Create(&model.Session{SessionID: sessionID, OwnerID: userID, SessionType: model.SessionTypeDirect, UpdatedAt: now, CreatedAt: now}).Error; err != nil {
		t.Fatalf("seed session error: %v", err)
	}
	if err := store.DB.Create(&model.SessionMember{SessionID: sessionID, MemberID: userID, MemberType: 1, Role: 3, LastActiveAt: now, JoinedAt: now}).Error; err != nil {
		t.Fatalf("seed owner member error: %v", err)
	}
	if err := store.DB.Create(&model.SessionMember{SessionID: sessionID, MemberID: 99887766, MemberType: 1, Role: 1, LastActiveAt: now, JoinedAt: now}).Error; err != nil {
		t.Fatalf("seed visitor member error: %v", err)
	}
	if err := store.DB.Create(&model.User{ID: 99887766, Username: "visitor_e2e", Email: "visitor_e2e@example.com", PasswordHash: "x"}).Error; err != nil {
		t.Fatalf("seed visitor user error: %v", err)
	}
	if err := store.DB.Create(&model.WidgetSite{ID: 8899001, OwnerUserID: userID, SiteKey: "wk_e2e", SiteSecretHash: "hash", SiteName: "E2E Site", Status: model.WidgetSiteStatusActive, CreatedAt: now, UpdatedAt: now}).Error; err != nil {
		t.Fatalf("seed widget site error: %v", err)
	}
	if err := store.DB.Create(&model.WidgetSession{ID: 8899002, SiteID: 8899001, OwnerUserID: userID, VisitorID: 99887766, VisitorKey: "vk_e2e", SessionID: sessionID, VisitorName: "E2E Visitor", VisitorEmail: "eve@example.com", LastPageURL: "https://example.com/p/1", Status: model.WidgetSessionStatusActive, CreatedAt: now, UpdatedAt: now, LastActiveAt: now}).Error; err != nil {
		t.Fatalf("seed widget session error: %v", err)
	}

	listResp := ctx.doReq(t, http.MethodGet, "/v1/sessions/list?limit=20&offset=0", token, nil)
	if listResp.Code != http.StatusOK {
		t.Fatalf("list status=%d body=%s", listResp.Code, listResp.Body.String())
	}
	listBody := parseResp(t, listResp)
	data, ok := listBody["data"].(map[string]interface{})
	if !ok {
		t.Fatalf("invalid list data payload")
	}
	rows, ok := data["list"].([]interface{})
	if !ok {
		t.Fatalf("invalid list rows payload")
	}
	foundVisitor := false
	for _, row := range rows {
		m, ok := row.(map[string]interface{})
		if !ok {
			continue
		}
		if m["session_id"] == sessionID {
			if m["is_visitor"] != true {
				t.Fatalf("expected is_visitor=true, got=%v", m["is_visitor"])
			}
			foundVisitor = true
			break
		}
	}
	if !foundVisitor {
		t.Fatalf("visitor session not found in list")
	}

	detailResp := ctx.doReq(t, http.MethodGet, "/v1/sessions/detail?session_id="+sessionID, token, nil)
	if detailResp.Code != http.StatusOK {
		t.Fatalf("detail status=%d body=%s", detailResp.Code, detailResp.Body.String())
	}
	detailBody := parseResp(t, detailResp)
	detailData, ok := detailBody["data"].(map[string]interface{})
	if !ok {
		t.Fatalf("invalid detail data payload")
	}
	if detailData["is_visitor"] != true {
		t.Fatalf("expected detail is_visitor=true, got=%v", detailData["is_visitor"])
	}
	visitorInfo, ok := detailData["visitor_info"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected visitor_info map, got=%T", detailData["visitor_info"])
	}
	if visitorInfo["site_name"] != "E2E Site" {
		t.Fatalf("unexpected site_name=%v", visitorInfo["site_name"])
	}
	if visitorInfo["visitor_name"] != "E2E Visitor" {
		t.Fatalf("unexpected visitor_name=%v", visitorInfo["visitor_name"])
	}
	if visitorInfo["site_id"] != strconv.FormatInt(8899001, 10) {
		t.Fatalf("unexpected site_id=%v", visitorInfo["site_id"])
	}
}
