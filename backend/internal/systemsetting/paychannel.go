// Package systemsetting 新增 pay_channel 设置组：支付宝 / PayPal 商户凭证。
//
// 敏感字段（应用私钥、支付宝公钥、Client Secret）按 AES-GCM 加密后落
// system_settings.value 的 JSON 里，主密钥派生自 config.C.Server.PayCryptoSecret，
// 与语音 BYOK / 短信 ak-sk 使用不同的加密域（secretcrypto.EncryptPay），
// 兜底回退 JWT secret。
//
// cmd/pay 是独立进程，无法接收 api 进程内的 reload hook，靠短 TTL 缓存
// 定期重读，塘主改配置后无需重启 pay 服务，最长 cacheTTL 后各副本自动生效。
package systemsetting

import (
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/secretcrypto"
	"github.com/askie/grix/backend/internal/store"
	"gorm.io/datatypes"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const payChannelSettingKey = "pay_channel"
const payChannelSettingsCacheTTL = 30 * time.Second

var payChannelSettingsNow = time.Now

var payChannelSettingsCache struct {
	mu        sync.RWMutex
	value     PayChannelSettings
	expiresAt time.Time
	loaded    bool
}

// PayChannelSettings 是支付通道的商户凭证配置。
type PayChannelSettings struct {
	Alipay PayAlipaySecret `json:"alipay"`
	Paypal PayPaypalSecret `json:"paypal"`
}

// PayAlipaySecret 是支付宝商户凭证。
type PayAlipaySecret struct {
	Enabled         bool   `json:"enabled"`
	Sandbox         bool   `json:"sandbox"`
	AppID           string `json:"app_id"`
	PrivateKey      string `json:"private_key"`       // 落库前加密
	AlipayPublicKey string `json:"alipay_public_key"` // 落库前加密
	SignType        string `json:"sign_type"`         // 固定 RSA2
}

// PayPaypalSecret 是 PayPal 应用凭证。
type PayPaypalSecret struct {
	Enabled      bool   `json:"enabled"`
	Sandbox      bool   `json:"sandbox"`
	ClientID     string `json:"client_id"`
	ClientSecret string `json:"client_secret"` // 落库前加密
	WebhookID    string `json:"webhook_id"`
}

// DefaultPayChannelSettings 安装态默认值：两个通道都不启用，塘主填好凭证后手动开启。
func DefaultPayChannelSettings() PayChannelSettings {
	return PayChannelSettings{
		Alipay: PayAlipaySecret{Sandbox: true, SignType: "RSA2"},
		Paypal: PayPaypalSecret{Sandbox: true},
	}
}

// GetPayChannelSettings 取当前生效配置（含解密后的明文凭证，仅服务端进程内使用）。
func GetPayChannelSettings() (PayChannelSettings, error) {
	now := payChannelSettingsNow()
	if settings, ok := getPayChannelSettingsFromCache(now); ok {
		return settings, nil
	}

	settings := DefaultPayChannelSettings()
	if store.DB == nil {
		setPayChannelSettingsCache(settings, now)
		return settings, nil
	}
	var row model.SystemSetting
	if err := store.DB.First(&row, "key = ?", payChannelSettingKey).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			setPayChannelSettingsCache(settings, now)
			return settings, nil
		}
		return PayChannelSettings{}, err
	}
	if len(row.Value) == 0 {
		setPayChannelSettingsCache(settings, now)
		return settings, nil
	}
	if err := json.Unmarshal(row.Value, &settings); err != nil {
		return PayChannelSettings{}, err
	}
	if err := decryptPayChannelSecrets(&settings); err != nil {
		return PayChannelSettings{}, err
	}
	setPayChannelSettingsCache(settings, now)
	return settings, nil
}

