package pricing

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestBeijingMinuteOfDay(t *testing.T) {
	// 2026-07-01 00:00:00 UTC = 北京 08:00 → 480 分钟
	utc0800 := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	assert.Equal(t, 8*60, beijingMinuteOfDay(utc0800))

	// 北京 00:30 = 前一天 16:30 UTC → 30 分钟
	utc1630 := time.Date(2026, 6, 30, 16, 30, 0, 0, time.UTC)
	assert.Equal(t, 30, beijingMinuteOfDay(utc1630))

	// 北京 08:30 = 00:30 UTC → 510 分钟
	utc0030 := time.Date(2026, 7, 1, 0, 30, 0, 0, time.UTC)
	assert.Equal(t, 8*60+30, beijingMinuteOfDay(utc0030))

	// 传入带非UTC时区的时间也应换算正确（内部先 .UTC()）
	shanghai := time.FixedZone("CST", 8*3600)
	bj0030 := time.Date(2026, 7, 1, 0, 30, 0, 0, shanghai)
	assert.Equal(t, 30, beijingMinuteOfDay(bj0030))
}

func TestWindowContains_Normal(t *testing.T) {
	// 错峰 北京00:30-08:30 → [30,510)
	start, end := 30, 510
	assert.True(t, windowContains(start, end, 30))   // 左闭：命中
	assert.True(t, windowContains(start, end, 300))  // 区间内
	assert.False(t, windowContains(start, end, 510)) // 右开：不命中
	assert.False(t, windowContains(start, end, 29))
	assert.False(t, windowContains(start, end, 600))
}

func TestWindowContains_Wrap(t *testing.T) {
	// 跨零点 北京22:00-02:00 → [1320,1440) ∪ [0,120)
	start, end := 1320, 120
	assert.True(t, windowContains(start, end, 1320)) // 22:00 命中
	assert.True(t, windowContains(start, end, 1439)) // 23:59 命中
	assert.True(t, windowContains(start, end, 0))    // 00:00 命中
	assert.True(t, windowContains(start, end, 119))  // 01:59 命中
	assert.False(t, windowContains(start, end, 120)) // 02:00 右开不命中
	assert.False(t, windowContains(start, end, 720)) // 白天不命中
}

func TestWindowsOverlap(t *testing.T) {
	// 08:00-11:00 vs 09:00-12:00 相交(09:00-11:00)
	assert.True(t, windowsOverlap(480, 660, 540, 720))
	// 08:00-11:00 vs 11:00-13:00 边界相接、左闭右开不算相交
	assert.False(t, windowsOverlap(480, 660, 660, 780))
	// 00:30-08:30 vs 09:00-12:00 完全不相交
	assert.False(t, windowsOverlap(30, 510, 540, 720))
	// 跨零点 22:00-02:00 vs 01:00-03:00 相交(01:00-02:00)
	assert.True(t, windowsOverlap(1320, 120, 60, 180))
	// 跨零点 22:00-02:00 vs 03:00-05:00 不相交
	assert.False(t, windowsOverlap(1320, 120, 180, 300))
	// 跨零点 22:00-02:00 vs 23:00-23:30 被包含 → 相交
	assert.True(t, windowsOverlap(1320, 120, 1380, 1410))
}
