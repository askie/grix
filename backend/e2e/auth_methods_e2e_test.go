package e2e

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/store"
	"github.com/askie/grix/backend/internal/systemsetting"
	"gorm.io/datatypes"
)

// writeSmsSettings 用 e2e 同款方式更新 SmsSettings：先 update value，再失效缓存。
func writeSmsSettings(t *testing.T, cfg systemsetting.SmsSettings) {
	t.Helper()
	raw, _ := json.Marshal(cfg)
	row := model.SystemSetting{Key: "sms", Value: datatypes.JSON(raw)}
	if err := store.DB.Where("key = ?", row.Key).Assign(row).FirstOrCreate(&row).Error; err != nil {
		t.Fatalf("write sms settings: %v", err)
	}
	systemsetting.InvalidateSmsSettingsCache()
}

// TestAuthMethods_AnonymousRespectsCNOnly：CN 开 / Global 关时，?region=cn 全开、?region=global 全关。
func TestAuthMethods_AnonymousRespectsCNOnly(t *testing.T) {
	ctx := setupE2E(t)
	defer ctx.cleanup()

	writeSmsSettings(t, systemsetting.SmsSettings{
		PhoneRegisterEnabledCN:     true,
		PhoneLoginEnabledCN:        true,
		PhoneRegisterEnabledGlobal: false,
		PhoneLoginEnabledGlobal:    false,
	})

	w := ctx.doReq(t, "GET", "/v1/auth/methods?region=cn", "", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("cn: code=%d body=%s", w.Code, w.Body.String())
	}
	data := parseAuthMethodsData(t, w.Body.Bytes())
	if data["region"] != "cn" {
		t.Fatalf("region not cn: %v", data)
	}
	if data["phone_login_enabled"] != true || data["phone_register_enabled"] != true {
		t.Fatalf("cn should be all true, got %v", data)
	}

	w = ctx.doReq(t, "GET", "/v1/auth/methods?region=global", "", nil)
	data = parseAuthMethodsData(t, w.Body.Bytes())
	if data["phone_login_enabled"] != false || data["phone_register_enabled"] != false {
		t.Fatalf("global should be all false, got %v", data)
	}
}

// TestAuthMethods_DisableCnLoginAfterAdminPatchAffectsClient
// 验证塘主改一次 SmsSettings + 失效缓存后，匿名客户端立刻拉到新值。
func TestAuthMethods_DisableCnLoginAfterAdminPatchAffectsClient(t *testing.T) {
	ctx := setupE2E(t)
	defer ctx.cleanup()

	// 初始：CN 全开
	writeSmsSettings(t, systemsetting.SmsSettings{
		PhoneLoginEnabledCN:    true,
		PhoneRegisterEnabledCN: true,
	})
	w := ctx.doReq(t, "GET", "/v1/auth/methods?region=cn", "", nil)
	data := parseAuthMethodsData(t, w.Body.Bytes())
	if data["phone_login_enabled"] != true {
		t.Fatalf("initial cn login should be true: %v", data)
	}

	// 塘主关掉 cn login
	writeSmsSettings(t, systemsetting.SmsSettings{
		PhoneLoginEnabledCN:    false,
		PhoneRegisterEnabledCN: true,
	})

	w = ctx.doReq(t, "GET", "/v1/auth/methods?region=cn", "", nil)
	data = parseAuthMethodsData(t, w.Body.Bytes())
	if data["phone_login_enabled"] != false {
		t.Fatalf("after disable cn login should be false: %v", data)
	}
	if data["phone_register_enabled"] != true {
		t.Fatalf("register should stay true: %v", data)
	}
}

// TestAuthMethods_NoRegionDefaultsToGlobal：不带 region 参数时按 global 读。
func TestAuthMethods_NoRegionDefaultsToGlobal(t *testing.T) {
	ctx := setupE2E(t)
	defer ctx.cleanup()

	writeSmsSettings(t, systemsetting.SmsSettings{
		PhoneLoginEnabledCN:    true,
		PhoneLoginEnabledGlobal: false,
	})

	w := ctx.doReq(t, "GET", "/v1/auth/methods", "", nil)
	data := parseAuthMethodsData(t, w.Body.Bytes())
	if data["region"] != "global" {
		t.Fatalf("no region should default to global, got %v", data["region"])
	}
	if data["phone_login_enabled"] != false {
		t.Fatalf("global login should be false, got %v", data)
	}
}

func parseAuthMethodsData(t *testing.T, body []byte) map[string]any {
	t.Helper()
	var resp struct {
		Code int            `json:"code"`
		Data map[string]any `json:"data"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("unmarshal: %v\n%s", err, string(body))
	}
	if resp.Code != 0 {
		t.Fatalf("non-zero code: %d body=%s", resp.Code, string(body))
	}
	return resp.Data
}
