package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/askie/grix/backend/internal/model"
	jwtpkg "github.com/askie/grix/backend/internal/pkg/jwt"
	"github.com/askie/grix/backend/internal/pkg/middleware"
	"github.com/askie/grix/backend/internal/pkg/snowflake"
	"github.com/askie/grix/backend/internal/pkg/testutil"
	"github.com/askie/grix/backend/internal/store"
	"github.com/askie/grix/backend/internal/systemsetting"
	"github.com/gin-gonic/gin"
)

func setupDeviceHandlerTest(t *testing.T) (*gin.Engine, *testutil.TestDB, string, int64, func()) {
	t.Helper()

	testDB := testutil.NewTestDB()
	store.DB = testDB.DB
	store.RDB = testutil.NewMockRedis()

	jwtpkg.Init("test-secret-key", 3600, 86400)
	_ = snowflake.Init(1)

	fixture := testutil.NewFixtureBuilder(testDB.DB)
	user := fixture.CreateUser(func(u *model.User) {
		u.ID = 12001
		u.Username = "device_handler_user"
		u.Email = "device_handler_user@example.com"
	})
	token, _, err := jwtpkg.GenerateAccessToken(user.ID)
	if err != nil {
		t.Fatalf("GenerateAccessToken() error = %v", err)
	}

	r := gin.New()
	r.Use(middleware.Auth())
	r.POST("/devices/bind", DeviceBind)
	r.GET("/devices/sessions", DeviceSessionList)
	r.DELETE("/devices/sessions/:session_id", DeviceSessionRemove)

	return r, testDB, token, user.ID, func() { testDB.Close() }
}

