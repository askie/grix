package service

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/askie/grix/backend/config"
	"github.com/askie/grix/backend/internal/featuregate"
	"github.com/askie/grix/backend/internal/model"
	jwtpkg "github.com/askie/grix/backend/internal/pkg/jwt"
	"github.com/askie/grix/backend/internal/pkg/snowflake"
	"github.com/askie/grix/backend/internal/pkg/testutil"
	"github.com/askie/grix/backend/internal/security"
	"github.com/askie/grix/backend/internal/store"
	"github.com/askie/grix/backend/internal/systemsetting"
	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
)

func init() {
	_ = snowflake.Init(1)
	jwtpkg.Init("test-secret-for-auth-service", 3600, 86400)
}

func setupAuthTest(t *testing.T) (*testutil.TestDB, func()) {
	t.Helper()
	testDB := testutil.NewTestDB()
	store.DB = testDB.DB
	store.RDB = testutil.NewMockRedis()
	systemsetting.InvalidateAuthSettingsCache()
	featuregate.InvalidateCache()
	seedAuthFeatureGates(t)

	originalRunner := registerWelcomeCompensationRunner
	registerWelcomeCompensationRunner = func(registerUserID, customerUserID int64) {
		_, _ = processRegisterWelcomeCompensationByTarget(registerUserID, customerUserID)
	}

	return testDB, func() {
		registerWelcomeCompensationRunner = originalRunner
		systemsetting.InvalidateAuthSettingsCache()
		featuregate.InvalidateCache()
		testDB.Close()
	}
}

func seedAuthFeatureGates(t *testing.T) {
	t.Helper()
	gates := []model.FeatureGate{
		{Key: "auth_register", DisplayName: "允许注册", Status: model.FeatureStatusEnabled},
		{Key: "auth_google_login", DisplayName: "允许 Google 登录", Status: model.FeatureStatusEnabled},
		{Key: "auth_apple_login", DisplayName: "允许 Apple 登录", Status: model.FeatureStatusEnabled},
	}
	for _, g := range gates {
		gate := g
		if err := store.DB.FirstOrCreate(&gate, "key = ?", gate.Key).Error; err != nil {
			t.Fatalf("seed feature gate %s error: %v", gate.Key, err)
		}
	}
}

func mustSeedEmailCode(t *testing.T, email, scene, code string) {
	t.Helper()
	if err := storeEmailCode(email, scene, code); err != nil {
		t.Fatalf("storeEmailCode() error = %v", err)
	}
}

const (
	testAuthDeviceID = "test-auth-device"
	testAuthPlatform = "ios"
	testAuthLanguage = "zh-CN"
)

func setGoogleTokenValidatorForTest(
	t *testing.T,
	fn func(context.Context, string, []string) (googleTokenProfile, error),
) {
	t.Helper()
	original := validateGoogleIDToken
	validateGoogleIDToken = fn
	t.Cleanup(func() {
		validateGoogleIDToken = original
	})
}

func setGoogleAllowedClientIDsForTest(t *testing.T, raw string) {
	t.Helper()
	original := config.C.OAuth.GoogleAllowedClientIDs
	config.C.OAuth.GoogleAllowedClientIDs = raw
	t.Cleanup(func() {
		config.C.OAuth.GoogleAllowedClientIDs = original
	})
}

func assertLoginDeviceSessionCreated(
	t *testing.T,
	testDB *testutil.TestDB,
	userID int64,
	accessToken, deviceID, platform string,
) string {
	t.Helper()

	claims, err := jwtpkg.ValidateAccessToken(accessToken)
	if err != nil {
		t.Fatalf("ValidateAccessToken() error = %v", err)
	}
	if claims.SessionID == "" {
		t.Fatal("expected access token to carry session_id")
	}

	var session model.LoginDeviceSession
	if err := testDB.DB.Where("session_id = ?", claims.SessionID).First(&session).Error; err != nil {
		t.Fatalf("load login_device_session error = %v", err)
	}
	if session.UserID != userID {
		t.Fatalf("expected session user_id=%d, got %d", userID, session.UserID)
	}
	if session.DeviceID != deviceID {
		t.Fatalf("expected session device_id=%q, got %q", deviceID, session.DeviceID)
	}
	if session.Platform != platform {
		t.Fatalf("expected session platform=%q, got %q", platform, session.Platform)
	}
	if session.RevokedAt != nil {
		t.Fatal("expected session to stay active")
	}

	return claims.SessionID
}

