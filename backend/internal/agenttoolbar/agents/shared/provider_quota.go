package shared

import (
	"fmt"
	"strings"

	toolprotocol "github.com/askie/grix/backend/internal/agenttoolbar/protocol"
)

// QuotaTier represents a single usage tier (e.g. 5-hour window, weekly limit).
type QuotaTier struct {
	Name        string
	Label       string
	UsedPercent float64
	ResetsAt    string
}

// BalanceInfo represents account balance data.
type BalanceInfo struct {
	Kind      string
	Remaining float64
	Total     float64
	Used      float64
	HasTotal  bool
	HasUsed   bool
	Unit      string
	ResetsAt  string
}

// ProviderQuota holds the parsed provider quota data from binding meta.
type ProviderQuota struct {
	Provider      string
	ProviderLabel string
	PlanName      string
	Tiers         []QuotaTier
	Balance       *BalanceInfo
	Error         string
}

// ParseProviderQuota extracts provider quota from binding meta.
func ParseProviderQuota(meta map[string]any) *ProviderQuota {
	if meta == nil {
		return nil
	}
	raw, ok := meta["provider_quota"]
	if !ok || raw == nil {
		return nil
	}
	quotaMap, ok := raw.(map[string]any)
	if !ok {
		return nil
	}

	provider, _ := quotaMap["provider"].(string)
	providerLabel, _ := quotaMap["providerLabel"].(string)
	planName, _ := quotaMap["planName"].(string)
	success, hasSuccess := quotaMap["success"].(bool)
	errMsg, _ := quotaMap["error"].(string)
	if providerLabel == "" {
		providerLabel = provider
	}
	if hasSuccess && !success {
		if strings.TrimSpace(errMsg) == "" {
			errMsg = "额度查询不可用"
		}
		return &ProviderQuota{
			Provider: provider, ProviderLabel: providerLabel, PlanName: planName,
			Tiers: []QuotaTier{}, Error: errMsg,
		}
	}

	tiers := parseQuotaTiers(quotaMap)
	balance := parseBalance(quotaMap)

	if len(tiers) == 0 && balance == nil && strings.TrimSpace(errMsg) == "" {
		return nil
	}

	return &ProviderQuota{
		Provider:      provider,
		ProviderLabel: providerLabel,
		PlanName:      planName,
		Tiers:         tiers,
		Balance:       balance,
		Error:         errMsg,
	}
}

// ParseProviderQuotaFromResult extracts provider quota from the raw get_rate_limits result.
// Unlike ParseProviderQuota which reads from persisted meta, this also handles error states.
func ParseProviderQuotaFromResult(result map[string]any) *ProviderQuota {
	quotaMap := nestedQuotaMap(result, "providerQuota")
	if quotaMap == nil {
		return nil
	}
	return ParseProviderQuota(map[string]any{"provider_quota": quotaMap})
}

func nestedQuotaMap(parent map[string]any, key string) map[string]any {
	if len(parent) == 0 {
		return nil
	}
	m, ok := parent[key].(map[string]any)
	if !ok {
		return nil
	}
	return m
}

func parseQuotaTiers(quotaMap map[string]any) []QuotaTier {
	raw, ok := quotaMap["tiers"]
	if !ok || raw == nil {
		return nil
	}
	arr, ok := raw.([]any)
	if !ok || len(arr) == 0 {
		return nil
	}

	var tiers []QuotaTier
	for _, item := range arr {
		entry, ok := item.(map[string]any)
		if !ok {
			continue
		}
		name, _ := entry["name"].(string)
		label, _ := entry["label"].(string)
		usedPercent := quotaMetaFloat64(entry, "usedPercent")
		resetsAt, _ := entry["resetsAt"].(string)

		if label == "" {
			label = tierLabelFromName(name)
		}

		tiers = append(tiers, QuotaTier{
			Name:        name,
			Label:       label,
			UsedPercent: usedPercent,
			ResetsAt:    resetsAt,
		})
	}
	return tiers
}

