package handler

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/snowflake"
	"github.com/askie/grix/backend/internal/pkg/testutil"
	"github.com/askie/grix/backend/internal/store"
	"github.com/gin-gonic/gin"
	"gorm.io/datatypes"
)

// TestWidgetSiteDisplayConfigRoundtrip verifies display_config survives
// create -> detail through the handler/service/store layers.
func TestWidgetSiteDisplayConfigRoundtrip(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tdb := testutil.NewTestDB()
	defer tdb.Close()
	store.DB = tdb.DB
	store.RDB = nil
	if err := snowflake.Init(7); err != nil {
		t.Fatalf("snowflake init error: %v", err)
	}

	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("user_id", int64(202))
		c.Next()
	})
	r.POST("/v1/widget/sites/create", WidgetSiteCreate)
	r.POST("/v1/widget/sites/update", WidgetSiteUpdate)
	r.GET("/v1/widget/sites/list", WidgetSiteList)
	r.GET("/v1/widget/sites/detail", WidgetSiteDetail)

	createBody := bytes.NewBufferString(`{
		"site_name":"Cfg",
		"allowed_origins":["https://cfg.example.com"],
		"allowed_apps":["com.example.cfg"],
		"display_config":{"theme_color":"#123456","title":"Hi","auto_expand":true,"position":"left"}
	}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/widget/sites/create", createBody)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create status=%d body=%s", rec.Code, rec.Body.String())
	}

	var createResp struct {
		Data struct {
			Site struct {
				ID            string   `json:"id"`
				AllowedApps   []string `json:"allowed_apps"`
				DisplayConfig struct {
					ThemeColor string `json:"theme_color"`
					Title      string `json:"title"`
					AutoExpand bool   `json:"auto_expand"`
					Position   string `json:"position"`
				} `json:"display_config"`
			} `json:"site"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &createResp); err != nil {
		t.Fatalf("unmarshal create resp: %v body=%s", err, rec.Body.String())
	}
	cfg := createResp.Data.Site.DisplayConfig
	if cfg.ThemeColor != "#123456" || cfg.Title != "Hi" || !cfg.AutoExpand || cfg.Position != "left" {
		t.Fatalf("create display_config mismatch: %+v", cfg)
	}
	if len(createResp.Data.Site.AllowedApps) != 1 || createResp.Data.Site.AllowedApps[0] != "com.example.cfg" {
		t.Fatalf("create allowed_apps mismatch: %+v", createResp.Data.Site.AllowedApps)
	}

	// Update allowed_apps and verify via list & detail.
	updateBody := bytes.NewBufferString(`{
		"id":"` + createResp.Data.Site.ID + `",
		"site_name":"Cfg",
		"allowed_origins":["https://cfg.example.com"],
		"allowed_apps":["com.example.cfg","io.grix.sdk"],
		"display_config":{"theme_color":"#123456","title":"Hi","auto_expand":true,"position":"left"},
		"status":1
	}`)
	updateReq := httptest.NewRequest(http.MethodPost, "/v1/widget/sites/update", updateBody)
	updateReq.Header.Set("Content-Type", "application/json")
	updateRec := httptest.NewRecorder()
	r.ServeHTTP(updateRec, updateReq)
	if updateRec.Code != http.StatusOK {
		t.Fatalf("update status=%d body=%s", updateRec.Code, updateRec.Body.String())
	}

	listReq := httptest.NewRequest(http.MethodGet, "/v1/widget/sites/list", nil)
	listRec := httptest.NewRecorder()
	r.ServeHTTP(listRec, listReq)
	if listRec.Code != http.StatusOK {
		t.Fatalf("list status=%d body=%s", listRec.Code, listRec.Body.String())
	}
	var listResp struct {
		Data struct {
			Items []struct {
				ID          string   `json:"id"`
				AllowedApps []string `json:"allowed_apps"`
			} `json:"items"`
		} `json:"data"`
	}
	if err := json.Unmarshal(listRec.Body.Bytes(), &listResp); err != nil {
		t.Fatalf("unmarshal list resp: %v", err)
	}
	if len(listResp.Data.Items) != 1 || len(listResp.Data.Items[0].AllowedApps) != 2 {
		t.Fatalf("list allowed_apps mismatch: %+v", listResp.Data.Items)
	}

	// Legacy client update without allowed_apps JSON field preserves existing allowed_apps.
	legacyUpdateBody := bytes.NewBufferString(`{
		"id":"` + createResp.Data.Site.ID + `",
		"site_name":"Cfg Legacy",
		"allowed_origins":["https://cfg.example.com"],
		"display_config":{"theme_color":"#123456","title":"Hi","auto_expand":true,"position":"left"},
		"status":1
	}`)
	legacyUpdateReq := httptest.NewRequest(http.MethodPost, "/v1/widget/sites/update", legacyUpdateBody)
	legacyUpdateReq.Header.Set("Content-Type", "application/json")
	legacyUpdateRec := httptest.NewRecorder()
	r.ServeHTTP(legacyUpdateRec, legacyUpdateReq)
	if legacyUpdateRec.Code != http.StatusOK {
		t.Fatalf("legacy update status=%d body=%s", legacyUpdateRec.Code, legacyUpdateRec.Body.String())
	}

	// Fetch detail and confirm config & allowed_apps persisted.
	detailReq := httptest.NewRequest(http.MethodGet, "/v1/widget/sites/detail?id="+createResp.Data.Site.ID, nil)
	detailReq.Host = "api.example.com"
	detailRec := httptest.NewRecorder()
	r.ServeHTTP(detailRec, detailReq)
	if detailRec.Code != http.StatusOK {
		t.Fatalf("detail status=%d body=%s", detailRec.Code, detailRec.Body.String())
	}
	var detailResp struct {
		Data struct {
			Site struct {
				AllowedApps   []string `json:"allowed_apps"`
				DisplayConfig struct {
					ThemeColor string `json:"theme_color"`
					AutoExpand bool   `json:"auto_expand"`
				} `json:"display_config"`
			} `json:"site"`
		} `json:"data"`
	}
	if err := json.Unmarshal(detailRec.Body.Bytes(), &detailResp); err != nil {
		t.Fatalf("unmarshal detail resp: %v body=%s", err, detailRec.Body.String())
	}
	if detailResp.Data.Site.DisplayConfig.ThemeColor != "#123456" || !detailResp.Data.Site.DisplayConfig.AutoExpand {
		t.Fatalf("detail display_config mismatch: %+v", detailResp.Data.Site.DisplayConfig)
	}
	if len(detailResp.Data.Site.AllowedApps) != 2 {
		t.Fatalf("detail allowed_apps not preserved after legacy update: %+v", detailResp.Data.Site.AllowedApps)
	}
}

