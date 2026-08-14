// Package alipay 是支付宝支付通道适配器，基于官方推荐的
// github.com/smartwalle/alipay/v3 SDK 实现电脑网站支付（trade.page.pay）。
//
// 商户凭证不在进程启动时固定，而是通过 CredentialProvider 每次调用时动态取——
// 凭证来自塘主后台加密存储（见 internal/systemsetting.GetPayChannelSettings），
// 改配置无需重启 cmd/pay。已构造的 SDK client 按凭证内容缓存，凭证不变则复用。
package alipay

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"sync"

	"github.com/shopspring/decimal"
	aliSDK "github.com/smartwalle/alipay/v3"

	"github.com/askie/grix/backend/internal/pay/channel"
)

// tradeNotExistCode 是支付宝「交易不存在」的业务错误码——查单时唯一可以安全地判定为
// 「用户压根没付」的失败码。其余失败码（系统异常、限流等）都属于"查不到 ≠ 没付"，必须
// 当作错误上抛让对账重试，不能归成 TradePending（那会让单子被当废单关掉，见 QueryTrade）。
const tradeNotExistCode = "ACQ.TRADE_NOT_EXIST"

// Config 是支付宝商户凭证与环境配置。
type Config struct {
	AppID           string // 商户 AppID
	PrivateKey      string // 应用私钥（RSA2，PKCS1/PKCS8 均可）
	AlipayPublicKey string // 支付宝公钥，用于验签
	Sandbox         bool   // true 用沙箱网关，false 用生产网关
	SignType        string // 展示用，SDK 固定按 RSA2 签名
}

// CredentialProvider 返回当前生效的支付宝凭证；enabled=false 表示通道未启用。
type CredentialProvider func() (Config, bool, error)

// ErrDisabled 表示通道未启用或凭证未配置完整。
var ErrDisabled = errors.New("alipay: 通道未启用或未配置商户凭证")

// Adapter 实现 channel.PaymentChannel。
type Adapter struct {
	provider CredentialProvider

	mu     sync.Mutex
	cfg    Config
	client *aliSDK.Client
}

// NewAdapter 用凭证提供者构造支付宝适配器。
func NewAdapter(provider CredentialProvider) *Adapter {
	return &Adapter{provider: provider}
}

var _ channel.PaymentChannel = (*Adapter)(nil)

// Code 返回通道标识。
func (a *Adapter) Code() string { return "alipay" }

// SupportedCurrencies 支付宝境内收款仅 CNY。
func (a *Adapter) SupportedCurrencies() []string { return []string{"CNY"} }