func parseBalance(quotaMap map[string]any) *BalanceInfo {
	raw, ok := quotaMap["balance"]
	if !ok || raw == nil {
		return nil
	}
	entry, ok := raw.(map[string]any)
	if !ok {
		return nil
	}
	kind, _ := entry["kind"].(string)
	unit, _ := entry["unit"].(string)
	if unit == "" {
		unit = "CNY"
	}
	resetsAt, _ := entry["resetsAt"].(string)
	return &BalanceInfo{
		Kind:      strings.TrimSpace(kind),
		Remaining: quotaMetaFloat64(entry, "remaining"),
		Total:     quotaMetaFloat64(entry, "total"),
		Used:      quotaMetaFloat64(entry, "used"),
		HasTotal:  quotaMetaHasNumber(entry, "total"),
		HasUsed:   quotaMetaHasNumber(entry, "used"),
		Unit:      unit,
		ResetsAt:  resetsAt,
	}
}

// BuildProviderQuotaItems creates toolbar progress items from provider quota data.
// quota 为 nil（尚无数据）时不渲染占位：用量未知不等于 0%，避免新建会话出现假的 5H 圈。
// 真实数据到达后，usedPercent==0 的档位仍会被跳过。
func BuildProviderQuotaItems(quota *ProviderQuota, hasLocalAction bool) []toolprotocol.Item {
	if quota == nil {
		return nil
	}
	var items []toolprotocol.Item

	for _, tier := range quota.Tiers {
		// 用量为 0 的限额档不渲染（5H/7D 等），与 Cursor/Claude/Codex 工具栏一致。
		if tier.UsedPercent == 0 {
			continue
		}
		centerText := tierLabelFromName(tier.Name)
		if centerText == "" {
			centerText = tier.Label
		}
		items = append(items, toolprotocol.Item{
			ItemID:         "provider_quota_" + tier.Name,
			GroupID:        "provider_quota",
			Kind:           toolprotocol.ItemKindProgress,
			ActionID:       "provider_quota_" + tier.Name,
			Variant:        "secondary",
			Percent:        tier.UsedPercent,
			CenterText:     centerText,
			ProgressDesc:   "quota_" + tier.Name + "_usage",
			ProgressDetail: tier.ResetsAt,
			LocalAction:    "get_rate_limits",
		})
	}

	if quota.Balance != nil {
		if shouldUseCurrencyBalanceButton(quota.Balance) {
			items = append(items, buildCurrencyBalanceButton(quota.ProviderLabel, quota.Balance))
		} else {
			items = append(items, buildBalanceProgressItem(quota.Balance))
		}
	}

	// 查询失败（quota.Error 非空）不渲染错误项：错误的按钮不展示给用户，
	// 与 nil quota 一样静默缺省，等待下次刷新数据到达。

	return items
}

func buildCurrencyBalanceButton(providerLabel string, balance *BalanceInfo) toolprotocol.Item {
	label := formatCurrencyCompactLabel(balance.Remaining, balance.Unit)
	badge := strings.ToUpper(strings.TrimSpace(balance.Unit))
	if badge == "RMB" {
		badge = "CNY"
	}
	detail := fmt.Sprintf("%.2f %s", balance.Remaining, balance.Unit)
	tooltip := currencyBalanceTooltip(providerLabel, label, detail)
	return toolprotocol.Item{
		ItemID:      "provider_quota_balance",
		GroupID:     "provider_quota",
		Kind:        toolprotocol.ItemKindButton,
		ActionID:    "provider_quota_balance",
		Label:       label,
		Variant:     "secondary",
		Tooltip:     tooltip,
		BadgeText:   badge,
		LocalAction: "get_rate_limits",
	}
}

