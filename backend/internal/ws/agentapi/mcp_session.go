package agentapi

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/agentscope"
	"github.com/askie/grix/backend/internal/pkg/logger"
	"github.com/askie/grix/backend/internal/pkg/snowflake"
	"github.com/askie/grix/backend/internal/store"
	"github.com/askie/grix/backend/internal/ws/protocol"
)

// mcpSession 是一次 MCP 会话的可信绑定。
type mcpSession struct {
	id        string
	agentID   int64
	ownerID   int64
	sessionID string
	createdAt time.Time
}

// mcpSessionStore 管理活跃 MCP 会话。
type mcpSessionStore struct {
	mu      sync.RWMutex
	byID    map[string]*mcpSession
	byKey   map[string]*mcpSession // (agentID:sessionID) → session，用于幂等
	byAgent map[int64][]*mcpSession
}

func newMcpSessionStore() *mcpSessionStore {
	return &mcpSessionStore{
		byID:    make(map[string]*mcpSession),
		byKey:   make(map[string]*mcpSession),
		byAgent: make(map[int64][]*mcpSession),
	}
}

func mcpSessionKey(agentID int64, sessionID string) string {
	return fmt.Sprintf("%d:%s", agentID, sessionID)
}

// getOrCreate 按 (agentID, sessionID) 幂等获取或创建 mcp_session。
// 同一 agent + 业务会话恒返回同一个会话与 mcp_session_id。
func (s *mcpSessionStore) getOrCreate(agentID, ownerID int64, sessionID string) *mcpSession {
	key := mcpSessionKey(agentID, sessionID)
	s.mu.RLock()
	if existing := s.byKey[key]; existing != nil {
		s.mu.RUnlock()
		return existing
	}
	s.mu.RUnlock()

	s.mu.Lock()
	defer s.mu.Unlock()
	// double-check
	if existing := s.byKey[key]; existing != nil {
		return existing
	}
	id := fmt.Sprintf("mcp_%d", snowflake.GenID())
	sess := &mcpSession{id: id, agentID: agentID, ownerID: ownerID, sessionID: sessionID, createdAt: time.Now()}
	s.byID[id] = sess
	s.byKey[key] = sess
	s.byAgent[agentID] = append(s.byAgent[agentID], sess)
	logger.L.Infof("[mcp-session] created id=%s agent=%d owner=%d session=%s", id, agentID, ownerID, sessionID)
	return sess
}

func (s *mcpSessionStore) get(id string) *mcpSession {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.byID[id]
}

func (s *mcpSessionStore) remove(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sess := s.byID[id]
	if sess == nil {
		return
	}
	delete(s.byID, id)
	delete(s.byKey, mcpSessionKey(sess.agentID, sess.sessionID))
	list := s.byAgent[sess.agentID]
	for i, item := range list {
		if item.id == id {
			s.byAgent[sess.agentID] = append(list[:i], list[i+1:]...)
			break
		}
	}
}

func (s *mcpSessionStore) removeByAgent(agentID int64) []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	var ids []string
	for _, sess := range s.byAgent[agentID] {
		ids = append(ids, sess.id)
		delete(s.byID, sess.id)
		delete(s.byKey, mcpSessionKey(sess.agentID, sess.sessionID))
	}
	delete(s.byAgent, agentID)
	return ids
}

// removeByAgentOwner 仅清属于 (agentID, ownerID) 的 mcp_session，用于共享场景下被共享者断开时
// 只清自己的 session，不影响主人或其他被共享者还在用的资源。
func (s *mcpSessionStore) removeByAgentOwner(agentID, ownerID int64) []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	list := s.byAgent[agentID]
	if len(list) == 0 {
		return nil
	}
	ids := make([]string, 0, len(list))
	remaining := list[:0]
	for _, sess := range list {
		if sess.ownerID == ownerID {
			ids = append(ids, sess.id)
			delete(s.byID, sess.id)
			delete(s.byKey, mcpSessionKey(sess.agentID, sess.sessionID))
			continue
		}
		remaining = append(remaining, sess)
	}
	if len(remaining) == 0 {
		delete(s.byAgent, agentID)
	} else {
		s.byAgent[agentID] = remaining
	}
	return ids
}