func TestLogin(t *testing.T) {
	testDB, cleanup := setupAuthTest(t)
	defer cleanup()

	fixture := testutil.NewFixtureBuilder(testDB.DB)

	t.Run("register new user", func(t *testing.T) {
		const code = "654321"
		mustSeedEmailCode(t, "newuser@example.com", "register", code)
		resp, err := Register("newuser@example.com", "password123", code, testAuthDeviceID, testAuthPlatform, testAuthLanguage, "")
		if err != nil {
			t.Fatalf("Register() error = %v", err)
		}

		if resp.AccessToken == "" {
			t.Error("expected access token")
		}
		if resp.RefreshToken == "" {
			t.Error("expected refresh token")
		}
		if resp.ExpiresIn <= 0 {
			t.Error("expected positive expires_in")
		}
		if resp.User.Email != "newuser@example.com" {
			t.Errorf("expected email 'newuser@example.com', got %s", resp.User.Email)
		}
		if resp.User.Username != "newuser" {
			t.Errorf("expected username 'newuser', got %s", resp.User.Username)
		}
		if resp.User.Nickname != "newuser" {
			t.Errorf("expected nickname 'newuser', got %s", resp.User.Nickname)
		}
		assertLoginDeviceSessionCreated(t, testDB, resp.User.ID, resp.AccessToken, testAuthDeviceID, testAuthPlatform)
		assertUserDefaultFriendAddSetting(t, testDB, resp.User.ID)
	})

	t.Run("register duplicate local part appends shortest random digits", func(t *testing.T) {
		fixture.CreateUser(func(u *model.User) {
			u.Username = "alice"
			u.Nickname = "alice"
			u.Email = "existing-alice@example.com"
		})

		const code = "654321"
		mustSeedEmailCode(t, "alice@example.com", "register", code)
		resp, err := Register("alice@example.com", "password123", code, testAuthDeviceID, testAuthPlatform, testAuthLanguage, "")
		if err != nil {
			t.Fatalf("Register() error = %v", err)
		}

		matchedUsername, _ := regexp.MatchString(`^alice\d$`, resp.User.Username)
		if !matchedUsername {
			t.Errorf("expected username to match ^alice\\d$, got %s", resp.User.Username)
		}
		matchedNickname, _ := regexp.MatchString(`^alice\d$`, resp.User.Nickname)
		if !matchedNickname {
			t.Errorf("expected nickname to match ^alice\\d$, got %s", resp.User.Nickname)
		}
	})

	t.Run("register escalates to two digits when one-digit names are exhausted", func(t *testing.T) {
		fixture.CreateUser(func(u *model.User) {
			u.Username = "tom"
			u.Nickname = "tom"
			u.Email = "tom-base@example.com"
		})
		for i := 0; i < 10; i++ {
			name := "tom" + string(rune('0'+i))
			fixture.CreateUser(func(u *model.User) {
				u.Username = name
				u.Nickname = name
				u.Email = "tom-" + name + "@example.com"
			})
		}

		const code = "654321"
		mustSeedEmailCode(t, "tom@example.com", "register", code)
		resp, err := Register("tom@example.com", "password123", code, testAuthDeviceID, testAuthPlatform, testAuthLanguage, "")
		if err != nil {
			t.Fatalf("Register() error = %v", err)
		}

		matchedUsername, _ := regexp.MatchString(`^tom\d{2}$`, resp.User.Username)
		if !matchedUsername {
			t.Errorf("expected username to match ^tom\\d{2}$, got %s", resp.User.Username)
		}
		matchedNickname, _ := regexp.MatchString(`^tom\d{2}$`, resp.User.Nickname)
		if !matchedNickname {
			t.Errorf("expected nickname to match ^tom\\d{2}$, got %s", resp.User.Nickname)
		}
	})

	t.Run("login existing user with correct password", func(t *testing.T) {
		// Create user with known password
		hash, _ := bcrypt.GenerateFromPassword([]byte("correctpassword"), bcrypt.DefaultCost)
		fixture.CreateUser(func(u *model.User) {
			u.Email = "existinguser@example.com"
			u.PasswordHash = string(hash)
		})

		resp, err := Login("existinguser@example.com", "correctpassword", testAuthDeviceID, testAuthPlatform, testAuthLanguage)
		if err != nil {
			t.Fatalf("Login() error = %v", err)
		}

		if resp.AccessToken == "" {
			t.Error("expected access token")
		}
		assertLoginDeviceSessionCreated(t, testDB, resp.User.ID, resp.AccessToken, testAuthDeviceID, testAuthPlatform)
	})

	t.Run("login existing user with wrong password", func(t *testing.T) {
		// Create user with known password
		hash, _ := bcrypt.GenerateFromPassword([]byte("rightpassword"), bcrypt.DefaultCost)
		fixture.CreateUser(func(u *model.User) {
			u.Email = "wrongpassuser@example.com"
			u.PasswordHash = string(hash)
		})

		_, err := Login("wrongpassuser@example.com", "wrongpassword", testAuthDeviceID, testAuthPlatform, testAuthLanguage)
		if err == nil {
			t.Error("expected error for wrong password")
		}
		if err.Error() != "用户不存在或密码错误" {
			t.Errorf("expected '用户不存在或密码错误', got '%s'", err.Error())
		}
	})

	t.Run("lock account after five failed attempts across email and username", func(t *testing.T) {
		hash, _ := bcrypt.GenerateFromPassword([]byte("correctpassword"), bcrypt.DefaultCost)
		user := fixture.CreateUser(func(u *model.User) {
			u.Username = "lockuser"
			u.Email = "lockuser@example.com"
			u.PasswordHash = string(hash)
		})

		for i := 0; i < 4; i++ {
			_, err := Login("lockuser@example.com", "wrongpassword", testAuthDeviceID, testAuthPlatform, testAuthLanguage)
			if err == nil || err.Error() != "用户不存在或密码错误" {
				t.Fatalf("attempt %d expected invalid credentials, got %v", i+1, err)
			}
		}

		_, err := Login("lockuser", "wrongpassword", testAuthDeviceID, testAuthPlatform, testAuthLanguage)
		if err == nil || !strings.Contains(err.Error(), "账号已被锁定") || !strings.Contains(err.Error(), "1小时") {
			t.Fatalf("expected account locked error on fifth attempt, got %v", err)
		}

		userGuard := security.NewUserLoginGuardByUserID(user.ID)
		locked, remaining, err := userGuard.IsLocked(context.Background())
		if err != nil {
			t.Fatalf("IsLocked() error = %v", err)
		}
		if !locked {
			t.Fatal("expected user to be locked")
		}
		if remaining < security.LockoutDuration-time.Minute || remaining > security.LockoutDuration {
			t.Fatalf("expected first lock duration close to %s, got %s", security.LockoutDuration, remaining)
		}

		_, err = Login("lockuser@example.com", "correctpassword", testAuthDeviceID, testAuthPlatform, testAuthLanguage)
		if err == nil || !strings.Contains(err.Error(), "账号已被锁定") {
			t.Fatalf("expected locked account to reject correct password, got %v", err)
		}

		if err := userGuard.ClearLock(context.Background()); err != nil {
			t.Fatalf("ClearLock() error = %v", err)
		}

		for i := 0; i < 4; i++ {
			_, err := Login("lockuser@example.com", "wrongpassword", testAuthDeviceID, testAuthPlatform, testAuthLanguage)
			if err == nil || err.Error() != "用户不存在或密码错误" {
				t.Fatalf("relock attempt %d expected invalid credentials, got %v", i+1, err)
			}
		}

		_, err = Login("lockuser", "wrongpassword", testAuthDeviceID, testAuthPlatform, testAuthLanguage)
		if err == nil || !strings.Contains(err.Error(), "账号已被锁定") || !strings.Contains(err.Error(), "12小时") {
			t.Fatalf("expected second lock error with 12h duration, got %v", err)
		}

		locked, remaining, err = userGuard.IsLocked(context.Background())
		if err != nil {
			t.Fatalf("IsLocked() after relock error = %v", err)
		}
		if !locked {
			t.Fatal("expected user to be locked after relock")
		}
		if remaining < security.RepeatedLockDuration-time.Minute || remaining > security.RepeatedLockDuration {
			t.Fatalf("expected repeated lock duration close to %s, got %s", security.RepeatedLockDuration, remaining)
		}
	})

	t.Run("successful login clears failed attempts", func(t *testing.T) {
		hash, _ := bcrypt.GenerateFromPassword([]byte("clearpassword"), bcrypt.DefaultCost)
		user := fixture.CreateUser(func(u *model.User) {
			u.Username = "clearuser"
			u.Email = "clearuser@example.com"
			u.PasswordHash = string(hash)
		})

		for i := 0; i < 2; i++ {
			_, err := Login("clearuser@example.com", "wrongpassword", testAuthDeviceID, testAuthPlatform, testAuthLanguage)
			if err == nil || err.Error() != "用户不存在或密码错误" {
				t.Fatalf("attempt %d expected invalid credentials, got %v", i+1, err)
			}
		}

		userGuard := security.NewUserLoginGuardByUserID(user.ID)
		remaining, err := userGuard.GetRemainingAttempts(context.Background())
		if err != nil {
			t.Fatalf("GetRemainingAttempts() error = %v", err)
		}
		if remaining != security.MaxLoginAttempts-2 {
			t.Fatalf("expected %d remaining attempts before success, got %d", security.MaxLoginAttempts-2, remaining)
		}

		if _, err := Login("clearuser", "clearpassword", testAuthDeviceID, testAuthPlatform, testAuthLanguage); err != nil {
			t.Fatalf("expected successful login, got %v", err)
		}

		remaining, err = userGuard.GetRemainingAttempts(context.Background())
		if err != nil {
			t.Fatalf("GetRemainingAttempts() after success error = %v", err)
		}
		if remaining != security.MaxLoginAttempts {
			t.Fatalf("expected remaining attempts reset to %d, got %d", security.MaxLoginAttempts, remaining)
		}
	})

	t.Run("tokens are valid", func(t *testing.T) {
		const code = "654321"
		mustSeedEmailCode(t, "tokentest@example.com", "register", code)
		resp, err := Register("tokentest@example.com", "password123", code, testAuthDeviceID, testAuthPlatform, testAuthLanguage, "")
		if err != nil {
			t.Fatalf("Register error: %v", err)
		}

		// Verify access token
		claims, err := jwtpkg.ValidateAccessToken(resp.AccessToken)
		if err != nil {
			t.Errorf("access token invalid: %v", err)
		}
		if claims.UserID != resp.User.ID {
			t.Errorf("token user_id mismatch")
		}

		// Verify refresh token
		refreshClaims, err := jwtpkg.ValidateRefreshToken(resp.RefreshToken)
		if err != nil {
			t.Errorf("refresh token invalid: %v", err)
		}
		if refreshClaims.UserID != resp.User.ID {
			t.Errorf("refresh token user_id mismatch")
		}
	})

}