func TestDeviceBindHandler(t *testing.T) {
	r, testDB, token, userID, cleanup := setupDeviceHandlerTest(t)
	defer cleanup()

	t.Run("missing push_env returns bad request", func(t *testing.T) {
		body, _ := json.Marshal(map[string]any{
			"platform":     model.DevicePlatformIOS,
			"device_token": "ios-token-1",
			"device_id":    "ios-device-1",
		})

		req, _ := http.NewRequest(http.MethodPost, "/devices/bind", bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")

		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("expected status %d, got %d, body=%s", http.StatusBadRequest, w.Code, w.Body.String())
		}
	})

	t.Run("invalid ios push_env returns business validation error", func(t *testing.T) {
		body, _ := json.Marshal(map[string]any{
			"platform":     model.DevicePlatformIOS,
			"push_env":     model.DevicePushEnvDefault,
			"device_token": "ios-token-2",
			"device_id":    "ios-device-2",
		})

		req, _ := http.NewRequest(http.MethodPost, "/devices/bind", bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")

		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("expected status %d, got %d, body=%s", http.StatusBadRequest, w.Code, w.Body.String())
		}

		var resp map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("unmarshal response: %v", err)
		}
		if code, ok := resp["code"].(float64); !ok || int(code) != 10003 {
			t.Fatalf("expected business code 10003, got %#v", resp["code"])
		}
	})

	t.Run("valid bind persists push_env", func(t *testing.T) {
		body, _ := json.Marshal(map[string]any{
			"platform":     model.DevicePlatformIOS,
			"push_env":     model.DevicePushEnvAPNsProduction,
			"device_token": "ios-token-3",
			"device_id":    "ios-device-3",
		})

		req, _ := http.NewRequest(http.MethodPost, "/devices/bind", bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")

		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected status %d, got %d, body=%s", http.StatusOK, w.Code, w.Body.String())
		}

		var device model.Device
		if err := testDB.DB.Where("user_id = ? AND device_id = ?", userID, "ios-device-3").First(&device).Error; err != nil {
			t.Fatalf("load device: %v", err)
		}
		if device.PushEnv != model.DevicePushEnvAPNsProduction {
			t.Fatalf("expected push_env %q, got %q", model.DevicePushEnvAPNsProduction, device.PushEnv)
		}
	})

	t.Run("list device sessions returns current and online session", func(t *testing.T) {
		sessionID := "device-session-1"
		tokenWithSession, _, err := jwtpkg.GenerateAccessTokenWithSession(userID, sessionID)
		if err != nil {
			t.Fatalf("GenerateAccessTokenWithSession() error = %v", err)
		}

		if err := testDB.DB.Create(&model.LoginDeviceSession{
			SessionID:  sessionID,
			UserID:     userID,
			DeviceID:   "ios-device-9",
			Platform:   "ios",
			LastSeenAt: time.Now().UTC(),
		}).Error; err != nil {
			t.Fatalf("seed login_device_session error = %v", err)
		}
		if err := store.RDB.Set(
			context.Background(),
			"im:ws:alive:12001:ios-device-9",
			"1",
			time.Minute,
		).Err(); err != nil {
			t.Fatalf("seed alive route error = %v", err)
		}

		req, _ := http.NewRequest(http.MethodGet, "/devices/sessions", nil)
		req.Header.Set("Authorization", "Bearer "+tokenWithSession)

		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected status %d, got %d, body=%s", http.StatusOK, w.Code, w.Body.String())
		}

		var resp struct {
			Code int `json:"code"`
			Data struct {
				Items []struct {
					SessionID string `json:"session_id"`
					Online    bool   `json:"online"`
					Current   bool   `json:"current"`
				} `json:"items"`
			} `json:"data"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("unmarshal response: %v", err)
		}
		if resp.Code != 0 {
			t.Fatalf("expected code 0, got %d", resp.Code)
		}
		if len(resp.Data.Items) != 1 {
			t.Fatalf("expected 1 item, got %d", len(resp.Data.Items))
		}
		if !resp.Data.Items[0].Online {
			t.Fatal("expected listed device session to be online")
		}
		if !resp.Data.Items[0].Current {
			t.Fatal("expected listed device session to be current")
		}
	})

	t.Run("remove device session revokes session record", func(t *testing.T) {
		sessionID := "device-session-remove"
		if err := testDB.DB.Create(&model.LoginDeviceSession{
			SessionID:  sessionID,
			UserID:     userID,
			DeviceID:   "ios-device-remove",
			Platform:   "ios",
			LastSeenAt: time.Now().UTC(),
		}).Error; err != nil {
			t.Fatalf("seed login_device_session error = %v", err)
		}
		if err := testDB.DB.Create(&model.RefreshToken{
			JTI:       "rt-device-session-remove",
			UserID:    userID,
			FamilyID:  sessionID,
			Status:    model.RefreshTokenStatusActive,
			ExpiresAt: time.Now().UTC().Add(24 * time.Hour),
		}).Error; err != nil {
			t.Fatalf("seed refresh token error = %v", err)
		}

		req, _ := http.NewRequest(
			http.MethodDelete,
			"/devices/sessions/"+sessionID,
			nil,
		)
		req.Header.Set("Authorization", "Bearer "+token)

		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected status %d, got %d, body=%s", http.StatusOK, w.Code, w.Body.String())
		}

		var found model.LoginDeviceSession
		if err := testDB.DB.Where("session_id = ?", sessionID).First(&found).Error; err != nil {
			t.Fatalf("load login_device_session error = %v", err)
		}
		if found.RevokedAt == nil {
			t.Fatal("expected login device session to be revoked")
		}
	})
}

// 409 + 10012 + data.disabled_platform 是客户端切换到下一条推送通道的唯一依据，
// 少一项客户端就只能当成普通失败，设备停在收不到推送的通道上。
func TestDeviceBindHandlerRejectsDisabledChannel(t *testing.T) {
	r, _, token, _, cleanup := setupDeviceHandlerTest(t)
	defer cleanup()

	defer func() {
		if err := systemsetting.SavePushChannelSettings(systemsetting.DefaultPushChannelSettings(), nil); err != nil {
			t.Fatalf("restore push channel settings error = %v", err)
		}
	}()

	disabled := systemsetting.DefaultPushChannelSettings()
	disabled.AndroidFCMEnabled = false
	if err := systemsetting.SavePushChannelSettings(disabled, nil); err != nil {
		t.Fatalf("SavePushChannelSettings() error = %v", err)
	}

	body, _ := json.Marshal(map[string]any{
		"platform":     model.DevicePlatformAndroidFCM,
		"push_env":     model.DevicePushEnvDefault,
		"device_token": "fcm-token-disabled",
		"device_id":    "android-device-disabled",
	})
	req := httptest.NewRequest(http.MethodPost, "/devices/bind", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusConflict {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusConflict)
	}

	var resp struct {
		Code int `json:"code"`
		Data struct {
			DisabledPlatform string `json:"disabled_platform"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response error = %v", err)
	}
	if resp.Code != 10012 {
		t.Fatalf("code = %d, want 10012", resp.Code)
	}
	if resp.Data.DisabledPlatform != model.DevicePlatformAndroidFCM {
		t.Fatalf("disabled_platform = %q, want %q", resp.Data.DisabledPlatform, model.DevicePlatformAndroidFCM)
	}
}
