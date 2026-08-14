// Package mock 是本地开发 / 端到端联调用的假支付通道。
//
// 它不对接任何真实第三方：CreatePay 直接返回一个假收银台链接，
// ParseNotify / ParseRefundNotify 把一段自定义 JSON 报文解析成归一化结果。
// 仅在显式开启（AIBOT_PAY_MOCK_ENABLED=1）时由 cmd/pay 注册，绝不进生产。
//
// 通知报文格式（POST /v1/pay/notify/mock 的 body）：
//
//	{"pay_order_id":"<id>","channel_trade_no":"<no>","state":"SUCCESS","amount":"100","currency":"CNY"}
package mock

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"

	"github.com/shopspring/decimal"

	"github.com/askie/grix/backend/internal/pay/channel"
)

// Code 是本通道的注册标识。
const Code = "mock"

// Adapter 实现 channel.PaymentChannel，用于本地联调。
type Adapter struct {
	currencies []string
}

// New 构造假通道。currencies 为空时默认支持 CNY / USD。
func New(currencies ...string) *Adapter {
	if len(currencies) == 0 {
		currencies = []string{"CNY", "USD"}
	}
	return &Adapter{currencies: currencies}
}

var _ channel.PaymentChannel = (*Adapter)(nil)

// Code 返回通道标识。
func (a *Adapter) Code() string { return Code }

// SupportedCurrencies 返回支持币种。
func (a *Adapter) SupportedCurrencies() []string { return a.currencies }

// CreatePay 返回一个假的收银台跳转地址与假交易号。
func (a *Adapter) CreatePay(_ context.Context, req *channel.PayRequest) (*channel.PayResult, error) {
	tradeNo := fmt.Sprintf("mocktrade-%d", req.PayOrderID)
	return &channel.PayResult{
		ChannelTradeNo: tradeNo,
		PayURL:         fmt.Sprintf("https://mock-cashier.local/pay?order=%d&trade=%s", req.PayOrderID, tradeNo),
		Raw:            "mock:create",
	}, nil
}

// notifyBody 是假通道支付通知的报文结构。
type notifyBody struct {
	PayOrderID     string `json:"pay_order_id"`
	ChannelTradeNo string `json:"channel_trade_no"`
	State          string `json:"state"`
	Amount         string `json:"amount"`
	Currency       string `json:"currency"`
}

// ParseNotify 解析支付通知报文（无验签，仅联调）。
func (a *Adapter) ParseNotify(_ context.Context, _ http.Header, raw []byte) (*channel.NotifyResult, error) {
	var b notifyBody
	if err := json.Unmarshal(raw, &b); err != nil {
		return nil, fmt.Errorf("mock: bad notify body: %w", err)
	}
	id, err := strconv.ParseInt(b.PayOrderID, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("mock: bad pay_order_id: %w", err)
	}
	amt, err := decimal.NewFromString(b.Amount)
	if err != nil {
		return nil, fmt.Errorf("mock: bad amount: %w", err)
	}
	tradeNo := b.ChannelTradeNo
	if tradeNo == "" {
		tradeNo = fmt.Sprintf("mocktrade-%d", id)
	}
	return &channel.NotifyResult{
		PayOrderID:     id,
		ChannelTradeNo: tradeNo,
		State:          mapTradeState(b.State),
		Amount:         amt,
		Currency:       b.Currency,
		Raw:            string(raw),
	}, nil
}

// QueryTrade 假查单：本地联调恒返回 PENDING（对账补偿由通知驱动即可）。
func (a *Adapter) QueryTrade(_ context.Context, req channel.TradeQueryRequest) (*channel.TradeStatus, error) {
	return &channel.TradeStatus{
		ChannelTradeNo: fmt.Sprintf("mocktrade-%d", req.PayOrderID),
		State:          channel.TradePending,
		Raw:            "mock:query",
	}, nil
}

// Refund 假退款：同步返回成功。
func (a *Adapter) Refund(_ context.Context, req *channel.RefundRequest) (*channel.RefundResult, error) {
	return &channel.RefundResult{
		ChannelRefundNo: fmt.Sprintf("mockrefund-%d", req.RefundID),
		State:           channel.RefundSuccess,
		Raw:             "mock:refund",
	}, nil
}

// refundNotifyBody 是假通道退款通知的报文结构。
type refundNotifyBody struct {
	PayOrderID      string `json:"pay_order_id"`
	RefundID        string `json:"refund_id"`
	ChannelRefundNo string `json:"channel_refund_no"`
	State           string `json:"state"`
	Amount          string `json:"amount"`
	Currency        string `json:"currency"`
}

// ParseRefundNotify 解析退款通知报文。
func (a *Adapter) ParseRefundNotify(_ context.Context, _ http.Header, raw []byte) (*channel.RefundNotifyResult, error) {
	var b refundNotifyBody
	if err := json.Unmarshal(raw, &b); err != nil {
		return nil, fmt.Errorf("mock: bad refund notify body: %w", err)
	}
	orderID, err := strconv.ParseInt(b.PayOrderID, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("mock: bad pay_order_id: %w", err)
	}
	refundID, err := strconv.ParseInt(b.RefundID, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("mock: bad refund_id: %w", err)
	}
	amt, _ := decimal.NewFromString(b.Amount)
	return &channel.RefundNotifyResult{
		PayOrderID:      orderID,
		RefundID:        refundID,
		ChannelRefundNo: b.ChannelRefundNo,
		State:           mapRefundState(b.State),
		Amount:          amt,
		Currency:        b.Currency,
		Raw:             string(raw),
	}, nil
}

func mapTradeState(s string) channel.TradeState {
	switch s {
	case "SUCCESS":
		return channel.TradeSuccess
	case "CLOSED":
		return channel.TradeClosed
	case "FAILED":
		return channel.TradeFailed
	default:
		return channel.TradePending
	}
}

func mapRefundState(s string) channel.RefundState {
	switch s {
	case "SUCCESS":
		return channel.RefundSuccess
	case "FAILED":
		return channel.RefundFailed
	default:
		return channel.RefundProcessing
	}
}