func TestRegisterAutoAddConfiguredCustomerFriend(t *testing.T) {
	testDB, cleanup := setupAuthTest(t)
	defer cleanup()

	fixture := testutil.NewFixtureBuilder(testDB.DB)
	customer := fixture.CreateUser(func(u *model.User) {
		u.Username = "system_customer"
		u.Email = "system_customer@example.com"
		u.Nickname = "System Customer"
		u.Status = model.UserStatusActive
	})

	settings := systemsetting.DefaultAuthSettings()
	settings.AutoAddCustomerUserID = customer.ID
	if err := systemsetting.SaveAuthSettings(settings, nil); err != nil {
		t.Fatalf("SaveAuthSettings() error = %v", err)
	}
	customerAutoAgentID := int64(9101)
	if err := testDB.DB.Create(&model.Agent{
		ID:        customerAutoAgentID,
		AgentName: "register-welcome-customer",
		OwnerID:   customer.ID,
		Status:    model.AgentStatusActive,
	}).Error; err != nil {
		t.Fatalf("seed customer auto delegate agent error: %v", err)
	}
	rawCustomerAutoAgentID := fmt.Sprintf("%d", customerAutoAgentID)
	if _, err := UpdateUserSettings(customer.ID, UserSettingsUpdateReq{
		Chat: &UserSettingsChatUpdateReq{
			AutoDelegateAgentID: OptionalStringUpdate{
				Set:   true,
				Value: &rawCustomerAutoAgentID,
			},
		},
	}); err != nil {
		t.Fatalf("set customer auto delegate agent error: %v", err)
	}

	const email = "autofriend@example.com"
	const code = "654321"
	mustSeedEmailCode(t, email, "register", code)

	resp, err := Register(email, "password123", code, testAuthDeviceID, testAuthPlatform, testAuthLanguage, "")
	if err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	var count int64
	if err := testDB.DB.Model(&model.Friend{}).
		Where("user_id = ? AND friend_id = ?", resp.User.ID, customer.ID).
		Count(&count).Error; err != nil {
		t.Fatalf("query friend relation error: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected new user -> customer friend relation, got count=%d", count)
	}

	if err := testDB.DB.Model(&model.Friend{}).
		Where("user_id = ? AND friend_id = ?", customer.ID, resp.User.ID).
		Count(&count).Error; err != nil {
		t.Fatalf("query reverse friend relation error: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected customer -> new user friend relation, got count=%d", count)
	}

	assertConfiguredCustomerWelcomeConversation(t, testDB, customer.ID, resp.User.ID)
	assertConfiguredCustomerWelcomeAutoDelegate(t, testDB, customer.ID, resp.User.ID, customerAutoAgentID)
	assertUserPreferredLanguage(t, testDB, resp.User.ID, preferredLanguageZH)
}

func TestRegisterPersistsPreferredLanguageAndUsesEnglishWelcome(t *testing.T) {
	testDB, cleanup := setupAuthTest(t)
	defer cleanup()

	fixture := testutil.NewFixtureBuilder(testDB.DB)
	customer := fixture.CreateUser(func(u *model.User) {
		u.Username = "english_customer"
		u.Email = "english_customer@example.com"
		u.Nickname = "English Customer"
		u.Status = model.UserStatusActive
	})

	settings := systemsetting.DefaultAuthSettings()
	settings.AutoAddCustomerUserID = customer.ID
	if err := systemsetting.SaveAuthSettings(settings, nil); err != nil {
		t.Fatalf("SaveAuthSettings() error = %v", err)
	}

	const email = "english-register@example.com"
	const code = "654321"
	mustSeedEmailCode(t, email, "register", code)

	resp, err := Register(email, "password123", code, testAuthDeviceID, testAuthPlatform, "en-US", "")
	if err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	assertUserPreferredLanguage(t, testDB, resp.User.ID, preferredLanguageEN)
	assertConfiguredCustomerWelcomeConversationWithExpectedContent(
		t,
		testDB,
		customer.ID,
		resp.User.ID,
		"official Grix support agent",
	)
}

