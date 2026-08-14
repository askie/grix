package e2e

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReachABTest_CreateAndStats(t *testing.T) {
	ctx := setupE2E(t)
	adminToken := seedAdminSession(t)

	w := ctx.doReq(t, "POST", "/admin/api/reach/templates", adminToken, map[string]interface{}{
		"name": "AB-A", "title": "Variant A",
	})
	require.Equal(t, http.StatusOK, w.Code)
	resp := parseResp(t, w)
	tplAID := resp["data"].(map[string]interface{})["id"]

	w = ctx.doReq(t, "POST", "/admin/api/reach/templates", adminToken, map[string]interface{}{
		"name": "AB-B", "title": "Variant B",
	})
	require.Equal(t, http.StatusOK, w.Code)
	resp = parseResp(t, w)
	tplBID := resp["data"].(map[string]interface{})["id"]

	w = ctx.doReq(t, "POST", "/admin/api/reach/tasks/ab-test", adminToken, map[string]interface{}{
		"variants": []map[string]interface{}{
			{"variant": "A", "template_id": tplAID},
			{"variant": "B", "template_id": tplBID},
		},
		"channels": []string{"in_app"},
	})
	require.Equal(t, http.StatusOK, w.Code, "body=%s", w.Body.String())
	resp = parseResp(t, w)
	data := resp["data"].(map[string]interface{})
	groupID := data["ab_group_id"].(string)
	assert.NotEmpty(t, groupID)
	tasks := data["tasks"].([]interface{})
	assert.Len(t, tasks, 2)

	w = ctx.doReq(t, "GET", "/admin/api/reach/ab/"+groupID+"/stats", adminToken, nil)
	require.Equal(t, http.StatusOK, w.Code)
	resp = parseResp(t, w)
	statsData := resp["data"].(map[string]interface{})
	variants := statsData["variants"].([]interface{})
	assert.Len(t, variants, 2)
}

func TestReachABTest_TooFewVariants(t *testing.T) {
	ctx := setupE2E(t)
	adminToken := seedAdminSession(t)

	w := ctx.doReq(t, "POST", "/admin/api/reach/templates", adminToken, map[string]interface{}{
		"name": "Single", "title": "Only One",
	})
	require.Equal(t, http.StatusOK, w.Code)
	resp := parseResp(t, w)
	tplID := resp["data"].(map[string]interface{})["id"]

	w = ctx.doReq(t, "POST", "/admin/api/reach/tasks/ab-test", adminToken, map[string]interface{}{
		"variants": []map[string]interface{}{
			{"variant": "A", "template_id": tplID},
		},
		"channels": []string{"in_app"},
	})
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestReachABTest_StatsNotFound(t *testing.T) {
	ctx := setupE2E(t)
	adminToken := seedAdminSession(t)

	w := ctx.doReq(t, "GET", "/admin/api/reach/ab/nonexistent/stats", adminToken, nil)
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestReachRegionalStats(t *testing.T) {
	ctx := setupE2E(t)
	adminToken := seedAdminSession(t)

	// The task stats endpoint should now include region_breakdown
	w := ctx.doReq(t, "POST", "/admin/api/reach/templates", adminToken, map[string]interface{}{
		"name": "Regional", "title": "Regional Test",
	})
	require.Equal(t, http.StatusOK, w.Code)
	resp := parseResp(t, w)
	tplID := resp["data"].(map[string]interface{})["id"]

	w = ctx.doReq(t, "POST", "/admin/api/reach/tasks/marketing", adminToken, map[string]interface{}{
		"template_id": tplID,
		"channels":    []string{"in_app"},
		"region":      "cn",
	})
	require.Equal(t, http.StatusOK, w.Code)
	resp = parseResp(t, w)
	data := resp["data"].(map[string]interface{})
	taskIDStr := data["id"].(string)

	w = ctx.doReq(t, "GET", "/admin/api/reach/tasks/"+taskIDStr+"/stats", adminToken, nil)
	require.Equal(t, http.StatusOK, w.Code)
	resp = parseResp(t, w)
	statsData := resp["data"].(map[string]interface{})
	_, ok := statsData["region_breakdown"]
	assert.True(t, ok, "should have region_breakdown in stats")
}
