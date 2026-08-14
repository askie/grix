package middleware

import (
	"context"
	_ "embed"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/askie/grix/backend/internal/pkg/logger"
	"github.com/askie/grix/backend/internal/pkg/response"
	"github.com/askie/grix/backend/internal/store"
	"github.com/gin-gonic/gin"
)

//go:embed token_bucket.lua
var tokenBucketScript string

var tokenBucketSHA string

func InitRateLimitScript() {
	sha, err := store.RDB.ScriptLoad(context.Background(), tokenBucketScript).Result()
	if err != nil {
		panic(fmt.Sprintf("failed to load rate limit lua script: %v", err))
	}
	tokenBucketSHA = sha
}

func evalRateLimit(ctx context.Context, key string, capacity, ratePerSec, now float64) (int, error) {
	result, err := store.RDB.EvalSha(ctx, tokenBucketSHA,
		[]string{key},
		capacity, ratePerSec, now, 1,
	).Int()
	if err != nil && strings.Contains(err.Error(), "NOSCRIPT") {
		sha, loadErr := store.RDB.ScriptLoad(ctx, tokenBucketScript).Result()
		if loadErr != nil {
			return 1, loadErr
		}
		tokenBucketSHA = sha
		result, err = store.RDB.EvalSha(ctx, sha,
			[]string{key},
			capacity, ratePerSec, now, 1,
		).Int()
	}
	return result, err
}

func RateLimit(tag string, capacity, ratePerSec float64, keyFn func(*gin.Context) string) gin.HandlerFunc {
	return func(c *gin.Context) {
		key := fmt.Sprintf("rate:%s:%s", keyFn(c), tag)
		now := float64(time.Now().UnixMilli()) / 1000.0
		result, err := evalRateLimit(context.Background(), key, capacity, ratePerSec, now)
		if err != nil {
			logger.L.Warnf("rate limit eval error: %v", err)
			c.Next()
			return
		}
		if result == 0 {
			response.Fail(c, http.StatusTooManyRequests, 10005, "请求过于频繁，请稍后再试")
			c.Abort()
			return
		}
		c.Next()
	}
}

func RateLimitByIP(tag string, capacity, ratePerSec float64) gin.HandlerFunc {
	return RateLimit(tag, capacity, ratePerSec, func(c *gin.Context) string {
		return c.ClientIP()
	})
}

func RateLimitByUser(tag string, capacity, ratePerSec float64) gin.HandlerFunc {
	return RateLimit(tag, capacity, ratePerSec, func(c *gin.Context) string {
		return fmt.Sprintf("%d", GetUserID(c))
	})
}

func RateLimitByOwner(tag string, capacity, ratePerSec float64) gin.HandlerFunc {
	return RateLimit(tag, capacity, ratePerSec, func(c *gin.Context) string {
		return fmt.Sprintf("%d", GetOwnerID(c))
	})
}

func RateLimitByAgent(tag string, capacity, ratePerSec float64) gin.HandlerFunc {
	return RateLimit(tag, capacity, ratePerSec, func(c *gin.Context) string {
		return fmt.Sprintf("%d", GetAgentID(c))
	})
}
