package service

import (
	"testing"
	"time"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/store"
	"gorm.io/datatypes"
)

func seedFirstOriginSite(t *testing.T, siteID int64, firstOrigin string, allowedOrigins string) {
	t.Helper()
	seedWidgetSite(t, model.WidgetSite{
		ID:             siteID,
		OwnerUserID:    940,
		SiteKey:        "wk_update_first_origin",
		SiteSecretHash: "hash",
		SiteName:       "Update Shop",
		AllowedOrigins: datatypes.JSON([]byte(allowedOrigins)),
		FirstOrigin:    firstOrigin,
		Status:         model.WidgetSiteStatusActive,
		CreatedAt:      time.Now().UTC(),
		UpdatedAt:      time.Now().UTC(),
	})
}

func loadFirstOrigin(t *testing.T, siteID int64) string {
	t.Helper()
	var site model.WidgetSite
	if err := store.DB.Where("id = ?", siteID).First(&site).Error; err != nil {
		t.Fatalf("load widget site error: %v", err)
	}
	return site.FirstOrigin
}

// 站长把老域名从 allowed_origins 删掉后，first_origin 必须清空；清空后第一个
// 从新域名接入的访客会按原有逻辑重新写入新域名。
func TestWidgetSiteUpdateResetsFirstOriginWhenHostRemoved(t *testing.T) {
	tdb := setupWidgetVisitorServiceTest(t)
	defer tdb.Close()

	seedFirstOriginSite(t, 9401, "old.example.com", `["https://old.example.com","https://new.example.com"]`)

	if _, err := WidgetSiteUpdate(WidgetSiteUpdateInput{
		OwnerUserID:    940,
		SiteID:         9401,
		SiteName:       "Update Shop",
		AllowedOrigins: []string{"https://new.example.com"},
		Status:         model.WidgetSiteStatusActive,
	}); err != nil {
		t.Fatalf("WidgetSiteUpdate() error = %v", err)
	}
	if got := loadFirstOrigin(t, 9401); got != "" {
		t.Fatalf("first_origin = %q after removing the recorded host, want empty", got)
	}

	if _, err := WidgetVisitorInit(WidgetVisitorInitInput{
		SiteKey:      "wk_update_first_origin",
		VisitorKey:   "vk_update_first_origin",
		VisitorName:  "Alice",
		VisitorEmail: "alice@example.com",
		PageURL:      "https://new.example.com/p/1",
		Origin:       "https://new.example.com",
		WSURL:        "wss://api.example.com/v1/widget/ws",
	}); err != nil {
		t.Fatalf("WidgetVisitorInit() error = %v", err)
	}
	if got := loadFirstOrigin(t, 9401); got != "new.example.com" {
		t.Fatalf("first_origin = %q after the first visitor on the new domain, want %q", got, "new.example.com")
	}
}

// 只调换顺序、新增域名、改站点名、改状态，以及旧 host 仍被允许（含端口/协议
// 差异、通配符条目不参与匹配）时，first_origin 都不能变。
func TestWidgetSiteUpdateKeepsFirstOriginWhenHostStillAllowed(t *testing.T) {
	cases := []struct {
		name            string
		firstOrigin     string
		origins         []string
		wantFirstOrigin string
	}{
		{
			name:            "reorder only",
			firstOrigin:     "shop.example.com",
			origins:         []string{"https://www.example.com", "https://shop.example.com"},
			wantFirstOrigin: "shop.example.com",
		},
		{
			name:            "add origin",
			firstOrigin:     "shop.example.com",
			origins:         []string{"https://shop.example.com", "https://new.example.com"},
			wantFirstOrigin: "shop.example.com",
		},
		{
			name:            "rename site",
			firstOrigin:     "shop.example.com",
			origins:         []string{"https://shop.example.com"},
			wantFirstOrigin: "shop.example.com",
		},
		{
			name:            "same host other scheme",
			firstOrigin:     "shop.example.com",
			origins:         []string{"http://shop.example.com"},
			wantFirstOrigin: "shop.example.com",
		},
		{
			name:            "same host explicit port",
			firstOrigin:     "shop.example.com",
			origins:         []string{"https://shop.example.com:8443"},
			wantFirstOrigin: "shop.example.com",
		},
		{
			name:            "wildcard never matches on the visitor path",
			firstOrigin:     "shop.example.com",
			origins:         []string{"https://*.example.com"},
			wantFirstOrigin: "",
		},
		{
			name:            "wildcard plus exact host",
			firstOrigin:     "shop.example.com",
			origins:         []string{"https://*.example.com", "https://shop.example.com"},
			wantFirstOrigin: "shop.example.com",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tdb := setupWidgetVisitorServiceTest(t)
			defer tdb.Close()

			seedFirstOriginSite(t, 9402, tc.firstOrigin, `["https://shop.example.com","https://old.example.com"]`)

			if _, err := WidgetSiteUpdate(WidgetSiteUpdateInput{
				OwnerUserID:    940,
				SiteID:         9402,
				SiteName:       "Renamed Shop",
				AllowedOrigins: tc.origins,
				Status:         model.WidgetSiteStatusActive,
			}); err != nil {
				t.Fatalf("WidgetSiteUpdate() error = %v", err)
			}
			if got := loadFirstOrigin(t, 9402); got != tc.wantFirstOrigin {
				t.Fatalf("first_origin = %q, want %q", got, tc.wantFirstOrigin)
			}
		})
	}
}

