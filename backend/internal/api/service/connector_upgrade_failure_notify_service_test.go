package service

import (
	"context"
	"encoding/json"
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

// 幂等键在投递前就占住了：上次整单失败（例如模板号没配）时必须能重来一次，
// 否则配好模板后这批用户在这个版本上永远发不出去。
func TestNotifyConnectorProblemUsers_RetriesAfterFailedTask(t *testing.T) {
	setupReachTestDB(t)
	restoreDirectReachHooks(t)
	stubNotifyEmailTemplate(t)
	seedNotifyOwner(t, 2105, "重试用户", "", "8000")
	require.NoError(t, store.DB.Model(&model.User{}).Where("id = ?", 2105).
		Updates(map[string]any{"phone_e164": "+8613800138002", "phone_country": "+86"}).Error)

	req := NotifyConnectorProblemUsersReq{
		Version: "4.3.5",
		UserIDs: []int64{2105},
		Channel: ConnectorNotifyChannelSMS,
		Body:    "请手动重装连接器。",
	}

	// 第一次：通知模板号没配。
	sendDirectReachSMS = func(context.Context, ReachSMSRequest) error { return ErrReachSMSNotConfigured }
	first, ec := NotifyConnectorProblemUsers(context.Background(), req)
	require.Nil(t, ec)
	require.Equal(t, ConnectorNotifyStatusNotConfigured, first[0].Status)

	// 配好之后原样再点一次：必须真的重发，而不是被判成 duplicate。
	smsCalls := 0
	sendDirectReachSMS = func(context.Context, ReachSMSRequest) error {
		smsCalls++
		return nil
	}
	second, ec := NotifyConnectorProblemUsers(context.Background(), req)
	require.Nil(t, ec)
	require.Equal(t, 1, smsCalls, "失败任务必须允许重试")
	require.Equal(t, model.ReachSendStatusSent, second[0].Status)
	require.Equal(t, first[0].TaskID, second[0].TaskID, "重试复用同一幂等任务")

	// 复用同一条日志行，不因唯一索引冲突而失败。
	var logs []model.ReachSendLog
	require.NoError(t, store.DB.Where("task_id = ?", second[0].TaskID).Find(&logs).Error)
	require.Len(t, logs, 1)
	require.Equal(t, model.ReachSendStatusSent, logs[0].Status)

	// 成功之后再点就该被幂等挡住。
	third, ec := NotifyConnectorProblemUsers(context.Background(), req)
	require.Nil(t, ec)
	require.Equal(t, ConnectorNotifyStatusDuplicate, third[0].Status)
	require.Equal(t, 1, smsCalls)
}

// 失败要计进任务 stats：not_configured 只是给后台看的口径，不能让 stats 一个都不记。
func TestNotifyConnectorProblemUsers_CountsNotConfiguredAsFailed(t *testing.T) {
	setupReachTestDB(t)
	restoreDirectReachHooks(t)
	stubNotifyEmailTemplate(t)
	seedNotifyOwner(t, 2106, "统计用户", "", "8000")
	require.NoError(t, store.DB.Model(&model.User{}).Where("id = ?", 2106).
		Updates(map[string]any{"phone_e164": "+8613800138003", "phone_country": "+86"}).Error)
	sendDirectReachSMS = func(context.Context, ReachSMSRequest) error { return ErrReachSMSNotConfigured }

	results, ec := NotifyConnectorProblemUsers(context.Background(), NotifyConnectorProblemUsersReq{
		Version: "4.3.5", UserIDs: []int64{2106}, Channel: ConnectorNotifyChannelSMS, Body: "x",
	})
	require.Nil(t, ec)

	var task model.ReachTask
	require.NoError(t, store.DB.Where("id = ?", results[0].TaskID).First(&task).Error)
	stats := map[string]int{}
	require.NoError(t, json.Unmarshal(task.Stats, &stats))
	require.Equal(t, 1, stats["failed"], "stats 必须与 reach_send_logs 对齐: %s", string(task.Stats))
}

// 机器换了 agent 重注册后报的成功，仍然要算已自愈。
func TestListConnectorProblemUsers_SelfHealAcrossAgentReregistration(t *testing.T) {
	setupReachTestDB(t)
	base := time.Date(2026, 8, 20, 10, 0, 0, 0, time.UTC)

	seedNotifyOwner(t, 2201, "owner-d", "d@example.com", "")
	seedNotifyAgent(t, 3201, 2201)
	seedNotifyAgent(t, 3202, 2201)

	seedUpgradeReport(t, 11, 3201, "install-d1", "4.3.5", model.UpgradeReportFailed, "STARTUP_CRASH", base)
	// 同一台机器（同 install_id）换了 agent 之后报成功。
	seedUpgradeReport(t, 12, 3202, "install-d1", "4.3.6", model.UpgradeReportSuccess, "", base.Add(time.Hour))

	result, ec := ListConnectorProblemUsers(ListConnectorProblemUsersReq{Version: "4.3.5"})
	require.Nil(t, ec)
	require.Equal(t, int64(0), result.Total, "换 agent 后报成功的机器不该再被打扰")
}

// 候选行有 install_id 而成功行只有 host_name 时，hostKey 对不上也要认出是同一台机器。
func TestListConnectorProblemUsers_SelfHealMatchesByHostName(t *testing.T) {
	setupReachTestDB(t)
	base := time.Date(2026, 8, 20, 10, 0, 0, 0, time.UTC)

	seedNotifyOwner(t, 2202, "owner-e", "e@example.com", "")
	seedNotifyAgent(t, 3203, 2202)

	host := "mac-e"
	failed := model.ConnectorUpgradeReport{
		ID: 21, AgentID: 3203, ClientType: "grix-connector", FromVersion: "4.3.4", ToVersion: "4.3.5",
		Status: model.UpgradeReportFailed, InstallID: strPtr("install-e1"), HostName: &host, ReportedAt: base,
	}
	require.NoError(t, store.DB.Create(&failed).Error)
	healed := model.ConnectorUpgradeReport{
		ID: 22, AgentID: 3203, ClientType: "grix-connector", FromVersion: "4.3.5", ToVersion: "4.3.6",
		Status: model.UpgradeReportSuccess, HostName: &host, ReportedAt: base.Add(time.Hour),
	}
	require.NoError(t, store.DB.Create(&healed).Error)

	result, ec := ListConnectorProblemUsers(ListConnectorProblemUsersReq{Version: "4.3.5"})
	require.Nil(t, ec)
	require.Equal(t, int64(0), result.Total, "按 host_name 也要能匹配到自愈上报")
}

// 并发双击不能各投一次：重开是条件更新，只有抢到的那个请求继续投递。
func TestNotifyConnectorProblemUsers_ConcurrentRetrySendsOnce(t *testing.T) {
	setupReachTestDB(t)
	restoreDirectReachHooks(t)
	stubNotifyEmailTemplate(t)
	seedNotifyOwner(t, 2107, "并发用户", "", "8000")
	require.NoError(t, store.DB.Model(&model.User{}).Where("id = ?", 2107).
		Updates(map[string]any{"phone_e164": "+8613800138004", "phone_country": "+86"}).Error)

	req := NotifyConnectorProblemUsersReq{
		Version: "4.3.5", UserIDs: []int64{2107}, Channel: ConnectorNotifyChannelSMS, Body: "请手动重装连接器。",
	}
	sendDirectReachSMS = func(context.Context, ReachSMSRequest) error { return ErrReachSMSNotConfigured }
	_, ec := NotifyConnectorProblemUsers(context.Background(), req)
	require.Nil(t, ec)

	// 任务停在 failed；两个请求同时读到 failed 后只能有一个真的重开。
	var task model.ReachTask
	require.NoError(t, store.DB.Where("dedup_key = ?", "connector_upgrade:4.3.5:2107:sms").First(&task).Error)
	require.Equal(t, model.ReachStatusFailed, task.Status)

	firstReopened, err := reopenFailedReachTask(context.Background(), task.ID)
	require.NoError(t, err)
	require.True(t, firstReopened)
	secondReopened, err := reopenFailedReachTask(context.Background(), task.ID)
	require.NoError(t, err)
	require.False(t, secondReopened, "第二个并发请求不该也拿到重开权")

	// 任务已被领走（status=sending）时，后到的请求判 duplicate 且不投递。
	smsCalls := 0
	sendDirectReachSMS = func(context.Context, ReachSMSRequest) error {
		smsCalls++
		return nil
	}
	results, ec := NotifyConnectorProblemUsers(context.Background(), req)
	require.Nil(t, ec)
	require.Equal(t, ConnectorNotifyStatusDuplicate, results[0].Status)
	require.Equal(t, 0, smsCalls)
}

// 主机名不全局唯一：别的 owner 同名机器报成功，不能把本 owner 的候选抵消掉。
func TestListConnectorProblemUsers_SelfHealHostNameScopedToOwner(t *testing.T) {
	setupReachTestDB(t)
	base := time.Date(2026, 8, 20, 10, 0, 0, 0, time.UTC)

	seedNotifyOwner(t, 2203, "owner-f", "f@example.com", "")
	seedNotifyOwner(t, 2204, "owner-g", "g@example.com", "")
	seedNotifyAgent(t, 3204, 2203)
	seedNotifyAgent(t, 3205, 2204)

	host := "MacBook-Pro.local"
	require.NoError(t, store.DB.Create(&model.ConnectorUpgradeReport{
		ID: 31, AgentID: 3204, ClientType: "grix-connector", FromVersion: "4.3.4", ToVersion: "4.3.5",
		Status: model.UpgradeReportFailed, HostName: &host, ReportedAt: base,
	}).Error)
	// 另一个 owner 的同名机器升级成功——与上面那台没有任何关系。
	require.NoError(t, store.DB.Create(&model.ConnectorUpgradeReport{
		ID: 32, AgentID: 3205, ClientType: "grix-connector", FromVersion: "4.3.4", ToVersion: "4.3.5",
		Status: model.UpgradeReportSuccess, HostName: &host, ReportedAt: base.Add(time.Hour),
	}).Error)

	result, ec := ListConnectorProblemUsers(ListConnectorProblemUsersReq{Version: "4.3.5"})
	require.Nil(t, ec)
	require.Equal(t, int64(1), result.Total, "别的 owner 的同名机器不该抵消本 owner 的候选")
	require.Equal(t, int64(2203), result.Users[0].UserID)
}

// 分页要回显 clamp 之后真正生效的值，而不是原样回请求参数。
func TestListConnectorProblemUsers_ReportsClampedPaging(t *testing.T) {
	setupReachTestDB(t)
	result, ec := ListConnectorProblemUsers(ListConnectorProblemUsersReq{
		Version: "4.3.5", Page: 0, PageSize: 500,
	})
	require.Nil(t, ec)
	require.Equal(t, 1, result.Page)
	require.Equal(t, 20, result.PageSize)
}

// channel 漏传时必须只走邮件：短信模板号还没报备，默认回落 auto 会把人意外短信轰一遍。
func TestNotifyConnectorProblemUsers_DefaultsToEmailOnly(t *testing.T) {
	setupReachTestDB(t)
	restoreDirectReachHooks(t)
	sent := stubNotifyEmailTemplate(t)
	seedNotifyOwner(t, 2108, "默认渠道用户", "default@example.com", "8000")
	require.NoError(t, store.DB.Model(&model.User{}).Where("id = ?", 2108).
		Updates(map[string]any{"phone_e164": "+8613800138005", "phone_country": "+86"}).Error)

	sendDirectReachSMS = func(context.Context, ReachSMSRequest) error {
		t.Fatal("channel 缺省时不该走短信")
		return nil
	}

	results, ec := NotifyConnectorProblemUsers(context.Background(), NotifyConnectorProblemUsersReq{
		Version: "4.3.5", UserIDs: []int64{2108}, Body: "请手动重装连接器。",
	})
	require.Nil(t, ec)
	require.Equal(t, ConnectorNotifyChannelEmail, results[0].Channel)
	require.Equal(t, model.ReachSendStatusSent, results[0].Status)
	require.Len(t, *sent, 1)

	// 幂等键也要落在 email 上，而不是 auto。
	var task model.ReachTask
	require.NoError(t, store.DB.Where("id = ?", results[0].TaskID).First(&task).Error)
	require.Equal(t, "connector_upgrade:4.3.5:2108:email", *task.DedupKey)
}

// 邮件发不出去时也不许偷偷回落短信——只有显式 auto 才回落。
func TestNotifyConnectorProblemUsers_DefaultDoesNotFallBackToSMS(t *testing.T) {
	setupReachTestDB(t)
	restoreDirectReachHooks(t)
	stubNotifyEmailTemplate(t)
	seedNotifyOwner(t, 2109, "无邮箱默认用户", "", "8000")
	require.NoError(t, store.DB.Model(&model.User{}).Where("id = ?", 2109).
		Updates(map[string]any{"phone_e164": "+8613800138006", "phone_country": "+86"}).Error)

	sendDirectReachSMS = func(context.Context, ReachSMSRequest) error {
		t.Fatal("缺省渠道下即使没有邮箱也不该走短信")
		return nil
	}

	results, ec := NotifyConnectorProblemUsers(context.Background(), NotifyConnectorProblemUsersReq{
		Version: "4.3.5", UserIDs: []int64{2109}, Body: "请手动重装连接器。",
	})
	require.Nil(t, ec)
	require.Equal(t, model.ReachSendStatusFailed, results[0].Status)
}

// agent 被删掉之后（查不到 owner），自愈判定不能整段跳过，也不能让无主的同名机器互相抵消。
func TestListConnectorProblemUsers_SelfHealSurvivesDeletedAgent(t *testing.T) {
	setupReachTestDB(t)
	base := time.Date(2026, 8, 20, 10, 0, 0, 0, time.UTC)
	host := "orphan.local"

	// 两台都没有 owner（agents 表里查不到）、同名、不同 agent 的机器。
	require.NoError(t, store.DB.Create(&model.ConnectorUpgradeReport{
		ID: 41, AgentID: 9001, ClientType: "grix-connector", FromVersion: "4.3.4", ToVersion: "4.3.5",
		Status: model.UpgradeReportFailed, HostName: &host, ReportedAt: base,
	}).Error)
	require.NoError(t, store.DB.Create(&model.ConnectorUpgradeReport{
		ID: 42, AgentID: 9002, ClientType: "grix-connector", FromVersion: "4.3.4", ToVersion: "4.3.5",
		Status: model.UpgradeReportSuccess, HostName: &host, ReportedAt: base.Add(time.Hour),
	}).Error)
	// 同一台无主机器自己后来报了成功，这条必须被认出来。
	require.NoError(t, store.DB.Create(&model.ConnectorUpgradeReport{
		ID: 43, AgentID: 9001, ClientType: "grix-connector", FromVersion: "4.3.5", ToVersion: "4.3.6",
		Status: model.UpgradeReportSuccess, HostName: &host, ReportedAt: base.Add(2 * time.Hour),
	}).Error)

	hosts, ec := collectConnectorProblemHosts("4.3.5", "", normalizeProblemStatuses(nil), false)
	require.Nil(t, ec)
	owners, ec := resolveProblemHostOwners(hosts)
	require.Nil(t, ec)
	require.NotEmpty(t, owners.ownerAgentIDs, "没有 owner 时也要留下候选 agent，否则自愈整段跳过")
	require.NoError(t, dropSelfHealedHosts(hosts, owners, ""))
	require.Empty(t, hosts, "无主机器自己报的成功必须被认出来")
}

func TestMaskUserPhone_ShortLegacyNumberIsHidden(t *testing.T) {
	require.Equal(t, "****8000", MaskUserPhone(model.User{PhoneLast4: "8000"}))
	require.Equal(t, "****8000", MaskUserPhone(model.User{PhoneE164: "+8613800138000"}))
	// 存量脏数据不足四位时不下发，否则整串号码会被当脱敏串输出。
	require.Equal(t, "", MaskUserPhone(model.User{PhoneE164: "123"}))
	require.Equal(t, "", MaskUserPhone(model.User{}))
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
	// 短信通道没配（本用例未注册任何 provider）时，预览要把这件事说清楚而不是静默。
	require.NotEmpty(t, preview.SMSError)
}
