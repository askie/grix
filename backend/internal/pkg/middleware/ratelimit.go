package middleware

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"fmt"
	"net/http"
	"strconv"
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

// RateLimitSyncBootstrapDuplicates rejects an identical sync-v2 session
// bootstrap repeated by the same login session inside window. The generic
// sessions-read token bucket protects request rate; this guard protects
// response bytes from a reconnect loop repeatedly downloading the same large
// snapshot. The access-token session ID is the server-authenticated device
// session identity. Legacy tokens without one safely fall back to user scope.
func RateLimitSyncBootstrapDuplicates(window time.Duration) gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Query("sync_head") != "1" || window <= 0 {
			c.Next()
			return
		}

		userID := GetUserID(c)
		deviceSessionID := "legacy"
		if claims := GetClaims(c); claims != nil {
			if sid := strings.TrimSpace(claims.SessionID); sid != "" {
				deviceSessionID = sid
			}
		}
		requestFingerprint := sha256.Sum256([]byte(c.Request.URL.Query().Encode()))
		identityFingerprint := sha256.Sum256([]byte(deviceSessionID))
		key := fmt.Sprintf(
			"rate:%d:sessions-bootstrap:%x:%x",
			userID,
			identityFingerprint[:8],
			requestFingerprint[:16],
		)

		reserved, err := store.RDB.SetNX(c.Request.Context(), key, "1", window).Result()
		if err != nil {
			logger.L.Warnf("sync bootstrap duplicate limit error user=%d: %v", userID, err)
			c.Next()
			return
		}
		if reserved {
			c.Next()
			return
		}

		retryAfter := int(window.Seconds())
		if ttl, ttlErr := store.RDB.PTTL(c.Request.Context(), key).Result(); ttlErr == nil && ttl > 0 {
			retryAfter = int((ttl + time.Second - 1) / time.Second)
		}
		if retryAfter < 1 {
			retryAfter = 1
		}
		c.Header("Retry-After", strconv.Itoa(retryAfter))
		response.Fail(c, http.StatusTooManyRequests, 10005, "请求过于频繁，请稍后再试")
		c.Abort()
	}
}