// 只改站点名或状态时 first_origin 也不能变。
func TestWidgetSiteUpdateNameOrStatusKeepsFirstOrigin(t *testing.T) {
	tdb := setupWidgetVisitorServiceTest(t)
	defer tdb.Close()

	seedFirstOriginSite(t, 9403, "shop.example.com", `["https://shop.example.com"]`)

	if _, err := WidgetSiteUpdate(WidgetSiteUpdateInput{
		OwnerUserID:    940,
		SiteID:         9403,
		SiteName:       "Renamed Shop",
		AllowedOrigins: []string{"https://shop.example.com"},
		Status:         model.WidgetSiteStatusActive,
	}); err != nil {
		t.Fatalf("WidgetSiteUpdate(rename) error = %v", err)
	}
	if got := loadFirstOrigin(t, 9403); got != "shop.example.com" {
		t.Fatalf("first_origin = %q after rename, want %q", got, "shop.example.com")
	}

	if _, err := WidgetSiteUpdate(WidgetSiteUpdateInput{
		OwnerUserID:    940,
		SiteID:         9403,
		SiteName:       "Renamed Shop",
		AllowedOrigins: []string{"https://shop.example.com"},
		Status:         model.WidgetSiteStatusDisabled,
	}); err != nil {
		t.Fatalf("WidgetSiteUpdate(disable) error = %v", err)
	}
	if got := loadFirstOrigin(t, 9403); got != "shop.example.com" {
		t.Fatalf("first_origin = %q after status change, want %q", got, "shop.example.com")
	}
}

// 空 first_origin 的站点更新配置时不应写入任何值。
func TestWidgetSiteUpdateDoesNotWriteFirstOriginWhenEmpty(t *testing.T) {
	tdb := setupWidgetSiteServiceTest(t)
	defer tdb.Close()

	seedFirstOriginSite(t, 9404, "", `["https://shop.example.com"]`)

	if _, err := WidgetSiteUpdate(WidgetSiteUpdateInput{
		OwnerUserID:    940,
		SiteID:         9404,
		SiteName:       "Update Shop",
		AllowedOrigins: []string{"https://other.example.com"},
		Status:         model.WidgetSiteStatusActive,
	}); err != nil {
		t.Fatalf("WidgetSiteUpdate() error = %v", err)
	}
	if got := loadFirstOrigin(t, 9404); got != "" {
		t.Fatalf("first_origin = %q, want empty", got)
	}
}

// App 通道写入的 "app:<app_id>" 在站点更新时：
// - 若 app_id 仍在 allowed_apps 中，保留 first_origin；
// - 若 app_id 已从 allowed_apps 移除，清空 first_origin。
func TestWidgetSiteUpdateHandlesAppFirstOrigin(t *testing.T) {
	tdb := setupWidgetVisitorServiceTest(t)
	defer tdb.Close()

	seedWidgetSite(t, model.WidgetSite{
		ID:             9405,
		OwnerUserID:    940,
		SiteKey:        "wk_update_app_first_origin",
		SiteSecretHash: "hash",
		SiteName:       "App Shop",
		AllowedOrigins: datatypes.JSON([]byte(`["https://shop.example.com"]`)),
		AllowedApps:    datatypes.JSON([]byte(`["com.example.old","com.example.new"]`)),
		FirstOrigin:    "app:com.example.old",
		Status:         model.WidgetSiteStatusActive,
		CreatedAt:      time.Now().UTC(),
		UpdatedAt:      time.Now().UTC(),
	})

	// 仍在 allowed_apps 中：保留
	if _, err := WidgetSiteUpdate(WidgetSiteUpdateInput{
		OwnerUserID:    940,
		SiteID:         9405,
		SiteName:       "App Shop Renamed",
		AllowedOrigins: []string{"https://shop.example.com"},
		AllowedApps:    []string{"com.example.new", "com.Example.Old"},
		Status:         model.WidgetSiteStatusActive,
	}); err != nil {
		t.Fatalf("WidgetSiteUpdate(keep app) error = %v", err)
	}
	if got := loadFirstOrigin(t, 9405); got != "app:com.example.old" {
		t.Fatalf("first_origin = %q when app still allowed, want %q", got, "app:com.example.old")
	}

	// 从 allowed_apps 移除后：清空
	if _, err := WidgetSiteUpdate(WidgetSiteUpdateInput{
		OwnerUserID:    940,
		SiteID:         9405,
		SiteName:       "App Shop Renamed",
		AllowedOrigins: []string{"https://shop.example.com"},
		AllowedApps:    []string{"com.example.new"},
		Status:         model.WidgetSiteStatusActive,
	}); err != nil {
		t.Fatalf("WidgetSiteUpdate(remove app) error = %v", err)
	}
	if got := loadFirstOrigin(t, 9405); got != "" {
		t.Fatalf("first_origin = %q after removing app_id, want empty", got)
	}

	// 随后新 App 访客接入，写入新的 "app:<app_id>"
	if _, err := WidgetVisitorInit(WidgetVisitorInitInput{
		SiteKey:    "wk_update_app_first_origin",
		VisitorKey: "vk_new_app",
		Client: &WidgetVisitorClientInfo{
			Platform:   "ios",
			AppID:      "com.example.new",
			SDKVersion: "1.0.0",
		},
		WSURL: "wss://api.example.com/v1/widget/ws",
	}); err != nil {
		t.Fatalf("WidgetVisitorInit(new app) error = %v", err)
	}
	if got := loadFirstOrigin(t, 9405); got != "app:com.example.new" {
		t.Fatalf("first_origin = %q after new app visitor, want %q", got, "app:com.example.new")
	}
}
