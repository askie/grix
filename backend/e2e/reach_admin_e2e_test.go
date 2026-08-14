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
	"github.com/askie/grix/backend/internal/systemsetting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func seedReachTask(t *testing.T, status string) int64 {
	t.Helper()
	// Windows 时钟粒度粗，UnixNano 连续取值会重复导致主键冲突，改用雪花 ID。
	id := snowflake.GenID()
	task := &model.ReachTask{
		ID:        id,
		Kind:      model.ReachKindSystemEvent,
		EventKey:  "app_release_published",
		Channels:  []byte(`["in_app","push"]`),
		Status:    status,
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}
	require.NoError(t, store.DB.Create(task).Error)
	return id
}

func TestReachAdminAPI_ListTasks(t *testing.T) {
	ctx := setupE2E(t)
	adminToken := seedAdminSession(t)

	seedReachTask(t, model.ReachStatusSending)
	seedReachTask(t, model.ReachStatusSent)
	seedReachTask(t, model.ReachStatusPaused)

	w := ctx.doReq(t, "GET", "/admin/api/reach/tasks?page=1&page_size=10", adminToken, nil)
	require.Equal(t, http.StatusOK, w.Code, "body=%s", w.Body.String())
	resp := parseResp(t, w)
	data := resp["data"].(map[string]interface{})
	totalNum, _ := data["total"].(json.Number)
	total, _ := totalNum.Int64()
	assert.GreaterOrEqual(t, total, int64(3))
	tasks := data["tasks"].([]interface{})
	assert.GreaterOrEqual(t, len(tasks), 3)
}

func TestReachAdminAPI_ListTasks_FilterByStatus(t *testing.T) {
	ctx := setupE2E(t)
	adminToken := seedAdminSession(t)

	seedReachTask(t, model.ReachStatusSending)
	seedReachTask(t, model.ReachStatusSent)

	w := ctx.doReq(t, "GET", "/admin/api/reach/tasks?status=sending", adminToken, nil)
	require.Equal(t, http.StatusOK, w.Code)
	resp := parseResp(t, w)
	data := resp["data"].(map[string]interface{})
	tasks := data["tasks"].([]interface{})
	for _, item := range tasks {
		task := item.(map[string]interface{})
		assert.Equal(t, "sending", task["status"])
	}
}

func TestReachAdminAPI_GetTask(t *testing.T) {
	ctx := setupE2E(t)
	adminToken := seedAdminSession(t)

	taskID := seedReachTask(t, model.ReachStatusSent)

	w := ctx.doReq(t, "GET", fmt.Sprintf("/admin/api/reach/tasks/%d", taskID), adminToken, nil)
	require.Equal(t, http.StatusOK, w.Code)
	resp := parseResp(t, w)
	data := resp["data"].(map[string]interface{})
	assert.Equal(t, "sent", data["status"])
}

func TestReachAdminAPI_GetTask_NotFound(t *testing.T) {
	ctx := setupE2E(t)
	adminToken := seedAdminSession(t)

	w := ctx.doReq(t, "GET", "/admin/api/reach/tasks/999999999", adminToken, nil)
	require.Equal(t, http.StatusNotFound, w.Code)
}

func TestReachAdminAPI_PauseTask(t *testing.T) {
	ctx := setupE2E(t)
	adminToken := seedAdminSession(t)

	taskID := seedReachTask(t, model.ReachStatusSending)

	w := ctx.doReq(t, "POST", fmt.Sprintf("/admin/api/reach/tasks/%d/pause", taskID), adminToken, nil)
	require.Equal(t, http.StatusOK, w.Code, "body=%s", w.Body.String())

	var loaded model.ReachTask
	require.NoError(t, store.DB.Where("id = ?", taskID).First(&loaded).Error)
	assert.Equal(t, model.ReachStatusPaused, loaded.Status)
}

func TestReachAdminAPI_PauseTask_AlreadyPaused(t *testing.T) {
	ctx := setupE2E(t)
	adminToken := seedAdminSession(t)

	taskID := seedReachTask(t, model.ReachStatusPaused)

	w := ctx.doReq(t, "POST", fmt.Sprintf("/admin/api/reach/tasks/%d/pause", taskID), adminToken, nil)
	assert.Equal(t, http.StatusConflict, w.Code)
}

func TestReachAdminAPI_CancelTask(t *testing.T) {
	ctx := setupE2E(t)
	adminToken := seedAdminSession(t)

	taskID := seedReachTask(t, model.ReachStatusSending)

	w := ctx.doReq(t, "POST", fmt.Sprintf("/admin/api/reach/tasks/%d/cancel", taskID), adminToken, nil)
	require.Equal(t, http.StatusOK, w.Code)

	var loaded model.ReachTask
	require.NoError(t, store.DB.Where("id = ?", taskID).First(&loaded).Error)
	assert.Equal(t, model.ReachStatusCancelled, loaded.Status)
}

func TestReachAdminAPI_CancelTask_AlreadySent(t *testing.T) {
	ctx := setupE2E(t)
	adminToken := seedAdminSession(t)

	taskID := seedReachTask(t, model.ReachStatusSent)

	w := ctx.doReq(t, "POST", fmt.Sprintf("/admin/api/reach/tasks/%d/cancel", taskID), adminToken, nil)
	assert.Equal(t, http.StatusConflict, w.Code)
}

func TestReachAdminAPI_Unauthenticated(t *testing.T) {
	ctx := setupE2E(t)

	w := ctx.doReq(t, "GET", "/admin/api/reach/tasks", "", nil)
	assert.NotEqual(t, http.StatusOK, w.Code)
}

