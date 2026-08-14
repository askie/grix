package service

import (
	"testing"

	"github.com/askie/grix/backend/internal/pkg/testutil"
	"github.com/askie/grix/backend/internal/store"
)

func TestRedisCaptchaStoreSharesStateAcrossInstances(t *testing.T) {
	previousRedis := store.RDB
	store.RDB = testutil.NewMockRedis()
	t.Cleanup(func() {
		_ = store.RDB.Close()
		store.RDB = previousRedis
	})

	writer := newRedisCaptchaStore(store.RDB)
	reader := newRedisCaptchaStore(store.RDB)

	if err := writer.Set("captcha-1", "2468"); err != nil {
		t.Fatalf("Set() error = %v", err)
	}

	if got := reader.Get("captcha-1", false); got != "2468" {
		t.Fatalf("Get() = %q, want %q", got, "2468")
	}
}

func TestVerifyCaptchaUsesSharedStoreAndClearsOnSuccess(t *testing.T) {
	previousRedis := store.RDB
	store.RDB = testutil.NewMockRedis()
	t.Cleanup(func() {
		_ = store.RDB.Close()
		store.RDB = previousRedis
	})

	if err := currentCaptchaStore().Set("captcha-2", "1357"); err != nil {
		t.Fatalf("Set() error = %v", err)
	}

	if ok := VerifyCaptcha("captcha-2", "1357"); !ok {
		t.Fatal("VerifyCaptcha() = false, want true")
	}
	if ok := VerifyCaptcha("captcha-2", "1357"); ok {
		t.Fatal("VerifyCaptcha() should clear captcha after successful verification")
	}
}
