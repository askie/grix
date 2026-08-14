package codex

import (
	"testing"

	"github.com/askie/grix/backend/internal/agenttoolbar/core"
	toolruntime "github.com/askie/grix/backend/internal/agenttoolbar/runtime"
)

func TestBuildCodexRateLimitItemsCreditsVisibility(t *testing.T) {
	tests := []struct {
		name       string
		credits    map[string]any
		wantCredit bool
	}{
		{
			name:       "zero balance is hidden",
			credits:    map[string]any{"hasCredits": true, "balance": float64(0)},
			wantCredit: false,
		},
		{
			name:       "positive balance remains visible",
			credits:    map[string]any{"hasCredits": true, "balance": 1.5},
			wantCredit: true,
		},
		{
			name:       "unlimited remains visible with zero balance",
			credits:    map[string]any{"hasCredits": true, "unlimited": true, "balance": float64(0)},
			wantCredit: true,
		},
		{
			name:       "nil balance keeps existing visibility",
			credits:    map[string]any{"hasCredits": true, "balance": nil},
			wantCredit: true,
		},
		{
			name:       "negative balance keeps existing visibility",
			credits:    map[string]any{"hasCredits": true, "balance": -1.0},
			wantCredit: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			items := buildCodexRateLimitItems(core.BuildInput{
				Runtime: toolruntime.Profile{Online: true, LocalActions: []string{"get_rate_limits"}},
				Binding: core.BindingInfo{Meta: map[string]any{
					"rate_limits": map[string]any{"sampledAt": "2026-08-07T00:00:00Z"},
					"credits":     tt.credits,
				}},
			})

			gotCredit := false
			for _, item := range items {
				if item.ItemID == "account_credits" {
					gotCredit = true
					break
				}
			}
			if gotCredit != tt.wantCredit {
				t.Fatalf("account_credits visibility = %v, want %v", gotCredit, tt.wantCredit)
			}
		})
	}
}
