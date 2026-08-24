// cmd/gateway 是大模型计费网关：独立服务，只认虚拟Key，不查Grix登录态。
// 实现 LLM 计费网关。
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
	"github.com/redis/go-redis/v9"

	"github.com/askie/grix/backend/config"
	"github.com/askie/grix/backend/internal/gateway/credential"
	"github.com/askie/grix/backend/internal/gateway/fxsync"
	"github.com/askie/grix/backend/internal/gateway/httpapi"
	"github.com/askie/grix/backend/internal/gateway/pricing"
	"github.com/askie/grix/backend/internal/gateway/reconcile"
	"github.com/askie/grix/backend/internal/gateway/relay"
	"github.com/askie/grix/backend/internal/gateway/wallet"
	"github.com/askie/grix/backend/internal/pkg/logger"
	"github.com/askie/grix/backend/internal/pkg/middleware"
	"github.com/askie/grix/backend/internal/pkg/proclock"
	"github.com/askie/grix/backend/internal/pkg/snowflake"
	"github.com/askie/grix/backend/internal/store"
	"github.com/askie/grix/backend/internal/version"
)

const reconcileInterval = time.Hour

// dialRelayCache 连"中转模型设置"的缓存 Redis。用户在 api 服务改完设置会主动失效这份缓存，
// 所以两边必须连同一个 Redis。
//
// ⚠️ 这里刻意不走 store.InitRedis：它连不上会直接 Fatalf 退进程。而这份缓存**只是加速**
// （relay 对缓存读写全程容错，读写失败当次直接查库，功能完全不受影响）。计费网关是管钱的服务，
// 网关此前也完全不依赖 Redis（凭据轮询用的是进程内计数器）——绝不能因为多了一个纯缓存
// 就让它连启动都启动不了。
func dialRelayCache(cfg config.RedisConfig) *redis.Client {
	rdb := redis.NewClient(&redis.Options{
		Addr:     cfg.Addr,
		Password: cfg.Password,
		DB:       cfg.DB,
		PoolSize: cfg.PoolSize,
	})
	// 启动时 Ping 只为在日志里留一条明确的现状，探测结果不决定去留：
	// go-redis 自带断线重连，relay 的每次缓存读写也都对错误容错（失败即当次查库）。
	// 若在这里因 Ping 失败把 client 置 nil，就成了"启动时 Redis 恰好抖一下 → 永久降级、
	// 恢复了也用不上缓存"，反而更糟。
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := rdb.Ping(ctx).Err(); err != nil {
		logger.L.Warnf("gateway: relay settings cache unreachable at startup (%v); serving with per-request DB reads until it recovers", err)
	} else {
		logger.L.Info("gateway: relay settings cache connected")
	}
	return rdb
}

