package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/askie/grix/backend/internal/pkg/logger"
	"github.com/gin-gonic/gin"
)

func TestHealthSupportsHead(t *testing.T) {
	gin.SetMode(gin.TestMode)
	logger.Init()

	router := SetupRouter()
	req, err := http.NewRequest(http.MethodHead, "/health", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, recorder.Code)
	}
}

func TestWidgetVisitorInitRouteAlwaysRegistered(t *testing.T) {
	gin.SetMode(gin.TestMode)
	logger.Init()

	router := SetupRouter()
	req := httptest.NewRequest(http.MethodPost, "/v1/widget/visitor/init", nil)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)
	if recorder.Code == http.StatusNotFound {
		t.Fatalf("widget visitor init route should always be registered; got status %d", recorder.Code)
	}
}

// The watch credential is minted with the phone's access token; an unauthenticated
// caller must never reach the issuer.
func TestWatchIssueRequiresAccessToken(t *testing.T) {
	gin.SetMode(gin.TestMode)
	logger.Init()

	router := SetupRouter()
	for _, header := range []string{"", "Bearer not-a-token"} {
		req := httptest.NewRequest(http.MethodPost, "/v1/auth/watch/issue", nil)
		if header != "" {
			req.Header.Set("Authorization", header)
		}
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, req)
		if recorder.Code != http.StatusUnauthorized {
			t.Fatalf("authorization=%q: expected status %d, got %d", header, http.StatusUnauthorized, recorder.Code)
		}
	}
}
