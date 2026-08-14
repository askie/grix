package e2e

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReachTemplateAPI_CRUD(t *testing.T) {
	ctx := setupE2E(t)
	adminToken := seedAdminSession(t)

	// Create
	w := ctx.doReq(t, "POST", "/admin/api/reach/templates", adminToken, map[string]interface{}{
		"name":       "夏季促销",
		"title":      "夏日特惠",
		"in_app_body": "全场五折起",
		"push_body":  "夏日特惠来了",
		"email_html": "<h1>五折</h1>",
	})
	require.Equal(t, http.StatusOK, w.Code, "body=%s", w.Body.String())
	resp := parseResp(t, w)
	data := resp["data"].(map[string]interface{})
	tplID := data["id"]

	// Get
	w = ctx.doReq(t, "GET", fmt.Sprintf("/admin/api/reach/templates/%s", tplID), adminToken, nil)
	require.Equal(t, http.StatusOK, w.Code)
	resp = parseResp(t, w)
	data = resp["data"].(map[string]interface{})
	assert.Equal(t, "夏季促销", data["name"])

	// Update
	w = ctx.doReq(t, "PUT", fmt.Sprintf("/admin/api/reach/templates/%s", tplID), adminToken, map[string]interface{}{
		"name": "秋季促销",
	})
	require.Equal(t, http.StatusOK, w.Code)
	resp = parseResp(t, w)
	data = resp["data"].(map[string]interface{})
	assert.Equal(t, "秋季促销", data["name"])
	assert.Equal(t, "夏日特惠", data["title"], "unchanged fields preserved")

	// List
	w = ctx.doReq(t, "GET", "/admin/api/reach/templates?page=1&page_size=10", adminToken, nil)
	require.Equal(t, http.StatusOK, w.Code)
	resp = parseResp(t, w)
	data = resp["data"].(map[string]interface{})
	totalNum, _ := data["total"].(json.Number)
	total, _ := totalNum.Int64()
	assert.GreaterOrEqual(t, total, int64(1))

	// Delete
	w = ctx.doReq(t, "DELETE", fmt.Sprintf("/admin/api/reach/templates/%s", tplID), adminToken, nil)
	require.Equal(t, http.StatusOK, w.Code)

	w = ctx.doReq(t, "GET", fmt.Sprintf("/admin/api/reach/templates/%s", tplID), adminToken, nil)
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestReachTemplateAPI_CreateMissingFields(t *testing.T) {
	ctx := setupE2E(t)
	adminToken := seedAdminSession(t)

	w := ctx.doReq(t, "POST", "/admin/api/reach/templates", adminToken, map[string]interface{}{
		"title": "only title",
	})
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestReachMarketingTaskAPI_Create(t *testing.T) {
	ctx := setupE2E(t)
	adminToken := seedAdminSession(t)

	// Create a template first
	w := ctx.doReq(t, "POST", "/admin/api/reach/templates", adminToken, map[string]interface{}{
		"name":       "营销模板",
		"title":      "限时优惠",
		"in_app_body": "全场三折",
	})
	require.Equal(t, http.StatusOK, w.Code)
	resp := parseResp(t, w)
	data := resp["data"].(map[string]interface{})
	tplID := data["id"]

	// Create marketing task
	w = ctx.doReq(t, "POST", "/admin/api/reach/tasks/marketing", adminToken, map[string]interface{}{
		"template_id": tplID,
		"channels":    []string{"in_app"},
		"region":      "cn",
	})
	require.Equal(t, http.StatusOK, w.Code, "body=%s", w.Body.String())
	resp = parseResp(t, w)
	data = resp["data"].(map[string]interface{})
	assert.Equal(t, "marketing", data["kind"])
	assert.Equal(t, "sending", data["status"])
}

func TestReachMarketingTaskAPI_MissingTemplate(t *testing.T) {
	ctx := setupE2E(t)
	adminToken := seedAdminSession(t)

	w := ctx.doReq(t, "POST", "/admin/api/reach/tasks/marketing", adminToken, map[string]interface{}{
		"template_id": "999999999",
		"channels":    []string{"in_app"},
	})
	assert.Equal(t, http.StatusBadRequest, w.Code)
}