func main() {
	configPath := "config.yaml"
	if len(os.Args) > 1 {
		configPath = os.Args[1]
	}

	logger.Init()

	lock := proclock.Acquire("gateway")
	defer lock.Release()

	config.Load(configPath)
	if err := snowflake.Init(config.C.Snowflake.MachineID); err != nil {
		logger.L.Fatalf("snowflake init: %v", err)
	}

	store.InitPostgres(config.C.Postgres)
	store.MaybeInitSchema()

	// 上游厂商官方Key不再从 env 固化，改由塘主后台动态管理、密文落库；网关转发时按厂商实时解密取用。
	creds := credential.New(store.DB)
	handler := &httpapi.Handler{
		Wallets:     wallet.New(store.DB),
		Pricing:     pricing.New(store.DB),
		Credentials: creds,
		Relay:       relay.New(store.DB, dialRelayCache(config.C.Redis)),
		HTTPClient:  &http.Client{Timeout: 120 * time.Second},
	}

	router := gin.New()
	// 只信任回环+私网对端的 XFF，防伪造 X-Forwarded-For 操纵 ClientIP。
	middleware.ApplyTrustedProxies(router)
	router.Use(gin.Recovery())
	// 健康检查：跟平台其它服务对齐（/health 存活、/readyz 就绪），/healthz 保留兼容。
	healthOK := func(c *gin.Context) { c.Status(http.StatusOK) }
	router.GET("/health", healthOK)
	router.GET("/readyz", readyz)
	router.GET("/healthz", healthOK)
	router.GET("/version", func(c *gin.Context) { c.JSON(http.StatusOK, version.Get()) })
	// 两个协议入口在两个前缀下各注册一遍，是为了兜住 connector 的一个固有限制：
	// 它的 MITM 默认路由是「进程级单例」，同一台机器上 Claude 和 Codex 两个托管Agent 都接中转时，
	// 后配置的 targetBaseUrl 会覆盖前一个（一个要 /anthropic、一个要 /openai），前者静默失效。
	// 两种客户端的请求路径本来就不冲突（/v1/messages vs /v1/chat/completions），
	// 所以只要两个前缀下都认这些路径，它们共用同一条 MITM 路由也各自能被接到正确的 handler 上。
	// 鉴权头也天然兼容：Anthropic 入口同时认 x-api-key 和 Authorization: Bearer。
	// /openai/v1/responses：Codex / 新版 OpenAI SDK 主入口；DeepSeek 走官方 Responses，
	// 其他厂商按能力回落到 chat/completions 桥接。
	router.POST("/openai/v1/chat/completions", handler.ChatCompletions)
	router.POST("/openai/v1/responses", handler.Responses)
	router.POST("/openai/v1/messages", handler.Messages)
	router.POST("/anthropic/v1/messages", handler.Messages)
	// count_tokens：Claude Code 直连前估算上下文占用；鉴权与模型映射同 Messages，不计费。
	router.POST("/anthropic/v1/messages/count_tokens", handler.CountTokens)
	router.POST("/anthropic/v1/chat/completions", handler.ChatCompletions)
	router.POST("/anthropic/v1/responses", handler.Responses)
	// 再注册一组不带 /v1 的别名，兼容把 base_url 直接填成 /openai 或 /anthropic 的客户端：
	// 标准 OpenAI SDK 的 base_url 包含 /v1，请求路径是 /chat/completions；
	// 而 CC Switch 等工具会自己补 /v1/...。两种拼法都命中同一套 handler，
	// 避免用户因为“要不要加 /v1”的提示踩 404。
	router.POST("/openai/chat/completions", handler.ChatCompletions)
	router.POST("/openai/responses", handler.Responses)
	router.POST("/openai/messages", handler.Messages)
	router.POST("/anthropic/messages", handler.Messages)
	router.POST("/anthropic/messages/count_tokens", handler.CountTokens)
	router.POST("/anthropic/chat/completions", handler.ChatCompletions)
	router.POST("/anthropic/responses", handler.Responses)

	addr := fmt.Sprintf(":%d", config.C.Gateway.Port)
	server := &http.Server{Addr: addr, Handler: router}

	go func() {
		logger.L.Infof("gateway server listening on %s", addr)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.L.Fatalf("gateway server error: %v", err)
		}
	}()

	stopReconcile := startReconcileScheduler(creds)
	defer stopReconcile()

	stopFxSync := startFxSyncScheduler(config.C.Gateway.FxSync)
	defer stopFxSync()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	logger.L.Info("gateway server shutting down")
	_ = server.Shutdown(context.Background())
}

// startReconcileScheduler 每小时自动跑一轮对账（DeepSeek + 火山方舟），不用再手动敲 gatewayadmin reconcile。
// 对账用的凭据（DeepSeek查余额复用推理Key、火山查余额用对账AK/SK）每轮从后台管理的凭据表实时构造，
// 后台增删Key后无需重启即随下一轮生效。
func startReconcileScheduler(creds *credential.Service) (stop func()) {
	svc := reconcile.New(store.DB)
	ticker := time.NewTicker(reconcileInterval)
	done := make(chan struct{})

	runOnce := func() {
		// 对账跑在后台裸 goroutine 里，任何未预期的 panic 都不能拖垮整个网关进程（它还在服务计费请求）。
		defer func() {
			if r := recover(); r != nil {
				logger.L.Errorf("reconcile scheduler: recovered from panic: %v", r)
			}
		}()
		checkers := buildBalanceCheckers(creds)
		if len(checkers) == 0 {
			logger.L.Warn("reconcile scheduler: no enabled upstream credential, skip this round")
			return
		}
		// 跨副本互斥：gateway 未来若多副本部署（KEDA扩容），必须保证同一时刻只有一个副本在跑对账，
		// 否则同一次真实价格漂移会被多个副本各自判定、各自调价，价格跳得比预期猛。用 PG 会话级 advisory lock 保证。
		// 非Postgres或取锁失败时降级为"照常单机跑"（本地SQLite测试等场景），不阻断。
		release, ok := acquireAdvisoryLock(reconcileAdvisoryLockKey, "reconcile")
		if !ok {
			return // 别的副本正在跑这一轮，跳过
		}
		defer release()
		for _, err := range svc.RunAllActive(context.Background(), checkers) {
			logger.L.Errorf("reconcile scheduler: %v", err)
		}
	}

	go func() {
		runOnce() // 启动时先跑一轮，不用等满一个小时才有第一次对账
		for {
			select {
			case <-ticker.C:
				runOnce()
			case <-done:
				return
			}
		}
	}()

	return func() {
		ticker.Stop()
		close(done)
	}
}

