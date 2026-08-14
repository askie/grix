package service

import (
	"context"
	"testing"
	"time"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/testutil"
	"github.com/askie/grix/backend/internal/security"
	"github.com/askie/grix/backend/internal/store"
)

func TestLoginDeviceSessionLifecycle(t *testing.T) {
	testDB := testutil.NewTestDB()
	defer testDB.Close()

	store.DB = testDB.DB
	store.RDB = testutil.NewMockRedis()

	const (
		userID    int64 = 7701
		sessionID       = "login-device-session-1"
		deviceID        = "ios-device-77"
	)

	if err := RegisterLoginDeviceSession(userID, sessionID, deviceID, "ios"); err != nil {
		t.Fatalf("RegisterLoginDeviceSession() error = %v", err)
	}
	if err := store.RDB.Set(
		context.Background(),
		"im:ws:alive:7701:ios-device-77",
		"1",
		time.Minute,
	).Err(); err != nil {
		t.Fatalf("seed alive route error = %v", err)
	}
	if err := testDB.DB.Create(&model.RefreshToken{
		JTI:       "rt-login-device-session-1",
		UserID:    userID,
		FamilyID:  sessionID,
		Status:    model.RefreshTokenStatusActive,
		ExpiresAt: time.Now().UTC().Add(24 * time.Hour),
	}).Error; err != nil {
		t.Fatalf("seed refresh token error = %v", err)
	}

	items, err := ListLoginDeviceSessions(userID, sessionID)
	if err != nil {
		t.Fatalf("ListLoginDeviceSessions() error = %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 session item, got %d", len(items))
	}
	if !items[0].Online {
		t.Fatal("expected device session to be online")
	}
	if !items[0].Current {
		t.Fatal("expected device session to be current")
	}

	if err := RevokeLoginDeviceSession(userID, sessionID); err != nil {
		t.Fatalf("RevokeLoginDeviceSession() error = %v", err)
	}
	if !security.IsLoginSessionRevoked(userID, sessionID) {
		t.Fatal("expected revoked session marker to be visible")
	}

	var found model.RefreshToken
	if err := testDB.DB.Where("jti = ?", "rt-login-device-session-1").First(&found).Error; err != nil {
		t.Fatalf("load refresh token error = %v", err)
	}
	if found.Status != model.RefreshTokenStatusRevoked {
		t.Fatalf("expected refresh token to be revoked, got %d", found.Status)
	}
}

func TestEnsureLoginDeviceSessionReadyBootstrapsLegacySession(t *testing.T) {
	testDB := testutil.NewTestDB()
	defer testDB.Close()

	store.DB = testDB.DB
	store.RDB = testutil.NewMockRedis()

	const (
		userID    int64 = 9901
		sessionID       = "legacy-login-device-session-1"
		deviceID        = "android-device-99"
		platform        = "android"
	)

	if err := testDB.DB.Create(&model.RefreshToken{
		JTI:       "rt-legacy-login-device-session-1",
		UserID:    userID,
		FamilyID:  sessionID,
		Status:    model.RefreshTokenStatusActive,
		ExpiresAt: time.Now().UTC().Add(24 * time.Hour),
	}).Error; err != nil {
		t.Fatalf("seed refresh token error = %v", err)
	}

	if err := EnsureLoginDeviceSessionReady(userID, sessionID, deviceID, platform); err != nil {
		t.Fatalf("EnsureLoginDeviceSessionReady() error = %v", err)
	}

	var found model.LoginDeviceSession
	if err := testDB.DB.Where("user_id = ? AND session_id = ?", userID, sessionID).First(&found).Error; err != nil {
		t.Fatalf("load login device session error = %v", err)
	}
	if found.DeviceID != deviceID {
		t.Fatalf("expected device_id %q, got %q", deviceID, found.DeviceID)
	}
	if found.Platform != platform {
		t.Fatalf("expected platform %q, got %q", platform, found.Platform)
	}
	if found.RevokedAt != nil {
		t.Fatal("expected bootstrapped login device session to be active")
	}
}

func TestIssueTokenWithDBReplacesActiveSessionOnSameDevice(t *testing.T) {
	testDB := testutil.NewTestDB()
	defer testDB.Close()

	store.DB = testDB.DB
	store.RDB = testutil.NewMockRedis()

	fixture := testutil.NewFixtureBuilder(testDB.DB)
	user := fixture.CreateUser(func(u *model.User) {
		u.ID = 8801
		u.Username = "device_replace_user"
		u.Email = "device_replace_user@example.com"
	})

	const (
		oldSessionID = "login-device-session-old"
		deviceID     = "ios-device-88"
		platform     = "ios"
	)

	if err := RegisterLoginDeviceSession(user.ID, oldSessionID, deviceID, platform); err != nil {
		t.Fatalf("RegisterLoginDeviceSession() error = %v", err)
	}
	if err := testDB.DB.Create(&model.RefreshToken{
		JTI:       "rt-login-device-session-old",
		UserID:    user.ID,
		FamilyID:  oldSessionID,
		Status:    model.RefreshTokenStatusActive,
		ExpiresAt: time.Now().UTC().Add(24 * time.Hour),
	}).Error; err != nil {
		t.Fatalf("seed refresh token error = %v", err)
	}

	resp, err := doIssueToken(*user, deviceID, platform, testAuthLanguage)
	if err != nil {
		t.Fatalf("doIssueToken() error = %v", err)
	}
	newSessionID := assertLoginDeviceSessionCreated(t, testDB, user.ID, resp.AccessToken, deviceID, platform)
	if newSessionID == oldSessionID {
		t.Fatal("expected new login to mint a new session id")
	}

	var activeCount int64
	if err := testDB.DB.Model(&model.LoginDeviceSession{}).
		Where("user_id = ? AND device_id = ? AND revoked_at IS NULL", user.ID, deviceID).
		Count(&activeCount).Error; err != nil {
		t.Fatalf("count active login device sessions error = %v", err)
	}
	if activeCount != 1 {
		t.Fatalf("expected exactly one active session for device, got %d", activeCount)
	}

	var oldSession model.LoginDeviceSession
	if err := testDB.DB.Where("session_id = ?", oldSessionID).First(&oldSession).Error; err != nil {
		t.Fatalf("load old login_device_session error = %v", err)
	}
	if oldSession.RevokedAt == nil {
		t.Fatal("expected old login device session to be revoked")
	}

	var oldRefresh model.RefreshToken
	if err := testDB.DB.Where("jti = ?", "rt-login-device-session-old").First(&oldRefresh).Error; err != nil {
		t.Fatalf("load old refresh token error = %v", err)
	}
	if oldRefresh.Status != model.RefreshTokenStatusRevoked {
		t.Fatalf("expected old refresh token revoked, got %d", oldRefresh.Status)
	}
}
