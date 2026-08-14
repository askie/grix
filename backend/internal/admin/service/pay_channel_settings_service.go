// 塘主 admin 支付通道设置：读 + 写 + 测试连接。
// 写入时统一通过 systemsetting.SavePayChannelSettings 加密落库；
// cmd/pay 是独立进程，靠短 TTL 缓存重读生效，无需 reload hook。
package service

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/askie/grix/backend/internal/pay/channel/alipay"
	"github.com/askie/grix/backend/internal/pay/channel/paypal"
	"github.com/askie/grix/backend/internal/pkg/secretcrypto"
	"github.com/askie/grix/backend/internal/store"
	"github.com/askie/grix/backend/internal/systemsetting"
	"gorm.io/gorm"
)

// PayChannelSettingsView 塘主面板看到的内容：敏感字段用 Hint 末四位脱敏。
type PayChannelSettingsView struct {
	Alipay PayAlipayView `json:"alipay"`
	Paypal PayPaypalView `json:"paypal"`
}

type PayAlipayView struct {
	Enabled             bool   `json:"enabled"`
	Sandbox             bool   `json:"sandbox"`
	AppID               string `json:"app_id"`
	PrivateKeyHint      string `json:"private_key_hint"`
	AlipayPublicKeyHint string `json:"alipay_public_key_hint"`
	SignType            string `json:"sign_type"`
}

type PayPaypalView struct {
	Enabled          bool   `json:"enabled"`
	Sandbox          bool   `json:"sandbox"`
	ClientID         string `json:"client_id"`
	ClientSecretHint string `json:"client_secret_hint"`
	WebhookID        string `json:"webhook_id"`
}

// PayChannelSettingsPatch 塘主提交的修改请求。私钥/公钥/Secret 留空表示保持原值不动。
type PayChannelSettingsPatch struct {
	Alipay PayAlipayPatch `json:"alipay"`
	Paypal PayPaypalPatch `json:"paypal"`
}

type PayAlipayPatch struct {
	Enabled         bool   `json:"enabled"`
	Sandbox         bool   `json:"sandbox"`
	AppID           string `json:"app_id"`
	PrivateKey      string `json:"private_key"`       // 留空保持
	AlipayPublicKey string `json:"alipay_public_key"` // 留空保持
	SignType        string `json:"sign_type"`
}

type PayPaypalPatch struct {
	Enabled      bool   `json:"enabled"`
	Sandbox      bool   `json:"sandbox"`
	ClientID     string `json:"client_id"`
	ClientSecret string `json:"client_secret"` // 留空保持
	WebhookID    string `json:"webhook_id"`
}

// GetPayChannelSettingsView 读取脱敏后的配置供塘主面板展示。
func GetPayChannelSettingsView() (PayChannelSettingsView, error) {
	s, err := systemsetting.GetPayChannelSettings()
	if err != nil {
		return PayChannelSettingsView{}, err
	}
	return PayChannelSettingsView{
		Alipay: PayAlipayView{
			Enabled:             s.Alipay.Enabled,
			Sandbox:             s.Alipay.Sandbox,
			AppID:               s.Alipay.AppID,
			PrivateKeyHint:      secretcrypto.Hint(s.Alipay.PrivateKey),
			AlipayPublicKeyHint: secretcrypto.Hint(s.Alipay.AlipayPublicKey),
			SignType:            s.Alipay.SignType,
		},
		Paypal: PayPaypalView{
			Enabled:          s.Paypal.Enabled,
			Sandbox:          s.Paypal.Sandbox,
			ClientID:         s.Paypal.ClientID,
			ClientSecretHint: secretcrypto.Hint(s.Paypal.ClientSecret),
			WebhookID:        s.Paypal.WebhookID,
		},
	}, nil
}

