package agentapi

import (
	"testing"
	"time"

	"github.com/askie/grix/backend/internal/pkg/logger"
	"github.com/stretchr/testify/assert"
)

// newTestAuthFailLimiter 返回带可控时钟的限流器。
func newTestAuthFailLimiter(window time.Duration, threshold int, blockFor time.Duration) (*authFailLimiter, *time.Time) {
	logger.Init()
	now := time.Date(2026, 7, 9, 12, 0, 0, 0, time.UTC)
	l := newAuthFailLimiter(window, threshold, blockFor)
	l.now = func() time.Time { return now }
	return l, &now
}

func TestAuthFailLimiterBlocksAfterThreshold(t *testing.T) {
	l, _ := newTestAuthFailLimiter(60*time.Second, 30, 10*time.Minute)

	for i := 0; i < 30; i++ {
		assert.False(t, l.RecordFailure(1001, "203.0.113.7"), "第 %d 次失败不应触发封锁", i+1)
		assert.False(t, l.Blocked(1001, "203.0.113.7"))
	}
	// 第 31 次（超过 30）触发封锁
	assert.True(t, l.RecordFailure(1001, "203.0.113.7"))
	assert.True(t, l.Blocked(1001, "203.0.113.7"))

	// 其他 agent / 其他 IP 互不影响
	assert.False(t, l.Blocked(1002, "203.0.113.7"))
	assert.False(t, l.Blocked(1001, "203.0.113.8"))
}

func TestAuthFailLimiterSlidingWindowExpiry(t *testing.T) {
	l, now := newTestAuthFailLimiter(60*time.Second, 30, 10*time.Minute)

	// 30 次失败落在窗口内
	for i := 0; i < 30; i++ {
		l.RecordFailure(2001, "198.51.100.1")
	}
	// 时间推进 61 秒，旧失败全部滑出窗口，再来一次不应触发封锁
	*now = now.Add(61 * time.Second)
	assert.False(t, l.RecordFailure(2001, "198.51.100.1"))
	assert.False(t, l.Blocked(2001, "198.51.100.1"))
}

func TestAuthFailLimiterBlockExpiresAndRecounts(t *testing.T) {
	l, now := newTestAuthFailLimiter(60*time.Second, 30, 10*time.Minute)

	for i := 0; i < 31; i++ {
		l.RecordFailure(3001, "192.0.2.9")
	}
	assert.True(t, l.Blocked(3001, "192.0.2.9"))

	// 封锁期内继续失败不重复延长（RecordFailure 短路）
	assert.False(t, l.RecordFailure(3001, "192.0.2.9"))

	// 10 分钟后自动解封，重新从零计数
	*now = now.Add(10*time.Minute + time.Second)
	assert.False(t, l.Blocked(3001, "192.0.2.9"))
	assert.False(t, l.RecordFailure(3001, "192.0.2.9"))
	assert.False(t, l.Blocked(3001, "192.0.2.9"))
}

func TestAuthFailLimiterIgnoresInvalidKey(t *testing.T) {
	l, _ := newTestAuthFailLimiter(60*time.Second, 1, 10*time.Minute)

	// agentID<=0 或空 IP 不参与限流
	assert.False(t, l.RecordFailure(0, "1.2.3.4"))
	assert.False(t, l.RecordFailure(0, "1.2.3.4"))
	assert.False(t, l.Blocked(0, "1.2.3.4"))
	assert.False(t, l.RecordFailure(5001, ""))
	assert.False(t, l.RecordFailure(5001, ""))
	assert.False(t, l.Blocked(5001, ""))
}

func TestAuthFailLimiterSweepRemovesStaleEntries(t *testing.T) {
	l, now := newTestAuthFailLimiter(60*time.Second, 30, 10*time.Minute)

	l.RecordFailure(6001, "10.0.0.1")
	l.RecordFailure(6002, "10.0.0.2")
	assert.Len(t, l.entries, 2)

	// 推进超过清扫间隔 + 窗口，下一次记录触发清扫，过期条目被移除
	*now = now.Add(6 * time.Minute)
	l.RecordFailure(6003, "10.0.0.3")
	assert.Len(t, l.entries, 1)
}
