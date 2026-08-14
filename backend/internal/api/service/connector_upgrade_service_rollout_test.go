package service

import (
	"testing"

	"github.com/askie/grix/backend/internal/model"
)

// --- Rollout rule CRUD tests ---

func TestCreateRolloutRule(t *testing.T) {
	_, cleanup := setupUpgradeServiceTest(t)
	defer cleanup()

	release := seedRelease(t, model.ConnectorRelease{
		ID:      9001,
		Version: "0.3.0",
		Channel: "stable",
		Status:  model.ReleaseStatusPublished,
	})

	rule, ec := CreateRolloutRule(CreateRolloutRuleReq{
		ReleaseID: release.ID,
		RuleType:  "percentage",
		RuleValue: []byte(`{"percent":50}`),
		Priority:  10,
	})
	if ec != nil {
		t.Fatalf("unexpected error: %v", ec)
	}
	if rule.ID == 0 {
		t.Error("rule ID should be set")
	}
	if rule.RuleType != "percentage" {
		t.Errorf("expected rule_type percentage, got %s", rule.RuleType)
	}
	if rule.Status != model.RolloutRuleActive {
		t.Errorf("expected status active (1), got %d", rule.Status)
	}
}

func TestCreateRolloutRule_InvalidRelease(t *testing.T) {
	_, cleanup := setupUpgradeServiceTest(t)
	defer cleanup()

	_, ec := CreateRolloutRule(CreateRolloutRuleReq{
		ReleaseID: 99999,
		RuleType:  "agent_list",
		RuleValue: []byte(`{"agent_ids":[1]}`),
	})
	if ec == nil {
		t.Fatal("should fail for non-existent release")
	}
}

func TestListRolloutRules(t *testing.T) {
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
		RuleValue: []byte(`{"percent":50}`),
		Priority:  10,
		Status:    model.RolloutRuleActive,
	})
	seedRolloutRule(t, model.ConnectorRolloutRule{
		ID:        8002,
		ReleaseID: release.ID,
		RuleType:  "agent_list",
		RuleValue: []byte(`{"agent_ids":[1,2,3]}`),
		Priority:  5,
		Status:    model.RolloutRulePaused,
	})

	rules, ec := ListRolloutRules(release.ID)
	if ec != nil {
		t.Fatalf("unexpected error: %v", ec)
	}
	if len(rules) != 2 {
		t.Fatalf("expected 2 rules, got %d", len(rules))
	}
	// Should be ordered by priority DESC
	if rules[0].RuleType != "percentage" {
		t.Errorf("expected first rule to be percentage (priority 10), got %s", rules[0].RuleType)
	}
}

func TestUpdateRolloutRuleStatus(t *testing.T) {
	_, cleanup := setupUpgradeServiceTest(t)
	defer cleanup()

	release := seedRelease(t, model.ConnectorRelease{
		ID:      9001,
		Version: "0.3.0",
		Channel: "stable",
		Status:  model.ReleaseStatusPublished,
	})
	rule := seedRolloutRule(t, model.ConnectorRolloutRule{
		ID:        8001,
		ReleaseID: release.ID,
		RuleType:  "percentage",
		RuleValue: []byte(`{"percent":50}`),
		Priority:  10,
		Status:    model.RolloutRuleActive,
	})

	// Pause the rule
	updated, ec := UpdateRolloutRuleStatus(rule.ID, model.RolloutRulePaused)
	if ec != nil {
		t.Fatalf("unexpected error: %v", ec)
	}
	if updated.Status != model.RolloutRulePaused {
		t.Errorf("expected status paused (2), got %d", updated.Status)
	}

	// Reactivate
	updated2, ec := UpdateRolloutRuleStatus(rule.ID, model.RolloutRuleActive)
	if ec != nil {
		t.Fatalf("unexpected error: %v", ec)
	}
	if updated2.Status != model.RolloutRuleActive {
		t.Errorf("expected status active (1), got %d", updated2.Status)
	}
}

func TestDeleteRolloutRule(t *testing.T) {
	_, cleanup := setupUpgradeServiceTest(t)
	defer cleanup()

	release := seedRelease(t, model.ConnectorRelease{
		ID:      9001,
		Version: "0.3.0",
		Channel: "stable",
		Status:  model.ReleaseStatusPublished,
	})
	rule := seedRolloutRule(t, model.ConnectorRolloutRule{
		ID:        8001,
		ReleaseID: release.ID,
		RuleType:  "percentage",
		RuleValue: []byte(`{"percent":50}`),
		Priority:  10,
		Status:    model.RolloutRuleActive,
	})

	ec := DeleteRolloutRule(rule.ID)
	if ec != nil {
		t.Fatalf("unexpected error: %v", ec)
	}

	// Verify rule is gone
	rules, _ := ListRolloutRules(release.ID)
	if len(rules) != 0 {
		t.Error("rule should be deleted")
	}
}

func TestDeleteRolloutRule_NotFound(t *testing.T) {
	_, cleanup := setupUpgradeServiceTest(t)
	defer cleanup()

	ec := DeleteRolloutRule(99999)
	if ec == nil {
		t.Fatal("should fail for non-existent rule")
	}
}

// --- Upgrade stats tests ---

func TestUpgradeStats(t *testing.T) {
	_, cleanup := setupUpgradeServiceTest(t)
	defer cleanup()

	// Seed reports
	for i := 0; i < 3; i++ {
		ReportUpgrade(ReportUpgradeReq{
			AgentID:     int64(i + 1),
			FromVersion: "0.2.0",
			ToVersion:   "0.3.0",
			Status:      model.UpgradeReportSuccess,
		})
	}
	ReportUpgrade(ReportUpgradeReq{
		AgentID:     10,
		FromVersion: "0.2.0",
		ToVersion:   "0.3.0",
		Status:      model.UpgradeReportFailed,
	})
	ReportUpgrade(ReportUpgradeReq{
		AgentID:     11,
		FromVersion: "0.1.0",
		ToVersion:   "0.3.0",
		Status:      model.UpgradeReportSuccess,
	})

	stats, ec := GetUpgradeStats("0.3.0", "")
	if ec != nil {
		t.Fatalf("unexpected error: %v", ec)
	}
	if stats.Total != 5 {
		t.Errorf("expected total 5, got %d", stats.Total)
	}
	if stats.Success != 4 {
		t.Errorf("expected success 4, got %d", stats.Success)
	}
	if stats.Failed != 1 {
		t.Errorf("expected failed 1, got %d", stats.Failed)
	}
}

func TestUpgradeStats_Empty(t *testing.T) {
	_, cleanup := setupUpgradeServiceTest(t)
	defer cleanup()

	stats, ec := GetUpgradeStats("0.99.0", "")
	if ec != nil {
		t.Fatalf("unexpected error: %v", ec)
	}
	if stats.Total != 0 {
		t.Errorf("expected total 0, got %d", stats.Total)
	}
}
