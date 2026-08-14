package main

import (
	"context"
	"errors"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/askie/grix/backend/config"
	apiservice "github.com/askie/grix/backend/internal/api/service"
	"github.com/askie/grix/backend/internal/llm"
	"github.com/askie/grix/backend/internal/llm/provider"
	"github.com/askie/grix/backend/internal/pkg/logger"
	"github.com/askie/grix/backend/internal/pkg/proclock"
	"github.com/askie/grix/backend/internal/pkg/snowflake"
	"github.com/askie/grix/backend/internal/store"
	"github.com/askie/grix/backend/internal/ws/agentmsg"
)

func main() {
	configPath := "config.yaml"
	if len(os.Args) > 1 {
		configPath = os.Args[1]
	}

	logger.Init()

	lock := proclock.Acquire("llm")
	defer lock.Release()

	config.Load(configPath)

	// 内置 AI 流式 chunk 广播合并窗口（生产可调；置 0 退回逐 chunk）。
	agentmsg.ConfigureStreamCoalescing(config.C.LLM.StreamFlushIntervalMs, config.C.LLM.StreamFlushBytes)

	// Initialize infrastructure
	store.InitPostgres(config.C.Postgres)
	store.MaybeInitSchema()
	store.InitRedis(config.C.Redis)
	store.InitNATS(config.C.NATS)

	if err := snowflake.Init(config.C.Snowflake.MachineID); err != nil {
		logger.L.Fatalf("snowflake init error: %v", err)
	}

	// Initialize LLM providers
	providers := map[string]provider.Provider{}
	if config.C.LLM.OpenAI.APIKey != "" {
		providers["openai"] = provider.NewOpenAI(
			config.C.LLM.OpenAI.APIKey,
			config.C.LLM.OpenAI.BaseURL,
			config.C.LLM.OpenAI.DefaultModel,
		)
	}
	if config.C.LLM.Claude.APIKey != "" {
		providers["claude"] = provider.NewClaude(
			config.C.LLM.Claude.APIKey,
			config.C.LLM.Claude.BaseURL,
			config.C.LLM.Claude.DefaultModel,
		)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	server := llm.NewServer(config.C.Server.LLMPort, providers)

	go func() {
		if err := server.Start(ctx); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.L.Fatalf("llm server error: %v", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	logger.L.Info("llm server shutting down")
	cancel()
	server.Shutdown()
	// 内容审核 worker 也会被 llm 进程拉起（AI 回复触发），同样要等它收尾。
	apiservice.StopContentModerationWorkers()
}
