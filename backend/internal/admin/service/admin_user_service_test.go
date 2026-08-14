package service

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/snowflake"
	"github.com/askie/grix/backend/internal/pkg/testutil"
	"github.com/askie/grix/backend/internal/security"
	"github.com/askie/grix/backend/internal/store"
	"github.com/askie/grix/backend/internal/systemsetting"
)

func setupAdminServiceTest(t *testing.T) (*testutil.TestDB, func()) {
	t.Helper()

	if err := snowflake.Init(1); err != nil {
		t.Fatalf("init snowflake: %v", err)
	}

	testDB := testutil.NewTestDB()
	store.DB = testDB.DB
	store.RDB = nil
	systemsetting.InvalidateAuthSettingsCache()
	systemsetting.InvalidateContentModerationSettingsCache()
	systemsetting.InvalidateGroupSettingsCache()
	return testDB, func() {
		systemsetting.InvalidateAuthSettingsCache()
		systemsetting.InvalidateContentModerationSettingsCache()
		systemsetting.InvalidateGroupSettingsCache()
		testDB.Close()
	}
}

func createAdminFixture(t *testing.T, db *testutil.TestDB, id int64, username, nickname, password string, status int16) *model.AdminUser {
	t.Helper()

	passwordHash, err := HashAdminPassword(password)
	if err != nil {
		t.Fatalf("hash admin password: %v", err)
	}

	admin := &model.AdminUser{
		ID:           id,
		Username:     username,
		PasswordHash: passwordHash,
		Nickname:     nickname,
		Role:         model.AdminRoleSuperAdmin,
		Status:       status,
	}
	if err := db.DB.Create(admin).Error; err != nil {
		t.Fatalf("create admin fixture: %v", err)
	}
	return admin
}

func createAdminSessionFixture(t *testing.T, db *testutil.TestDB, sessionToken string, adminID int64) {
	t.Helper()

	now := time.Now().UTC()
	session := &model.AdminSession{
		SessionID:  hashToken(sessionToken),
		AdminID:    adminID,
		ExpiresAt:  now.Add(12 * time.Hour),
		LastSeenAt: now,
	}
	if err := db.DB.Create(session).Error; err != nil {
		t.Fatalf("create admin session fixture: %v", err)
	}
}

func TestCreateAdminAndChangeOwnPassword(t *testing.T) {
	testDB, cleanup := setupAdminServiceTest(t)
	defer cleanup()

	operator := createAdminFixture(t, testDB, 1001, "root", "Root", "OldPassword123A", model.AdminStatusActive)
	createAdminSessionFixture(t, testDB, "session-a", operator.ID)
	createAdminSessionFixture(t, testDB, "session-b", operator.ID)

	created, err := CreateAdmin(operator.ID, CreateAdminInput{
		Username: "ops-admin",
		Nickname: "Ops Admin",
		Password: "OpsPassword123A",
	}, "127.0.0.1", "test-agent")
	if err != nil {
		t.Fatalf("CreateAdmin() error = %v", err)
	}
	if created.Username != "ops-admin" {
		t.Fatalf("unexpected username: %s", created.Username)
	}

	_, err = CreateAdmin(operator.ID, CreateAdminInput{
		Username: "ops-admin",
		Nickname: "Other",
		Password: "AnotherPassword123A",
	}, "127.0.0.1", "test-agent")
	if !errors.Is(err, ErrAdminUsernameExists) {
		t.Fatalf("expected ErrAdminUsernameExists, got %v", err)
	}

	if err := ChangeOwnPassword(operator.ID, "OldPassword123A", "NewPassword123A", "127.0.0.1", "test-agent"); err != nil {
		t.Fatalf("ChangeOwnPassword() error = %v", err)
	}

	if _, _, err := Login("root", "OldPassword123A", "127.0.0.1", "test-agent"); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("expected old password login to fail, got %v", err)
	}
	if _, _, err := Login("root", "NewPassword123A", "127.0.0.1", "test-agent"); err != nil {
		t.Fatalf("expected new password login success, got %v", err)
	}

	var revokedCount int64
	if err := testDB.DB.Model(&model.AdminSession{}).
		Where("admin_id = ? AND revoked_at IS NOT NULL", operator.ID).
		Count(&revokedCount).Error; err != nil {
		t.Fatalf("count revoked sessions: %v", err)
	}
	if revokedCount != 2 {
		t.Fatalf("expected 2 revoked sessions, got %d", revokedCount)
	}
}

