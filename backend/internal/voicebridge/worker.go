// Package voicebridge 提供 Voice Bridge Worker 的 NATS 控制通道。
//
// 实际的音频桥接（SFU 订阅 + LLM 连接）由独立的 Python 进程实现，
// 使用 LiveKit Agents SDK（livekit-agents + livekit-plugins-openai）。
//
// 本包只负责：
//   - 监听 NATS 控制通道，接收 StartBridge/StopBridge/InterruptBridge 指令；
//   - 将指令转发给 Python voicebridge 进程（通过 NATS）。
//
// Python voicebridge 进程位于 voicebridge/ 目录，使用 LiveKit Agents 框架。
package voicebridge

import (
	"sync"

	"github.com/askie/grix/backend/internal/pkg/logger"
	"github.com/nats-io/nats.go"
)

// NATS 主题常量（与 Python voicebridge 进程共享）
const (
	SubjectStartBridge     = "voicebridge.control.start"
	SubjectStopBridge      = "voicebridge.control.stop"
	SubjectInterruptBridge = "voicebridge.control.interrupt"
)

// WorkerConfig 是 Worker 的启动配置。
type WorkerConfig struct {
	NATSConn *nats.Conn
}

// Worker 监听 NATS 控制通道，记录活跃通话（用于 Stop 时的清理）。
type Worker struct {
	cfg  WorkerConfig
	mu   sync.Mutex
	subs []*nats.Subscription
}

// NewWorker 创建 Worker 实例。
func NewWorker(cfg WorkerConfig) *Worker {
	return &Worker{cfg: cfg}
}

// Start 启动 Go stub。
// 注意：不再订阅 SubjectStartBridge，避免与 Python voicebridge 进程竞争 NATS 消息。
// Go plain Subscribe 不回复 Request，会截获消息导致 15 秒超时。
// 实际的 bridge 控制全部由 Python 进程通过 NATS 订阅处理。
func (w *Worker) Start() error {
	if w.cfg.NATSConn == nil {
		logger.L.Warn("voicebridge: NATS not configured, running in stub mode")
		return nil
	}
	logger.L.Info("voicebridge Go stub started (Python process handles actual bridging)")
	return nil
}

// Stop 取消订阅。
func (w *Worker) Stop() {
	w.mu.Lock()
	defer w.mu.Unlock()
	for _, sub := range w.subs {
		_ = sub.Unsubscribe()
	}
	logger.L.Info("voicebridge Go stub stopped")
}
