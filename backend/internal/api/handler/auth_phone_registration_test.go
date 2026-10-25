package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/store"
	"github.com/askie/grix/backend/internal/systemsetting"
	"github.com/redis/go-redis/v9"
	"gorm.io/datatypes"
)

func TestPhoneLoginNewClientRequiresRegistrationConfirmation(t *testing.T) {
	tc, _ := setupPhoneRegistrationTest(t)
	const phone = "+8613800140000"
	seedPhoneLoginCode(t, phone, "654321")

	status, code, data := requestPhoneLogin(t, tc, map[string]any{
		"phone_e164":                         phone,
		"code":                               "654321",
		"supports_registration_confirmation": true,
	})
	if status != http.StatusOK || code != 0 {
		t.Fatalf("expected successful challenge, status=%d code=%d data=%v", status, code, data)
	}
	if data["requires_registration_confirmation"] != true {
		t.Fatalf("expected registration confirmation, data=%v", data)
	}
	if ticket, _ := data["registration_ticket"].(string); ticket == "" {
		t.Fatalf("expected registration ticket, data=%v", data)
	}
	if _, exists := data["access_token"]; exists {
		t.Fatalf("confirmation response must not issue tokens, data=%v", data)
	}
	assertPhoneLoginUserCount(t, 0)
	remaining, err := store.RDB.Exists(context.Background(), "auth:sms_code:login:"+phone).Result()
	if err != nil || remaining != 0 {
		t.Fatalf("expected SMS code consumed, remaining=%d err=%v", remaining, err)
	}
}

func TestPhoneLoginRegistrationTicketConfirmAndSingleUse(t *testing.T) {
	tc, _ := setupPhoneRegistrationTest(t)
	const phone = "+8613800140001"
	ticket := requirePhoneRegistrationTicket(t, tc, phone)
	confirmation := map[string]any{
		"phone_e164":                         phone,
		"registration_ticket":                ticket,
		"supports_registration_confirmation": true,
	}

	status, code, data := requestPhoneLogin(t, tc, confirmation)
	if status != http.StatusOK || code != 0 || data["access_token"] == "" {
		t.Fatalf("expected confirmed registration, status=%d code=%d data=%v", status, code, data)
	}
	assertPhoneLoginUserCount(t, 1)

	status, code, _ = requestPhoneLogin(t, tc, confirmation)
	if status == http.StatusOK && code == 0 {
		t.Fatalf("expected replayed ticket to fail, status=%d code=%d", status, code)
	}
	assertPhoneLoginUserCount(t, 1)
}

func TestPhoneLoginRegistrationTicketExpires(t *testing.T) {
	tc, mr := setupPhoneRegistrationTest(t)
	const phone = "+8613800140002"
	ticket := requirePhoneRegistrationTicket(t, tc, phone)
	mr.FastForward(6 * time.Minute)

	status, code, _ := requestPhoneLogin(t, tc, map[string]any{
		"phone_e164":                         phone,
		"registration_ticket":                ticket,
		"supports_registration_confirmation": true,
	})
	if status == http.StatusOK && code == 0 {
		t.Fatalf("expected expired ticket to fail, status=%d code=%d", status, code)
	}
	assertPhoneLoginUserCount(t, 0)
}

func TestPhoneLoginRegistrationTicketIsBoundToPhone(t *testing.T) {
	tc, _ := setupPhoneRegistrationTest(t)
	ticket := requirePhoneRegistrationTicket(t, tc, "+8613800140003")

	status, code, _ := requestPhoneLogin(t, tc, map[string]any{
		"phone_e164":                         "+8613800140004",
		"registration_ticket":                ticket,
		"supports_registration_confirmation": true,
	})
	if status == http.StatusOK && code == 0 {
		t.Fatalf("expected ticket for another phone to fail, status=%d code=%d", status, code)
	}
	assertPhoneLoginUserCount(t, 0)
}

func TestPhoneLoginLegacyClientStillAutoRegisters(t *testing.T) {
	tc, _ := setupPhoneRegistrationTest(t)
	const phone = "+8613800140005"
	seedPhoneLoginCode(t, phone, "654321")

	status, code, data := requestPhoneLogin(t, tc, map[string]any{
		"phone_e164": phone,
		"code":       "654321",
	})
	if status != http.StatusOK || code != 0 || data["access_token"] == "" {
		t.Fatalf("expected legacy automatic registration, status=%d code=%d data=%v", status, code, data)
	}
	if data["requires_registration_confirmation"] == true || data["registration_ticket"] != nil {
		t.Fatalf("legacy response must not contain a confirmation challenge, data=%v", data)
	}
	assertPhoneLoginUserCount(t, 1)
}

