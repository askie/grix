package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

func TestCORS_WidgetPublicPath_AllowsArbitraryOrigin(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name string
		path string
	}{
		{"visitor init", "/v1/widget/visitor/init"},
		{"widget config", "/v1/widget/config"},
		{"widget ws", "/v1/widget/ws"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := gin.New()
			r.Use(CORS())
			r.POST(tt.path, func(c *gin.Context) { c.Status(http.StatusOK) })

			// Preflight
			req := httptest.NewRequest(http.MethodOptions, tt.path, nil)
			req.Header.Set("Origin", "https://grix-website.pages.dev")
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			assert.Equal(t, http.StatusNoContent, w.Code)
			assert.Equal(t, "https://grix-website.pages.dev", w.Header().Get("Access-Control-Allow-Origin"))
			assert.Equal(t, "Origin", w.Header().Get("Vary"))
			assert.Equal(t, "no-store", w.Header().Get("Cache-Control"),
				"widget public responses must never be cached")
		})
	}
}

func TestCORS_NonWidgetPath_NoCacheControl(t *testing.T) {
	gin.SetMode(gin.TestMode)

	r := gin.New()
	r.Use(CORS())
	r.POST("/v1/auth/login", func(c *gin.Context) { c.Status(http.StatusOK) })

	req := httptest.NewRequest(http.MethodPost, "/v1/auth/login", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Empty(t, w.Header().Get("Cache-Control"),
		"non-widget routes should not get the widget no-store header")
}

func TestCORS_WidgetPublicPath_Post_AllowsArbitraryOrigin(t *testing.T) {
	gin.SetMode(gin.TestMode)

	r := gin.New()
	r.Use(CORS())
	r.POST("/v1/widget/visitor/init", func(c *gin.Context) { c.Status(http.StatusOK) })

	req := httptest.NewRequest(http.MethodPost, "/v1/widget/visitor/init", nil)
	req.Header.Set("Origin", "https://random-third-party.example.com")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, "https://random-third-party.example.com", w.Header().Get("Access-Control-Allow-Origin"))
}

func TestCORS_NonWidgetPath_RejectsUnknownOrigin(t *testing.T) {
	gin.SetMode(gin.TestMode)

	r := gin.New()
	r.Use(CORS())
	r.POST("/v1/auth/login", func(c *gin.Context) { c.Status(http.StatusOK) })

	req := httptest.NewRequest(http.MethodOptions, "/v1/auth/login", nil)
	req.Header.Set("Origin", "https://unknown.example.com")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNoContent, w.Code)
	assert.Empty(t, w.Header().Get("Access-Control-Allow-Origin"))
}

func TestCORS_WidgetManagementPath_NotInPublicPaths(t *testing.T) {
	gin.SetMode(gin.TestMode)

	r := gin.New()
	r.Use(CORS())
	r.POST("/v1/widget/sites/create", func(c *gin.Context) { c.Status(http.StatusOK) })

	req := httptest.NewRequest(http.MethodOptions, "/v1/widget/sites/create", nil)
	req.Header.Set("Origin", "https://grix-website.pages.dev")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNoContent, w.Code)
	assert.Empty(t, w.Header().Get("Access-Control-Allow-Origin"),
		"widget management routes should NOT allow arbitrary origins")
}

func TestIsWidgetPublicPath(t *testing.T) {
	assert.True(t, isWidgetPublicPath("/v1/widget/visitor/init"))
	assert.True(t, isWidgetPublicPath("/v1/widget/config"))
	assert.True(t, isWidgetPublicPath("/v1/widget/ws"))
	assert.False(t, isWidgetPublicPath("/v1/widget/sites/create"))
	assert.False(t, isWidgetPublicPath("/v1/widget/sessions/list"))
	assert.False(t, isWidgetPublicPath("/v1/auth/login"))
	assert.False(t, isWidgetPublicPath("/v1/agents/list"))
}
