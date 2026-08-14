// cmd/voicebridge 是 Voice Bridge 的 Go 侧 stub。
//
// 实际的音频桥接由 Python 进程（voicebridge/main.py）完成，
// 使用 LiveKit Agents SDK。本进程仅用于日志/监控。
//
// 生产部署时启动 Python 进程即可，无需启动本进程。
package main

import (
	"os"
	"os/signal"
	"syscall"

	"github.com/askie/grix/backend/config"
	"github.com/askie/grix/backend/internal/pkg/logger"
	"github.com/askie/grix/backend/internal/store"
	"github.com/askie/grix/backend/internal/voicebridge"
)

func main() {
	configPath := "config.yaml"
	if len(os.Args) > 1 {
		configPath = os.Args[1]
	}

	logger.Init()
	config.Load(configPath)
	store.InitNATS(config.C.NATS)

	worker := voicebridge.NewWorker(voicebridge.WorkerConfig{NATSConn: store.NC})
	if err := worker.Start(); err != nil {
		logger.L.Fatalf("voicebridge stub start error: %v", err)
	}

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	worker.Stop()
}
