package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/askie/grix/backend/config"
	"github.com/askie/grix/backend/internal/api"
	"github.com/askie/grix/backend/internal/api/service"
	jwtpkg "github.com/askie/grix/backend/internal/pkg/jwt"
	"github.com/askie/grix/backend/internal/pkg/logger"
	"github.com/askie/grix/backend/internal/pkg/middleware"
	"github.com/askie/grix/backend/internal/pkg/proclock"
	"github.com/askie/grix/backend/internal/pkg/snowflake"
	"github.com/askie/grix/backend/internal/store"
)

func main() {
	configPath := "config.yaml"
	if len(os.Args) > 1 {
		configPath = os.Args[1]
	}

	logger.Init()

	lock := proclock.Acquire("api")
	defer lock.Release()

	config.Load(configPath)

	// Initialize infrastructure
	store.InitPostgres(config.C.Postgres)
	store.MaybeInitSchema()
	store.InitRedis(config.C.Redis)
	store.InitNATS(config.C.NATS)

	if err := snowflake.Init(config.C.Snowflake.MachineID); err != nil {
		logger.L.Fatalf("snowflake init error: %v", err)
	}

	jwtpkg.Init(config.C.JWT.Secret, config.C.JWT.AccessTTL, config.C.JWT.RefreshTTL)
	middleware.InitRateLimitScript()

	// Initialize OSS client
	if err := service.InitOSS(); err != nil {
		logger.L.Warnf("oss init error: %v", err)
	}
	// 注入 SMS provider reload 钩子，并按当前 system_settings 构建一次 provider
	service.InitSmsBootstrap()
	appCtx, appCancel := context.WithCancel(context.Background())
	defer appCancel()
	service.StartRegisterWelcomeCompensationWorker(appCtx)
	service.StartEggTranslationWorker(appCtx)
	// 跨 api 节点同步链接黑名单失效广播（任一节点改规则后全网立即生效）。
	service.StartLinkBlocklistInvalidateListener(appCtx)
	// 链接黑名单命中计数：内存聚合 + 周期 flush，避免高频命中场景下行写竞争。
	service.StartLinkBlocklistHitFlusher(appCtx)
	// 支付成功 → 用户充值自动入账：订阅 pay.order.paid，把 wallet_topup 支付记进钱包。
	service.StartGatewayTopupConsumer(appCtx)
	// 充值对账补偿：兜住事件丢失 / 消费失败，把已付成功但未入账的充值单捞回。
	service.StartGatewayTopupReconciler(appCtx)
	service.StartReachConsumer(appCtx)
	service.StartReachScheduler(appCtx)

	// Start API server
	r := api.SetupRouter()
	addr := fmt.Sprintf(":%d", config.C.Server.APIPort)
	logger.L.Infof("api server starting on %s", addr)
	srv := &http.Server{
		Addr:    addr,
		Handler: r,
	}

	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.L.Fatalf("api server error: %v", err)
		}
	}()

	// Wait for shutdown signal
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	logger.L.Info("api server shutting down")
	appCancel()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		logger.L.Warnf("api server shutdown error: %v", err)
	}
	// 停内容审核 worker：先停 HTTP 入口不再收新消息，再让在跑的审核任务收尾退出。
	// 不停的话，进程退出会把正在处理的任务硬生生打断（审核状态卡在 processing）。
	service.StopContentModerationWorkers()
}
