package main

import (
	"context"
	"errors"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/askie/grix/backend/config"
	"github.com/askie/grix/backend/internal/pkg/logger"
	"github.com/askie/grix/backend/internal/pkg/proclock"
	"github.com/askie/grix/backend/internal/push"
	"github.com/askie/grix/backend/internal/store"
)

func main() {
	configPath := "config.yaml"
	if len(os.Args) > 1 {
		configPath = os.Args[1]
	}

	logger.Init()

	lock := proclock.Acquire("push")
	defer lock.Release()

	config.Load(configPath)

	// Initialize infrastructure
	store.InitPostgres(config.C.Postgres)
	store.MaybeInitSchema()
	store.InitRedis(config.C.Redis)
	store.InitNATS(config.C.NATS)

	providers, err := buildPushProviders(config.C.Push)
	if err != nil {
		logger.L.Fatalf("push provider config error: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	server := push.NewServer(
		config.C.Server.PushPort,
		providers.apnsSandbox,
		providers.apnsProduction,
		providers.fcm,
		providers.jpush,
		providers.webpush,
		providers.vendors,
	)

	go func() {
		if err := server.Start(ctx); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.L.Fatalf("push server error: %v", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	logger.L.Info("push server shutting down")
	cancel()
	server.Shutdown()
}