func TestPhoneLoginNewClientExistingAccountDoesNotRequireConfirmation(t *testing.T) {
	tc, _ := setupPhoneRegistrationTest(t)
	const phone = "+8613800140006"
	seedPhoneLoginCode(t, phone, "654321")
	status, code, data := requestPhoneLogin(t, tc, map[string]any{
		"phone_e164": phone,
		"code":       "654321",
	})
	if status != http.StatusOK || code != 0 || data["access_token"] == "" {
		t.Fatalf("seed existing account failed, status=%d code=%d data=%v", status, code, data)
	}

	seedPhoneLoginCode(t, phone, "123456")
	status, code, data = requestPhoneLogin(t, tc, map[string]any{
		"phone_e164":                         phone,
		"code":                               "123456",
		"supports_registration_confirmation": true,
	})
	if status != http.StatusOK || code != 0 || data["access_token"] == "" {
		t.Fatalf("expected existing account login, status=%d code=%d data=%v", status, code, data)
	}
	if data["requires_registration_confirmation"] == true {
		t.Fatalf("existing account must not require confirmation, data=%v", data)
	}
	assertPhoneLoginUserCount(t, 1)
}

func setupPhoneRegistrationTest(t *testing.T) (*testContext, *miniredis.Miniredis) {
	t.Helper()
	tc := setupTest(t)
	mr, err := miniredis.Run()
	if err != nil {
		tc.cleanup()
		t.Fatalf("start miniredis: %v", err)
	}
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	store.RDB = client
	writePhoneRegistrationSmsSettings(t)
	tc.router.POST("/phone/login-code", PhoneLoginWithCode)
	t.Cleanup(func() {
		_ = client.Close()
		mr.Close()
		systemsetting.InvalidateAuthSettingsCache()
		systemsetting.InvalidateSmsSettingsCache()
		tc.cleanup()
	})
	return tc, mr
}

func writePhoneRegistrationSmsSettings(t *testing.T) {
	t.Helper()
	raw, err := json.Marshal(systemsetting.SmsSettings{
		PhoneLoginEnabledCN:    true,
		PhoneRegisterEnabledCN: true,
		AllowedCountryCodesCN:  []string{"+86"},
	})
	if err != nil {
		t.Fatalf("marshal sms settings: %v", err)
	}
	row := model.SystemSetting{Key: "sms", Value: datatypes.JSON(raw)}
	if err := store.DB.Where("key = ?", row.Key).Assign(row).FirstOrCreate(&row).Error; err != nil {
		t.Fatalf("write sms settings: %v", err)
	}
	systemsetting.InvalidateSmsSettingsCache()
}

func seedPhoneLoginCode(t *testing.T, phone, code string) {
	t.Helper()
	if err := store.RDB.Set(context.Background(), "auth:sms_code:login:"+phone, code, 5*time.Minute).Err(); err != nil {
		t.Fatalf("seed login code: %v", err)
	}
}

func requirePhoneRegistrationTicket(t *testing.T, tc *testContext, phone string) string {
	t.Helper()
	seedPhoneLoginCode(t, phone, "654321")
	status, code, data := requestPhoneLogin(t, tc, map[string]any{
		"phone_e164":                         phone,
		"code":                               "654321",
		"supports_registration_confirmation": true,
	})
	if status != http.StatusOK || code != 0 || data["requires_registration_confirmation"] != true {
		t.Fatalf("expected registration challenge, status=%d code=%d data=%v", status, code, data)
	}
	ticket, _ := data["registration_ticket"].(string)
	if ticket == "" {
		t.Fatalf("missing registration ticket, data=%v", data)
	}
	return ticket
}

func requestPhoneLogin(t *testing.T, tc *testContext, payload map[string]any) (int, int, map[string]any) {
	t.Helper()
	payload["device_id"] = "phone-registration-test-device"
	payload["platform"] = "test"
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/phone/login-code", bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	tc.router.ServeHTTP(w, req)
	var body struct {
		Code int            `json:"code"`
		Data map[string]any `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response %d: %v; body=%s", w.Code, err, w.Body.String())
	}
	return w.Code, body.Code, body.Data
}

func assertPhoneLoginUserCount(t *testing.T, want int64) {
	t.Helper()
	var got int64
	if err := store.DB.Model(&model.User{}).Count(&got).Error; err != nil {
		t.Fatalf("count users: %v", err)
	}
	if got != want {
		t.Fatalf("expected %d users, got %d", want, got)
	}
}
