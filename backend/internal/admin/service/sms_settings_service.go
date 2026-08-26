// 塘主 admin 短信设置：读 + 写 + 测试发送。
// 写入时统一通过 systemsetting.SaveSmsSettings 加密落库，并触发 reload hook 重建 identity providers。
package service

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/askie/grix/backend/internal/api/service/identity"
	"github.com/askie/grix/backend/internal/pkg/secretcrypto"
	"github.com/askie/grix/backend/internal/store"
	"github.com/askie/grix/backend/internal/systemsetting"
	"gorm.io/gorm"
)

// SmsSettingsView 塘主面板看到的内容：ak/sk 用 Hint 末四位脱敏，避免暴露完整密钥。
type SmsSettingsView struct {
	PhoneRegisterEnabledCN     bool          `json:"phone_register_enabled_cn"`
	PhoneRegisterEnabledGlobal bool          `json:"phone_register_enabled_global"`
	PhoneLoginEnabledCN        bool          `json:"phone_login_enabled_cn"`
	PhoneLoginEnabledGlobal    bool          `json:"phone_login_enabled_global"`
	AllowedCountryCodesCN      []string      `json:"allowed_country_codes_cn"`
	AllowedCountryCodesGlobal  []string      `json:"allowed_country_codes_global"`
	CnSmsProvider              string        `json:"cn_sms_provider"`
	GlobalSmsProvider          string        `json:"global_sms_provider"`
	Aliyun                     SmsAliyunView `json:"aliyun"`
	AwsSns                     SmsAwsSnsView `json:"aws_sns"`
}

type SmsAliyunView struct {
	RegionID              string `json:"region_id"`
	AccessKeyIDHint       string `json:"access_key_id_hint"`     // 末四位
	AccessKeySecretHint   string `json:"access_key_secret_hint"` // 末四位
	SignName              string `json:"sign_name"`
	TemplateCodeRegister  string `json:"template_code_register"`
	TemplateCodeLogin     string `json:"template_code_login"`
	TemplateCodeReset     string `json:"template_code_reset"`
	TemplateCodeMarketing string `json:"template_code_marketing"`
	TemplateCodeNotify    string `json:"template_code_notify"`
}

type SmsAwsSnsView struct {
	Region              string `json:"region"`
	AccessKeyIDHint     string `json:"access_key_id_hint"`
	AccessKeySecretHint string `json:"access_key_secret_hint"`
	SenderID            string `json:"sender_id"`
}

// SmsSettingsPatch 塘主提交的修改请求。ak/sk 留空表示保持原值不动；填了即覆盖。
type SmsSettingsPatch struct {
	PhoneRegisterEnabledCN     bool           `json:"phone_register_enabled_cn"`
	PhoneRegisterEnabledGlobal bool           `json:"phone_register_enabled_global"`
	PhoneLoginEnabledCN        bool           `json:"phone_login_enabled_cn"`
	PhoneLoginEnabledGlobal    bool           `json:"phone_login_enabled_global"`
	AllowedCountryCodesCN      []string       `json:"allowed_country_codes_cn"`
	AllowedCountryCodesGlobal  []string       `json:"allowed_country_codes_global"`
	CnSmsProvider              string         `json:"cn_sms_provider"`
	GlobalSmsProvider          string         `json:"global_sms_provider"`
	Aliyun                     SmsAliyunPatch `json:"aliyun"`
	AwsSns                     SmsAwsSnsPatch `json:"aws_sns"`
}

type SmsAliyunPatch struct {
	RegionID              string `json:"region_id"`
	AccessKeyID           string `json:"access_key_id"`     // 留空保持
	AccessKeySecret       string `json:"access_key_secret"` // 留空保持
	SignName              string `json:"sign_name"`
	TemplateCodeRegister  string `json:"template_code_register"`
	TemplateCodeLogin     string `json:"template_code_login"`
	TemplateCodeReset     string `json:"template_code_reset"`
	TemplateCodeMarketing string `json:"template_code_marketing"`
	TemplateCodeNotify    string `json:"template_code_notify"`
}