func seedReachDraftTask(t *testing.T, content string) int64 {
	t.Helper()
	id := snowflake.GenID()
	task := &model.ReachTask{
		ID:        id,
		Kind:      model.ReachKindSystemEvent,
		EventKey:  "app_release_published",
		Channels:  []byte(`["in_app","push"]`),
		Content:   []byte(content),
		Status:    model.ReachStatusDraft,
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}
	require.NoError(t, store.DB.Create(task).Error)
	return id
}

func TestReachAdminAPI_UpdateDraftContent(t *testing.T) {
	ctx := setupE2E(t)
	adminToken := seedAdminSession(t)

	taskID := seedReachDraftTask(t, `{}`)

	body := map[string]interface{}{
		"zh": map[string]string{"title": "Grix 9.9.9 新版本已发布", "body": "更新内容A", "email_subject": "Grix 9.9.9", "email_intro": "快来更新"},
		"en": map[string]string{"title": "Grix 9.9.9 is now available", "body": "Changes A", "email_subject": "Grix 9.9.9", "email_intro": "Update now"},
	}
	w := ctx.doReq(t, "PUT", fmt.Sprintf("/admin/api/reach/tasks/%d/content", taskID), adminToken, body)
	require.Equal(t, http.StatusOK, w.Code, "body=%s", w.Body.String())

	var loaded model.ReachTask
	require.NoError(t, store.DB.Where("id = ?", taskID).First(&loaded).Error)
	assert.Contains(t, string(loaded.Content), "更新内容A")

	// 缺英文标题 → 校验失败
	bad := map[string]interface{}{
		"zh": map[string]string{"title": "只有中文标题"},
		"en": map[string]string{"title": " "},
	}
	w = ctx.doReq(t, "PUT", fmt.Sprintf("/admin/api/reach/tasks/%d/content", taskID), adminToken, bad)
	assert.Equal(t, http.StatusConflict, w.Code)
}

func TestReachAdminAPI_UpdateContent_NonDraftRejected(t *testing.T) {
	ctx := setupE2E(t)
	adminToken := seedAdminSession(t)

	taskID := seedReachTask(t, model.ReachStatusSent)

	body := map[string]interface{}{
		"zh": map[string]string{"title": "改不了"},
		"en": map[string]string{"title": "nope"},
	}
	w := ctx.doReq(t, "PUT", fmt.Sprintf("/admin/api/reach/tasks/%d/content", taskID), adminToken, body)
	assert.Equal(t, http.StatusConflict, w.Code)
}

func TestReachAdminAPI_CancelDraft(t *testing.T) {
	ctx := setupE2E(t)
	adminToken := seedAdminSession(t)

	taskID := seedReachDraftTask(t, `{}`)

	w := ctx.doReq(t, "POST", fmt.Sprintf("/admin/api/reach/tasks/%d/cancel", taskID), adminToken, nil)
	require.Equal(t, http.StatusOK, w.Code)

	var loaded model.ReachTask
	require.NoError(t, store.DB.Where("id = ?", taskID).First(&loaded).Error)
	assert.Equal(t, model.ReachStatusCancelled, loaded.Status)
}

func TestReachAdminAPI_SendDraft(t *testing.T) {
	ctx := setupE2E(t)
	adminToken := seedAdminSession(t)

	csID := snowflake.GenID()
	customer := &model.User{ID: csID, Username: fmt.Sprintf("cs%d", csID), Email: fmt.Sprintf("cs%d@test.local", csID), Nickname: "客服", Status: model.UserStatusActive}
	require.NoError(t, store.DB.Create(customer).Error)
	require.NoError(t, systemsetting.SaveAuthSettings(
		systemsetting.AuthSettings{AutoAddCustomerUserID: customer.ID}, nil))
	t.Cleanup(systemsetting.InvalidateAuthSettingsCache)

	target := &model.User{ID: customer.ID + 1, Username: fmt.Sprintf("u%d", customer.ID), Email: fmt.Sprintf("u%d@test.local", customer.ID), Nickname: "用户", Status: model.UserStatusActive}
	require.NoError(t, store.DB.Create(target).Error)

	taskID := seedReachDraftTask(t, `{"zh":{"title":"Grix 8.8.8 新版本已发布","body":"内容B"},"en":{"title":"Grix 8.8.8 is now available","body":"Changes B"}}`)

	w := ctx.doReq(t, "POST", fmt.Sprintf("/admin/api/reach/tasks/%d/send", taskID), adminToken, nil)
	require.Equal(t, http.StatusOK, w.Code, "body=%s", w.Body.String())

	require.Eventually(t, func() bool {
		var loaded model.ReachTask
		if err := store.DB.Where("id = ?", taskID).First(&loaded).Error; err != nil {
			return false
		}
		return loaded.Status == model.ReachStatusSent
	}, 5*time.Second, 20*time.Millisecond)

	var msg model.Message
	require.NoError(t, store.DB.Where("sender_id = ?", customer.ID).First(&msg).Error)
	assert.Contains(t, msg.Content, "8.8.8")

	// 二次发送必须被拒绝
	w = ctx.doReq(t, "POST", fmt.Sprintf("/admin/api/reach/tasks/%d/send", taskID), adminToken, nil)
	assert.Equal(t, http.StatusConflict, w.Code)
}

func TestReachAdminAPI_SendNonDraftRejected(t *testing.T) {
	ctx := setupE2E(t)
	adminToken := seedAdminSession(t)

	taskID := seedReachTask(t, model.ReachStatusSent)
	w := ctx.doReq(t, "POST", fmt.Sprintf("/admin/api/reach/tasks/%d/send", taskID), adminToken, nil)
	assert.Equal(t, http.StatusConflict, w.Code)
}
