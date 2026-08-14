package service

import (
	"context"
	"testing"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/logger"
	"github.com/askie/grix/backend/internal/pkg/secretcrypto"
	"github.com/askie/grix/backend/internal/pkg/testutil"
	"github.com/askie/grix/backend/internal/store"
)

// TestRunPhoneEncryptionMigration 校验存量明文手机号回填为加密存储，并且可重复跑（幂等）。
func TestRunPhoneEncryptionMigration(t *testing.T) {
	logger.Init() // 迁移内有 logger 调用；生产由 cmd/migrate 的 main 初始化
	store.DB = testutil.NewTestDB().DB
	phone := "+8613800138000"

	u := model.User{ID: 1001, Username: "legacy", PhoneE164: phone, PhoneCountry: "+86"}
	if err := store.DB.Create(&u).Error; err != nil {
		t.Fatalf("seed user: %v", err)
	}
	id := model.UserIdentity{ID: 2001, UserID: 1001, Provider: model.IdentityProviderPhoneSmsCN, ExternalID: phone, CountryCode: "+86"}
	if err := store.DB.Create(&id).Error; err != nil {
		t.Fatalf("seed identity: %v", err)
	}

	if err := RunPhoneEncryptionMigration(context.Background()); err != nil {
		t.Fatalf("migration: %v", err)
	}

	var got model.User
	if err := store.DB.First(&got, 1001).Error; err != nil {
		t.Fatalf("load user: %v", err)
	}
	if got.PhoneE164 != "" {
		t.Fatalf("phone_e164 should be nulled, got %q", got.PhoneE164)
	}
	if plain, err := secretcrypto.Decrypt(got.PhoneCipher); err != nil || plain != phone {
		t.Fatalf("decrypt cipher = %q err=%v, want %q", plain, err, phone)
	}
	if got.PhoneBlind != secretcrypto.BlindIndex(phone) {
		t.Fatalf("phone_blind mismatch")
	}
	if got.PhoneLast4 != "8000" {
		t.Fatalf("phone_last4 = %q, want 8000", got.PhoneLast4)
	}

	var gotID model.UserIdentity
	if err := store.DB.First(&gotID, 2001).Error; err != nil {
		t.Fatalf("load identity: %v", err)
	}
	if gotID.ExternalID != secretcrypto.BlindIndex(phone) {
		t.Fatalf("identity external_id = %q, want blind index", gotID.ExternalID)
	}

	// 幂等：再跑一次，不报错且不改动
	if err := RunPhoneEncryptionMigration(context.Background()); err != nil {
		t.Fatalf("rerun migration: %v", err)
	}
	var again model.User
	if err := store.DB.First(&again, 1001).Error; err != nil {
		t.Fatalf("reload user: %v", err)
	}
	if again.PhoneBlind != got.PhoneBlind || again.PhoneCipher != got.PhoneCipher {
		t.Fatalf("migration not idempotent")
	}
}
