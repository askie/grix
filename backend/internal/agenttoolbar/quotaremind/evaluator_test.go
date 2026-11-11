package quotaremind_test

import (
	"testing"

	toolprotocol "github.com/askie/grix/backend/internal/agenttoolbar/protocol"
	"github.com/askie/grix/backend/internal/agenttoolbar/quotaremind"
)

func TestEvaluateQuotaAlert_SelectsLowestTriggeredWindow(t *testing.T) {
	tests := []struct {
		name          string
		fiveHourUsed  float64
		weeklyUsed    float64
		wantWindow    string
		wantRemaining float64
	}{
		{name: "healthy 5H does not hide low weekly", fiveHourUsed: 80, weeklyUsed: 95, wantWindow: quotaremind.WindowWeekly, wantRemaining: 5},
		{name: "lower weekly wins when both are low", fiveHourUsed: 92, weeklyUsed: 96, wantWindow: quotaremind.WindowWeekly, wantRemaining: 4},
		{name: "lower 5H wins when both are low", fiveHourUsed: 97, weeklyUsed: 92, wantWindow: quotaremind.WindowFiveHour, wantRemaining: 3},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			items := []toolprotocol.Item{
				{
					ItemID:                "rate_limit_primary",
					Kind:                  toolprotocol.ItemKindProgress,
					CenterText:            "5H",
					ProgressWindowMinutes: 300,
					Percent:               tc.fiveHourUsed,
					ProgressDetail:        "2026-10-31T14:00:00Z",
				},
				{
					ItemID:                "rate_limit_secondary",
					Kind:                  toolprotocol.ItemKindProgress,
					CenterText:            "7D",
					ProgressWindowMinutes: 10080,
					Percent:               tc.weeklyUsed,
					ProgressDetail:        "2026-11-07T00:00:00Z",
				},
			}
			alert, triggered := quotaremind.EvaluateQuotaAlert(items)
			if !triggered {
				t.Fatal("expected a quota alert")
			}
			if alert.WindowType != tc.wantWindow || alert.RemainingPercent != tc.wantRemaining {
				t.Fatalf("alert=%+v want window=%q remaining=%v", alert, tc.wantWindow, tc.wantRemaining)
			}
		})
	}
}

func TestEvaluateQuotaAlert_5HourTriggered(t *testing.T) {
	// 5H 剩余 8%（已达阈值 <= 10%）
	items := []toolprotocol.Item{
		{
			ItemID:                "rate_limit_primary",
			Kind:                  toolprotocol.ItemKindProgress,
			CenterText:            "5H",
			ProgressWindowMinutes: 300,
			Percent:               92.0,
			ProgressDetail:        "2026-10-31T14:00:00Z",
		},
		{
			ItemID:                "rate_limit_secondary",
			Kind:                  toolprotocol.ItemKindProgress,
			CenterText:            "7D",
			ProgressWindowMinutes: 10080,
			Percent:               50.0,
			ProgressDetail:        "2026-11-07T00:00:00Z",
		},
	}

	alert, triggered := quotaremind.EvaluateQuotaAlert(items)
	if !triggered {
		t.Fatal("expected 5H alert triggered")
	}
	if alert.WindowType != quotaremind.WindowFiveHour {
		t.Fatalf("expected WindowFiveHour, got %q", alert.WindowType)
	}
	if alert.RemainingPercent != 8.0 {
		t.Fatalf("expected remaining 8.0, got %v", alert.RemainingPercent)
	}
	if alert.Tier != quotaremind.Tier10 {
		t.Fatalf("expected Tier10, got %q", alert.Tier)
	}
	if alert.ThresholdInt != 10 {
		t.Fatalf("expected ThresholdInt 10, got %d", alert.ThresholdInt)
	}
	if alert.ResetsAt != "2026-10-31T14:00:00Z" {
		t.Fatalf("expected resetsAt 2026-10-31T14:00:00Z, got %q", alert.ResetsAt)
	}
}

func TestEvaluateQuotaAlert_5HourTier5Triggered(t *testing.T) {
	// 5H 剩余 3.5%（已达阈值 <= 5%）
	items := []toolprotocol.Item{
		{
			ItemID:                "rate_limit_primary",
			Kind:                  toolprotocol.ItemKindProgress,
			CenterText:            "5H",
			ProgressWindowMinutes: 300,
			Percent:               96.5,
			ProgressDetail:        "2026-10-31T14:00:00Z",
		},
	}

	alert, triggered := quotaremind.EvaluateQuotaAlert(items)
	if !triggered {
		t.Fatal("expected 5H tier5 alert triggered")
	}
	if alert.Tier != quotaremind.Tier5 {
		t.Fatalf("expected Tier5, got %q", alert.Tier)
	}
	if alert.ThresholdInt != 5 {
		t.Fatalf("expected ThresholdInt 5, got %d", alert.ThresholdInt)
	}
	if alert.RemainingPercent != 3.5 {
		t.Fatalf("expected remaining 3.5, got %v", alert.RemainingPercent)
	}
}

