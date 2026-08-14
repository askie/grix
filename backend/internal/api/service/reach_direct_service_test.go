package service

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/testutil"
	"github.com/askie/grix/backend/internal/store"
	"github.com/askie/grix/backend/internal/systemsetting"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func restoreDirectReachHooks(t *testing.T) {
	t.Helper()
	origEmail := sendDirectReachEmail
	origSMS := sendDirectReachSMS
	t.Cleanup(func() {
		sendDirectReachEmail = origEmail
		sendDirectReachSMS = origSMS
	})
}

func TestSendDirectUserReach_UsesAppChannelFirst(t *testing.T) {
	setupReachTestDB(t)
	restoreDirectReachHooks(t)
	seedReachCustomerAccount(t, 9001)
	originalRDB := store.RDB
	store.RDB = testutil.NewMockRedis()
	t.Cleanup(func() {
		_ = store.RDB.Close()
		store.RDB = originalRDB
	})

	const targetID = int64(1001)
	require.NoError(t, store.DB.Create(&model.User{
		ID:       targetID,
		Username: "direct_app_user",
		Email:    "direct-app@example.com",
		Status:   model.UserStatusActive,
		Region:   "cn",
	}).Error)
	require.NoError(t, store.RDB.HSet(context.Background(), fmt.Sprintf("im:ws:route:%d", targetID), "direct-online-device", "node-1").Err())
	require.NoError(t, store.RDB.Set(context.Background(), fmt.Sprintf("im:ws:alive:%d:%s", targetID, "direct-online-device"), "1", 0).Err())

	sendDirectReachEmail = func(string, string, string) error {
		t.Fatal("email must not be called when app channel succeeds")
		return nil
	}
	sendDirectReachSMS = func(context.Context, ReachSMSRequest) error {
		t.Fatal("sms must not be called when app channel succeeds")
		return nil
	}

	result, err := SendDirectUserReach(context.Background(), SendDirectUserReachReq{
		UserID:    targetID,
		Title:     "系统通知",
		LongText:  "这是一条较长的客服触达消息",
		ShortText: "客服通知",
	})
	require.NoError(t, err)
	require.Equal(t, model.ReachChannelInApp, result.Channel)
	require.Equal(t, model.ReachStatusSent, result.Status)
	require.Equal(t, model.ReachKindDirect, result.Task.Kind)

	var logs []model.ReachSendLog
	require.NoError(t, store.DB.Where("task_id = ?", result.Task.ID).Order("created_at ASC").Find(&logs).Error)
	require.Len(t, logs, 1)
	assert.Equal(t, model.ReachChannelInApp, logs[0].Channel)
	assert.Equal(t, model.ReachSendStatusSent, logs[0].Status)

	var messages int64
	store.DB.Model(&model.Message{}).Where("sender_id = ? AND content LIKE ?", int64(9001), "%较长的客服触达消息%").Count(&messages)
	assert.Equal(t, int64(1), messages)
}

func TestSendDirectUserReach_DeviceWithoutOfflinePushFallsBackToEmail(t *testing.T) {
	setupReachTestDB(t)
	restoreDirectReachHooks(t)
	seedReachCustomerAccount(t, 9001)

	const targetID = int64(1006)
	require.NoError(t, store.DB.Create(&model.User{
		ID:       targetID,
		Username: "direct_device_no_js_user",
		Email:    "direct-device-no-js@example.com",
		Status:   model.UserStatusActive,
		Region:   "global",
	}).Error)
	require.NoError(t, store.DB.Create(&model.Device{
		UserID:      targetID,
		Platform:    model.DevicePlatformIOS,
		PushEnv:     model.DevicePushEnvDefault,
		DeviceToken: "direct-no-js-token",
		DeviceID:    "direct-no-js-device",
		IsActive:    true,
	}).Error)

	var gotTo string
	sendDirectReachEmail = func(to, _, _ string) error {
		gotTo = to
		return nil
	}

	result, err := SendDirectUserReach(context.Background(), SendDirectUserReachReq{
		UserID:   targetID,
		Title:    "邮件兜底",
		LongText: "有设备但离线推送不可用时走邮件",
	})
	require.NoError(t, err)
	assert.Equal(t, model.ReachChannelEmail, result.Channel)
	assert.Equal(t, "direct-device-no-js@example.com", gotTo)
	require.NotEmpty(t, result.Attempts)
	assert.Equal(t, model.ReachChannelInApp, result.Attempts[0].Channel)
	assert.Equal(t, model.ReachSendStatusSkipped, result.Attempts[0].Status)
}