func TestRegisterWelcomeCompensationCanRecoverFromPendingJob(t *testing.T) {
	testDB, cleanup := setupAuthTest(t)
	defer cleanup()

	fixture := testutil.NewFixtureBuilder(testDB.DB)
	customer := fixture.CreateUser(func(u *model.User) {
		u.Username = "recover_customer"
		u.Email = "recover_customer@example.com"
		u.Nickname = "Recover Customer"
		u.Status = model.UserStatusActive
	})

	settings := systemsetting.DefaultAuthSettings()
	settings.AutoAddCustomerUserID = customer.ID
	if err := systemsetting.SaveAuthSettings(settings, nil); err != nil {
		t.Fatalf("SaveAuthSettings() error = %v", err)
	}

	originalRunner := registerWelcomeCompensationRunner
	registerWelcomeCompensationRunner = nil
	defer func() {
		registerWelcomeCompensationRunner = originalRunner
	}()

	const email = "recover_pending_job@example.com"
	const code = "654321"
	mustSeedEmailCode(t, email, "register", code)

	resp, err := Register(email, "password123", code, testAuthDeviceID, testAuthPlatform, testAuthLanguage, "")
	if err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	var job model.RegisterWelcomeCompensation
	if err := testDB.DB.
		Where("register_user_id = ? AND customer_user_id = ?", resp.User.ID, customer.ID).
		First(&job).Error; err != nil {
		t.Fatalf("query compensation job error: %v", err)
	}
	if job.Status != model.RegisterWelcomeCompensationStatusPending {
		t.Fatalf("expected pending compensation job, got status=%d", job.Status)
	}

	processed, err := processRegisterWelcomeCompensationDueBatch(8)
	if err != nil {
		t.Fatalf("process compensation batch error: %v", err)
	}
	if processed < 1 {
		t.Fatalf("expected at least one processed compensation job, got %d", processed)
	}

	if err := testDB.DB.First(&job, job.ID).Error; err != nil {
		t.Fatalf("reload compensation job error: %v", err)
	}
	if job.Status != model.RegisterWelcomeCompensationStatusDone {
		t.Fatalf("expected compensation job done, got status=%d", job.Status)
	}

	assertConfiguredCustomerWelcomeConversation(t, testDB, customer.ID, resp.User.ID)
}

func TestRegisterFailsWhenConfiguredCustomerDisabled(t *testing.T) {
	testDB, cleanup := setupAuthTest(t)
	defer cleanup()

	fixture := testutil.NewFixtureBuilder(testDB.DB)
	customer := fixture.CreateUser(func(u *model.User) {
		u.Username = "disabled_customer"
		u.Email = "disabled_customer@example.com"
		u.Nickname = "Disabled Customer"
		u.Status = model.UserStatusBanned
	})

	settings := systemsetting.DefaultAuthSettings()
	settings.AutoAddCustomerUserID = customer.ID
	if err := systemsetting.SaveAuthSettings(settings, nil); err != nil {
		t.Fatalf("SaveAuthSettings() error = %v", err)
	}

	const email = "blocked_register@example.com"
	const code = "654321"
	mustSeedEmailCode(t, email, "register", code)

	_, err := Register(email, "password123", code, testAuthDeviceID, testAuthPlatform, testAuthLanguage, "")
	if err == nil {
		t.Fatal("expected Register() to fail")
	}
	if !strings.Contains(err.Error(), "系统客户账户已禁用") {
		t.Fatalf("expected disabled customer error, got %v", err)
	}

	var userCount int64
	if err := testDB.DB.Model(&model.User{}).Where("email = ?", email).Count(&userCount).Error; err != nil {
		t.Fatalf("query user count error: %v", err)
	}
	if userCount != 0 {
		t.Fatalf("expected register rollback, got user count=%d", userCount)
	}
	if err := testDB.DB.Model(&model.UserSetting{}).
		Count(&userCount).Error; err != nil {
		t.Fatalf("query user_settings count error: %v", err)
	}
	if userCount != 0 {
		t.Fatalf("expected register rollback with no user_settings, got count=%d", userCount)
	}
}

func TestLoginWithGoogleAutoAddConfiguredCustomerFriend(t *testing.T) {
	testDB, cleanup := setupAuthTest(t)
	defer cleanup()
	setGoogleAllowedClientIDsForTest(t, "test-google-client-id")
	setGoogleTokenValidatorForTest(t, func(ctx context.Context, idToken string, audiences []string) (googleTokenProfile, error) {
		if idToken != "test-google-token" {
			return googleTokenProfile{}, errGoogleLoginVerifyFailed
		}
		return googleTokenProfile{
			Email:         "test@google.com",
			Name:          "Google Test User",
			Subject:       "mock_google_id_12345",
			EmailVerified: true,
		}, nil
	})

	fixture := testutil.NewFixtureBuilder(testDB.DB)
	customer := fixture.CreateUser(func(u *model.User) {
		u.Username = "google_customer"
		u.Email = "google_customer@example.com"
		u.Nickname = "Google Customer"
		u.Status = model.UserStatusActive
	})

	settings := systemsetting.DefaultAuthSettings()
	settings.AutoAddCustomerUserID = customer.ID
	if err := systemsetting.SaveAuthSettings(settings, nil); err != nil {
		t.Fatalf("SaveAuthSettings() error = %v", err)
	}

	resp, err := LoginWithGoogle("test-google-token", testAuthDeviceID, testAuthPlatform, testAuthLanguage)
	if err != nil {
		t.Fatalf("LoginWithGoogle() error = %v", err)
	}

	var count int64
	if err := testDB.DB.Model(&model.Friend{}).
		Where("user_id = ? AND friend_id = ?", resp.User.ID, customer.ID).
		Count(&count).Error; err != nil {
		t.Fatalf("query friend relation error: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected google user -> customer friend relation, got count=%d", count)
	}

	if err := testDB.DB.Model(&model.Friend{}).
		Where("user_id = ? AND friend_id = ?", customer.ID, resp.User.ID).
		Count(&count).Error; err != nil {
		t.Fatalf("query reverse friend relation error: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected customer -> google user friend relation, got count=%d", count)
	}

	assertUserDefaultFriendAddSetting(t, testDB, resp.User.ID)
	assertLoginDeviceSessionCreated(t, testDB, resp.User.ID, resp.AccessToken, testAuthDeviceID, testAuthPlatform)
	assertConfiguredCustomerWelcomeConversation(t, testDB, customer.ID, resp.User.ID)
	assertUserPreferredLanguage(t, testDB, resp.User.ID, preferredLanguageZH)
}

func TestLoginUpdatesPreferredLanguage(t *testing.T) {
	testDB, cleanup := setupAuthTest(t)
	defer cleanup()

	hash, _ := bcrypt.GenerateFromPassword([]byte("correctpassword"), bcrypt.DefaultCost)
	fixture := testutil.NewFixtureBuilder(testDB.DB)
	user := fixture.CreateUser(func(u *model.User) {
		u.Email = "language-login@example.com"
		u.Username = "language_login"
		u.PasswordHash = string(hash)
	})

	if err := testDB.DB.Create(&model.UserSetting{
		UserID:            user.ID,
		PreferredLanguage: preferredLanguageZH,
		FriendAddSetting:  model.FriendAddSettingNeedApproval,
		AllowGroupInvite:  true,
	}).Error; err != nil {
		t.Fatalf("seed user setting error: %v", err)
	}

	if _, err := Login("language-login@example.com", "correctpassword", testAuthDeviceID, testAuthPlatform, "en-US"); err != nil {
		t.Fatalf("Login() error = %v", err)
	}

	assertUserPreferredLanguage(t, testDB, user.ID, preferredLanguageEN)
}

