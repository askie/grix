// Package channel 定义支付通道适配器契约。
//
// 每个第三方支付渠道（支付宝、PayPal、微信…）实现 PaymentChannel 接口，
// 通过 registry 注册后由支付核心按 channel 选用。通道之间的差异
// （跳转 vs SDK、同步退 vs 异步退、验签方式、支持币种）全部收敛在
// 各自适配器内部，支付主流程对通道无感知。
//
// 该设计复刻自 internal/agentadapter 的显式 registry 模式。
package channel

import (
	"context"
	"net/http"

	"github.com/shopspring/decimal"
)

// PaymentChannel 是所有支付通道适配器必须实现的接口。
type PaymentChannel interface {
	// Code 返回通道唯一标识，如 "alipay" / "paypal"。
	Code() string

	// SupportedCurrencies 返回该通道支持的币种（ISO 4217，如 "CNY" "USD"）。
	// 支付核心在下单前用它校验 channel + currency 组合是否合法。
	SupportedCurrencies() []string

	// CreatePay 向第三方发起下单，返回收银台跳转信息。
	CreatePay(ctx context.Context, req *PayRequest) (*PayResult, error)

	// ParseNotify 解析并验签第三方的异步支付通知。验签不过必须返回 error。
	// headers 是原始 HTTP 请求头——部分通道（如 PayPal Webhook）验签必须依赖
	// 传输头字段（transmission-id/cert-url/sig 等），仅报文 body 不足以验签。
	ParseNotify(ctx context.Context, headers http.Header, raw []byte) (*NotifyResult, error)

	// QueryTrade 主动查询第三方交易状态，用于对账 / 补偿。同时给商户单号与第三方
	// 交易号——支付宝用商户单号查（下单时后者未必已生成），PayPal 等只能用其自身
	// 下单时返回的交易号查，两者取舍留给各适配器。
	QueryTrade(ctx context.Context, req TradeQueryRequest) (*TradeStatus, error)

	// Refund 向第三方发起退款。
	Refund(ctx context.Context, req *RefundRequest) (*RefundResult, error)

	// ParseRefundNotify 解析并验签第三方的异步退款结果通知。
	ParseRefundNotify(ctx context.Context, headers http.Header, raw []byte) (*RefundNotifyResult, error)
}

// RedirectCapturer 是需要在用户从收银台跳转返回后，由商户主动发起二次确认才算
// 完成扣款的通道（如 PayPal Orders v2：用户 approve 后必须显式调用 capture）。
// 支付宝等下单即完成支付、异步通知即为最终结果的通道无需实现此接口。
//
// 支付核心在 /v1/pay/return/:channel 收到用户跳转回调时，若通道实现了该接口，
// 会调用 CompleteRedirect 主动确认交易，结果与异步通知走同一幂等入账路径。
type RedirectCapturer interface {
	CompleteRedirect(ctx context.Context, req TradeQueryRequest) (*NotifyResult, error)
}

// TradeQueryRequest 是主动查单 / 二次确认时的入参：同时带商户单号与第三方交易号。
type TradeQueryRequest struct {
	// PayOrderID 支付系统内部支付单号。
	PayOrderID int64
	// ChannelTradeNo 通道下单时返回的第三方交易号；下单从未成功过时可能为空。
	ChannelTradeNo string
}

// PayRequest 是支付核心传给通道的下单请求（通道无关字段）。
type PayRequest struct {
	// PayOrderID 支付系统内部支付单号（snowflake），作为商户订单号传给第三方。
	PayOrderID int64
	// Amount 支付金额，币种由 Currency 指定。适配器负责按渠道要求格式化。
	Amount decimal.Decimal
	// Currency ISO 4217 币种。
	Currency string
	// Subject 交易标题 / 描述。
	Subject string
	// NotifyURL 第三方异步通知回调地址（由支付核心按通道拼装）。
	NotifyURL string
	// ReturnURL 用户支付完成后的前端跳转地址（可选）。
	ReturnURL string
	// ReturnBridgeURL 是支付核心 /v1/pay/return/:channel 的可达地址（不含 query）。
	// 需要二次确认扣款的通道（RedirectCapturer，如 PayPal）应把用户导向这里而不是
	// 直接导向 ReturnURL，由支付核心确认交易后再跳转到 ReturnURL；不需要二次确认
	// 的通道（如支付宝）忽略此字段，直接用 ReturnURL。
	ReturnBridgeURL string
	// Extra 通道特有的扩展参数。
	Extra map[string]string
}

