package service

import (
	"context"
	"testing"
	"time"

	"github.com/askie/grix/backend/config"
	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/store"
	"github.com/stretchr/testify/require"
)

func seedNotifyOwner(t *testing.T, id int64, nickname, email, last4 string) {
	t.Helper()
	require.NoError(t, store.DB.Create(&model.User{
		ID:         id,
		Username:   nickname,
		Nickname:   nickname,
		Email:      email,
		PhoneLast4: last4,
		Status:     model.UserStatusActive,
		Region:     "cn",
	}).Error)
}

func seedNotifyAgent(t *testing.T, id, ownerID int64) {
	t.Helper()
	require.NoError(t, store.DB.Create(&model.Agent{
		ID:        id,
		AgentName: "agent",
		OwnerID:   ownerID,
	}).Error)
}

func seedUpgradeReport(t *testing.T, id, agentID int64, installID, version, status, errorCode string, at time.Time) {
	t.Helper()
	row := model.ConnectorUpgradeReport{
		ID:          id,
		AgentID:     agentID,
		ClientType:  "grix-connector",
		FromVersion: "4.3.4",
		ToVersion:   version,
		Status:      status,
		InstallID:   &installID,
		ReportedAt:  at,
	}
	if errorCode != "" {
		row.ErrorCode = &errorCode
	}
	require.NoError(t, store.DB.Create(&row).Error)
}

func TestListConnectorProblemUsers_LatestPerInstallAndSelfHeal(t *testing.T) {
	setupReachTestDB(t)
	base := time.Date(2026, 8, 20, 10, 0, 0, 0, time.UTC)

	seedNotifyOwner(t, 2001, "owner-a", "a@example.com", "8000")
	seedNotifyOwner(t, 2002, "owner-b", "b@example.com", "")
	seedNotifyOwner(t, 2003, "owner-c", "c@example.com", "")
	seedNotifyAgent(t, 3001, 2001)
	seedNotifyAgent(t, 3002, 2002)
	seedNotifyAgent(t, 3003, 2003)

	// A: 先失败后无成功 → 仍是问题用户，两台机器。
	seedUpgradeReport(t, 1, 3001, "install-a1", "4.3.5", model.UpgradeReportFailed, "STARTUP_CRASH", base)
	seedUpgradeReport(t, 2, 3001, "install-a2", "4.3.5", model.UpgradeReportRolledBack, "NPM_TIMEOUT", base.Add(time.Minute))
	// A 的同一台机器同版本重复上报，只算最新一条，不重复计数。
	seedUpgradeReport(t, 3, 3001, "install-a1", "4.3.5", model.UpgradeReportFailed, "STARTUP_CRASH", base.Add(2*time.Minute))

	// B: 失败后在更高版本上报成功 → 已自愈，剔除。
	seedUpgradeReport(t, 4, 3002, "install-b1", "4.3.5", model.UpgradeReportFailed, "NPM_NOT_FOUND", base)
	seedUpgradeReport(t, 5, 3002, "install-b1", "4.3.6", model.UpgradeReportSuccess, "", base.Add(time.Hour))

	// C: 平台不支持 → 默认剔除。
	seedUpgradeReport(t, 6, 3003, "install-c1", "4.3.5", model.UpgradeReportFailed, connectorUnsupportedErrorCode, base)

	result, ec := ListConnectorProblemUsers(ListConnectorProblemUsersReq{Version: "4.3.5"})
	require.Nil(t, ec)
	require.Equal(t, int64(1), result.Total)
	require.Len(t, result.Users, 1)

	u := result.Users[0]
	require.Equal(t, int64(2001), u.UserID)
	require.Equal(t, "owner-a", u.Nickname)
	require.Equal(t, "****8000", u.PhoneMasked)
	require.Equal(t, 2, u.FailedHosts)
	require.Equal(t, []string{"NPM_TIMEOUT", "STARTUP_CRASH"}, u.ErrorCodes)
	require.Equal(t, []string{"3001"}, u.AgentIDs)

	// include_unsupported 打开后 C 也要出现。
	withUnsupported, ec := ListConnectorProblemUsers(ListConnectorProblemUsersReq{Version: "4.3.5", IncludeUnsupported: true})
	require.Nil(t, ec)
	require.Equal(t, int64(2), withUnsupported.Total)
}

func TestListConnectorProblemUsers_RequiresVersion(t *testing.T) {
	setupReachTestDB(t)
	_, ec := ListConnectorProblemUsers(ListConnectorProblemUsersReq{})
	require.NotNil(t, ec)
}

func stubNotifyEmailTemplate(t *testing.T) *[]string {
	t.Helper()
	origDesc := descAliEmailTemplate
	origSend := sendDirectReachEmail
	origTemplateID := config.C.AliEmail.ReachTemplateID
	InvalidateAliEmailTemplateCache()
	config.C.AliEmail.ReachTemplateID = 440876
	descAliEmailTemplate = func(int) (AliEmailTemplate, error) {
		return AliEmailTemplate{Name: "notify", Subject: "模板主题", Text: "<p>Hi {name}</p>{body}"}, nil
	}
	sent := []string{}
	sendDirectReachEmail = func(to, subject, body string) error {
		sent = append(sent, to+"|"+subject+"|"+body)
		return nil
	}
	t.Cleanup(func() {
		descAliEmailTemplate = origDesc
		sendDirectReachEmail = origSend
		config.C.AliEmail.ReachTemplateID = origTemplateID
		InvalidateAliEmailTemplateCache()
	})
	return &sent
}