func TestEvaluateQuotaAlert_WeeklyFallbackWhenNo5Hour(t *testing.T) {
	// 没有 5H，只有周限额，剩余 7%（<= 10%）
	items := []toolprotocol.Item{
		{
			ItemID:                "rate_limit_weekly",
			Kind:                  toolprotocol.ItemKindProgress,
			CenterText:            "W",
			ProgressWindowMinutes: 10080,
			Percent:               93.0,
			ProgressDetail:        "2026-11-07T00:00:00Z",
		},
	}

	alert, triggered := quotaremind.EvaluateQuotaAlert(items)
	if !triggered {
		t.Fatal("expected weekly fallback alert triggered")
	}
	if alert.WindowType != quotaremind.WindowWeekly {
		t.Fatalf("expected WindowWeekly, got %q", alert.WindowType)
	}
	if alert.RemainingPercent != 7.0 {
		t.Fatalf("expected remaining 7.0, got %v", alert.RemainingPercent)
	}
}

func TestEvaluateQuotaAlert_Neither5HNorWeeklyPresent(t *testing.T) {
	// 仅有月度或余额进度项，不参与裁决
	items := []toolprotocol.Item{
		{
			ItemID:         "rate_limit_monthly",
			Kind:           toolprotocol.ItemKindProgress,
			CenterText:     "M",
			Percent:        99.0,
			ProgressDetail: "2026-11-30T00:00:00Z",
		},
		{
			ItemID:     "provider_quota_balance",
			Kind:       toolprotocol.ItemKindButton,
			BadgeText:  "CNY",
			CenterText: "¥",
		},
	}

	_, triggered := quotaremind.EvaluateQuotaAlert(items)
	if triggered {
		t.Fatal("should not trigger when neither 5H nor weekly is present")
	}
}

func TestEvaluateQuotaAlert_CursorAPINotTreatedAsWeekly(t *testing.T) {
	// Cursor：W（included 周账期）剩余 27%，API 仅剩 5%。
	// API 虽挂着 ProgressWindowMinutes=10080，但不得当成「本周」触发提醒。
	items := []toolprotocol.Item{
		{
			ItemID:                "rate_limit_weekly",
			Kind:                  toolprotocol.ItemKindProgress,
			CenterText:            "W",
			ProgressWindowMinutes: 10080,
			Percent:               73.0,
			ProgressDetail:        "1793682441",
		},
		{
			ItemID:                "rate_limit_api",
			Kind:                  toolprotocol.ItemKindProgress,
			CenterText:            "API",
			ProgressWindowMinutes: 10080,
			Percent:               95.0,
			ProgressDetail:        "1793682441",
		},
	}

	_, triggered := quotaremind.EvaluateQuotaAlert(items)
	if triggered {
		t.Fatal("Cursor API ring must not trigger weekly handover when W still has ~27% remaining")
	}
}

func TestEvaluateQuotaAlert_CursorAPIAloneDoesNotTriggerWeekly(t *testing.T) {
	items := []toolprotocol.Item{
		{
			ItemID:                "rate_limit_api",
			Kind:                  toolprotocol.ItemKindProgress,
			CenterText:            "API",
			ProgressWindowMinutes: 10080,
			Percent:               96.0,
			ProgressDetail:        "1793682441",
		},
	}

	_, triggered := quotaremind.EvaluateQuotaAlert(items)
	if triggered {
		t.Fatal("API-only snapshot must not trigger a weekly quota reminder")
	}
}

func TestEvaluateCreditAlert_BelowThreshold(t *testing.T) {
	items := []toolprotocol.Item{
		{
			ItemID:         "account_credits",
			Kind:           toolprotocol.ItemKindProgress,
			CenterText:     "850.5",
			ProgressDetail: "剩余 850.5",
		},
	}
	alert, triggered := quotaremind.EvaluateCreditAlert(items)
	if !triggered {
		t.Fatal("expected credit alert when balance < 1000")
	}
	if alert.WindowType != quotaremind.WindowCredits {
		t.Fatalf("window=%q want credits", alert.WindowType)
	}
	if alert.Tier != quotaremind.TierCreditOnce {
		t.Fatalf("tier=%q want once", alert.Tier)
	}
	if alert.RemainingPercent != 850.5 {
		t.Fatalf("balance=%v want 850.5", alert.RemainingPercent)
	}
}