// buildBalanceCheckers 每轮对账时从后台管理的凭据表实时构造各厂商的余额查询器。
// DeepSeek 查余额复用其推理Key（同账户）；火山查余额用对账用的 AK/SK（跟推理ARK Key不是一回事）。
func buildBalanceCheckers(creds *credential.Service) map[string]reconcile.BalanceChecker {
	checkers := map[string]reconcile.BalanceChecker{}
	if cred, err := creds.NextInference("deepseek"); err == nil {
		checkers["deepseek"] = reconcile.NewDeepSeekBalanceChecker(cred.APIKey)
	} else {
		// 某厂商没有可用凭据（未配 / 全部解密失败）时单独记日志，避免对账悄无声息长期跳过它形成观测盲区。
		logger.L.Warnf("reconcile scheduler: skip deepseek, no usable inference credential: %v", err)
	}
	if cred, ok := creds.FirstReconcile("volcano_ark"); ok {
		checker, err := reconcile.NewVolcanoArkBalanceChecker(cred.APIKey, cred.APISecret, cred.Region)
		if err != nil {
			logger.L.Errorf("reconcile scheduler: init volcano balance checker failed: %v", err)
		} else {
			checkers["volcano_ark"] = checker
		}
	} else {
		logger.L.Warn("reconcile scheduler: skip volcano_ark, no enabled reconcile credential")
	}
	return checkers
}

// 网关专用的 PG advisory lock key（各取一个固定常量，互不冲突）。
const (
	reconcileAdvisoryLockKey = 918273645
	fxSyncAdvisoryLockKey    = 918273646
)

// acquireAdvisoryLock 用 PG 会话级 advisory lock 保证同一时刻只有一个 gateway 副本在跑该周期任务。
// 取到返回 (释放函数, true)；没取到(别的副本在跑)返回 (nil, false)；
// 非Postgres或出错时降级为"允许本机跑"(返回一个no-op释放函数, true)，不阻断本地测试等场景。
func acquireAdvisoryLock(key int, name string) (release func(), ok bool) {
	sqlDB, err := store.DB.DB()
	if err != nil {
		logger.L.Warnf("%s lock: get sql db failed, run without cross-replica lock: %v", name, err)
		return func() {}, true
	}
	conn, err := sqlDB.Conn(context.Background())
	if err != nil {
		logger.L.Warnf("%s lock: open conn failed, run without cross-replica lock: %v", name, err)
		return func() {}, true
	}
	var locked bool
	if err := conn.QueryRowContext(context.Background(), "SELECT pg_try_advisory_lock($1)", key).Scan(&locked); err != nil {
		// 非Postgres(如本地SQLite)不支持该函数：降级为允许本机跑。
		_ = conn.Close()
		logger.L.Warnf("%s lock: advisory lock unsupported, run without cross-replica lock: %v", name, err)
		return func() {}, true
	}
	if !locked {
		_ = conn.Close()
		return nil, false
	}
	return func() {
		_, _ = conn.ExecContext(context.Background(), "SELECT pg_advisory_unlock($1)", key)
		_ = conn.Close()
	}, true
}

// startFxSyncScheduler 定时把外部汇率源的「源币种→USD」汇率同步进库，供充值入账换汇用。
// 跟对账一样：单协程、跨副本 advisory lock 互斥、panic 兜住，绝不拖垮正在服务计费请求的网关。
func startFxSyncScheduler(cfg config.GatewayFxSyncConfig) (stop func()) {
	if !cfg.Enabled {
		logger.L.Warn("fxsync scheduler: disabled by config, topup fx rate will not auto-update")
		return func() {}
	}
	if len(cfg.Currencies) == 0 {
		logger.L.Warn("fxsync scheduler: no currency configured, skip")
		return func() {}
	}
	interval := cfg.Interval
	if interval <= 0 {
		interval = fxsync.DefaultInterval
	}

	syncer := fxsync.New(store.DB, cfg.APIURL, cfg.APIKey, cfg.Currencies)
	ticker := time.NewTicker(interval)
	done := make(chan struct{})

	runOnce := func() {
		defer func() {
			if r := recover(); r != nil {
				logger.L.Errorf("fxsync scheduler: recovered from panic: %v", r)
			}
		}()
		release, ok := acquireAdvisoryLock(fxSyncAdvisoryLockKey, "fxsync")
		if !ok {
			return // 别的副本正在跑这一轮，跳过
		}
		defer release()
		syncer.SyncOnce(context.Background())
	}

	go func() {
		runOnce() // 启动即拉一轮，不用等满一个周期才有第一份汇率
		for {
			select {
			case <-ticker.C:
				runOnce()
			case <-done:
				return
			}
		}
	}()

	logger.L.Infof("fxsync scheduler: started, currencies=%v interval=%s", cfg.Currencies, interval)
	return func() {
		ticker.Stop()
		close(done)
	}
}

// readyz 是网关的就绪探针：只探 Postgres。钱包、虚拟 Key、计价规则全在库里，
// 连不上就一个请求也服务不了，此时必须把本实例摘出 Service 端点。
//
// 刻意不把 Redis 计入：本服务从不调用 store.InitRedis（见 dialRelayCache 的说明——
// 那份缓存只是加速，连不上照样能计费），store.RDB 恒为 nil，把它算进来会让网关永远
// not ready，滚动发布再也起不来。
func readyz(c *gin.Context) {
	dbOk, _, _ := store.ReadyCheck(3 * time.Second)
	if dbOk {
		c.Status(http.StatusOK)
		return
	}
	c.JSON(http.StatusServiceUnavailable, gin.H{"db": dbOk})
}