// PayResult 是通道下单成功后返回给支付核心的结果。
type PayResult struct {
	// ChannelTradeNo 第三方交易号（部分通道下单即返回，部分要等通知）。
	ChannelTradeNo string
	// PayURL 收银台 / 跳转 URL（网页、扫码等）。
	PayURL string
	// Raw 原始返回，便于排障与审计。
	Raw string
}

// TradeState 是通道交易的归一化状态。
type TradeState string

const (
	TradeSuccess TradeState = "SUCCESS" // 支付成功
	// TradeAuthorized 已授权未扣款：用户已在第三方完成授权，但需商户再显式二次确认
	// （capture）才真正扣款完成（PayPal Orders v2 的 APPROVED 态）。仅在主动查单
	// （QueryTrade）结果里出现——用户从收银台正常跳回时走 CompleteRedirect 当场
	// capture，不会残留此态；只有"授权了但没跳回来"才会被对账查到 TradeAuthorized，
	// 由对账驱动通道（RedirectCapturer）主动补一次 capture。异步通知即终态的通道
	// （支付宝）永不返回此态。
	TradeAuthorized TradeState = "AUTHORIZED"
	// TradeSettling 资金待定：扣款已被第三方受理，但钱还没到账（PayPal 反欺诈延审 /
	// e-check 的 capture status=PENDING）。它和 TradePending 语义完全不同，必须分开：
	//
	//   TradePending  = 用户压根没付（收银台开了又跑），这单大概率是废单，可以超龄关掉；
	//   TradeSettling = 用户付了、第三方也受理了，只是钱在路上，关掉就是把到账的钱丢了。
	//
	// 二者混在一起，对账要么不敢关废单（扫描被废单饿死、补偿链路失效），要么关错延审单
	// （钱到了却入不了账）。异步通知即终态的通道（支付宝）永不返回此态。
	TradeSettling TradeState = "SETTLING"
	TradePending  TradeState = "PENDING" // 进行中 / 用户未支付
	TradeClosed   TradeState = "CLOSED"  // 交易关闭 / 超时
	TradeFailed   TradeState = "FAILED"  // 支付失败
)

// NotifyResult 是解析并验签后的异步支付通知。
type NotifyResult struct {
	// PayOrderID 从商户订单号还原出的支付系统内部单号。
	PayOrderID int64
	// ChannelTradeNo 第三方交易号。
	ChannelTradeNo string
	// State 归一化的交易状态。
	State TradeState
	// Amount 第三方回传的实付金额，支付核心必须与原单比对。
	Amount decimal.Decimal
	// Currency 第三方回传的币种，支付核心必须与原单比对。
	Currency string
	// Raw 原始通知报文。
	Raw string
}

// TradeStatus 是主动查单的归一化结果。
type TradeStatus struct {
	ChannelTradeNo string
	State          TradeState
	Amount         decimal.Decimal
	Currency       string
	Raw            string
}

// RefundRequest 是支付核心传给通道的退款请求。
type RefundRequest struct {
	PayOrderID     int64           // 原支付单号
	ChannelTradeNo string          // 原交易的第三方交易号
	RefundID       int64           // 支付系统内部退款单号（snowflake）
	Amount         decimal.Decimal // 本次退款金额
	TotalAmount    decimal.Decimal // 原支付总金额（部分退款时第三方常要求传入）
	Currency       string
	Reason         string
}

// RefundState 是通道退款的归一化状态。
type RefundState string

const (
	RefundSuccess    RefundState = "SUCCESS"    // 退款成功
	RefundProcessing RefundState = "PROCESSING" // 受理中（异步退款）
	RefundFailed     RefundState = "FAILED"     // 退款失败
)

// RefundResult 是通道退款的同步返回。
type RefundResult struct {
	ChannelRefundNo string
	State           RefundState
	Raw             string
}

// RefundNotifyResult 是解析并验签后的异步退款结果通知。
type RefundNotifyResult struct {
	PayOrderID      int64
	RefundID        int64
	ChannelRefundNo string
	State           RefundState
	Amount          decimal.Decimal
	Currency        string
	Raw             string
}
