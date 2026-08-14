// Package paypal 是 PayPal 支付通道适配器（海外收款），基于官方 Orders v2 API
// 通过 github.com/plutov/paypal/v4 SDK 实现。
//
// PayPal 与支付宝的关键差异：下单（CreateOrder）只是让用户在收银台 approve，
// approve 之后必须由商户再显式调用一次 capture 才算真正扣款完成——这就是本包
// 实现 channel.RedirectCapturer 的原因：用户从收银台跳转回来时，支付核心会
// 调 CompleteRedirect 主动 capture，同时 PAYMENT.CAPTURE.COMPLETED webhook
// 作为独立、幂等的兜底路径（用户跳转丢失时靠它兜底）。
//
// 商户凭证同样通过 CredentialProvider 动态取（塘主后台加密存储），不在进程
// 启动时固定；已构造的 SDK client 按凭证内容缓存。
package paypal

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"sync"
	"time"

	paypalSDK "github.com/plutov/paypal/v4"
	"github.com/shopspring/decimal"

	"github.com/askie/grix/backend/internal/pay/channel"
)

// callTimeout 是每次调 PayPal（下单 / capture / 查单 / 退款 / 验签）的单次外呼上限。
// PayPal 端一旦卡住，同步请求路径不会拖垮整个 gin 请求、对账单 goroutine 也不会
// 被一次调用卡死整条补偿链路。每个对外方法在最前处派生一个带此超时的子 ctx。
const callTimeout = 15 * time.Second

// Config 是 PayPal 应用凭证与环境配置。
type Config struct {
	ClientID   string   // 应用 Client ID
	Secret     string   // 应用 Secret
	Sandbox    bool     // true 用沙箱 API，false 用生产 API
	WebhookID  string   // 用于校验 Webhook 签名（塘主在 PayPal 后台创建 webhook 后填入）
	APIBase    string   // 测试注入用，覆盖默认沙箱/生产地址；留空按 Sandbox 选择
	Currencies []string // 覆盖默认支持币种（可选）
}

// CredentialProvider 返回当前生效的 PayPal 凭证；enabled=false 表示通道未启用。
type CredentialProvider func() (Config, bool, error)

// ErrDisabled 表示通道未启用或凭证未配置完整。
var ErrDisabled = errors.New("paypal: 通道未启用或未配置应用凭证")

// defaultCurrencies 是 PayPal 常用支持币种；确切范围以商户结算配置为准。
var defaultCurrencies = []string{"USD", "EUR", "GBP", "AUD", "CAD", "JPY", "HKD", "SGD"}

// Adapter 实现 channel.PaymentChannel + channel.RedirectCapturer。
type Adapter struct {
	provider CredentialProvider

	mu     sync.Mutex
	cfg    Config
	client *paypalSDK.Client
}

// NewAdapter 用凭证提供者构造 PayPal 适配器。
func NewAdapter(provider CredentialProvider) *Adapter {
	return &Adapter{provider: provider}
}

var _ channel.PaymentChannel = (*Adapter)(nil)
var _ channel.RedirectCapturer = (*Adapter)(nil)

// Code 返回通道标识。
func (a *Adapter) Code() string { return "paypal" }

// SupportedCurrencies 返回支持币种。
func (a *Adapter) SupportedCurrencies() []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	if len(a.cfg.Currencies) > 0 {
		return a.cfg.Currencies
	}
	return defaultCurrencies
}

func apiBase(cfg Config) string {
	if cfg.APIBase != "" {
		return cfg.APIBase
	}
	if cfg.Sandbox {
		return paypalSDK.APIBaseSandBox
	}
	return paypalSDK.APIBaseLive
}

func buildClient(cfg Config) (*paypalSDK.Client, error) {
	client, err := paypalSDK.NewClient(cfg.ClientID, cfg.Secret, apiBase(cfg))
	if err != nil {
		return nil, fmt.Errorf("paypal: new client: %w", err)
	}
	return client, nil
}

// SelfTest 用凭证实际换取一次 OAuth token，验证 client_id/secret 是否正确、
// APIBase 是否可达，供塘主后台保存配置后自检使用。
func SelfTest(ctx context.Context, cfg Config) error {
	client, err := buildClient(cfg)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	_, err = client.GetAccessToken(ctx)
	return err
}

