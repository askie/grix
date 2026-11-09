package quotaremind

import (
	"fmt"
	"strconv"
	"strings"

	toolprotocol "github.com/askie/grix/backend/internal/agenttoolbar/protocol"
)

const (
	WindowFiveHour = "five_hour"
	WindowWeekly   = "weekly"
	WindowCredits  = "credits"

	// Tier10 和 Tier5 定义限额预警的两个阶梯
	Tier10 = "10"
	Tier5  = "5"
	// TierCreditOnce Credit 低余额提醒只有一档，周期内最多一次。
	TierCreditOnce = "once"

	// QuotaAlertRemainingThreshold 定义第一档预警阈值（剩余 <= 10%）。
	QuotaAlertRemainingThreshold = 10.0
	// QuotaAlertTier5Threshold 定义第二档终局预警阈值（剩余 <= 5%）。
	QuotaAlertTier5Threshold = 5.0
	// CreditAlertBalanceThreshold Credit 余额低于该值时触发一次交接提醒（Codex 等）。
	CreditAlertBalanceThreshold = 1000.0

	accountCreditsItemID = "account_credits"
)

// QuotaAlert 封装从工具栏快照中识别出的低额度预警信息。
type QuotaAlert struct {
	ItemID           string
	WindowType       string  // WindowFiveHour / WindowWeekly / WindowCredits
	Tier             string  // Tier10 / Tier5 / TierCreditOnce
	ThresholdInt     int     // 10 或 5；Credit 提醒为 1000
	UsedPercent      float64 // 用量百分比（Credit 提醒为 0）
	RemainingPercent float64 // 剩余百分比；Credit 提醒存放余额本身
	ResetsAt         string
}

// EvaluateQuotaAlert 统一检查 5 小时和周限额，在触达阈值的条目中返回剩余比例最低者。
// 剩余比例并列时优先返回 5 小时限额。
func EvaluateQuotaAlert(items []toolprotocol.Item) (QuotaAlert, bool) {
	var selected QuotaAlert
	triggered := false
	for _, item := range items {
		windowType := ""
		switch {
		case isFiveHourItem(item):
			windowType = WindowFiveHour
		case isWeeklyItem(item):
			windowType = WindowWeekly
		default:
			continue
		}
		alert, ok := checkThreshold(item, windowType)
		if !ok {
			continue
		}
		if !triggered || alert.RemainingPercent < selected.RemainingPercent ||
			(alert.RemainingPercent == selected.RemainingPercent && windowType == WindowFiveHour && selected.WindowType == WindowWeekly) {
			selected = alert
			triggered = true
		}
	}
	return selected, triggered
}

// EvaluateCreditAlert 从快照中识别 Credit 余额是否低于阈值。
// 仅识别 item_id=account_credits（Codex 等工具栏额度环）；无限额度不提醒；
// 周期内由 limiter 保证最多发送一次。
func EvaluateCreditAlert(items []toolprotocol.Item) (QuotaAlert, bool) {
	for _, item := range items {
		if item.Kind != toolprotocol.ItemKindProgress {
			continue
		}
		if strings.TrimSpace(item.ItemID) != accountCreditsItemID {
			continue
		}
		if isUnlimitedCreditsItem(item) {
			return QuotaAlert{}, false
		}
		balance, ok := parseCreditsBalance(item)
		if !ok {
			return QuotaAlert{}, false
		}
		if balance >= CreditAlertBalanceThreshold {
			return QuotaAlert{}, false
		}
		return QuotaAlert{
			ItemID:           item.ItemID,
			WindowType:       WindowCredits,
			Tier:             TierCreditOnce,
			ThresholdInt:     int(CreditAlertBalanceThreshold),
			UsedPercent:      0,
			RemainingPercent: balance,
			ResetsAt:         "below_1000",
		}, true
	}
	return QuotaAlert{}, false
}

func isUnlimitedCreditsItem(item toolprotocol.Item) bool {
	center := strings.TrimSpace(item.CenterText)
	if center == "∞" || strings.EqualFold(center, "inf") || strings.EqualFold(center, "unlimited") {
		return true
	}
	detail := strings.TrimSpace(item.ProgressDetail)
	return detail == "无限额度" || strings.EqualFold(detail, "unlimited")
}

func parseCreditsBalance(item toolprotocol.Item) (float64, bool) {
	detail := strings.TrimSpace(item.ProgressDetail)
	if detail != "" {
		var bal float64
		if _, err := fmt.Sscanf(detail, "剩余 %f", &bal); err == nil {
			return bal, true
		}
		// 兼容可能的英文 detail
		if _, err := fmt.Sscanf(strings.ToLower(detail), "remaining %f", &bal); err == nil {
			return bal, true
		}
		if v, err := strconv.ParseFloat(detail, 64); err == nil {
			return v, true
		}
	}
	center := strings.TrimSpace(item.CenterText)
	if center == "" || center == "∞" {
		return 0, false
	}
	// CenterText 可能是压缩标签（如 1.2k）；仅接受纯数字，避免误解析。
	if v, err := strconv.ParseFloat(center, 64); err == nil {
		return v, true
	}
	return 0, false
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
	id := strings.ToLower(strings.TrimSpace(item.ItemID))
	center := strings.ToUpper(strings.TrimSpace(item.CenterText))
	// Cursor 把 connector 的 sevenDay 槽位渲染成 API 分项，并把
	// ProgressWindowMinutes 设成与周账期相同的 10080。API 用量不是「本周」
	// included 限额，绝不能参与周限额交接提醒。
	if id == "rate_limit_api" || center == "API" {
		return false
	}
	if item.ProgressWindowMinutes == 10080 {
		return true
	}
	if center == "7D" || center == "W" {
		return true
	}
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
	if remaining <= QuotaAlertTier5Threshold {
		return QuotaAlert{
			ItemID:           item.ItemID,
			WindowType:       windowType,
			Tier:             Tier5,
			ThresholdInt:     5,
			UsedPercent:      used,
			RemainingPercent: remaining,
			ResetsAt:         strings.TrimSpace(item.ProgressDetail),
		}, true
	}
	if remaining <= QuotaAlertRemainingThreshold {
		return QuotaAlert{
			ItemID:           item.ItemID,
			WindowType:       windowType,
			Tier:             Tier10,
			ThresholdInt:     10,
			UsedPercent:      used,
			RemainingPercent: remaining,
			ResetsAt:         strings.TrimSpace(item.ProgressDetail),
		}, true
	}
	return QuotaAlert{}, false
}
