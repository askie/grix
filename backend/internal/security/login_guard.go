package security

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/askie/grix/backend/internal/store"
	"github.com/redis/go-redis/v9"
)

const (
	MaxLoginAttempts     = 5
	LockoutDuration      = time.Hour
	RepeatedLockDuration = 12 * time.Hour
	FailedAttemptsWindow = time.Hour
)

// 锁定时长策略。锁定时长按 baseLockTTL * lockFactor^(lockCount-1) 指数增长，
// 封顶 maxLockTTL；lockCountTTL>0 时，连续锁定计数在该时长内无新锁定即衰减归零。
type lockSchedule struct {
	baseLockTTL  time.Duration
	lockFactor   float64
	maxLockTTL   time.Duration
	lockCountTTL time.Duration
}

// 普通用户：与历史行为等价（首次锁 1 小时，再犯锁 12 小时，计数不过期）。
var userLockSchedule = lockSchedule{
	baseLockTTL:  LockoutDuration,
	lockFactor:   12,
	maxLockTTL:   RepeatedLockDuration,
	lockCountTTL: 0,
}

// 管理员：指数退避，1h→2h→4h→8h→16h→封顶 24h，连续锁定计数 24 小时内衰减。
var adminLockSchedule = lockSchedule{
	baseLockTTL:  time.Hour,
	lockFactor:   2,
	maxLockTTL:   24 * time.Hour,
	lockCountTTL: 24 * time.Hour,
}

var (
	ErrAccountLocked         = errors.New("账号已被锁定，请稍后再试")
	recordLoginFailureScript = redis.NewScript(`
local failedKey = KEYS[1]
local lockedKey = KEYS[2]
local lockCountKey = KEYS[3]
local maxAttempts = tonumber(ARGV[1])
local failedTTL = tonumber(ARGV[2])
local baseLockTTL = tonumber(ARGV[3])
local lockFactor = tonumber(ARGV[4])
local maxLockTTL = tonumber(ARGV[5])
local lockCountTTL = tonumber(ARGV[6])

local count = redis.call("INCR", failedKey)
redis.call("PEXPIRE", failedKey, failedTTL)

if count >= maxAttempts then
  local lockCount = redis.call("INCR", lockCountKey)
  if lockCountTTL > 0 then
    redis.call("PEXPIRE", lockCountKey, lockCountTTL)
  end
  local lockTTL = baseLockTTL * (lockFactor ^ (lockCount - 1))
  if lockTTL > maxLockTTL then
    lockTTL = maxLockTTL
  end
  lockTTL = math.floor(lockTTL)
  redis.call("SET", lockedKey, "1", "PX", lockTTL)
  redis.call("DEL", failedKey)
  return {count, 1, lockTTL}
end

return {count, 0, 0}
`)
)

const (
	loginFailedKeyPrefix = "auth:login_failed:"
	loginLockedKeyPrefix = "auth:login_locked:"
	loginLockCountPrefix = "auth:login_lock_count:"
)

type LoginGuard struct {
	scope    string
	subject  string
	schedule lockSchedule
}

func NewUserLoginGuardByUserID(userID int64) *LoginGuard {
	return newLoginGuard("user:id", strconv.FormatInt(userID, 10), userLockSchedule)
}

func NewUserLoginGuardByAccount(account string) *LoginGuard {
	return newLoginGuard("user:account", hashLoginGuardSubject(account), userLockSchedule)
}

func NewAdminLoginGuardByAdminID(adminID int64) *LoginGuard {
	return newLoginGuard("admin:id", strconv.FormatInt(adminID, 10), adminLockSchedule)
}

func NewAdminLoginGuardByUsername(username string) *LoginGuard {
	return newLoginGuard("admin:username", hashLoginGuardSubject(username), adminLockSchedule)
}

func newLoginGuard(scope, subject string, schedule lockSchedule) *LoginGuard {
	return &LoginGuard{
		scope:    strings.TrimSpace(scope),
		subject:  strings.TrimSpace(subject),
		schedule: schedule,
	}
}

func hashLoginGuardSubject(raw string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(raw)))
	return hex.EncodeToString(sum[:])
}

func (g *LoginGuard) failedKey() string {
	return loginFailedKeyPrefix + g.scope + ":" + g.subject
}

func (g *LoginGuard) lockedKey() string {
	return loginLockedKeyPrefix + g.scope + ":" + g.subject
}

func (g *LoginGuard) lockCountKey() string {
	return loginLockCountPrefix + g.scope + ":" + g.subject
}

