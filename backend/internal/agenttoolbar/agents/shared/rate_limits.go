package shared

import (
	"fmt"
	"math"
)

// CreditsInfo represents the account credits from meta["credits"].
// This is an agent-agnostic key: codex, claude, and cursor may all emit it.
type CreditsInfo struct {
	HasCredits bool
	Unlimited  bool
	Balance    *float64
}

// ExtraLimit represents one extra rate limit bucket from meta["extra_limits"].
// This is an agent-agnostic key; each entry is rendered as a progress item.
type ExtraLimit struct {
	ID            string
	Label         string
	UsedPercent   float64
	WindowMinutes float64
	ResetsAt      string
}

// ParseCredits reads meta["credits"] safely. Returns nil when the key is absent.
func ParseCredits(meta map[string]any) *CreditsInfo {
	if meta == nil {
		return nil
	}
	raw, ok := meta["credits"]
	if !ok || raw == nil {
		return nil
	}
	obj, ok := raw.(map[string]any)
	if !ok {
		return nil
	}

	hasCredits, _ := obj["hasCredits"].(bool)
	unlimited, _ := obj["unlimited"].(bool)

	var balance *float64
	if v, ok := obj["balance"]; ok && v != nil {
		switch val := v.(type) {
		case float64:
			balance = &val
		case float32:
			f := float64(val)
			balance = &f
		case int:
			f := float64(val)
			balance = &f
		case int64:
			f := float64(val)
			balance = &f
		}
	}
	return &CreditsInfo{
		HasCredits: hasCredits,
		Unlimited:  unlimited,
		Balance:    balance,
	}
}

// ShouldShow returns true unless hasCredits is false AND balance is nil.
func (c *CreditsInfo) ShouldShow() bool {
	return !(!c.HasCredits && c.Balance == nil)
}

// ParseExtraLimits reads meta["extra_limits"] safely. Returns nil when the key is absent.
func ParseExtraLimits(meta map[string]any) []ExtraLimit {
	if meta == nil {
		return nil
	}
	raw, ok := meta["extra_limits"]
	if !ok || raw == nil {
		return nil
	}
	arr, ok := raw.([]any)
	if !ok {
		return nil
	}

	result := make([]ExtraLimit, 0, len(arr))
	for _, item := range arr {
		obj, ok := item.(map[string]any)
		if !ok {
			continue
		}
		id, _ := obj["id"].(string)
		label, _ := obj["label"].(string)
		usedPercent := MetaFloat64(obj, "usedPercent")
		windowMinutes := MetaFloat64(obj, "windowMinutes")
		resetsAt, _ := obj["resetsAt"].(string)

		result = append(result, ExtraLimit{
			ID:            id,
			Label:         label,
			UsedPercent:   usedPercent,
			WindowMinutes: windowMinutes,
			ResetsAt:      resetsAt,
		})
	}
	return result
}

// MetaFloat64 safely reads a float64 from a map, handling float64/float32/int/int64 types.
func MetaFloat64(meta map[string]any, key string) float64 {
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

// FormatWindowLabel converts windowMinutes to a short label like "5H", "7D".
// Returns empty string for unrecognized or zero durations.
func FormatWindowLabel(windowMinutes float64) string {
	if windowMinutes <= 0 {
		return ""
	}
	if windowMinutes == 300 {
		return "5H"
	}
	if windowMinutes == 10080 {
		return "7D"
	}
	// Generic: try days first
	days := int(math.Round(windowMinutes / 1440))
	if days > 0 && math.Abs(windowMinutes-float64(days*1440)) < 60 {
		return fmt.Sprintf("%dD", days)
	}
	h := int(math.Round(windowMinutes / 60))
	return fmt.Sprintf("%dH", h)
}
