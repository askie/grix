package service

import (
	"math"
	"testing"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/testutil"
	"github.com/askie/grix/backend/internal/store"
)

func setupUpgradeServiceTest(t *testing.T) (*testutil.TestDB, func()) {
	t.Helper()
	testDB := testutil.NewTestDB()
	store.DB = testDB.DB
	store.RDB = testutil.NewMockRedis()
	// 关闭失败上报后的异步 auto-pause：该 fire-and-forget goroutine 读全局 store.DB，
	// 会跨用例边界执行、提前暂停下一个用例的规则，导致 AutoPause 测试偶发失败。
	// 单测通过显式调用 AutoPauseCheck() 验证逻辑，不依赖该异步触发。
	autoPauseAsyncOnReport = false
	return testDB, func() {
		autoPauseAsyncOnReport = true
		testDB.Close()
	}
}

func seedRelease(t *testing.T, r model.ConnectorRelease) model.ConnectorRelease {
	t.Helper()
	if r.ID == 0 {
		r.ID = 9001
	}
	if err := store.DB.Create(&r).Error; err != nil {
		t.Fatalf("seed release: %v", err)
	}
	return r
}

func seedRolloutRule(t *testing.T, rule model.ConnectorRolloutRule) model.ConnectorRolloutRule {
	t.Helper()
	if rule.ID == 0 {
		rule.ID = 8001
	}
	if err := store.DB.Create(&rule).Error; err != nil {
		t.Fatalf("seed rollout rule: %v", err)
	}
	return rule
}

// --- CheckUpgrade tests ---

func TestCheckUpgrade_NoPublishedRelease(t *testing.T) {
	_, cleanup := setupUpgradeServiceTest(t)
	defer cleanup()

	result, ec := CheckUpgrade(CheckUpgradeReq{
		ClientVersion: "0.2.0",
		Channel:       "stable",
	})
	if ec != nil {
		t.Fatalf("unexpected error: %v", ec)
	}
	if result.Available {
		t.Error("should not be available when no releases exist")
	}
}

func TestCheckUpgrade_DraftReleaseNotVisible(t *testing.T) {
	_, cleanup := setupUpgradeServiceTest(t)
	defer cleanup()

	seedRelease(t, model.ConnectorRelease{
		ID:      9001,
		Version: "0.3.0",
		Channel: "stable",
		Status:  model.ReleaseStatusDraft,
	})

	result, ec := CheckUpgrade(CheckUpgradeReq{
		ClientVersion: "0.2.0",
		Channel:       "stable",
	})
	if ec != nil {
		t.Fatalf("unexpected error: %v", ec)
	}
	if result.Available {
		t.Error("draft release should not be visible")
	}
}

func TestCheckUpgrade_PublishedReleaseAvailable(t *testing.T) {
	_, cleanup := setupUpgradeServiceTest(t)
	defer cleanup()

	seedRelease(t, model.ConnectorRelease{
		ID:      9001,
		Version: "0.3.0",
		Channel: "stable",
		Status:  model.ReleaseStatusPublished,
	})

	result, ec := CheckUpgrade(CheckUpgradeReq{
		ClientVersion: "0.2.0",
		Channel:       "stable",
	})
	if ec != nil {
		t.Fatalf("unexpected error: %v", ec)
	}
	if !result.Available {
		t.Error("should be available")
	}
	if result.Release.Version != "0.3.0" {
		t.Errorf("expected version 0.3.0, got %s", result.Release.Version)
	}
}

func TestCheckUpgrade_OlderVersionNotReturned(t *testing.T) {
	_, cleanup := setupUpgradeServiceTest(t)
	defer cleanup()

	seedRelease(t, model.ConnectorRelease{
		ID:      9001,
		Version: "0.1.0",
		Channel: "stable",
		Status:  model.ReleaseStatusPublished,
	})

	result, ec := CheckUpgrade(CheckUpgradeReq{
		ClientVersion: "0.2.0",
		Channel:       "stable",
	})
	if ec != nil {
		t.Fatalf("unexpected error: %v", ec)
	}
	if result.Available {
		t.Error("older version should not be returned")
	}
}

