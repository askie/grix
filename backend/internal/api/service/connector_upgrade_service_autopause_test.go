package service

import (
	"testing"

	"github.com/askie/grix/backend/internal/model"
)

// --- Auto-pause tests ---

func TestAutoPause_BelowThreshold(t *testing.T) {
	_, cleanup := setupUpgradeServiceTest(t)
	defer cleanup()

	release := seedRelease(t, model.ConnectorRelease{
		ID:      9001,
		Version: "0.3.0",
		Channel: "stable",
		Status:  model.ReleaseStatusPublished,
	})
	seedRolloutRule(t, model.ConnectorRolloutRule{
		ID:              8001,
		ReleaseID:       release.ID,
		RuleType:        "percentage",
		RuleValue:       []byte(`{"percent":50}`),
		Priority:        10,
		Status:          model.RolloutRuleActive,
		AutoPauseConfig: []byte(`{"failure_rate_threshold":0.3,"min_sample_size":3,"window_minutes":30}`),
	})

	// 3 successes, 0 failures → failure rate 0% → should NOT pause
	for i := 0; i < 3; i++ {
		ReportUpgrade(ReportUpgradeReq{
			AgentID:     int64(i + 1),
			FromVersion: "0.2.0",
			ToVersion:   "0.3.0",
			Status:      model.UpgradeReportSuccess,
		})
	}

	paused, ec := AutoPauseCheck()
	if ec != nil {
		t.Fatalf("unexpected error: %v", ec)
	}
	if len(paused) != 0 {
		t.Errorf("expected 0 rules paused, got %d", len(paused))
	}

	// Verify rule still active
	rules, _ := ListRolloutRules(release.ID)
	if rules[0].Status != model.RolloutRuleActive {
		t.Error("rule should still be active")
	}
}

func TestAutoPause_AboveThreshold(t *testing.T) {
	_, cleanup := setupUpgradeServiceTest(t)
	defer cleanup()

	release := seedRelease(t, model.ConnectorRelease{
		ID:      9001,
		Version: "0.3.0",
		Channel: "stable",
		Status:  model.ReleaseStatusPublished,
	})
	seedRolloutRule(t, model.ConnectorRolloutRule{
		ID:              8001,
		ReleaseID:       release.ID,
		RuleType:        "percentage",
		RuleValue:       []byte(`{"percent":50}`),
		Priority:        10,
		Status:          model.RolloutRuleActive,
		AutoPauseConfig: []byte(`{"failure_rate_threshold":0.3,"min_sample_size":3,"window_minutes":30}`),
	})

	// 1 success + 2 failures → failure rate 66% → should pause
	ReportUpgrade(ReportUpgradeReq{
		AgentID:     1,
		FromVersion: "0.2.0",
		ToVersion:   "0.3.0",
		Status:      model.UpgradeReportSuccess,
	})
	for i := 0; i < 2; i++ {
		ReportUpgrade(ReportUpgradeReq{
			AgentID:     int64(i + 10),
			FromVersion: "0.2.0",
			ToVersion:   "0.3.0",
			Status:      model.UpgradeReportFailed,
		})
	}

	paused, ec := AutoPauseCheck()
	if ec != nil {
		t.Fatalf("unexpected error: %v", ec)
	}
	if len(paused) != 1 {
		t.Fatalf("expected 1 rule paused, got %d", len(paused))
	}
	if paused[0].ID != 8001 {
		t.Errorf("expected rule 8001 paused, got %d", paused[0].ID)
	}

	// Verify rule is now paused
	rules, _ := ListRolloutRules(release.ID)
	if rules[0].Status != model.RolloutRulePaused {
		t.Error("rule should be paused")
	}
}

func TestAutoPause_BelowMinSample(t *testing.T) {
	_, cleanup := setupUpgradeServiceTest(t)
	defer cleanup()

	release := seedRelease(t, model.ConnectorRelease{
		ID:      9001,
		Version: "0.3.0",
		Channel: "stable",
		Status:  model.ReleaseStatusPublished,
	})
	seedRolloutRule(t, model.ConnectorRolloutRule{
		ID:              8001,
		ReleaseID:       release.ID,
		RuleType:        "percentage",
		RuleValue:       []byte(`{"percent":50}`),
		Priority:        10,
		Status:          model.RolloutRuleActive,
		AutoPauseConfig: []byte(`{"failure_rate_threshold":0.3,"min_sample_size":5,"window_minutes":30}`),
	})

	// Only 2 failures (below min_sample_size of 5) → should NOT pause
	ReportUpgrade(ReportUpgradeReq{
		AgentID:     1,
		FromVersion: "0.2.0",
		ToVersion:   "0.3.0",
		Status:      model.UpgradeReportFailed,
	})
	ReportUpgrade(ReportUpgradeReq{
		AgentID:     2,
		FromVersion: "0.2.0",
		ToVersion:   "0.3.0",
		Status:      model.UpgradeReportFailed,
	})

	paused, ec := AutoPauseCheck()
	if ec != nil {
		t.Fatalf("unexpected error: %v", ec)
	}
	if len(paused) != 0 {
		t.Errorf("expected 0 rules paused (below min_sample), got %d", len(paused))
	}
}

