package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	jwtpkg "github.com/askie/grix/backend/internal/pkg/jwt"
	"github.com/askie/grix/backend/internal/pkg/testutil"
	"github.com/askie/grix/backend/internal/store"
	"github.com/gin-gonic/gin"
)

func TestRateLimitSyncBootstrapDuplicates(t *testing.T) {
	gin.SetMode(gin.TestMode)
	previousRedis := store.RDB
	store.RDB = testutil.NewMockRedis()
	t.Cleanup(func() {
		_ = store.RDB.Close()
		store.RDB = previousRedis
	})

	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set("user_id", int64(42))
		c.Set(claimsContextKey, &jwtpkg.Claims{SessionID: c.GetHeader("X-Test-Session")})
		c.Next()
	})
	router.GET(
		"/sessions/list",
		RateLimitSyncBootstrapDuplicates(time.Minute),
		func(c *gin.Context) { c.Status(http.StatusOK) },
	)

	request := func(rawQuery, sessionID string) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, "/sessions/list?"+rawQuery, nil)
		req.Header.Set("X-Test-Session", sessionID)
		resp := httptest.NewRecorder()
		router.ServeHTTP(resp, req)
		return resp
	}

	const bootstrap = "sync_head=1&limit=10000&offset=0"
	if got := request(bootstrap, "device-a"); got.Code != http.StatusOK {
		t.Fatalf("first bootstrap status=%d want=%d", got.Code, http.StatusOK)
	}
	duplicate := request("offset=0&limit=10000&sync_head=1", "device-a")
	if duplicate.Code != http.StatusTooManyRequests {
		t.Fatalf("duplicate bootstrap status=%d want=%d", duplicate.Code, http.StatusTooManyRequests)
	}
	if got := duplicate.Header().Get("Retry-After"); got != "60" {
		t.Fatalf("Retry-After=%q want=60", got)
	}

	if got := request(bootstrap, "device-b"); got.Code != http.StatusOK {
		t.Fatalf("other device status=%d want=%d", got.Code, http.StatusOK)
	}
	if got := request("sync_head=1&limit=200&offset=0", "device-a"); got.Code != http.StatusOK {
		t.Fatalf("different bootstrap request status=%d want=%d", got.Code, http.StatusOK)
	}
	for i := 0; i < 2; i++ {
		if got := request("limit=50&offset=0", "device-a"); got.Code != http.StatusOK {
			t.Fatalf("ordinary list attempt=%d status=%d want=%d", i+1, got.Code, http.StatusOK)
		}
	}
}

func TestRateLimitSyncBootstrapDuplicatesFallsBackToUser(t *testing.T) {
	gin.SetMode(gin.TestMode)
	previousRedis := store.RDB
	store.RDB = testutil.NewMockRedis()
	t.Cleanup(func() {
		_ = store.RDB.Close()
		store.RDB = previousRedis
	})

	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set("user_id", int64(42))
		c.Next()
	})
	router.GET(
		"/sessions/list",
		RateLimitSyncBootstrapDuplicates(time.Minute),
		func(c *gin.Context) { c.Status(http.StatusOK) },
	)

	request := func() int {
		req := httptest.NewRequest(http.MethodGet, "/sessions/list?sync_head=1&limit=10000&offset=0", nil)
		resp := httptest.NewRecorder()
		router.ServeHTTP(resp, req)
		return resp.Code
	}
	if got := request(); got != http.StatusOK {
		t.Fatalf("first legacy bootstrap status=%d want=%d", got, http.StatusOK)
	}
	if got := request(); got != http.StatusTooManyRequests {
		t.Fatalf("duplicate legacy bootstrap status=%d want=%d", got, http.StatusTooManyRequests)
	}
}
