package reconcile

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/shopspring/decimal"
)

// DeepSeekBalanceChecker 查 DeepSeek 官方账户当前总余额（人民币）。
// 只用于对账这个低频场景，不在计费热路径上。
type DeepSeekBalanceChecker struct {
	apiKey     string
	httpClient *http.Client
}

func NewDeepSeekBalanceChecker(apiKey string) *DeepSeekBalanceChecker {
	return &DeepSeekBalanceChecker{apiKey: apiKey, httpClient: &http.Client{Timeout: 15 * time.Second}}
}

type deepseekBalanceResponse struct {
	IsAvailable bool `json:"is_available"`
	BalanceInfos []struct {
		Currency      string `json:"currency"`
		TotalBalance  string `json:"total_balance"`
	} `json:"balance_infos"`
}

// CurrentBalanceCNY 返回 DeepSeek 账户当前的人民币总余额。
func (c *DeepSeekBalanceChecker) CurrentBalanceCNY(ctx context.Context) (decimal.Decimal, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.deepseek.com/user/balance", nil)
	if err != nil {
		return decimal.Zero, err
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return decimal.Zero, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return decimal.Zero, fmt.Errorf("deepseek balance query error: %d", resp.StatusCode)
	}

	var parsed deepseekBalanceResponse
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return decimal.Zero, fmt.Errorf("parse deepseek balance response: %w", err)
	}
	for _, info := range parsed.BalanceInfos {
		if info.Currency == "CNY" {
			return decimal.NewFromString(info.TotalBalance)
		}
	}
	return decimal.Zero, fmt.Errorf("deepseek balance response has no CNY entry")
}
