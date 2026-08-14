// 匿名能力开关：给前端登录/注册页用，决定 UI 上哪些入口可见。
//
// 当前只暴露塘主在 SmsSettings 里能切的「手机号注册 / 登录」四个开关
// （CN / Global × Register / Login）；邮箱链路一直可用，第三方登录由 build flag 决定，
// 这两类暂不通过此接口暴露，避免无意义的策略蔓延。
package service

import (
	"strings"

	"github.com/askie/grix/backend/internal/systemsetting"
)

// AuthMethodsView 给前端读的能力开关；字段越窄越好，加新开关时按需扩展。
type AuthMethodsView struct {
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
// 任何读取失败都返回保守默认（全部 false），让前端跟"没开"一致；
// 失败原因记到日志，但接口不暴露错误避免给探测者信号。
func GetAuthMethods(region string) AuthMethodsView {
	r := normalizeMethodsRegion(region)
	view := AuthMethodsView{Region: r}
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
	return view
}

func normalizeMethodsRegion(region string) string {
	r := strings.ToLower(strings.TrimSpace(region))
	if r == "cn" {
		return "cn"
	}
	return "global"
}
