package handler

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/testutil"
	"github.com/askie/grix/backend/internal/store"
	"github.com/gin-gonic/gin"
	"gorm.io/datatypes"
)

func TestWidgetSiteDetailIncludesEmbedCode(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tdb := testutil.NewTestDB()
	defer tdb.Close()
	store.DB = tdb.DB
	now := time.Now().UTC()
	if err := store.DB.Create(&model.WidgetSite{ID: 1001, OwnerUserID: 101, SiteKey: "wk_demo", SiteSecretHash: "hash", SiteName: "Demo", AllowedOrigins: datatypes.JSON([]byte(`["https://demo.example.com"]`)), Status: model.WidgetSiteStatusActive, CreatedAt: now, UpdatedAt: now}).Error; err != nil {
		t.Fatalf("seed widget site error: %v", err)
	}

	r := gin.New()
	r.Use(func(c *gin.Context) {
		c.Set("user_id", int64(101))
		c.Next()
	})
	r.GET("/v1/widget/sites/detail", WidgetSiteDetail)

	req := httptest.NewRequest(http.MethodGet, "/v1/widget/sites/detail?id=1001", nil)
	req.Host = "api.example.com"
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !(strings.Contains(body, "embed_code") && strings.Contains(body, "data-site-key=\\\"wk_demo\\\"")) {
		t.Fatalf("embed payload missing, body=%s", body)
	}
}
