package service

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/reach"
	"github.com/askie/grix/backend/internal/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 可编辑字段（标题/导语/正文）进邮件 HTML 前必须转义，主题剥掉换行。
func TestReachAnnouncementEmailContent_EscapesEditableFields(t *testing.T) {
	content := DefaultAppReleaseAnnouncementContent("3.7.0", "a<b & c")
	content.ZH.Title = `<script>alert(1)</script>标题`
	content.ZH.EmailIntro = `<img src=x onerror=1>导语`
	content.ZH.EmailSubject = "第一行\r\n第二行"

	subject, body := ReachAnnouncementEmailContent(content, "zh")

	assert.NotContains(t, body, "<script>")
	assert.Contains(t, body, "&lt;script&gt;")
	assert.NotContains(t, body, "<img")
	assert.Contains(t, body, "a&lt;b &amp; c")
	assert.NotContains(t, subject, "\n")
	assert.NotContains(t, subject, "\r")
	assert.Contains(t, subject, "第一行")
}

// 与编辑接口同一口径：任一语言标题为空（脏数据绕过编辑校验写入）都拒发，
// 任务保持 draft。
func TestSendReachAnnouncement_RejectsPartialEmptyTitle(t *testing.T) {
	setupReachTestDB(t)
	seedReachCustomerAccount(t, 1001)

	task, created, err := createAppReleaseReachTask(reach.AppReleaseEvent{
		EventKey: reach.EventAppReleasePublished, ReleaseID: 803, Version: "3.7.1", Channel: "stable",
	})
	require.NoError(t, err)
	require.True(t, created)

	bad := DefaultAppReleaseAnnouncementContent("3.7.1", "")
	bad.EN.Title = ""
	badJSON, _ := json.Marshal(bad)
	require.NoError(t, store.DB.Model(&model.ReachTask{}).Where("id = ?", task.ID).
		Update("content", string(badJSON)).Error)

	assert.Error(t, SendReachAnnouncement(context.Background(), task.ID))

	var after model.ReachTask
	require.NoError(t, store.DB.Where("id = ?", task.ID).First(&after).Error)
	assert.Equal(t, model.ReachStatusDraft, after.Status)
}