func TestAutoPause_MaxTotalFailures(t *testing.T) {
	_, cleanup := setupUpgradeServiceTest(t)
	defer cleanup()

	release := seedRelease(t, model.ConnectorRelease{
		ID:      9001,
		Version: "0.3.0",
		Channel: "stable",
		Status:  model.ReleaseStatusPublished,
	})
	seedRolloutRule(t, model.ConnectorRolloutRule{
		ID:              8001,
		ReleaseID:       release.ID,
		RuleType:        "percentage",
		RuleValue:       []byte(`{"percent":50}`),
		Priority:        10,
		Status:          model.RolloutRuleActive,
		AutoPauseConfig: []byte(`{"failure_rate_threshold":0.5,"min_sample_size":3,"max_total_failures":3}`),
	})

	// 3 successes + 3 failures → rate 50% (at threshold, not above), but total failures >= 3 → pause
	for i := 0; i < 3; i++ {
		ReportUpgrade(ReportUpgradeReq{
			AgentID:     int64(i + 1),
			FromVersion: "0.2.0",
			ToVersion:   "0.3.0",
			Status:      model.UpgradeReportSuccess,
		})
	}
	for i := 0; i < 3; i++ {
		ReportUpgrade(ReportUpgradeReq{
			AgentID:     int64(i + 10),
			FromVersion: "0.2.0",
			ToVersion:   "0.3.0",
			Status:      model.UpgradeReportFailed,
		})
	}

	paused, ec := AutoPauseCheck()
	if ec != nil {
		t.Fatalf("unexpected error: %v", ec)
	}
	if len(paused) != 1 {
		t.Fatalf("expected 1 rule paused (max_total_failures), got %d", len(paused))
	}
}

func TestAutoPause_EmptyConfig(t *testing.T) {
	_, cleanup := setupUpgradeServiceTest(t)
	defer cleanup()

	release := seedRelease(t, model.ConnectorRelease{
		ID:      9001,
		Version: "0.3.0",
		Channel: "stable",
		Status:  model.ReleaseStatusPublished,
	})
	// Rule with empty auto_pause_config → should be skipped
	seedRolloutRule(t, model.ConnectorRolloutRule{
		ID:              8001,
		ReleaseID:       release.ID,
		RuleType:        "percentage",
		RuleValue:       []byte(`{"percent":50}`),
		Priority:        10,
		Status:          model.RolloutRuleActive,
		AutoPauseConfig: []byte(`{}`),
	})

	// Add failures
	ReportUpgrade(ReportUpgradeReq{
		AgentID:     1,
		FromVersion: "0.2.0",
		ToVersion:   "0.3.0",
		Status:      model.UpgradeReportFailed,
	})

	paused, ec := AutoPauseCheck()
	if ec != nil {
		t.Fatalf("unexpected error: %v", ec)
	}
	if len(paused) != 0 {
		t.Error("empty config should not trigger auto-pause")
	}
}

func TestAutoPause_AlreadyPausedSkipped(t *testing.T) {
	_, cleanup := setupUpgradeServiceTest(t)
	defer cleanup()

	release := seedRelease(t, model.ConnectorRelease{
		ID:      9001,
		Version: "0.3.0",
		Channel: "stable",
		Status:  model.ReleaseStatusPublished,
	})
	seedRolloutRule(t, model.ConnectorRolloutRule{
		ID:              8001,
		ReleaseID:       release.ID,
		RuleType:        "percentage",
		RuleValue:       []byte(`{"percent":50}`),
		Priority:        10,
		Status:          model.RolloutRulePaused,
		AutoPauseConfig: []byte(`{"failure_rate_threshold":0.1,"min_sample_size":1}`),
	})

	ReportUpgrade(ReportUpgradeReq{
		AgentID:     1,
		FromVersion: "0.2.0",
		ToVersion:   "0.3.0",
		Status:      model.UpgradeReportFailed,
	})

	paused, ec := AutoPauseCheck()
	if ec != nil {
		t.Fatalf("unexpected error: %v", ec)
	}
	if len(paused) != 0 {
		t.Error("already paused rule should be skipped")
	}
}
