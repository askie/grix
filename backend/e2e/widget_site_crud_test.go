package e2e

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// assertCodeOK checks that the response code equals 0 (json.Number or float64).
func assertCodeOK(t *testing.T, res map[string]interface{}) {
	t.Helper()
	code := res["code"]
	switch v := code.(type) {
	case json.Number:
		assert.Equal(t, "0", v.String())
	case float64:
		assert.Equal(t, float64(0), v)
	default:
		assert.Equal(t, 0, code)
	}
}

// assertCodeNotOK checks that the response code is NOT 0.
func assertCodeNotOK(t *testing.T, res map[string]interface{}) {
	t.Helper()
	code := res["code"]
	switch v := code.(type) {
	case json.Number:
		assert.NotEqual(t, "0", v.String())
	case float64:
		assert.NotEqual(t, float64(0), v)
	}
}

// assertCodeEquals checks that the response code equals the expected value.
func assertCodeEquals(t *testing.T, expected int, res map[string]interface{}) {
	t.Helper()
	code := res["code"]
	switch v := code.(type) {
	case json.Number:
		n, _ := v.Int64()
		assert.Equal(t, int64(expected), n)
	case float64:
		assert.Equal(t, float64(expected), v)
	}
}

// TestHTTP_WidgetSiteCRUD tests the full create→detail→update→delete lifecycle.
func TestHTTP_WidgetSiteCRUD(t *testing.T) {
	ctx := setupE2E(t)
	defer ctx.cleanup()

	token, _ := ctx.loginHelper(t, "ws_e2e_user", "password123")
	assert.NotEmpty(t, token)

	// ── 1. Create ──
	w := ctx.doReq(t, "POST", "/v1/widget/sites/create", token, map[string]interface{}{
		"site_name":       "E2E测试站点",
		"allowed_origins": []string{"https://test.example.com"},
	})
	assert.Equal(t, http.StatusOK, w.Code)

	res := parseResp(t, w)
	assertCodeOK(t, res)
	data := res["data"].(map[string]interface{})
	site := data["site"].(map[string]interface{})
	siteID := site["id"].(string)
	siteKey := site["site_key"].(string)
	siteName := site["site_name"].(string)
	assert.Equal(t, "E2E测试站点", siteName)
	require.NotEmpty(t, siteID)
	require.NotEmpty(t, siteKey)
	t.Logf("created site: id=%s key=%s", siteID, siteKey)

	// ── 2. Detail ──
	w = ctx.doReq(t, "GET", "/v1/widget/sites/detail?id="+siteID, token, nil)
	assert.Equal(t, http.StatusOK, w.Code)
	res = parseResp(t, w)
	detailData := res["data"].(map[string]interface{})
	detailSite := detailData["site"].(map[string]interface{})
	assert.Equal(t, siteID, detailSite["id"])
	assert.Equal(t, siteKey, detailSite["site_key"])
	assert.Contains(t, detailData, "embed_code")

	// ── 3. Update (rename + add domain, key should stay unchanged) ──
	w = ctx.doReq(t, "POST", "/v1/widget/sites/update", token, map[string]interface{}{
		"id":              siteID,
		"site_name":       "E2E已改名",
		"allowed_origins": []string{"https://test.example.com", "https://new.example.com"},
		"status":          float64(1),
	})
	assert.Equal(t, http.StatusOK, w.Code)
	res = parseResp(t, w)
	updateData := res["data"].(map[string]interface{})
	assert.Equal(t, "E2E已改名", updateData["site_name"])
	// site_key must NOT change after update
	assert.Equal(t, siteKey, updateData["site_key"])
	t.Logf("updated site: name=%s key=%s (unchanged)", updateData["site_name"], updateData["site_key"])

	// ── 4. Verify detail after update ──
	w = ctx.doReq(t, "GET", "/v1/widget/sites/detail?id="+siteID, token, nil)
	assert.Equal(t, http.StatusOK, w.Code)
	res = parseResp(t, w)
	detailSite = res["data"].(map[string]interface{})["site"].(map[string]interface{})
	assert.Equal(t, "E2E已改名", detailSite["site_name"])
	assert.Equal(t, siteKey, detailSite["site_key"])
	origins := detailSite["allowed_origins"].([]interface{})
	assert.Equal(t, 2, len(origins))

	// ── 5. Delete ──
	w = ctx.doReq(t, "POST", "/v1/widget/sites/delete", token, map[string]interface{}{
		"id": siteID,
	})
	assert.Equal(t, http.StatusOK, w.Code)
	res = parseResp(t, w)
	assertCodeOK(t, res)
	t.Logf("deleted site: id=%s", siteID)

	// ── 6. Detail after delete → should return HTTP 404 with body code 4004 ──
	w = ctx.doReq(t, "GET", "/v1/widget/sites/detail?id="+siteID, token, nil)
	assert.Equal(t, http.StatusNotFound, w.Code)
	res = parseResp(t, w)
	assertCodeEquals(t, 4004, res)
	t.Logf("confirmed 404 after delete: code=%v msg=%v", res["code"], res["msg"])
}

