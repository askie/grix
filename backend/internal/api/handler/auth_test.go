package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/askie/grix/backend/internal/featuregate"
	"github.com/askie/grix/backend/internal/model"
	jwtpkg "github.com/askie/grix/backend/internal/pkg/jwt"
	"github.com/askie/grix/backend/internal/pkg/snowflake"
	"github.com/askie/grix/backend/internal/pkg/testutil"
	"github.com/askie/grix/backend/internal/store"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

func init() {
	gin.SetMode(gin.TestMode)
}

// testContext holds test resources that need cleanup
type testContext struct {
	router *gin.Engine
	db     *testutil.TestDB
}

// setupTest creates a test environment with all dependencies initialized
func setupTest(t *testing.T) *testContext {
	t.Helper()

	// Initialize test database
	testDB := testutil.NewTestDB()

	// Replace global DB with test DB
	store.DB = testDB.DB
	store.RDB = testutil.NewMockRedis()

	// 注册受 auth_register 公共功能门控制（生产默认关闭，需管理员开启）。
	// 注册相关用例依赖其开启；SaveGate 同时失效全局缓存，隔离跨用例污染。
	if err := featuregate.SaveGate("auth_register", "注册", model.FeatureStatusEnabled); err != nil {
		t.Fatalf("failed to enable auth_register gate: %v", err)
	}

	// Initialize JWT
	jwtpkg.Init("test-secret-key-for-testing", 3600, 86400)

	// Initialize snowflake for ID generation
	if err := snowflake.Init(1); err != nil {
		t.Fatalf("failed to init snowflake: %v", err)
	}

	// Initialize feature gates for auth
	authRegisterGate := &model.FeatureGate{
		Key:         "auth_register",
		DisplayName: "用户注册",
		Status:      model.FeatureStatusEnabled,
	}
	_ = testDB.DB.FirstOrCreate(authRegisterGate, "key = ?", "auth_register")

	// Initialize system settings
	authSettings := map[string]interface{}{
		"auto_add_customer_user_id": 0,
	}
	raw, _ := json.Marshal(authSettings)
	authSetting := &model.SystemSetting{
		Key:   "auth",
		Value: raw,
	}
	_ = testDB.DB.FirstOrCreate(authSetting, "key = ?", "auth")

	// Create router
	r := gin.New()
	r.POST("/login", Login)
	r.POST("/register", Register)
	r.POST("/refresh", Refresh)
	r.POST("/logout", Logout)

	// Protected routes
	auth := r.Group("/")
	auth.Use(func(c *gin.Context) {
		// Mock auth middleware for testing
		token := c.GetHeader("Authorization")
		if token == "" {
			c.AbortWithStatusJSON(401, gin.H{"error": "unauthorized"})
			return
		}
		claims, err := jwtpkg.ValidateAccessToken(token)
		if err != nil {
			c.AbortWithStatusJSON(401, gin.H{"error": "invalid token"})
			return
		}
		c.Set("user_id", claims.UserID)
		c.Next()
	})
	auth.GET("/profile", GetProfile)
	auth.PUT("/profile", UpdateProfile)
	auth.DELETE("/me", DeleteAccount)

	return &testContext{
		router: r,
		db:     testDB,
	}
}

func TestDeleteAccount(t *testing.T) {
	tc := setupTest(t)
	defer tc.cleanup()

	store.RDB = testutil.NewMockRedis()

	fixture := testutil.NewFixtureBuilder(tc.db.DB)
	user := fixture.CreateUser(func(u *model.User) {
		u.ID = 9800001
		u.Username = "delete_handler_user"
	})
	accessToken, _, _ := jwtpkg.GenerateAccessToken(user.ID)

	req, _ := http.NewRequest(http.MethodDelete, "/me", nil)
	req.Header.Set("Authorization", accessToken)

	w := httptest.NewRecorder()
	tc.router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d, body=%s", http.StatusOK, w.Code, w.Body.String())
	}

	var deletedUser model.User
	if err := tc.db.DB.First(&deletedUser, user.ID).Error; err != nil {
		t.Fatalf("load deleted user: %v", err)
	}
	if deletedUser.Status != model.UserStatusDeleted {
		t.Fatalf("expected user deleted status, got %d", deletedUser.Status)
	}
}

// cleanup releases test resources
func (tc *testContext) cleanup() {
	tc.db.Close()
}

