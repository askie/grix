package dispatch

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/askie/grix/backend/internal/pkg/logger"
	"github.com/askie/grix/backend/internal/store"
	"github.com/askie/grix/backend/internal/ws/agentapi"
	"github.com/askie/grix/backend/internal/ws/handler"
	"github.com/askie/grix/backend/internal/ws/protocol"
)

// StartRedisSub subscribes to this node's channel for cross-node message delivery.
func StartRedisSub(nodeID string, hub handler.HubInterface) func() {
	if store.RDB == nil {
		return func() {}
	}

	channel := fmt.Sprintf("chan:%s", nodeID)
	broadcastChannel := agentapi.BroadcastChannel()
	ctx, cancel := context.WithCancel(context.Background())
	// 同时订阅本节点定向 channel + 全局广播 channel；后者用于
	// admin 升级 push 等需要让所有 ws 节点都收到一份的命令。
	sub := store.RDB.Subscribe(ctx, channel, broadcastChannel)
	// 等待订阅确认，确保返回时已可接收消息。
	if _, err := sub.Receive(ctx); err != nil {
		logger.L.Warnf("redis pubsub initial receive error: %v", err)
	}
	ch := sub.Channel()
	done := make(chan struct{})
	var stopOnce sync.Once

	go func() {
		defer close(done)
		for {
			select {
			case <-ctx.Done():
				return
			case msg, ok := <-ch:
				if !ok {
					return
				}
				var envelope struct {
					UserID          int64           `json:"user_id"`
					Cmd             string          `json:"cmd"`
					Payload         json.RawMessage `json:"payload"`
					ExcludeDeviceID string          `json:"exclude_device_id,omitempty"`
					TargetDeviceID  string          `json:"target_device_id,omitempty"`
				}
				if err := json.Unmarshal([]byte(msg.Payload), &envelope); err != nil {
					logger.L.Warnf("redis pubsub unmarshal error: %v", err)
					continue
				}
				if handler.HandleInternalRedisDispatch(envelope.Cmd, envelope.Payload) {
					continue
				}
				if agentapi.HandleRedisDispatch(envelope.Cmd, envelope.Payload) {
					continue
				}

				conns := hub.GetUserConns(envelope.UserID)
				for _, c := range conns {
					if envelope.TargetDeviceID != "" && c.GetDeviceID() != envelope.TargetDeviceID {
						continue
					}
					if envelope.ExcludeDeviceID != "" && c.GetDeviceID() == envelope.ExcludeDeviceID {
						continue
					}
					// 跨节点投递到网站访客连接时，同样隐藏 agent 内部过程消息。
					if handler.WidgetDropPushRaw(c.GetPlatform(), envelope.Cmd, envelope.Payload) {
						continue
					}
					pkt := &protocol.Packet{
						Cmd:     envelope.Cmd,
						Seq:     c.NextSeq(),
						Payload: envelope.Payload,
					}
					c.SendPacket(pkt)
					if envelope.Cmd == protocol.CmdKicked {
						hub.Unregister(c)
						c.Close()
					}
				}
			}
		}
	}()

	logger.L.Infof("redis pubsub subscriber started on channels: %s,%s", channel, broadcastChannel)

	return func() {
		stopOnce.Do(func() {
			cancel()
			_ = sub.Close()
			select {
			case <-done:
			case <-time.After(2 * time.Second):
				logger.L.Warnf("redis pubsub subscriber shutdown timed out channel=%s", channel)
			}
		})
	}
}
