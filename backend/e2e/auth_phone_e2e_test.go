package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/askie/grix/backend/internal/api/service/identity"
	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/secretcrypto"
	"github.com/askie/grix/backend/internal/store"
	"github.com/askie/grix/backend/internal/systemsetting"
	"gorm.io/datatypes"
)

// ----- mock provider -----

type mockSmsProvider struct {
	name  string
	mu    sync.Mutex
	calls []identity.SendSmsRequest
}

func (m *mockSmsProvider) Name() string { return m.name }

func (m *mockSmsProvider) Send(_ context.Context, req identity.SendSmsRequest) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls = append(m.calls, req)
	// 把生成的 code 也直接写到 mock store，让后续 verify 命中
	// （实际 store 已写过；这里只记录调用）
	return nil
}

func (m *mockSmsProvider) HealthCheck(_ context.Context) error { return nil }

func (m *mockSmsProvider) lastCode() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.calls) == 0 {
		return ""
	}
	return m.calls[len(m.calls)-1].Code
}

// enablePhoneSms 把测试用的 mock provider 装到 identity.Default()
// 同时把 system_settings.sms 开关都打开（含 CN + Global register/login）。
func enablePhoneSms(t *testing.T) (*mockSmsProvider, *mockSmsProvider) {
	t.Helper()
	cnMock := &mockSmsProvider{name: model.IdentityProviderPhoneSmsCN}
	globalMock := &mockSmsProvider{name: model.IdentityProviderPhoneSmsGlobal}
	identity.Default().SetSms(cnMock)
	identity.Default().SetSms(globalMock)

	// 强制刷新 SMS settings 缓存为"全开 + 白名单 +86 / *"
	smsCfg := systemsetting.SmsSettings{
		PhoneRegisterEnabledCN:     true,
		PhoneRegisterEnabledGlobal: true,
		PhoneLoginEnabledCN:        true,
		PhoneLoginEnabledGlobal:    true,
		AllowedCountryCodesCN:      []string{"+86"},
		AllowedCountryCodesGlobal:  []string{"*"},
		CnSmsProvider:              "aliyun",
		GlobalSmsProvider:          "aws_sns",
	}
	raw, _ := json.Marshal(smsCfg)
	row := &model.SystemSetting{Key: "sms", Value: datatypes.JSON(raw)}
	if err := store.DB.Where("key = ?", "sms").Assign(model.SystemSetting{Value: datatypes.JSON(raw)}).
		FirstOrCreate(row).Error; err != nil {
		t.Fatalf("seed sms settings: %v", err)
	}
	systemsetting.InvalidateSmsSettingsCache()
	return cnMock, globalMock
}

// ----- 测试 -----

func TestPhoneSms_SendAndLogin_AutoRegister_CN(t *testing.T) {
	ctx := setupE2E(t)
	defer ctx.cleanup()
	cnMock, _ := enablePhoneSms(t)

	phone := "+8613800139000"

	// 1) 发码
	w := ctx.doReq(t, "POST", "/v1/auth/sms/send", "", map[string]any{
		"phone_e164": phone,
		"scene":      "login",
	})
	if w.Code != http.StatusOK {
		t.Fatalf("send code failed: %d %s", w.Code, w.Body.String())
	}
	code := cnMock.lastCode()
	if len(code) != 6 {
		t.Fatalf("expected 6-digit code, got %q", code)
	}

	// 2) login-code：新号自动注册
	w = ctx.doReq(t, "POST", "/v1/auth/phone/login-code", "", map[string]any{
		"phone_e164": phone,
		"code":       code,
		"device_id":  "e2e-phone-cn-1",
		"platform":   "test",
	})
	if w.Code != http.StatusOK {
		t.Fatalf("login-code failed: %d %s", w.Code, w.Body.String())
	}
	res := parseResp(t, w)
	data, ok := res["data"].(map[string]any)
	if !ok {
		t.Fatalf("missing data")
	}
	if _, ok := data["access_token"].(string); !ok {
		t.Fatalf("expected access_token")
	}
	user, _ := data["user"].(map[string]any)
	if user["region"] != "cn" {
		t.Fatalf("expected region cn, got %v", user["region"])
	}
	// 出站脱敏：phone_e164 只给末 4 位（****xxxx），绝不是真实号
	maskedPhone, _ := user["phone_e164"].(string)
	if !strings.HasPrefix(maskedPhone, "****") || maskedPhone == phone {
		t.Fatalf("expected masked phone_e164 ****xxxx, got %v", user["phone_e164"])
	}

	// 3) 二次登录：用 Redis 直接注入码（绕过二次发码 captcha 强制路径），
	//    重点在校验"老号码命中 identity → 不重复建 user"。
	if err := store.RDB.Set(context.Background(), "auth:sms_code:login:"+phone, "246810", 5*60_000_000_000).Err(); err != nil {
		t.Fatalf("seed second code: %v", err)
	}
	w = ctx.doReq(t, "POST", "/v1/auth/phone/login-code", "", map[string]any{
		"phone_e164": phone,
		"code":       "246810",
		"device_id":  "e2e-phone-cn-2",
		"platform":   "test",
	})
	if w.Code != http.StatusOK {
		t.Fatalf("second login-code failed: %d %s", w.Code, w.Body.String())
	}

	// 校验 user_identities 只有一行（external_id 存盲索引）
	var count int64
	if err := store.DB.Model(&model.UserIdentity{}).Where("external_id = ?", secretcrypto.BlindIndex(phone)).Count(&count).Error; err != nil {
		t.Fatalf("count identities: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected 1 identity row for phone, got %d", count)
	}
}

