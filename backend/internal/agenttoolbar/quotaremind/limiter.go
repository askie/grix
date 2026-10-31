package quotaremind

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

// Limiter 定义限额提醒的跨实例原子防重与节流接口。
type Limiter interface {
	// TryAcquire 尝试为指定告警目标获取发送名额。
	// 若本周期对应 tier（或更高优先级的 tier）已发送过，或当前仍处于节流冷却期内，返回 acquired=false。
	// 若成功获取发送资格，返回 acquired=true 及 release 回滚回调（若后续发送发生网络等临时错误，可调用 release 回退状态）。
	TryAcquire(
		ctx context.Context,
		ownerID int64,
		sessionID string,
		agentID int64,
		windowType, cycleID, tier string,
		throttleTTL time.Duration,
	) (acquired bool, release func(), err error)
}

const (
	defaultCycleTTL    = 7 * 24 * time.Hour
	defaultThrottleTTL = 2 * time.Minute
)

var acquireLuaScript = redis.NewScript(`
local throttleKey = KEYS[1]
local cycleKeyTier = KEYS[2]
local cycleKeyHigher = KEYS[3]
local throttleMs = tonumber(ARGV[1])
local cycleMs = tonumber(ARGV[2])

if redis.call('EXISTS', throttleKey) == 1 then
    return 0
end
if redis.call('EXISTS', cycleKeyTier) == 1 then
    return 0
end
if cycleKeyHigher ~= "" and redis.call('EXISTS', cycleKeyHigher) == 1 then
    return 0
end

redis.call('SET', throttleKey, '1', 'PX', throttleMs)
redis.call('SET', cycleKeyTier, '1', 'PX', cycleMs)
return 1
`)

// RedisLimiter 基于 Redis Lua 脚本提供集群多副本原子去重与节流能力。
type RedisLimiter struct {
	rdb         redis.Cmdable
	throttleTTL time.Duration
	fallback    *MemoryLimiter
}

// NewRedisLimiter 创建基于 Redis 的去重节流器。若 rdb 为 nil，自动回退到内存实现。
func NewRedisLimiter(rdb redis.Cmdable, throttleTTL time.Duration) *RedisLimiter {
	if throttleTTL <= 0 {
		throttleTTL = defaultThrottleTTL
	}
	return &RedisLimiter{
		rdb:         rdb,
		throttleTTL: throttleTTL,
		fallback:    NewMemoryLimiter(throttleTTL),
	}
}

// TryAcquire 原子尝试获取发送资格。
func (r *RedisLimiter) TryAcquire(
	ctx context.Context,
	ownerID int64,
	sessionID string,
	agentID int64,
	windowType, cycleID, tier string,
	throttleTTL time.Duration,
) (bool, func(), error) {
	if r.rdb == nil {
		return r.fallback.TryAcquire(ctx, ownerID, sessionID, agentID, windowType, cycleID, tier, throttleTTL)
	}

	if throttleTTL <= 0 {
		throttleTTL = r.throttleTTL
	}
	cycleTTL := parseCycleTTL(cycleID)

	throttleKey := fmt.Sprintf("grix:toolbar:quota:throttle:%d:%s:%d", ownerID, strings.TrimSpace(sessionID), agentID)
	cycleKeyTier := fmt.Sprintf("grix:toolbar:quota:cycle:%d:%s:%d:%s:%s:%s", ownerID, strings.TrimSpace(sessionID), agentID, windowType, strings.TrimSpace(cycleID), tier)
	cycleKeyHigher := ""
	if tier == Tier10 {
		cycleKeyHigher = fmt.Sprintf("grix:toolbar:quota:cycle:%d:%s:%d:%s:%s:%s", ownerID, strings.TrimSpace(sessionID), agentID, windowType, strings.TrimSpace(cycleID), Tier5)
	}

	throttleMs := throttleTTL.Milliseconds()
	cycleMs := cycleTTL.Milliseconds()
	if throttleMs <= 0 {
		throttleMs = 60 * 1000
	}
	if cycleMs <= 0 {
		cycleMs = defaultCycleTTL.Milliseconds()
	}

	res, err := acquireLuaScript.Run(ctx, r.rdb, []string{throttleKey, cycleKeyTier, cycleKeyHigher}, throttleMs, cycleMs).Int64()
	if err != nil {
		// Redis 异常时回退到本地内存节流器，保证不阻断关键逻辑且不裸发
		return r.fallback.TryAcquire(ctx, ownerID, sessionID, agentID, windowType, cycleID, tier, throttleTTL)
	}

	if res != 1 {
		return false, nil, nil
	}

	release := func() {
		_ = r.rdb.Del(context.Background(), throttleKey, cycleKeyTier).Err()
	}

	return true, release, nil
}

