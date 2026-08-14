package security

import (
	"context"
	"testing"
	"time"

	"github.com/askie/grix/backend/internal/store"
	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

func setupLoginGuardTest(t *testing.T) func() {
	t.Helper()

	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis.Run() error = %v", err)
	}

	store.RDB = redis.NewClient(&redis.Options{Addr: mr.Addr()})

	return func() {
		if store.RDB != nil {
			_ = store.RDB.Close()
		}
		store.RDB = nil
		mr.Close()
	}
}

func assertLockDurationNear(t *testing.T, actual, expected time.Duration) {
	t.Helper()

	const tolerance = 5 * time.Second
	if actual < expected-tolerance || actual > expected {
		t.Fatalf("expected lock duration close to %s, got %s", expected, actual)
	}
}

func TestLoginGuardEscalatesLockDuration(t *testing.T) {
	cleanup := setupLoginGuardTest(t)
	defer cleanup()

	ctx := context.Background()
	guard := NewUserLoginGuardByUserID(1001)

	for i := 0; i < MaxLoginAttempts-1; i++ {
		count, locked, duration, err := guard.RecordFailure(ctx)
		if err != nil {
			t.Fatalf("first cycle attempt %d RecordFailure() error = %v", i+1, err)
		}
		if count != i+1 {
			t.Fatalf("first cycle attempt %d expected count %d, got %d", i+1, i+1, count)
		}
		if locked {
			t.Fatalf("first cycle attempt %d should not be locked", i+1)
		}
		if duration != 0 {
			t.Fatalf("first cycle attempt %d expected no lock duration, got %s", i+1, duration)
		}
	}

	count, locked, duration, err := guard.RecordFailure(ctx)
	if err != nil {
		t.Fatalf("first cycle final RecordFailure() error = %v", err)
	}
	if count != MaxLoginAttempts {
		t.Fatalf("first cycle expected count %d, got %d", MaxLoginAttempts, count)
	}
	if !locked {
		t.Fatal("first cycle expected locked=true")
	}
	if duration != LockoutDuration {
		t.Fatalf("first cycle expected lock duration %s, got %s", LockoutDuration, duration)
	}

	lockCount, err := store.RDB.Get(ctx, guard.lockCountKey()).Int()
	if err != nil {
		t.Fatalf("first cycle lock count query error = %v", err)
	}
	if lockCount != 1 {
		t.Fatalf("first cycle expected lock count 1, got %d", lockCount)
	}

	isLocked, remaining, err := guard.IsLocked(ctx)
	if err != nil {
		t.Fatalf("first cycle IsLocked() error = %v", err)
	}
	if !isLocked {
		t.Fatal("first cycle expected locked account")
	}
	assertLockDurationNear(t, remaining, LockoutDuration)

	if err := guard.ClearLock(ctx); err != nil {
		t.Fatalf("ClearLock() error = %v", err)
	}

	for i := 0; i < MaxLoginAttempts-1; i++ {
		count, locked, duration, err := guard.RecordFailure(ctx)
		if err != nil {
			t.Fatalf("second cycle attempt %d RecordFailure() error = %v", i+1, err)
		}
		if count != i+1 {
			t.Fatalf("second cycle attempt %d expected count %d, got %d", i+1, i+1, count)
		}
		if locked {
			t.Fatalf("second cycle attempt %d should not be locked", i+1)
		}
		if duration != 0 {
			t.Fatalf("second cycle attempt %d expected no lock duration, got %s", i+1, duration)
		}
	}

	count, locked, duration, err = guard.RecordFailure(ctx)
	if err != nil {
		t.Fatalf("second cycle final RecordFailure() error = %v", err)
	}
	if count != MaxLoginAttempts {
		t.Fatalf("second cycle expected count %d, got %d", MaxLoginAttempts, count)
	}
	if !locked {
		t.Fatal("second cycle expected locked=true")
	}
	if duration != RepeatedLockDuration {
		t.Fatalf("second cycle expected lock duration %s, got %s", RepeatedLockDuration, duration)
	}

	lockCount, err = store.RDB.Get(ctx, guard.lockCountKey()).Int()
	if err != nil {
		t.Fatalf("second cycle lock count query error = %v", err)
	}
	if lockCount != 2 {
		t.Fatalf("second cycle expected lock count 2, got %d", lockCount)
	}

	isLocked, remaining, err = guard.IsLocked(ctx)
	if err != nil {
		t.Fatalf("second cycle IsLocked() error = %v", err)
	}
	if !isLocked {
		t.Fatal("second cycle expected locked account")
	}
	assertLockDurationNear(t, remaining, RepeatedLockDuration)
}

// TestAdminLoginGuardExponentialBackoff 验证管理员锁定时长按指数退避增长并封顶。
func TestAdminLoginGuardExponentialBackoff(t *testing.T) {
	cleanup := setupLoginGuardTest(t)
	defer cleanup()

	ctx := context.Background()
	guard := NewAdminLoginGuardByAdminID(2001)

	// 期望序列：1h → 2h → 4h → 8h → 16h → 24h(封顶) → 24h
	expected := []time.Duration{
		1 * time.Hour,
		2 * time.Hour,
		4 * time.Hour,
		8 * time.Hour,
		16 * time.Hour,
		24 * time.Hour,
		24 * time.Hour,
	}

	for cycle, want := range expected {
		// 每个周期内累计 MaxLoginAttempts 次失败触发一次锁定。
		var duration time.Duration
		for i := 0; i < MaxLoginAttempts; i++ {
			_, _, d, err := guard.RecordFailure(ctx)
			if err != nil {
				t.Fatalf("cycle %d attempt %d RecordFailure() error = %v", cycle+1, i+1, err)
			}
			duration = d
		}
		if duration != want {
			t.Fatalf("cycle %d expected lock duration %s, got %s", cycle+1, want, duration)
		}
		// 清除锁定但保留锁定计数，以便下一周期继续升级。
		if err := guard.ClearLock(ctx); err != nil {
			t.Fatalf("cycle %d ClearLock() error = %v", cycle+1, err)
		}
	}
}