type SmsAwsSnsPatch struct {
	Region          string `json:"region"`
	AccessKeyID     string `json:"access_key_id"`     // 留空保持
	AccessKeySecret string `json:"access_key_secret"` // 留空保持
	SenderID        string `json:"sender_id"`
}

// GetSmsSettingsView 读取脱敏后的配置供塘主面板展示。
func GetSmsSettingsView() (SmsSettingsView, error) {
	s, err := systemsetting.GetSmsSettings()
	if err != nil {
		return SmsSettingsView{}, err
	}
	return SmsSettingsView{
		PhoneRegisterEnabledCN:     s.PhoneRegisterEnabledCN,
		PhoneRegisterEnabledGlobal: s.PhoneRegisterEnabledGlobal,
		PhoneLoginEnabledCN:        s.PhoneLoginEnabledCN,
		PhoneLoginEnabledGlobal:    s.PhoneLoginEnabledGlobal,
		AllowedCountryCodesCN:      s.AllowedCountryCodesCN,
		AllowedCountryCodesGlobal:  s.AllowedCountryCodesGlobal,
		CnSmsProvider:              s.CnSmsProvider,
		GlobalSmsProvider:          s.GlobalSmsProvider,
		Aliyun: SmsAliyunView{
			RegionID:              s.Aliyun.RegionID,
			AccessKeyIDHint:       secretcrypto.Hint(s.Aliyun.AccessKeyID),
			AccessKeySecretHint:   secretcrypto.Hint(s.Aliyun.AccessKeySecret),
			SignName:              s.Aliyun.SignName,
			TemplateCodeRegister:  s.Aliyun.TemplateCodeRegister,
			TemplateCodeLogin:     s.Aliyun.TemplateCodeLogin,
			TemplateCodeReset:     s.Aliyun.TemplateCodeReset,
			TemplateCodeMarketing: s.Aliyun.TemplateCodeMarketing,
			TemplateCodeNotify:    s.Aliyun.TemplateCodeNotify,
		},
		AwsSns: SmsAwsSnsView{
			Region:              s.AwsSns.Region,
			AccessKeyIDHint:     secretcrypto.Hint(s.AwsSns.AccessKeyID),
			AccessKeySecretHint: secretcrypto.Hint(s.AwsSns.AccessKeySecret),
			SenderID:            s.AwsSns.SenderID,
		},
	}, nil
}