// SavePayChannelSettings 落库；敏感字段加密后写入；写入后立即失效缓存供下次读取。
func SavePayChannelSettings(settings PayChannelSettings, updatedBy *int64) error {
	plain := settings
	encrypted := settings
	if err := encryptPayChannelSecrets(&encrypted); err != nil {
		return err
	}
	raw, err := json.Marshal(encrypted)
	if err != nil {
		return err
	}
	row := model.SystemSetting{
		Key:       payChannelSettingKey,
		Value:     datatypes.JSON(raw),
		UpdatedBy: updatedBy,
	}
	if err := store.DB.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "key"}},
		DoUpdates: clause.AssignmentColumns([]string{"value", "updated_by", "updated_at"}),
	}).Create(&row).Error; err != nil {
		return err
	}
	setPayChannelSettingsCache(plain, payChannelSettingsNow())
	return nil
}

// InvalidatePayChannelSettingsCache 清缓存（手动测试时使用）。
func InvalidatePayChannelSettingsCache() {
	payChannelSettingsCache.mu.Lock()
	payChannelSettingsCache.loaded = false
	payChannelSettingsCache.expiresAt = time.Time{}
	payChannelSettingsCache.value = PayChannelSettings{}
	payChannelSettingsCache.mu.Unlock()
}

func getPayChannelSettingsFromCache(now time.Time) (PayChannelSettings, bool) {
	payChannelSettingsCache.mu.RLock()
	defer payChannelSettingsCache.mu.RUnlock()
	if !payChannelSettingsCache.loaded {
		return PayChannelSettings{}, false
	}
	if now.After(payChannelSettingsCache.expiresAt) {
		return PayChannelSettings{}, false
	}
	return payChannelSettingsCache.value, true
}

func setPayChannelSettingsCache(settings PayChannelSettings, now time.Time) {
	payChannelSettingsCache.mu.Lock()
	payChannelSettingsCache.value = settings
	payChannelSettingsCache.expiresAt = now.Add(payChannelSettingsCacheTTL)
	payChannelSettingsCache.loaded = true
	payChannelSettingsCache.mu.Unlock()
}

func encryptPayChannelSecrets(s *PayChannelSettings) error {
	if err := encryptPayField(&s.Alipay.PrivateKey); err != nil {
		return err
	}
	if err := encryptPayField(&s.Alipay.AlipayPublicKey); err != nil {
		return err
	}
	if err := encryptPayField(&s.Paypal.ClientSecret); err != nil {
		return err
	}
	return nil
}

func decryptPayChannelSecrets(s *PayChannelSettings) error {
	if err := decryptPayField("alipay.private_key", &s.Alipay.PrivateKey); err != nil {
		return err
	}
	if err := decryptPayField("alipay.alipay_public_key", &s.Alipay.AlipayPublicKey); err != nil {
		return err
	}
	if err := decryptPayField("paypal.client_secret", &s.Paypal.ClientSecret); err != nil {
		return err
	}
	return nil
}

func encryptPayField(p *string) error {
	if *p == "" {
		return nil
	}
	enc, err := secretcrypto.EncryptPay(*p)
	if err != nil {
		return err
	}
	*p = enc
	return nil
}

// decryptPayField 解密一个支付密钥字段。解密失败一律返回错误，绝不降级成明文。
//
// 这里曾经"兼容老数据"地把解密失败的值当明文用。但 pay_channel 是全新设置组，从第一天
// 起就没有历史明文数据要兼容——那段降级唯一的实际效果是：PayCryptoSecret 被意外轮换、
// 或密文损坏时，系统不报错，而是拿一段乱码当密钥去调 PayPal / 支付宝，在支付链路的最深处
// 抛出一堆看不懂的底层错误。钱的事不能静默失败：直接在配置这一层失败，把原因说清楚。
func decryptPayField(field string, p *string) error {
	if *p == "" {
		return nil
	}
	dec, err := secretcrypto.DecryptPay(*p)
	if err != nil {
		return fmt.Errorf("systemsetting: 支付通道密钥 %s 解密失败（PayCryptoSecret 是否被轮换或密文已损坏？）: %w", field, err)
	}
	*p = dec
	return nil
}
