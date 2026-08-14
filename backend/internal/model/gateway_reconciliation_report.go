package model

import (
	"time"

	"github.com/shopspring/decimal"
)

const (
	GatewayReconciliationStatusOK       = "ok"
	GatewayReconciliationStatusWarning  = "warning"
	GatewayReconciliationStatusCritical = "critical"
)

// GatewayReconciliationReport 记录每一轮对账窗口内"厂商真实花费"与"我方流水理论花费"的比对结果。
type GatewayReconciliationReport struct {
	ID                  int64           `gorm:"primaryKey" json:"id,string"`
	Provider            string          `gorm:"size:32;not null;index:idx_gateway_reconciliation_provider_window,priority:1" json:"provider"`
	WindowStart         time.Time       `gorm:"not null;index:idx_gateway_reconciliation_provider_window,priority:2" json:"window_start"`
	WindowEnd           time.Time       `gorm:"not null" json:"window_end"`
	VendorActualCost    decimal.Decimal `gorm:"type:numeric(24,12);not null" json:"vendor_actual_cost"`
	LedgerExpectedCost  decimal.Decimal `gorm:"type:numeric(24,12);not null" json:"ledger_expected_cost"`
	Diff                decimal.Decimal `gorm:"type:numeric(24,12);not null" json:"diff"`
	DiffRatio           *decimal.Decimal `gorm:"type:numeric(10,6)" json:"diff_ratio,omitempty"`
	Status              string          `gorm:"size:16;not null" json:"status"`
	AutoAdjusted        bool            `gorm:"not null;default:false" json:"auto_adjusted"`
	VendorBalanceSnapshot *decimal.Decimal `gorm:"type:numeric(24,12)" json:"vendor_balance_snapshot,omitempty"`
	CreatedAt           time.Time       `json:"created_at"`
}

func (GatewayReconciliationReport) TableName() string { return "gateway_reconciliation_reports" }
