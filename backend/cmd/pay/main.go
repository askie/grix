// cmd/pay 是独立支付系统服务：统一收款 / 退款中台，支付渠道可插拔。
// 支付服务入口。
package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/askie/grix/backend/config"
	"github.com/askie/grix/backend/internal/pay"
	"github.com/askie/grix/backend/internal/pay/channel"
	"github.com/askie/grix/backend/internal/pay/channel/alipay"
	"github.com/askie/grix/backend/internal/pay/channel/mock"
	"github.com/askie/grix/backend/internal/pay/channel/paypal"
	payhttp "github.com/askie/grix/backend/internal/pay/httpapi"
	"github.com/askie/grix/backend/internal/pay/paynats"
	"github.com/askie/grix/backend/internal/pkg/logger"
	"github.com/askie/grix/backend/internal/pkg/proclock"
	"github.com/askie/grix/backend/internal/pkg/snowflake"
	"github.com/askie/grix/backend/internal/store"
	"github.com/askie/grix/backend/internal/systemsetting"
	"github.com/askie/grix/backend/internal/version"
)

func main() {
	configPath := "config.yaml"
	if len(os.Args) > 1 {
		configPath = os.Args[1]
	}

	logger.Init()

	lock := proclock.Acquire("pay")
	defer lock.Release()

	config.Load(configPath)
	// notify_url_base 是第三方(支付宝/PayPal)回调与用户跳转回来的对外基址。为空会让回调地址
	// 退化成相对路径（notifyURL = "" + "/v1/pay/notify/{channel}"），第三方通知永远打不进来，
	// 且全程无任何报错——静默失败、排查极痛。故真实部署一律 fail-loud 挡在启动最前处（早于连库）；
	// 仅本地 mock 联调(AIBOT_PAY_MOCK_ENABLED=1)允许留空；生产环境由各区配置或 secret 提供。
	if config.C.Pay.NotifyURLBase == "" && os.Getenv("AIBOT_PAY_MOCK_ENABLED") != "1" {
		logger.L.Fatalf("pay: notify_url_base 为空——请在本区 configmap 的 pay.notify_url_base 或 secret 的 AIBOT_PAY_NOTIFY_URL_BASE 配置第三方回调对外基址(如 https://grix.dhf.pub)")
	}
	if err := snowflake.Init(config.C.Snowflake.MachineID); err != nil {
		logger.L.Fatalf("snowflake init: %v", err)
	}
	store.InitPostgres(config.C.Postgres)
	store.MaybeInitSchema()

	registry := channel.NewRegistry()
	registerChannels(registry)
	// 假通道：仅本地开发 / 端到端联调时按需注册，绝不进生产。
	if os.Getenv("AIBOT_PAY_MOCK_ENABLED") == "1" {
		registry.Register(mock.New())
	}
	logger.L.Infof("pay: registered channels %v", registry.Codes())

	// outbound 事件通道：配置了 NATS 则用 JetStream 真投递，否则 Nop 空转。
	// 支付 / 退款结果事件（pay.order.* / pay.refund.*）经此发布，
	// 充值业务侧订阅后据此增加余额。
	var notifier pay.Notifier = pay.NopNotifier{}
	if config.C.NATS.URL != "" {
		store.InitNATS(config.C.NATS)
		notifier = paynats.New()
		logger.L.Infof("pay: outbound events via jetstream stream=%s", config.C.NATS.StreamName)
	} else {
		logger.L.Warn("pay: NATS url empty, outbound events disabled (nop notifier)")
	}
	svc := pay.NewService(pay.NewStore(store.DB), registry, notifier, config.C.Pay.NotifyURLBase)
	h := &payhttp.Handler{Svc: svc}

	// 对账补偿：周期扫描长时间仍 CREATED 的支付单，主动查单推进，兜第三方通知丢失（审查 R1）。
	reconcileCtx, reconcileCancel := context.WithCancel(context.Background())
	defer reconcileCancel()
	go func() {
		ticker := time.NewTicker(60 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-reconcileCtx.Done():
				return
			case <-ticker.C:
				// 失败已由 ReconcileStale 内部统一告警（一条汇总 Errorf + 一条 pay.reconcile.failed
				// 汇总事件，带失败原因样本），这里不再重复打——同一件事出两条错误日志，反而
				// 让人以为发生了两次。这里只报扫描整轮起不来（补偿当轮完全停摆）和正常进展。
				advanced, _, err := svc.ReconcileStale(reconcileCtx, time.Now().Add(-3*time.Minute), pay.ReconcileScanLimit)
				switch {
				case err != nil:
					logger.L.Errorf("pay reconcile: 本轮对账扫描失败，补偿链路停摆: %v", err)
				case advanced > 0:
					// advanced 只统计真的推进了状态的单（入账 / 关单 / 落失败），不含 no-op。
					logger.L.Infof("pay reconcile: advanced %d stale orders", advanced)
				}
			}
		}
	}()

	router := gin.New()
	router.Use(gin.Recovery())
	healthOK := func(c *gin.Context) { c.Status(http.StatusOK) }
	router.GET("/health", healthOK)
	router.GET("/readyz", healthOK)
	router.GET("/healthz", healthOK)
	router.GET("/version", func(c *gin.Context) { c.JSON(http.StatusOK, version.Get()) })
	payhttp.Register(router, h)

	port := config.C.Pay.Port
	if port == 0 {
		port = pay.DefaultPort
	}
	addr := fmt.Sprintf(":%d", port)
	server := &http.Server{Addr: addr, Handler: router}

	go func() {
		logger.L.Infof("pay server listening on %s", addr)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.L.Fatalf("pay server error: %v", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	logger.L.Info("pay server shutting down")
	_ = server.Shutdown(context.Background())
}

// registerChannels 始终注册支付宝 + PayPal 两个适配器，凭证按需从塘主后台的
// 加密配置动态读取（见 internal/systemsetting.GetPayChannelSettings）。某通道
// 未在后台启用/未填凭证时，其 CreatePay 等调用会返回明确的“未启用”错误，
// 不需要在进程启动时按区域取舍注册哪些通道——两区的差异完全由后台配置决定。
func registerChannels(reg *channel.Registry) {
	reg.Register(alipay.NewAdapter(func() (alipay.Config, bool, error) {
		s, err := systemsetting.GetPayChannelSettings()
		if err != nil {
			return alipay.Config{}, false, err
		}
		return alipay.Config{
			AppID:           s.Alipay.AppID,
			PrivateKey:      s.Alipay.PrivateKey,
			AlipayPublicKey: s.Alipay.AlipayPublicKey,
			Sandbox:         s.Alipay.Sandbox,
			SignType:        s.Alipay.SignType,
		}, s.Alipay.Enabled, nil
	}))
	reg.Register(paypal.NewAdapter(func() (paypal.Config, bool, error) {
		s, err := systemsetting.GetPayChannelSettings()
		if err != nil {
			return paypal.Config{}, false, err
		}
		return paypal.Config{
			ClientID:  s.Paypal.ClientID,
			Secret:    s.Paypal.ClientSecret,
			Sandbox:   s.Paypal.Sandbox,
			WebhookID: s.Paypal.WebhookID,
		}, s.Paypal.Enabled, nil
	}))
}