func TestPhoneSms_GlobalRegion(t *testing.T) {
	ctx := setupE2E(t)
	defer ctx.cleanup()
	_, globalMock := enablePhoneSms(t)
	phone := "+15551234999"
	w := ctx.doReq(t, "POST", "/v1/auth/sms/send", "", map[string]any{
		"phone_e164": phone,
		"scene":      "login",
	})
	if w.Code != http.StatusOK {
		t.Fatalf("send code: %d %s", w.Code, w.Body.String())
	}
	code := globalMock.lastCode()
	w = ctx.doReq(t, "POST", "/v1/auth/phone/login-code", "", map[string]any{
		"phone_e164": phone,
		"code":       code,
		"device_id":  "e2e-phone-global-1",
		"platform":   "test",
	})
	if w.Code != http.StatusOK {
		t.Fatalf("login-code: %d %s", w.Code, w.Body.String())
	}
	res := parseResp(t, w)
	data, _ := res["data"].(map[string]any)
	user, _ := data["user"].(map[string]any)
	if user["region"] != "global" {
		t.Fatalf("expected region global, got %v", user["region"])
	}
}

func TestPhoneSms_CountryCodeWhitelist_RejectsCNRequestForGlobalNumber(t *testing.T) {
	ctx := setupE2E(t)
	defer ctx.cleanup()
	enablePhoneSms(t)
	// 把 cn 白名单只留 +86，但用 +44 号码发码 → 走 global 通道；OK
	// 把 global 白名单设为 [] 不允许任何 → 该号被拒
	cfg := systemsetting.SmsSettings{
		PhoneRegisterEnabledCN:     true,
		PhoneRegisterEnabledGlobal: true,
		PhoneLoginEnabledCN:        true,
		PhoneLoginEnabledGlobal:    true,
		AllowedCountryCodesCN:      []string{"+86"},
		AllowedCountryCodesGlobal:  []string{"+1"}, // 只允许 +1
	}
	raw, _ := json.Marshal(cfg)
	_ = store.DB.Model(&model.SystemSetting{}).Where("key = ?", "sms").
		Update("value", datatypes.JSON(raw)).Error
	systemsetting.InvalidateSmsSettingsCache()

	w := ctx.doReq(t, "POST", "/v1/auth/sms/send", "", map[string]any{
		"phone_e164": "+447712345678",
		"scene":      "login",
	})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for +44 when only +1 whitelisted, got %d %s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	if !strings.Contains(body, "不支持的区号") {
		t.Fatalf("expected '不支持的区号' in body, got %s", body)
	}
}

