package systemsetting

import (
	"testing"

	"github.com/askie/grix/backend/internal/pkg/testutil"
	"github.com/askie/grix/backend/internal/store"
)

func TestGetPushChannelSettingsDefaultAllEnabled(t *testing.T) {
	store.DB = testutil.NewTestDB().DB
	InvalidatePushChannelSettingsCache()

	s, err := GetPushChannelSettings()
	if err != nil {
		t.Fatalf("GetPushChannelSettings error: %v", err)
	}
	if !s.IOSAPNsEnabled || !s.AndroidFCMEnabled || !s.WebPushEnabled || !s.JPushEnabled {
		t.Fatalf("expected all channels enabled by default, got %+v", s)
	}
}

func TestSaveAndGetPushChannelSettings(t *testing.T) {
	store.DB = testutil.NewTestDB().DB
	InvalidatePushChannelSettingsCache()

	// 关掉走 Google 的两个通道，保留 iOS / 极光。
	want := PushChannelSettings{
		IOSAPNsEnabled:    true,
		AndroidFCMEnabled: false,
		WebPushEnabled:    false,
		JPushEnabled:      true,
	}
	if err := SavePushChannelSettings(want, nil); err != nil {
		t.Fatalf("SavePushChannelSettings error: %v", err)
	}

	// 失效缓存，强制从 DB 重新读取，验证持久化正确。
	InvalidatePushChannelSettingsCache()
	got, err := GetPushChannelSettings()
	if err != nil {
		t.Fatalf("GetPushChannelSettings error: %v", err)
	}
	if got != want {
		t.Fatalf("roundtrip mismatch: want %+v, got %+v", want, got)
	}
}
