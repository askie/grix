package e2e

import (
	"net/http"
	"strings"
	"testing"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/secretcrypto"
	"github.com/askie/grix/backend/internal/store"
)

// TestPhoneMaskOutbound_LoginAndProfile 端到端校验：
//   - 手机短信登录返回的 user.phone_e164 必须脱敏（不能等于真实号）
//   - GET /v1/users/profile 返回的 phone_e164 必须脱敏
//   - 数据库里 users.phone_e164 仍然是真实号（后端内部仍可用）
func TestPhoneMaskOutbound_LoginAndProfile(t *testing.T) {
	ctx := setupE2E(t)
	defer ctx.cleanup()
	cnMock, _ := enablePhoneSms(t)

	phone := "+8613800138000"
	wantMasked := "****8000"

	// 1) 发码
	w := ctx.doReq(t, "POST", "/v1/auth/sms/send", "", map[string]any{
		"phone_e164": phone,
		"scene":      "login",
	})
	if w.Code != http.StatusOK {
		t.Fatalf("send code: %d %s", w.Code, w.Body.String())
	}
	code := cnMock.lastCode()

	// 2) 登录（自动注册）
	w = ctx.doReq(t, "POST", "/v1/auth/phone/login-code", "", map[string]any{
		"phone_e164": phone,
		"code":       code,
		"device_id":  "e2e-phone-mask-outbound",
		"platform":   "test",
	})
	if w.Code != http.StatusOK {
		t.Fatalf("login-code: %d %s", w.Code, w.Body.String())
	}
	res := parseResp(t, w)
	data, _ := res["data"].(map[string]any)
	token, _ := data["access_token"].(string)
	if token == "" {
		t.Fatalf("missing access_token")
	}
	user, _ := data["user"].(map[string]any)
	loginPhone, _ := user["phone_e164"].(string)
	if loginPhone == phone {
		t.Fatalf("login response leaked real phone: %q", loginPhone)
	}
	if loginPhone != wantMasked {
		t.Fatalf("login response phone_e164 = %q, want %q", loginPhone, wantMasked)
	}
	// 整段响应体二次过滤，防止任何角落漏出真实号
	if strings.Contains(w.Body.String(), phone) {
		t.Fatalf("login response body contains real phone %q in: %s", phone, w.Body.String())
	}

	// 3) GET /v1/users/profile
	w = ctx.doReq(t, "GET", "/v1/users/profile", token, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("get profile: %d %s", w.Code, w.Body.String())
	}
	res = parseResp(t, w)
	pdata, _ := res["data"].(map[string]any)
	pPhone, _ := pdata["phone_e164"].(string)
	if pPhone != wantMasked {
		t.Fatalf("profile phone_e164 = %q, want %q", pPhone, wantMasked)
	}
	if strings.Contains(w.Body.String(), phone) {
		t.Fatalf("profile response body contains real phone %q in: %s", phone, w.Body.String())
	}

	// 4) 数据库里不再有明文：按盲索引能查到、密文能解回真实号、末4位正确、明文列为空
	blind := secretcrypto.BlindIndex(phone)
	var u model.User
	if err := store.DB.Where("phone_blind = ?", blind).First(&u).Error; err != nil {
		t.Fatalf("expected DB row found by blind index, err: %v", err)
	}
	if u.PhoneE164 != "" {
		t.Fatalf("DB phone_e164 should be empty after encryption, got %q", u.PhoneE164)
	}
	plain, derr := secretcrypto.Decrypt(u.PhoneCipher)
	if derr != nil || plain != phone {
		t.Fatalf("decrypt phone_cipher = %q err=%v, want real %q", plain, derr, phone)
	}
	if u.PhoneLast4 != "8000" {
		t.Fatalf("phone_last4 = %q, want 8000", u.PhoneLast4)
	}
}

// TestPhoneMaskOutbound_EmptyPhoneOmitted 校验未绑手机的用户，
// 登录响应里 phone_e164 字段应通过 omitempty 缺席。
func TestPhoneMaskOutbound_EmptyPhoneOmitted(t *testing.T) {
	ctx := setupE2E(t)
	defer ctx.cleanup()

	// 走邮箱注册，不绑手机号
	token, _ := ctx.loginHelper(t, "phonemaskempty", "Aa12345678!")
	if token == "" {
		t.Fatalf("loginHelper failed")
	}

	w := ctx.doReq(t, "GET", "/v1/users/profile", token, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("get profile: %d %s", w.Code, w.Body.String())
	}
	body := w.Body.String()
	if strings.Contains(body, `"phone_e164"`) {
		t.Fatalf("expected phone_e164 field omitted for empty value, got body: %s", body)
	}
}
