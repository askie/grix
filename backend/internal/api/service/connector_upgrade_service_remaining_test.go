package service

import (
	"testing"

	"github.com/askie/grix/backend/internal/model"
)

// --- Pause/Resume release tests ---

func TestPauseConnectorRelease(t *testing.T) {
	_, cleanup := setupUpgradeServiceTest(t)
	defer cleanup()

	release := seedRelease(t, model.ConnectorRelease{
		ID:      9001,
		Version: "0.3.0",
		Channel: "stable",
		Status:  model.ReleaseStatusPublished,
	})

	result, ec := PauseConnectorRelease(release.ID)
	if ec != nil {
		t.Fatalf("unexpected error: %v", ec)
	}
	if result.Status != model.ReleaseStatusPaused {
		t.Errorf("expected status paused (4), got %d", result.Status)
	}
}

func TestPauseConnectorRelease_NotPublished(t *testing.T) {
	_, cleanup := setupUpgradeServiceTest(t)
	defer cleanup()

	release := seedRelease(t, model.ConnectorRelease{
		ID:      9001,
		Version: "0.3.0",
		Channel: "stable",
		Status:  model.ReleaseStatusDraft,
	})

	_, ec := PauseConnectorRelease(release.ID)
	if ec == nil {
		t.Fatal("should fail to pause a draft release")
	}
}

func TestResumeConnectorRelease(t *testing.T) {
	_, cleanup := setupUpgradeServiceTest(t)
	defer cleanup()

	release := seedRelease(t, model.ConnectorRelease{
		ID:      9001,
		Version: "0.3.0",
		Channel: "stable",
		Status:  model.ReleaseStatusPaused,
	})

	result, ec := ResumeConnectorRelease(release.ID)
	if ec != nil {
		t.Fatalf("unexpected error: %v", ec)
	}
	if result.Status != model.ReleaseStatusPublished {
		t.Errorf("expected status published (2), got %d", result.Status)
	}
}

// --- Upgrade report list tests ---

func TestListUpgradeReports(t *testing.T) {
	_, cleanup := setupUpgradeServiceTest(t)
	defer cleanup()

	// Seed reports
	ReportUpgrade(ReportUpgradeReq{
		AgentID:     1,
		FromVersion: "0.2.0",
		ToVersion:   "0.3.0",
		Status:      model.UpgradeReportSuccess,
	})
	ReportUpgrade(ReportUpgradeReq{
		AgentID:     2,
		FromVersion: "0.2.0",
		ToVersion:   "0.3.0",
		Status:      model.UpgradeReportFailed,
		ErrorCode:   strPtr("NPM_INSTALL_FAILED"),
	})
	ReportUpgrade(ReportUpgradeReq{
		AgentID:     3,
		FromVersion: "0.1.0",
		ToVersion:   "0.3.0",
		Status:      model.UpgradeReportSuccess,
	})

	// List all
	result, ec := ListUpgradeReports(ListUpgradeReportsReq{})
	if ec != nil {
		t.Fatalf("unexpected error: %v", ec)
	}
	if result.Total != 3 {
		t.Errorf("expected total 3, got %d", result.Total)
	}
	if len(result.Reports) != 3 {
		t.Errorf("expected 3 reports, got %d", len(result.Reports))
	}

	// Filter by version
	result2, ec := ListUpgradeReports(ListUpgradeReportsReq{ToVersion: "0.3.0"})
	if ec != nil {
		t.Fatalf("unexpected error: %v", ec)
	}
	if result2.Total != 3 {
		t.Errorf("expected 3 reports for version 0.3.0, got %d", result2.Total)
	}

	// Filter by status
	result3, ec := ListUpgradeReports(ListUpgradeReportsReq{Status: model.UpgradeReportFailed})
	if ec != nil {
		t.Fatalf("unexpected error: %v", ec)
	}
	if result3.Total != 1 {
		t.Errorf("expected 1 failed report, got %d", result3.Total)
	}

	// Filter by agent
	result4, ec := ListUpgradeReports(ListUpgradeReportsReq{AgentID: int64Ptr(1)})
	if ec != nil {
		t.Fatalf("unexpected error: %v", ec)
	}
	if result4.Total != 1 {
		t.Errorf("expected 1 report for agent 1, got %d", result4.Total)
	}
}

