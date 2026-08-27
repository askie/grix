package agentapi

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/askie/grix/backend/internal/pkg/logger"
	"github.com/askie/grix/backend/internal/pkg/snowflake"
	"github.com/askie/grix/backend/internal/store"
	"github.com/askie/grix/backend/internal/ws/protocol"
)

// redisCmdBroadcastConnectorRollbackPush 是 admin 触发定向 connector_rollback 的广播 cmd。
// 与 connector_upgrade_push 的区别：那个只是催客户端"现在跑一次常规升级检查"，走的仍是
// staged + guardian 的事务链；connector_rollback 是"立刻把这个版本装上去然后重启"，
// 客户端侧直接 npm install + SIGTERM，不经 guardian。老客户端卡在事务链上升不动时，
// 这是唯一还能远端救回来的通道。
const redisCmdBroadcastConnectorRollbackPush = "connector_rollback_push_broadcast"

// 一次 push 的送达集合。派发发生在各个 ws 节点上，admin 进程本身通常没有 agent 连接，
// 只能靠这个集合回收"到底哪些 agent 真收到了帧"。
func connectorRollbackDispatchKey(pushID string) string {
	return "connector:rollback_push:" + pushID + ":dispatched"
}

// 单 agent 冷却键。重复推同一台机器会让它反复 npm install + 重启，冷却期内直接跳过。
func connectorRollbackCooldownKey(agentID int64) string {
	return "connector:rollback_push:cooldown:" + strconv.FormatInt(agentID, 10)
}

const (
	connectorRollbackDispatchTTL = 10 * time.Minute
	// 冷却在"帧确实发出去了"的那一刻就地打上，不依赖 admin 侧回收到回执。
	// 送达发生在 ws 节点，回执要跨 Redis 再被有界轮询捞回来；把冷却挂在回执上，
	// SAdd 失败、回写晚于轮询窗口、admin 客户端中途断连这三种情况都会漏打，
	// 于是同一台机器被重复推一次 npm install + 重启。语义本来就是"推过的别再推"。
	connectorRollbackCooldownTTL = 15 * time.Minute
)

type broadcastConnectorRollbackPushPayload struct {
	PushID        string  `json:"push_id"`
	TargetVersion string  `json:"target_version"`
	AgentIDs      []int64 `json:"agent_ids"`
}

// PublishConnectorRollbackPush 向所有 ws 节点广播一次定向 rollback push。
// 只负责发出广播；哪些 agent 真的收到由各节点写回 dispatch 集合，调用方用
// ConnectorRollbackDispatched 回收。
func PublishConnectorRollbackPush(pushID, targetVersion string, agentIDs []int64) error {
	if store.RDB == nil {
		return fmt.Errorf("redis unavailable")
	}
	envelope := map[string]any{
		"cmd": redisCmdBroadcastConnectorRollbackPush,
		"payload": broadcastConnectorRollbackPushPayload{
			PushID:        pushID,
			TargetVersion: targetVersion,
			AgentIDs:      agentIDs,
		},
	}
	data, err := json.Marshal(envelope)
	if err != nil {
		logger.L.Warnf("marshal broadcast connector_rollback_push failed: %v", err)
		return err
	}
	if err := store.RDB.Publish(context.Background(), connectorUpgradeBroadcastChannel, data).Err(); err != nil {
		logger.L.Warnf("publish broadcast connector_rollback_push failed: %v", err)
		return err
	}
	return nil
}

// ConnectorRollbackDispatched 读回本次 push 已确认送达的 agent 列表。
func ConnectorRollbackDispatched(ctx context.Context, pushID string) ([]int64, error) {
	if store.RDB == nil {
		return nil, fmt.Errorf("redis unavailable")
	}
	members, err := store.RDB.SMembers(ctx, connectorRollbackDispatchKey(pushID)).Result()
	if err != nil {
		return nil, err
	}
	out := make([]int64, 0, len(members))
	for _, m := range members {
		id, convErr := strconv.ParseInt(m, 10, 64)
		if convErr != nil {
			continue
		}
		out = append(out, id)
	}
	return out, nil
}