// --- 会话归属校验 ---

// verifyAgentSessionMembership 校验该 agent 是否为目标 session 的合法参与成员（member_type=2）。
func verifyAgentSessionMembership(agentID int64, sessionID string) bool {
	var count int64
	if err := store.DB.Model(&model.SessionMember{}).
		Where("session_id = ? AND member_id = ? AND member_type = 2", sessionID, agentID).
		Count(&count).Error; err != nil {
		logger.L.Warnf("[mcp-session] membership check failed agent=%d session=%s err=%v", agentID, sessionID, err)
		return false
	}
	return count > 0
}

// --- tools/call 鉴权（闸3）---

// mcpFrameMethod 从 JSON-RPC frame 中提取 method 字段（不做完整解析）。
type mcpJsonRpcRequest struct {
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
}

type mcpToolsCallParams struct {
	Name string `json:"name"`
}

// mcpToolScopes 把 APP 工具名映射到所需 agent 能力(scope)。复用现有 agent
// 能力(scope)体系做授权，无需独立授权表。未登记的工具一律拒绝（与 agent_invoke
// 的 actionRegistry 同策略：未登记=不允许）；新增 APP 工具须在此登记并在
// agentscope 中加入对应 scope，才能被授权使用。
var mcpToolScopes = map[string]string{
	"grix_local_search": agentscope.ScopeAppLocalSearch,
	"grix_open_chat":    agentscope.ScopeAppOpenChat,
	"grix_open_page":    agentscope.ScopeAppOpenPage,
}

// checkToolsCallAuth 对 tools/call 请求做按工具的能力(scope)校验。
// 返回 true 放行，false 拒绝（调用方回 JSON-RPC error）。非 tools/call 帧不拦截。
// 校验复用 ws 层统一的 checkAgentScope（查 agent_api_scope 表）。
func checkToolsCallAuth(sess *mcpSession, frame json.RawMessage) (allowed bool, toolName string) {
	var req mcpJsonRpcRequest
	if err := json.Unmarshal(frame, &req); err != nil {
		return true, "" // 非法帧透传，由 APP MCP Server 处理
	}
	if req.Method != "tools/call" {
		return true, "" // 非 tools/call，不拦截
	}
	var params mcpToolsCallParams
	if err := json.Unmarshal(req.Params, &params); err != nil {
		return true, ""
	}
	toolName = strings.TrimSpace(params.Name)
	if toolName == "" {
		return true, ""
	}
	scope, ok := mcpToolScopes[toolName]
	if !ok {
		return false, toolName // 未登记工具：拒绝
	}
	if err := checkAgentScope(sess.agentID, scope); err != nil {
		return false, toolName // agent 未被授予该能力
	}
	return true, toolName
}

// --- tools/list 响应过滤（闸2）---

// filterToolsListResponse 对 tools/list 响应帧按 agent 能力(scope)过滤工具集：
// 只保留该 agent 已被授予对应 scope、且已登记的工具，其余从列表移除（agent
// 根本看不到未授权工具）。非 tools/list 响应原样透传。
func filterToolsListResponse(sess *mcpSession, frame json.RawMessage) json.RawMessage {
	var probe struct {
		Result *struct {
			Tools []json.RawMessage `json:"tools"`
		} `json:"result"`
	}
	if err := json.Unmarshal(frame, &probe); err != nil || probe.Result == nil || probe.Result.Tools == nil {
		return frame // 非 tools/list 响应，原样透传
	}

	var full map[string]json.RawMessage
	if err := json.Unmarshal(frame, &full); err != nil {
		return frame
	}
	var result map[string]json.RawMessage
	if err := json.Unmarshal(full["result"], &result); err != nil {
		return frame
	}

	filtered := make([]json.RawMessage, 0, len(probe.Result.Tools))
	for _, t := range probe.Result.Tools {
		var meta struct {
			Name string `json:"name"`
		}
		if err := json.Unmarshal(t, &meta); err != nil {
			continue
		}
		scope, ok := mcpToolScopes[strings.TrimSpace(meta.Name)]
		if !ok {
			continue // 未登记工具不暴露
		}
		if err := checkAgentScope(sess.agentID, scope); err != nil {
			continue // agent 无该能力
		}
		filtered = append(filtered, t)
	}

	toolsJSON, err := json.Marshal(filtered)
	if err != nil {
		return frame
	}
	result["tools"] = toolsJSON
	resultJSON, err := json.Marshal(result)
	if err != nil {
		return frame
	}
	full["result"] = resultJSON
	out, err := json.Marshal(full)
	if err != nil {
		return frame
	}
	return out
}

