package middleware

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/askie/grix/backend/internal/model"
	jwtpkg "github.com/askie/grix/backend/internal/pkg/jwt"
	"github.com/askie/grix/backend/internal/pkg/testutil"
	"github.com/askie/grix/backend/internal/security"
	"github.com/askie/grix/backend/internal/store"
	"github.com/gin-gonic/gin"
)

func init() {
	gin.SetMode(gin.TestMode)
	jwtpkg.Init("test-secret-key-for-auth-middleware", 3600, 86400)
}

func TestAuth(t *testing.T) {
	testDB := testutil.NewTestDB()
	defer testDB.Close()
	store.DB = testDB.DB
	store.RDB = nil
	if err := store.DB.Create(&model.User{
		ID:       12345,
		Username: "authuser",
		Email:    "authuser@example.com",
		Nickname: "authuser",
		Status:   model.UserStatusActive,
	}).Error; err != nil {
		t.Fatalf("seed user error: %v", err)
	}

	// Create a valid token for testing
	validToken, _, _ := jwtpkg.GenerateAccessToken(12345)

	// Create an expired or invalid scenarios
	refreshToken, _ := jwtpkg.GenerateRefreshToken(12345)

	tests := []struct {
		name       string
		authHeader string
		wantStatus int
		wantUserID int64
	}{
		{
			name:       "valid bearer token",
			authHeader: "Bearer " + validToken,
			wantStatus: http.StatusOK,
			wantUserID: 12345,
		},
		{
			name:       "missing authorization header",
			authHeader: "",
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:       "invalid prefix",
			authHeader: "Basic " + validToken,
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:       "missing bearer prefix",
			authHeader: validToken,
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:       "invalid token",
			authHeader: "Bearer invalid.token.here",
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:       "refresh token instead of access token",
			authHeader: "Bearer " + refreshToken,
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:       "empty bearer",
			authHeader: "Bearer ",
			wantStatus: http.StatusUnauthorized,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup
			var capturedUserID int64
			r := gin.New()
			r.Use(Auth())
			r.GET("/test", func(c *gin.Context) {
				capturedUserID = GetUserID(c)
				c.Status(http.StatusOK)
			})

			// Execute
			req, _ := http.NewRequest(http.MethodGet, "/test", nil)
			if tt.authHeader != "" {
				req.Header.Set("Authorization", tt.authHeader)
			}
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			// Assert
			if w.Code != tt.wantStatus {
				t.Errorf("expected status %d, got %d, body: %s", tt.wantStatus, w.Code, w.Body.String())
			}

			if tt.wantStatus == http.StatusOK && capturedUserID != tt.wantUserID {
				t.Errorf("expected user_id %d, got %d", tt.wantUserID, capturedUserID)
			}
		})
	}
}

func TestGetUserID(t *testing.T) {
	tests := []struct {
		name     string
		setup    func(*gin.Context)
		expected int64
	}{
		{
			name: "user_id exists",
			setup: func(c *gin.Context) {
				c.Set("user_id", int64(12345))
			},
			expected: 12345,
		},
		{
			name:     "user_id does not exist",
			setup:    func(c *gin.Context) {},
			expected: 0,
		},
		{
			name: "user_id is wrong type",
			setup: func(c *gin.Context) {
				c.Set("user_id", "not-an-int64")
			},
			expected: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)

			tt.setup(c)

			result := GetUserID(c)
			if result != tt.expected {
				t.Errorf("expected %d, got %d", tt.expected, result)
			}
		})
	}
}

func TestAuthWithDifferentTokenTypes(t *testing.T) {
	testDB := testutil.NewTestDB()
	defer testDB.Close()
	store.DB = testDB.DB
	store.RDB = nil

	// Test with different user IDs
	userIDs := []int64{1, 100, 9999999999, -1}

	for _, userID := range userIDs {
		t.Run("user_"+strconv.FormatInt(userID, 10), func(t *testing.T) {
			name := "user_" + strconv.FormatInt(userID, 10)
			if err := store.DB.Create(&model.User{
				ID:       userID,
				Username: name,
				Email:    name + "@example.com",
				Nickname: name,
				Status:   model.UserStatusActive,
			}).Error; err != nil {
				t.Fatalf("seed user error: %v", err)
			}
			token, _, _ := jwtpkg.GenerateAccessToken(userID)

			r := gin.New()
			r.Use(Auth())
			r.GET("/test", func(c *gin.Context) {
				uid := GetUserID(c)
				if uid != userID {
					t.Errorf("expected user_id %d, got %d", userID, uid)
				}
				c.Status(http.StatusOK)
			})

			req, _ := http.NewRequest(http.MethodGet, "/test", nil)
			req.Header.Set("Authorization", "Bearer "+token)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			if w.Code != http.StatusOK {
				t.Errorf("expected status 200, got %d", w.Code)
			}
		})
	}
}

func TestAuthRejectsTokenIssuedBeforePasswordChange(t *testing.T) {
	testDB := testutil.NewTestDB()
	defer testDB.Close()
	store.DB = testDB.DB
	store.RDB = testutil.NewMockRedis()

	const userID int64 = 54321
	if err := store.DB.Create(&model.User{
		ID:       userID,
		Username: "pwdchangeuser",
		Email:    "pwdchange@example.com",
		Nickname: "pwdchange",
		Status:   model.UserStatusActive,
	}).Error; err != nil {
		t.Fatalf("seed user error: %v", err)
	}

	token, _, err := jwtpkg.GenerateAccessToken(userID)
	if err != nil {
		t.Fatalf("GenerateAccessToken() error = %v", err)
	}
	claims, err := jwtpkg.ValidateAccessToken(token)
	if err != nil {
		t.Fatalf("ValidateAccessToken() error = %v", err)
	}
	if claims.IssuedAt == nil {
		t.Fatal("expected access token issued_at")
	}
	if err := security.MarkUserPasswordChanged(userID, claims.IssuedAt.Time.Add(time.Second)); err != nil {
		t.Fatalf("MarkUserPasswordChanged() error = %v", err)
	}

	r := gin.New()
	r.Use(Auth())
	r.GET("/test", func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	req, _ := http.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected status %d, got %d", http.StatusUnauthorized, w.Code)
	}
}

func TestAuthRejectsRevokedLoginSession(t *testing.T) {
	testDB := testutil.NewTestDB()
	defer testDB.Close()

	store.DB = testDB.DB
	store.RDB = testutil.NewMockRedis()

	const (
		userID    int64 = 65432
		sessionID       = "revoked-login-session"
	)

	if err := store.DB.Create(&model.User{
		ID:       userID,
		Username: "revokedsessionuser",
		Email:    "revokedsession@example.com",
		Nickname: "revokedsession",
		Status:   model.UserStatusActive,
	}).Error; err != nil {
		t.Fatalf("seed user error: %v", err)
	}

	token, _, err := jwtpkg.GenerateAccessTokenWithSession(userID, sessionID)
	if err != nil {
		t.Fatalf("GenerateAccessTokenWithSession() error = %v", err)
	}
	if err := security.MarkLoginSessionRevoked(userID, sessionID); err != nil {
		t.Fatalf("MarkLoginSessionRevoked() error = %v", err)
	}

	r := gin.New()
	r.Use(Auth())
	r.GET("/test", func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	req, _ := http.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected status %d, got %d", http.StatusUnauthorized, w.Code)
	}
}
