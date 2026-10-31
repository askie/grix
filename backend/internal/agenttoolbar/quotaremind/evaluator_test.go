package quotaremind_test

import (
	"testing"

	toolprotocol "github.com/askie/grix/backend/internal/agenttoolbar/protocol"
	"github.com/askie/grix/backend/internal/agenttoolbar/quotaremind"
)

func TestEvaluateQuotaAlert_5HourTakesPriorityOverWeekly(t *testing.T) {
	// 5H 剩余 20%（未达阈值），周限额剩余 5%（已达阈值）。
	// 按照需求：有 5 小时限额时严格以 5 小时为准，因此不触发。
	items := []toolprotocol.Item{
		{
			ItemID:                "rate_limit_primary",
			Kind:                  toolprotocol.ItemKindProgress,
			CenterText:            "5H",
			ProgressWindowMinutes: 300,
			Percent:               80.0,
			ProgressDetail:        "2026-10-31T14:00:00Z",
		},
		{
			ItemID:                "rate_limit_secondary",
			Kind:                  toolprotocol.ItemKindProgress,
			CenterText:            "7D",
			ProgressWindowMinutes: 10080,
			Percent:               95.0,
			ProgressDetail:        "2026-11-07T00:00:00Z",
		},
	}

	alert, triggered := quotaremind.EvaluateQuotaAlert(items)
	if triggered {
		t.Fatalf("expected not triggered when 5H has 20%% remaining, got alert: %+v", alert)
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
	if alert.ResetsAt != "2026-10-31T14:00:00Z" {
		t.Fatalf("expected resetsAt 2026-10-31T14:00:00Z, got %q", alert.ResetsAt)
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

func TestEvaluateQuotaAlert_VariousAgentSnapshots(t *testing.T) {
	tests := []struct {
		name         string
		items        []toolprotocol.Item
		wantTrigger  bool
		wantWindow   string
		wantRemain   float64
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
