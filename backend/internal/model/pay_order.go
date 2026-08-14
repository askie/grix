package model

import (
	"time"

	"github.com/shopspring/decimal"
)

// 支付单状态。状态机只能单向前进（见 docs/payment 设计文档）。
const (
	PayOrderStatusCreated         = "CREATED"          // 已创建，待支付
	PayOrderStatusPaid            = "PAID"             // 已支付
	PayOrderStatusClosed          = "CLOSED"           // 超时 / 主动关单
	PayOrderStatusFailed          = "FAILED"           // 支付失败
	PayOrderStatusRefunding       = "REFUNDING"        // 退款中
	PayOrderStatusRefunded        = "REFUNDED"         // 全额已退
	PayOrderStatusPartialRefunded = "PARTIAL_REFUNDED" // 部分已退
)

// PayOrder 是一次收款的支付单。业务无关，用 biz_type + biz_order_id 关联业务。
// 金额统一用 decimal（与 gateway 记账口径一致），currency 为 ISO 4217。
type PayOrder struct {
	ID             int64           `gorm:"primaryKey" json:"id,string"`
	BizType        string          `gorm:"size:32;not null;uniqueIndex:uk_pay_biz" json:"biz_type"`
	BizOrderID     string          `gorm:"size:64;not null;uniqueIndex:uk_pay_biz" json:"biz_order_id"`
	Channel        string          `gorm:"size:32;not null" json:"channel"`
	Amount         decimal.Decimal `gorm:"type:numeric(24,12);not null" json:"amount"`
	Currency       string          `gorm:"size:8;not null" json:"currency"`
	Status         string          `gorm:"size:24;not null" json:"status"`
	Subject        string          `gorm:"size:256" json:"subject"`
	ChannelTradeNo string          `gorm:"size:128;index" json:"channel_trade_no"`
	PaidAt         *time.Time      `json:"paid_at"`
	CreatedAt      time.Time       `json:"created_at"`
	UpdatedAt      time.Time       `json:"updated_at"`
}

func (PayOrder) TableName() string { return "pay_order" }