func TestListUpgradeReports_Pagination(t *testing.T) {
	_, cleanup := setupUpgradeServiceTest(t)
	defer cleanup()

	for i := 0; i < 5; i++ {
		ReportUpgrade(ReportUpgradeReq{
			AgentID:     int64(i + 1),
			FromVersion: "0.2.0",
			ToVersion:   "0.3.0",
			Status:      model.UpgradeReportSuccess,
		})
	}

	// Page 1, size 2
	result, ec := ListUpgradeReports(ListUpgradeReportsReq{Page: 1, PageSize: 2})
	if ec != nil {
		t.Fatalf("unexpected error: %v", ec)
	}
	if result.Total != 5 {
		t.Errorf("expected total 5, got %d", result.Total)
	}
	if len(result.Reports) != 2 {
		t.Errorf("expected 2 reports on page 1, got %d", len(result.Reports))
	}

	// Page 3, size 2 — only 1 report
	result2, _ := ListUpgradeReports(ListUpgradeReportsReq{Page: 3, PageSize: 2})
	if len(result2.Reports) != 1 {
		t.Errorf("expected 1 report on page 3, got %d", len(result2.Reports))
	}
}

// --- Agent upgrade history ---

func TestGetAgentUpgradeHistory(t *testing.T) {
	_, cleanup := setupUpgradeServiceTest(t)
	defer cleanup()

	ReportUpgrade(ReportUpgradeReq{
		AgentID:     100,
		FromVersion: "0.1.0",
		ToVersion:   "0.2.0",
		Status:      model.UpgradeReportSuccess,
	})
	ReportUpgrade(ReportUpgradeReq{
		AgentID:     100,
		FromVersion: "0.2.0",
		ToVersion:   "0.3.0",
		Status:      model.UpgradeReportFailed,
		ErrorCode:   strPtr("NPM_INSTALL_FAILED"),
	})

	reports, ec := GetAgentUpgradeHistory(100)
	if ec != nil {
		t.Fatalf("unexpected error: %v", ec)
	}
	if len(reports) != 2 {
		t.Fatalf("expected 2 reports, got %d", len(reports))
	}
	// Should be ordered by reported_at DESC (latest first)
	if reports[0].ToVersion != "0.3.0" {
		t.Errorf("expected latest report to_version 0.3.0, got %s", reports[0].ToVersion)
	}
}

// --- Enhanced stats with error distribution ---

func TestGetUpgradeStatsDetail(t *testing.T) {
	_, cleanup := setupUpgradeServiceTest(t)
	defer cleanup()

	ReportUpgrade(ReportUpgradeReq{
		AgentID:     1,
		FromVersion: "0.2.0",
		ToVersion:   "0.3.0",
		Status:      model.UpgradeReportSuccess,
		DurationMs:  intPtr(5000),
	})
	ReportUpgrade(ReportUpgradeReq{
		AgentID:     2,
		FromVersion: "0.2.0",
		ToVersion:   "0.3.0",
		Status:      model.UpgradeReportFailed,
		ErrorCode:   strPtr("NPM_INSTALL_FAILED"),
		DurationMs:  intPtr(3000),
	})
	ReportUpgrade(ReportUpgradeReq{
		AgentID:     3,
		FromVersion: "0.2.0",
		ToVersion:   "0.3.0",
		Status:      model.UpgradeReportFailed,
		ErrorCode:   strPtr("STARTUP_CRASH"),
		DurationMs:  intPtr(1000),
	})

	stats, ec := GetUpgradeStatsDetail("0.3.0", "")
	if ec != nil {
		t.Fatalf("unexpected error: %v", ec)
	}
	if stats.Total != 3 {
		t.Errorf("expected total 3, got %d", stats.Total)
	}
	if stats.Failed != 2 {
		t.Errorf("expected failed 2, got %d", stats.Failed)
	}
	if len(stats.ErrorDistribution) != 2 {
		t.Fatalf("expected 2 error types, got %d", len(stats.ErrorDistribution))
	}
	if stats.ErrorDistribution["NPM_INSTALL_FAILED"] != 1 {
		t.Error("NPM_INSTALL_FAILED count should be 1")
	}
	if stats.ErrorDistribution["STARTUP_CRASH"] != 1 {
		t.Error("STARTUP_CRASH count should be 1")
	}
}

// --- Helpers ---

func strPtr(s string) *string { return &s }
func intPtr(i int) *int       { return &i }
