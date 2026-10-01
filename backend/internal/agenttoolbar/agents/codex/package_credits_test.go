package codex

import (
	"fmt"
	"testing"
	"unicode/utf8"

	"github.com/askie/grix/backend/internal/agenttoolbar/core"
	toolruntime "github.com/askie/grix/backend/internal/agenttoolbar/runtime"
)

func TestBuildCodexRateLimitItemsCreditsVisibility(t *testing.T) {
	tests := []struct {
		name       string
		credits    map[string]any
		wantCredit bool
		wantCenter string
		wantDetail string
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
			wantCenter: "1.5",
			wantDetail: "剩余 1.5",
		},
		{
			name:       "unlimited remains visible with zero balance",
			credits:    map[string]any{"hasCredits": true, "unlimited": true, "balance": float64(0)},
			wantCredit: true,
			wantCenter: "∞",
			wantDetail: "无限额度",
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
			wantCenter: "-1.0",
			wantDetail: "剩余 -1.0",
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
					if item.CenterText != tt.wantCenter || item.ProgressDetail != tt.wantDetail {
						t.Fatalf("credits text = (%q, %q), want (%q, %q)",
							item.CenterText, item.ProgressDetail, tt.wantCenter, tt.wantDetail)
					}
					break
				}
			}
			if gotCredit != tt.wantCredit {
				t.Fatalf("account_credits visibility = %v, want %v", gotCredit, tt.wantCredit)
			}
		})
	}
}

func TestBuildCodexRateLimitItemsCreditsCompactText(t *testing.T) {
	tests := []struct {
		balance float64
		want    string
	}{
		{0.1, "0.1"},
		{1, "1.0"},
		{12.34, "12.3"},
		{99.95, "99.9"},
		{999, "999"},
		{999.99, "999"},
		{1000, "1k"},
		{1099.99, "1k"},
		{1999.99, "1.9k"},
		{6250, "6.2k"},
		{6250.99, "6.2k"},
		{9999.99, "9.9k"},
		{10000, "10k"},
		{62500, "62k"},
		{625000, "625k"},
		{999999.99, "999k"},
		{1000000, "1M"},
		{6250000, "6.2M"},
		{62500000, "62M"},
		{625000000, "625M"},
		{999999999.99, "999M"},
		{1000000000, "1B"},
		{6250000000, "6.2B"},
		{62500000000, "62B"},
		{999999999999, "999B"},
		{1000000000000, "1T"},
		{6250000000000, "6.2T"},
		{1e15, "1P"},
		{1e18, "1E"},
		{1e21, "≥1E"},
		{-1, "-1.0"},
		{-12.34, "-12"},
		{-999.99, "-999"},
		{-1000, "-1k"},
		{-6250, "-6k"},
		{-62500, "-62k"},
		{-625000, "-.6M"},
		{-999999, "-.9M"},
		{-1000000, "-1M"},
		{-6250000000, "-6B"},
		{-1e21, "≤-1E"},
	}
	for _, tt := range tests {
		t.Run(fmt.Sprintf("%g", tt.balance), func(t *testing.T) {
			items := buildCodexRateLimitItems(core.BuildInput{
				Runtime: toolruntime.Profile{Online: true, LocalActions: []string{"get_rate_limits"}},
				Binding: core.BindingInfo{Meta: map[string]any{
					"rate_limits": map[string]any{"sampledAt": "2026-08-07T00:00:00Z"},
					"credits":     map[string]any{"hasCredits": true, "balance": tt.balance},
				}},
			})
			if len(items) != 1 || items[0].ItemID != "account_credits" {
				t.Fatalf("items = %+v, want one account_credits item", items)
			}
			item := items[0]
			if item.CenterText != tt.want {
				t.Fatalf("center_text = %q, want %q", item.CenterText, tt.want)
			}
			if utf8.RuneCountInString(item.CenterText) > 4 {
				t.Fatalf("center_text = %q exceeds the four-character ring budget", item.CenterText)
			}
			if wantDetail := fmt.Sprintf("剩余 %.1f", tt.balance); item.ProgressDetail != wantDetail {
				t.Fatalf("progress_detail = %q, want full balance %q", item.ProgressDetail, wantDetail)
			}
			t.Logf("balance %g -> %s; detail: %s", tt.balance, item.CenterText, item.ProgressDetail)
		})
	}
}

func TestBuildCodexExtraRateLimitKeepsResetTimeAndWindow(t *testing.T) {
	items := buildCodexRateLimitItems(core.BuildInput{
		Runtime: toolruntime.Profile{Online: true, LocalActions: []string{"get_rate_limits"}},
		Binding: core.BindingInfo{Meta: map[string]any{
			"rate_limits": map[string]any{"sampledAt": "2026-08-07T00:00:00Z"},
			"extra_limits": []any{map[string]any{
				"label":         "GPT weekly",
				"usedPercent":   32.0,
				"windowMinutes": float64(10080),
				"resetsAt":      "2026-08-28T00:00:57Z",
			}},
		}},
	})

	if len(items) != 1 {
		t.Fatalf("rate limit items = %d, want 1", len(items))
	}
	item := items[0]
	if item.ProgressDetail != "2026-08-28T00:00:57Z" {
		t.Fatalf("progress_detail = %q, want raw reset time", item.ProgressDetail)
	}
	if item.ProgressWindowMinutes != 10080 {
		t.Fatalf("progress_window_minutes = %v, want 10080", item.ProgressWindowMinutes)
	}
}

func TestBuildCodexRateLimitWindowWithoutResetKeepsDetailEmpty(t *testing.T) {
	item := buildCodexRateLimitProgressItem(
		"rate_limit_extra_0", "rate_limits", "32", "GPT weekly", 32, 10080, "",
	)
	if item.ProgressDetail != "" {
		t.Fatalf("progress_detail = %q, want empty without reset timestamp", item.ProgressDetail)
	}
	if item.ProgressWindowMinutes != 10080 {
		t.Fatalf("progress_window_minutes = %v, want 10080", item.ProgressWindowMinutes)
	}
}
