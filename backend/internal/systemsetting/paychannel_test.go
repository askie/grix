package systemsetting

import (
	"strings"
	"testing"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/testutil"
	"github.com/askie/grix/backend/internal/store"
)

func TestGetPayChannelSettingsDefaultDisabled(t *testing.T) {
	store.DB = testutil.NewTestDB().DB
	InvalidatePayChannelSettingsCache()

	s, err := GetPayChannelSettings()
	if err != nil {
		t.Fatalf("GetPayChannelSettings error: %v", err)
	}
	if s.Alipay.Enabled || s.Paypal.Enabled {
		t.Fatalf("expected both channels disabled by default, got %+v", s)
	}
}

func TestSaveAndGetPayChannelSettings_RoundtripAndEncryptedAtRest(t *testing.T) {
	store.DB = testutil.NewTestDB().DB
	InvalidatePayChannelSettingsCache()

	want := PayChannelSettings{
		Alipay: PayAlipaySecret{
			Enabled: true, Sandbox: true, AppID: "2021000000000000",
			PrivateKey: "fake-private-key", AlipayPublicKey: "fake-alipay-public-key", SignType: "RSA2",
		},
		Paypal: PayPaypalSecret{
			Enabled: true, Sandbox: true, ClientID: "cid", ClientSecret: "fake-secret", WebhookID: "WH-1",
		},
	}
	if err := SavePayChannelSettings(want, nil); err != nil {
		t.Fatalf("SavePayChannelSettings error: %v", err)
	}

	// 直接读库校验落盘的是密文，而不是明文——这是本设置组存在的核心目的。
	var row model.SystemSetting
	if err := store.DB.First(&row, "key = ?", payChannelSettingKey).Error; err != nil {
		t.Fatalf("read raw row: %v", err)
	}
	raw := string(row.Value)
	for _, plain := range []string{"fake-private-key", "fake-alipay-public-key", "fake-secret"} {
		if strings.Contains(raw, plain) {
			t.Fatalf("secret %q found in plaintext in stored row: %s", plain, raw)
		}
	}

	InvalidatePayChannelSettingsCache()
	got, err := GetPayChannelSettings()
	if err != nil {
		t.Fatalf("GetPayChannelSettings error: %v", err)
	}
	if got != want {
		t.Fatalf("roundtrip mismatch: want %+v, got %+v", want, got)
	}
}

// TestGetPayChannelSettings_CorruptedSecretFailsLoud 锁死"密钥解密失败必须报错，绝不静默
// 降级成明文"。若降级，PayCryptoSecret 被误轮换或密文损坏时系统不会报错，而是拿一段乱码
// 当密钥去调 PayPal / 支付宝，在支付链路最深处抛出一堆看不懂的底层错误——钱的事不能静默
// 失败，必须在配置这一层就炸出来，并说清是哪个字段。
func TestGetPayChannelSettings_CorruptedSecretFailsLoud(t *testing.T) {
	store.DB = testutil.NewTestDB().DB
	InvalidatePayChannelSettingsCache()

	// 先正常存一份（落库是密文）。
	if err := SavePayChannelSettings(PayChannelSettings{
		Paypal: PayPaypalSecret{Enabled: true, ClientID: "cid", ClientSecret: "real-secret"},
	}, nil); err != nil {
		t.Fatalf("SavePayChannelSettings error: %v", err)
	}

	// 模拟密文损坏 / 主密钥被轮换：把 client_secret 换成一段解不开的垃圾。
	if err := store.DB.Model(&model.SystemSetting{}).Where("key = ?", payChannelSettingKey).
		Update("value", `{"alipay":{},"paypal":{"enabled":true,"client_id":"cid","client_secret":"not-a-valid-ciphertext"}}`).Error; err != nil {
		t.Fatalf("corrupt row: %v", err)
	}
	InvalidatePayChannelSettingsCache()

	got, err := GetPayChannelSettings()
	if err == nil {
		t.Fatalf("解密失败必须返回错误，不得降级为明文；却拿到了 %+v", got)
	}
	if !strings.Contains(err.Error(), "paypal.client_secret") {
		t.Fatalf("错误信息必须点名是哪个字段，便于排查，实际: %v", err)
	}
	if got.Paypal.ClientSecret == "not-a-valid-ciphertext" {
		t.Fatal("绝不能把解不开的密文当明文密钥返回")
	}
}
