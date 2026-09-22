package systemsetting

import (
	"reflect"
	"testing"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/testutil"
	"github.com/askie/grix/backend/internal/store"
)

func TestAgentClientTypesSettings_MissingMeansAllEnabled(t *testing.T) {
	testDB := testutil.NewTestDB()
	store.DB = testDB.DB
	defer testDB.Close()

	InvalidateAgentClientTypesSettingsCache()
	defer InvalidateAgentClientTypesSettingsCache()

	settings, err := GetAgentClientTypesSettings()
	if err != nil {
		t.Fatalf("GetAgentClientTypesSettings() error = %v", err)
	}
	if len(settings.Enabled) != 0 {
		t.Fatalf("Enabled = %#v, want empty (all enabled)", settings.Enabled)
	}
	ok, err := IsAgentClientTypeEnabled(model.AgentClientTypeClaude)
	if err != nil {
		t.Fatalf("IsAgentClientTypeEnabled() error = %v", err)
	}
	if !ok {
		t.Fatal("expected claude enabled when unset")
	}
}

func TestAgentClientTypesSettings_FiltersEnabled(t *testing.T) {
	testDB := testutil.NewTestDB()
	store.DB = testDB.DB
	defer testDB.Close()

	InvalidateAgentClientTypesSettingsCache()
	defer InvalidateAgentClientTypesSettingsCache()

	if err := SaveAgentClientTypesSettings(AgentClientTypesSettings{
		Enabled: []string{" Hermes ", "claude", "claude", "not-a-type"},
	}, nil); err != nil {
		t.Fatalf("SaveAgentClientTypesSettings() error = %v", err)
	}

	got, err := GetAgentClientTypesSettings()
	if err != nil {
		t.Fatalf("GetAgentClientTypesSettings() error = %v", err)
	}
	want := []string{model.AgentClientTypeClaude, model.AgentClientTypeHermes}
	if !reflect.DeepEqual(got.Enabled, want) {
		t.Fatalf("Enabled = %#v, want %#v", got.Enabled, want)
	}

	ok, err := IsAgentClientTypeEnabled(model.AgentClientTypeCodex)
	if err != nil {
		t.Fatalf("IsAgentClientTypeEnabled(codex) error = %v", err)
	}
	if ok {
		t.Fatal("expected codex disabled")
	}
	ok, err = IsAgentClientTypeEnabled(model.AgentClientTypeHermes)
	if err != nil {
		t.Fatalf("IsAgentClientTypeEnabled(hermes) error = %v", err)
	}
	if !ok {
		t.Fatal("expected hermes enabled")
	}
}