func TestLoginWithGoogleRejectsUnverifiedEmail(t *testing.T) {
	testDB, cleanup := setupAuthTest(t)
	defer cleanup()
	setGoogleAllowedClientIDsForTest(t, "test-google-client-id")
	setGoogleTokenValidatorForTest(t, func(ctx context.Context, idToken string, audiences []string) (googleTokenProfile, error) {
		return googleTokenProfile{
			Email:         "test@google.com",
			Name:          "Google Test User",
			Subject:       "mock_google_id_12345",
			EmailVerified: false,
		}, nil
	})

	resp, err := LoginWithGoogle("test-google-token", testAuthDeviceID, testAuthPlatform, testAuthLanguage)
	if err == nil {
		t.Fatalf("expected LoginWithGoogle() to fail, got resp=%+v", resp)
	}
	if !errors.Is(err, errGoogleEmailUnverified) {
		t.Fatalf("expected errGoogleEmailUnverified, got %v", err)
	}
	if testDB.DB == nil {
		t.Fatal("expected test database to stay available")
	}
}

func assertUserDefaultFriendAddSetting(t *testing.T, testDB *testutil.TestDB, userID int64) {
	t.Helper()

	var setting model.UserSetting
	if err := testDB.DB.Where("user_id = ?", userID).First(&setting).Error; err != nil {
		t.Fatalf("query user setting error: %v", err)
	}
	if setting.FriendAddSetting != model.FriendAddSettingNeedApproval {
		t.Fatalf(
			"expected friend_add_setting=%d, got %d",
			model.FriendAddSettingNeedApproval,
			setting.FriendAddSetting,
		)
	}
	if !setting.AllowGroupInvite {
		t.Fatal("expected allow_group_invite=true")
	}
}

func assertUserPreferredLanguage(t *testing.T, testDB *testutil.TestDB, userID int64, expected string) {
	t.Helper()

	var setting model.UserSetting
	if err := testDB.DB.Where("user_id = ?", userID).First(&setting).Error; err != nil {
		t.Fatalf("query user setting for preferred language error: %v", err)
	}
	if setting.PreferredLanguage != expected {
		t.Fatalf("expected preferred_language=%q, got %q", expected, setting.PreferredLanguage)
	}
}

func assertConfiguredCustomerWelcomeConversation(t *testing.T, testDB *testutil.TestDB, customerID, registerUserID int64) {
	t.Helper()
	assertConfiguredCustomerWelcomeConversationWithExpectedContent(
		t,
		testDB,
		customerID,
		registerUserID,
		"Grix 官方客服",
	)
}

func assertConfiguredCustomerWelcomeConversationWithExpectedContent(
	t *testing.T,
	testDB *testutil.TestDB,
	customerID, registerUserID int64,
	expectedContent string,
) {
	t.Helper()

	directKey := buildDirectKey(customerID, registerUserID, 1)
	var session model.Session
	if err := testDB.DB.
		Where("direct_key = ? AND session_type = ? AND is_deleted = ?", directKey, 1, false).
		Order("updated_at DESC").
		First(&session).Error; err != nil {
		t.Fatalf("query welcome session error: %v", err)
	}

	var msg model.Message
	if err := testDB.DB.
		Where("session_id = ?", session.SessionID).
		Order("created_at ASC, msg_id ASC").
		First(&msg).Error; err != nil {
		t.Fatalf("query welcome message error: %v", err)
	}
	if msg.SenderID != customerID {
		t.Fatalf("expected welcome sender=%d, got %d", customerID, msg.SenderID)
	}
	if msg.SenderType != 1 {
		t.Fatalf("expected welcome sender_type=1, got %d", msg.SenderType)
	}
	if msg.MsgType != 1 {
		t.Fatalf("expected welcome msg_type=1, got %d", msg.MsgType)
	}
	if !strings.Contains(msg.Content, expectedContent) {
		t.Fatalf("expected welcome message includes %q, got %q", expectedContent, msg.Content)
	}

	var count int64
	if err := testDB.DB.Model(&model.UserInbox{}).
		Where("user_id = ? AND session_id = ? AND msg_id = ?", customerID, session.SessionID, msg.MsgID).
		Count(&count).Error; err != nil {
		t.Fatalf("query customer inbox error: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected customer inbox has welcome message, got count=%d", count)
	}

	if err := testDB.DB.Model(&model.UserInbox{}).
		Where("user_id = ? AND session_id = ? AND msg_id = ?", registerUserID, session.SessionID, msg.MsgID).
		Count(&count).Error; err != nil {
		t.Fatalf("query register user inbox error: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected register user inbox has welcome message, got count=%d", count)
	}

	var registerMember model.SessionMember
	if err := testDB.DB.
		Where("session_id = ? AND member_id = ? AND member_type = 1", session.SessionID, registerUserID).
		First(&registerMember).Error; err != nil {
		t.Fatalf("query register member error: %v", err)
	}
	if registerMember.UnreadCount < 1 {
		t.Fatalf("expected register user unread_count >= 1, got %d", registerMember.UnreadCount)
	}
}

func assertConfiguredCustomerWelcomeAutoDelegate(
	t *testing.T,
	testDB *testutil.TestDB,
	customerID, registerUserID, customerAutoAgentID int64,
) {
	t.Helper()

	directKey := buildDirectKey(customerID, registerUserID, 1)
	var session model.Session
	if err := testDB.DB.
		Where("direct_key = ? AND session_type = ? AND is_deleted = ?", directKey, 1, false).
		Order("updated_at DESC").
		First(&session).Error; err != nil {
		t.Fatalf("query welcome session for auto delegate error: %v", err)
	}

	ctx := context.Background()
	delegateKey := fmt.Sprintf("im:delegate:%s:%d", session.SessionID, customerID)
	agentIDInRedis, err := store.RDB.HGet(ctx, delegateKey, "agent_id").Result()
	if err != nil {
		t.Fatalf("read customer auto delegate agent_id from redis error: %v", err)
	}
	if agentIDInRedis != fmt.Sprintf("%d", customerAutoAgentID) {
		t.Fatalf(
			"unexpected customer auto delegate agent_id, got=%s want=%d",
			agentIDInRedis,
			customerAutoAgentID,
		)
	}

	maxRounds, err := store.RDB.HGet(ctx, delegateKey, "max_consecutive_replies").Int()
	if err != nil {
		t.Fatalf("read customer auto delegate max_consecutive_replies from redis error: %v", err)
	}
	if maxRounds != autoDelegateDefaultMaxConsecutiveReplies {
		t.Fatalf(
			"unexpected customer auto delegate max_consecutive_replies, got=%d want=%d",
			maxRounds,
			autoDelegateDefaultMaxConsecutiveReplies,
		)
	}

	var log model.DelegationLog
	if err := testDB.DB.
		Where("session_id = ? AND user_id = ? AND action = ?", session.SessionID, customerID, "auto_start").
		First(&log).Error; err != nil {
		t.Fatalf("query customer auto_start delegation log error: %v", err)
	}
	if log.AgentID != customerAutoAgentID {
		t.Fatalf("unexpected customer delegation log agent_id=%d want=%d", log.AgentID, customerAutoAgentID)
	}
}

