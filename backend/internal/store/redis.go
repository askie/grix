package store

import (
	"context"

	"github.com/askie/grix/backend/config"
	"github.com/askie/grix/backend/internal/pkg/logger"
	"github.com/redis/go-redis/v9"
)

var RDB *redis.Client

func InitRedis(cfg config.RedisConfig) {
	RDB = redis.NewClient(&redis.Options{
		Addr:     cfg.Addr,
		Password: cfg.Password,
		DB:       cfg.DB,
		PoolSize: cfg.PoolSize,
	})
	if err := RDB.Ping(context.Background()).Err(); err != nil {
		logger.L.Fatalf("failed to connect redis: %v", err)
	}
	logger.L.Info("redis connected")
}