func TestCheckUpgrade_SameVersionNotReturned(t *testing.T) {
	_, cleanup := setupUpgradeServiceTest(t)
	defer cleanup()

	seedRelease(t, model.ConnectorRelease{
		ID:      9001,
		Version: "0.2.0",
		Channel: "stable",
		Status:  model.ReleaseStatusPublished,
	})

	result, ec := CheckUpgrade(CheckUpgradeReq{
		ClientVersion: "0.2.0",
		Channel:       "stable",
	})
	if ec != nil {
		t.Fatalf("unexpected error: %v", ec)
	}
	if result.Available {
		t.Error("same version should not trigger upgrade")
	}
}

func TestCheckUpgrade_MinVersionFilter(t *testing.T) {
	_, cleanup := setupUpgradeServiceTest(t)
	defer cleanup()

	minVer := "0.3.0"
	seedRelease(t, model.ConnectorRelease{
		ID:         9001,
		Version:    "0.5.0",
		Channel:    "stable",
		Status:     model.ReleaseStatusPublished,
		MinVersion: &minVer,
	})

	// Client is 0.2.0, min_version is 0.3.0 → should not be available
	result, ec := CheckUpgrade(CheckUpgradeReq{
		ClientVersion: "0.2.0",
		Channel:       "stable",
	})
	if ec != nil {
		t.Fatalf("unexpected error: %v", ec)
	}
	if result.Available {
		t.Error("should not be available when client below min_version")
	}

	// Client is 0.3.0, min_version is 0.3.0 → should be available
	result2, ec := CheckUpgrade(CheckUpgradeReq{
		ClientVersion: "0.3.0",
		Channel:       "stable",
	})
	if ec != nil {
		t.Fatalf("unexpected error: %v", ec)
	}
	if !result2.Available {
		t.Error("should be available when client meets min_version")
	}
}

func TestCheckUpgrade_ChannelMismatch(t *testing.T) {
	_, cleanup := setupUpgradeServiceTest(t)
	defer cleanup()

	seedRelease(t, model.ConnectorRelease{
		ID:      9001,
		Version: "0.3.0",
		Channel: "beta",
		Status:  model.ReleaseStatusPublished,
	})

	result, ec := CheckUpgrade(CheckUpgradeReq{
		ClientVersion: "0.2.0",
		Channel:       "stable",
	})
	if ec != nil {
		t.Fatalf("unexpected error: %v", ec)
	}
	if result.Available {
		t.Error("different channel should not match")
	}
}

func TestCheckUpgrade_PercentageRule(t *testing.T) {
	_, cleanup := setupUpgradeServiceTest(t)
	defer cleanup()

	release := seedRelease(t, model.ConnectorRelease{
		ID:      9001,
		Version: "0.3.0",
		Channel: "stable",
		Status:  model.ReleaseStatusPublished,
	})
	seedRolloutRule(t, model.ConnectorRolloutRule{
		ID:        8001,
		ReleaseID: release.ID,
		RuleType:  "percentage",
		RuleValue: []byte(`{"percent":0}`),
		Priority:  10,
		Status:    model.RolloutRuleActive,
	})

	// 0% rollout → should not match
	result, ec := CheckUpgrade(CheckUpgradeReq{
		ClientVersion: "0.2.0",
		Channel:       "stable",
	})
	if ec != nil {
		t.Fatalf("unexpected error: %v", ec)
	}
	if result.Available {
		t.Error("0% rollout should not match")
	}
}

