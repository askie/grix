package service

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/askie/grix/backend/internal/store"
	"github.com/mojocn/base64Captcha"
	"github.com/redis/go-redis/v9"
)

const captchaKeyPrefix = "auth:captcha:"

var fallbackCaptchaStore = base64Captcha.DefaultMemStore

func currentCaptchaStore() base64Captcha.Store {
	if store.RDB == nil {
		return fallbackCaptchaStore
	}
	return newRedisCaptchaStore(store.RDB)
}

type redisCaptchaStore struct {
	client *redis.Client
	ttl    time.Duration
}

func newRedisCaptchaStore(client *redis.Client) base64Captcha.Store {
	return &redisCaptchaStore{
		client: client,
		ttl:    base64Captcha.Expiration,
	}
}

func (s *redisCaptchaStore) Set(id string, value string) error {
	if strings.TrimSpace(id) == "" {
		return fmt.Errorf("captcha id must not be empty")
	}
	return s.client.Set(context.Background(), captchaRedisKey(id), value, s.ttl).Err()
}

func (s *redisCaptchaStore) Get(id string, clear bool) string {
	if strings.TrimSpace(id) == "" {
		return ""
	}
	ctx := context.Background()
	key := captchaRedisKey(id)
	var (
		value string
		err   error
	)
	if clear {
		value, err = s.client.GetDel(ctx, key).Result()
	} else {
		value, err = s.client.Get(ctx, key).Result()
	}
	if err != nil {
		return ""
	}
	return value
}

func (s *redisCaptchaStore) Verify(id, answer string, clear bool) bool {
	if strings.TrimSpace(id) == "" || strings.TrimSpace(answer) == "" {
		return false
	}
	value := s.Get(id, clear)
	if value == "" {
		return false
	}
	return strings.EqualFold(value, answer)
}

func captchaRedisKey(id string) string {
	return captchaKeyPrefix + strings.TrimSpace(id)
}
