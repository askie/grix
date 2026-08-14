package model

import (
	"time"

	"github.com/shopspring/decimal"
)

// 充值单状态。状态机单向前进：PENDING → PAID / CLOSED / FAILED。
const (
	GatewayTopupOrderStatusPending = "PENDING" // 已创建，待支付
	GatewayTopupOrderStatusPaid    = "PAID"    // 已支付并入账
	GatewayTopupOrderStatusClosed  = "CLOSED"  // 超时 / 关单
	GatewayTopupOrderStatusFailed  = "FAILED"  // 支付失败
)

// GatewayTopupOrder 是用户自助充值单：把支付系统的支付单与用户钱包、充值金额
// 关联起来。支付系统业务无关，只回传 biz_order_id（= 本单 id），据此反查是谁充多少。
// 余额 / 充值流水 / 消费扣减复用 gateway 现有表，本单只记"充值发起"。
type GatewayTopupOrder struct {
	ID             int64           `gorm:"primaryKey" json:"id,string"`
	WalletID       int64           `gorm:"not null;index" json:"wallet_id,string"`
	OwnerID        int64           `gorm:"not null;index" json:"owner_id,string"`
	SourceCurrency string          `gorm:"size:8;not null" json:"source_currency"`
	SourceAmount   decimal.Decimal `gorm:"type:numeric(24,12);not null" json:"source_amount"`
	Channel        string          `gorm:"size:32;not null" json:"channel"`
	Status         string          `gorm:"size:24;not null" json:"status"`
	// PayOrderID / TopupRecordID 未绑定时必须落 NULL 而非 0：表上的
	// uk_gateway_topup_orders_pay 是 WHERE pay_order_id IS NOT NULL 的部分唯一索引，
	// 若写 0，所有未绑定的单都会挤进同一个索引槽位互相撞唯一键。
	PayOrderID    *int64     `gorm:"index" json:"pay_order_id,string"`
	TopupRecordID *int64     `json:"topup_record_id,string"`
	CreatedAt     time.Time  `json:"created_at"`
	PaidAt        *time.Time `json:"paid_at"`
	UpdatedAt     time.Time  `json:"updated_at"`
}

func (GatewayTopupOrder) TableName() string { return "gateway_topup_orders" }
