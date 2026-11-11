package service

import (
	"errors"
	"testing"
	"time"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/snowflake"
	"github.com/askie/grix/backend/internal/pkg/testutil"
	"github.com/askie/grix/backend/internal/store"
	"gorm.io/datatypes"
)

func setupWidgetSiteServiceTest(t *testing.T) *testutil.TestDB {
	t.Helper()
	tdb := testutil.NewTestDB()
	store.DB = tdb.DB
	if err := snowflake.Init(1); err != nil {
		t.Fatalf("snowflake init error: %v", err)
	}
	return tdb
}

func TestWidgetSiteCreateAndDetail(t *testing.T) {
	tdb := setupWidgetSiteServiceTest(t)
	defer tdb.Close()

	created, err := WidgetSiteCreate(WidgetSiteCreateInput{
		OwnerUserID:    9001,
		SiteName:       "Shop",
		AllowedOrigins: []string{"https://shop.example.com", "https://SHOP.example.com"},
	})
	if err != nil {
		t.Fatalf("WidgetSiteCreate() error = %v", err)
	}
	if created.Site.ID <= 0 || created.Site.SiteKey == "" || created.SiteSecret == "" {
		t.Fatalf("unexpected created payload: %+v", created)
	}
	if len(created.Site.AllowedOrigins) != 1 || created.Site.AllowedOrigins[0] != "https://shop.example.com" {
		t.Fatalf("unexpected allowed origins: %+v", created.Site.AllowedOrigins)
	}

	detail, err := WidgetSiteDetail(9001, created.Site.ID)
	if err != nil {
		t.Fatalf("WidgetSiteDetail() error = %v", err)
	}
	if detail.SiteKey != created.Site.SiteKey || detail.SiteName != "Shop" {
		t.Fatalf("unexpected detail: %+v", detail)
	}
}