func TestPhoneSms_RegisterDisabledCN(t *testing.T) {
	ctx := setupE2E(t)
	defer ctx.cleanup()
	enablePhoneSms(t)
	cfg := systemsetting.SmsSettings{
		PhoneRegisterEnabledCN:     false, // 关掉 cn 注册
		PhoneRegisterEnabledGlobal: true,
		PhoneLoginEnabledCN:        true,
		PhoneLoginEnabledGlobal:    true,
		AllowedCountryCodesCN:      []string{"+86"},
		AllowedCountryCodesGlobal:  []string{"*"},
	}
	raw, _ := json.Marshal(cfg)
	_ = store.DB.Model(&model.SystemSetting{}).Where("key = ?", "sms").
		Update("value", datatypes.JSON(raw)).Error
	systemsetting.InvalidateSmsSettingsCache()

	phone := fmt.Sprintf("+861380013%04d", 1234)
	w := ctx.doReq(t, "POST", "/v1/auth/sms/send", "", map[string]any{
		"phone_e164": phone,
		"scene":      "login", // login 是开的
	})
	if w.Code != http.StatusOK {
		t.Fatalf("send-code for login should work even if register closed; got %d %s", w.Code, w.Body.String())
	}
	code := identityDefaultLastCode(t)

	// 但 login-code 命中"未注册 → 走注册"路径，会被 register closed 拒绝
	w = ctx.doReq(t, "POST", "/v1/auth/phone/login-code", "", map[string]any{
		"phone_e164": phone,
		"code":       code,
		"device_id":  "e2e-cn-noreg",
		"platform":   "test",
	})
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 with register disabled, got %d %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "系统已关闭注册") {
		t.Fatalf("expected '系统已关闭注册', got %s", w.Body.String())
	}
}

func TestPhoneSms_BindForExistingEmailUser(t *testing.T) {
	ctx := setupE2E(t)
	defer ctx.cleanup()
	cnMock, _ := enablePhoneSms(t)
	// 先用邮箱注册一个用户
	token, userID := ctx.loginHelper(t, "phonebind", "Aa12345678!")
	if token == "" || userID == 0 {
		t.Fatalf("loginHelper failed")
	}

	phone := "+8613800139500"
	w := ctx.doReq(t, "POST", "/v1/auth/sms/send", "", map[string]any{
		"phone_e164": phone,
		"scene":      "bind",
	})
	if w.Code != http.StatusOK {
		t.Fatalf("send bind code: %d %s", w.Code, w.Body.String())
	}
	code := cnMock.lastCode()

	w = ctx.doReq(t, "POST", "/v1/users/bind-phone", token, map[string]any{
		"phone_e164": phone,
		"code":       code,
	})
	if w.Code != http.StatusOK {
		t.Fatalf("bind: %d %s", w.Code, w.Body.String())
	}

	// 校验加密绑定：明文列为空、密文解回真实号、盲索引与末4位写入；identity 行按盲索引匹配
	var u model.User
	if err := store.DB.First(&u, userID).Error; err != nil {
		t.Fatalf("load user: %v", err)
	}
	if u.PhoneE164 != "" {
		t.Fatalf("users.phone_e164 should stay empty after encrypted bind, got %q", u.PhoneE164)
	}
	if plain, derr := secretcrypto.Decrypt(u.PhoneCipher); derr != nil || plain != phone {
		t.Fatalf("decrypt phone_cipher = %q err=%v, want real %q", plain, derr, phone)
	}
	if u.PhoneBlind != secretcrypto.BlindIndex(phone) {
		t.Fatalf("phone_blind mismatch")
	}
	if u.PhoneLast4 == "" || !strings.HasSuffix(phone, u.PhoneLast4) {
		t.Fatalf("phone_last4 = %q, want suffix of %q", u.PhoneLast4, phone)
	}
	var idCount int64
	if err := store.DB.Model(&model.UserIdentity{}).
		Where("user_id = ? AND external_id = ?", userID, secretcrypto.BlindIndex(phone)).Count(&idCount).Error; err != nil {
		t.Fatalf("count: %v", err)
	}
	if idCount != 1 {
		t.Fatalf("expected 1 identity row, got %d", idCount)
	}
}

// identityDefaultLastCode 取 cn mock 上一次发的码（小 helper，避免显式 cast）
func identityDefaultLastCode(t *testing.T) string {
	t.Helper()
	p, err := identity.Default().GetSms(model.IdentityProviderPhoneSmsCN)
	if err != nil {
		t.Fatalf("get cn provider: %v", err)
	}
	m, ok := p.(*mockSmsProvider)
	if !ok {
		t.Fatalf("expected mock provider, got %T", p)
	}
	return m.lastCode()
}
