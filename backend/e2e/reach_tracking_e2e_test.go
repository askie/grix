package e2e

import (
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

func TestReachTracking_OpenPixel(t *testing.T) {
	ctx := setupE2E(t)

	logID := snowflake.GenID()
	require.NoError(t, store.DB.Create(&model.ReachSendLog{
		ID: logID, TaskID: snowflake.GenID(), UserID: 1,
		Channel: model.ReachChannelEmail, Status: model.ReachSendStatusSent,
	}).Error)

	w := ctx.doReq(t, "GET", fmt.Sprintf("/v1/reach/t/o/%d", logID), "", nil)
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "image/gif", w.Header().Get("Content-Type"))
	assert.True(t, w.Body.Len() > 0, "should return GIF data")

	var log model.ReachSendLog
	require.NoError(t, store.DB.Where("id = ?", logID).First(&log).Error)
	assert.NotNil(t, log.OpenedAt)
}

func TestReachTracking_ClickRedirect(t *testing.T) {
	ctx := setupE2E(t)

	logID := snowflake.GenID()
	require.NoError(t, store.DB.Create(&model.ReachSendLog{
		ID: logID, TaskID: snowflake.GenID(), UserID: 1,
		Channel: model.ReachChannelEmail, Status: model.ReachSendStatusSent,
	}).Error)

	w := ctx.doReq(t, "GET", fmt.Sprintf("/v1/reach/t/c/%d?url=https://example.com", logID), "", nil)
	assert.Equal(t, http.StatusFound, w.Code)
	assert.Equal(t, "https://example.com", w.Header().Get("Location"))

	var log model.ReachSendLog
	require.NoError(t, store.DB.Where("id = ?", logID).First(&log).Error)
	assert.NotNil(t, log.ClickedAt)
}

func TestReachTracking_InvalidID(t *testing.T) {
	ctx := setupE2E(t)

	w := ctx.doReq(t, "GET", "/v1/reach/t/o/invalid", "", nil)
	assert.Equal(t, http.StatusOK, w.Code, "should still return pixel even for invalid IDs")
}

func TestReachTracking_StatsIncludeOpenClick(t *testing.T) {
	ctx := setupE2E(t)
	adminToken := seedAdminSession(t)

	taskID := snowflake.GenID()
	require.NoError(t, store.DB.Create(&model.ReachTask{
		ID: taskID, Kind: model.ReachKindMarketing,
		Channels: []byte(`["email"]`), Status: model.ReachStatusSent,
		Stats: []byte(`{}`), CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	}).Error)

	now := time.Now().UTC()
	store.DB.Create(&model.ReachSendLog{
		ID: snowflake.GenID(), TaskID: taskID, UserID: 1,
		Channel: model.ReachChannelEmail, Status: model.ReachSendStatusSent,
		OpenedAt: &now,
	})
	store.DB.Create(&model.ReachSendLog{
		ID: snowflake.GenID(), TaskID: taskID, UserID: 2,
		Channel: model.ReachChannelEmail, Status: model.ReachSendStatusSent,
		OpenedAt: &now, ClickedAt: &now,
	})
	store.DB.Create(&model.ReachSendLog{
		ID: snowflake.GenID(), TaskID: taskID, UserID: 3,
		Channel: model.ReachChannelEmail, Status: model.ReachSendStatusSent,
	})

	w := ctx.doReq(t, "GET", fmt.Sprintf("/admin/api/reach/tasks/%d/stats", taskID), adminToken, nil)
	require.Equal(t, http.StatusOK, w.Code)
	resp := parseResp(t, w)
	data := resp["data"].(map[string]interface{})
	assert.NotNil(t, data["opened"])
	assert.NotNil(t, data["clicked"])
	assert.NotNil(t, data["open_rate"])
	assert.NotNil(t, data["click_rate"])
}