func TestDisableEnableDeleteAdmin(t *testing.T) {
	testDB, cleanup := setupAdminServiceTest(t)
	defer cleanup()

	root := createAdminFixture(t, testDB, 2001, "root", "Root", "RootPassword123A", model.AdminStatusActive)
	target := createAdminFixture(t, testDB, 2002, "target", "Target", "TargetPassword123A", model.AdminStatusActive)
	disabledOperator := createAdminFixture(t, testDB, 2003, "disabled", "Disabled", "DisabledPassword123A", model.AdminStatusDisabled)
	createAdminSessionFixture(t, testDB, "target-session", target.ID)

	if err := DisableAdmin(root.ID, root.ID, "127.0.0.1", "test-agent"); !errors.Is(err, ErrAdminDisableSelf) {
		t.Fatalf("expected ErrAdminDisableSelf, got %v", err)
	}

	if err := DisableAdmin(root.ID, target.ID, "127.0.0.1", "test-agent"); err != nil {
		t.Fatalf("DisableAdmin() error = %v", err)
	}

	var targetAdmin model.AdminUser
	if err := testDB.DB.First(&targetAdmin, target.ID).Error; err != nil {
		t.Fatalf("load disabled admin: %v", err)
	}
	if targetAdmin.Status != model.AdminStatusDisabled {
		t.Fatalf("expected disabled status, got %d", targetAdmin.Status)
	}

	var activeSessions int64
	if err := testDB.DB.Model(&model.AdminSession{}).
		Where("admin_id = ? AND revoked_at IS NULL", target.ID).
		Count(&activeSessions).Error; err != nil {
		t.Fatalf("count active sessions: %v", err)
	}
	if activeSessions != 0 {
		t.Fatalf("expected target sessions revoked, got %d active", activeSessions)
	}

	if err := EnableAdmin(root.ID, target.ID, "127.0.0.1", "test-agent"); err != nil {
		t.Fatalf("EnableAdmin() error = %v", err)
	}

	if err := DisableAdmin(root.ID, target.ID, "127.0.0.1", "test-agent"); err != nil {
		t.Fatalf("DisableAdmin() second pass error = %v", err)
	}

	if err := DisableAdmin(disabledOperator.ID, root.ID, "127.0.0.1", "test-agent"); !errors.Is(err, ErrAdminLastActiveProtected) {
		t.Fatalf("expected ErrAdminLastActiveProtected, got %v", err)
	}

	if err := DeleteAdmin(root.ID, root.ID, "127.0.0.1", "test-agent"); !errors.Is(err, ErrAdminDeleteSelf) {
		t.Fatalf("expected ErrAdminDeleteSelf, got %v", err)
	}

	if err := DeleteAdmin(root.ID, target.ID, "127.0.0.1", "test-agent"); err != nil {
		t.Fatalf("DeleteAdmin() error = %v", err)
	}

	var count int64
	if err := testDB.DB.Model(&model.AdminUser{}).Where("id = ?", target.ID).Count(&count).Error; err != nil {
		t.Fatalf("count deleted admin: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected target admin deleted, got count=%d", count)
	}

	var sessionCount int64
	if err := testDB.DB.Model(&model.AdminSession{}).Where("admin_id = ?", target.ID).Count(&sessionCount).Error; err != nil {
		t.Fatalf("count deleted admin sessions: %v", err)
	}
	if sessionCount != 0 {
		t.Fatalf("expected target sessions deleted, got %d", sessionCount)
	}
}

func TestChangeOwnPasswordClearsAdminLoginLock(t *testing.T) {
	testDB, cleanup := setupAdminServiceTest(t)
	defer cleanup()

	store.RDB = testutil.NewMockRedis()

	admin := createAdminFixture(t, testDB, 4001, "lock-reset-admin", "Lock Reset", "OldPassword123A", model.AdminStatusActive)

	for i := 0; i < security.MaxLoginAttempts; i++ {
		_, _, err := Login(admin.Username, "wrong-password", "127.0.0.1", "test-agent")
		if i < security.MaxLoginAttempts-1 {
			if !errors.Is(err, ErrInvalidCredentials) {
				t.Fatalf("attempt %d expected ErrInvalidCredentials, got %v", i+1, err)
			}
			continue
		}
		if err == nil || !strings.Contains(err.Error(), "账号已被锁定") {
			t.Fatalf("attempt %d expected lock error, got %v", i+1, err)
		}
	}

	guard := security.NewAdminLoginGuardByAdminID(admin.ID)
	locked, _, err := guard.IsLocked(context.Background())
	if err != nil {
		t.Fatalf("IsLocked() before change error = %v", err)
	}
	if !locked {
		t.Fatal("expected admin to be locked before password change")
	}

	if err := ChangeOwnPassword(admin.ID, "OldPassword123A", "NewPassword123A", "127.0.0.1", "test-agent"); err != nil {
		t.Fatalf("ChangeOwnPassword() error = %v", err)
	}

	locked, _, err = guard.IsLocked(context.Background())
	if err != nil {
		t.Fatalf("IsLocked() after change error = %v", err)
	}
	if locked {
		t.Fatal("expected admin lock to be cleared after password change")
	}

	remaining, err := guard.GetRemainingAttempts(context.Background())
	if err != nil {
		t.Fatalf("GetRemainingAttempts() after change error = %v", err)
	}
	if remaining != security.MaxLoginAttempts {
		t.Fatalf("expected remaining attempts reset to %d, got %d", security.MaxLoginAttempts, remaining)
	}

	if _, _, err := Login(admin.Username, "NewPassword123A", "127.0.0.1", "test-agent"); err != nil {
		t.Fatalf("expected new password login success, got %v", err)
	}
}
