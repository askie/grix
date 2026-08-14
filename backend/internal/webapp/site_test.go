package webapp

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/gin-gonic/gin"
)

func TestHandleNoRouteServesEmbeddedWebApp(t *testing.T) {
	gin.SetMode(gin.TestMode)

	r := gin.New()
	handler := siteHandler{
		root: fstest.MapFS{
			"index.html": {
				Data: []byte("<!doctype html><html><body>web-app</body></html>"),
			},
			"flutter_bootstrap.js": {
				Data: []byte("console.log('boot');"),
			},
			"flutter_service_worker.js": {
				Data: []byte("self.unregister();"),
			},
			"main.dart.js": {
				Data: []byte("console.log('app');"),
			},
			"sw.js": {
				Data: []byte("self.skipWaiting();"),
			},
			"version.json": {
				Data: []byte(`{"version":"2.3.0","build_number":"355"}`),
			},
			".last_build_id": {
				Data: []byte("build-355"),
			},
			"assets/AssetManifest.json": {
				Data: []byte(`{"assets":[]}`),
			},
		},
	}
	r.NoRoute(handler.handleNoRoute)

	tests := []struct {
		name         string
		method       string
		path         string
		wantStatus   int
		wantContains string
		wantCacheCtl string
	}{
		{
			name:         "root returns index",
			method:       http.MethodGet,
			path:         "/",
			wantStatus:   http.StatusOK,
			wantContains: "web-app",
			wantCacheCtl: "no-cache, must-revalidate",
		},
		{
			name:         "asset returns file",
			method:       http.MethodGet,
			path:         "/flutter_bootstrap.js",
			wantStatus:   http.StatusOK,
			wantContains: "console.log",
			wantCacheCtl: "no-cache, must-revalidate",
		},
		{
			name:         "legacy flutter sw uses no-store cache policy",
			method:       http.MethodGet,
			path:         "/flutter_service_worker.js",
			wantStatus:   http.StatusOK,
			wantContains: "self.unregister",
			wantCacheCtl: "no-cache, must-revalidate",
		},
		{
			name:         "unversioned asset requires revalidation",
			method:       http.MethodGet,
			path:         "/main.dart.js",
			wantStatus:   http.StatusOK,
			wantContains: "console.log",
			wantCacheCtl: "public, max-age=0, must-revalidate",
		},
		{
			name:         "versioned asset uses immutable cache policy",
			method:       http.MethodGet,
			path:         "/main.dart.js?build=build-355",
			wantStatus:   http.StatusOK,
			wantContains: "console.log",
			wantCacheCtl: "public, max-age=31536000, immutable",
		},
		{
			name:         "sw uses no-store cache policy",
			method:       http.MethodGet,
			path:         "/sw.js",
			wantStatus:   http.StatusOK,
			wantContains: "self.skipWaiting",
			wantCacheCtl: "no-cache, must-revalidate",
		},
		{
			name:         "version metadata uses no-store cache policy",
			method:       http.MethodGet,
			path:         "/version.json",
			wantStatus:   http.StatusOK,
			wantContains: "\"version\"",
			wantCacheCtl: "no-cache, must-revalidate",
		},
		{
			name:         "build id metadata uses no-store cache policy",
			method:       http.MethodGet,
			path:         "/.last_build_id",
			wantStatus:   http.StatusOK,
			wantContains: "build-355",
			wantCacheCtl: "no-cache, must-revalidate",
		},
		{
			name:         "spa route falls back to index",
			method:       http.MethodGet,
			path:         "/chat/session/42",
			wantStatus:   http.StatusOK,
			wantContains: "web-app",
			wantCacheCtl: "no-cache, must-revalidate",
		},
		{
			name:       "missing asset with extension stays 404",
			method:     http.MethodGet,
			path:       "/missing.js",
			wantStatus: http.StatusNotFound,
		},
		{
			name:       "reserved api path stays 404",
			method:     http.MethodGet,
			path:       "/v1/unknown",
			wantStatus: http.StatusNotFound,
		},
		{
			name:       "reserved ws path stays 404",
			method:     http.MethodGet,
			path:       "/ws",
			wantStatus: http.StatusNotFound,
		},
		{
			name:       "non get request stays 404",
			method:     http.MethodPost,
			path:       "/chat/session/42",
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
				t.Fatalf("expected response to contain %q, body=%s", tc.wantContains, w.Body.String())
			}
			if tc.wantCacheCtl != "" {
				if got := w.Header().Get("Cache-Control"); got != tc.wantCacheCtl {
					t.Fatalf("expected Cache-Control %q, got %q", tc.wantCacheCtl, got)
				}
			}
		})
	}
}

func TestRegisterRoutesReturnsFalseWithoutEmbeddedIndex(t *testing.T) {
	gin.SetMode(gin.TestMode)

	r := gin.New()
	if registerRoutesWithRootFS(r, fstest.MapFS{
		"README.md": {
			Data: []byte("placeholder"),
		},
	}) {
		t.Fatalf("expected embedded app registration to be disabled without index.html")
	}
}
