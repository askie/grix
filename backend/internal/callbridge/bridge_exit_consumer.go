// Package callbridge 提供通过 NATS 控制 Voice Bridge Worker 的 BridgeManager 实现。
package callbridge

import (
	"context"
	"encoding/json"
	"strconv"

	"github.com/askie/grix/backend/internal/pkg/logger"
	"github.com/nats-io/nats.go"
)

const subjectBridgeExit = "voicebridge.control.bridge_exit"

// BridgeExitHandler 是收到 bridge_exit 消息后的回调签名。
type BridgeExitHandler func(ctx context.Context, callID int64)

// BridgeExitConsumer 订阅 voicebridge bridge_exit 事件，通知 ws 服务清理通话资源。
type BridgeExitConsumer struct {
	nc      *nats.Conn
	handler BridgeExitHandler
	sub     *nats.Subscription
}

// NewBridgeExitConsumer 创建 consumer。handler 在收到 bridge_exit 时被调用。
func NewBridgeExitConsumer(nc *nats.Conn, handler BridgeExitHandler) *BridgeExitConsumer {
	return &BridgeExitConsumer{nc: nc, handler: handler}
}

// Start 订阅 NATS 主题。
func (c *BridgeExitConsumer) Start() error {
	if c.nc == nil {
		logger.L.Warn("bridge_exit_consumer: NATS not configured, skipping")
		return nil
	}
	sub, err := c.nc.Subscribe(subjectBridgeExit, c.handleMsg)
	if err != nil {
		return err
	}
	c.sub = sub
	logger.L.Info("bridge_exit_consumer: subscribed to " + subjectBridgeExit)
	return nil
}

// Stop 取消订阅。
func (c *BridgeExitConsumer) Stop() {
	if c.sub != nil {
		_ = c.sub.Unsubscribe()
	}
}

func (c *BridgeExitConsumer) handleMsg(msg *nats.Msg) {
	var payload struct {
		CallID string `json:"call_id"`
	}
	if err := json.Unmarshal(msg.Data, &payload); err != nil {
		logger.L.Warnf("bridge_exit_consumer: unmarshal error: %v", err)
		return
	}
	callID, err := strconv.ParseInt(payload.CallID, 10, 64)
	if err != nil || callID <= 0 {
		logger.L.Warnf("bridge_exit_consumer: invalid call_id=%q", payload.CallID)
		return
	}
	logger.L.Infof("bridge_exit_consumer: received call=%d", callID)
	c.handler(context.Background(), callID)
}
