package systemsetting

import (
	"testing"

	"github.com/askie/grix/backend/internal/pkg/testutil"
	"github.com/askie/grix/backend/internal/store"
)

func TestGetVoiceModelsSettingsDefaultWhenUnset(t *testing.T) {
	testDB := testutil.NewTestDB()
	defer testDB.Close()
	store.DB = testDB.DB
	InvalidateVoiceModelsCache()
	defer InvalidateVoiceModelsCache()

	got, err := GetVoiceModelsSettings()
	if err != nil {
		t.Fatalf("GetVoiceModelsSettings() error = %v", err)
	}
	want := DefaultVoiceModelsSettings()
	if len(got.Options) != len(want.Options) {
		t.Fatalf("default options len = %d, want %d", len(got.Options), len(want.Options))
	}
}

func TestSaveAndEnabledVoiceModelOptions(t *testing.T) {
	testDB := testutil.NewTestDB()
	defer testDB.Close()
	store.DB = testDB.DB
	InvalidateVoiceModelsCache()
	defer InvalidateVoiceModelsCache()

	settings := VoiceModelsSettings{Options: []VoiceModelOption{
		{ID: "b", Label: "B", Provider: "doubao_realtime", Model: "m2", Endpoint: "wss://x/2", Enabled: true, Sort: 2},
		{ID: "a", Label: "A", Provider: "openai_realtime", Model: "m1", Endpoint: "wss://x/1", Enabled: true, Sort: 1},
		{ID: "c", Label: "C", Provider: "openai_realtime", Model: "m3", Endpoint: "wss://x/3", Enabled: false, Sort: 0},
	}}
	if err := SaveVoiceModelsSettings(settings, nil); err != nil {
		t.Fatalf("SaveVoiceModelsSettings() error = %v", err)
	}

	InvalidateVoiceModelsCache()
	enabled, err := EnabledVoiceModelOptions()
	if err != nil {
		t.Fatalf("EnabledVoiceModelOptions() error = %v", err)
	}
	// 只保留启用项（剔除 c），并按 Sort 升序（a 在 b 前）。
	if len(enabled) != 2 {
		t.Fatalf("enabled len = %d, want 2", len(enabled))
	}
	if enabled[0].ID != "a" || enabled[1].ID != "b" {
		t.Fatalf("enabled order = %s,%s, want a,b", enabled[0].ID, enabled[1].ID)
	}
}

func TestIsSupportedVoiceProvider(t *testing.T) {
	if !IsSupportedVoiceProvider("openai_realtime") || !IsSupportedVoiceProvider("doubao_realtime") {
		t.Fatal("known providers must be supported")
	}
	if IsSupportedVoiceProvider("unknown_realtime") {
		t.Fatal("unknown provider must not be supported")
	}
}
