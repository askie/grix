// Package systemsetting 已有的 auth/group/voice_model 等设置组之外，
// 新增 sms 设置组：手机号无密码短信登录注册的开关 + 阿里/AWS SNS provider 配置。
//
// 敏感字段（ak/sk）按 AES-GCM 加密后落 system_settings.value 的 JSON 里，
// 主密钥派生自 config.C.Server.VoiceCryptoSecret（与 BYOK 共用），
// 兜底回退 JWT secret。
package systemsetting

import (
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/secretcrypto"
	"github.com/askie/grix/backend/internal/store"
	"gorm.io/datatypes"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const smsSettingKey = "sms"
const smsSettingsCacheTTL = time.Minute

var smsSettingsNow = time.Now

var smsSettingsCache struct {
	mu        sync.RWMutex
	value     SmsSettings
	expiresAt time.Time
	loaded    bool
}

// 短信场景 → 开关键。CN 区与 Global 区分别开关注册/登录。
type SmsSettings struct {
	PhoneRegisterEnabledCN     bool     `json:"phone_register_enabled_cn"`
	PhoneRegisterEnabledGlobal bool     `json:"phone_register_enabled_global"`
	PhoneLoginEnabledCN        bool     `json:"phone_login_enabled_cn"`
	PhoneLoginEnabledGlobal    bool     `json:"phone_login_enabled_global"`
	AllowedCountryCodesCN      []string `json:"allowed_country_codes_cn"`
	AllowedCountryCodesGlobal  []string `json:"allowed_country_codes_global"`

	CnSmsProvider     string          `json:"cn_sms_provider"`     // aliyun
	GlobalSmsProvider string          `json:"global_sms_provider"` // aws_sns
	Aliyun            SmsAliyunSecret `json:"aliyun"`
	AwsSns            SmsAwsSnsSecret `json:"aws_sns"`
}

type SmsAliyunSecret struct {
	RegionID             string `json:"region_id"`
	AccessKeyID          string `json:"access_key_id"`     // 落库前加密
	AccessKeySecret      string `json:"access_key_secret"` // 落库前加密
	SignName             string `json:"sign_name"`
	TemplateCodeRegister string `json:"template_code_register"`
	TemplateCodeLogin    string `json:"template_code_login"`
	TemplateCodeReset    string `json:"template_code_reset"`
	// TemplateCodeMarketing 营销短信模板号（变量 ${content}），用于触达模块群发。
	TemplateCodeMarketing string `json:"template_code_marketing"`
	// TemplateCodeNotify 通知短信模板号（变量 ${content}）。阿里云把通知类与推广类分开报备，
	// 故障告知这类内容必须走通知模板，不能复用营销模板。
	TemplateCodeNotify string `json:"template_code_notify"`
}

type SmsAwsSnsSecret struct {
	Region          string `json:"region"`
	AccessKeyID     string `json:"access_key_id"`     // 落库前加密
	AccessKeySecret string `json:"access_key_secret"` // 落库前加密
	SenderID        string `json:"sender_id"`
}

// DefaultSmsSettings 安装态默认值：保守关闭所有开关，避免上线即被刷。
// 上线后塘主在 admin 后台手动启用并填 ak/sk。
func DefaultSmsSettings() SmsSettings {
	return SmsSettings{
		PhoneRegisterEnabledCN:     false,
		PhoneRegisterEnabledGlobal: false,
		PhoneLoginEnabledCN:        false,
		PhoneLoginEnabledGlobal:    false,
		AllowedCountryCodesCN:      []string{"+86"},
		AllowedCountryCodesGlobal:  []string{"*"}, // * = 除 +86 外全部
		CnSmsProvider:              "aliyun",
		GlobalSmsProvider:          "aws_sns",
	}
}

// GetSmsSettings 取当前生效配置（含解密的 ak/sk 明文，仅服务端进程内使用）。
func GetSmsSettings() (SmsSettings, error) {
	now := smsSettingsNow()
	if settings, ok := getSmsSettingsFromCache(now); ok {
		return settings, nil
	}

	settings := DefaultSmsSettings()
	if store.DB == nil {
		setSmsSettingsCache(settings, now)
		return settings, nil
	}
	var row model.SystemSetting
	if err := store.DB.First(&row, "key = ?", smsSettingKey).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			setSmsSettingsCache(settings, now)
			return settings, nil
		}
		return SmsSettings{}, err
	}
	if len(row.Value) == 0 {
		setSmsSettingsCache(settings, now)
		return settings, nil
	}
	if err := json.Unmarshal(row.Value, &settings); err != nil {
		return SmsSettings{}, err
	}
	if err := decryptSmsSecrets(&settings); err != nil {
		return SmsSettings{}, err
	}
	setSmsSettingsCache(settings, now)
	return settings, nil
}