func TestCheckUpgrade_AgentListRule(t *testing.T) {
	_, cleanup := setupUpgradeServiceTest(t)
	defer cleanup()

	release := seedRelease(t, model.ConnectorRelease{
		ID:      9001,
		Version: "0.3.0",
		Channel: "stable",
		Status:  model.ReleaseStatusPublished,
	})

	// Rule targeting specific agent IDs
	seedRolloutRule(t, model.ConnectorRolloutRule{
		ID:        8001,
		ReleaseID: release.ID,
		RuleType:  "agent_list",
		RuleValue: []byte(`{"agent_ids":[111,222,333]}`),
		Priority:  10,
		Status:    model.RolloutRuleActive,
	})

	// Agent not in list → should not match
	result, ec := CheckUpgrade(CheckUpgradeReq{
		ClientVersion: "0.2.0",
		Channel:       "stable",
		AgentID:       999,
	})
	if ec != nil {
		t.Fatalf("unexpected error: %v", ec)
	}
	if result.Available {
		t.Error("agent not in list should not match")
	}

	// Agent in list → should match
	result2, ec := CheckUpgrade(CheckUpgradeReq{
		ClientVersion: "0.2.0",
		Channel:       "stable",
		AgentID:       222,
	})
	if ec != nil {
		t.Fatalf("unexpected error: %v", ec)
	}
	if !result2.Available {
		t.Error("agent in list should match")
	}
}

func TestCheckUpgrade_PausedRuleIgnored(t *testing.T) {
	_, cleanup := setupUpgradeServiceTest(t)
	defer cleanup()

	release := seedRelease(t, model.ConnectorRelease{
		ID:      9001,
		Version: "0.3.0",
		Channel: "stable",
		Status:  model.ReleaseStatusPublished,
	})

	seedRolloutRule(t, model.ConnectorRolloutRule{
		ID:        8001,
		ReleaseID: release.ID,
		RuleType:  "agent_list",
		RuleValue: []byte(`{"agent_ids":[111]}`),
		Priority:  10,
		Status:    model.RolloutRulePaused,
	})

	result, ec := CheckUpgrade(CheckUpgradeReq{
		ClientVersion: "0.2.0",
		Channel:       "stable",
		AgentID:       111,
	})
	if ec != nil {
		t.Fatalf("unexpected error: %v", ec)
	}
	if result.Available {
		t.Error("paused rule should be ignored")
	}
}

func TestCheckUpgrade_RevokedReleaseNotVisible(t *testing.T) {
	_, cleanup := setupUpgradeServiceTest(t)
	defer cleanup()

	seedRelease(t, model.ConnectorRelease{
		ID:      9001,
		Version: "0.3.0",
		Channel: "stable",
		Status:  model.ReleaseStatusRevoked,
	})

	result, ec := CheckUpgrade(CheckUpgradeReq{
		ClientVersion: "0.2.0",
		Channel:       "stable",
	})
	if ec != nil {
		t.Fatalf("unexpected error: %v", ec)
	}
	if result.Available {
		t.Error("revoked release should not be visible")
	}
}

// --- ReportUpgrade tests ---

func TestReportUpgrade_Success(t *testing.T) {
	_, cleanup := setupUpgradeServiceTest(t)
	defer cleanup()

	nodeVer := "v20.11.0"
	plat := "darwin"
	arch := "arm64"
	ec := ReportUpgrade(ReportUpgradeReq{
		AgentID:     123,
		FromVersion: "0.2.0",
		ToVersion:   "0.3.0",
		Status:      model.UpgradeReportSuccess,
		NodeVersion: &nodeVer,
		Platform:    &plat,
		Arch:        &arch,
	})
	if ec != nil {
		t.Fatalf("unexpected error: %v", ec)
	}

	var report model.ConnectorUpgradeReport
	if err := store.DB.First(&report).Error; err != nil {
		t.Fatalf("report not found: %v", err)
	}
	if report.AgentID != 123 {
		t.Errorf("expected agent_id 123, got %d", report.AgentID)
	}
	if report.Status != model.UpgradeReportSuccess {
		t.Errorf("expected status success, got %s", report.Status)
	}
}