// TestWidgetVisitorConfigEndpoint verifies the public config endpoint returns
// the site's display_config without creating a session, and enforces origin.
func TestWidgetVisitorConfigEndpoint(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tdb := testutil.NewTestDB()
	defer tdb.Close()
	store.DB = tdb.DB
	store.RDB = nil

	now := time.Now().UTC()
	if err := store.DB.Create(&model.WidgetSite{
		ID:             8801,
		OwnerUserID:    4401,
		SiteKey:        "wk_cfg_pub",
		SiteSecretHash: "hash",
		SiteName:       "Pub",
		AllowedOrigins: datatypes.JSON([]byte(`["https://pub.example.com"]`)),
		DisplayConfig:  datatypes.JSON([]byte(`{"theme_color":"#abcdef","auto_expand":true}`)),
		Status:         model.WidgetSiteStatusActive,
		CreatedAt:      now,
		UpdatedAt:      now,
	}).Error; err != nil {
		t.Fatalf("seed widget site error: %v", err)
	}

	r := gin.New()
	r.GET("/v1/widget/config", WidgetVisitorConfig)

	// Allowed origin -> 200 with config.
	req := httptest.NewRequest(http.MethodGet, "/v1/widget/config?site_key=wk_cfg_pub", nil)
	req.Header.Set("Origin", "https://pub.example.com")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("config status=%d body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Data struct {
			DisplayConfig struct {
				ThemeColor string `json:"theme_color"`
				AutoExpand bool   `json:"auto_expand"`
			} `json:"display_config"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal config resp: %v body=%s", err, rec.Body.String())
	}
	if resp.Data.DisplayConfig.ThemeColor != "#abcdef" || !resp.Data.DisplayConfig.AutoExpand {
		t.Fatalf("config mismatch: %+v", resp.Data.DisplayConfig)
	}

	// Blocked origin -> 403.
	blockedReq := httptest.NewRequest(http.MethodGet, "/v1/widget/config?site_key=wk_cfg_pub", nil)
	blockedReq.Header.Set("Origin", "https://evil.example.com")
	blockedRec := httptest.NewRecorder()
	r.ServeHTTP(blockedRec, blockedReq)
	if blockedRec.Code != http.StatusForbidden {
		t.Fatalf("blocked origin expected 403, got %d body=%s", blockedRec.Code, blockedRec.Body.String())
	}
}
