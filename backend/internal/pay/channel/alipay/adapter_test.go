package alipay

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"net/url"
	"testing"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/askie/grix/backend/internal/pay/channel"
)

// testConfig 生成一份自签名测试凭证：应用私钥与「支付宝公钥」用同一把 RSA 密钥对，
// 这样可以离线闭环验证签名/验签逻辑，而不必依赖真实支付宝网络。
func testConfig(t *testing.T) Config {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	privDER := x509.MarshalPKCS1PrivateKey(key)
	pubDER, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	require.NoError(t, err)
	return Config{
		AppID:           "2021000000000000",
		PrivateKey:      base64.StdEncoding.EncodeToString(privDER),
		AlipayPublicKey: base64.StdEncoding.EncodeToString(pubDER),
		Sandbox:         true,
		SignType:        "RSA2",
	}
}

func TestSelfTest_ValidCredential(t *testing.T) {
	assert.NoError(t, SelfTest(testConfig(t)))
}

func TestSelfTest_BadPrivateKey(t *testing.T) {
	cfg := testConfig(t)
	cfg.PrivateKey = "not-a-key"
	assert.Error(t, SelfTest(cfg))
}

func TestCreatePay_BuildsSignedURL(t *testing.T) {
	cfg := testConfig(t)
	a := NewAdapter(func() (Config, bool, error) { return cfg, true, nil })

	res, err := a.CreatePay(context.Background(), &channel.PayRequest{
		PayOrderID: 123,
		Amount:     decimal.NewFromInt(100),
		Currency:   "CNY",
		Subject:    "测试订单",
		NotifyURL:  "https://pay.test/v1/pay/notify/alipay",
		ReturnURL:  "https://pay.test/v1/pay/return/alipay",
	})
	require.NoError(t, err)
	require.NotEmpty(t, res.PayURL)

	u, err := url.Parse(res.PayURL)
	require.NoError(t, err)
	q := u.Query()
	assert.NotEmpty(t, q.Get("sign"))
	var biz struct {
		OutTradeNo  string `json:"out_trade_no"`
		TotalAmount string `json:"total_amount"`
	}
	require.NoError(t, json.Unmarshal([]byte(q.Get("biz_content")), &biz))
	assert.Equal(t, "123", biz.OutTradeNo)
	assert.Equal(t, "100.00", biz.TotalAmount)
}

func TestCreatePay_DisabledChannel(t *testing.T) {
	a := NewAdapter(func() (Config, bool, error) { return Config{}, false, nil })
	_, err := a.CreatePay(context.Background(), &channel.PayRequest{PayOrderID: 1, Amount: decimal.NewFromInt(1)})
	assert.ErrorIs(t, err, ErrDisabled)
}

// TestParseNotify_VerifiesAndParses 用同一把测试密钥对自签一份通知报文，
// 验证 ParseNotify 的验签 + 字段解析闭环正确。
func TestParseNotify_VerifiesAndParses(t *testing.T) {
	cfg := testConfig(t)
	client, err := buildClient(cfg)
	require.NoError(t, err)

	values := url.Values{}
	values.Set("app_id", cfg.AppID)
	values.Set("out_trade_no", "456")
	values.Set("trade_no", "2024010112345")
	values.Set("trade_status", "TRADE_SUCCESS")
	values.Set("total_amount", "88.88")

	sigBytes, err := client.SignValues(values)
	require.NoError(t, err)
	values.Set("sign", base64.StdEncoding.EncodeToString(sigBytes))
	values.Set("sign_type", "RSA2")

	a := NewAdapter(func() (Config, bool, error) { return cfg, true, nil })
	res, err := a.ParseNotify(context.Background(), nil, []byte(values.Encode()))
	require.NoError(t, err)
	assert.Equal(t, int64(456), res.PayOrderID)
	assert.Equal(t, "2024010112345", res.ChannelTradeNo)
	assert.Equal(t, channel.TradeSuccess, res.State)
	assert.True(t, decimal.NewFromFloat(88.88).Equal(res.Amount))
	assert.Equal(t, "CNY", res.Currency)
}

func TestParseNotify_BadSignRejected(t *testing.T) {
	cfg := testConfig(t)
	values := url.Values{}
	values.Set("out_trade_no", "456")
	values.Set("trade_status", "TRADE_SUCCESS")
	values.Set("total_amount", "88.88")
	values.Set("sign", base64.StdEncoding.EncodeToString([]byte("bogus")))
	values.Set("sign_type", "RSA2")

	a := NewAdapter(func() (Config, bool, error) { return cfg, true, nil })
	_, err := a.ParseNotify(context.Background(), nil, []byte(values.Encode()))
	assert.Error(t, err)
}

func TestMapTradeState(t *testing.T) {
	assert.Equal(t, channel.TradeSuccess, mapTradeState("TRADE_SUCCESS"))
	assert.Equal(t, channel.TradeSuccess, mapTradeState("TRADE_FINISHED"))
	assert.Equal(t, channel.TradeClosed, mapTradeState("TRADE_CLOSED"))
	assert.Equal(t, channel.TradePending, mapTradeState("WAIT_BUYER_PAY"))
}

func TestParseRefundNotify_NotSupported(t *testing.T) {
	a := NewAdapter(func() (Config, bool, error) { return testConfig(t), true, nil })
	_, err := a.ParseRefundNotify(context.Background(), nil, []byte("{}"))
	assert.Error(t, err)
}

// TestClassifyQueryFailure 锁死"查不到 ≠ 没付"。这个判断直接关乎丢账：
//
// TradePending 的语义是「用户压根没付，超龄可当废单关掉」（见 pay.Service.ReconcileOne）。
// 如果把系统异常、限流这类失败码也归成 Pending，那么一张已付但异步通知丢了的单，只要某轮
// 对账恰好撞上这类错误码，就会被当废单关掉；之后到账通知来了会被 MarkPaid 的 CAS 挡住
// （状态已不是 CREATED）——钱到了却入不了账。而这种单能活到超龄，本身就说明通知和对账
// 长期不通，恰恰是最可能反复撞上错误码的那批。
//
// 所以：只有「交易不存在」可以归 Pending，其余一律报错让对账重试。
func TestClassifyQueryFailure(t *testing.T) {
	// 交易不存在：用户连收银台都没打开，确实没付 → Pending（可被当废单关掉）
	st, err := classifyQueryFailure("40004", "ACQ.TRADE_NOT_EXIST", "交易不存在")
	if err != nil {
		t.Fatalf("交易不存在应归为待支付，而非报错: %v", err)
	}
	if st.State != channel.TradePending {
		t.Fatalf("want TradePending, got %v", st.State)
	}

	// 系统异常 / 限流 / 未知码：查不到 ≠ 没付，必须报错，绝不能被当废单关掉
	for _, subCode := range []string{"ACQ.SYSTEM_ERROR", "ACQ.INVALID_PARAMETER", "isv.busy", ""} {
		st, err := classifyQueryFailure("40004", subCode, "系统繁忙")
		if err == nil {
			t.Fatalf("sub_code=%q 不能判定用户没付（会被当废单关掉、导致钱到了入不了账），必须报错；却返回了 %+v", subCode, st)
		}
		if st != nil {
			t.Fatalf("sub_code=%q 报错时不应返回状态", subCode)
		}
	}
}
