package service

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/testutil"
	"github.com/askie/grix/backend/internal/security"
	"github.com/askie/grix/backend/internal/store"
)

func TestAdminLoginLockout(t *testing.T) {
	testDB, cleanup := setupAdminServiceTest(t)
	defer cleanup()

	store.RDB = testutil.NewMockRedis()

	admin := createAdminFixture(t, testDB, 3001, "lockadmin", "Lock Admin", "LockPassword123A", model.AdminStatusActive)

	for i := 0; i < 4; i++ {
		_, _, err := Login("lockadmin", "wrong-password", "127.0.0.1", "test-agent")
		if !errors.Is(err, ErrInvalidCredentials) {
			t.Fatalf("attempt %d expected ErrInvalidCredentials, got %v", i+1, err)
		}
	}

	_, _, err := Login("lockadmin", "wrong-password", "127.0.0.1", "test-agent")
	if err == nil || !strings.Contains(err.Error(), "账号已被锁定") || !strings.Contains(err.Error(), "1小时") {
		t.Fatalf("expected lock error on fifth attempt, got %v", err)
	}

	adminGuard := security.NewAdminLoginGuardByAdminID(admin.ID)
	locked, remaining, err := adminGuard.IsLocked(context.Background())
	if err != nil {
		t.Fatalf("IsLocked() error = %v", err)
	}
	if !locked {
		t.Fatal("expected admin account to be locked")
	}
	if remaining < security.LockoutDuration-time.Minute || remaining > security.LockoutDuration {
		t.Fatalf("expected first lock duration close to %s, got %s", security.LockoutDuration, remaining)
	}

	_, _, err = Login("lockadmin", "LockPassword123A", "127.0.0.1", "test-agent")
	if err == nil || !strings.Contains(err.Error(), "账号已被锁定") {
		t.Fatalf("expected locked admin account to reject correct password, got %v", err)
	}

	if err := adminGuard.ClearLock(context.Background()); err != nil {
		t.Fatalf("ClearLock() error = %v", err)
	}

	for i := 0; i < 4; i++ {
		_, _, err := Login("lockadmin", "wrong-password", "127.0.0.1", "test-agent")
		if !errors.Is(err, ErrInvalidCredentials) {
			t.Fatalf("relock attempt %d expected ErrInvalidCredentials, got %v", i+1, err)
		}
	}

	// 指数退避：第二次锁定时长为首次的 2 倍（1h → 2h）。
	const secondLockDuration = 2 * time.Hour
	_, _, err = Login("lockadmin", "wrong-password", "127.0.0.1", "test-agent")
	if err == nil || !strings.Contains(err.Error(), "账号已被锁定") || !strings.Contains(err.Error(), "2小时") {
		t.Fatalf("expected second lock error with 2h duration, got %v", err)
	}

	locked, remaining, err = adminGuard.IsLocked(context.Background())
	if err != nil {
		t.Fatalf("IsLocked() after relock error = %v", err)
	}
	if !locked {
		t.Fatal("expected admin account to be locked after relock")
	}
	if remaining < secondLockDuration-time.Minute || remaining > secondLockDuration {
		t.Fatalf("expected second lock duration close to %s, got %s", secondLockDuration, remaining)
	}
}

func TestAdminLoginSuccessClearsFailedAttempts(t *testing.T) {
	testDB, cleanup := setupAdminServiceTest(t)
	defer cleanup()

	store.RDB = testutil.NewMockRedis()

	admin := createAdminFixture(t, testDB, 3002, "clearadmin", "Clear Admin", "ClearPassword123A", model.AdminStatusActive)

	for i := 0; i < 2; i++ {
		_, _, err := Login("clearadmin", "wrong-password", "127.0.0.1", "test-agent")
		if !errors.Is(err, ErrInvalidCredentials) {
			t.Fatalf("attempt %d expected ErrInvalidCredentials, got %v", i+1, err)
		}
	}

	adminGuard := security.NewAdminLoginGuardByAdminID(admin.ID)
	remaining, err := adminGuard.GetRemainingAttempts(context.Background())
	if err != nil {
		t.Fatalf("GetRemainingAttempts() error = %v", err)
	}
	if remaining != security.MaxLoginAttempts-2 {
		t.Fatalf("expected %d remaining attempts before success, got %d", security.MaxLoginAttempts-2, remaining)
	}

	if _, _, err := Login("clearadmin", "ClearPassword123A", "127.0.0.1", "test-agent"); err != nil {
		t.Fatalf("expected successful admin login, got %v", err)
	}

	remaining, err = adminGuard.GetRemainingAttempts(context.Background())
	if err != nil {
		t.Fatalf("GetRemainingAttempts() after success error = %v", err)
	}
	if remaining != security.MaxLoginAttempts {
		t.Fatalf("expected remaining attempts reset to %d, got %d", security.MaxLoginAttempts, remaining)
	}
}
