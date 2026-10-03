package service

import (
	"context"
	"errors"
	"testing"

	"github.com/askie/grix/backend/internal/api/service/identity"
	"github.com/askie/grix/backend/internal/featuregate"
	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/secretcrypto"
	"github.com/askie/grix/backend/internal/store"
	"github.com/askie/grix/backend/internal/systemsetting"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func registrationFlow(t *testing.T, kind string) (*LoginResp, error) {
	t.Helper()
	switch kind {
	case "email":
		mustSeedEmailCode(t, "new-policy@example.com", "register", "654321")
		return Register("new-policy@example.com", "Password123", "654321", testAuthDeviceID, testAuthPlatform, testAuthLanguage, "")
	case "phone":
		require.NoError(t, phoneSmsStore.StoreCode(context.Background(), string(identity.SmsSceneLogin), "+8613812345678", "654321"))
		return PhoneLoginWithCode("+8613812345678", "654321", testAuthDeviceID, testAuthPlatform, testAuthLanguage, "127.0.0.1")
	case "google":
		return LoginWithGoogle("fake-google-token", testAuthDeviceID, testAuthPlatform, testAuthLanguage)
	default:
		return LoginWithApple("fake-apple-token", testAuthDeviceID, testAuthPlatform, testAuthLanguage)
	}
}

func stubPolicyProviders(t *testing.T) {
	t.Helper()
	setGoogleTokenValidatorForTest(t, func(context.Context, string, []string) (googleTokenProfile, error) {
		return googleTokenProfile{Email: "new-policy@example.com", Subject: "policy-google", Name: "Policy Google", EmailVerified: true}, nil
	})
	old := validateAppleIDToken
	validateAppleIDToken = func(string) (appleTokenProfile, error) {
		return appleTokenProfile{Email: "new-policy@example.com", Subject: "policy-apple"}, nil
	}
	t.Cleanup(func() { validateAppleIDToken = old })
	writeSmsSettings(t, systemsetting.SmsSettings{PhoneLoginEnabledCN: true, PhoneRegisterEnabledCN: true, PhoneLoginEnabledGlobal: true, PhoneRegisterEnabledGlobal: true, AllowedCountryCodesCN: []string{"+86"}, AllowedCountryCodesGlobal: []string{"*"}})
}

func accountCounts(t *testing.T) map[string]int64 {
	t.Helper()
	counts := map[string]int64{}
	for _, table := range []string{"users", "user_identities", "oauth_accounts", "user_settings", "devices", "login_device_sessions", "friends", "register_welcome_compensations"} {
		var count int64
		require.NoError(t, store.DB.Table(table).Count(&count).Error)
		counts[table] = count
	}
	return counts
}

func TestRegistrationPolicyActualEntries(t *testing.T) {
	for _, state := range []string{"enabled", "disabled", "missing", "whitelist", "read-error"} {
		for _, kind := range []string{"email", "phone", "google", "apple"} {
			t.Run(state+"/"+kind, func(t *testing.T) {
				_, cleanup := setupAuthTest(t)
				defer cleanup()
				stubPolicyProviders(t)
				// Warm the old snapshot before a separate DB connection changes the row.
				cached, err := featuregate.IsPublicFeatureEnabled(featuregate.FeatureRegistration)
				require.NoError(t, err)
				require.True(t, cached)
				independent, err := gorm.Open(sqlite.Open(store.DB.Dialector.(*sqlite.Dialector).DSN), &gorm.Config{})
				require.NoError(t, err)
				sqlDB, err := independent.DB()
				require.NoError(t, err)
				defer sqlDB.Close()
				if state == "missing" {
					require.NoError(t, independent.Where("key = ?", featuregate.FeatureRegistration).Delete(&model.FeatureGate{}).Error)
				} else if state != "read-error" {
					require.NoError(t, independent.Model(&model.FeatureGate{}).Where("key = ?", featuregate.FeatureRegistration).Update("status", state).Error)
				}
				if state == "read-error" {
					require.NoError(t, store.DB.Callback().Query().Before("gorm:query").Register("policy_read_error", func(db *gorm.DB) {
						if db.Statement.Table == "feature_gates" {
							db.AddError(errors.New("gate DB unavailable"))
						}
					}))
					defer store.DB.Callback().Query().Remove("policy_read_error")
				}
				// Cached enabled must remain as a negative control; admission ignores it.
				cached, err = featuregate.IsPublicFeatureEnabled(featuregate.FeatureRegistration)
				require.NoError(t, err)
				require.True(t, cached)
				before := accountCounts(t)
				resp, err := registrationFlow(t, kind)
				if state == "enabled" {
					require.NoError(t, err)
					require.NotNil(t, resp)
					require.NotEmpty(t, resp.AccessToken)
					require.EqualValues(t, before["users"]+1, accountCounts(t)["users"])
				} else {
					require.Error(t, err)
					require.Nil(t, resp)
					if state != "read-error" {
						require.Contains(t, err.Error(), "关闭注册")
					}
					require.Equal(t, before, accountCounts(t), "denied request must create no related records")
				}
				methods := GetAuthMethods("cn")
				require.Equal(t, state == "enabled", methods.RegistrationEnabled)
				require.Equal(t, state == "enabled", methods.PhoneRegisterEnabled)
				require.True(t, methods.PhoneLoginEnabled, "global admission must not disable existing phone login")
			})
		}
	}
}

func TestRegistrationPolicyExistingAccounts(t *testing.T) {
	for _, kind := range []string{"password", "phone", "google", "apple", "reset"} {
		t.Run(kind, func(t *testing.T) {
			_, cleanup := setupAuthTest(t)
			defer cleanup()
			stubPolicyProviders(t)
			mustSeedEmailCode(t, "new-policy@example.com", "register", "654321")
			user, err := Register("new-policy@example.com", "Password123", "654321", testAuthDeviceID, testAuthPlatform, testAuthLanguage, "")
			require.NoError(t, err)
			require.NoError(t, store.DB.Model(&model.FeatureGate{}).Where("key = ?", featuregate.FeatureRegistration).Update("status", "disabled").Error)
			before := accountCounts(t)["users"]
			var resp *LoginResp
			switch kind {
			case "password":
				resp, err = Login("new-policy@example.com", "Password123", testAuthDeviceID, testAuthPlatform, testAuthLanguage)
			case "phone":
				require.NoError(t, store.DB.Create(&model.UserIdentity{ID: 12001, UserID: user.User.ID, Provider: model.IdentityProviderPhoneSmsCN, ExternalID: secretcrypto.BlindIndex("+8613812345678")}).Error)
				resp, err = registrationFlow(t, "phone")
			case "reset":
				mustSeedEmailCode(t, "new-policy@example.com", "reset", "654321")
				err = ResetPassword("new-policy@example.com", "NewPassword123", "654321")
			default:
				// First call binds the verified email to the existing user; second uses UID.
				resp, err = registrationFlow(t, kind)
				require.NoError(t, err)
				require.Equal(t, user.User.ID, resp.User.ID)
				resp, err = registrationFlow(t, kind)
			}
			require.NoError(t, err)
			if kind != "reset" {
				require.Equal(t, user.User.ID, resp.User.ID)
				require.NotEmpty(t, resp.AccessToken)
			}
			require.Equal(t, before, accountCounts(t)["users"])
		})
	}
}

func TestRegistrationPolicyVerificationAndSmsConjunction(t *testing.T) {
	_, cleanup := setupAuthTest(t)
	defer cleanup()
	stubPolicyProviders(t)
	require.NoError(t, store.DB.Model(&model.FeatureGate{}).Where("key = ?", featuregate.FeatureRegistration).Update("status", "disabled").Error)
	// Public service entry must deny before touching delivery or quota.
	require.ErrorContains(t, SendEmailCode("127.0.0.1", "new-policy@example.com", "register", "", "", "en"), "关闭注册")
	require.ErrorContains(t, SendPhoneSmsCode("127.0.0.1", "+8613812345678", identity.SmsSceneRegister, "", "", "en"), "关闭注册")
	for _, global := range []bool{false, true} {
		status := "disabled"
		if global {
			status = "enabled"
		}
		require.NoError(t, store.DB.Model(&model.FeatureGate{}).Where("key = ?", featuregate.FeatureRegistration).Update("status", status).Error)
		for _, regional := range []bool{false, true} {
			writeSmsSettings(t, systemsetting.SmsSettings{PhoneRegisterEnabledCN: regional, PhoneLoginEnabledCN: true, AllowedCountryCodesCN: []string{"+86"}})
			methods := GetAuthMethods("cn")
			require.Equal(t, global, methods.RegistrationEnabled)
			require.Equal(t, global && regional, methods.PhoneRegisterEnabled)
			require.True(t, methods.PhoneLoginEnabled)
			if !global || !regional {
				resp, err := registrationFlow(t, "phone")
				require.Nil(t, resp)
				require.ErrorContains(t, err, "关闭注册")
			}
		}
	}
}