func TestSendDirectUserReach_FallsBackToEmail(t *testing.T) {
	setupReachTestDB(t)
	restoreDirectReachHooks(t)
	require.NoError(t, systemsetting.SaveAuthSettings(systemsetting.AuthSettings{AutoAddCustomerUserID: 9001}, nil))
	t.Cleanup(systemsetting.InvalidateAuthSettingsCache)

	const targetID = int64(1002)
	require.NoError(t, store.DB.Create(&model.User{
		ID:       targetID,
		Username: "direct_email_user",
		Email:    "direct-email@example.com",
		Status:   model.UserStatusActive,
		Region:   "global",
	}).Error)

	var gotTo, gotSubject, gotBody string
	sendDirectReachEmail = func(to, subject, body string) error {
		gotTo, gotSubject, gotBody = to, subject, body
		return nil
	}
	sendDirectReachSMS = func(context.Context, ReachSMSRequest) error {
		t.Fatal("sms must not be called when email succeeds")
		return nil
	}

	result, err := SendDirectUserReach(context.Background(), SendDirectUserReachReq{
		UserID:   targetID,
		Title:    "邮件通知",
		LongText: "邮件正文\n\n- 第一项\n- **第二项**\n\n[官网](https://grix.im)\n\n<script>alert(1)</script>",
	})
	require.NoError(t, err)
	assert.Equal(t, model.ReachChannelEmail, result.Channel)
	assert.Equal(t, "direct-email@example.com", gotTo)
	assert.Equal(t, "邮件通知", gotSubject)
	assert.Contains(t, gotBody, "邮件正文")
	assert.Contains(t, gotBody, "<ul>")
	assert.Contains(t, gotBody, "<strong>第二项</strong>")
	assert.Contains(t, gotBody, `<a href="https://grix.im">官网</a>`)
	assert.Contains(t, gotBody, "raw HTML omitted")
	assert.NotContains(t, gotBody, "<script>alert(1)</script>")

	var logs []model.ReachSendLog
	require.NoError(t, store.DB.Where("task_id = ?", result.Task.ID).Order("created_at ASC").Find(&logs).Error)
	require.Len(t, logs, 1)
	assert.Equal(t, model.ReachChannelEmail, logs[0].Channel)
	assert.Equal(t, model.ReachSendStatusSent, logs[0].Status)
}

func TestDirectReachEmailContent_RendersMarkdownAsSimpleHTML(t *testing.T) {
	subject, body := directReachEmailContent(SendDirectUserReachReq{
		Title:    "Markdown 邮件",
		LongText: "## 小标题\n\n普通段落\n\n1. A\n2. B",
	})

	assert.Equal(t, "Markdown 邮件", subject)
	assert.Contains(t, body, "<h2>小标题</h2>")
	assert.Contains(t, body, "<p>普通段落</p>")
	assert.Contains(t, body, "<ol>")
}

