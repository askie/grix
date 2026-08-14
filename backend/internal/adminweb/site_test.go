package adminweb

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/gin-gonic/gin"
)

func newTestRouter(t *testing.T) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	// Reproduce the production router shape that previously crashed at startup:
	// a top-level /api route group whose static "api" segment conflicts with a
	// registered /admin/*filepath catch-all in gin's radix tree. The admin SPA
	// must register without panicking alongside it.
	apiGroup := r.Group("/api")
	apiGroup.GET("/ping", func(c *gin.Context) { c.String(http.StatusOK, "api-pong") })
	r.GET("/v1/ping", func(c *gin.Context) { c.String(http.StatusOK, "v1-pong") })

	ok := registerRoutesWithRootFS(r, fstest.MapFS{
		"index.html": {
			Data: []byte("<!doctype html><html><body>admin-app</body></html>"),
		},
		"flutter_bootstrap.js": {
			Data: []byte("console.log('boot');"),
		},
		"main.dart.js": {
			Data: []byte("console.log('app');"),
		},
		"version.json": {
			Data: []byte(`{"version":"1.0.0","build_number":"16"}`),
		},
		"assets/AssetManifest.json": {
			Data: []byte(`{"assets":[]}`),
		},
	})
	if !ok {
		t.Fatalf("expected admin routes to register with an embedded index.html")
	}
	// Mirror production: webapp owns the NoRoute fallback for everything else.
	r.NoRoute(func(c *gin.Context) { c.String(http.StatusNotFound, "webapp-fallback") })
	return r
}

func TestAdminRoutesServeEmbeddedBundle(t *testing.T) {
	r := newTestRouter(t)

	tests := []struct {
		name         string
		method       string
		path         string
		wantStatus   int
		wantContains string
		wantCacheCtl string
	}{
		{
			name:         "admin root serves shell",
			method:       http.MethodGet,
			path:         "/admin",
			wantStatus:   http.StatusOK,
			wantContains: "admin-app",
			wantCacheCtl: "no-cache, must-revalidate",
		},
		{
			name:         "admin trailing slash serves shell",
			method:       http.MethodGet,
			path:         "/admin/",
			wantStatus:   http.StatusOK,
			wantContains: "admin-app",
			wantCacheCtl: "no-cache, must-revalidate",
		},
		{
			name:         "admin asset returns file",
			method:       http.MethodGet,
			path:         "/admin/flutter_bootstrap.js",
			wantStatus:   http.StatusOK,
			wantContains: "console.log",
			wantCacheCtl: "no-cache, must-revalidate",
		},
		{
			name:         "admin nested asset returns file",
			method:       http.MethodGet,
			path:         "/admin/assets/AssetManifest.json",
			wantStatus:   http.StatusOK,
			wantContains: "assets",
			wantCacheCtl: "public, max-age=0, must-revalidate",
		},
		{
			name:         "admin versioned asset uses immutable cache",
			method:       http.MethodGet,
			path:         "/admin/main.dart.js?build=16",
			wantStatus:   http.StatusOK,
			wantContains: "console.log",
			wantCacheCtl: "public, max-age=31536000, immutable",
		},
		{
			name:         "admin spa deep link falls back to shell",
			method:       http.MethodGet,
			path:         "/admin/users/42",
			wantStatus:   http.StatusOK,
			wantContains: "admin-app",
			wantCacheCtl: "no-cache, must-revalidate",
		},
		{
			name:       "admin missing asset with extension is 404",
			method:     http.MethodGet,
			path:       "/admin/missing.js",
			wantStatus: http.StatusNotFound,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req, err := http.NewRequest(tc.method, tc.path, nil)
			if err != nil {
				t.Fatalf("new request: %v", err)
			}
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			if w.Code != tc.wantStatus {
				t.Fatalf("expected status %d, got %d", tc.wantStatus, w.Code)
			}
			if tc.wantContains != "" && !strings.Contains(w.Body.String(), tc.wantContains) {
				t.Fatalf("expected body to contain %q, body=%s", tc.wantContains, w.Body.String())
			}
			if tc.wantCacheCtl != "" {
				if got := w.Header().Get("Cache-Control"); got != tc.wantCacheCtl {
					t.Fatalf("expected Cache-Control %q, got %q", tc.wantCacheCtl, got)
				}
			}
		})
	}
}

// Paths outside /admin must not be captured by the admin handler; they fall
// through to webapp's NoRoute handler.
func TestNonAdminPathsFallThrough(t *testing.T) {
	r := newTestRouter(t)

	for _, p := range []string{"/", "/chat/session/1", "/adminx"} {
		req, _ := http.NewRequest(http.MethodGet, p, nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if !strings.Contains(w.Body.String(), "webapp-fallback") {
			t.Fatalf("path %q should fall through to webapp, body=%s", p, w.Body.String())
		}
	}
}

// The admin middleware must not shadow the real API routes it coexists with.
func TestCoexistingRoutesStillReachable(t *testing.T) {
	r := newTestRouter(t)

	cases := map[string]string{
		"/api/ping": "api-pong",
		"/v1/ping":  "v1-pong",
	}
	for path, want := range cases {
		req, _ := http.NewRequest(http.MethodGet, path, nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), want) {
			t.Fatalf("%s: code=%d body=%s want %q", path, w.Code, w.Body.String(), want)
		}
	}
}

func TestRegisterRoutesReturnsFalseWithoutEmbeddedIndex(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	if registerRoutesWithRootFS(r, fstest.MapFS{
		"README.md": {Data: []byte("placeholder")},
	}) {
		t.Fatalf("expected admin registration to be disabled without index.html")
	}
}
