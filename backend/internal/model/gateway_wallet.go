package model

import (
	"time"

	"github.com/shopspring/decimal"
)

// GatewayWallet 是大模型计费网关的记账货币(USD)钱包，1个Grix用户对应1个钱包。
type GatewayWallet struct {
	ID        int64           `gorm:"primaryKey" json:"id,string"`
	OwnerID   int64           `gorm:"not null;uniqueIndex" json:"owner_id,string"`
	Balance   decimal.Decimal `gorm:"type:numeric(24,12);not null;default:0" json:"balance"`
	CreatedAt time.Time       `json:"created_at"`
	UpdatedAt time.Time       `json:"updated_at"`
}

func (GatewayWallet) TableName() string { return "gateway_wallets" }
