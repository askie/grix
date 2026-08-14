package store

import (
	"context"
	"time"
)

// ReadyCheck pings all configured downstream dependencies and returns true
// only when every one of them responds within the given timeout.
func ReadyCheck(timeout time.Duration) (dbOk, redisOk, natsOk bool) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	if DB != nil {
		sqlDB, err := DB.DB()
		if err == nil {
			if err = sqlDB.PingContext(ctx); err == nil {
				dbOk = true
			}
		}
	}

	if RDB != nil {
		if err := RDB.Ping(ctx).Err(); err == nil {
			redisOk = true
		}
	}

	if NC != nil && NC.IsConnected() {
		natsOk = true
	}

	return
}
