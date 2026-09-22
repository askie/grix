package service

import (
	"testing"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/systemsetting"
)

func TestUpdateAgentClientTypesSettingsRejectsEmptyAndInvalid(t *testing.T) {
	testDB, cleanup := setupAdminServiceTest(t)
	defer cleanup()
	systemsetting.InvalidateAgentClientTypesSettingsCache()
	defer systemsetting.InvalidateAgentClientTypesSettingsCache()

	admin := createAdminFixture(t, testDB, 4701, "act-admin", "ACT", "RootPassword123A", model.AdminStatusActive)

	if err := UpdateAgentClientTypesSettings(admin.ID, nil, "127.0.0.1", "test"); err == nil {
		t.Fatal("expected empty enabled list to fail")
	}
	if err := UpdateAgentClientTypesSettings(admin.ID, []string{"not-a-real-type"}, "127.0.0.1", "test"); err == nil {
		t.Fatal("expected invalid type to fail")
	}
	if err := UpdateAgentClientTypesSettings(admin.ID, []string{"claude", "bogus"}, "127.0.0.1", "test"); err == nil {
		t.Fatal("expected mixed invalid type to fail")
	}
}

func TestUpdateAgentClientTypesSettingsPersists(t *testing.T) {
	testDB, cleanup := setupAdminServiceTest(t)
	defer cleanup()
	systemsetting.InvalidateAgentClientTypesSettingsCache()
	defer systemsetting.InvalidateAgentClientTypesSettingsCache()

	admin := createAdminFixture(t, testDB, 4702, "act-admin-2", "ACT2", "RootPassword123A", model.AdminStatusActive)

	if err := UpdateAgentClientTypesSettings(admin.ID, []string{"hermes", "claude"}, "127.0.0.1", "test"); err != nil {
		t.Fatalf("UpdateAgentClientTypesSettings() error = %v", err)
	}

	view, err := GetAgentClientTypesAdminView()
	if err != nil {
		t.Fatalf("GetAgentClientTypesAdminView() error = %v", err)
	}
	if view.AllEnabled {
		t.Fatal("expected all_enabled=false after explicit save")
	}
	enabled := map[string]bool{}
	for _, item := range view.Items {
		enabled[item.Type] = item.Enabled
		if item.Label == "" {
			t.Fatalf("missing label for %s", item.Type)
		}
	}
	if !enabled[model.AgentClientTypeClaude] || !enabled[model.AgentClientTypeHermes] {
		t.Fatalf("expected hermes+claude enabled, got %#v", enabled)
	}
	if enabled[model.AgentClientTypeCodex] {
		t.Fatal("expected codex disabled")
	}

	var auditCount int64
	if err := testDB.DB.Model(&model.AdminOperationLog{}).
		Where("action = ? AND target_id = ?", "agent_client_types_settings_update", "agent_client_types").
		Count(&auditCount).Error; err != nil {
		t.Fatalf("count audit: %v", err)
	}
	if auditCount != 1 {
		t.Fatalf("audit count = %d, want 1", auditCount)
	}
}
