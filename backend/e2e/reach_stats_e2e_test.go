package e2e

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/snowflake"
	"github.com/askie/grix/backend/internal/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestReachAdminAPI_TaskStats(t *testing.T) {
	ctx := setupE2E(t)
	adminToken := seedAdminSession(t)

	taskID := snowflake.GenID()
	statsJSON, _ := json.Marshal(map[string]int{"sent": 3, "skipped": 1})
	require.NoError(t, store.DB.Create(&model.ReachTask{
		ID: taskID, Kind: model.ReachKindSystemEvent,
		Channels: []byte(`["in_app"]`), Status: model.ReachStatusSent,
		Stats: statsJSON, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	}).Error)

	store.DB.Create(&model.ReachSendLog{
		ID: snowflake.GenID(), TaskID: taskID, UserID: 1,
		Channel: model.ReachChannelInApp, Status: model.ReachSendStatusSent,
	})
	store.DB.Create(&model.ReachSendLog{
		ID: snowflake.GenID(), TaskID: taskID, UserID: 2,
		Channel: model.ReachChannelInApp, Status: model.ReachSendStatusSent,
	})

	w := ctx.doReq(t, "GET", fmt.Sprintf("/admin/api/reach/tasks/%d/stats", taskID), adminToken, nil)
	require.Equal(t, http.StatusOK, w.Code, "body=%s", w.Body.String())
	resp := parseResp(t, w)
	data := resp["data"].(map[string]interface{})
	assert.Equal(t, "sent", data["status"])
	breakdown := data["channel_breakdown"].(map[string]interface{})
	inAppCount, _ := breakdown["in_app"].(json.Number)
	cnt, _ := inAppCount.Int64()
	assert.Equal(t, int64(2), cnt)
}

func TestReachAdminAPI_SubscriptionOverview(t *testing.T) {
	ctx := setupE2E(t)
	adminToken := seedAdminSession(t)

	w := ctx.doReq(t, "GET", "/admin/api/reach/subscriptions/overview", adminToken, nil)
	require.Equal(t, http.StatusOK, w.Code)
	resp := parseResp(t, w)
	data := resp["data"].(map[string]interface{})
	_, ok := data["total_subscriptions"]
	assert.True(t, ok, "should have total_subscriptions field")
}

func TestReachMarketingAPI_ScheduledTask(t *testing.T) {
	ctx := setupE2E(t)
	adminToken := seedAdminSession(t)

	w := ctx.doReq(t, "POST", "/admin/api/reach/templates", adminToken, map[string]interface{}{
		"name": "定时测试", "title": "定时标题",
	})
	require.Equal(t, http.StatusOK, w.Code)
	resp := parseResp(t, w)
	tplID := resp["data"].(map[string]interface{})["id"]

	future := time.Now().UTC().Add(1 * time.Hour).Format(time.RFC3339)
	w = ctx.doReq(t, "POST", "/admin/api/reach/tasks/marketing", adminToken, map[string]interface{}{
		"template_id":  tplID,
		"channels":     []string{"in_app"},
		"scheduled_at": future,
	})
	require.Equal(t, http.StatusOK, w.Code, "body=%s", w.Body.String())
	resp = parseResp(t, w)
	data := resp["data"].(map[string]interface{})
	assert.Equal(t, "scheduled", data["status"], "future task should be scheduled, not sending")
}

func TestReachMarketingAPI_AudienceFilter(t *testing.T) {
	ctx := setupE2E(t)
	adminToken := seedAdminSession(t)

	w := ctx.doReq(t, "POST", "/admin/api/reach/templates", adminToken, map[string]interface{}{
		"name": "筛选测试", "title": "筛选标题", "in_app_body": "内容",
	})
	require.Equal(t, http.StatusOK, w.Code)
	resp := parseResp(t, w)
	tplID := resp["data"].(map[string]interface{})["id"]

	w = ctx.doReq(t, "POST", "/admin/api/reach/tasks/marketing", adminToken, map[string]interface{}{
		"template_id": tplID,
		"channels":    []string{"in_app"},
		"region":      "cn",
		"audience": map[string]interface{}{
			"region":            "cn",
			"active_within_days": 30,
		},
	})
	require.Equal(t, http.StatusOK, w.Code, "body=%s", w.Body.String())
	resp = parseResp(t, w)
	data := resp["data"].(map[string]interface{})
	assert.Equal(t, "marketing", data["kind"])
}
