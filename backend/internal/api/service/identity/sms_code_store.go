package identity

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"time"

	"github.com/askie/grix/backend/internal/store"
)

const (
	smsCodePrefix         = "auth:sms_code:"
	smsCodeFailPrefix     = "auth:sms_code_fail:"
	smsCooldownPrefix     = "auth:sms_cooldown:"
	smsIPCountPrefix      = "auth:sms_ip_count:"
	smsDayCountPrefix     = "auth:sms_day_count:"
	smsForceCaptchaPrefix = "auth:sms_force_captcha:"

	SmsCodeTTL        = 5 * time.Minute
	SmsCooldownTTL    = 60 * time.Second
	SmsIPWindowTTL    = 5 * time.Minute
	SmsDayWindowTTL   = 24 * time.Hour
	SmsCaptchaHintTTL = 5 * time.Minute
	SmsMaxFailures    = 5
	SmsIPMax5Min      = 5
	SmsDayMax         = 10
)

// ErrSmsRedisUnavailable 表示 Redis 不可用 → 拒发；
// 不复制邮件验证码 store 的"跳过存储"老坑（那是个 bug）。
var ErrSmsRedisUnavailable = errors.New("短信服务暂不可用，请稍后再试")
var ErrSmsCooldown = errors.New("发送过于频繁，请稍后再试")
var ErrSmsIPLimit = errors.New("IP 请求过于频繁，请稍后再试")
var ErrSmsDayLimit = errors.New("当日发送次数已达上限")
var ErrSmsCaptchaRequired = errors.New("请先完成图形验证码")

// SmsCodeStore 封装短信码相关 Redis 操作。
type SmsCodeStore struct{}

func (SmsCodeStore) Generate6Digit() (string, error) {
	n, err := rand.Int(rand.Reader, big.NewInt(1000000))
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%06d", n.Int64()), nil
}

// ReserveCooldown 一次性预占冷却 + IP + 日额度三道限流。
// 全部成功才允许真正调 provider 发送。任一道失败立刻返错。
func (s SmsCodeStore) ReserveCooldown(ctx context.Context, phone, clientIP string) error {
	if store.RDB == nil {
		return ErrSmsRedisUnavailable
	}
	// 1. 60s/手机号 冷却（SETNX）
	cooldownKey := smsCooldownPrefix + phone
	ok, err := store.RDB.SetNX(ctx, cooldownKey, "1", SmsCooldownTTL).Result()
	if err != nil {
		return ErrSmsRedisUnavailable
	}
	if !ok {
		return ErrSmsCooldown
	}
	// 2. 24h/手机号 10 条
	dayKey := smsDayCountPrefix + phone
	dayCount, err := store.RDB.Incr(ctx, dayKey).Result()
	if err != nil {
		_ = store.RDB.Del(ctx, cooldownKey).Err() // 回滚冷却
		return ErrSmsRedisUnavailable
	}
	if dayCount == 1 {
		_ = store.RDB.Expire(ctx, dayKey, SmsDayWindowTTL).Err()
	}
	if dayCount > SmsDayMax {
		_ = store.RDB.Del(ctx, cooldownKey).Err()
		return ErrSmsDayLimit
	}
	// 3. 5min/IP 5 条
	if clientIP != "" {
		ipKey := smsIPCountPrefix + clientIP
		ipCount, err := store.RDB.Incr(ctx, ipKey).Result()
		if err != nil {
			_ = store.RDB.Del(ctx, cooldownKey).Err()
			return ErrSmsRedisUnavailable
		}
		if ipCount == 1 {
			_ = store.RDB.Expire(ctx, ipKey, SmsIPWindowTTL).Err()
		}
		if ipCount > SmsIPMax5Min {
			_ = store.RDB.Del(ctx, cooldownKey).Err()
			return ErrSmsIPLimit
		}
	}
	return nil
}

// IsCaptchaRequired 第二次起强 captcha；标记键 5min 内有效。
func (s SmsCodeStore) IsCaptchaRequired(ctx context.Context, phone string) bool {
	if store.RDB == nil {
		return false
	}
	v, err := store.RDB.Get(ctx, smsForceCaptchaPrefix+phone).Result()
	return err == nil && v != ""
}

// MarkCaptchaRequired 发码成功后标记，下一次再发必须带 captcha。
func (s SmsCodeStore) MarkCaptchaRequired(ctx context.Context, phone string) {
	if store.RDB == nil {
		return
	}
	_ = store.RDB.Set(ctx, smsForceCaptchaPrefix+phone, "1", SmsCaptchaHintTTL).Err()
}

// StoreCode 把生成的 6 位码落 Redis；同 (scene, phone) 旧码自动覆盖（失效）。
func (s SmsCodeStore) StoreCode(ctx context.Context, scene, phone, code string) error {
	if store.RDB == nil {
		return ErrSmsRedisUnavailable
	}
	key := smsCodePrefix + scene + ":" + phone
	if err := store.RDB.Set(ctx, key, code, SmsCodeTTL).Err(); err != nil {
		return ErrSmsRedisUnavailable
	}
	// 重置失败计数
	_ = store.RDB.Del(ctx, smsCodeFailPrefix+scene+":"+phone).Err()
	return nil
}

// VerifyCode 校验码 + 失败封顶：5 次错码即作废当前码。
func (s SmsCodeStore) VerifyCode(ctx context.Context, scene, phone, input string) bool {
	if store.RDB == nil {
		return false
	}
	key := smsCodePrefix + scene + ":" + phone
	stored, err := store.RDB.Get(ctx, key).Result()
	if err != nil {
		return false
	}
	failKey := smsCodeFailPrefix + scene + ":" + phone
	if subtle.ConstantTimeCompare([]byte(stored), []byte(strings.TrimSpace(input))) == 1 {
		_ = store.RDB.Del(ctx, key, failKey).Err()
		return true
	}
	if attempts, aerr := store.RDB.Incr(ctx, failKey).Result(); aerr == nil {
		if attempts == 1 {
			_ = store.RDB.Expire(ctx, failKey, SmsCodeTTL).Err()
		}
		if attempts >= SmsMaxFailures {
			_ = store.RDB.Del(ctx, key, failKey).Err()
		}
	}
	return false
}

// RollbackCooldown 当 provider 发送失败时回滚 60s 冷却，
// 允许用户立刻重试（不退回日额度计数 — 防"失败重试"绕过日限）。
func (s SmsCodeStore) RollbackCooldown(ctx context.Context, phone string) {
	if store.RDB == nil {
		return
	}
	_ = store.RDB.Del(ctx, smsCooldownPrefix+phone).Err()
}
