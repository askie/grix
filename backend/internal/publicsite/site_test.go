package publicsite

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestRegisterRoutes(t *testing.T) {
	gin.SetMode(gin.TestMode)

	r := gin.New()
	RegisterRoutes(r)

	tests := []struct {
		name         string
		path         string
		wantContains string
	}{
		{
			name:         "privacy policy page",
			path:         PrivacyPolicyPath,
			wantContains: "Privacy Policy",
		},
		{
			name:         "terms of service page",
			path:         TermsOfServicePath,
			wantContains: "Terms of Service",
		},
		{
			name:         "account deletion page",
			path:         AccountDeletionPath,
			wantContains: "Account Deletion",
		},
		{
			name:         "support page",
			path:         SupportPath,
			wantContains: "Support",
		},
		{
			name:         "support page hero contains grix login url",
			path:         SupportPath,
			wantContains: "https://grix.dhf.pub",
		},
		{
			name:         "assets are served",
			path:         assetsBasePath + "/public-docs.css",
			wantContains: ":root",
		},
		{
			name:         "widget loader served",
			path:         widgetBasePath + "/widget.js",
			wantContains: "grix-widget",
		},
		{
			name:         "widget frame served",
			path:         "/public/widget/frame.html",
			wantContains: "Online Service",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req, err := http.NewRequest(http.MethodGet, tc.path, nil)
			if err != nil {
				t.Fatalf("new request: %v", err)
			}

			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			if w.Code != http.StatusOK {
				t.Fatalf("expected status 200, got %d", w.Code)
			}
			if !strings.Contains(w.Body.String(), tc.wantContains) {
				t.Fatalf("expected response to contain %q, body=%s", tc.wantContains, w.Body.String())
			}
		})
	}
}

func TestRegisterRoutesSupportsHeadForPublicPages(t *testing.T) {
	gin.SetMode(gin.TestMode)

	r := gin.New()
	RegisterRoutes(r)

	for _, page := range pages {
		t.Run(page.route, func(t *testing.T) {
			req, err := http.NewRequest(http.MethodHead, page.route, nil)
			if err != nil {
				t.Fatalf("new request: %v", err)
			}

			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			if w.Code != http.StatusOK {
				t.Fatalf("expected status 200, got %d", w.Code)
			}
			if w.Body.Len() != 0 {
				t.Fatalf("expected empty HEAD body, got %q", w.Body.String())
			}
		})
	}
}

func TestOnlySupportPageContainsGrixHomepageURL(t *testing.T) {
	gin.SetMode(gin.TestMode)

	r := gin.New()
	RegisterRoutes(r)

	tests := []struct {
		name string
		path string
		want bool
	}{
		{name: "support contains url", path: SupportPath, want: true},
		{name: "terms does not contain url", path: TermsOfServicePath, want: false},
		{name: "account deletion does not contain url", path: AccountDeletionPath, want: false},
		{name: "privacy policy does not contain url", path: PrivacyPolicyPath, want: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req, err := http.NewRequest(http.MethodGet, tc.path, nil)
			if err != nil {
				t.Fatalf("new request: %v", err)
			}

			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			if w.Code != http.StatusOK {
				t.Fatalf("expected status 200, got %d", w.Code)
			}

			got := strings.Contains(w.Body.String(), "https://grix.dhf.pub")
			if got != tc.want {
				t.Fatalf("expected url presence=%t, got %t, body=%s", tc.want, got, w.Body.String())
			}
		})
	}
}
