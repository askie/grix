package service

import (
	"context"
	"testing"
	"time"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/testutil"
	"github.com/askie/grix/backend/internal/security"
	"github.com/askie/grix/backend/internal/store"
	"golang.org/x/crypto/bcrypt"
)

func TestChangeOwnPassword(t *testing.T) {
	testDB, cleanup := setupAuthTest(t)
	defer cleanup()

	fixture := testutil.NewFixtureBuilder(testDB.DB)
	oldHash, err := bcrypt.GenerateFromPassword([]byte("Oldpass123"), bcrypt.DefaultCost)
	if err != nil {
		t.Fatalf("GenerateFromPassword() error = %v", err)
	}

	user := fixture.CreateUser(func(u *model.User) {
		u.Email = "change-password@example.com"
		u.PasswordHash = string(oldHash)
	})

	if err := storeEmailCode(user.Email, changePasswordEmailCodeScene, "654321"); err != nil {
		t.Fatalf("storeEmailCode() error = %v", err)
	}

	if err := ChangeOwnPassword(user.ID, "Newpass123", "654321", "", time.Time{}); err != nil {
		t.Fatalf("ChangeOwnPassword() error = %v", err)
	}

	var updated model.User
	if err := testDB.DB.First(&updated, user.ID).Error; err != nil {
		t.Fatalf("query updated user error = %v", err)
	}
	if err := bcrypt.CompareHashAndPassword(
		[]byte(updated.PasswordHash),
		[]byte("Newpass123"),
	); err != nil {
		t.Fatalf("expected new password hash match, got error: %v", err)
	}
	if err := bcrypt.CompareHashAndPassword(
		[]byte(updated.PasswordHash),
		[]byte("Oldpass123"),
	); err == nil {
		t.Fatal("expected old password to be invalid")
	}
}

func TestChangeOwnPasswordRejectsInvalidCode(t *testing.T) {
	testDB, cleanup := setupAuthTest(t)
	defer cleanup()

	fixture := testutil.NewFixtureBuilder(testDB.DB)
	oldHash, err := bcrypt.GenerateFromPassword([]byte("Oldpass123"), bcrypt.DefaultCost)
	if err != nil {
		t.Fatalf("GenerateFromPassword() error = %v", err)
	}

	user := fixture.CreateUser(func(u *model.User) {
		u.Email = "change-password-invalid-code@example.com"
		u.PasswordHash = string(oldHash)
	})

	err = ChangeOwnPassword(user.ID, "Newpass123", "000000", "", time.Time{})
	if err == nil {
		t.Fatal("expected invalid email code error")
	}
	if err != ErrChangePasswordCodeInvalid {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestChangeOwnPasswordRejectsWeakPassword(t *testing.T) {
	testDB, cleanup := setupAuthTest(t)
	defer cleanup()

	fixture := testutil.NewFixtureBuilder(testDB.DB)
	oldHash, err := bcrypt.GenerateFromPassword([]byte("Oldpass123"), bcrypt.DefaultCost)
	if err != nil {
		t.Fatalf("GenerateFromPassword() error = %v", err)
	}
	user := fixture.CreateUser(func(u *model.User) {
		u.Email = "change-password-weak@example.com"
		u.PasswordHash = string(oldHash)
	})
	if err := storeEmailCode(user.Email, changePasswordEmailCodeScene, "654321"); err != nil {
		t.Fatalf("storeEmailCode() error = %v", err)
	}

	err = ChangeOwnPassword(user.ID, "weakpass", "654321", "", time.Time{})
	if err == nil {
		t.Fatal("expected weak password error")
	}
	if err != ErrUserPasswordPolicy {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestChangeOwnPasswordRevokesAllSessions(t *testing.T) {
	testDB, cleanup := setupAuthTest(t)
	defer cleanup()

	fixture := testutil.NewFixtureBuilder(testDB.DB)
	oldHash, err := bcrypt.GenerateFromPassword([]byte("Oldpass123"), bcrypt.DefaultCost)
	if err != nil {
		t.Fatalf("GenerateFromPassword() error = %v", err)
	}
	user := fixture.CreateUser(func(u *model.User) {
		u.Email = "change-password-revoke@example.com"
		u.PasswordHash = string(oldHash)
	})
	if err := storeEmailCode(user.Email, changePasswordEmailCodeScene, "654321"); err != nil {
		t.Fatalf("storeEmailCode() error = %v", err)
	}

	now := time.Now().UTC()
	seedTokens := []model.RefreshToken{
		{
			JTI:       "active-rt",
			UserID:    user.ID,
			FamilyID:  "family-1",
			Status:    model.RefreshTokenStatusActive,
			ExpiresAt: now.Add(24 * time.Hour),
		},
		{
			JTI:       "used-rt",
			UserID:    user.ID,
			FamilyID:  "family-2",
			Status:    model.RefreshTokenStatusUsed,
			ExpiresAt: now.Add(24 * time.Hour),
		},
	}
	for _, token := range seedTokens {
		item := token
		if err := testDB.DB.Create(&item).Error; err != nil {
			t.Fatalf("seed refresh token error = %v", err)
		}
	}

	accessJTI := "access-jti-1"
	accessExp := now.Add(30 * time.Minute)
	if err := ChangeOwnPassword(user.ID, "Newpass123", "654321", accessJTI, accessExp); err != nil {
		t.Fatalf("ChangeOwnPassword() error = %v", err)
	}

	var updatedTokens []model.RefreshToken
	if err := testDB.DB.Where("user_id = ?", user.ID).Find(&updatedTokens).Error; err != nil {
		t.Fatalf("query refresh tokens error = %v", err)
	}
	if len(updatedTokens) != 2 {
		t.Fatalf("expected 2 refresh tokens, got %d", len(updatedTokens))
	}
	for _, token := range updatedTokens {
		if token.Status != model.RefreshTokenStatusRevoked {
			t.Fatalf("expected token %s revoked, got status=%d", token.JTI, token.Status)
		}
		if token.RevokedAt == nil {
			t.Fatalf("expected token %s revoked_at to be set", token.JTI)
		}
	}

	key := revokedAccessTokenRedisKey(accessJTI)
	value, err := store.RDB.Get(context.Background(), key).Result()
	if err != nil {
		t.Fatalf("expected revoked access token marker, get error = %v", err)
	}
	if value != "1" {
		t.Fatalf("expected revoked marker value '1', got %q", value)
	}
	if !security.IsAccessTokenInvalidByPasswordChange(user.ID, now.Add(-1*time.Second)) {
		t.Fatal("expected older access token to be invalid after password change")
	}
}

func TestSendChangePasswordEmailCodeRequiresUserEmail(t *testing.T) {
	testDB, cleanup := setupAuthTest(t)
	defer cleanup()

	fixture := testutil.NewFixtureBuilder(testDB.DB)
	user := fixture.CreateUser(func(u *model.User) {
		u.Email = ""
	})

	err := SendChangePasswordEmailCode(user.ID, "zh-CN")
	if err == nil {
		t.Fatal("expected user email missing error")
	}
	if err != ErrChangePasswordUserEmailAbsent {
		t.Fatalf("unexpected error: %v", err)
	}
}
