package agentapi

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/askie/grix/backend/internal/pkg/logger"
	"github.com/askie/grix/backend/internal/store"
)

const redisCmdForwardLifecycle = "_agent_api_lifecycle_forward"

// forwardedLifecycleCommand 承载需要跨副本转发的 agent 指令。
// OwnerID 用于 agent 共享场景:被共享者 B 触发的生命周期命令必须落到 B 的 connector,
// 而不是主人 A 的。OwnerID 必须 >0;为 0 时目标节点直接丢弃（fail-closed,不回退主连接）。
type forwardedLifecycleCommand struct {
	AgentID int64           `json:"agent_id,string"`
	OwnerID int64           `json:"owner_id,string,omitempty"`
	Cmd     string          `json:"cmd"`
	Payload json.RawMessage `json:"payload"`
}

// dispatchAgentCommand 是给 agent 主动下发 fire-and-forget 指令的统一收口：
// agent 连接在本节点就直发，否则跨副本转发到 agent 所在节点。
// 所有主动下发（event_stop / event_cancel / queue_clear / queue_snapshot_query 等）
// 都必须经过这里，避免在多副本下因连接不在本节点而静默丢失。
// ownerID 必须 >0：严格按 (agentID, ownerID) 找连接(agent 共享多连接物理隔离)；
// ownerID<=0 非法，直接拒绝（fail-closed），不 fallback 主连接。
func (m *Manager) dispatchAgentCommand(agentID, ownerID int64, cmd string, payload any) bool {
	if agentID <= 0 {
		return false
	}
	if ownerID <= 0 {
		logger.L.Warnf("reject agent command with missing owner: agent_id=%d cmd=%s", agentID, cmd)
		return false
	}
	conn := m.lookupConnByOwner(agentID, ownerID)
	if conn != nil && m.ensureAgentConnectionAuthoritative(conn) {
		return conn.sendPayload(cmd, conn.nextSeq(), payload)
	}
	return m.forwardEventLifecycleCommand(agentID, ownerID, cmd, payload)
}

// forwardEventLifecycleCommand 在 agent 未连接到本节点时，把 fire-and-forget 指令
// (event_stop / queue_snapshot_query / event_cancel / queue_clear 等)跨副本转发到
// agent 所在节点。agent 处理后通过各自结果通道(queue_snapshot / *_result / *_stop_result)
// 经 fanout 跨节点下发前端，无需 reply 回本节点。统一收口见 dispatchAgentCommand。
func (m *Manager) forwardEventLifecycleCommand(agentID, ownerID int64, cmd string, payload any) bool {
	if store.RDB == nil || agentID <= 0 || ownerID <= 0 {
		return false
	}
	// 按发起者 owner 找连接所在节点(共享场景下被共享者连接可能在另一节点)。
	targetNode := loadAgentRouteForOwner(context.Background(), agentID, ownerID)
	if targetNode == "" || targetNode == m.getNodeID() {
		return false
	}
	inner, err := json.Marshal(payload)
	if err != nil {
		logger.L.Warnf("marshal lifecycle payload failed agent=%d cmd=%s err=%v", agentID, cmd, err)
		return false
	}
	data, err := json.Marshal(map[string]any{
		"cmd":     redisCmdForwardLifecycle,
		"payload": forwardedLifecycleCommand{AgentID: agentID, OwnerID: ownerID, Cmd: cmd, Payload: inner},
	})
	if err != nil {
		return false
	}
	if err := store.RDB.Publish(context.Background(), fmt.Sprintf("chan:%s", targetNode), data).Err(); err != nil {
		logger.L.Warnf("publish forwarded lifecycle failed node=%s agent=%d cmd=%s err=%v", targetNode, agentID, cmd, err)
		return false
	}
	logger.L.Infof("[queue-debug] forwarded lifecycle cmd=%s agent=%d owner=%d -> node=%s", cmd, agentID, ownerID, targetNode)
	return true
}

// handleForwardedLifecycleCommand 在目标节点收到转发命令后发给本地 agent 连接。
// 按 (agentID, ownerID) 严格找连接(共享多连接物理隔离);ownerID<=0 非法,直接丢弃。
func (m *Manager) handleForwardedLifecycleCommand(fwd forwardedLifecycleCommand) {
	if fwd.OwnerID <= 0 {
		logger.L.Warnf("[queue-debug] forwarded lifecycle rejected: missing owner agent=%d cmd=%s", fwd.AgentID, fwd.Cmd)
		return
	}
	conn := m.lookupConnByOwner(fwd.AgentID, fwd.OwnerID)
	if conn == nil {
		logger.L.Warnf("[queue-debug] forwarded lifecycle target has no agent conn agent=%d owner=%d cmd=%s", fwd.AgentID, fwd.OwnerID, fwd.Cmd)
		return
	}
	if !m.ensureAgentConnectionAuthoritative(conn) {
		logger.L.Warnf(
			"[queue-debug] forwarded lifecycle target is stale agent=%d owner=%d cmd=%s epoch=%d",
			fwd.AgentID,
			fwd.OwnerID,
			fwd.Cmd,
			conn.connectionEpoch,
		)
		return
	}
	conn.sendPayload(fwd.Cmd, conn.nextSeq(), json.RawMessage(fwd.Payload))
}

// HandleForwardedLifecycleDispatch 是 Redis 订阅分发入口。
func HandleForwardedLifecycleDispatch(cmd string, payload json.RawMessage) bool {
	if cmd != redisCmdForwardLifecycle {
		return false
	}
	var fwd forwardedLifecycleCommand
	if err := json.Unmarshal(payload, &fwd); err != nil {
		logger.L.Warnf("decode forwarded lifecycle failed: %v", err)
		return true
	}
	globalMu.RLock()
	mgr := globalManager
	globalMu.RUnlock()
	if mgr == nil {
		return true
	}
	mgr.handleForwardedLifecycleCommand(fwd)
	return true
}
