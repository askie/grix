package identity

import (
	"context"
	"testing"

	"github.com/askie/grix/backend/internal/pkg/testutil"
	"github.com/askie/grix/backend/internal/store"
)

func setupRedisForTest(t *testing.T) {
	t.Helper()
	store.RDB = testutil.NewMockRedis()
}

func TestSmsCodeStore_ReserveCooldown_BlocksSecond(t *testing.T) {
	setupRedisForTest(t)
	s := SmsCodeStore{}
	ctx := context.Background()
	phone := "+8613800138000"
	if err := s.ReserveCooldown(ctx, phone, "1.2.3.4"); err != nil {
		t.Fatalf("first reserve should pass: %v", err)
	}
	if err := s.ReserveCooldown(ctx, phone, "1.2.3.4"); err != ErrSmsCooldown {
		t.Fatalf("second reserve should fail with cooldown, got %v", err)
	}
}

func TestSmsCodeStore_DayLimit(t *testing.T) {
	setupRedisForTest(t)
	s := SmsCodeStore{}
	ctx := context.Background()
	phone := "+8613800138001"
	for i := 0; i < SmsDayMax; i++ {
		if err := s.ReserveCooldown(ctx, phone, ""); err != nil {
			t.Fatalf("iter %d: %v", i, err)
		}
		s.RollbackCooldown(ctx, phone) // 滚回 60s 让下一轮过
	}
	// 第 11 次必须撞日限
	if err := s.ReserveCooldown(ctx, phone, ""); err != ErrSmsDayLimit {
		t.Fatalf("expected ErrSmsDayLimit, got %v", err)
	}
}

func TestSmsCodeStore_VerifyLifecycle(t *testing.T) {
	setupRedisForTest(t)
	s := SmsCodeStore{}
	ctx := context.Background()
	phone := "+8613800138002"
	code, err := s.Generate6Digit()
	if err != nil {
		t.Fatalf("gen: %v", err)
	}
	if err := s.StoreCode(ctx, "login", phone, code); err != nil {
		t.Fatalf("store: %v", err)
	}
	if !s.VerifyCode(ctx, "login", phone, code) {
		t.Fatalf("verify should pass")
	}
	// 一次成功后码作废
	if s.VerifyCode(ctx, "login", phone, code) {
		t.Fatalf("verify second time should fail")
	}
}

func TestSmsCodeStore_FailureCap(t *testing.T) {
	setupRedisForTest(t)
	s := SmsCodeStore{}
	ctx := context.Background()
	phone := "+8613800138003"
	if err := s.StoreCode(ctx, "login", phone, "123456"); err != nil {
		t.Fatalf("store: %v", err)
	}
	for i := 0; i < SmsMaxFailures; i++ {
		if s.VerifyCode(ctx, "login", phone, "000000") {
			t.Fatalf("wrong code shouldn't pass")
		}
	}
	// 失败 5 次后，正确码也应失效
	if s.VerifyCode(ctx, "login", phone, "123456") {
		t.Fatalf("after %d failures, even right code should be invalidated", SmsMaxFailures)
	}
}

func TestSmsCodeStore_CaptchaMarker(t *testing.T) {
	setupRedisForTest(t)
	s := SmsCodeStore{}
	ctx := context.Background()
	phone := "+8613800138004"
	if s.IsCaptchaRequired(ctx, phone) {
		t.Fatalf("fresh phone should not require captcha")
	}
	s.MarkCaptchaRequired(ctx, phone)
	if !s.IsCaptchaRequired(ctx, phone) {
		t.Fatalf("after mark, must require captcha")
	}
}

func TestSmsCodeStore_RedisUnavailable(t *testing.T) {
	store.RDB = nil
	defer func() { store.RDB = testutil.NewMockRedis() }()
	s := SmsCodeStore{}
	if err := s.ReserveCooldown(context.Background(), "+8613800138999", ""); err != ErrSmsRedisUnavailable {
		t.Fatalf("expected ErrSmsRedisUnavailable, got %v", err)
	}
}
