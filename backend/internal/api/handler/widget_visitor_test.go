package handler

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/askie/grix/backend/internal/model"
	jwtpkg "github.com/askie/grix/backend/internal/pkg/jwt"
	"github.com/askie/grix/backend/internal/pkg/snowflake"
	"github.com/askie/grix/backend/internal/pkg/testutil"
	"github.com/askie/grix/backend/internal/store"
	"github.com/gin-gonic/gin"
	"gorm.io/datatypes"
)

func setupWidgetVisitorHandlerTest(t *testing.T) *testutil.TestDB {
	t.Helper()
	tdb := testutil.NewTestDB()
	store.DB = tdb.DB
	store.RDB = nil
	jwtpkg.Init("widget-visitor-handler-test-secret", 3600, 86400)
	if err := snowflake.Init(1); err != nil {
		t.Fatalf("snowflake init error: %v", err)
	}
	return tdb
}

func TestWidgetVisitorInitHandlerSuccess(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tdb := setupWidgetVisitorHandlerTest(t)
	defer tdb.Close()

	now := time.Now().UTC()
	if err := store.DB.Create(&model.WidgetSite{
		ID:             9601,
		OwnerUserID:    5601,
		SiteKey:        "wk_handler_ok",
		SiteSecretHash: "hash",
		SiteName:       "Handler",
		AllowedOrigins: datatypes.JSON([]byte(`["https://widget.example.com"]`)),
		Status:         model.WidgetSiteStatusActive,
		CreatedAt:      now,
		UpdatedAt:      now,
	}).Error; err != nil {
		t.Fatalf("seed widget site error: %v", err)
	}

	router := gin.New()
	router.POST("/v1/widget/visitor/init", WidgetVisitorInit)

	body := bytes.NewBufferString(`{"site_key":"wk_handler_ok","visitor_key":"vk_h_1","visitor_name":"Bob"}`)
	req, _ := http.NewRequest(http.MethodPost, "/v1/widget/visitor/init", body)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "https://widget.example.com")
	req.Header.Set("X-Forwarded-Proto", "https")
	req.Host = "widget.example.com"
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d body=%s", resp.Code, resp.Body.String())
	}

	var payload map[string]any
	if err := json.Unmarshal(resp.Body.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal response error: %v", err)
	}
	data, ok := payload["data"].(map[string]any)
	if !ok {
		t.Fatalf("response data missing, body=%s", resp.Body.String())
	}
	if data["ws_url"] != "wss://widget.example.com/v1/widget/ws" {
		t.Fatalf("unexpected ws_url=%v", data["ws_url"])
	}
	if data["session_id"] == "" || data["widget_token"] == "" {
		t.Fatalf("session_id/widget_token should not be empty, data=%v", data)
	}
}

func TestWidgetVisitorInitHandlerOriginRejected(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tdb := setupWidgetVisitorHandlerTest(t)
	defer tdb.Close()

	now := time.Now().UTC()
	if err := store.DB.Create(&model.WidgetSite{
		ID:             9701,
		OwnerUserID:    6701,
		SiteKey:        "wk_handler_origin",
		SiteSecretHash: "hash",
		SiteName:       "Origin",
		AllowedOrigins: datatypes.JSON([]byte(`["https://allowed.example.com"]`)),
		Status:         model.WidgetSiteStatusActive,
		CreatedAt:      now,
		UpdatedAt:      now,
	}).Error; err != nil {
		t.Fatalf("seed widget site error: %v", err)
	}

	router := gin.New()
	router.POST("/v1/widget/visitor/init", WidgetVisitorInit)

	body := bytes.NewBufferString(`{"site_key":"wk_handler_origin"}`)
	req, _ := http.NewRequest(http.MethodPost, "/v1/widget/visitor/init", body)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "https://blocked.example.com")
	req.Host = "widget.example.com"
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)
	if resp.Code != http.StatusForbidden {
		t.Fatalf("expected status 403, got %d body=%s", resp.Code, resp.Body.String())
	}
}