// SaveSmsSettings 落库；ak/sk 加密后写入；写入后立即失效缓存供下次读取。
// 同时通知 identity.Registry hook（由调用方注入，避免循环依赖）。
var smsReloadHook func(SmsSettings)

// RegisterSmsReloadHook 由 cmd/api 启动时注入；塘主保存配置后回调，重建 identity providers。
func RegisterSmsReloadHook(fn func(SmsSettings)) {
	smsReloadHook = fn
}

func SaveSmsSettings(settings SmsSettings, updatedBy *int64) error {
	// 副本：encrypt 前先暂存明文以便缓存继续返回明文用
	plain := settings
	encrypted := settings
	if err := encryptSmsSecrets(&encrypted); err != nil {
		return err
	}
	raw, err := json.Marshal(encrypted)
	if err != nil {
		return err
	}
	row := model.SystemSetting{
		Key:       smsSettingKey,
		Value:     datatypes.JSON(raw),
		UpdatedBy: updatedBy,
	}
	if err := store.DB.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "key"}},
		DoUpdates: clause.AssignmentColumns([]string{"value", "updated_by", "updated_at"}),
	}).Create(&row).Error; err != nil {
		return err
	}
	setSmsSettingsCache(plain, smsSettingsNow())
	if smsReloadHook != nil {
		smsReloadHook(plain)
	}
	return nil
}

// InvalidateSmsSettingsCache 清缓存（手动测试时使用）。
func InvalidateSmsSettingsCache() {
	smsSettingsCache.mu.Lock()
	smsSettingsCache.loaded = false
	smsSettingsCache.expiresAt = time.Time{}
	smsSettingsCache.value = SmsSettings{}
	smsSettingsCache.mu.Unlock()
}

func getSmsSettingsFromCache(now time.Time) (SmsSettings, bool) {
	smsSettingsCache.mu.RLock()
	defer smsSettingsCache.mu.RUnlock()
	if !smsSettingsCache.loaded {
		return SmsSettings{}, false
	}
	if now.After(smsSettingsCache.expiresAt) {
		return SmsSettings{}, false
	}
	return smsSettingsCache.value, true
}

func setSmsSettingsCache(settings SmsSettings, now time.Time) {
	smsSettingsCache.mu.Lock()
	smsSettingsCache.value = settings
	smsSettingsCache.expiresAt = now.Add(smsSettingsCacheTTL)
	smsSettingsCache.loaded = true
	smsSettingsCache.mu.Unlock()
}

func encryptSmsSecrets(s *SmsSettings) error {
	if err := encryptField(&s.Aliyun.AccessKeyID); err != nil {
		return err
	}
	if err := encryptField(&s.Aliyun.AccessKeySecret); err != nil {
		return err
	}
	if err := encryptField(&s.AwsSns.AccessKeyID); err != nil {
		return err
	}
	if err := encryptField(&s.AwsSns.AccessKeySecret); err != nil {
		return err
	}
	return nil
}

func decryptSmsSecrets(s *SmsSettings) error {
	if err := decryptField(&s.Aliyun.AccessKeyID); err != nil {
		return err
	}
	if err := decryptField(&s.Aliyun.AccessKeySecret); err != nil {
		return err
	}
	if err := decryptField(&s.AwsSns.AccessKeyID); err != nil {
		return err
	}
	if err := decryptField(&s.AwsSns.AccessKeySecret); err != nil {
		return err
	}
	return nil
}

func encryptField(p *string) error {
	v := strings.TrimSpace(*p)
	if v == "" {
		*p = ""
		return nil
	}
	enc, err := secretcrypto.Encrypt(v)
	if err != nil {
		return err
	}
	*p = enc
	return nil
}

func decryptField(p *string) error {
	v := strings.TrimSpace(*p)
	if v == "" {
		*p = ""
		return nil
	}
	dec, err := secretcrypto.Decrypt(v)
	if err != nil {
		// 兼容老数据/明文降级：解密失败时按明文使用，避免锁死
		return nil
	}
	*p = dec
	return nil
}