func buildBalanceProgressItem(balance *BalanceInfo) toolprotocol.Item {
	pct := float64(0)
	centerText := "Bal"
	detail := ""
	if balance.Total > 0 {
		pct = (balance.Used / balance.Total) * 100
		if pct < 1 {
			pct = 1
		} else if pct > 99 {
			pct = 99
		}
		detail = fmt.Sprintf("%.0f/%.0f", balance.Used, balance.Total)
		centerText = fmt.Sprintf("%d%%", int(pct))
	} else {
		// DeepSeek / SiliconFlow 等只给 remaining 的厂商：展示余额而非 0/0。
		detail = fmt.Sprintf("%.2f %s", balance.Remaining, balance.Unit)
		switch strings.ToUpper(strings.TrimSpace(balance.Unit)) {
		case "CNY", "RMB":
			centerText = "¥"
		case "USD":
			centerText = "$"
		default:
			centerText = "Bal"
		}
		if balance.Remaining > 0 {
			pct = 1
		}
	}
	if balance.ResetsAt != "" {
		detail = balance.ResetsAt
	}
	return toolprotocol.Item{
		ItemID:         "provider_quota_balance",
		GroupID:        "provider_quota",
		Kind:           toolprotocol.ItemKindProgress,
		ActionID:       "provider_quota_balance",
		Variant:        "secondary",
		Percent:        pct,
		CenterText:     centerText,
		ProgressDesc:   "quota_balance",
		ProgressDetail: detail,
		LocalAction:    "get_rate_limits",
	}
}

func shouldUseCurrencyBalanceButton(balance *BalanceInfo) bool {
	if balance == nil || !isCurrencyBalance(balance) {
		return false
	}
	// 金额余额且无法用 total/used 计算百分比时，走普通 button，避免假圆形 progress。
	return !canComputeBalancePercent(balance)
}

func isCurrencyBalance(balance *BalanceInfo) bool {
	if balance == nil {
		return false
	}
	if strings.EqualFold(strings.TrimSpace(balance.Kind), "currency") {
		return true
	}
	// 旧插件兼容：无 kind，但 unit 是法币且 total/used 为空。
	if strings.TrimSpace(balance.Kind) != "" {
		return false
	}
	return isCurrencyUnit(balance.Unit) && !balance.HasTotal && !balance.HasUsed
}

func canComputeBalancePercent(balance *BalanceInfo) bool {
	if balance == nil {
		return false
	}
	return balance.HasTotal && balance.Total > 0
}

func isCurrencyUnit(unit string) bool {
	switch strings.ToUpper(strings.TrimSpace(unit)) {
	case "CNY", "RMB", "USD":
		return true
	default:
		return false
	}
}

func formatCurrencyCompactLabel(remaining float64, unit string) string {
	switch strings.ToUpper(strings.TrimSpace(unit)) {
	case "CNY", "RMB":
		return fmt.Sprintf("¥%.2f", remaining)
	case "USD":
		return fmt.Sprintf("$%.2f", remaining)
	default:
		u := strings.TrimSpace(unit)
		if u == "" {
			return fmt.Sprintf("%.2f", remaining)
		}
		return fmt.Sprintf("%.2f %s", remaining, u)
	}
}

func currencyBalanceTooltip(providerLabel, compactLabel, detail string) string {
	label := strings.TrimSpace(providerLabel)
	amount := strings.TrimSpace(compactLabel)
	if amount == "" {
		amount = strings.TrimSpace(detail)
	}
	switch {
	case label != "" && amount != "":
		return fmt.Sprintf("%s 剩余余额 %s，点击刷新", label, amount)
	case amount != "":
		return fmt.Sprintf("剩余余额 %s，点击刷新", amount)
	case label != "":
		return fmt.Sprintf("%s 剩余余额，点击刷新", label)
	default:
		return "剩余余额，点击刷新"
	}
}

func tierLabelFromName(name string) string {
	switch name {
	case "five_hour":
		return "5H"
	case "weekly_limit":
		return "7D"
	default:
		return ""
	}
}

func quotaMetaFloat64(meta map[string]any, key string) float64 {
	if meta == nil {
		return 0
	}
	value, ok := meta[key]
	if !ok || value == nil {
		return 0
	}
	switch v := value.(type) {
	case float64:
		return v
	case float32:
		return float64(v)
	case int:
		return float64(v)
	case int64:
		return float64(v)
	default:
		return 0
	}
}

func quotaMetaHasNumber(meta map[string]any, key string) bool {
	if meta == nil {
		return false
	}
	value, ok := meta[key]
	if !ok || value == nil {
		return false
	}
	switch value.(type) {
	case float64, float32, int, int64:
		return true
	default:
		return false
	}
}