func TestReportUpgrade_ClampsOversizedTelemetry(t *testing.T) {
	_, cleanup := setupUpgradeServiceTest(t)
	defer cleanup()

	// 旧版 connector 用 Number.MAX_SAFE_INTEGER 当"磁盘充足"哨兵值上报，
	// 超出 int4 列上限会让整条 INSERT 失败。验证服务端钳制后上报仍能落库。
	maxSafeInt := 9007199254740991
	hugeDuration := 5000000000
	ec := ReportUpgrade(ReportUpgradeReq{
		AgentID:     789,
		FromVersion: "0.1.0",
		ToVersion:   "3.1.2",
		Status:      model.UpgradeReportFailed,
		DiskFreeMb:  &maxSafeInt,
		DurationMs:  &hugeDuration,
	})
	if ec != nil {
		t.Fatalf("oversized telemetry should not fail the report insert, got: %v", ec)
	}

	var report model.ConnectorUpgradeReport
	if err := store.DB.Where("agent_id = ?", 789).First(&report).Error; err != nil {
		t.Fatalf("report not found: %v", err)
	}
	if report.DiskFreeMb == nil || *report.DiskFreeMb != math.MaxInt32 {
		t.Errorf("expected disk_free_mb clamped to %d, got %v", math.MaxInt32, report.DiskFreeMb)
	}
	if report.DurationMs == nil || *report.DurationMs != math.MaxInt32 {
		t.Errorf("expected duration_ms clamped to %d, got %v", math.MaxInt32, report.DurationMs)
	}
}

func TestReportUpgrade_Failed(t *testing.T) {
	_, cleanup := setupUpgradeServiceTest(t)
	defer cleanup()

	errCode := "NPM_INSTALL_FAILED"
	errMsg := "network error"
	ec := ReportUpgrade(ReportUpgradeReq{
		AgentID:      456,
		FromVersion:  "0.2.0",
		ToVersion:    "0.3.0",
		Status:       model.UpgradeReportFailed,
		ErrorCode:    &errCode,
		ErrorMsg:     &errMsg,
	})
	if ec != nil {
		t.Fatalf("unexpected error: %v", ec)
	}

	var report model.ConnectorUpgradeReport
	store.DB.First(&report)
	if report.Status != model.UpgradeReportFailed {
		t.Errorf("expected status failed, got %s", report.Status)
	}
	if report.ErrorCode == nil || *report.ErrorCode != "NPM_INSTALL_FAILED" {
		t.Error("error_code not stored")
	}
}

// 回归：多条已发布版本并存时，必须按 semver 选最高版，而不是字符串排序。
// "1.5.6" > "1.5.10" 是字符串序的坑，老的 Order("version DESC") 会错选 1.5.6。
func TestCheckUpgrade_PicksHighestSemverAmongPublished(t *testing.T) {
	_, cleanup := setupUpgradeServiceTest(t)
	defer cleanup()

	seedRelease(t, model.ConnectorRelease{
		ID:      9101,
		Version: "1.5.6",
		Channel: "stable",
		Status:  model.ReleaseStatusPublished,
	})
	seedRelease(t, model.ConnectorRelease{
		ID:      9102,
		Version: "1.5.10",
		Channel: "stable",
		Status:  model.ReleaseStatusPublished,
	})

	result, ec := CheckUpgrade(CheckUpgradeReq{
		ClientVersion: "1.5.7",
		Channel:       "stable",
	})
	if ec != nil {
		t.Fatalf("unexpected error: %v", ec)
	}
	if !result.Available {
		t.Fatal("should be available: 1.5.10 > 1.5.7")
	}
	if result.Release.Version != "1.5.10" {
		t.Errorf("expected highest semver 1.5.10, got %s (lexical sort bug?)", result.Release.Version)
	}
}
