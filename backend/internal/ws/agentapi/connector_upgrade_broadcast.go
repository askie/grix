package agentapi

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/askie/grix/backend/internal/pkg/logger"
	"github.com/askie/grix/backend/internal/pkg/snowflake"
	"github.com/askie/grix/backend/internal/store"
	"github.com/askie/grix/backend/internal/ws/protocol"
)

// redisCmdBroadcastConnectorUpgradePush 是 admin 触发 connector 升级 push 的广播 cmd。
// 每个 ws 节点都订阅广播 channel，收到后对本地所有在线 agent 派一份 local_action。
const redisCmdBroadcastConnectorUpgradePush = "connector_upgrade_push_broadcast"

// connectorUpgradeBroadcastChannel 是所有 ws 节点都订阅的 Redis pub/sub 全局 channel。
// 用于触发跨节点广播命令（目前唯一用例是 connector 升级 push）。
const connectorUpgradeBroadcastChannel = "chan:broadcast"

// BroadcastChannel 暴露给 ws 启动代码：作为追加订阅 channel。
func BroadcastChannel() string {
	return connectorUpgradeBroadcastChannel
}

type broadcastConnectorUpgradePushPayload struct {
	IssuedAt int64 `json:"issued_at"`
}

// PublishConnectorUpgradePush 由 admin push 接口调用，向所有 ws 节点广播一次升级 push。
// 返回 (nodes, err)：err == nil 表示广播成功发出，nodes 为收到该广播的订阅者数
// （单实例/哨兵 Redis 下即等于在线 ws 节点数，因为每个 ws 进程订阅 chan:broadcast 一次）。
// 具体派发由各 ws 节点订阅 chan:broadcast 后自行处理。
//
// CAVEAT（Redis Cluster）：PUBLISH 的返回值只统计「本分片」收到消息的客户端数。
// 若将来迁到 Redis Cluster，跨分片的订阅者不会被计入，nodes 会偏小、不再等于全网节点数。
// 届时需改用分片感知的 fan-out（如 SPUBLISH/分片轮询）或放弃精确计数。当前为单实例，成立。
func PublishConnectorUpgradePush() (nodes int64, err error) {
	if store.RDB == nil {
		return 0, fmt.Errorf("redis unavailable")
	}
	envelope := map[string]any{
		"cmd": redisCmdBroadcastConnectorUpgradePush,
		"payload": broadcastConnectorUpgradePushPayload{
			IssuedAt: snowflake.GenID(),
		},
	}
	data, err := json.Marshal(envelope)
	if err != nil {
		logger.L.Warnf("marshal broadcast connector_upgrade_push failed: %v", err)
		return 0, err
	}
	nodes, err = store.RDB.Publish(context.Background(), connectorUpgradeBroadcastChannel, data).Result()
	if err != nil {
		logger.L.Warnf("publish broadcast connector_upgrade_push failed: %v", err)
		return 0, err
	}
	return nodes, nil
}

// handleBroadcastConnectorUpgradePush 在每个 ws 节点订阅到广播 cmd 时执行：
// 对本节点上每个 agent 的每一条本地连接（主连接 + 共享连接）各派一份
// connector_upgrade_push local_action —— 每条连接都是独立的 connector 实例，
// 都应收到升级推送。actionID 每条连接唯一（幂等）；按 (agentID, ownerID) 精确
// 路由，绝不把某个 owner 的推送投到另一个 owner 的连接上。
func handleBroadcastConnectorUpgradePush() {
	globalMu.RLock()
	mgr := globalManager
	globalMu.RUnlock()
	if mgr == nil {
		return
	}
	var pushed int
	mgr.ForEachLocalAgentConn(func(conn *agentConn) bool {
		if conn.agentID <= 0 || conn.ownerID <= 0 {
			return true
		}
		action := protocol.LocalActionPayload{
			ActionID:   fmt.Sprintf("upgrade-push:%d", snowflake.GenID()),
			ActionType: "connector_upgrade_push",
		}
		if mgr.SendLocalActionForOwner(conn.agentID, conn.ownerID, action) {
			pushed++
		}
		return true
	})
	logger.L.Infof("broadcast connector_upgrade_push pushed=%d on node=%s", pushed, mgr.getNodeID())
}