// --- JSON-RPC error 构造 ---

func mcpJsonRpcError(id json.RawMessage, code int, msg string) json.RawMessage {
	resp := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      json.RawMessage(id),
		"error":   map[string]interface{}{"code": code, "message": msg},
	}
	data, _ := json.Marshal(resp)
	return data
}

func extractJsonRpcID(frame json.RawMessage) json.RawMessage {
	var obj struct {
		ID json.RawMessage `json:"id"`
	}
	if err := json.Unmarshal(frame, &obj); err != nil || obj.ID == nil {
		return []byte("null")
	}
	return obj.ID
}

// --- 帧处理入口 ---

func (m *Manager) handleMcpFrame(conn *agentConn, pkt *protocol.Packet) {
	var payload protocol.McpFramePayload
	if err := json.Unmarshal(pkt.Payload, &payload); err != nil {
		conn.sendPayload("error", pkt.Seq, SendNackPayload{Code: 4001, Msg: "invalid mcp_frame payload"})
		return
	}

	// Connector 上行只带业务 session_id（无状态）；后端按 (agentID, sessionID) 幂等映射 mcp_session。
	sessionID := strings.TrimSpace(payload.SessionID)
	if sessionID == "" {
		conn.sendPayload("error", pkt.Seq, SendNackPayload{Code: 4001, Msg: "session_id required"})
		return
	}
	if len(payload.Frame) == 0 {
		return
	}

	// 闸1：会话归属校验
	if !verifyAgentSessionMembership(conn.agentID, sessionID) {
		conn.sendPayload("error", pkt.Seq, SendNackPayload{Code: 4003, Msg: "agent is not a member of this session"})
		return
	}

	sess := m.mcpSessions.getOrCreate(conn.agentID, conn.ownerID, sessionID)
	// 跨节点共享会话元数据（续期 TTL），供 APP 回帧在其它 ws 节点回查路由。
	persistMcpSession(sess)

	// 闸3：tools/call 授权校验
	allowed, toolName := checkToolsCallAuth(sess, payload.Frame)
	if !allowed {
		// 直接回 JSON-RPC error 给 Connector（原样写回 Agent stdout 由其按 id 匹配）
		errFrame := mcpJsonRpcError(extractJsonRpcID(payload.Frame), -32600, fmt.Sprintf("tool %q not authorized for this agent", toolName))
		conn.sendPayload(protocol.CmdMcpFrame, conn.nextSeq(), protocol.McpFramePayload{
			SessionID: sessionID,
			Frame:     errFrame,
		})
		return
	}

	m.forwardMcpFrameToApp(sess, payload.Frame)
}

