package systemsetting

import (
	"reflect"
	"testing"

	"github.com/askie/grix/backend/internal/pkg/testutil"
	"github.com/askie/grix/backend/internal/store"
)

func TestSaveContentModerationSettingsNormalizesValues(t *testing.T) {
	testDB := testutil.NewTestDB()
	store.DB = testDB.DB
	defer testDB.Close()

	InvalidateContentModerationSettingsCache()
	defer InvalidateContentModerationSettingsCache()

	err := SaveContentModerationSettings(ContentModerationSettings{
		Enabled:            true,
		Keywords:           []string{" Forbidden ", "敏感词", "forbidden", "  "},
		HumanMuteThreshold: 0,
	}, nil)
	if err != nil {
		t.Fatalf("SaveContentModerationSettings() error = %v", err)
	}

	got, err := GetContentModerationSettings()
	if err != nil {
		t.Fatalf("GetContentModerationSettings() error = %v", err)
	}

	if !got.Enabled {
		t.Fatal("expected moderation to stay enabled")
	}
	if got.HumanMuteThreshold != 3 {
		t.Fatalf("HumanMuteThreshold = %d, want 3", got.HumanMuteThreshold)
	}
	wantKeywords := []string{"forbidden", "敏感词"}
	if !reflect.DeepEqual(got.Keywords, wantKeywords) {
		t.Fatalf("Keywords = %#v, want %#v", got.Keywords, wantKeywords)
	}
}