func TestSendDirectUserReach_FallsBackToSMSAfterEmailFailure(t *testing.T) {
	setupReachTestDB(t)
	restoreDirectReachHooks(t)
	require.NoError(t, systemsetting.SaveAuthSettings(systemsetting.AuthSettings{AutoAddCustomerUserID: 9001}, nil))
	t.Cleanup(systemsetting.InvalidateAuthSettingsCache)

	const targetID = int64(1003)
	require.NoError(t, store.DB.Create(&model.User{
		ID:           targetID,
		Username:     "direct_sms_user",
		Email:        "direct-sms@example.com",
		Status:       model.UserStatusActive,
		Region:       "global",
		PhoneE164:    "+14155550100",
		PhoneCountry: "+1",
	}).Error)

	sendDirectReachEmail = func(string, string, string) error {
		return errors.New("email provider down")
	}
	var gotSMS ReachSMSRequest
	sendDirectReachSMS = func(_ context.Context, req ReachSMSRequest) error {
		gotSMS = req
		return nil
	}

	result, err := SendDirectUserReach(context.Background(), SendDirectUserReachReq{
		UserID:    targetID,
		Title:     "触达通知",
		LongText:  "这是一条长文本，邮件失败后不应丢失",
		ShortText: "短信短文案",
	})
	require.NoError(t, err)
	assert.Equal(t, model.ReachChannelSMS, result.Channel)
	assert.Equal(t, "+14155550100", gotSMS.PhoneE164)
	assert.Equal(t, "+1", gotSMS.CountryCode)
	assert.Equal(t, "短信短文案", gotSMS.Text)

	var logs []model.ReachSendLog
	require.NoError(t, store.DB.Where("task_id = ?", result.Task.ID).Order("created_at ASC").Find(&logs).Error)
	require.Len(t, logs, 2)
	assert.Equal(t, model.ReachChannelEmail, logs[0].Channel)
	assert.Equal(t, model.ReachSendStatusFailed, logs[0].Status)
	assert.Contains(t, logs[0].Error, "email provider down")
	assert.Equal(t, model.ReachChannelSMS, logs[1].Channel)
	assert.Equal(t, model.ReachSendStatusSent, logs[1].Status)
}

func TestSendDirectUserReach_RecordsFailedTaskWhenNoChannelSucceeds(t *testing.T) {
	setupReachTestDB(t)
	restoreDirectReachHooks(t)
	require.NoError(t, systemsetting.SaveAuthSettings(systemsetting.AuthSettings{AutoAddCustomerUserID: 0}, nil))
	t.Cleanup(systemsetting.InvalidateAuthSettingsCache)

	const targetID = int64(1004)
	require.NoError(t, store.DB.Create(&model.User{
		ID:       targetID,
		Username: "direct_no_channel_user",
		Status:   model.UserStatusActive,
		Region:   "global",
	}).Error)

	result, err := SendDirectUserReach(context.Background(), SendDirectUserReachReq{
		UserID:   targetID,
		LongText: "没有可用通道",
	})
	require.NoError(t, err)
	assert.Equal(t, model.ReachStatusFailed, result.Status)
	assert.Equal(t, model.ReachStatusFailed, result.Task.Status)
	assert.Empty(t, result.Channel)
	assert.Len(t, result.Attempts, 3)

	var logCount int64
	store.DB.Model(&model.ReachSendLog{}).Where("task_id = ?", result.Task.ID).Count(&logCount)
	assert.Equal(t, int64(0), logCount)
}

func TestSendDirectUserReach_DedupKeyUsesReachTaskUniqueIndex(t *testing.T) {
	setupReachTestDB(t)
	restoreDirectReachHooks(t)
	seedReachCustomerAccount(t, 9001)

	const targetID = int64(1005)
	require.NoError(t, store.DB.Create(&model.User{
		ID:       targetID,
		Username: "direct_dedup_user",
		Status:   model.UserStatusActive,
		Region:   "global",
	}).Error)

	dedupKey := "direct:test:1005"
	first, err := SendDirectUserReach(context.Background(), SendDirectUserReachReq{
		UserID:   targetID,
		LongText: "first",
		DedupKey: dedupKey,
	})
	require.NoError(t, err)
	second, err := SendDirectUserReach(context.Background(), SendDirectUserReachReq{
		UserID:   targetID,
		LongText: "second",
		DedupKey: dedupKey,
	})
	require.NoError(t, err)
	assert.Equal(t, first.Task.ID, second.Task.ID)

	var taskCount int64
	store.DB.Model(&model.ReachTask{}).Where("dedup_key = ?", dedupKey).Count(&taskCount)
	assert.Equal(t, int64(1), taskCount)
}