// forwardMcpFrameToApp 把 MCP 帧下发给 owner 的 Human WS 连接。
// humanWsSendFn 由 ws 层注入，已负责多节点点对点投递（owner 的 APP 连接可能
// 在其它 ws 节点），此处只做封装与下发。
func (m *Manager) forwardMcpFrameToApp(sess *mcpSession, frame json.RawMessage) {
	if sess == nil || len(frame) == 0 {
		return
	}
	outPayload := protocol.McpFramePayload{McpSessionID: sess.id, Frame: frame}
	data, err := json.Marshal(map[string]interface{}{"cmd": protocol.CmdMcpFrame, "payload": outPayload})
	if err != nil {
		return
	}
	if m.humanWsSendFn != nil {
		m.humanWsSendFn(sess.ownerID, data)
	} else {
		logger.L.Warnf("[mcp-session] humanWsSendFn not set id=%s owner=%d", sess.id, sess.ownerID)
	}
}

// HandleMcpFrameFromApp 处理从 APP 来的回帧，转发给对应 Connector。
func (m *Manager) HandleMcpFrameFromApp(userID int64, payload protocol.McpFramePayload) {
	mcpSessID := strings.TrimSpace(payload.McpSessionID)
	if mcpSessID == "" {
		return
	}
	sess := m.mcpSessions.get(mcpSessID)
	if sess == nil {
		// 本节点内存无此会话：可能是会话创建在 agent 所在的另一 ws 节点，
		// 回退到 Redis 共享元数据按 mcp_session_id 还原路由信息。
		sess = loadMcpSession(mcpSessID)
	}
	if sess == nil {
		// mcp_session 已失效（如 agent 重连后换了 mcp_session_id），无法定位 agent。
		// 记录可观测日志而非静默丢弃；agent 侧该 JSON-RPC 请求会按其自身超时收口。
		logger.L.Warnf("[mcp-session] app frame for unknown/expired session id=%s user=%d, drop", mcpSessID, userID)
		return
	}
	if sess.ownerID != userID {
		logger.L.Warnf("[mcp-session] app frame owner mismatch id=%s expect=%d got=%d", mcpSessID, sess.ownerID, userID)
		return
	}

	// 闸2：对 tools/list 响应做可见性过滤
	filteredFrame := filterToolsListResponse(sess, payload.Frame)

	// 按 mcp 会话的 owner 精确回包：共享场景下回到对应被共享者的连接。
	conn := m.lookupConnByOwner(sess.agentID, sess.ownerID)
	if conn == nil {
		// agent 不在本节点：跨节点按 (agentID, ownerID) 路由到该 owner 连接所在的节点;
		// owner 必须严格传(共享场景下 B 的 connector 可能在另一节点,误用主路由会投到主人 A)。
		if m.tryForwardMcpFrameToAgent(sess.agentID, sess.ownerID, sess.sessionID, filteredFrame) {
			return
		}
		logger.L.Warnf("[mcp-session] agent offline, cannot forward id=%s agent=%d owner=%d", mcpSessID, sess.agentID, sess.ownerID)
		return
	}
	conn.sendPayload(protocol.CmdMcpFrame, conn.nextSeq(), protocol.McpFramePayload{
		SessionID: sess.sessionID,
		Frame:     filteredFrame,
	})
}

// CleanupMcpSessionsForAgent 在 agent 全部连接断开时清理其所有 mcp_session（含 Redis 共享元数据）。
// 共享场景下单个连接断开应使用 CleanupMcpSessionsForAgentOwner,避免误清其它 owner 还在用的会话。
func (m *Manager) CleanupMcpSessionsForAgent(agentID int64) {
	ids := m.mcpSessions.removeByAgent(agentID)
	deleteMcpSessionMeta(ids)
}

// CleanupMcpSessionsForAgentOwner 仅清属于 (agentID, ownerID) 的 mcp_session。
// 被共享者 B 断开自己的 WS 连接时调用,只删 B 自己的 mcp 会话元数据,
// 不影响 A 主连接或其它共享者(C/D...)正在用的会话。
func (m *Manager) CleanupMcpSessionsForAgentOwner(agentID, ownerID int64) {
	if agentID <= 0 || ownerID <= 0 {
		return
	}
	ids := m.mcpSessions.removeByAgentOwner(agentID, ownerID)
	deleteMcpSessionMeta(ids)
}