// MarkConnectorRollbackCooldown 给已发出下发帧的 agent 打冷却，避免重复推导致反复重装。
// 由派发侧就地调用（见 handleBroadcastConnectorRollbackPush）。
func MarkConnectorRollbackCooldown(ctx context.Context, agentIDs []int64) {
	if store.RDB == nil {
		return
	}
	for _, id := range agentIDs {
		if err := store.RDB.Set(ctx, connectorRollbackCooldownKey(id), "1", connectorRollbackCooldownTTL).Err(); err != nil {
			logger.L.Warnf("set connector rollback cooldown failed agent=%d: %v", id, err)
		}
	}
}

// ConnectorRollbackInCooldown 返回处于冷却期、本次应当跳过的 agent。
func ConnectorRollbackInCooldown(ctx context.Context, agentIDs []int64) map[int64]bool {
	skip := make(map[int64]bool)
	if store.RDB == nil {
		return skip
	}
	for _, id := range agentIDs {
		n, err := store.RDB.Exists(ctx, connectorRollbackCooldownKey(id)).Result()
		if err != nil {
			// Redis 抖动时按"不在冷却"处理：漏挡一次重复推，好过把整批救援挡在门外。
			logger.L.Warnf("check connector rollback cooldown failed agent=%d: %v", id, err)
			continue
		}
		if n > 0 {
			skip[id] = true
		}
	}
	return skip
}

// handleBroadcastConnectorRollbackPush 在每个 ws 节点订阅到广播 cmd 时执行：
// 对本节点上命中的每一条连接各派一份 connector_rollback。SendLocalActionForOwner
// 自带 local_action_v1 与 action_type 声明校验，不支持 connector_rollback 的老客户端
// 会被它挡下并记日志，不会计入送达。
//
// 同一 agent 可能有多条 owner 连接（agent 共享），这里逐连接下发而不是按 agent 去重：
// 共享场景下每个 owner 侧跑的是各自那台机器上的 connector 实例，各自都需要被救。
func handleBroadcastConnectorRollbackPush(p broadcastConnectorRollbackPushPayload) {
	globalMu.RLock()
	mgr := globalManager
	globalMu.RUnlock()
	if mgr == nil {
		return
	}
	if p.TargetVersion == "" || len(p.AgentIDs) == 0 {
		return
	}
	want := make(map[int64]bool, len(p.AgentIDs))
	for _, id := range p.AgentIDs {
		want[id] = true
	}

	var dispatched []int64
	mgr.ForEachLocalAgentConn(func(conn *agentConn) bool {
		if conn.agentID <= 0 || conn.ownerID <= 0 || !want[conn.agentID] {
			return true
		}
		action := protocol.LocalActionPayload{
			ActionID:   fmt.Sprintf("rollback-push:%s:%d", p.PushID, snowflake.GenID()),
			ActionType: "connector_rollback",
			Params: map[string]any{
				"target_version": p.TargetVersion,
				"reason":         "admin_rollback_push",
			},
		}
		if mgr.SendLocalActionForOwner(conn.agentID, conn.ownerID, action) {
			dispatched = append(dispatched, conn.agentID)
		}
		return true
	})

	if len(dispatched) == 0 {
		return
	}
	// 先打冷却再写送达集合：冷却是防重复下发的护栏，送达集合只是给调用方看的回执，
	// 前者不能因为后者失败而漏掉。用 context.Background()——冷却的依据是"帧已发出"，
	// 与 admin 请求是否还活着无关。
	MarkConnectorRollbackCooldown(context.Background(), dispatched)
	if store.RDB != nil {
		ctx := context.Background()
		key := connectorRollbackDispatchKey(p.PushID)
		members := make([]any, 0, len(dispatched))
		for _, id := range dispatched {
			members = append(members, strconv.FormatInt(id, 10))
		}
		if err := store.RDB.SAdd(ctx, key, members...).Err(); err != nil {
			logger.L.Warnf("record connector rollback dispatch failed push=%s: %v", p.PushID, err)
		} else if err := store.RDB.Expire(ctx, key, connectorRollbackDispatchTTL).Err(); err != nil {
			// 集合只用于回收送达结果，必须带 TTL，否则每次 push 都在 Redis 里留垃圾。
			logger.L.Warnf("set connector rollback dispatch ttl failed push=%s: %v", p.PushID, err)
		}
	}
	logger.L.Infof(
		"broadcast connector_rollback_push push=%s target=%s dispatched=%d on node=%s",
		p.PushID, p.TargetVersion, len(dispatched), mgr.getNodeID(),
	)
}
