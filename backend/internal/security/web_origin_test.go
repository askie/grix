package security

import (
	"net/http/httptest"
	"testing"
)

func TestIsAllowedWebOrigin(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name           string
		target         string
		requestHost    string
		origin         string
		allowedOrigins string
		want           bool
	}{
		{
			name:        "missing origin is allowed",
			target:      "http://127.0.0.1:27189/ws",
			requestHost: "127.0.0.1:27189",
			origin:      "",
			want:        true,
		},
		{
			name:        "same host and port is allowed",
			target:      "http://127.0.0.1:27189/ws",
			requestHost: "127.0.0.1:27189",
			origin:      "http://127.0.0.1:27189",
			want:        true,
		},
		{
			name:           "configured origin is allowed",
			target:         "http://127.0.0.1:27189/ws",
			requestHost:    "127.0.0.1:27189",
			origin:         "http://127.0.0.1:34123",
			allowedOrigins: "https://app.example.com, http://127.0.0.1:34123",
			want:           true,
		},
		{
			name:        "non loopback cross port is denied without whitelist",
			target:      "http://api.example.com/ws",
			requestHost: "api.example.com",
			origin:      "https://app.example.com",
			want:        false,
		},
		{
			name:        "loopback cross port is allowed",
			target:      "http://127.0.0.1:27189/ws",
			requestHost: "127.0.0.1:27189",
			origin:      "http://127.0.0.1:34123",
			want:        true,
		},
		{
			name:        "loopback hostname alias is allowed",
			target:      "http://localhost:27189/ws",
			requestHost: "localhost:27189",
			origin:      "http://127.0.0.1:34123",
			want:        true,
		},
		{
			name:        "invalid origin is denied",
			target:      "http://127.0.0.1:27189/ws",
			requestHost: "127.0.0.1:27189",
			origin:      "not-a-valid-origin",
			want:        false,
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			req := httptest.NewRequest("GET", tc.target, nil)
			if tc.requestHost != "" {
				req.Host = tc.requestHost
			}
			if tc.origin != "" {
				req.Header.Set("Origin", tc.origin)
			}

			got := IsAllowedWebOrigin(req, tc.allowedOrigins)
			if got != tc.want {
				t.Fatalf("IsAllowedWebOrigin() = %v, want %v", got, tc.want)
			}
		})
	}
}
