package service

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/askie/grix/backend/internal/store"
	"github.com/google/uuid"
)

const (
	emailCodeSendCooldownTTL = 5 * time.Minute
	emailCodeSendLockTTL     = time.Minute

	emailCodeSendIPKeyPrefix    = "auth:email_code:send:ip:"
	emailCodeSendEmailKeyPrefix = "auth:email_code:send:email:"
)

var ErrEmailCodeSendTooFrequent = errors.New("验证码发送过于频繁，请5分钟后再试")

const acquireEmailCodeSendCooldownScript = `
if redis.call("EXISTS", KEYS[1]) == 1 or redis.call("EXISTS", KEYS[2]) == 1 then
	return 0
end
redis.call("SET", KEYS[1], ARGV[1], "EX", ARGV[2])
redis.call("SET", KEYS[2], ARGV[1], "EX", ARGV[2])
return 1
`

const commitEmailCodeSendCooldownScript = `
if redis.call("GET", KEYS[1]) ~= ARGV[1] or redis.call("GET", KEYS[2]) ~= ARGV[1] then
	return 0
end
redis.call("SET", KEYS[1], "cooldown", "EX", ARGV[2])
redis.call("SET", KEYS[2], "cooldown", "EX", ARGV[2])
return 1
`

const rollbackEmailCodeSendCooldownScript = `
if redis.call("GET", KEYS[1]) == ARGV[1] then
	redis.call("DEL", KEYS[1])
end
if redis.call("GET", KEYS[2]) == ARGV[1] then
	redis.call("DEL", KEYS[2])
end
return 1
`

type emailCodeSendReservation struct {
	ipKey    string
	emailKey string
	token    string
}

func reserveEmailCodeSendCooldown(clientIP, email string) (*emailCodeSendReservation, error) {
	if store.RDB == nil {
		return nil, errors.New("验证码服务暂时不可用，请稍后再试")
	}

	ipKey, emailKey, err := emailCodeSendCooldownKeys(clientIP, email)
	if err != nil {
		return nil, err
	}

	reservation := &emailCodeSendReservation{
		ipKey:    ipKey,
		emailKey: emailKey,
		token:    uuid.NewString(),
	}

	result, err := store.RDB.Eval(
		emailCodeSendContext(),
		acquireEmailCodeSendCooldownScript,
		[]string{reservation.ipKey, reservation.emailKey},
		reservation.token,
		int(emailCodeSendLockTTL/time.Second),
	).Int()
	if err != nil {
		return nil, errors.New("验证码服务暂时不可用，请稍后再试")
	}
	if result == 0 {
		return nil, ErrEmailCodeSendTooFrequent
	}
	return reservation, nil
}

func (r *emailCodeSendReservation) Commit() error {
	if r == nil {
		return nil
	}
	result, err := store.RDB.Eval(
		emailCodeSendContext(),
		commitEmailCodeSendCooldownScript,
		[]string{r.ipKey, r.emailKey},
		r.token,
		int(emailCodeSendCooldownTTL/time.Second),
	).Int()
	if err != nil {
		return err
	}
	if result == 0 {
		return ErrEmailCodeSendTooFrequent
	}
	return nil
}

func (r *emailCodeSendReservation) Rollback() error {
	if r == nil || store.RDB == nil {
		return nil
	}
	return store.RDB.Eval(
		emailCodeSendContext(),
		rollbackEmailCodeSendCooldownScript,
		[]string{r.ipKey, r.emailKey},
		r.token,
	).Err()
}

func emailCodeSendCooldownKeys(clientIP, email string) (string, string, error) {
	normalizedIP := strings.TrimSpace(clientIP)
	if normalizedIP == "" {
		return "", "", errors.New("参数错误")
	}

	normalizedEmail := strings.ToLower(strings.TrimSpace(email))
	if normalizedEmail == "" {
		return "", "", errors.New("参数错误")
	}

	return emailCodeSendIPKeyPrefix + normalizedIP, emailCodeSendEmailKeyPrefix + normalizedEmail, nil
}

func emailCodeSendContext() context.Context {
	return context.Background()
}
