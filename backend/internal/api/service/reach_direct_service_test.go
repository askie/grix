package service

import (
	"context"
	"fmt"
	"strings"
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

func TestSendDirectUserReach_DeviceWithoutOfflinePushNeverFallsBackToEmail(t *testing.T) {
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

	sendDirectReachEmail = func(string, string, string) error {
		t.Fatal("direct reach must never send email, even when in_app is unavailable")
		return nil
	}

	result, err := SendDirectUserReach(context.Background(), SendDirectUserReachReq{
		UserID:   targetID,
		Title:    "站内不可用",
		LongText: "有设备但离线推送不可用时也不能落到 no-reply 邮件",
	})
	require.NoError(t, err)
	// 用户有邮箱、没手机号：默认链路里已经没有邮件这一环，只能整单失败。
	assert.Equal(t, model.ReachStatusFailed, result.Status)
	assert.Empty(t, result.Channel)
	require.Len(t, result.Attempts, 2)
	assert.Equal(t, model.ReachChannelInApp, result.Attempts[0].Channel)
	assert.Equal(t, model.ReachSendStatusSkipped, result.Attempts[0].Status)
	assert.Equal(t, model.ReachChannelSMS, result.Attempts[1].Channel)
	assert.Equal(t, model.ReachSendStatusSkipped, result.Attempts[1].Status)

	var logs []model.ReachSendLog
	require.NoError(t, store.DB.Where("task_id = ?", result.Task.ID).Find(&logs).Error)
	for _, l := range logs {
		assert.NotEqual(t, model.ReachChannelEmail, l.Channel)
	}
}

func TestSendDirectUserReach_DefaultChannelsExcludeEmail(t *testing.T) {
	setupReachTestDB(t)
	restoreDirectReachHooks(t)
	require.NoError(t, systemsetting.SaveAuthSettings(systemsetting.AuthSettings{AutoAddCustomerUserID: 9001}, nil))
	t.Cleanup(systemsetting.InvalidateAuthSettingsCache)

	const targetID = int64(1002)
	require.NoError(t, store.DB.Create(&model.User{
		ID:           targetID,
		Username:     "direct_default_user",
		Email:        "direct-default@example.com",
		Status:       model.UserStatusActive,
		Region:       "global",
		PhoneE164:    "+14155550111",
		PhoneCountry: "+1",
	}).Error)

	// 默认兜底链是 in_app -> sms，中间不再有邮件这一环。
	assert.Equal(t, []string{model.ReachChannelInApp, model.ReachChannelSMS}, directReachDefaultChannels)

	sendDirectReachEmail = func(string, string, string) error {
		t.Fatal("default channel chain must not reach the no-reply email channel")
		return nil
	}
	var gotSMS ReachSMSRequest
	sendDirectReachSMS = func(_ context.Context, req ReachSMSRequest) error {
		gotSMS = req
		return nil
	}

	result, err := SendDirectUserReach(context.Background(), SendDirectUserReachReq{
		UserID:    targetID,
		Title:     "默认链路",
		LongText:  "不传 channels 时不能发邮件",
		ShortText: "短信短文案",
	})
	require.NoError(t, err)
	assert.Equal(t, model.ReachChannelSMS, result.Channel)
	assert.Equal(t, "短信短文案", gotSMS.Text)

	var logs []model.ReachSendLog
	require.NoError(t, store.DB.Where("task_id = ?", result.Task.ID).Order("created_at ASC").Find(&logs).Error)
	require.Len(t, logs, 1)
	assert.Equal(t, model.ReachChannelSMS, logs[0].Channel)
}

func TestSendDirectUserReach_RejectsExplicitEmailChannel(t *testing.T) {
	setupReachTestDB(t)
	restoreDirectReachHooks(t)

	const targetID = int64(1007)
	require.NoError(t, store.DB.Create(&model.User{
		ID:       targetID,
		Username: "direct_explicit_email_user",
		Email:    "direct-explicit@example.com",
		Status:   model.UserStatusActive,
		Region:   "global",
	}).Error)

	sendDirectReachEmail = func(string, string, string) error {
		t.Fatal("explicit email channel must be rejected before delivery")
		return nil
	}

	result, err := SendDirectUserReach(context.Background(), SendDirectUserReachReq{
		UserID:   targetID,
		Title:    "显式指定邮件",
		LongText: "显式传 email 应当按参数错误拒绝",
		Channels: []string{model.ReachChannelEmail},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "channels contains no known channel")
	assert.Nil(t, result)

	var taskCount int64
	store.DB.Model(&model.ReachTask{}).Count(&taskCount)
	assert.Equal(t, int64(0), taskCount, "参数错误不该留下触达任务")
}

func TestDirectReachMarkdownHTML_RendersMarkdownAsSimpleHTML(t *testing.T) {
	body := directReachMarkdownHTML("## 小标题\n\n普通段落\n\n1. A\n2. B")

	assert.Contains(t, body, "<h2>小标题</h2>")
	assert.Contains(t, body, "<p>普通段落</p>")
	assert.Contains(t, body, "<ol>")
}

func TestDirectReachMarkdownHTML_ImageAndCTA(t *testing.T) {
	body := directReachMarkdownHTML("正文里的[行内链接](https://grix.im/inline)不该变按钮。\n\n![封面](https://cdn.example.com/cover.jpg)\n\n[![点封面跳转](https://cdn.example.com/c.jpg)](https://grix.im/demo)\n\n[马上接入 →](https://grix.im/zh-CN/)")

	// 图片自适应正文宽度，超宽原图不会被外层 overflow:hidden 裁掉。
	assert.Contains(t, body, `<img style="max-width:100%;height:auto;`)
	assert.NotContains(t, body, `<img src=`)

	// 独占一段的链接渲染成按钮，Outlook 走 VML 分支，其余客户端走普通 <a>，两者互斥。
	assert.Contains(t, body, `<a href="https://grix.im/zh-CN/" style="display:inline-block;`)
	assert.Contains(t, body, "马上接入 →</a>")
	assert.Contains(t, body, `<v:roundrect xmlns:v="urn:schemas-microsoft-com:vml"`)
	assert.Contains(t, body, `href="https://grix.im/zh-CN/" style="height:44px;`)
	assert.Contains(t, body, `fillcolor="#4A90D9"`)
	assert.Contains(t, body, "<!--[if mso]>")
	assert.Contains(t, body, "<!--[if !mso]><!-->")

	// 行内链接保持原样，不能被按钮样式污染。
	assert.Contains(t, body, `<a href="https://grix.im/inline">行内链接</a>`)

	// 封面图包在链接里，链接文字是图片而非文本，同样不该变按钮。
	assert.Contains(t, body, `<a href="https://grix.im/demo"><img style=`)
}

func TestDirectReachMarkdownHTML_BareURLStaysPlainLink(t *testing.T) {
	// GFM autolink 会把独占一行的裸 URL 渲染成跟 CTA 一样的形状，但正文里单独放一行
	// 参考链接是很自然的写法，不该被撑成大按钮。
	body := directReachMarkdownHTML("参考文档：\n\nhttps://grix.im/docs\n\n有问题联系\n\nsupport@grix.im\n\n[马上接入 →](https://grix.im/zh-CN/)")

	assert.Contains(t, body, `<p><a href="https://grix.im/docs">https://grix.im/docs</a></p>`)
	assert.NotContains(t, body, `href="https://grix.im/docs" style="display:inline-block;`)

	// 裸邮箱走同一条 autolink，只是 href 多了 mailto: 前缀，同样不该变按钮。
	assert.Contains(t, body, `<p><a href="mailto:support@grix.im">support@grix.im</a></p>`)
	assert.NotContains(t, body, `href="mailto:support@grix.im" style="display:inline-block;`)

	// 真正的 CTA 不受影响。
	assert.Contains(t, body, `<a href="https://grix.im/zh-CN/" style="display:inline-block;`)
}

func TestReachEmailCTAButtonWidth(t *testing.T) {
	// VML 按钮必须给死宽度，太窄会把文字裁掉。
	assert.Equal(t, reachCTAMinWidth, reachEmailCTAButtonWidth("去"), "短文案取下限")
	assert.Equal(t, reachCTAMaxWidth, reachEmailCTAButtonWidth(strings.Repeat("很长的文案", 20)), "超长文案取上限")

	// 箭头、破折号、省略号在 Arial 里接近一个字号宽，必须按宽字符算，否则 Outlook 会裁字。
	// 文案里的 CTA 基本都带 →，这是最容易踩的一个。
	assert.Greater(t, reachEmailCTAButtonWidth("aaaaaaaaaa→"), reachEmailCTAButtonWidth("aaaaaaaaaaa"))
	assert.Greater(t, reachEmailCTAButtonWidth("aaaaaaaaaa—"), reachEmailCTAButtonWidth("aaaaaaaaaaa"))

	// 同样 12 个字符，中文比英文宽。
	cn := reachEmailCTAButtonWidth("马上接入马上接入马上接入")
	en := reachEmailCTAButtonWidth("Get Started ")
	assert.Greater(t, cn, en)
	assert.Greater(t, cn, reachCTAMinWidth)
	assert.LessOrEqual(t, cn, reachCTAMaxWidth)
}

func TestSendDirectUserReach_FallsBackToSMSWhenInAppUnavailable(t *testing.T) {
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
		t.Fatal("in_app 不可用时必须落到短信，不能落到 no-reply 邮件")
		return nil
	}
	var gotSMS ReachSMSRequest
	sendDirectReachSMS = func(_ context.Context, req ReachSMSRequest) error {
		gotSMS = req
		return nil
	}

	result, err := SendDirectUserReach(context.Background(), SendDirectUserReachReq{
		UserID:    targetID,
		Title:     "触达通知",
		LongText:  "这是一条长文本，站内不可用后不应丢失",
		ShortText: "短信短文案",
	})
	require.NoError(t, err)
	assert.Equal(t, model.ReachChannelSMS, result.Channel)
	assert.Equal(t, "+14155550100", gotSMS.PhoneE164)
	assert.Equal(t, "+1", gotSMS.CountryCode)
	assert.Equal(t, "短信短文案", gotSMS.Text)

	var logs []model.ReachSendLog
	require.NoError(t, store.DB.Where("task_id = ?", result.Task.ID).Order("created_at ASC").Find(&logs).Error)
	require.Len(t, logs, 1)
	assert.Equal(t, model.ReachChannelSMS, logs[0].Channel)
	assert.Equal(t, model.ReachSendStatusSent, logs[0].Status)
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
	assert.Len(t, result.Attempts, 2)

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
