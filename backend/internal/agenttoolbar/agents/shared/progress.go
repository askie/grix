package shared

import (
	"math"
	"strconv"
)

// PercentCenterText 将进度百分比格式化为环形进度中心显示的整数文本。
// 取四舍五入后的整数，并限定在 1-99 的范围内，保证始终是两位以内的非空数字。
func PercentCenterText(percent float64) string {
	value := int(math.Round(percent))
	if value < 1 {
		value = 1
	}
	if value > 99 {
		value = 99
	}
	return strconv.Itoa(value)
}