func TestWidgetVisitorInitAndConfigHandlerAppChannel(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tdb := setupWidgetVisitorHandlerTest(t)
	defer tdb.Close()

	now := time.Now().UTC()
	if err := store.DB.Create(&model.WidgetSite{
		ID:             9801,
		OwnerUserID:    6801,
		SiteKey:        "wk_handler_app",
		SiteSecretHash: "hash",
		SiteName:       "Handler App",
		AllowedOrigins: datatypes.JSON([]byte(`["https://web.example.com"]`)),
		AllowedApps:    datatypes.JSON([]byte(`["com.example.mobile"]`)),
		DisplayConfig:  datatypes.JSON([]byte(`{"theme_color":"#112233","title":"App Chat"}`)),
		Status:         model.WidgetSiteStatusActive,
		CreatedAt:      now,
		UpdatedAt:      now,
	}).Error; err != nil {
		t.Fatalf("seed widget site error: %v", err)
	}

	router := gin.New()
	router.POST("/v1/widget/visitor/init", WidgetVisitorInit)
	router.GET("/v1/widget/config", WidgetVisitorConfig)

	// 1. POST /v1/widget/visitor/init with valid client -> 200 (no Origin header)
	initBody := bytes.NewBufferString(`{
		"site_key":"wk_handler_app",
		"visitor_name":"MobileUser",
		"client":{"platform":"ios","app_id":"com.example.mobile","sdk_version":"1.0.0"}
	}`)
	initReq, _ := http.NewRequest(http.MethodPost, "/v1/widget/visitor/init", initBody)
	initReq.Header.Set("Content-Type", "application/json")
	initReq.Host = "api.example.com"
	initResp := httptest.NewRecorder()
	router.ServeHTTP(initResp, initReq)
	if initResp.Code != http.StatusOK {
		t.Fatalf("expected status 200 for allowed app, got %d body=%s", initResp.Code, initResp.Body.String())
	}

	// 2. POST /v1/widget/visitor/init with unallowed app_id -> 403
	badInitBody := bytes.NewBufferString(`{
		"site_key":"wk_handler_app",
		"client":{"platform":"ios","app_id":"com.evil.app","sdk_version":"1.0.0"}
	}`)
	badInitReq, _ := http.NewRequest(http.MethodPost, "/v1/widget/visitor/init", badInitBody)
	badInitReq.Header.Set("Content-Type", "application/json")
	badInitReq.Host = "api.example.com"
	badInitResp := httptest.NewRecorder()
	router.ServeHTTP(badInitResp, badInitReq)
	if badInitResp.Code != http.StatusForbidden {
		t.Fatalf("expected status 403 for unallowed app, got %d body=%s", badInitResp.Code, badInitResp.Body.String())
	}

	// 3. GET /v1/widget/config with query params platform/app_id/sdk_version -> 200
	cfgReq, _ := http.NewRequest(http.MethodGet, "/v1/widget/config?site_key=wk_handler_app&platform=flutter&app_id=com.example.mobile&sdk_version=1.0.0", nil)
	cfgResp := httptest.NewRecorder()
	router.ServeHTTP(cfgResp, cfgReq)
	if cfgResp.Code != http.StatusOK {
		t.Fatalf("expected status 200 for config with allowed app, got %d body=%s", cfgResp.Code, cfgResp.Body.String())
	}

	// 4. GET /v1/widget/config with unallowed app_id -> 403
	badCfgReq, _ := http.NewRequest(http.MethodGet, "/v1/widget/config?site_key=wk_handler_app&platform=android&app_id=com.evil.app", nil)
	badCfgResp := httptest.NewRecorder()
	router.ServeHTTP(badCfgResp, badCfgReq)
	if badCfgResp.Code != http.StatusForbidden {
		t.Fatalf("expected status 403 for config with unallowed app, got %d body=%s", badCfgResp.Code, badCfgResp.Body.String())
	}
}
