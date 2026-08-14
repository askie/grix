package model

import (
	"time"

	"github.com/shopspring/decimal"
)

// GatewayTopupRecord 是一笔充值流水：用户实际支付的币种/金额，及换算成USD记入钱包的数额。
type GatewayTopupRecord struct {
	ID               int64           `gorm:"primaryKey" json:"id,string"`
	WalletID         int64           `gorm:"not null;index" json:"wallet_id,string"`
	SourceCurrency   string          `gorm:"size:8;not null" json:"source_currency"`
	SourceAmount     decimal.Decimal `gorm:"type:numeric(24,12);not null" json:"source_amount"`
	FxRateUsed       decimal.Decimal `gorm:"type:numeric(24,12);not null" json:"fx_rate_used"`
	CreditedAmount   decimal.Decimal `gorm:"type:numeric(24,12);not null" json:"credited_amount"`
	PaymentChannel   string          `gorm:"size:32" json:"payment_channel"`
	PaymentReference string          `gorm:"size:128" json:"payment_reference"`
	CreatedAt        time.Time       `json:"created_at"`
}

func (GatewayTopupRecord) TableName() string { return "gateway_topup_records" }
