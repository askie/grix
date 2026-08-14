package shared

import (
	"fmt"
	"time"
)

func FormatDuration(d time.Duration) string {
	if d <= 0 {
		return "0分钟"
	}
	hours := int(d.Hours())
	minutes := int(d.Minutes()) % 60
	switch {
	case hours > 0 && minutes > 0:
		return fmt.Sprintf("%d小时%d分钟", hours, minutes)
	case hours > 0:
		return fmt.Sprintf("%d小时", hours)
	default:
		return fmt.Sprintf("%d分钟", minutes)
	}
}
