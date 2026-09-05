package service

import (
	"testing"
	"time"

	"github.com/askie/grix/backend/internal/model"
	jwtpkg "github.com/askie/grix/backend/internal/pkg/jwt"
	"github.com/askie/grix/backend/internal/security"
	"github.com/askie/grix/backend/internal/store"
)

// registerPhoneForWatchTest creates a user with a normal phone login session,
// which is what the watch credential must stay independent of.
func registerPhoneForWatchTest(t *testing.T, email string) *LoginResp {
	t.Helper()
	const code = "654321"
	mustSeedEmailCode(t, email, "register", code)
	resp, err := Register(email, "password123", code, testAuthDeviceID, testAuthPlatform, testAuthLanguage, "")
	if err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	return resp
}

func watchFamilyOf(t *testing.T, accessToken string) string {
	t.Helper()
	claims, err := jwtpkg.ValidateAccessToken(accessToken)
	if err != nil {
		t.Fatalf("ValidateAccessToken() error = %v", err)
	}
	if claims.SessionID == "" {
		t.Fatal("expected access token bound to a login session")
	}
	return claims.SessionID
}

func TestIssueWatchTokensCreatesIndependentFamily(t *testing.T) {
	_, cleanup := setupAuthTest(t)
	defer cleanup()

	phone := registerPhoneForWatchTest(t, "watch-family@example.com")
	watch, err := IssueWatchTokens(phone.User.ID)
	if err != nil {
		t.Fatalf("IssueWatchTokens() error = %v", err)
	}
	if watch.AccessToken == "" || watch.RefreshToken == "" {
		t.Fatal("expected a watch access + refresh pair")
	}

	phoneFamily := watchFamilyOf(t, phone.AccessToken)
	watchFamily := watchFamilyOf(t, watch.AccessToken)
	if phoneFamily == watchFamily {
		t.Fatal("watch must not share the phone's refresh family")
	}

	var session model.LoginDeviceSession
	if err := store.DB.
		Where("user_id = ? AND session_id = ?", phone.User.ID, watchFamily).
		First(&session).Error; err != nil {
		t.Fatalf("watch login device session missing: %v", err)
	}
	if session.Platform != WatchPlatform {
		t.Fatalf("expected platform %q, got %q", WatchPlatform, session.Platform)
	}
	if session.DeviceID == testAuthDeviceID {
		t.Fatal("watch must not reuse the phone's device id; that would revoke the phone session")
	}

	// The phone's own family survives both the issue and the watch's rotation.
	if _, err := RefreshToken(phone.RefreshToken); err != nil {
		t.Fatalf("phone refresh after watch issue error = %v", err)
	}
	rotated, err := RefreshToken(watch.RefreshToken)
	if err != nil {
		t.Fatalf("watch refresh error = %v", err)
	}
	if got := watchFamilyOf(t, rotated.AccessToken); got != watchFamily {
		t.Fatalf("watch refresh must stay in its own family, got %s want %s", got, watchFamily)
	}
}

func TestIssueWatchTokensRevokesPreviousWatchFamily(t *testing.T) {
	_, cleanup := setupAuthTest(t)
	defer cleanup()

	phone := registerPhoneForWatchTest(t, "watch-reissue@example.com")
	first, err := IssueWatchTokens(phone.User.ID)
	if err != nil {
		t.Fatalf("first IssueWatchTokens() error = %v", err)
	}
	firstFamily := watchFamilyOf(t, first.AccessToken)

	second, err := IssueWatchTokens(phone.User.ID)
	if err != nil {
		t.Fatalf("second IssueWatchTokens() error = %v", err)
	}
	if watchFamilyOf(t, second.AccessToken) == firstFamily {
		t.Fatal("re-issue must mint a new family")
	}

	if _, err := RefreshToken(first.RefreshToken); err == nil {
		t.Fatal("expected the superseded watch refresh token to be rejected")
	}
	if !security.IsLoginSessionRevoked(phone.User.ID, firstFamily) {
		t.Fatal("expected the superseded watch access token to be revoked")
	}

	// Exactly one live watch session, and the phone is untouched.
	var live int64
	if err := store.DB.Model(&model.LoginDeviceSession{}).
		Where("user_id = ? AND platform = ? AND revoked_at IS NULL", phone.User.ID, WatchPlatform).
		Count(&live).Error; err != nil {
		t.Fatalf("count watch sessions error = %v", err)
	}
	if live != 1 {
		t.Fatalf("expected 1 live watch session, got %d", live)
	}
	if _, err := RefreshToken(phone.RefreshToken); err != nil {
		t.Fatalf("phone refresh after watch re-issue error = %v", err)
	}
}

func TestLogoutRevokesWatchFamily(t *testing.T) {
	_, cleanup := setupAuthTest(t)
	defer cleanup()

	phone := registerPhoneForWatchTest(t, "watch-logout@example.com")
	watch, err := IssueWatchTokens(phone.User.ID)
	if err != nil {
		t.Fatalf("IssueWatchTokens() error = %v", err)
	}
	watchFamily := watchFamilyOf(t, watch.AccessToken)
	phoneFamily := watchFamilyOf(t, phone.AccessToken)

	// A phone logout is scoped to the phone's own session id; the watch lives
	// in a different family and would otherwise survive it.
	if err := LogoutWithToken(
		phone.User.ID,
		testAuthDeviceID,
		phoneFamily,
		"",
		time.Time{},
	); err != nil {
		t.Fatalf("LogoutWithToken() error = %v", err)
	}

	if _, err := RefreshToken(watch.RefreshToken); err == nil {
		t.Fatal("expected the watch refresh token to be dead after phone logout")
	}
	if !security.IsLoginSessionRevoked(phone.User.ID, watchFamily) {
		t.Fatal("expected the watch access token to be revoked after phone logout")
	}
}

func TestPasswordChangeInvalidatesWatchTokens(t *testing.T) {
	_, cleanup := setupAuthTest(t)
	defer cleanup()

	phone := registerPhoneForWatchTest(t, "watch-password@example.com")
	watch, err := IssueWatchTokens(phone.User.ID)
	if err != nil {
		t.Fatalf("IssueWatchTokens() error = %v", err)
	}
	watchFamily := watchFamilyOf(t, watch.AccessToken)

	const code = "654321"
	mustSeedEmailCode(t, "watch-password@example.com", "reset", code)
	if err := ResetPassword("watch-password@example.com", "newpassword456", code); err != nil {
		t.Fatalf("ResetPassword() error = %v", err)
	}

	// Asserted through the session revocation rather than the password-changed
	// clock: that clock has one-second resolution, so a token minted in the same
	// second as the reset would look valid. Session revocation is exact, and it
	// is the check middleware.Auth() and the ws service both run.
	if !security.IsLoginSessionRevoked(phone.User.ID, watchFamily) {
		t.Fatal("expected the watch access token to be invalidated by the password change")
	}
	if !security.IsAccessTokenInvalidByPasswordChange(phone.User.ID, time.Now().UTC().Add(-time.Minute)) {
		t.Fatal("expected the password-changed marker to cover this user")
	}
	if _, err := RefreshToken(watch.RefreshToken); err == nil {
		t.Fatal("expected the watch refresh token to be dead after a password change")
	}
}
