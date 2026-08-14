package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	jwtpkg "github.com/askie/grix/backend/internal/pkg/jwt"
	"github.com/gin-gonic/gin"
)

func TestWidgetAuth(t *testing.T) {
	gin.SetMode(gin.TestMode)
	jwtpkg.Init("test-secret-key-for-widget-auth", 3600, 86400)

	validToken, _, err := jwtpkg.GenerateWidgetAccessToken(
		101, "widget-session-101", 202, 303, []string{"chat:send", "chat:sync"},
	)
	if err != nil {
		t.Fatalf("GenerateWidgetAccessToken() error = %v", err)
	}
	accessToken, _, err := jwtpkg.GenerateAccessToken(999)
	if err != nil {
		t.Fatalf("GenerateAccessToken() error = %v", err)
	}

	tests := []struct {
		name          string
		authHeader    string
		requiredScope []string
		wantStatus    int
	}{
		{
			name:          "valid widget token",
			authHeader:    "Bearer " + validToken,
			requiredScope: []string{"chat:send"},
			wantStatus:    http.StatusOK,
		},
		{
			name:       "missing authorization header",
			authHeader: "",
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:       "invalid token",
			authHeader: "Bearer not.a.valid.token",
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:       "wrong token type access token",
			authHeader: "Bearer " + accessToken,
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:          "insufficient widget scope",
			authHeader:    "Bearer " + validToken,
			requiredScope: []string{"chat:ack"},
			wantStatus:    http.StatusForbidden,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var (
				gotSiteID    int64
				gotVisitorID int64
				gotOwnerID   int64
				gotSessionID string
			)

			router := gin.New()
			router.Use(WidgetAuth(tc.requiredScope...))
			router.GET("/widget-test", func(c *gin.Context) {
				gotSiteID = GetWidgetSiteID(c)
				gotVisitorID = GetWidgetVisitorID(c)
				gotOwnerID = GetWidgetOwnerUserID(c)
				gotSessionID = GetWidgetSessionID(c)
				c.Status(http.StatusOK)
			})

			req, _ := http.NewRequest(http.MethodGet, "/widget-test", nil)
			if tc.authHeader != "" {
				req.Header.Set("Authorization", tc.authHeader)
			}
			resp := httptest.NewRecorder()
			router.ServeHTTP(resp, req)

			if resp.Code != tc.wantStatus {
				t.Fatalf("expected status %d, got %d body=%s", tc.wantStatus, resp.Code, resp.Body.String())
			}
			if tc.wantStatus == http.StatusOK {
				if gotSiteID != 101 || gotVisitorID != 202 || gotOwnerID != 303 || gotSessionID != "widget-session-101" {
					t.Fatalf("unexpected widget context values: site=%d visitor=%d owner=%d session=%q",
						gotSiteID, gotVisitorID, gotOwnerID, gotSessionID)
				}
			}
		})
	}
}
