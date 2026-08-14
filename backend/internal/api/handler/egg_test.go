package handler

import (
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestResolveEggRequestLocale(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("prefer query locale", func(t *testing.T) {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		req := httptest.NewRequest("GET", "/eggs/search?locale=ja-JP", nil)
		req.Header.Set("X-App-Locale", "fr-FR")
		c.Request = req

		if got := resolveEggRequestLocale(c); got != "ja-JP" {
			t.Fatalf("resolveEggRequestLocale()=%q want=%q", got, "ja-JP")
		}
	})

	t.Run("fallback to app locale header", func(t *testing.T) {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		req := httptest.NewRequest("GET", "/eggs/search", nil)
		req.Header.Set("X-App-Locale", "fr-FR")
		req.Header.Set("Accept-Language", "de-DE,de;q=0.8")
		c.Request = req

		if got := resolveEggRequestLocale(c); got != "fr-FR" {
			t.Fatalf("resolveEggRequestLocale()=%q want=%q", got, "fr-FR")
		}
	})

	t.Run("fallback to accept language header", func(t *testing.T) {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		req := httptest.NewRequest("GET", "/eggs/search", nil)
		req.Header.Set("Accept-Language", "de-DE,de;q=0.8")
		c.Request = req

		if got := resolveEggRequestLocale(c); got != "de-DE,de;q=0.8" {
			t.Fatalf("resolveEggRequestLocale()=%q want=%q", got, "de-DE,de;q=0.8")
		}
	})
}
