package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/askie/grix/backend/internal/pkg/logger"
	"github.com/askie/grix/backend/internal/store"
)

func pushRealtimeEvent(userID int64, cmd string, payload interface{}) {
	if store.RDB == nil {
		return
	}

	ctx := context.Background()
	routeKey := fmt.Sprintf("im:ws:route:%d", userID)
	devices, err := store.RDB.HGetAll(ctx, routeKey).Result()
	if err != nil || len(devices) == 0 {
		return
	}

	data, err := json.Marshal(map[string]interface{}{
		"user_id": userID,
		"cmd":     cmd,
		"payload": payload,
	})
	if err != nil {
		logger.L.Warnf("marshal realtime event error: %v", err)
		return
	}

	publishedNodes := make(map[string]bool)
	for _, nodeID := range devices {
		if nodeID == "" || publishedNodes[nodeID] {
			continue
		}
		publishedNodes[nodeID] = true
		channel := fmt.Sprintf("chan:%s", nodeID)
		if err := store.RDB.Publish(ctx, channel, string(data)).Err(); err != nil {
			logger.L.Warnf("publish realtime event error user=%d node=%s err=%v", userID, nodeID, err)
		}
	}
}

// broadcastChannelName 是所有 ws 节点都订阅的全局 Redis pub/sub channel。
// 必须与 agentapi.connectorUpgradeBroadcastChannel 保持一致（service 不能反向 import ws 包）。
const broadcastChannelName = "chan:broadcast"

// publishBroadcastEvent 向所有 ws 节点广播一条内部命令（如 session_type 缓存失效）。
// 与定向 pushRealtimeEvent 不同，这里不分用户、不依赖路由表，全网节点各收一份。
func publishBroadcastEvent(cmd string, payload interface{}) {
	if store.RDB == nil || cmd == "" {
		return
	}
	data, err := json.Marshal(map[string]interface{}{
		"cmd":     cmd,
		"payload": payload,
	})
	if err != nil {
		logger.L.Warnf("marshal broadcast event error cmd=%s err=%v", cmd, err)
		return
	}
	if err := store.RDB.Publish(context.Background(), broadcastChannelName, string(data)).Err(); err != nil {
		logger.L.Warnf("publish broadcast event error cmd=%s err=%v", cmd, err)
	}
}

func hasOnlineRealtimeRoute(userID int64) bool {
	if userID <= 0 || store.RDB == nil {
		return false
	}

	ctx := context.Background()
	routeKey := fmt.Sprintf("im:ws:route:%d", userID)
	devices, err := store.RDB.HGetAll(ctx, routeKey).Result()
	if err != nil {
		logger.L.Warnf("load realtime route error user=%d err=%v", userID, err)
		return false
	}
	if len(devices) == 0 {
		return false
	}

	online := false
	staleDevices := make([]string, 0)
	for deviceID, rawNodeID := range devices {
		nodeID := strings.TrimSpace(rawNodeID)
		if nodeID == "" {
			staleDevices = append(staleDevices, deviceID)
			continue
		}
		exists, existsErr := store.RDB.Exists(
			ctx,
			fmt.Sprintf("im:ws:alive:%d:%s", userID, deviceID),
		).Result()
		if existsErr != nil {
			logger.L.Warnf(
				"check realtime route alive error user=%d device=%s err=%v",
				userID,
				deviceID,
				existsErr,
			)
			continue
		}
		if exists > 0 {
			online = true
			continue
		}
		staleDevices = append(staleDevices, deviceID)
	}

	if len(staleDevices) > 0 {
		if err := store.RDB.HDel(ctx, routeKey, staleDevices...).Err(); err != nil {
			logger.L.Warnf(
				"prune stale realtime routes error user=%d devices=%v err=%v",
				userID,
				staleDevices,
				err,
			)
		}
	}

	return online
}

func pushOfflineEvent(userID int64, cmd string, payload interface{}) {
	if userID <= 0 || cmd == "" || store.JS == nil {
		return
	}

	data, err := json.Marshal(map[string]interface{}{
		"user_id": userID,
		"cmd":     cmd,
		"payload": payload,
	})
	if err != nil {
		logger.L.Warnf("marshal offline event error user=%d cmd=%s err=%v", userID, cmd, err)
		return
	}

	subject := fmt.Sprintf("im.push.offline.%d", userID)
	if _, err := store.JS.Publish(subject, data); err != nil {
		logger.L.Warnf("publish offline event error user=%d cmd=%s err=%v", userID, cmd, err)
	}
}