// UpdateSmsSettings 合并 patch（空 ak/sk 保留原值）→ 落库 → 触发 reload。
func UpdateSmsSettings(adminID int64, patch SmsSettingsPatch, clientIP, userAgent string) error {
	current, err := systemsetting.GetSmsSettings()
	if err != nil {
		return err
	}
	next := current
	next.PhoneRegisterEnabledCN = patch.PhoneRegisterEnabledCN
	next.PhoneRegisterEnabledGlobal = patch.PhoneRegisterEnabledGlobal
	next.PhoneLoginEnabledCN = patch.PhoneLoginEnabledCN
	next.PhoneLoginEnabledGlobal = patch.PhoneLoginEnabledGlobal
	if patch.AllowedCountryCodesCN != nil {
		next.AllowedCountryCodesCN = sanitizeCountryList(patch.AllowedCountryCodesCN, []string{"+86"})
	}
	if patch.AllowedCountryCodesGlobal != nil {
		next.AllowedCountryCodesGlobal = sanitizeCountryList(patch.AllowedCountryCodesGlobal, []string{"*"})
	}
	if v := strings.TrimSpace(patch.CnSmsProvider); v != "" {
		next.CnSmsProvider = v
	}
	if v := strings.TrimSpace(patch.GlobalSmsProvider); v != "" {
		next.GlobalSmsProvider = v
	}

	// 阿里：保留 ak/sk 原值（patch 空时）
	next.Aliyun.RegionID = strings.TrimSpace(patch.Aliyun.RegionID)
	if v := strings.TrimSpace(patch.Aliyun.AccessKeyID); v != "" {
		next.Aliyun.AccessKeyID = v
	}
	if v := strings.TrimSpace(patch.Aliyun.AccessKeySecret); v != "" {
		next.Aliyun.AccessKeySecret = v
	}
	next.Aliyun.SignName = strings.TrimSpace(patch.Aliyun.SignName)
	next.Aliyun.TemplateCodeRegister = strings.TrimSpace(patch.Aliyun.TemplateCodeRegister)
	next.Aliyun.TemplateCodeLogin = strings.TrimSpace(patch.Aliyun.TemplateCodeLogin)
	next.Aliyun.TemplateCodeReset = strings.TrimSpace(patch.Aliyun.TemplateCodeReset)
	next.Aliyun.TemplateCodeMarketing = strings.TrimSpace(patch.Aliyun.TemplateCodeMarketing)
	next.Aliyun.TemplateCodeNotify = strings.TrimSpace(patch.Aliyun.TemplateCodeNotify)

	// AWS SNS
	next.AwsSns.Region = strings.TrimSpace(patch.AwsSns.Region)
	if v := strings.TrimSpace(patch.AwsSns.AccessKeyID); v != "" {
		next.AwsSns.AccessKeyID = v
	}
	if v := strings.TrimSpace(patch.AwsSns.AccessKeySecret); v != "" {
		next.AwsSns.AccessKeySecret = v
	}
	next.AwsSns.SenderID = strings.TrimSpace(patch.AwsSns.SenderID)

	if err := store.DB.Transaction(func(tx *gorm.DB) error {
		// 写入由 systemsetting 包内完成（含加密/缓存/reload hook）
		if err := systemsetting.SaveSmsSettings(next, &adminID); err != nil {
			return err
		}
		return recordOperationTx(tx, adminID, "sms_settings_update", "system_setting", "sms", map[string]any{
			"cn_provider":     next.CnSmsProvider,
			"global_provider": next.GlobalSmsProvider,
			"register_cn":     next.PhoneRegisterEnabledCN,
			"register_global": next.PhoneRegisterEnabledGlobal,
			"login_cn":        next.PhoneLoginEnabledCN,
			"login_global":    next.PhoneLoginEnabledGlobal,
		}, clientIP, userAgent)
	}); err != nil {
		return err
	}
	return nil
}

// TestSendSms 给指定手机号发一条测试码（不进入业务限流，仅用于配置自检）。
func TestSendSms(phoneE164 string, region string) error {
	phone, err := identity.SanitizePhoneE164(phoneE164)
	if err != nil {
		return err
	}
	country, err := identity.ParseCountryCode(phone)
	if err != nil {
		return err
	}
	actualRegion := identity.RegionForCountry(country)
	if strings.TrimSpace(region) != "" && region != actualRegion {
		return errors.New("手机号区号与所选区域不一致")
	}

	providerName := identityProviderForRegionAdmin(actualRegion)
	p, err := identity.Default().GetSms(providerName)
	if err != nil {
		return errors.New("短信通道未配置")
	}
	if err := p.HealthCheck(context.Background()); err != nil {
		return err
	}
	store := identity.SmsCodeStore{}
	code, err := store.Generate6Digit()
	if err != nil {
		return errors.New("生成测试码失败")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return p.Send(ctx, identity.SendSmsRequest{
		PhoneE164:   phone,
		CountryCode: country,
		Scene:       identity.SmsSceneLogin,
		Code:        code,
		Lang:        "zh",
	})
}

func identityProviderForRegionAdmin(region string) string {
	if region == "cn" {
		return "phone_sms_cn"
	}
	return "phone_sms_global"
}

func sanitizeCountryList(in []string, fallback []string) []string {
	out := make([]string, 0, len(in))
	for _, v := range in {
		v = strings.TrimSpace(v)
		if v == "" {
			continue
		}
		if v != "*" && !strings.HasPrefix(v, "+") {
			continue
		}
		out = append(out, v)
	}
	if len(out) == 0 {
		return fallback
	}
	return out
}