func TestRefreshToken(t *testing.T) {
	testDB, cleanup := setupAuthTest(t)
	defer cleanup()

	fixture := testutil.NewFixtureBuilder(testDB.DB)

	t.Run("valid refresh token", func(t *testing.T) {
		const code = "654321"
		mustSeedEmailCode(t, "refreshuser@example.com", "register", code)
		respLogin, err := Register("refreshuser@example.com", "password123", code, testAuthDeviceID, testAuthPlatform, testAuthLanguage, "")
		if err != nil {
			t.Fatalf("Login() error = %v", err)
		}

		resp, err := RefreshToken(respLogin.RefreshToken)
		if err != nil {
			t.Fatalf("RefreshToken() error = %v", err)
		}

		if resp.AccessToken == "" {
			t.Error("expected access token")
		}
		if resp.RefreshToken == "" {
			t.Error("expected new refresh token")
		}
		if resp.User.ID != respLogin.User.ID {
			t.Errorf("user ID mismatch")
		}
	})

	t.Run("invalid refresh token", func(t *testing.T) {
		_, err := RefreshToken("invalid.token.here")
		if err == nil {
			t.Error("expected error for invalid token")
		}
	})

	t.Run("access token as refresh token", func(t *testing.T) {
		user := fixture.CreateUser(func(u *model.User) {
			u.Username = "accesstest"
		})
		accessToken, _, _ := jwtpkg.GenerateAccessToken(user.ID)

		_, err := RefreshToken(accessToken)
		if err == nil {
			t.Error("expected error when using access token")
		}
	})

	t.Run("user not found", func(t *testing.T) {
		userID := int64(99999999)
		familyID := uuid.NewString()
		refreshToken, jti, expiresAt, err := jwtpkg.GenerateRefreshTokenWithFamily(userID, familyID, "")
		if err != nil {
			t.Fatalf("GenerateRefreshTokenWithFamily() error = %v", err)
		}
		if err := testDB.DB.Create(&model.RefreshToken{
			JTI:       jti,
			UserID:    userID,
			FamilyID:  familyID,
			Status:    model.RefreshTokenStatusActive,
			ExpiresAt: expiresAt,
		}).Error; err != nil {
			t.Fatalf("seed refresh token error: %v", err)
		}

		_, err = RefreshToken(refreshToken)
		if err == nil {
			t.Error("expected error for non-existent user")
		}
		if err.Error() != "用户不存在" {
			t.Errorf("expected '用户不存在', got '%s'", err.Error())
		}
	})

	t.Run("new tokens are valid", func(t *testing.T) {
		const code = "654321"
		mustSeedEmailCode(t, "newtokens@example.com", "register", code)
		respLogin, err := Register("newtokens@example.com", "password123", code, testAuthDeviceID, testAuthPlatform, testAuthLanguage, "")
		if err != nil {
			t.Fatalf("Register() error = %v", err)
		}
		resp, err := RefreshToken(respLogin.RefreshToken)
		if err != nil {
			t.Fatalf("RefreshToken() error = %v", err)
		}

		// Verify the new tokens are valid
		_, err = jwtpkg.ValidateAccessToken(resp.AccessToken)
		if err != nil {
			t.Errorf("new access token invalid: %v", err)
		}
		_, err = jwtpkg.ValidateRefreshToken(resp.RefreshToken)
		if err != nil {
			t.Errorf("new refresh token invalid: %v", err)
		}
	})
}

func TestResetPasswordRevokesSessions(t *testing.T) {
	testDB, cleanup := setupAuthTest(t)
	defer cleanup()

	fixture := testutil.NewFixtureBuilder(testDB.DB)
	oldHash, err := bcrypt.GenerateFromPassword([]byte("Oldpass123"), bcrypt.DefaultCost)
	if err != nil {
		t.Fatalf("GenerateFromPassword() error = %v", err)
	}

	user := fixture.CreateUser(func(u *model.User) {
		u.Email = "reset-pwd@example.com"
		u.PasswordHash = string(oldHash)
	})

	const code = "654321"
	mustSeedEmailCode(t, user.Email, "reset", code)
	if err := testDB.DB.Create(&model.RefreshToken{
		JTI:       "reset-rt-1",
		UserID:    user.ID,
		FamilyID:  "reset-family-1",
		Status:    model.RefreshTokenStatusActive,
		ExpiresAt: time.Now().UTC().Add(time.Hour),
	}).Error; err != nil {
		t.Fatalf("seed refresh token error: %v", err)
	}

	if err := ResetPassword(user.Email, "Newpass123", code); err != nil {
		t.Fatalf("ResetPassword() error = %v", err)
	}

	var updated model.User
	if err := testDB.DB.First(&updated, user.ID).Error; err != nil {
		t.Fatalf("load updated user error: %v", err)
	}
	if err := bcrypt.CompareHashAndPassword([]byte(updated.PasswordHash), []byte("Newpass123")); err != nil {
		t.Fatalf("expected new password hash match, got %v", err)
	}
	if err := bcrypt.CompareHashAndPassword([]byte(updated.PasswordHash), []byte("Oldpass123")); err == nil {
		t.Fatal("expected old password to be invalid")
	}

	var token model.RefreshToken
	if err := testDB.DB.Where("jti = ?", "reset-rt-1").First(&token).Error; err != nil {
		t.Fatalf("load refresh token error: %v", err)
	}
	if token.Status != model.RefreshTokenStatusRevoked {
		t.Fatalf("expected refresh token revoked, got status=%d", token.Status)
	}
	if token.RevokedAt == nil {
		t.Fatal("expected refresh token revoked_at set")
	}
	if !security.IsAccessTokenInvalidByPasswordChange(user.ID, time.Now().UTC().Add(-time.Minute)) {
		t.Fatal("expected older access tokens invalid after reset password")
	}
}

