// 匿名能力开关：给前端登录/注册页用，决定 UI 上哪些入口可见。
//
// 当前暴露全局新账号注册能力，以及塘主在 SmsSettings 里能切的「手机号注册 / 登录」四个开关
// （CN / Global × Register / Login）；第三方登录由 feature gate 决定，
// 第三方登录入口不通过此接口暴露。
package service

import (
	"strings"

	"github.com/askie/grix/backend/internal/featuregate"

	"github.com/askie/grix/backend/internal/systemsetting"
)

// AuthMethodsView 给前端读的能力开关；字段越窄越好，加新开关时按需扩展。
type AuthMethodsView struct {
	RegistrationEnabled  bool   `json:"registration_enabled"`
	Region               string `json:"region"`
	PhoneLoginEnabled    bool   `json:"phone_login_enabled"`
	PhoneRegisterEnabled bool   `json:"phone_register_enabled"`
}

// GetAuthMethods 返回当前区域的认证能力开关。
//
// region 取值：
//   - "cn"      → 读 PhoneLoginEnabledCN / PhoneRegisterEnabledCN
//   - 其他/空    → 读 PhoneLoginEnabledGlobal / PhoneRegisterEnabledGlobal
//
// 注册策略读取失败时仅拒绝新账号；不影响已有账号的手机号登录能力。
// 短信配置读取失败时手机号能力为 false。接口只暴露最小能力。
func GetAuthMethods(region string) AuthMethodsView {
	r := normalizeMethodsRegion(region)
	view := AuthMethodsView{Region: r}
	view.RegistrationEnabled, _ = featuregate.RegistrationEnabled()
	s, err := systemsetting.GetSmsSettings()
	if err != nil {
		return view
	}
	if r == "cn" {
		view.PhoneLoginEnabled = s.PhoneLoginEnabledCN
		view.PhoneRegisterEnabled = s.PhoneRegisterEnabledCN
	} else {
		view.PhoneLoginEnabled = s.PhoneLoginEnabledGlobal
		view.PhoneRegisterEnabled = s.PhoneRegisterEnabledGlobal
	}
	view.PhoneRegisterEnabled = view.PhoneRegisterEnabled && view.RegistrationEnabled
	return view
}

func normalizeMethodsRegion(region string) string {
	r := strings.ToLower(strings.TrimSpace(region))
	if r == "cn" {
		return "cn"
	}
	return "global"
}
