package service

import (
	"testing"
	"time"

	"github.com/askie/grix/backend/internal/model"
)

// --- 按 agent 熔断：同版本反复失败后跳过该 release 继续回退 ---

var breakerReportSeq int64 = 70000

func seedBreakerReports(t *testing.T, agentID int64, version, status string, n int, base time.Time) {
	t.Helper()
	for i := 0; i < n; i++ {
		breakerReportSeq++
		seedUpgradeReport(t, breakerReportSeq, agentID, "inst-"+version, version, status, "", base.Add(time.Duration(i)*time.Minute))
	}
}

func seedBreakerReleases(t *testing.T) {
	t.Helper()
	seedRelease(t, model.ConnectorRelease{ID: 9101, Version: "4.5.1", Channel: "stable", Status: model.ReleaseStatusPublished})
	seedRelease(t, model.ConnectorRelease{ID: 9102, Version: "4.3.9", Channel: "stable", Status: model.ReleaseStatusPublished})
}

func checkFor(t *testing.T, agentID int64) *CheckUpgradeResp {
	t.Helper()
	resp, ec := CheckUpgrade(CheckUpgradeReq{ClientVersion: "3.9.0", Channel: "stable", AgentID: agentID})
	if ec != nil {
		t.Fatalf("unexpected error: %v", ec)
	}
	return resp
}

func TestCheckUpgrade_BreakerSkipsRepeatedlyFailedVersion(t *testing.T) {
	_, cleanup := setupUpgradeServiceTest(t)
	defer cleanup()
	seedBreakerReleases(t)
	seedBreakerReports(t, 1, "4.5.1", model.UpgradeReportRolledBack, 3, time.Now().Add(-time.Hour))

	resp := checkFor(t, 1)
	if !resp.Available || resp.Release.Version != "4.3.9" {
		t.Fatalf("expected fallback to 4.3.9, got %+v", resp)
	}
	// 其他 agent 不受影响
	other := checkFor(t, 2)
	if !other.Available || other.Release.Version != "4.5.1" {
		t.Fatalf("other agent should still get 4.5.1, got %+v", other)
	}
}

func TestCheckUpgrade_BreakerBelowThresholdAndMixedStatus(t *testing.T) {
	_, cleanup := setupUpgradeServiceTest(t)
	defer cleanup()
	seedBreakerReleases(t)
	// 2 次 rolled_back + 1 次 failed = 3 次，混合状态一起计数
	seedBreakerReports(t, 1, "4.5.1", model.UpgradeReportRolledBack, 2, time.Now().Add(-2*time.Hour))
	if resp := checkFor(t, 1); resp.Release == nil || resp.Release.Version != "4.5.1" {
		t.Fatalf("2 failures should not trip, got %+v", resp)
	}
	seedUpgradeReport(t, 79999, 1, "inst-4.5.1", "4.5.1", model.UpgradeReportFailed, "", time.Now().Add(-time.Hour))
	if resp := checkFor(t, 1); resp.Release == nil || resp.Release.Version != "4.3.9" {
		t.Fatalf("3 mixed failures should trip, got %+v", resp)
	}
}

func TestCheckUpgrade_BreakerIgnoresStaleFailures(t *testing.T) {
	_, cleanup := setupUpgradeServiceTest(t)
	defer cleanup()
	seedBreakerReleases(t)
	seedBreakerReports(t, 1, "4.5.1", model.UpgradeReportRolledBack, 5, time.Now().Add(-8*24*time.Hour))

	if resp := checkFor(t, 1); resp.Release == nil || resp.Release.Version != "4.5.1" {
		t.Fatalf("failures older than window should not trip, got %+v", resp)
	}
}

func TestCheckUpgrade_BreakerResetsAfterSuccess(t *testing.T) {
	_, cleanup := setupUpgradeServiceTest(t)
	defer cleanup()
	seedBreakerReleases(t)
	seedBreakerReports(t, 1, "4.5.1", model.UpgradeReportRolledBack, 3, time.Now().Add(-3*time.Hour))
	seedUpgradeReport(t, 79998, 1, "inst-4.5.1", "4.5.1", model.UpgradeReportSuccess, "", time.Now().Add(-2*time.Hour))
	seedUpgradeReport(t, 79997, 1, "inst-4.5.1", "4.5.1", model.UpgradeReportRolledBack, "", time.Now().Add(-time.Hour))

	if resp := checkFor(t, 1); resp.Release == nil || resp.Release.Version != "4.5.1" {
		t.Fatalf("failures before the last success must not count, got %+v", resp)
	}
}

func TestCheckUpgrade_BreakerBypassedByForceRelease(t *testing.T) {
	_, cleanup := setupUpgradeServiceTest(t)
	defer cleanup()
	seedRelease(t, model.ConnectorRelease{ID: 9101, Version: "4.5.1", Channel: "stable", Status: model.ReleaseStatusPublished, Force: true})
	seedRelease(t, model.ConnectorRelease{ID: 9102, Version: "4.3.9", Channel: "stable", Status: model.ReleaseStatusPublished})
	seedBreakerReports(t, 1, "4.5.1", model.UpgradeReportRolledBack, 3, time.Now().Add(-time.Hour))

	if resp := checkFor(t, 1); resp.Release == nil || resp.Release.Version != "4.5.1" {
		t.Fatalf("force release must bypass breaker, got %+v", resp)
	}
}

func TestCheckUpgrade_BreakerCascadesDownVersions(t *testing.T) {
	_, cleanup := setupUpgradeServiceTest(t)
	defer cleanup()
	seedBreakerReleases(t)
	seedBreakerReports(t, 1, "4.5.1", model.UpgradeReportRolledBack, 3, time.Now().Add(-time.Hour))
	seedBreakerReports(t, 1, "4.3.9", model.UpgradeReportRolledBack, 3, time.Now().Add(-time.Hour))

	if resp := checkFor(t, 1); resp.Available {
		t.Fatalf("all versions tripped should yield no update, got %+v", resp)
	}
}