func TestLogout(t *testing.T) {
	testDB, cleanup := setupAuthTest(t)
	defer cleanup()

	fixture := testutil.NewFixtureBuilder(testDB.DB)
	user := fixture.CreateUser(func(u *model.User) {
		u.Username = "logoutuser"
	})

	t.Run("logout with device id", func(t *testing.T) {
		err := Logout(user.ID, "device-123")
		if err != nil {
			t.Errorf("Logout() error = %v", err)
		}
	})

	t.Run("logout should deactivate push binding for that device", func(t *testing.T) {
		deviceID := "logout-cleanup-device"
		if err := testDB.DB.Create(&model.Device{
			UserID:      user.ID,
			Platform:    model.DevicePlatformIOS,
			PushEnv:     model.DevicePushEnvAPNsSandbox,
			DeviceToken: "logout-cleanup-token",
			DeviceID:    deviceID,
			IsActive:    true,
		}).Error; err != nil {
			t.Fatalf("seed user device error = %v", err)
		}
		if err := store.RDB.HSet(
			context.Background(),
			fmt.Sprintf("im:user:devices:%d", user.ID),
			deviceID,
			`{"platform":"ios","push_env":"apns_sandbox","device_token":"logout-cleanup-token"}`,
		).Err(); err != nil {
			t.Fatalf("seed redis device cache error = %v", err)
		}

		if err := Logout(user.ID, deviceID); err != nil {
			t.Fatalf("Logout() error = %v", err)
		}

		var found model.Device
		if err := testDB.DB.Where("user_id = ? AND device_id = ?", user.ID, deviceID).First(&found).Error; err != nil {
			t.Fatalf("query user device error = %v", err)
		}
		if found.IsActive {
			t.Fatal("expected logout device push binding to be inactive")
		}
		if exists := store.RDB.HExists(
			context.Background(),
			fmt.Sprintf("im:user:devices:%d", user.ID),
			deviceID,
		).Val(); exists {
			t.Fatal("expected logout device redis cache entry removed")
		}
	})

	t.Run("logout without device id", func(t *testing.T) {
		err := Logout(user.ID, "")
		if err != nil {
			t.Errorf("Logout() error = %v", err)
		}
	})

	t.Run("logout with empty user id", func(t *testing.T) {
		err := Logout(0, "device-123")
		// Current implementation doesn't validate user_id
		if err != nil {
			t.Logf("Logout with 0 user_id: %v", err)
		}
	})
}

// 登录错误必须携带可判定的类型/哨兵，供 handler 做 403/429/503 语义化映射。
func TestLoginErrorSemantics(t *testing.T) {
	testDB, cleanup := setupAuthTest(t)
	defer cleanup()

	fixture := testutil.NewFixtureBuilder(testDB.DB)

	t.Run("banned user returns ErrUserDisabled sentinel", func(t *testing.T) {
		hash, _ := bcrypt.GenerateFromPassword([]byte("password123"), bcrypt.DefaultCost)
		fixture.CreateUser(func(u *model.User) {
			u.Email = "banned-sem@example.com"
			u.PasswordHash = string(hash)
			u.Status = model.UserStatusBanned
		})

		_, err := Login("banned-sem@example.com", "password123", testAuthDeviceID, testAuthPlatform, testAuthLanguage)
		if !errors.Is(err, security.ErrUserDisabled) {
			t.Fatalf("expected ErrUserDisabled, got %v", err)
		}
	})

	t.Run("locked account returns LoginLockedError type", func(t *testing.T) {
		hash, _ := bcrypt.GenerateFromPassword([]byte("rightpass"), bcrypt.DefaultCost)
		fixture.CreateUser(func(u *model.User) {
			u.Email = "locked-sem@example.com"
			u.PasswordHash = string(hash)
		})

		var lockedErr *security.LoginLockedError
		for i := 0; i < security.MaxLoginAttempts; i++ {
			_, err := Login("locked-sem@example.com", "wrongpass", testAuthDeviceID, testAuthPlatform, testAuthLanguage)
			if errors.As(err, &lockedErr) {
				break
			}
		}
		if lockedErr == nil {
			t.Fatal("expected LoginLockedError after exceeding failure limit")
		}
		if lockedErr.Error() != security.LoginLockedMessage(lockedErr.Remaining) {
			t.Fatalf("locked error message changed: %s", lockedErr.Error())
		}

		// 锁定期内即使密码正确也应返回同类型错误。
		var lockedAgain *security.LoginLockedError
		_, err := Login("locked-sem@example.com", "rightpass", testAuthDeviceID, testAuthPlatform, testAuthLanguage)
		if !errors.As(err, &lockedAgain) {
			t.Fatalf("expected LoginLockedError with correct password while locked, got %v", err)
		}
	})

	t.Run("wrong password stays generic credential error", func(t *testing.T) {
		hash, _ := bcrypt.GenerateFromPassword([]byte("rightpass"), bcrypt.DefaultCost)
		fixture.CreateUser(func(u *model.User) {
			u.Email = "plain-sem@example.com"
			u.PasswordHash = string(hash)
		})

		_, err := Login("plain-sem@example.com", "wrongpass", testAuthDeviceID, testAuthPlatform, testAuthLanguage)
		if err == nil || err.Error() != "用户不存在或密码错误" {
			t.Fatalf("expected generic credential error, got %v", err)
		}
		var lockedErr *security.LoginLockedError
		if errors.As(err, &lockedErr) || errors.Is(err, security.ErrUserDisabled) || errors.Is(err, ErrAuthServiceUnavailable) {
			t.Fatalf("credential error must not match semantic sentinels: %v", err)
		}
	})
}

// 刷新令牌时的存储层故障必须报成"服务暂时不可用"，绝不能报成"凭证失效"。
//
// handler 会把凭证失效转成 401，而客户端把 401 当作"会话已经没了"，会直接清掉本地
// 登录态。所以一旦把数据库故障混进凭证失效里，数据库抖一下，所有正在刷新令牌的在线
// 用户都会被踢回登录页，恢复后也回不来。登录路径早就用 ErrAuthServiceUnavailable
// 做了这个区分，refresh 一直漏了。
func TestRefreshTokenStorageFaultIsRetryable(t *testing.T) {
	testDB, cleanup := setupAuthTest(t)
	defer cleanup()

	const code = "654322"
	mustSeedEmailCode(t, "refreshfault@example.com", "register", code)
	respLogin, err := Register("refreshfault@example.com", "password123", code, testAuthDeviceID, testAuthPlatform, testAuthLanguage, "")
	if err != nil {
		t.Fatalf("Register() error = %v", err)
	}

	// 令牌完全有效，坏的只是存储层。
	if err := testDB.DB.Exec("DROP TABLE auth_refresh_tokens").Error; err != nil {
		t.Fatalf("drop auth_refresh_tokens error: %v", err)
	}

	_, err = RefreshToken(respLogin.RefreshToken)
	if err == nil {
		t.Fatal("expected an error when the auth_refresh_tokens table is unreadable")
	}
	if !errors.Is(err, ErrAuthServiceUnavailable) {
		t.Fatalf("存储层故障返回了 %q，期望 ErrAuthServiceUnavailable。"+
			"报成凭证失效会让 handler 回 401，客户端据此清掉会话——"+
			"数据库抖一下就能把正在刷新令牌的在线用户全部踢下线", err)
	}
}

