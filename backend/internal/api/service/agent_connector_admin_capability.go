package service

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/redis/go-redis/v9"

	"github.com/askie/grix/backend/internal/store"
)

// connectorAdminActionType 是连接器管理指令的 local_action 类型；与
// ws/agentapi.ConnectorAdminActionType 同值，这里独立写一份避免 import cycle
// （ws/agentapi 已经 import 本包）。两处必须一致。
const connectorAdminActionType = "connector_admin"

// loadAgentConnectorAdminCapableMap 批量判断这些 agent 当前的连接能不能收
// connector_admin：必须在线，且该连接 auth 时声明的 localActions 里含
// connector_admin。做法与 gatewayAgentStatesKnown 的 state_known 一致——
// 同一台机器上可能还挂着 Hermes(Python) 连接，它不声明这个能力，也就收不到指令，
// 所以手机端不能把它列成可选通道。
//
// 老 connector 从不声明，返回 false，客户端据此提示"请升级连接器"。
func loadAgentConnectorAdminCapableMap(ctx context.Context, ownerID int64, agentIDs []int64) map[int64]bool {
	capable := make(map[int64]bool, len(agentIDs))
	if store.RDB == nil || ownerID <= 0 || len(agentIDs) == 0 {
		return capable
	}
	if ctx == nil {
		ctx = context.Background()
	}

	pipe := store.RDB.Pipeline()
	type probe struct {
		ownerRoute *redis.StringCmd
		mainRoute  *redis.StringCmd
		ownerCaps  *redis.StringCmd
		mainCaps   *redis.StringCmd
	}
	probes := make(map[int64]probe, len(agentIDs))
	for _, id := range agentIDs {
		if id <= 0 {
			continue
		}
		probes[id] = probe{
			ownerRoute: pipe.Get(ctx, agentWSRouteKeyForOwner(id, ownerID)),
			mainRoute:  pipe.Get(ctx, agentWSRouteKey(id)),
			ownerCaps:  pipe.Get(ctx, agentWSCapabilitiesKeyForOwner(id, ownerID)),
			mainCaps:   pipe.Get(ctx, agentWSCapabilitiesKey(id)),
		}
	}
	_, _ = pipe.Exec(ctx)
	for id, p := range probes {
		// key 不存在/出错时 Val() 返回 ""，正好即"离线或无能力"。
		ownerOnline := strings.TrimSpace(p.ownerRoute.Val()) != ""
		mainOnline := strings.TrimSpace(p.mainRoute.Val()) != ""
		capable[id] = (ownerOnline && connectorAdminCapable(p.ownerCaps.Val())) ||
			(mainOnline && connectorAdminCapable(p.mainCaps.Val()))
	}
	return capable
}

// connectorAdminCapable 判断能力清单（refreshAgentCapabilities 持久化的 localActions
// JSON 数组）是否声明了 connector_admin；解析失败按未声明处理。
func connectorAdminCapable(capsJSON string) bool {
	capsJSON = strings.TrimSpace(capsJSON)
	if capsJSON == "" {
		return false
	}
	var actions []string
	if err := json.Unmarshal([]byte(capsJSON), &actions); err != nil {
		return false
	}
	for _, a := range actions {
		if a == connectorAdminActionType {
			return true
		}
	}
	return false
}
