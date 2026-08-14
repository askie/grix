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
