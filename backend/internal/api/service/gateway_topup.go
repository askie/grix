// 这个文件是用户自助充值的 C 端接口层：建充值单 + 调支付系统下单。
// 余额/充值入账逻辑复用 internal/gateway/wallet，本层只做编排。
package service

import (
	"context"
	"net/http"
	"strconv"

	"github.com/shopspring/decimal"

	"github.com/askie/grix/backend/config"
	"github.com/askie/grix/backend/internal/gateway/payclient"
	"github.com/askie/grix/backend/internal/gateway/wallet"
	"github.com/askie/grix/backend/internal/pkg/errcode"
	"github.com/askie/grix/backend/internal/pkg/logger"
)

// GatewayCreateTopupReq 是发起充值的请求。
type GatewayCreateTopupReq struct {
	Amount    string `json:"amount"`     // 充值金额（源币种），字符串防精度丢失
	Currency  string `json:"currency"`   // 源币种，如 CNY / USD
	Channel   string `json:"channel"`    // 支付通道，如 alipay / paypal
	ReturnURL string `json:"return_url"` // 支付完成后前端跳转地址（可选）
}

// GatewayCreateTopupResp 返回充值单号与收银台跳转。
type GatewayCreateTopupResp struct {
	TopupOrderID string `json:"topup_order_id"`
	PayURL       string `json:"pay_url"`
	Status       string `json:"status"`
}

// GatewayCreateTopup 为当前用户发起一笔充值：建充值单 → 调支付系统下单 → 回填支付单号。
// 支付成功后由 pay.order.paid 消费者自动入账（见 gateway_topup_consumer.go）。
func GatewayCreateTopup(ownerID int64, req GatewayCreateTopupReq) (*GatewayCreateTopupResp, *errcode.ErrCode) {
	amount, err := decimal.NewFromString(req.Amount)
	if err != nil || !amount.IsPositive() {
		return nil, &errcode.ErrBadRequest
	}
	if req.Currency == "" || req.Channel == "" {
		return nil, &errcode.ErrBadRequest
	}

	svc := gatewayWalletService()
	// 资损防护：入账时才做换汇，若此刻查不到该币种的有效汇率，必须在下单前拒掉；
	// 否则用户支付成功后 SettleTopup 因取不到汇率而失败，订单永远卡 PENDING（钱收了不入账）。
	if _, err := svc.EffectiveRate(req.Currency); err != nil {
		logger.L.Warnf("gateway topup: reject %s, no fx rate: %v", req.Currency, err)
		return nil, &errcode.ErrCode{
			HTTPStatus: http.StatusBadRequest,
			BizCode:    10003,
			Msg:        "该币种充值暂未开通，请稍后再试",
		}
	}
	order, err := svc.CreateTopupOrder(ownerID, req.Currency, amount, req.Channel)
	if err != nil {
		logger.L.Errorf("gateway topup: create order: %v", err)
		return nil, &errcode.ErrInternal
	}

	pc := payclient.New(config.C.Pay.InternalBaseURL)
	payRes, err := pc.CreateOrder(context.Background(), payclient.CreateOrderRequest{
		BizType:    wallet.TopupBizType,
		BizOrderID: strconv.FormatInt(order.ID, 10),
		Channel:    req.Channel,
		Amount:     amount.String(),
		Currency:   req.Currency,
		Subject:    "钱包充值",
		ReturnURL:  req.ReturnURL,
	})
	if err != nil {
		logger.L.Errorf("gateway topup: pay create order: %v", err)
		_ = svc.MarkTopupFailed(order.ID) // 下单失败，收尾充值单避免孤儿 PENDING
		return nil, &errcode.ErrInternal
	}

	payOrderID, err := strconv.ParseInt(payRes.PayOrderID, 10, 64)
	if err != nil {
		logger.L.Errorf("gateway topup: bad pay order id %q: %v", payRes.PayOrderID, err)
		_ = svc.MarkTopupFailed(order.ID)
		return nil, &errcode.ErrInternal
	}
	if err := svc.BindPayOrder(order.ID, payOrderID); err != nil {
		logger.L.Errorf("gateway topup: bind pay order: %v", err)
		_ = svc.MarkTopupFailed(order.ID)
		return nil, &errcode.ErrInternal
	}

	return &GatewayCreateTopupResp{
		TopupOrderID: strconv.FormatInt(order.ID, 10),
		PayURL:       payRes.PayURL,
		Status:       order.Status,
	}, nil
}