func (g *LoginGuard) IsLocked(ctx context.Context) (bool, time.Duration, error) {
	if store.RDB == nil {
		return false, 0, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}

	ttl, err := store.RDB.TTL(ctx, g.lockedKey()).Result()
	if err != nil {
		return false, 0, fmt.Errorf("检查锁定状态失败: %w", err)
	}

	if ttl <= 0 {
		return false, 0, nil
	}

	return true, ttl, nil
}

func (g *LoginGuard) RecordFailure(ctx context.Context) (int, bool, time.Duration, error) {
	if store.RDB == nil {
		return 0, false, 0, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}

	result, err := recordLoginFailureScript.Run(
		ctx,
		store.RDB,
		[]string{g.failedKey(), g.lockedKey(), g.lockCountKey()},
		MaxLoginAttempts,
		FailedAttemptsWindow.Milliseconds(),
		g.schedule.baseLockTTL.Milliseconds(),
		g.schedule.lockFactor,
		g.schedule.maxLockTTL.Milliseconds(),
		g.schedule.lockCountTTL.Milliseconds(),
	).Result()
	if err != nil {
		return 0, false, 0, fmt.Errorf("记录登录失败失败: %w", err)
	}

	values, ok := result.([]interface{})
	if !ok || len(values) != 3 {
		return 0, false, 0, fmt.Errorf("记录登录失败返回值异常: %T", result)
	}

	count, ok := loginGuardInt64(values[0])
	if !ok {
		return 0, false, 0, fmt.Errorf("记录登录失败计数异常: %T", values[0])
	}

	locked, ok := loginGuardInt64(values[1])
	if !ok {
		return 0, false, 0, fmt.Errorf("记录登录失败锁定标记异常: %T", values[1])
	}

	lockDurationMs, ok := loginGuardInt64(values[2])
	if !ok {
		return 0, false, 0, fmt.Errorf("记录登录失败锁定时长异常: %T", values[2])
	}

	return int(count), locked == 1, time.Duration(lockDurationMs) * time.Millisecond, nil
}

func (g *LoginGuard) RecordSuccess(ctx context.Context) error {
	if store.RDB == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}

	return store.RDB.Del(ctx, g.failedKey()).Err()
}

func (g *LoginGuard) GetRemainingAttempts(ctx context.Context) (int, error) {
	if store.RDB == nil {
		return MaxLoginAttempts, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}

	locked, _, err := g.IsLocked(ctx)
	if err != nil {
		return 0, err
	}
	if locked {
		return 0, nil
	}

	count, err := store.RDB.Get(ctx, g.failedKey()).Int()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return MaxLoginAttempts, nil
		}
		return 0, fmt.Errorf("获取剩余尝试次数失败: %w", err)
	}

	remaining := MaxLoginAttempts - count
	if remaining < 0 {
		remaining = 0
	}
	return remaining, nil
}

func (g *LoginGuard) ClearLock(ctx context.Context) error {
	if store.RDB == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}

	return store.RDB.Del(ctx, g.failedKey(), g.lockedKey()).Err()
}

func FormatLockTime(d time.Duration) string {
	if d <= 0 {
		return ""
	}

	if d < time.Minute {
		return "不到1分钟"
	}

	minutes := int(math.Ceil(d.Minutes()))
	if minutes < 60 {
		return fmt.Sprintf("%d分钟", minutes)
	}

	hours := minutes / 60
	remainingMinutes := minutes % 60
	if remainingMinutes == 0 {
		return fmt.Sprintf("%d小时", hours)
	}
	return fmt.Sprintf("%d小时%d分钟", hours, remainingMinutes)
}

func LoginLockedMessage(d time.Duration) string {
	if d <= 0 {
		d = LockoutDuration
	}
	return fmt.Sprintf("账号已被锁定，请在%s后再试", FormatLockTime(d))
}

// LoginLockedError 表示账号因登录失败次数过多被临时锁定。
// handler 层据此类型返回 429，与"凭证错误"的 401 区分开。
type LoginLockedError struct {
	Remaining time.Duration
}

func (e *LoginLockedError) Error() string {
	return LoginLockedMessage(e.Remaining)
}

func loginGuardInt64(v interface{}) (int64, bool) {
	switch n := v.(type) {
	case int64:
		return n, true
	case int:
		return int64(n), true
	case string:
		parsed, err := strconv.ParseInt(n, 10, 64)
		if err != nil {
			return 0, false
		}
		return parsed, true
	default:
		return 0, false
	}
}
