package push

import (
	"context"
	"fmt"
	"time"

	"github.com/askie/grix/backend/internal/store"
)

// ShouldThrottle checks if a push notification should be throttled.
// Returns true if the push should be skipped (within throttle window).
func ShouldThrottle(ctx context.Context, userID int64, sessionID string, isAISession bool) bool {
	key := fmt.Sprintf("push:throttle:%d:%s", userID, sessionID)
	ttl := 10 * time.Second
	if isAISession {
		ttl = 30 * time.Second
	}

	// SET NX - only succeeds if key doesn't exist
	ok, err := store.RDB.SetNX(ctx, key, "1", ttl).Result()
	if err != nil {
		return false // on error, don't throttle
	}

	// If SET NX succeeded, this is the first push in the window - allow it
	// If it failed, a recent push was sent - throttle
	return !ok
}

// GetThrottledCount returns the number of accumulated messages during throttle.
func GetThrottledCount(ctx context.Context, userID int64, sessionID string) int64 {
	countKey := fmt.Sprintf("push:count:%d:%s", userID, sessionID)
	count, _ := store.RDB.Incr(ctx, countKey).Result()
	store.RDB.Expire(ctx, countKey, 60*time.Second)
	return count
}

// ResetThrottledCount resets the count after sending a merged notification.
func ResetThrottledCount(ctx context.Context, userID int64, sessionID string) {
	countKey := fmt.Sprintf("push:count:%d:%s", userID, sessionID)
	store.RDB.Del(ctx, countKey)
}