// resolveClient 取当前凭证并返回可用的 SDK client；凭证内容不变则复用已构造的 client。
func (a *Adapter) resolveClient() (*aliSDK.Client, error) {
	cfg, enabled, err := a.provider()
	if err != nil {
		return nil, err
	}
	if !enabled {
		return nil, ErrDisabled
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.client != nil && a.cfg == cfg {
		return a.client, nil
	}
	client, err := buildClient(cfg)
	if err != nil {
		return nil, err
	}
	a.client = client
	a.cfg = cfg
	return client, nil
}

// buildClient 解析私钥 + 加载支付宝公钥，构造一个可直接使用的 SDK client。
func buildClient(cfg Config) (*aliSDK.Client, error) {
	client, err := aliSDK.New(cfg.AppID, cfg.PrivateKey, !cfg.Sandbox)
	if err != nil {
		return nil, fmt.Errorf("alipay: parse private key: %w", err)
	}
	if err := client.LoadAliPayPublicKey(cfg.AlipayPublicKey); err != nil {
		return nil, fmt.Errorf("alipay: load alipay public key: %w", err)
	}
	return client, nil
}

// SelfTest 校验凭证格式是否合法（解析私钥 + 加载支付宝公钥），不发起任何网络请求，
// 供塘主后台保存配置后自检使用。
func SelfTest(cfg Config) error {
	_, err := buildClient(cfg)
	return err
}

// CreatePay 生成支付宝电脑网站支付（trade.page.pay）的跳转 URL。
func (a *Adapter) CreatePay(_ context.Context, req *channel.PayRequest) (*channel.PayResult, error) {
	client, err := a.resolveClient()
	if err != nil {
		return nil, err
	}
	subject := req.Subject
	if subject == "" {
		subject = "支付"
	}
	var p aliSDK.TradePagePay
	p.NotifyURL = req.NotifyURL
	p.ReturnURL = req.ReturnURL
	p.Subject = subject
	p.OutTradeNo = strconv.FormatInt(req.PayOrderID, 10)
	p.TotalAmount = req.Amount.StringFixed(2)
	p.ProductCode = "FAST_INSTANT_TRADE_PAY"

	u, err := client.TradePagePay(p)
	if err != nil {
		return nil, fmt.Errorf("alipay: build pay url: %w", err)
	}
	return &channel.PayResult{PayURL: u.String(), Raw: u.String()}, nil
}

// ParseNotify 验签并解析支付宝异步支付通知（application/x-www-form-urlencoded body）。
func (a *Adapter) ParseNotify(ctx context.Context, _ http.Header, raw []byte) (*channel.NotifyResult, error) {
	client, err := a.resolveClient()
	if err != nil {
		return nil, err
	}
	values, err := url.ParseQuery(string(raw))
	if err != nil {
		return nil, fmt.Errorf("alipay: bad notify body: %w", err)
	}
	notification, err := client.DecodeNotification(ctx, values)
	if err != nil {
		return nil, fmt.Errorf("alipay: verify notify sign: %w", err)
	}
	payOrderID, err := strconv.ParseInt(notification.OutTradeNo, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("alipay: bad out_trade_no %q: %w", notification.OutTradeNo, err)
	}
	amount, err := decimal.NewFromString(notification.TotalAmount)
	if err != nil {
		return nil, fmt.Errorf("alipay: bad total_amount %q: %w", notification.TotalAmount, err)
	}
	return &channel.NotifyResult{
		PayOrderID:     payOrderID,
		ChannelTradeNo: notification.TradeNo,
		State:          mapTradeState(notification.TradeStatus),
		Amount:         amount,
		Currency:       "CNY",
		Raw:            string(raw),
	}, nil
}

// QueryTrade 调 alipay.trade.query 主动查单，用于对账补偿。支付宝支持用商户订单号
// （即我们的 payOrderID）直接查，不依赖第三方交易号是否已生成。
func (a *Adapter) QueryTrade(ctx context.Context, req channel.TradeQueryRequest) (*channel.TradeStatus, error) {
	client, err := a.resolveClient()
	if err != nil {
		return nil, err
	}
	rsp, err := client.TradeQuery(ctx, aliSDK.TradeQuery{OutTradeNo: strconv.FormatInt(req.PayOrderID, 10)})
	if err != nil {
		return nil, fmt.Errorf("alipay: query trade: %w", err)
	}
	if !rsp.IsSuccess() {
		return classifyQueryFailure(string(rsp.Code), rsp.SubCode, rsp.SubMsg)
	}
	amount, _ := decimal.NewFromString(rsp.TotalAmount)
	return &channel.TradeStatus{
		ChannelTradeNo: rsp.TradeNo,
		State:          mapTradeState(rsp.TradeStatus),
		Amount:         amount,
		Currency:       "CNY",
		Raw:            rsp.TradeNo,
	}, nil
}

// classifyQueryFailure 判定查单的业务失败码该怎么归一。这个判断关乎丢账，单独成函数。
//
// 只有「交易不存在」才等同于"用户压根没付"（收银台都没打开过），可以归 TradePending。
//
// 其余失败码（ACQ.SYSTEM_ERROR、系统繁忙、限流等）都是"查不到 ≠ 没付"，必须当错误上抛，
// 让对账下轮重试——绝不能归成 TradePending。因为 TradePending 现在的语义是「用户没付，
// 超龄可当废单关掉」（见 pay.Service.ReconcileOne）：一张已付但异步通知丢了的单，若某轮
// 对账恰好撞上系统错误码就被判成废单关掉，之后到账通知来了会被 MarkPaid 的 CAS 挡住
// （状态已不是 CREATED）——钱到了却入不了账。
//
// 而且这种单能活到超龄本身就说明通知和对账长期不通，恰恰是最可能反复撞上错误码的那批。
func classifyQueryFailure(code, subCode, subMsg string) (*channel.TradeStatus, error) {
	if subCode == tradeNotExistCode {
		return &channel.TradeStatus{State: channel.TradePending, Raw: subMsg}, nil
	}
	return nil, fmt.Errorf("alipay: query trade failed: code=%s sub_code=%s sub_msg=%s", code, subCode, subMsg)
}

// Refund 调 alipay.trade.refund 发起退款。支付宝退款为同步返回，不产生异步通知。
func (a *Adapter) Refund(ctx context.Context, req *channel.RefundRequest) (*channel.RefundResult, error) {
	client, err := a.resolveClient()
	if err != nil {
		return nil, err
	}
	rsp, err := client.TradeRefund(ctx, aliSDK.TradeRefund{
		OutTradeNo:   strconv.FormatInt(req.PayOrderID, 10),
		RefundAmount: req.Amount.StringFixed(2),
		RefundReason: req.Reason,
		OutRequestNo: strconv.FormatInt(req.RefundID, 10),
	})
	if err != nil {
		return nil, fmt.Errorf("alipay: refund request: %w", err)
	}
	if !rsp.IsSuccess() {
		return &channel.RefundResult{State: channel.RefundFailed, Raw: rsp.SubMsg}, nil
	}
	return &channel.RefundResult{ChannelRefundNo: rsp.TradeNo, State: channel.RefundSuccess, Raw: rsp.TradeNo}, nil
}

// ParseRefundNotify 支付宝退款为同步结果，不支持异步退款通知。
func (a *Adapter) ParseRefundNotify(context.Context, http.Header, []byte) (*channel.RefundNotifyResult, error) {
	return nil, errors.New("alipay: 退款为同步结果，不支持异步退款通知")
}

func mapTradeState(s aliSDK.TradeStatus) channel.TradeState {
	switch s {
	case aliSDK.TradeStatusSuccess, aliSDK.TradeStatusFinished:
		return channel.TradeSuccess
	case aliSDK.TradeStatusClosed:
		return channel.TradeClosed
	default:
		return channel.TradePending
	}
}
