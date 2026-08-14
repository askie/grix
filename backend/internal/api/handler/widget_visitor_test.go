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