// resolveClient 取当前凭证并返回可用的 SDK client；凭证内容不变则复用已构造的 client
// （client 内部会缓存 OAuth token，复用可避免每次调用都重新换取）。
func (a *Adapter) resolveClient() (*paypalSDK.Client, Config, error) {
	cfg, enabled, err := a.provider()
	if err != nil {
		return nil, Config{}, err
	}
	if !enabled {
		return nil, Config{}, ErrDisabled
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.client != nil && a.cfg.ClientID == cfg.ClientID && a.cfg.Secret == cfg.Secret &&
		a.cfg.Sandbox == cfg.Sandbox && a.cfg.APIBase == cfg.APIBase {
		a.cfg = cfg // WebhookID/Currencies 等非连接态字段仍需刷新
		return a.client, cfg, nil
	}
	client, err := buildClient(cfg)
	if err != nil {
		return nil, Config{}, err
	}
	a.client = client
	a.cfg = cfg
	return client, cfg, nil
}

// CreatePay 创建 PayPal Order（intent=CAPTURE）并返回 approve 跳转链接。
//
// ReturnURL 指向 req.ReturnBridgeURL（支付核心的二次确认端点）而不是业务方的
// ReturnURL——用户 approve 后必须先经这里 capture，capture 成功才最终跳回业务方。
func (a *Adapter) CreatePay(ctx context.Context, req *channel.PayRequest) (*channel.PayResult, error) {
	client, _, err := a.resolveClient()
	if err != nil {
		return nil, err
	}
	if req.ReturnBridgeURL == "" {
		return nil, errors.New("paypal: missing return bridge url")
	}
	ctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	payOrderIDStr := strconv.FormatInt(req.PayOrderID, 10)

	appCtx := &paypalSDK.ApplicationContext{
		ReturnURL:  req.ReturnBridgeURL,
		CancelURL:  cancelURLOf(req),
		UserAction: paypalSDK.UserActionPayNow,
	}
	order, err := client.CreateOrder(ctx, "CAPTURE", []paypalSDK.PurchaseUnitRequest{
		{
			ReferenceID: payOrderIDStr,
			CustomID:    payOrderIDStr, // 原样透传到 capture webhook 的 resource.custom_id，用于还原内部单号
			Description: req.Subject,
			Amount: &paypalSDK.PurchaseUnitAmount{
				Currency: req.Currency,
				Value:    req.Amount.StringFixed(2),
			},
		},
	}, nil, appCtx)
	if err != nil {
		return nil, fmt.Errorf("paypal: create order: %w", err)
	}
	approve := approveLink(order.Links)
	if approve == "" {
		return nil, errors.New("paypal: create order response missing approve link")
	}
	raw, _ := json.Marshal(order)
	return &channel.PayResult{ChannelTradeNo: order.ID, PayURL: approve, Raw: string(raw)}, nil
}

func cancelURLOf(req *channel.PayRequest) string {
	if req.ReturnURL != "" {
		return req.ReturnURL + querySep(req.ReturnURL) + "status=cancelled"
	}
	return req.ReturnBridgeURL
}

func querySep(u string) string {
	for i := 0; i < len(u); i++ {
		if u[i] == '?' {
			return "&"
		}
	}
	return "?"
}

func approveLink(links []paypalSDK.Link) string {
	for _, l := range links {
		if l.Rel == "approve" {
			return l.Href
		}
	}
	return ""
}

// CompleteRedirect 是 channel.RedirectCapturer 实现：用户从 approve 页面跳转
// 回来后，主动调用 capture 完成扣款确认。若订单已经被 webhook 先一步 capture
// （PayPal 对已 capture 的订单再次 capture 会报错），退化为查单确认当前状态，
// 避免把已成功的支付误判为失败。
func (a *Adapter) CompleteRedirect(ctx context.Context, req channel.TradeQueryRequest) (*channel.NotifyResult, error) {
	if req.ChannelTradeNo == "" {
		return nil, errors.New("paypal: missing paypal order id, cannot capture")
	}
	client, _, err := a.resolveClient()
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	capRes, err := client.CaptureOrder(ctx, req.ChannelTradeNo, paypalSDK.CaptureOrderRequest{})
	if err != nil {
		// capture 报错有两种可能：① 订单已被 webhook / 对账先一步 capture（PayPal 对已
		// capture 的订单再 capture 会报错）；② 本次外呼超时或调用方断开。两种都必须查一次
		// 单才能判定钱到底扣没扣——尤其 ② 极可能"钱已扣、响应没拿到"。
		//
		// 兜底查单必须派生一个不继承父 ctx 取消与超时的新 ctx：若沿用 ctx，capture 耗满
		// callTimeout 超时的那一刻 ctx 已经烧干，GetOrder 会立即失败，兜底形同虚设；调用方
		// （用户跳回请求）中途断开时同理。剥离取消链后，这笔钱仍能被查出来并正常入账。
		fallbackCtx, fallbackCancel := context.WithTimeout(context.WithoutCancel(ctx), callTimeout)
		defer fallbackCancel()
		if order, qerr := client.GetOrder(fallbackCtx, req.ChannelTradeNo); qerr == nil {
			if state, amount, currency := paypalOrderState(order); state == channel.TradeSuccess {
				raw, _ := json.Marshal(order)
				return &channel.NotifyResult{PayOrderID: req.PayOrderID, ChannelTradeNo: order.ID, State: state, Amount: amount, Currency: currency, Raw: string(raw)}, nil
			}
		}
		return nil, fmt.Errorf("paypal: capture order: %w", err)
	}
	state, amount, currency := paypalCaptureState(capRes)
	raw, _ := json.Marshal(capRes)
	return &channel.NotifyResult{PayOrderID: req.PayOrderID, ChannelTradeNo: capRes.ID, State: state, Amount: amount, Currency: currency, Raw: string(raw)}, nil
}

// ParseNotify 校验 PayPal Webhook 签名并解析支付结果通知。
func (a *Adapter) ParseNotify(ctx context.Context, headers http.Header, raw []byte) (*channel.NotifyResult, error) {
	client, cfg, err := a.resolveClient()
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	if err := verifyWebhook(ctx, client, cfg.WebhookID, headers, raw); err != nil {
		return nil, err
	}
	var evt struct {
		EventType string `json:"event_type"`
		Resource  struct {
			ID       string `json:"id"` // capture id，不是 order id，不能当 ChannelTradeNo 用
			CustomID string `json:"custom_id"`
			Status   string `json:"status"`
			Amount   struct {
				CurrencyCode string `json:"currency_code"`
				Value        string `json:"value"`
			} `json:"amount"`
			SupplementaryData struct {
				RelatedIDs struct {
					OrderID string `json:"order_id"`
				} `json:"related_ids"`
			} `json:"supplementary_data"`
		} `json:"resource"`
	}
	if err := json.Unmarshal(raw, &evt); err != nil {
		return nil, fmt.Errorf("paypal: bad webhook body: %w", err)
	}
	payOrderID, err := strconv.ParseInt(evt.Resource.CustomID, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("paypal: bad custom_id %q: %w", evt.Resource.CustomID, err)
	}
	amount, err := decimal.NewFromString(evt.Resource.Amount.Value)
	if err != nil {
		return nil, fmt.Errorf("paypal: bad amount %q: %w", evt.Resource.Amount.Value, err)
	}
	// ChannelTradeNo 全链路的语义统一是「PayPal order id」（CreatePay/CompleteRedirect/
	// QueryTrade/Refund 都靠它反查），必须从 resource.supplementary_data.related_ids.order_id
	// 取，而不是 resource.id（那是 capture id）——否则会把已落库的 order id 覆盖成
	// capture id，后续 Refund/QueryTrade 反查 PayPal 全部失败。
	channelTradeNo := evt.Resource.SupplementaryData.RelatedIDs.OrderID
	if channelTradeNo == "" {
		channelTradeNo = evt.Resource.ID // 兜底：理论上不应发生，仅保证通知去重仍可工作
	}
	return &channel.NotifyResult{
		PayOrderID:     payOrderID,
		ChannelTradeNo: channelTradeNo,
		State:          mapCaptureEventState(evt.EventType),
		Amount:         amount,
		Currency:       evt.Resource.Amount.CurrencyCode,
		Raw:            string(raw),
	}, nil
}

// QueryTrade 用下单时返回的 PayPal order id 主动查单，用于对账补偿。PayPal 不支持
// 按商户自定义单号查单，下单从未成功过（ChannelTradeNo 为空）时视为待支付。
func (a *Adapter) QueryTrade(ctx context.Context, req channel.TradeQueryRequest) (*channel.TradeStatus, error) {
	if req.ChannelTradeNo == "" {
		return &channel.TradeStatus{State: channel.TradePending}, nil
	}
	client, _, err := a.resolveClient()
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	order, err := client.GetOrder(ctx, req.ChannelTradeNo)
	if err != nil {
		return nil, fmt.Errorf("paypal: get order: %w", err)
	}
	state, amount, currency := paypalOrderState(order)
	raw, _ := json.Marshal(order)
	return &channel.TradeStatus{ChannelTradeNo: order.ID, State: state, Amount: amount, Currency: currency, Raw: string(raw)}, nil
}

// Refund 先查单拿到 capture id，再对该 capture 发起退款。
func (a *Adapter) Refund(ctx context.Context, req *channel.RefundRequest) (*channel.RefundResult, error) {
	if req.ChannelTradeNo == "" {
		return nil, errors.New("paypal: missing channel trade no (paypal order id)")
	}
	client, _, err := a.resolveClient()
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	order, err := client.GetOrder(ctx, req.ChannelTradeNo)
	if err != nil {
		return nil, fmt.Errorf("paypal: get order for refund: %w", err)
	}
	captureID := captureIDOf(order)
	if captureID == "" {
		return nil, errors.New("paypal: order has no captured payment to refund")
	}
	// InvoiceID 借用来携带我们内部的退款单号——PayPal 退款接口没有 custom_id 字段，
	// 但退款 webhook 的 resource.custom_id 会从原 capture 继承（即我们的支付单号），
	// resource.invoice_id 则原样回传这里填的值，两者合起来还原 PayOrderID + RefundID。
	rsp, err := client.RefundCapture(ctx, captureID, paypalSDK.RefundCaptureRequest{
		Amount:      &paypalSDK.Money{Currency: req.Currency, Value: req.Amount.StringFixed(2)},
		InvoiceID:   strconv.FormatInt(req.RefundID, 10),
		NoteToPayer: req.Reason,
	})
	if err != nil {
		return nil, fmt.Errorf("paypal: refund capture: %w", err)
	}
	return &channel.RefundResult{ChannelRefundNo: rsp.ID, State: mapRefundStatus(rsp.Status), Raw: rsp.Status}, nil
}

// ParseRefundNotify 校验并解析 PayPal 退款 Webhook（PAYMENT.CAPTURE.REFUNDED）。
func (a *Adapter) ParseRefundNotify(ctx context.Context, headers http.Header, raw []byte) (*channel.RefundNotifyResult, error) {
	client, cfg, err := a.resolveClient()
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	if err := verifyWebhook(ctx, client, cfg.WebhookID, headers, raw); err != nil {
		return nil, err
	}
	var evt struct {
		EventType string `json:"event_type"`
		Resource  struct {
			ID        string `json:"id"`
			CustomID  string `json:"custom_id"`
			InvoiceID string `json:"invoice_id"`
			Status    string `json:"status"`
			Amount    struct {
				CurrencyCode string `json:"currency_code"`
				Value        string `json:"value"`
			} `json:"amount"`
		} `json:"resource"`
	}
	if err := json.Unmarshal(raw, &evt); err != nil {
		return nil, fmt.Errorf("paypal: bad refund webhook body: %w", err)
	}
	payOrderID, err := strconv.ParseInt(evt.Resource.CustomID, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("paypal: bad custom_id %q: %w", evt.Resource.CustomID, err)
	}
	refundID, err := strconv.ParseInt(evt.Resource.InvoiceID, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("paypal: bad invoice_id %q: %w", evt.Resource.InvoiceID, err)
	}
	amount, _ := decimal.NewFromString(evt.Resource.Amount.Value)
	return &channel.RefundNotifyResult{
		PayOrderID:      payOrderID,
		RefundID:        refundID,
		ChannelRefundNo: evt.Resource.ID,
		State:           mapRefundEventState(evt.EventType),
		Amount:          amount,
		Currency:        evt.Resource.Amount.CurrencyCode,
		Raw:             string(raw),
	}, nil
}

func verifyWebhook(ctx context.Context, client *paypalSDK.Client, webhookID string, headers http.Header, raw []byte) error {
	if webhookID == "" {
		return errors.New("paypal: webhook_id 未配置，无法验签")
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://paypal.local/webhook", bytes.NewReader(raw))
	if err != nil {
		return err
	}
	httpReq.Header = headers
	verify, err := client.VerifyWebhookSignature(ctx, httpReq, webhookID)
	if err != nil {
		return fmt.Errorf("paypal: verify webhook: %w", err)
	}
	if verify.VerificationStatus != "SUCCESS" {
		return errors.New("paypal: webhook signature verification failed")
	}
	return nil
}

func captureIDOf(order *paypalSDK.Order) string {
	if len(order.PurchaseUnits) == 0 || order.PurchaseUnits[0].Payments == nil {
		return ""
	}
	captures := order.PurchaseUnits[0].Payments.Captures
	if len(captures) == 0 {
		return ""
	}
	return captures[0].ID
}

// paypalOrderState 把 PayPal 订单归一成通道状态 + 金额 + 币种。
//
// 金额解析失败时留 0：这不会导致带病运行——支付核心的 applyNotifyResult 会拿它跟订单
// 金额比对，0 必然对不上，直接 ErrNotifyMismatch 拒绝入账并上抛。安全侧是对的，只是
// 报错会显示成"金额不匹配"而非"金额解析失败"。PayPal 返回非法金额字符串在实践中不会
// 发生，故不为此把 error 一路透传改六个调用点。
func paypalOrderState(order *paypalSDK.Order) (channel.TradeState, decimal.Decimal, string) {
	amount, currency := decimal.Zero, ""
	if len(order.PurchaseUnits) > 0 && order.PurchaseUnits[0].Amount != nil {
		amount, _ = decimal.NewFromString(order.PurchaseUnits[0].Amount.Value)
		currency = order.PurchaseUnits[0].Amount.Currency
	}
	if len(order.PurchaseUnits) > 0 && order.PurchaseUnits[0].Payments != nil && len(order.PurchaseUnits[0].Payments.Captures) > 0 {
		switch order.PurchaseUnits[0].Payments.Captures[0].Status {
		case "COMPLETED":
			return channel.TradeSuccess, amount, currency
		case "DECLINED", "FAILED":
			return channel.TradeFailed, amount, currency
		case "PENDING":
			// 资金待定（反欺诈延审 / e-check 等）：capture 已受理但钱没到。绝不能落到下面
			// 的 order.Status 分支——capture 一旦建立，order.Status 就是 COMPLETED（它只表示
			// 下单流程走完，不代表收到钱），会被误判成支付成功而提前入账。归一成 Settling
			// （而非 Pending：用户确实付了，这单不是废单，对账不得把它当废单关掉），等
			// PAYMENT.CAPTURE.COMPLETED webhook 或下一轮对账查到 capture 转 COMPLETED 再入账；
			// 延审被拒则走 PAYMENT.CAPTURE.DENIED → TradeFailed。
			return channel.TradeSettling, amount, currency
		}
	}
	switch order.Status {
	case "COMPLETED":
		return channel.TradeSuccess, amount, currency
	case "APPROVED":
		// 用户已 approve 但商户尚未 capture：对账据此主动补一次 capture（见 service.ReconcileOne）。
		return channel.TradeAuthorized, amount, currency
	case "VOIDED":
		return channel.TradeClosed, amount, currency
	default:
		return channel.TradePending, amount, currency
	}
}

func paypalCaptureState(capRes *paypalSDK.CaptureOrderResponse) (channel.TradeState, decimal.Decimal, string) {
	amount, currency := decimal.Zero, ""
	if len(capRes.PurchaseUnits) > 0 && capRes.PurchaseUnits[0].Payments != nil && len(capRes.PurchaseUnits[0].Payments.Captures) > 0 {
		c := capRes.PurchaseUnits[0].Payments.Captures[0]
		if c.Amount != nil {
			amount, _ = decimal.NewFromString(c.Amount.Value)
			currency = c.Amount.Currency
		}
		switch c.Status {
		case "COMPLETED":
			return channel.TradeSuccess, amount, currency
		case "DECLINED", "FAILED":
			return channel.TradeFailed, amount, currency
		case "PENDING":
			// 同 paypalOrderState：capture 受理了但资金待定，不能因外层 capRes.Status=COMPLETED
			// 就判成功。这条路径是用户跳回时的主路径（CompleteRedirect），误判即当场提前入账。
			return channel.TradeSettling, amount, currency
		}
	}
	switch capRes.Status {
	case "COMPLETED":
		return channel.TradeSuccess, amount, currency
	default:
		return channel.TradePending, amount, currency
	}
}

func mapCaptureEventState(eventType string) channel.TradeState {
	switch eventType {
	case "PAYMENT.CAPTURE.COMPLETED":
		return channel.TradeSuccess
	case "PAYMENT.CAPTURE.DENIED", "CHECKOUT.ORDER.VOIDED":
		return channel.TradeFailed
	default:
		return channel.TradePending
	}
}

func mapRefundEventState(eventType string) channel.RefundState {
	switch eventType {
	case "PAYMENT.CAPTURE.REFUNDED":
		return channel.RefundSuccess
	default:
		return channel.RefundProcessing
	}
}

func mapRefundStatus(status string) channel.RefundState {
	switch status {
	case "COMPLETED":
		return channel.RefundSuccess
	case "FAILED":
		return channel.RefundFailed
	default:
		return channel.RefundProcessing
	}
}
