package systemsetting

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/testutil"
	"github.com/askie/grix/backend/internal/store"
	"gorm.io/datatypes"
)

func TestGetAuthSettingsUsesCacheWithinOneMinute(t *testing.T) {
	testDB := testutil.NewTestDB()
	defer testDB.Close()
	store.DB = testDB.DB

	InvalidateAuthSettingsCache()
	defer InvalidateAuthSettingsCache()

	current := time.Date(2026, 3, 12, 14, 50, 0, 0, time.UTC)
	originalNow := authSettingsNow
	authSettingsNow = func() time.Time { return current }
	defer func() {
		authSettingsNow = originalNow
	}()

	first := DefaultAuthSettings()
	first.AutoAddCustomerUserID = 1001
	mustUpsertAuthSettingRow(t, first)

	got, err := GetAuthSettings()
	if err != nil {
		t.Fatalf("GetAuthSettings() first read error = %v", err)
	}
	if !reflect.DeepEqual(got, first) {
		t.Fatalf("expected first settings %+v, got %+v", first, got)
	}

	second := first
	second.AutoAddCustomerUserID = 2002
	mustUpsertAuthSettingRow(t, second)

	got, err = GetAuthSettings()
	if err != nil {
		t.Fatalf("GetAuthSettings() cached read error = %v", err)
	}
	if !reflect.DeepEqual(got, first) {
		t.Fatalf("expected cached settings %+v, got %+v", first, got)
	}

	current = current.Add(authSettingsCacheTTL + time.Second)
	got, err = GetAuthSettings()
	if err != nil {
		t.Fatalf("GetAuthSettings() expired read error = %v", err)
	}
	if !reflect.DeepEqual(got, second) {
		t.Fatalf("expected refreshed settings %+v, got %+v", second, got)
	}
}

func TestInvalidateAuthSettingsCacheForcesDatabaseRefresh(t *testing.T) {
	testDB := testutil.NewTestDB()
	defer testDB.Close()
	store.DB = testDB.DB

	InvalidateAuthSettingsCache()
	defer InvalidateAuthSettingsCache()

	current := time.Date(2026, 3, 12, 15, 0, 0, 0, time.UTC)
	originalNow := authSettingsNow
	authSettingsNow = func() time.Time { return current }
	defer func() {
		authSettingsNow = originalNow
	}()

	first := DefaultAuthSettings()
	first.AutoAddCustomerUserID = 1001
	mustUpsertAuthSettingRow(t, first)

	if _, err := GetAuthSettings(); err != nil {
		t.Fatalf("GetAuthSettings() first read error = %v", err)
	}

	second := first
	second.AutoAddCustomerUserID = 2002
	mustUpsertAuthSettingRow(t, second)

	InvalidateAuthSettingsCache()
	got, err := GetAuthSettings()
	if err != nil {
		t.Fatalf("GetAuthSettings() refreshed read error = %v", err)
	}
	if !reflect.DeepEqual(got, second) {
		t.Fatalf("expected refreshed settings %+v, got %+v", second, got)
	}
}

func TestGetAuthSettingsWithContextHonorsCanceledContext(t *testing.T) {
	testDB := testutil.NewTestDB()
	defer testDB.Close()
	store.DB = testDB.DB

	InvalidateAuthSettingsCache()
	defer InvalidateAuthSettingsCache()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := GetAuthSettingsWithContext(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("GetAuthSettingsWithContext error = %v, want context canceled", err)
	}
}

func mustUpsertAuthSettingRow(t *testing.T, settings AuthSettings) {
	t.Helper()

	raw, err := json.Marshal(settings)
	if err != nil {
		t.Fatalf("marshal settings error: %v", err)
	}
	row := model.SystemSetting{
		Key:   authSettingKey,
		Value: datatypes.JSON(raw),
	}
	if err := store.DB.Where("key = ?", authSettingKey).Assign(row).FirstOrCreate(&row).Error; err != nil {
		t.Fatalf("upsert auth setting row error: %v", err)
	}
}
