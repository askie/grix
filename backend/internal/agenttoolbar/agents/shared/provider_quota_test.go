package shared

import (
	"testing"

	toolprotocol "github.com/askie/grix/backend/internal/agenttoolbar/protocol"
)

func TestBuildProviderQuotaItemsCurrencyCNYButton(t *testing.T) {
	quota := ParseProviderQuota(map[string]any{
		"provider_quota": map[string]any{
			"provider":      "deepseek",
			"providerLabel": "DeepSeek",
			"balance": map[string]any{
				"kind":      "currency",
				"remaining": 12.34,
				"unit":      "CNY",
			},
		},
	})
	items := BuildProviderQuotaItems(quota, true)
	if len(items) != 1 {
		t.Fatalf("items=%d want=1", len(items))
	}
	item := items[0]
	if item.Kind != toolprotocol.ItemKindButton {
		t.Fatalf("kind=%q want=button", item.Kind)
	}
	if item.ItemID != "provider_quota_balance" || item.ActionID != "provider_quota_balance" {
		t.Fatalf("item/action=%q/%q", item.ItemID, item.ActionID)
	}
	if item.GroupID != "provider_quota" {
		t.Fatalf("group=%q", item.GroupID)
	}
	if item.LocalAction != "get_rate_limits" {
		t.Fatalf("localAction=%q", item.LocalAction)
	}
	if item.Label != "¥12.34" {
		t.Fatalf("label=%q want=¥12.34", item.Label)
	}
	if item.BadgeText != "CNY" {
		t.Fatalf("badge=%q want=CNY", item.BadgeText)
	}
	if item.Tooltip != "DeepSeek 剩余余额 ¥12.34，点击刷新" {
		t.Fatalf("tooltip=%q", item.Tooltip)
	}
}

func TestBuildProviderQuotaItemsCurrencyUSDButton(t *testing.T) {
	quota := ParseProviderQuota(map[string]any{
		"provider_quota": map[string]any{
			"provider":      "openrouter",
			"providerLabel": "OpenRouter",
			"balance": map[string]any{
				"kind":      "currency",
				"remaining": 12.34,
				"unit":      "USD",
			},
		},
	})
	items := BuildProviderQuotaItems(quota, true)
	if len(items) != 1 {
		t.Fatalf("items=%d want=1", len(items))
	}
	item := items[0]
	if item.Kind != toolprotocol.ItemKindButton {
		t.Fatalf("kind=%q want=button", item.Kind)
	}
	if item.Label != "$12.34" {
		t.Fatalf("label=%q want=$12.34", item.Label)
	}
	if item.BadgeText != "USD" {
		t.Fatalf("badge=%q want=USD", item.BadgeText)
	}
	if item.Tooltip != "OpenRouter 剩余余额 $12.34，点击刷新" {
		t.Fatalf("tooltip=%q", item.Tooltip)
	}
}

func TestBuildProviderQuotaItemsLegacyCurrencyWithoutKind(t *testing.T) {
	// 旧插件：无 kind，unit=CNY，total/used 为空（含 null）→ 按 currency 余额按钮处理。
	quota := ParseProviderQuota(map[string]any{
		"provider_quota": map[string]any{
			"provider":      "deepseek",
			"providerLabel": "DeepSeek",
			"balance": map[string]any{
				"remaining": 944.55,
				"total":     nil,
				"used":      nil,
				"unit":      "CNY",
			},
		},
	})
	if quota == nil || quota.Balance == nil {
		t.Fatal("quota/balance nil")
	}
	if quota.Balance.Kind != "" {
		t.Fatalf("kind=%q want empty", quota.Balance.Kind)
	}
	if quota.Balance.HasTotal || quota.Balance.HasUsed {
		t.Fatalf("HasTotal/HasUsed=%v/%v want false", quota.Balance.HasTotal, quota.Balance.HasUsed)
	}
	items := BuildProviderQuotaItems(quota, true)
	if len(items) != 1 {
		t.Fatalf("items=%d want=1", len(items))
	}
	item := items[0]
	if item.Kind != toolprotocol.ItemKindButton {
		t.Fatalf("kind=%q want=button", item.Kind)
	}
	if item.Label != "¥944.55" {
		t.Fatalf("label=%q want=¥944.55", item.Label)
	}
	if item.BadgeText != "CNY" {
		t.Fatalf("badge=%q want=CNY", item.BadgeText)
	}
}

