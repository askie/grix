package pay

import "github.com/shopspring/decimal"

// 支付系统对外（→业务）的异步结果事件主题。业务侧至少一次消费、自行幂等。
const (
	EventOrderPaid       = "pay.order.paid"
	EventOrderClosed     = "pay.order.closed"
	EventRefundSucceeded = "pay.refund.succeeded"
	EventRefundFailed    = "pay.refund.failed"
	// EventReconcileFailed 是运维告警事件（不是给业务消费的结果事件）：某张单的对账补偿
	// 失败了。对账是防丢钱的最后一道防线，它自己失效必须能被看见——凭证配错、第三方长时间
	// 不可用会让每张单都失败，若无声无息，整条补偿链路瘫痪而没人知道。
	EventReconcileFailed = "pay.reconcile.failed"
	// EventOrderStateInconsistent 是运维告警事件：库写入失败导致订单状态与实际不一致
	// （如退款已成功但订单的退款状态没更新上）。资金相关的写失败绝不能吞。
	EventOrderStateInconsistent = "pay.order.state_inconsistent"
)

// OrderEvent 是支付单结果事件体，带 biz_type + biz_order_id 供业务定位自己的单。
type OrderEvent struct {
	PayOrderID     int64           `json:"pay_order_id,string"`
	BizType        string          `json:"biz_type"`
	BizOrderID     string          `json:"biz_order_id"`
	Channel        string          `json:"channel"`
	Amount         decimal.Decimal `json:"amount"`
	Currency       string          `json:"currency"`
	Status         string          `json:"status"`
	ChannelTradeNo string          `json:"channel_trade_no"`
}

// RefundEvent 是退款结果事件体。
type RefundEvent struct {
	PayOrderID  int64           `json:"pay_order_id,string"`
	RefundID    int64           `json:"refund_id,string"`
	BizType     string          `json:"biz_type"`
	BizOrderID  string          `json:"biz_order_id"`
	BizRefundID string          `json:"biz_refund_id"`
	Amount      decimal.Decimal `json:"amount"`
	Currency    string          `json:"currency"`
	Status      string          `json:"status"`
}

// AlertEvent 是单张单的运维告警事件体（EventOrderStateInconsistent）。
// 与业务结果事件分开：它不表示支付结果，而表示"支付系统自己出问题了，需要人来看"。
type AlertEvent struct {
	PayOrderID int64  `json:"pay_order_id,string"`
	BizType    string `json:"biz_type"`
	BizOrderID string `json:"biz_order_id"`
	Channel    string `json:"channel"`
	Status     string `json:"status"` // 出问题时订单的当前状态
	Reason     string `json:"reason"` // 失败原因（error 文本）
}

// ReconcileSummaryEvent 是一轮对账的失败汇总告警（EventReconcileFailed）。
//
// 必须是「每轮一条」而不是「每单一条」。凭证配错、第三方长时间不可用时——正是这个告警要报的
// 场景——一轮扫描的每一张单都会失败：
//
//   - 逐单发会在 ReconcileScanLimit 的规模上产生告警风暴（每分钟上千条），信号被噪声淹没，
//     等于没有告警；
//   - 更糟的是 Notifier.Publish 同步等 JetStream ack，NATS 不可用时每条告警都要卡一次 ack
//     超时——对账循环会被它自己的告警拖死。想让最后一道防线出声，反而给它绑了块石头。
type ReconcileSummaryEvent struct {
	Scanned int `json:"scanned"`
	Failed  int `json:"failed"`
	// AllFailed 全军覆没：通常意味着凭证配错或通道整体不可用，而不是个别单子的问题。
	AllFailed bool     `json:"all_failed"`
	Samples   []string `json:"samples"` // 前几条失败原因样本，够定位就行
}

// Notifier 是 outbound 事件发布抽象。生产用 NATS JetStream 实现，测试用 fake。
type Notifier interface {
	Publish(subject string, event any) error
}

// NopNotifier 是空实现（本地开发 / 未接 NATS 时用），不发布任何事件。
type NopNotifier struct{}

// Publish 丢弃事件。
func (NopNotifier) Publish(string, any) error { return nil }
