package model

import (
	"time"

	"github.com/shopspring/decimal"
)

const (
	GatewayLedgerStatusSettled                  = "settled"
	GatewayLedgerStatusFailed                   = "failed"
	GatewayLedgerStatusRejectedInsufficientFunds = "rejected_insufficient_balance"
)

// GatewayLedgerEntry 是网关每次转发请求产生的一笔消费流水。
type GatewayLedgerEntry struct {
	ID               int64           `gorm:"primaryKey" json:"id,string"`
	WalletID         int64           `gorm:"not null;index" json:"wallet_id,string"`
	VirtualKeyID     int64           `gorm:"not null" json:"virtual_key_id,string"`
	RequestID        string          `gorm:"size:64;not null" json:"request_id"`
	Provider         string          `gorm:"size:32;not null" json:"provider"`
	Model            string          `gorm:"size:64;not null" json:"model"`
	PromptTokens     int             `gorm:"not null;default:0" json:"prompt_tokens"`
	CachedTokens     int             `gorm:"not null;default:0" json:"cached_tokens"`
	CompletionTokens int             `gorm:"not null;default:0" json:"completion_tokens"`
	ReasoningTokens  int             `gorm:"not null;default:0" json:"reasoning_tokens"`
	Cost             decimal.Decimal `gorm:"type:numeric(24,12);not null;default:0" json:"cost"`
	BalanceAfter     *decimal.Decimal `gorm:"type:numeric(24,12)" json:"balance_after,omitempty"`
	Status           string          `gorm:"size:16;not null" json:"status"`
	CreatedAt        time.Time       `json:"created_at"`
}

func (GatewayLedgerEntry) TableName() string { return "gateway_ledger_entries" }