func TestEvaluateCreditAlert_AboveThresholdOrUnlimited(t *testing.T) {
	cases := []struct {
		name  string
		items []toolprotocol.Item
	}{
		{
			name: "balance above 1000",
			items: []toolprotocol.Item{{
				ItemID:         "account_credits",
				Kind:           toolprotocol.ItemKindProgress,
				CenterText:     "1.2k",
				ProgressDetail: "剩余 1200.0",
			}},
		},
		{
			name: "unlimited",
			items: []toolprotocol.Item{{
				ItemID:         "account_credits",
				Kind:           toolprotocol.ItemKindProgress,
				CenterText:     "∞",
				ProgressDetail: "无限额度",
			}},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, triggered := quotaremind.EvaluateCreditAlert(tc.items); triggered {
				t.Fatal("expected no credit alert")
			}
		})
	}
}

func TestEvaluateQuotaAlert_VariousAgentSnapshots(t *testing.T) {
	tests := []struct {
		name        string
		items       []toolprotocol.Item
		wantTrigger bool
		wantWindow  string
		wantRemain  float64
	}{
		{
			name: "ProviderQuota (DeepSeek/Gemini/Kimi) 5H low",
			items: []toolprotocol.Item{
				{
					ItemID:         "provider_quota_five_hour",
					Kind:           toolprotocol.ItemKindProgress,
					CenterText:     "5H",
					Percent:        95.5,
					ProgressDetail: "2026-10-31T15:00:00Z",
				},
				{
					ItemID:         "provider_quota_weekly_limit",
					Kind:           toolprotocol.ItemKindProgress,
					CenterText:     "7D",
					Percent:        30.0,
					ProgressDetail: "2026-11-07T15:00:00Z",
				},
			},
			wantTrigger: true,
			wantWindow:  quotaremind.WindowFiveHour,
			wantRemain:  4.5,
		},
		{
			name: "Claude 5H low",
			items: []toolprotocol.Item{
				{
					ItemID:                "rate_limit_5h",
					Kind:                  toolprotocol.ItemKindProgress,
					CenterText:            "5H",
					ProgressWindowMinutes: 300,
					Percent:               91.0,
					ProgressDetail:        "1793444000",
				},
			},
			wantTrigger: true,
			wantWindow:  quotaremind.WindowFiveHour,
			wantRemain:  9.0,
		},
		{
			name: "Codex weekly low (no 5H)",
			items: []toolprotocol.Item{
				{
					ItemID:                "rate_limit_secondary",
					Kind:                  toolprotocol.ItemKindProgress,
					CenterText:            "7D",
					ProgressWindowMinutes: 10080,
					Percent:               90.0,
					ProgressDetail:        "2026-11-05T08:00:00Z",
				},
			},
			wantTrigger: true,
			wantWindow:  quotaremind.WindowWeekly,
			wantRemain:  10.0, // exactly 10.0% remaining triggers
		},
		{
			name: "Threshold edge: 10.1% remaining does not trigger",
			items: []toolprotocol.Item{
				{
					ItemID:                "rate_limit_primary",
					Kind:                  toolprotocol.ItemKindProgress,
					CenterText:            "5H",
					ProgressWindowMinutes: 300,
					Percent:               89.9,
				},
			},
			wantTrigger: false,
		},
		{
			name: "Claude modelScoped (Fable) low does not trigger account weekly handover alert",
			items: []toolprotocol.Item{
				{
					ItemID:                "rate_limit_7d",
					Kind:                  toolprotocol.ItemKindProgress,
					CenterText:            "7D",
					ProgressWindowMinutes: 10080,
					Percent:               40.0,
					ProgressDetail:        "1793444000",
				},
				{
					ItemID:                "rate_limit_model_fable",
					Kind:                  toolprotocol.ItemKindProgress,
					CenterText:            "F",
					ProgressWindowMinutes: 10080,
					Percent:               95.0,
					ProgressDetail:        "1793444000",
				},
			},
			wantTrigger: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			alert, triggered := quotaremind.EvaluateQuotaAlert(tc.items)
			if triggered != tc.wantTrigger {
				t.Fatalf("triggered=%v want=%v", triggered, tc.wantTrigger)
			}
			if tc.wantTrigger {
				if alert.WindowType != tc.wantWindow {
					t.Fatalf("window=%q want=%q", alert.WindowType, tc.wantWindow)
				}
				if alert.RemainingPercent != tc.wantRemain {
					t.Fatalf("remaining=%v want=%v", alert.RemainingPercent, tc.wantRemain)
				}
			}
		})
	}
}