// TestRegisterNewUser tests login with a new user (auto-register)
func TestRegisterNewUser(t *testing.T) {
	tc := setupTest(t)
	defer tc.cleanup()

	tests := []struct {
		name       string
		payload    registerReq
		wantStatus int
		checkFunc  func(t *testing.T, body map[string]interface{})
	}{
		{
			name: "register new user",
			payload: registerReq{
				Email:     "newuser@example.com",
				Password:  "password123",
				EmailCode: "123456",
				DeviceID:  "handler-device-1",
				Platform:  "ios",
			},
			wantStatus: http.StatusOK,
			checkFunc: func(t *testing.T, body map[string]interface{}) {
				if body["access_token"] == nil {
					t.Error("expected access_token in response")
				}
				if body["refresh_token"] == nil {
					t.Error("expected refresh_token in response")
				}
				user := body["user"].(map[string]interface{})
				if user["email"] != "newuser@example.com" {
					t.Errorf("expected email 'newuser@example.com', got %v", user["email"])
				}
			},
		},
		{
			name: "missing email",
			payload: registerReq{
				Email:     "",
				Password:  "password123",
				EmailCode: "123456",
				DeviceID:  "handler-device-2",
				Platform:  "ios",
			},
			wantStatus: http.StatusBadRequest,
		},
		{
			name: "missing password",
			payload: registerReq{
				Email:     "testuser@example.com",
				Password:  "",
				EmailCode: "123456",
				DeviceID:  "handler-device-3",
				Platform:  "ios",
			},
			wantStatus: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.payload.Email != "" && tt.payload.EmailCode != "" {
				key := fmt.Sprintf("auth:email_code:%s:%s", "register", tt.payload.Email)
				if err := store.RDB.Set(context.Background(), key, tt.payload.EmailCode, 5*time.Minute).Err(); err != nil {
					t.Fatalf("storeEmailCode() error = %v", err)
				}
			}
			reqBody := tt.payload
			bodyBytes, _ := json.Marshal(reqBody)
			req, _ := http.NewRequest("POST", "/register", bytes.NewBuffer(bodyBytes))
			req.Header.Set("Content-Type", "application/json")

			w := httptest.NewRecorder()
			tc.router.ServeHTTP(w, req)

			if w.Code != tt.wantStatus {
				t.Errorf("expected status %d, got %d, body: %s", tt.wantStatus, w.Code, w.Body.String())
				return
			}

			if tt.checkFunc != nil && w.Code == http.StatusOK {
				var resp map[string]interface{}
				_ = json.Unmarshal(w.Body.Bytes(), &resp)
				data := resp["data"].(map[string]interface{})
				tt.checkFunc(t, data)
			}
		})
	}
}

// TestRefreshToken tests token refresh functionality
func TestRefreshToken(t *testing.T) {
	tc := setupTest(t)
	defer tc.cleanup()

	// Create a test user
	fixture := testutil.NewFixtureBuilder(tc.db.DB)
	user := fixture.CreateUser(func(u *model.User) {
		u.Username = "refreshuser"
	})

	familyID := uuid.NewString()
	refreshToken, jti, expiresAt, err := jwtpkg.GenerateRefreshTokenWithFamily(user.ID, familyID, "")
	if err != nil {
		t.Fatalf("GenerateRefreshTokenWithFamily() error = %v", err)
	}
	if err := tc.db.DB.Create(&model.RefreshToken{
		JTI:       jti,
		UserID:    user.ID,
		FamilyID:  familyID,
		Status:    model.RefreshTokenStatusActive,
		ExpiresAt: expiresAt.UTC().Add(1 * time.Second),
	}).Error; err != nil {
		t.Fatalf("seed refresh token error: %v", err)
	}

	tests := []struct {
		name       string
		token      string
		wantStatus int
	}{
		{
			name:       "valid refresh token",
			token:      refreshToken,
			wantStatus: http.StatusOK,
		},
		{
			name:       "invalid refresh token",
			token:      "invalid.token.here",
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:       "empty token",
			token:      "",
			wantStatus: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body, _ := json.Marshal(refreshReq{RefreshToken: tt.token})
			req, _ := http.NewRequest(http.MethodPost, "/refresh", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")

			w := httptest.NewRecorder()
			tc.router.ServeHTTP(w, req)

			if w.Code != tt.wantStatus {
				t.Errorf("expected status %d, got %d, body: %s", tt.wantStatus, w.Code, w.Body.String())
			}
		})
	}
}

// TestLogout tests logout functionality
// Note: This test requires Redis to be initialized
// In production, use dependency injection or mock Redis client
func TestLogout(t *testing.T) {
	tc := setupTest(t)
	defer tc.cleanup()

	// Initialize mock Redis (using a simple in-memory implementation)
	// For real projects, use github.com/alicebob/miniredis
	store.RDB = testutil.NewMockRedis()

	// Create a test user
	fixture := testutil.NewFixtureBuilder(tc.db.DB)
	user := fixture.CreateUser(func(u *model.User) {
		u.Username = "logoutuser"
	})

	// Generate a valid access token
	accessToken, _, _ := jwtpkg.GenerateAccessToken(user.ID)

	tests := []struct {
		name       string
		authToken  string
		deviceID   string
		wantStatus int
	}{
		{
			name:       "logout with device id",
			authToken:  accessToken,
			deviceID:   "device-123",
			wantStatus: http.StatusOK,
		},
		{
			name:       "logout without device id",
			authToken:  accessToken,
			deviceID:   "",
			wantStatus: http.StatusOK,
		},
		// Note: Logout endpoint doesn't require auth in current implementation
		// If auth is required, add middleware to the route
		{
			name:       "logout without auth - still works (no auth middleware)",
			authToken:  "",
			deviceID:   "device-123",
			wantStatus: http.StatusOK, // Current behavior: no auth required
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body, _ := json.Marshal(logoutReq{DeviceID: tt.deviceID})
			req, _ := http.NewRequest(http.MethodPost, "/logout", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			if tt.authToken != "" {
				req.Header.Set("Authorization", tt.authToken)
			}

			w := httptest.NewRecorder()
			tc.router.ServeHTTP(w, req)

			if w.Code != tt.wantStatus {
				t.Errorf("expected status %d, got %d, body: %s", tt.wantStatus, w.Code, w.Body.String())
			}
		})
	}
}