// TestHTTP_WidgetSiteDeleteNotOwned ensures a user cannot delete another user's site.
func TestHTTP_WidgetSiteDeleteNotOwned(t *testing.T) {
	ctx := setupE2E(t)
	defer ctx.cleanup()

	tokenA, _ := ctx.loginHelper(t, "ws_user_a", "password123")
	tokenB, _ := ctx.loginHelper(t, "ws_user_b", "password123")

	// User A creates a site
	w := ctx.doReq(t, "POST", "/v1/widget/sites/create", tokenA, map[string]interface{}{
		"site_name":       "A的站点",
		"allowed_origins": []string{"https://a.example.com"},
	})
	assert.Equal(t, http.StatusOK, w.Code)
	res := parseResp(t, w)
	siteID := res["data"].(map[string]interface{})["site"].(map[string]interface{})["id"].(string)

	// User B tries to delete User A's site → should fail with 404
	w = ctx.doReq(t, "POST", "/v1/widget/sites/delete", tokenB, map[string]interface{}{
		"id": siteID,
	})
	assert.Equal(t, http.StatusNotFound, w.Code)
	res = parseResp(t, w)
	assertCodeEquals(t, 4004, res)
	t.Logf("cross-user delete blocked: code=%v msg=%v", res["code"], res["msg"])

	// Verify site still exists for user A
	w = ctx.doReq(t, "GET", "/v1/widget/sites/detail?id="+siteID, tokenA, nil)
	assert.Equal(t, http.StatusOK, w.Code)
	res = parseResp(t, w)
	assertCodeOK(t, res)
	t.Logf("site still exists for owner after failed cross-user delete")
}

// TestHTTP_WidgetSiteList verifies list endpoint works.
func TestHTTP_WidgetSiteList(t *testing.T) {
	ctx := setupE2E(t)
	defer ctx.cleanup()

	token, _ := ctx.loginHelper(t, "ws_list_user", "password123")

	// Create 2 sites
	for i := 0; i < 2; i++ {
		w := ctx.doReq(t, "POST", "/v1/widget/sites/create", token, map[string]interface{}{
			"site_name":       "List测试" + string(rune('A'+i)),
			"allowed_origins": []string{"https://list" + string(rune('A'+i)) + ".example.com"},
		})
		assert.Equal(t, http.StatusOK, w.Code)
	}

	// List all
	w := ctx.doReq(t, "GET", "/v1/widget/sites/list?limit=100", token, nil)
	assert.Equal(t, http.StatusOK, w.Code)
	res := parseResp(t, w)
	data := res["data"].(map[string]interface{})
	items := data["items"].([]interface{})
	assert.GreaterOrEqual(t, len(items), 2)
	t.Logf("listed %d sites", len(items))

	// Parse first item to validate structure
	first := items[0].(map[string]interface{})
	b, _ := json.Marshal(first)
	t.Logf("sample item: %s", string(b))
}
