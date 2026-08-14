package ws

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/askie/grix/backend/internal/pkg/testutil"
	"github.com/askie/grix/backend/internal/store"
	"github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
)

func TestWebhookIncomingRejectsInvalidRequests(t *testing.T) {
	gin.SetMode(gin.TestMode)
	s := &Server{}
	tdb := testutil.NewTestDB()
	defer tdb.Close()
	store.DB = tdb.DB
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis start: %v", err)
	}
	defer mr.Close()
	store.RDB = redis.NewClient(&redis.Options{Addr: mr.Addr()})

	t.Run("method not allowed", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodGet, "/v1/webhook/incoming/abc", nil)
		w := httptest.NewRecorder()
		s.handleWebhookIncoming(w, req)
		if w.Code != http.StatusMethodNotAllowed {
			t.Fatalf("expected 405 got %d", w.Code)
		}
	})

	t.Run("invalid payload", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodPost, "/v1/webhook/incoming/abc", strings.NewReader("{"))
		w := httptest.NewRecorder()
		s.handleWebhookIncoming(w, req)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 got %d", w.Code)
		}
	})

	t.Run("not found token", func(t *testing.T) {
		req, _ := http.NewRequest(http.MethodPost, "/v1/webhook/incoming/whk_not_exist", strings.NewReader(`{"content":"hi"}`))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		s.handleWebhookIncoming(w, req)
		if w.Code != http.StatusNotFound {
			t.Fatalf("expected 404 got %d body=%s", w.Code, w.Body.String())
		}
	})

	t.Run("rate limited", func(t *testing.T) {
		token := "whk_rate_test"
		ip := "127.0.0.1"
		for i := 0; i < webhookRateLimitMax; i++ {
			if !allowWebhookRate(token, ip) {
				t.Fatalf("unexpected limited at %d", i)
			}
		}
		if allowWebhookRate(token, ip) {
			t.Fatalf("expected rate limited")
		}
	})
}
