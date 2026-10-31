package quotaremind

import (
	"strings"

	toolprotocol "github.com/askie/grix/backend/internal/agenttoolbar/protocol"
)

const (
	WindowFiveHour = "five_hour"
	WindowWeekly   = "weekly"

	// QuotaAlertRemainingThreshold 定义触发提醒的剩余比例上限（10%）。
	QuotaAlertRemainingThreshold = 10.0
)

// QuotaAlert 封装从工具栏快照中识别出的低额度预警信息。
type QuotaAlert struct {
	ItemID           string
	WindowType       string // WindowFiveHour 或 WindowWeekly
	UsedPercent      float64
	RemainingPercent float64
	ResetsAt         string
}

// EvaluateQuotaAlert 统一从快照条目中判定限额是否触达告警阈值。
// 规则：
// 1. 优先检查 5 小时限额条目；
// 2. 若无 5 小时限额条目，再回退检查周限额条目；
// 3. 仅当所选限额的剩余比例 <= 10.0% 时触发预警。
func EvaluateQuotaAlert(items []toolprotocol.Item) (QuotaAlert, bool) {
	if len(items) == 0 {
		return QuotaAlert{}, false
	}

	// 1. 第一优先级：寻找 5 小时限额条目
	for _, item := range items {
		if isFiveHourItem(item) {
			return checkThreshold(item, WindowFiveHour)
		}
	}

	// 2. 第二优先级：若无 5 小时条目，寻找周限额条目
	for _, item := range items {
		if isWeeklyItem(item) {
			return checkThreshold(item, WindowWeekly)
		}
	}

	return QuotaAlert{}, false
}

func isFiveHourItem(item toolprotocol.Item) bool {
	if item.Kind != toolprotocol.ItemKindProgress {
		return false
	}
	if item.ProgressWindowMinutes == 300 {
		return true
	}
	if strings.EqualFold(strings.TrimSpace(item.CenterText), "5H") {
		return true
	}
	id := strings.ToLower(item.ItemID)
	return strings.Contains(id, "five_hour") || strings.Contains(id, "5h")
}

func isWeeklyItem(item toolprotocol.Item) bool {
	if item.Kind != toolprotocol.ItemKindProgress {
		return false
	}
	if item.ProgressWindowMinutes == 10080 {
		return true
	}
	center := strings.ToUpper(strings.TrimSpace(item.CenterText))
	if center == "7D" || center == "W" {
		return true
	}
	id := strings.ToLower(item.ItemID)
	return strings.Contains(id, "weekly") || strings.Contains(id, "7d")
}

func checkThreshold(item toolprotocol.Item, windowType string) (QuotaAlert, bool) {
	used := item.Percent
	if used <= 0 {
		return QuotaAlert{}, false
	}
	if used > 100 {
		used = 100
	}
	remaining := 100.0 - used
	if remaining <= QuotaAlertRemainingThreshold {
		return QuotaAlert{
			ItemID:           item.ItemID,
			WindowType:       windowType,
			UsedPercent:      used,
			RemainingPercent: remaining,
			ResetsAt:         strings.TrimSpace(item.ProgressDetail),
		}, true
	}
	return QuotaAlert{}, false
}