// 用户在别处把邮箱绑成了带大写的写法时，Google 登录仍应认领到同一个账号，
// 而不是按「邮箱不存在」再建一个。
func TestLoginWithGoogleClaimsExistingAccountIgnoringEmailCase(t *testing.T) {
	testDB, cleanup := setupAuthTest(t)
	defer cleanup()
	setGoogleAllowedClientIDsForTest(t, "test-google-client-id")
	setGoogleTokenValidatorForTest(t, func(ctx context.Context, idToken string, audiences []string) (googleTokenProfile, error) {
		return googleTokenProfile{
			Email:         "mixed.case@google.com",
			Name:          "Mixed Case User",
			Subject:       "mock_google_id_mixed_case",
			EmailVerified: true,
		}, nil
	})

	fixture := testutil.NewFixtureBuilder(testDB.DB)
	existing := fixture.CreateUser(func(u *model.User) {
		u.Username = "mixed_case_user"
		u.Email = "Mixed.Case@Google.com"
		u.Nickname = "Mixed Case User"
		u.Status = model.UserStatusActive
	})

	resp, err := LoginWithGoogle("test-google-token", testAuthDeviceID, testAuthPlatform, testAuthLanguage)
	if err != nil {
		t.Fatalf("LoginWithGoogle() error = %v", err)
	}
	if resp.User.ID != existing.ID {
		t.Fatalf("expected login into existing account %d, got %d", existing.ID, resp.User.ID)
	}

	var userCount int64
	if err := testDB.DB.Model(&model.User{}).
		Where("LOWER(email) = ?", "mixed.case@google.com").
		Count(&userCount).Error; err != nil {
		t.Fatalf("count users error: %v", err)
	}
	if userCount != 1 {
		t.Fatalf("expected no duplicated account, got count=%d", userCount)
	}
}

// 绑定链路把邮箱统一存小写，用户按自己记的写法登录也要能进。
func TestLoginMatchesEmailIgnoringCase(t *testing.T) {
	testDB, cleanup := setupAuthTest(t)
	defer cleanup()

	hash, _ := bcrypt.GenerateFromPassword([]byte("correctpassword"), bcrypt.DefaultCost)
	fixture := testutil.NewFixtureBuilder(testDB.DB)
	user := fixture.CreateUser(func(u *model.User) {
		u.Email = "case-login@example.com"
		u.Username = "case_login"
		u.PasswordHash = string(hash)
	})

	resp, err := Login("Case-Login@Example.com", "correctpassword", testAuthDeviceID, testAuthPlatform, testAuthLanguage)
	if err != nil {
		t.Fatalf("Login() error = %v", err)
	}
	if resp.User.ID != user.ID {
		t.Fatalf("expected login into %d, got %d", user.ID, resp.User.ID)
	}
}

// 同理：重置密码不能在验证码验过之后才报"用户不存在"。
func TestResetPasswordMatchesEmailIgnoringCase(t *testing.T) {
	testDB, cleanup := setupAuthTest(t)
	defer cleanup()

	hash, _ := bcrypt.GenerateFromPassword([]byte("oldpassword"), bcrypt.DefaultCost)
	fixture := testutil.NewFixtureBuilder(testDB.DB)
	user := fixture.CreateUser(func(u *model.User) {
		u.Email = "case-reset@example.com"
		u.Username = "case_reset"
		u.PasswordHash = string(hash)
	})

	if err := storeEmailCode("Case-Reset@Example.com", "reset", "654321"); err != nil {
		t.Fatalf("写入验证码失败: %v", err)
	}
	if err := ResetPassword("Case-Reset@Example.com", "NewPassword123", "654321"); err != nil {
		t.Fatalf("ResetPassword() error = %v", err)
	}

	var updated model.User
	if err := testDB.DB.First(&updated, user.ID).Error; err != nil {
		t.Fatalf("查询用户失败: %v", err)
	}
	if bcrypt.CompareHashAndPassword([]byte(updated.PasswordHash), []byte("NewPassword123")) != nil {
		t.Fatal("密码未更新")
	}
}

// 唯一索引不折叠大小写，判重必须自己折叠，否则同一个人能开出第二个号。
func TestRegisterRejectsExistingEmailIgnoringCase(t *testing.T) {
	testDB, cleanup := setupAuthTest(t)
	defer cleanup()

	fixture := testutil.NewFixtureBuilder(testDB.DB)
	fixture.CreateUser(func(u *model.User) {
		u.Email = "Case-Register@Example.com"
		u.Username = "case_register"
		u.Status = model.UserStatusActive
	})

	if err := storeEmailCode("case-register@example.com", "register", "654321"); err != nil {
		t.Fatalf("写入验证码失败: %v", err)
	}
	if _, err := Register("case-register@example.com", "NewPassword123", "654321", testAuthDeviceID, testAuthPlatform, testAuthLanguage, ""); err == nil {
		t.Fatal("大小写变体的同一邮箱不应注册成功")
	}

	var count int64
	if err := testDB.DB.Model(&model.User{}).
		Where("LOWER(email) = ?", "case-register@example.com").
		Count(&count).Error; err != nil {
		t.Fatalf("count users error: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected no duplicated account, got count=%d", count)
	}
}

// Apple 侧与 Google 侧对等：已有账号邮箱大小写不同也要认领，而不是新建。
func TestLoginWithAppleClaimsExistingAccountIgnoringEmailCase(t *testing.T) {
	testDB, cleanup := setupAuthTest(t)
	defer cleanup()

	originalValidator := validateAppleIDToken
	validateAppleIDToken = func(idToken string) (appleTokenProfile, error) {
		return appleTokenProfile{
			Email:   "apple.case@example.com",
			Subject: "mock_apple_sub_case",
		}, nil
	}
	defer func() { validateAppleIDToken = originalValidator }()

	fixture := testutil.NewFixtureBuilder(testDB.DB)
	existing := fixture.CreateUser(func(u *model.User) {
		u.Username = "apple_case_user"
		u.Email = "Apple.Case@Example.com"
		u.Status = model.UserStatusActive
	})

	resp, err := LoginWithApple("test-apple-token", testAuthDeviceID, testAuthPlatform, testAuthLanguage)
	if err != nil {
		t.Fatalf("LoginWithApple() error = %v", err)
	}
	if resp.User.ID != existing.ID {
		t.Fatalf("expected login into existing account %d, got %d", existing.ID, resp.User.ID)
	}
}