func TestBuildProviderQuotaItemsWithTotalUsedKeepsProgress(t *testing.T) {
	quota := ParseProviderQuota(map[string]any{
		"provider_quota": map[string]any{
			"provider":      "demo",
			"providerLabel": "Demo",
			"balance": map[string]any{
				"remaining": 8.0,
				"total":     10.0,
				"used":      2.0,
				"unit":      "credits",
			},
		},
	})
	items := BuildProviderQuotaItems(quota, true)
	if len(items) != 1 {
		t.Fatalf("items=%d want=1", len(items))
	}
	item := items[0]
	if item.Kind != toolprotocol.ItemKindProgress {
		t.Fatalf("kind=%q want=progress", item.Kind)
	}
	if item.CenterText != "20%" {
		t.Fatalf("centerText=%q want=20%%", item.CenterText)
	}
	if item.ProgressDetail != "2/10" {
		t.Fatalf("progressDetail=%q want=2/10", item.ProgressDetail)
	}
	if item.Percent < 19 || item.Percent > 21 {
		t.Fatalf("percent=%v want~20", item.Percent)
	}
}

func TestParseBalanceCurrencyKind(t *testing.T) {
	quota := ParseProviderQuota(map[string]any{
		"provider_quota": map[string]any{
			"provider": "deepseek",
			"balance": map[string]any{
				"kind":      "currency",
				"remaining": 1.5,
				"unit":      "CNY",
			},
		},
	})
	if quota == nil || quota.Balance == nil {
		t.Fatal("nil")
	}
	if quota.Balance.Kind != "currency" {
		t.Fatalf("kind=%q", quota.Balance.Kind)
	}
	if !isCurrencyBalance(quota.Balance) {
		t.Fatal("expected currency balance")
	}
	if !shouldUseCurrencyBalanceButton(quota.Balance) {
		t.Fatal("expected currency button path")
	}
}

func TestBuildProviderQuotaItemsOmitsZeroUsedTiers(t *testing.T) {
	quota := ParseProviderQuota(map[string]any{
		"provider_quota": map[string]any{
			"provider": "demo",
			"tiers": []any{
				map[string]any{"name": "five_hour", "label": "5h", "usedPercent": 0.0, "resetsAt": "2026-08-08T00:00:00Z"},
				map[string]any{"name": "weekly_limit", "label": "7d", "usedPercent": 25.0, "resetsAt": "2026-08-14T00:00:00Z"},
			},
		},
	})
	items := BuildProviderQuotaItems(quota, true)
	if len(items) != 1 {
		t.Fatalf("items=%d want=1", len(items))
	}
	if items[0].ItemID != "provider_quota_weekly_limit" {
		t.Fatalf("itemID=%q want provider_quota_weekly_limit", items[0].ItemID)
	}
	if items[0].Percent != 25 {
		t.Fatalf("percent=%v want=25", items[0].Percent)
	}
}

func TestBuildProviderQuotaItemsOmitsAllZeroTiers(t *testing.T) {
	quota := ParseProviderQuota(map[string]any{
		"provider_quota": map[string]any{
			"provider": "demo",
			"tiers": []any{
				map[string]any{"name": "five_hour", "usedPercent": 0.0},
				map[string]any{"name": "weekly_limit", "usedPercent": 0.0},
			},
		},
	})
	items := BuildProviderQuotaItems(quota, true)
	if len(items) != 0 {
		t.Fatalf("items=%d want=0 when all tiers usedPercent=0", len(items))
	}
}

func TestBuildProviderQuotaItemsNilQuotaNoPlaceholder(t *testing.T) {
	items := BuildProviderQuotaItems(nil, true)
	if len(items) != 0 {
		t.Fatalf("items=%d want=0 when quota is nil (no fake 5H placeholder)", len(items))
	}
}

func TestParseProviderQuotaPersistedFailureWithoutErrorText(t *testing.T) {
	quota := ParseProviderQuota(map[string]any{"provider_quota": map[string]any{
		"provider": "deepseek", "providerLabel": "DeepSeek", "success": false,
	}})
	if quota == nil || quota.Error == "" {
		t.Fatalf("quota=%+v", quota)
	}
	items := BuildProviderQuotaItems(quota, true)
	if len(items) != 0 {
		t.Fatalf("items=%+v want=0 when quota query failed (no error button)", items)
	}
}
