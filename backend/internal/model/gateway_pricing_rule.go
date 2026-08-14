package model

import (
	"time"

	"github.com/shopspring/decimal"
)

const (
	GatewayPricingRuleCreatedByManual             = "manual"
	GatewayPricingRuleCreatedByAutoReconciliation = "auto_reconciliation"
)

// GatewayPricingRule 是某厂商某模型在某个时间段内生效的单价（已统一换算成记账货币USD）。
// 同一 provider+model 可以有多条按时间versioned的记录，effective_to为NULL表示当前生效。
type GatewayPricingRule struct {
	ID                     int64            `gorm:"primaryKey" json:"id,string"`
	Provider               string           `gorm:"size:32;not null;index:idx_gateway_pricing_lookup,priority:1" json:"provider"`
	Model                  string           `gorm:"size:64;not null;index:idx_gateway_pricing_lookup,priority:2" json:"model"`
	CachedInputPricePerM   decimal.Decimal  `gorm:"column:cached_input_price_per_m;type:numeric(24,12);not null" json:"cached_input_price_per_m"`
	UncachedInputPricePerM decimal.Decimal  `gorm:"column:uncached_input_price_per_m;type:numeric(24,12);not null" json:"uncached_input_price_per_m"`
	OutputPricePerM        decimal.Decimal  `gorm:"column:output_price_per_m;type:numeric(24,12);not null" json:"output_price_per_m"`
	SourceCurrency         string           `gorm:"size:8;not null" json:"source_currency"`
	FxRateUsed             *decimal.Decimal `gorm:"type:numeric(24,12)" json:"fx_rate_used,omitempty"`
	CreatedBy              string           `gorm:"size:16;not null;default:manual" json:"created_by"`
	TriggeredByReportID    *int64           `json:"triggered_by_report_id,string,omitempty"`
	// DailyWindowStartMin/EndMin：分时定价的每日生效时段，北京时间(UTC+8)当日分钟数[0,1440)，左闭右开。
	// 两者都为 nil = 全天兜底价(平峰基准价)；StartMin>EndMin 表示跨零点的时段。
	DailyWindowStartMin *int `json:"daily_window_start_min,omitempty"`
	DailyWindowEndMin   *int `json:"daily_window_end_min,omitempty"`
	// InputTierStartTokens/EndTokens：按"单次请求输入token数(缓存命中+未命中)"分档定价的档位区间，
	// [start,end) 左闭右开。两者都为 nil = 不按输入长度分档。档外的输入落全天兜底价，
	// 所以分档模型的兜底价应按最高档配置，保证任何输入长度都不亏。
	InputTierStartTokens *int       `json:"input_tier_start_tokens,omitempty"`
	InputTierEndTokens   *int       `json:"input_tier_end_tokens,omitempty"`
	EffectiveFrom        time.Time  `gorm:"not null;index:idx_gateway_pricing_lookup,priority:3" json:"effective_from"`
	EffectiveTo          *time.Time `json:"effective_to,omitempty"`
}

func (GatewayPricingRule) TableName() string { return "gateway_pricing_rules" }