func TestWidgetSiteUpdateOwnership(t *testing.T) {
	tdb := setupWidgetSiteServiceTest(t)
	defer tdb.Close()

	now := time.Now().UTC()
	site := model.WidgetSite{
		ID:             93001,
		OwnerUserID:    100,
		SiteKey:        "wk_test",
		SiteSecretHash: "hash",
		SiteName:       "Before",
		AllowedOrigins: datatypes.JSON([]byte(`["https://before.example.com"]`)),
		Status:         model.WidgetSiteStatusActive,
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	if err := store.DB.Create(&site).Error; err != nil {
		t.Fatalf("seed site error: %v", err)
	}

	_, err := WidgetSiteUpdate(WidgetSiteUpdateInput{
		OwnerUserID:    101,
		SiteID:         93001,
		SiteName:       "After",
		AllowedOrigins: []string{"https://after.example.com"},
		Status:         model.WidgetSiteStatusDisabled,
	})
	if !errors.Is(err, ErrWidgetSiteNotOwned) {
		t.Fatalf("expected ErrWidgetSiteNotOwned, got %v", err)
	}

	updated, err := WidgetSiteUpdate(WidgetSiteUpdateInput{
		OwnerUserID:    100,
		SiteID:         93001,
		SiteName:       "After",
		AllowedOrigins: []string{"https://after.example.com"},
		Status:         model.WidgetSiteStatusDisabled,
	})
	if err != nil {
		t.Fatalf("WidgetSiteUpdate() error = %v", err)
	}
	if updated.Status != model.WidgetSiteStatusDisabled || updated.SiteName != "After" {
		t.Fatalf("unexpected updated payload: %+v", updated)
	}
}

func TestWidgetSiteListAndRotateSecret(t *testing.T) {
	tdb := setupWidgetSiteServiceTest(t)
	defer tdb.Close()

	first, err := WidgetSiteCreate(WidgetSiteCreateInput{
		OwnerUserID:    8800,
		SiteName:       "S1",
		AllowedOrigins: []string{"https://s1.example.com"},
	})
	if err != nil {
		t.Fatalf("create first site error: %v", err)
	}
	_, err = WidgetSiteCreate(WidgetSiteCreateInput{
		OwnerUserID:    8800,
		SiteName:       "S2",
		AllowedOrigins: []string{"https://s2.example.com"},
	})
	if err != nil {
		t.Fatalf("create second site error: %v", err)
	}

	list, err := WidgetSiteList(WidgetSiteListInput{OwnerUserID: 8800, Limit: 10, Offset: 0})
	if err != nil {
		t.Fatalf("WidgetSiteList() error = %v", err)
	}
	if list.Total != 2 || len(list.Items) != 2 {
		t.Fatalf("unexpected list payload: %+v", list)
	}

	rotated, err := WidgetSiteRotateSecret(8800, first.Site.ID)
	if err != nil {
		t.Fatalf("WidgetSiteRotateSecret() error = %v", err)
	}
	if rotated.SiteID != first.Site.ID || rotated.SiteSecret == "" {
		t.Fatalf("unexpected rotate payload: %+v", rotated)
	}

	var dbSite model.WidgetSite
	if err := store.DB.Where("id = ?", first.Site.ID).First(&dbSite).Error; err != nil {
		t.Fatalf("reload site error: %v", err)
	}
	if dbSite.SiteSecretHash != hashWidgetSiteSecret(rotated.SiteSecret) {
		t.Fatalf("site secret hash not updated")
	}
}

func TestWidgetSiteAllowedAppsCreateUpdateListAndValidation(t *testing.T) {
	tdb := setupWidgetSiteServiceTest(t)
	defer tdb.Close()

	created, err := WidgetSiteCreate(WidgetSiteCreateInput{
		OwnerUserID:    9100,
		SiteName:       "Mobile App Site",
		AllowedOrigins: []string{},
		AllowedApps:    []string{"com.example.shop", "COM.Example.Shop", "io.grix.app_1.debug-build"},
	})
	if err != nil {
		t.Fatalf("WidgetSiteCreate() error = %v", err)
	}
	if len(created.Site.AllowedApps) != 2 || created.Site.AllowedApps[0] != "com.example.shop" || created.Site.AllowedApps[1] != "io.grix.app_1.debug-build" {
		t.Fatalf("unexpected allowed_apps on create: %+v", created.Site.AllowedApps)
	}
	if len(created.Site.AllowedOrigins) != 0 {
		t.Fatalf("expected empty allowed_origins, got %+v", created.Site.AllowedOrigins)
	}

	detail, err := WidgetSiteDetail(9100, created.Site.ID)
	if err != nil {
		t.Fatalf("WidgetSiteDetail() error = %v", err)
	}
	if len(detail.AllowedApps) != 2 || detail.AllowedApps[0] != "com.example.shop" {
		t.Fatalf("unexpected allowed_apps on detail: %+v", detail.AllowedApps)
	}

	updated, err := WidgetSiteUpdate(WidgetSiteUpdateInput{
		OwnerUserID:    9100,
		SiteID:         created.Site.ID,
		SiteName:       "Mobile App Site v2",
		AllowedOrigins: []string{"https://shop.example.com"},
		AllowedApps:    []string{"com.example.v2"},
		Status:         model.WidgetSiteStatusActive,
	})
	if err != nil {
		t.Fatalf("WidgetSiteUpdate() error = %v", err)
	}
	if len(updated.AllowedApps) != 1 || updated.AllowedApps[0] != "com.example.v2" {
		t.Fatalf("unexpected allowed_apps on update: %+v", updated.AllowedApps)
	}

	list, err := WidgetSiteList(WidgetSiteListInput{OwnerUserID: 9100, Limit: 10, Offset: 0})
	if err != nil {
		t.Fatalf("WidgetSiteList() error = %v", err)
	}
	if len(list.Items) != 1 || len(list.Items[0].AllowedApps) != 1 || list.Items[0].AllowedApps[0] != "com.example.v2" {
		t.Fatalf("unexpected allowed_apps on list: %+v", list.Items)
	}

	invalidCases := []string{
		"singleword",
		"https://com.example.app",
		".com.example",
		"com.example.",
		"com..example",
		"1com.example.app",
		"com.example.app/path",
		"com.example.*",
	}
	for _, invalidApp := range invalidCases {
		_, err := WidgetSiteCreate(WidgetSiteCreateInput{
			OwnerUserID: 9100,
			SiteName:    "Invalid App",
			AllowedApps: []string{invalidApp},
		})
		if err == nil {
			t.Fatalf("expected error for invalid app_id %q, got nil", invalidApp)
		}
	}

	// 既无 allowed_origins 也无 allowed_apps 时应报错
	if _, err := WidgetSiteCreate(WidgetSiteCreateInput{
		OwnerUserID: 9100,
		SiteName:    "Empty Both",
	}); err == nil {
		t.Fatal("expected error when both allowed_origins and allowed_apps are empty")
	}
}

// 老版本客户端更新站点时不会传 allowed_apps（nil）：必须保留数据库中的原值；
// 只有显式传入空切片 []string{} 时才清空。同理 allowed_origins 为 nil 时也保留原值，
// 且仅配了 App 的站点被老客户端编辑后不会报错、也不会丢失 allowed_apps。
func TestWidgetSiteUpdatePreservesOmittedAllowlists(t *testing.T) {
	tdb := setupWidgetSiteServiceTest(t)
	defer tdb.Close()

	// 1. 同时有域名和 App 的站点：不传 AllowedApps（nil）时保留原值
	created, err := WidgetSiteCreate(WidgetSiteCreateInput{
		OwnerUserID:    9200,
		SiteName:       "Hybrid Site",
		AllowedOrigins: []string{"https://shop.example.com"},
		AllowedApps:    []string{"com.example.ios", "com.example.android"},
	})
	if err != nil {
		t.Fatalf("WidgetSiteCreate() error = %v", err)
	}

	updatedLegacy, err := WidgetSiteUpdate(WidgetSiteUpdateInput{
		OwnerUserID:    9200,
		SiteID:         created.Site.ID,
		SiteName:       "Hybrid Site Edited By Old Client",
		AllowedOrigins: []string{"https://shop.example.com", "https://m.example.com"},
		AllowedApps:    nil, // 模拟老客户端未带 allowed_apps 字段
		Status:         model.WidgetSiteStatusActive,
	})
	if err != nil {
		t.Fatalf("WidgetSiteUpdate(nil AllowedApps) error = %v", err)
	}
	if len(updatedLegacy.AllowedApps) != 2 || updatedLegacy.AllowedApps[0] != "com.example.android" || updatedLegacy.AllowedApps[1] != "com.example.ios" {
		t.Fatalf("expected allowed_apps preserved when nil, got %+v", updatedLegacy.AllowedApps)
	}
	if len(updatedLegacy.AllowedOrigins) != 2 {
		t.Fatalf("unexpected allowed_origins: %+v", updatedLegacy.AllowedOrigins)
	}

	// 不传 AllowedOrigins（nil）时也保留原值
	updatedAppsOnlyField, err := WidgetSiteUpdate(WidgetSiteUpdateInput{
		OwnerUserID:    9200,
		SiteID:         created.Site.ID,
		SiteName:       "Hybrid Site Edited Apps Only",
		AllowedOrigins: nil,
		AllowedApps:    []string{"com.example.ios"},
		Status:         model.WidgetSiteStatusActive,
	})
	if err != nil {
		t.Fatalf("WidgetSiteUpdate(nil AllowedOrigins) error = %v", err)
	}
	if len(updatedAppsOnlyField.AllowedOrigins) != 2 {
		t.Fatalf("expected allowed_origins preserved when nil, got %+v", updatedAppsOnlyField.AllowedOrigins)
	}
	if len(updatedAppsOnlyField.AllowedApps) != 1 || updatedAppsOnlyField.AllowedApps[0] != "com.example.ios" {
		t.Fatalf("unexpected allowed_apps: %+v", updatedAppsOnlyField.AllowedApps)
	}

	// 显式传空切片 []string{} 时才清空 allowed_apps
	clearedApps, err := WidgetSiteUpdate(WidgetSiteUpdateInput{
		OwnerUserID:    9200,
		SiteID:         created.Site.ID,
		SiteName:       "Hybrid Site Cleared Apps",
		AllowedOrigins: nil,
		AllowedApps:    []string{},
		Status:         model.WidgetSiteStatusActive,
	})
	if err != nil {
		t.Fatalf("WidgetSiteUpdate(empty slice AllowedApps) error = %v", err)
	}
	if len(clearedApps.AllowedApps) != 0 {
		t.Fatalf("expected allowed_apps cleared when explicit empty slice, got %+v", clearedApps.AllowedApps)
	}

	// 2. 只配了 App 的站点（allowed_origins 为 []）：被老客户端编辑（传 allowed_origins: [] 且不带 allowed_apps）时，
	// 不会报错，也不会把 allowed_apps 改坏。
	appOnlySite, err := WidgetSiteCreate(WidgetSiteCreateInput{
		OwnerUserID:    9200,
		SiteName:       "App Only Site",
		AllowedOrigins: []string{},
		AllowedApps:    []string{"com.example.onlyapp"},
	})
	if err != nil {
		t.Fatalf("WidgetSiteCreate(app only) error = %v", err)
	}

	updatedAppOnlyByLegacy, err := WidgetSiteUpdate(WidgetSiteUpdateInput{
		OwnerUserID:    9200,
		SiteID:         appOnlySite.Site.ID,
		SiteName:       "App Only Toggled By Old Client",
		AllowedOrigins: []string{}, // 老客户端原样回传空 origins
		AllowedApps:    nil,        // 老客户端不带 allowed_apps
		Status:         model.WidgetSiteStatusDisabled,
	})
	if err != nil {
		t.Fatalf("WidgetSiteUpdate(app-only site by legacy client) error = %v", err)
	}
	if len(updatedAppOnlyByLegacy.AllowedApps) != 1 || updatedAppOnlyByLegacy.AllowedApps[0] != "com.example.onlyapp" {
		t.Fatalf("app-only site allowed_apps corrupted by legacy update: %+v", updatedAppOnlyByLegacy.AllowedApps)
	}
}
