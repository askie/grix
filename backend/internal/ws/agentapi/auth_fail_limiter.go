package agentapi

import (
	"fmt"
	"sync"
	"time"

	"github.com/askie/grix/backend/internal/pkg/logger"
)

// 认证失败滑动窗口限流（进程内内存实现，多副本各自独立，不追求全局精确）。
// 针对"同一 agent_id + 来源 IP"的认证失败计数：窗口内超过阈值后进入封锁期，
// 封锁期内的连接在升级握手前/认证前直接拒绝，不再走完整认证流程。
// 场景：老版本 connector 对已删除 agent 无退避疯狂重连，TLS 握手流量烧爆 LB 流量费。

const (
	authFailWindow        = 60 * time.Second
	authFailThreshold     = 30 // 窗口内超过该次数（即第 31 次）进入封锁期
	authFailBlockDuration = 10 * time.Minute

	// authFailSweepInterval 过期条目全量清扫的最小间隔，防止 map 无限增长。
	authFailSweepInterval = 5 * time.Minute
	// authFailRejectLogSample 封锁期内拒绝日志的采样步长（首个与每 N 个打一条）。
	authFailRejectLogSample = 100
)

// agentAuthFailLimiter 全局限流器实例；var 便于测试替换。
var agentAuthFailLimiter = newAuthFailLimiter(authFailWindow, authFailThreshold, authFailBlockDuration)

type authFailEntry struct {
	failures     []time.Time
	blockedUntil time.Time
	rejected     int64 // 封锁期内被拒绝的连接数（日志采样用），解封时清零
}

type authFailLimiter struct {
	mu        sync.Mutex
	entries   map[string]*authFailEntry
	window    time.Duration
	threshold int
	blockFor  time.Duration
	lastSweep time.Time
	now       func() time.Time // 便于测试注入时钟
}

func newAuthFailLimiter(window time.Duration, threshold int, blockFor time.Duration) *authFailLimiter {
	return &authFailLimiter{
		entries:   make(map[string]*authFailEntry),
		window:    window,
		threshold: threshold,
		blockFor:  blockFor,
		now:       time.Now,
	}
}

func authFailKey(agentID int64, ip string) string {
	return fmt.Sprintf("%d|%s", agentID, ip)
}

// Blocked 返回该 (agentID, ip) 当前是否处于封锁期。封锁期内计数并按采样打日志。
func (l *authFailLimiter) Blocked(agentID int64, ip string) bool {
	if l == nil || agentID <= 0 || ip == "" {
		return false
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	e := l.entries[authFailKey(agentID, ip)]
	if e == nil || !e.blockedUntil.After(l.now()) {
		return false
	}
	e.rejected++
	if e.rejected == 1 || e.rejected%authFailRejectLogSample == 0 {
		logger.L.Warnf(
			"agent api auth-fail limiter reject: agent=%d ip=%s rejected=%d blocked_until=%s",
			agentID, ip, e.rejected, e.blockedUntil.Format(time.RFC3339),
		)
	}
	return true
}

// RecordFailure 记录一次认证失败；本次记录触发进入封锁期时返回 true。
func (l *authFailLimiter) RecordFailure(agentID int64, ip string) bool {
	if l == nil || agentID <= 0 || ip == "" {
		return false
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	l.sweepLocked(now)

	key := authFailKey(agentID, ip)
	e := l.entries[key]
	if e == nil {
		e = &authFailEntry{}
		l.entries[key] = e
	}
	if e.blockedUntil.After(now) {
		return false
	}
	// 滑动窗口：先剔除窗口外的旧失败，再计入本次。
	cutoff := now.Add(-l.window)
	kept := e.failures[:0]
	for _, ts := range e.failures {
		if ts.After(cutoff) {
			kept = append(kept, ts)
		}
	}
	e.failures = append(kept, now)
	if len(e.failures) <= l.threshold {
		return false
	}
	e.blockedUntil = now.Add(l.blockFor)
	e.failures = nil
	e.rejected = 0
	logger.L.Warnf(
		"agent api auth-fail limiter block: agent=%d ip=%s failures>%d within %s, blocking for %s",
		agentID, ip, l.threshold, l.window, l.blockFor,
	)
	return true
}

// sweepLocked 周期性清理既不在封锁期、窗口内也无失败记录的条目。调用方须持锁。
func (l *authFailLimiter) sweepLocked(now time.Time) {
	if now.Sub(l.lastSweep) < authFailSweepInterval {
		return
	}
	l.lastSweep = now
	cutoff := now.Add(-l.window)
	for key, e := range l.entries {
		if e.blockedUntil.After(now) {
			continue
		}
		stale := true
		for _, ts := range e.failures {
			if ts.After(cutoff) {
				stale = false
				break
			}
		}
		if stale {
			delete(l.entries, key)
		}
	}
}
