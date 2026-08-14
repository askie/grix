package model

import (
	"time"

	"github.com/shopspring/decimal"
)

// 退款单状态。
const (
	PayRefundStatusRefunding = "REFUNDING" // 退款中 / 已受理
	PayRefundStatusRefunded  = "REFUNDED"  // 退款成功
	PayRefundStatusFailed    = "FAILED"    // 退款失败
)

// PayRefund 是挂在支付单下的一次退款，支持部分退 / 多次退。
type PayRefund struct {
	ID              int64           `gorm:"primaryKey" json:"id,string"`
	PayOrderID      int64           `gorm:"not null;index" json:"pay_order_id,string"`
	BizRefundID     string          `gorm:"size:64;not null;uniqueIndex" json:"biz_refund_id"`
	Amount          decimal.Decimal `gorm:"type:numeric(24,12);not null" json:"amount"`
	Currency        string          `gorm:"size:8;not null" json:"currency"`
	Status          string          `gorm:"size:24;not null" json:"status"`
	ChannelRefundNo string          `gorm:"size:128" json:"channel_refund_no"`
	CreatedAt       time.Time       `json:"created_at"`
	UpdatedAt       time.Time       `json:"updated_at"`
}

func (PayRefund) TableName() string { return "pay_refund" }