// UpdatePayChannelSettings 合并 patch（空私钥/Secret 保留原值）→ 落库。
func UpdatePayChannelSettings(adminID int64, patch PayChannelSettingsPatch, clientIP, userAgent string) error {
	current, err := systemsetting.GetPayChannelSettings()
	if err != nil {
		return err
	}
	next := current

	next.Alipay.Enabled = patch.Alipay.Enabled
	next.Alipay.Sandbox = patch.Alipay.Sandbox
	next.Alipay.AppID = strings.TrimSpace(patch.Alipay.AppID)
	if v := strings.TrimSpace(patch.Alipay.PrivateKey); v != "" {
		next.Alipay.PrivateKey = v
	}
	if v := strings.TrimSpace(patch.Alipay.AlipayPublicKey); v != "" {
		next.Alipay.AlipayPublicKey = v
	}
	next.Alipay.SignType = strings.TrimSpace(patch.Alipay.SignType)
	if next.Alipay.SignType == "" {
		next.Alipay.SignType = "RSA2"
	}

	next.Paypal.Enabled = patch.Paypal.Enabled
	next.Paypal.Sandbox = patch.Paypal.Sandbox
	next.Paypal.ClientID = strings.TrimSpace(patch.Paypal.ClientID)
	if v := strings.TrimSpace(patch.Paypal.ClientSecret); v != "" {
		next.Paypal.ClientSecret = v
	}
	next.Paypal.WebhookID = strings.TrimSpace(patch.Paypal.WebhookID)

	if next.Alipay.Enabled && (next.Alipay.AppID == "" || next.Alipay.PrivateKey == "" || next.Alipay.AlipayPublicKey == "") {
		return errors.New("启用支付宝需先填写 app_id / private_key / alipay_public_key")
	}
	if next.Paypal.Enabled && (next.Paypal.ClientID == "" || next.Paypal.ClientSecret == "") {
		return errors.New("启用 PayPal 需先填写 client_id / client_secret")
	}

	if err := store.DB.Transaction(func(tx *gorm.DB) error {
		if err := systemsetting.SavePayChannelSettings(next, &adminID); err != nil {
			return err
		}
		return recordOperationTx(tx, adminID, "pay_channel_settings_update", "system_setting", "pay_channel", map[string]any{
			"alipay_enabled": next.Alipay.Enabled,
			"alipay_sandbox": next.Alipay.Sandbox,
			"paypal_enabled": next.Paypal.Enabled,
			"paypal_sandbox": next.Paypal.Sandbox,
		}, clientIP, userAgent)
	}); err != nil {
		return err
	}
	return nil
}

// TestPayChannel 用当前已保存的凭证做一次自检，不涉及真实收款。
// alipay：本地解析私钥/支付宝公钥格式是否合法；paypal：实际换取一次 OAuth token 验证 client_id/secret 是否正确。
func TestPayChannel(channelCode string) error {
	s, err := systemsetting.GetPayChannelSettings()
	if err != nil {
		return err
	}
	switch channelCode {
	case "alipay":
		if s.Alipay.AppID == "" || s.Alipay.PrivateKey == "" || s.Alipay.AlipayPublicKey == "" {
			return errors.New("支付宝凭证未填写完整")
		}
		return alipay.SelfTest(alipay.Config{
			AppID:           s.Alipay.AppID,
			PrivateKey:      s.Alipay.PrivateKey,
			AlipayPublicKey: s.Alipay.AlipayPublicKey,
			Sandbox:         s.Alipay.Sandbox,
			SignType:        s.Alipay.SignType,
		})
	case "paypal":
		if s.Paypal.ClientID == "" || s.Paypal.ClientSecret == "" {
			return errors.New("PayPal 凭证未填写完整")
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return paypal.SelfTest(ctx, paypal.Config{
			ClientID: s.Paypal.ClientID,
			Secret:   s.Paypal.ClientSecret,
			Sandbox:  s.Paypal.Sandbox,
		})
	default:
		return errors.New("未知的支付通道")
	}
}
