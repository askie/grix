package model

import (
	"time"

	"github.com/shopspring/decimal"
)

// GatewayFxRate 是充值/价目表维护这两个低频场景用的汇率参考值，不在计费热路径上被查询。
// (from_currency, to_currency, effective_from) 唯一：fxsync 以数据源报价时间为幂等键，
// 靠该约束在并发下兜底去重（生产由 migration 101 建索引，此 tag 只作用于测试的 AutoMigrate）。
type GatewayFxRate struct {
	ID            int64           `gorm:"primaryKey" json:"id,string"`
	FromCurrency  string          `gorm:"size:8;not null;uniqueIndex:idx_gateway_fx_rates_key,priority:1" json:"from_currency"`
	ToCurrency    string          `gorm:"size:8;not null;default:USD;uniqueIndex:idx_gateway_fx_rates_key,priority:2" json:"to_currency"`
	Rate          decimal.Decimal `gorm:"type:numeric(24,12);not null" json:"rate"`
	EffectiveFrom time.Time       `gorm:"not null;uniqueIndex:idx_gateway_fx_rates_key,priority:3" json:"effective_from"`
	Source        string          `gorm:"size:32;not null" json:"source"`
}

func (GatewayFxRate) TableName() string { return "gateway_fx_rates" }
