package agentapi

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/askie/grix/backend/internal/pkg/logger"
	"github.com/askie/grix/backend/internal/store"
	"github.com/askie/grix/backend/internal/ws/protocol"
)

// mcp_session 元数据跨节点共享：mcpSessionStore 是各 ws 节点的内存结构，
// 会话在“Agent 帧首次到达的节点”创建，而 APP 的回帧到达“APP 连接所在节点”，
// 跨节点时后者内存无此会话。这里把会话的路由元数据（agentID/ownerID/sessionID）
// 额外写一份到 Redis（带 TTL、上行帧续期、Agent 断连即删），APP 节点内存 miss
// 时回退读取，从而把回帧正确路由回 Agent。
const mcpSessionRedisTTL = time.Hour

func mcpSessionRedisKey(id string) string {
	return "mcp:sess:" + id
}

type mcpSessionMeta struct {
	AgentID   int64  `json:"agent_id"`
	OwnerID   int64  `json:"owner_id"`
	SessionID string `json:"session_id"`
}

// persistMcpSession 写入/续期会话元数据。
func persistMcpSession(sess *mcpSession) {
	if store.RDB == nil || sess == nil {
		return
	}
	data, err := json.Marshal(mcpSessionMeta{AgentID: sess.agentID, OwnerID: sess.ownerID, SessionID: sess.sessionID})
	if err != nil {
		return
	}
	if err := store.RDB.Set(context.Background(), mcpSessionRedisKey(sess.id), data, mcpSessionRedisTTL).Err(); err != nil {
		logger.L.Warnf("[mcp-session] persist meta failed id=%s err=%v", sess.id, err)
	}
}

// loadMcpSession 从 Redis 读会话元数据（本节点内存 miss 时回退）。
func loadMcpSession(id string) *mcpSession {
	if store.RDB == nil {
		return nil
	}
	data, err := store.RDB.Get(context.Background(), mcpSessionRedisKey(id)).Bytes()
	if err != nil {
		return nil
	}
	var meta mcpSessionMeta
	if err := json.Unmarshal(data, &meta); err != nil {
		return nil
	}
	return &mcpSession{id: id, agentID: meta.AgentID, ownerID: meta.OwnerID, sessionID: meta.SessionID}
}

// deleteMcpSessionMeta 删除一批会话元数据（Agent 断连清理时调用）。
func deleteMcpSessionMeta(ids []string) {
	if store.RDB == nil || len(ids) == 0 {
		return
	}
	keys := make([]string, len(ids))
	for i, id := range ids {
		keys[i] = mcpSessionRedisKey(id)
	}
	if err := store.RDB.Del(context.Background(), keys...).Err(); err != nil {
		logger.L.Warnf("[mcp-session] delete meta failed err=%v", err)
	}
}

// redisCmdForwardMcpFrame 把一条 MCP 下行帧从 owner(APP) 所在节点转发到 agent
// 所在节点。MCP 帧是 fire-and-forget（请求/响应匹配由两端 MCP 用 JSON-RPC id
// 自行完成），因此无需 correlation/响应回传，比 local_action 转发更简单。
const redisCmdForwardMcpFrame = "_agent_api_mcp_frame_forward"

// forwardedMcpFrame 是跨节点转发的 MCP 帧载荷。
// OwnerID 用于 agent 共享多连接物理隔离:目标节点据此按 (agentID, ownerID) 精确选连接,
// 避免 B 的 MCP 回帧落到主人 A 的 connector。必须 >0;为 0 时目标节点直接丢弃（fail-closed）。
type forwardedMcpFrame struct {
	AgentID   int64           `json:"agent_id,string"`
	OwnerID   int64           `json:"owner_id,string,omitempty"`
	SessionID string          `json:"session_id"`
	Frame     json.RawMessage `json:"frame"`
}

// tryForwardMcpFrameToAgent 在 agent 未连本节点时，按路由把帧转发到 agent 所在
// 节点。返回 true 表示已成功发出（不保证对端投递成功），false 表示无法转发。
// ownerID 用于按 owner 跨节点路由(agent 共享多连接物理隔离);ownerID<=0 非法,直接失败。
func (m *Manager) tryForwardMcpFrameToAgent(agentID, ownerID int64, sessionID string, frame json.RawMessage) bool {
	if store.RDB == nil || len(frame) == 0 || ownerID <= 0 {
		return false
	}
	targetNode := loadAgentRouteForOwner(context.Background(), agentID, ownerID)
	if targetNode == "" || targetNode == m.getNodeID() {
		return false
	}
	req := forwardedMcpFrame{AgentID: agentID, OwnerID: ownerID, SessionID: sessionID, Frame: frame}
	data, err := json.Marshal(map[string]any{
		"cmd":     redisCmdForwardMcpFrame,
		"payload": req,
	})
	if err != nil {
		return false
	}
	if err := store.RDB.Publish(context.Background(), fmt.Sprintf("chan:%s", targetNode), data).Err(); err != nil {
		logger.L.Warnf("[mcp-session] publish forwarded mcp_frame failed node=%s agent=%d owner=%d: %v", targetNode, agentID, ownerID, err)
		return false
	}
	return true
}

// handleForwardedMcpFrame 在 agent 所在节点执行：按 (agentID, ownerID) 严格选连接;
// ownerID<=0 非法,记录告警并丢弃（fail-closed,不回退主连接）。
func (m *Manager) handleForwardedMcpFrame(req forwardedMcpFrame) {
	if req.OwnerID <= 0 {
		logger.L.Warnf("[mcp-session] forwarded mcp_frame rejected: missing owner agent=%d", req.AgentID)
		return
	}
	conn := m.lookupConnByOwner(req.AgentID, req.OwnerID)
	if conn == nil {
		logger.L.Warnf("[mcp-session] forwarded mcp_frame: target conn not found agent=%d owner=%d", req.AgentID, req.OwnerID)
		return
	}
	conn.sendPayload(protocol.CmdMcpFrame, conn.nextSeq(), protocol.McpFramePayload{
		SessionID: req.SessionID,
		Frame:     req.Frame,
	})
}

// HandleForwardedMcpFrameDispatch 是 Redis 订阅者的入口（由 HandleRedisDispatch 调用）。
func HandleForwardedMcpFrameDispatch(cmd string, payload json.RawMessage) bool {
	if cmd != redisCmdForwardMcpFrame {
		return false
	}
	var req forwardedMcpFrame
	if err := json.Unmarshal(payload, &req); err != nil {
		logger.L.Warnf("decode forwarded mcp_frame failed: %v", err)
		return true
	}
	globalMu.RLock()
	mgr := globalManager
	globalMu.RUnlock()
	if mgr == nil {
		logger.L.Warnf("drop forwarded mcp_frame: manager unavailable agent=%d", req.AgentID)
		return true
	}
	mgr.handleForwardedMcpFrame(req)
	return true
}