func TestNotifyConnectorProblemUsers_EmailSendsOnceAndDedupes(t *testing.T) {
	setupReachTestDB(t)
	sent := stubNotifyEmailTemplate(t)
	seedNotifyOwner(t, 2101, "老郭", "owner@example.com", "8000")

	req := NotifyConnectorProblemUsersReq{
		Version: "4.3.5",
		UserIDs: []int64{2101},
		Channel: ConnectorNotifyChannelEmail,
		Title:   "连接器升级失败",
		Body:    "请手动重装 **grix-connector**。",
	}
	results, ec := NotifyConnectorProblemUsers(context.Background(), req)
	require.Nil(t, ec)
	require.Len(t, results, 1)
	require.Equal(t, model.ReachSendStatusSent, results[0].Status)
	require.Equal(t, ConnectorNotifyChannelEmail, results[0].Channel)
	require.Len(t, *sent, 1)
	require.Contains(t, (*sent)[0], "owner@example.com|连接器升级失败|")
	require.Contains(t, (*sent)[0], "Hi 老郭")
	require.Contains(t, (*sent)[0], "<strong>grix-connector</strong>")

	var task model.ReachTask
	require.NoError(t, store.DB.Where("id = ?", results[0].TaskID).First(&task).Error)
	require.Equal(t, ConnectorNotifyEventKey, task.EventKey)
	require.NotNil(t, task.DedupKey)
	require.Equal(t, "connector_upgrade:4.3.5:2101:email", *task.DedupKey)
	require.Equal(t, model.ReachStatusSent, task.Status)

	// 再点一次同样的按钮：命中幂等键，不重复发。
	again, ec := NotifyConnectorProblemUsers(context.Background(), req)
	require.Nil(t, ec)
	require.Equal(t, ConnectorNotifyStatusDuplicate, again[0].Status)
	require.Len(t, *sent, 1)
}

func TestNotifyConnectorProblemUsers_AutoFallsBackToSMS(t *testing.T) {
	setupReachTestDB(t)
	restoreDirectReachHooks(t)
	stubNotifyEmailTemplate(t)
	seedNotifyOwner(t, 2102, "无邮箱用户", "", "8000")
	require.NoError(t, store.DB.Model(&model.User{}).Where("id = ?", 2102).
		Updates(map[string]any{"phone_e164": "+8613800138000", "phone_country": "+86"}).Error)

	smsCalls := 0
	sendDirectReachSMS = func(_ context.Context, req ReachSMSRequest) error {
		smsCalls++
		require.Equal(t, "notify", req.Kind)
		require.Equal(t, "+8613800138000", req.PhoneE164)
		return nil
	}

	results, ec := NotifyConnectorProblemUsers(context.Background(), NotifyConnectorProblemUsersReq{
		Version: "4.3.5",
		UserIDs: []int64{2102},
		Channel: ConnectorNotifyChannelAuto,
		Title:   "连接器升级失败",
		Body:    "请手动重装连接器。",
	})
	require.Nil(t, ec)
	require.Equal(t, 1, smsCalls)
	require.Equal(t, model.ReachSendStatusSent, results[0].Status)
	require.Equal(t, ConnectorNotifyChannelSMS, results[0].Channel)
}

func TestNotifyConnectorProblemUsers_ReportsNotConfigured(t *testing.T) {
	setupReachTestDB(t)
	restoreDirectReachHooks(t)
	stubNotifyEmailTemplate(t)
	seedNotifyOwner(t, 2103, "短信用户", "", "8000")
	require.NoError(t, store.DB.Model(&model.User{}).Where("id = ?", 2103).
		Updates(map[string]any{"phone_e164": "+8613800138001", "phone_country": "+86"}).Error)

	sendDirectReachSMS = func(context.Context, ReachSMSRequest) error { return ErrReachSMSNotConfigured }

	results, ec := NotifyConnectorProblemUsers(context.Background(), NotifyConnectorProblemUsersReq{
		Version: "4.3.5",
		UserIDs: []int64{2103},
		Channel: ConnectorNotifyChannelSMS,
		Body:    "请手动重装连接器。",
	})
	require.Nil(t, ec)
	require.Equal(t, ConnectorNotifyStatusNotConfigured, results[0].Status)
}

func TestNotifyConnectorProblemUsers_RejectsBadInput(t *testing.T) {
	setupReachTestDB(t)
	_, ec := NotifyConnectorProblemUsers(context.Background(), NotifyConnectorProblemUsersReq{
		Version: "4.3.5", UserIDs: []int64{1}, Channel: "wechat", Body: "x",
	})
	require.NotNil(t, ec)

	_, ec = NotifyConnectorProblemUsers(context.Background(), NotifyConnectorProblemUsersReq{
		Version: "4.3.5", UserIDs: []int64{1}, Channel: ConnectorNotifyChannelEmail,
	})
	require.NotNil(t, ec)
}

func TestPreviewConnectorNotify_RendersTemplate(t *testing.T) {
	setupReachTestDB(t)
	stubNotifyEmailTemplate(t)
	seedNotifyOwner(t, 2104, "预览用户", "p@example.com", "")

	preview, ec := PreviewConnectorNotify("升级失败告知", "请手动重装连接器。", 2104)
	require.Nil(t, ec)
	require.Equal(t, "升级失败告知", preview.EmailSubject)
	require.Contains(t, preview.EmailHTML, "Hi 预览用户")
	require.Equal(t, "请手动重装连接器。", preview.SMSText)
	// 通知模板号未配置时，预览要把这件事说清楚而不是静默。
	require.NotEmpty(t, preview.SMSError)
}