// MemoryLimiter 单机内存去重与节流器（适用于单测或无 Redis 环境）。
type MemoryLimiter struct {
	mu          sync.Mutex
	throttleTTL time.Duration
	throttles   map[string]time.Time
	cycles      map[string]time.Time
}

// NewMemoryLimiter 创建基于内存的去重节流器。
func NewMemoryLimiter(throttleTTL time.Duration) *MemoryLimiter {
	if throttleTTL <= 0 {
		throttleTTL = defaultThrottleTTL
	}
	return &MemoryLimiter{
		throttleTTL: throttleTTL,
		throttles:   make(map[string]time.Time),
		cycles:      make(map[string]time.Time),
	}
}

// TryAcquire 在互斥锁保护下原子检查并登记发送状态。
func (m *MemoryLimiter) TryAcquire(
	_ context.Context,
	ownerID int64,
	sessionID string,
	agentID int64,
	windowType, cycleID, tier string,
	throttleTTL time.Duration,
) (bool, func(), error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := time.Now()
	throttleKey := fmt.Sprintf("%d:%s:%d", ownerID, strings.TrimSpace(sessionID), agentID)
	cycleKeyTier := fmt.Sprintf("%d:%s:%d:%s:%s:%s", ownerID, strings.TrimSpace(sessionID), agentID, windowType, strings.TrimSpace(cycleID), tier)
	var cycleKeyHigher string
	if tier == Tier10 {
		cycleKeyHigher = fmt.Sprintf("%d:%s:%d:%s:%s:%s", ownerID, strings.TrimSpace(sessionID), agentID, windowType, strings.TrimSpace(cycleID), Tier5)
	}

	if exp, ok := m.throttles[throttleKey]; ok && now.Before(exp) {
		return false, nil, nil
	}
	if exp, ok := m.cycles[cycleKeyTier]; ok && now.Before(exp) {
		return false, nil, nil
	}
	if cycleKeyHigher != "" {
		if exp, ok := m.cycles[cycleKeyHigher]; ok && now.Before(exp) {
			return false, nil, nil
		}
	}

	if throttleTTL <= 0 {
		throttleTTL = m.throttleTTL
	}
	cycleTTL := parseCycleTTL(cycleID)

	m.cleanupExpiredLocked(now)
	m.throttles[throttleKey] = now.Add(throttleTTL)
	m.cycles[cycleKeyTier] = now.Add(cycleTTL)

	release := func() {
		m.mu.Lock()
		defer m.mu.Unlock()
		delete(m.throttles, throttleKey)
		delete(m.cycles, cycleKeyTier)
	}

	return true, release, nil
}

func (m *MemoryLimiter) cleanupExpiredLocked(now time.Time) {
	for k, exp := range m.throttles {
		if now.After(exp) {
			delete(m.throttles, k)
		}
	}
	for k, exp := range m.cycles {
		if now.After(exp) {
			delete(m.cycles, k)
		}
	}
}

func parseCycleTTL(cycleID string) time.Duration {
	s := strings.TrimSpace(cycleID)
	if s == "" || s == "active" {
		return defaultCycleTTL
	}

	// 尝试解析 Unix 时间戳（秒或毫秒）
	if ts, err := strconv.ParseInt(s, 10, 64); err == nil && ts > 0 {
		var t time.Time
		if ts > 1e11 {
			t = time.UnixMilli(ts)
		} else {
			t = time.Unix(ts, 0)
		}
		if d := time.Until(t); d > 0 {
			return d + time.Hour
		}
	}

	// 尝试解析 ISO 8601 / RFC3339 时间格式
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		if d := time.Until(t); d > 0 {
			return d + time.Hour
		}
	}

	return defaultCycleTTL
}
