package e2e

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/askie/grix/backend/internal/model"
)

// TestPublicProfileNotFoundReturns200 验证：解析一个既非用户、也非本人访客的 ID 时，
// 接口返回 HTTP 200 + data=null，而不是 404，避免前端反复触发浏览器控制台的 404 噪音。
func TestPublicProfileNotFoundReturns200(t *testing.T) {
	ctx := setupE2E(t)
	defer ctx.db.Close()

	token, _ := ctx.loginHelper(t, "profile_viewer@example.com", "Passw0rd!A")

	w := ctx.doReq(t, http.MethodGet, "/v1/users/99990001/profile", token, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("expected HTTP 200 for missing profile, got %d, body: %s", w.Code, w.Body.String())
	}
	resp := parseResp(t, w)
	if code, _ := resp["code"].(float64); code != 0 {
		t.Fatalf("expected code 0, got %v", resp["code"])
	}
	if data, ok := resp["data"]; ok && data != nil {
		t.Fatalf("expected null data for missing profile, got %v", data)
	}
}

// TestPublicProfileVisitorFallback 验证：查不到正式用户时，回退查本人名下挂件访客，
// 返回访客名并带 is_visitor=true；非归属请求者读不到，退化为 200 空。
func TestPublicProfileVisitorFallback(t *testing.T) {
	ctx := setupE2E(t)
	defer ctx.db.Close()

	ownerToken, ownerID := ctx.loginHelper(t, "widget_owner@example.com", "Passw0rd!A")
	otherToken, _ := ctx.loginHelper(t, "widget_other@example.com", "Passw0rd!A")

	const visitorID int64 = 88880002
	if err := ctx.db.DB.Create(&model.WidgetSession{
		ID:          77770003,
		SiteID:      1,
		OwnerUserID: ownerID,
		VisitorID:   visitorID,
		VisitorKey:  "vkf_e2e",
		SessionID:   "sess-visitor-e2e",
		VisitorName: "访客阿强",
	}).Error; err != nil {
		t.Fatalf("create widget session: %v", err)
	}

	// 归属 owner 能拿到访客资料
	w := ctx.doReq(t, http.MethodGet, fmt.Sprintf("/v1/users/%d/profile", visitorID), ownerToken, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("expected HTTP 200 for visitor, got %d, body: %s", w.Code, w.Body.String())
	}
	resp := parseResp(t, w)
	data, ok := resp["data"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected visitor data object, got %v", resp["data"])
	}
	if isVisitor, _ := data["is_visitor"].(bool); !isVisitor {
		t.Errorf("expected is_visitor=true, got %v", data["is_visitor"])
	}
	if nickname, _ := data["nickname"].(string); nickname != "访客阿强" {
		t.Errorf("expected nickname '访客阿强', got %v", data["nickname"])
	}

	// 非归属用户读不到他人访客，退化为 200 空
	w2 := ctx.doReq(t, http.MethodGet, fmt.Sprintf("/v1/users/%d/profile", visitorID), otherToken, nil)
	if w2.Code != http.StatusOK {
		t.Fatalf("expected HTTP 200 for non-owner, got %d", w2.Code)
	}
	resp2 := parseResp(t, w2)
	if d, ok := resp2["data"]; ok && d != nil {
		t.Fatalf("expected null data for non-owner visitor lookup, got %v", d)
	}
}
