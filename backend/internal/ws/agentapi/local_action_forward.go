package agentapi

import (
	"context"
	"encoding/json"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/askie/grix/backend/internal/pkg/logger"
	"github.com/askie/grix/backend/internal/store"
	"github.com/askie/grix/backend/internal/ws/protocol"
)

const (
	redisCmdForwardLocalAction         = "_agent_api_local_action_forward"
	redisCmdForwardLocalActionResponse = "_agent_api_local_action_response"
	forwardedLocalActionTimeout        = 20 * time.Second
)

var forwardedCorrelationSeq atomic.Int64

// forwardedLocalActionRequest is sent from the originating node to the target
// node carrying the local_action payload that should be delivered to the agent.
type forwardedLocalActionRequest struct {
	CorrelationID string `json:"correlation_id"`
	ReplyTo       string `json:"reply_to"`
	AgentID       int64  `json:"agent_id,string"`
	// OwnerID 是请求发起者；目标节点据此按 owner 精确路由到对应连接，
	// 实现 agent 共享多连接的物理隔离。必须 >0；为 0 时目标节点直接报错（fail-closed）。
	OwnerID int64                       `json:"owner_id,string,omitempty"`
	Action  protocol.LocalActionPayload `json:"action"`
}

// forwardedLocalActionResponse is sent back from the target node to the
// originating node carrying the local_action_result from the agent.
type forwardedLocalActionResponse struct {
	CorrelationID string                            `json:"correlation_id"`
	Payload       protocol.LocalActionResultPayload `json:"payload"`
}

// tryForwardLocalActionForOwner 携带请求发起者 ownerID，把 local_action 转发到目标节点，
// 让目标节点按 owner 精确路由到对应连接（agent 共享多连接物理隔离）。
// 路由表已加 owner 维度(loadAgentRouteForOwner)，能在 A 主连接 / B 共享连接散到不同节点时精确选址。
// ownerID<=0 非法，直接失败（fail-closed，不再回退主路由）。
func (m *Manager) tryForwardLocalActionForOwner(agentID, ownerID int64, action protocol.LocalActionPayload, pending *pendingLocalAction) bool {
	if ownerID <= 0 {
		logger.L.Warnf(
			"[file-list-diag] forward !! reject with missing owner agent_id=%d action_id=%s",
			agentID, action.ActionID,
		)
		return false
	}
	if store.RDB == nil {
		logger.L.Warnf(
			"[file-list-diag] forward !! redis unavailable, cannot forward agent_id=%d action_id=%s",
			agentID, action.ActionID,
		)
		return false
	}
	targetNode := loadAgentRouteForOwner(context.Background(), agentID, ownerID)
	logger.L.Infof(
		"[file-list-diag] forward -- route lookup agent_id=%d target_node=%q self_node=%q action_id=%s",
		agentID, targetNode, m.getNodeID(), action.ActionID,
	)
	if targetNode == "" || targetNode == m.getNodeID() {
		logger.L.Warnf(
			"[file-list-diag] forward !! no valid target node agent_id=%d action_id=%s target=%q self=%q",
			agentID, action.ActionID, targetNode, m.getNodeID(),
		)
		return false
	}

	req := forwardedLocalActionRequest{
		CorrelationID: fmt.Sprintf("fwd-la-%d", forwardedCorrelationSeq.Add(1)),
		ReplyTo:       m.getNodeID(),
		AgentID:       agentID,
		OwnerID:       ownerID,
		Action:        action,
	}

	if pending != nil {
		m.storePendingLocalAction(pending)
	}

	data, err := json.Marshal(map[string]any{
		"cmd":     redisCmdForwardLocalAction,
		"payload": req,
	})
	if err != nil {
		logger.L.Warnf("marshal forwarded local_action failed agent=%d action_id=%s err=%v", agentID, action.ActionID, err)
		return false
	}

	channel := fmt.Sprintf("chan:%s", targetNode)
	if err := store.RDB.Publish(context.Background(), channel, data).Err(); err != nil {
		logger.L.Warnf("publish forwarded local_action failed node=%s agent=%d action_id=%s err=%v", targetNode, agentID, action.ActionID, err)
		if pending != nil {
			m.deletePendingLocalAction(action.ActionID)
		}
		return false
	}

	logger.L.Infof(
		"[file-list-diag] forward -> publish ok target_node=%s reply_to=%s correlation=%s agent_id=%d action_id=%s action_type=%s payload_len=%d channel=%s",
		targetNode, req.ReplyTo, req.CorrelationID, agentID, action.ActionID, action.ActionType, len(data), channel,
	)
	return true
}

// handleForwardedLocalActionRequest is called on the TARGET node.  It
// receives the forwarded request, sends the local_action to the locally
// connected agent, waits for the response, and sends it back via Redis.
func (m *Manager) handleForwardedLocalActionRequest(req forwardedLocalActionRequest) {
	logger.L.Infof(
		"[file-list-diag] forward << target recv correlation=%s reply_to=%s agent_id=%d action_id=%s action_type=%s self_node=%s",
		req.CorrelationID, req.ReplyTo, req.AgentID, req.Action.ActionID, req.Action.ActionType, m.getNodeID(),
	)
	// owner 缺失的转发请求直接报错（fail-closed）：绝不允许回退主连接，
	// 否则请求方 A 的 local_action 会被投到共享连接 B 上（本次事故根因）。
	if req.OwnerID <= 0 {
		logger.L.Warnf(
			"[file-list-diag] forward !! reject request with missing owner agent_id=%d action_id=%s correlation=%s",
			req.AgentID, req.Action.ActionID, req.CorrelationID,
		)
		m.sendForwardedLocalActionError(req, "agent_not_connected", "missing owner id on forwarded local_action")
		return
	}
	// 按发起者 owner 精确选连接（agent 共享多连接物理隔离），无对应连接即失败。
	conn := m.lookupConnByOwner(req.AgentID, req.OwnerID)
	if conn == nil {
		logger.L.Warnf(
			"[file-list-diag] forward !! target has no agent conn agent_id=%d owner_id=%d action_id=%s",
			req.AgentID, req.OwnerID, req.Action.ActionID,
		)
		m.sendForwardedLocalActionError(req, "agent_not_connected", "agent is not connected to this node")
		return
	}

	// Channel to capture the local_action_result from the agent.
	ch := make(chan protocol.LocalActionResultPayload, 1)
	pending := &pendingLocalAction{
		actionID:         req.Action.ActionID,
		kind:             "forwarded_local_action",
		agentID:          req.AgentID,
		ownerID:          req.OwnerID,
		actionType:       req.Action.ActionType,
		fileListResultCh: make(chan *fileListResponse, 1),
		// We use a custom handler: when the result arrives, deliver to ch.
		forwardedResultCh: ch,
	}

	m.storePendingLocalAction(pending)

	// 按发起者 owner 路由到对应连接发送，无对应连接即失败（不回退主连接）。
	if !m.SendLocalActionForOwner(req.AgentID, req.OwnerID, req.Action) {
		m.deletePendingLocalAction(req.Action.ActionID)
		logger.L.Warnf(
			"[file-list-diag] forward !! target SendLocalAction failed agent_id=%d action_id=%s",
			req.AgentID, req.Action.ActionID,
		)
		m.sendForwardedLocalActionError(req, "send_failed", "could not send local_action to agent")
		return
	}
	logger.L.Infof(
		"[file-list-diag] forward -> target sent to plugin agent_id=%d action_id=%s",
		req.AgentID, req.Action.ActionID,
	)

	timer := time.NewTimer(forwardedLocalActionTimeout)
	defer timer.Stop()

	startWait := time.Now()
	select {
	case result := <-ch:
		logger.L.Infof(
			"[file-list-diag] forward << target got plugin result agent_id=%d action_id=%s waited=%dms status=%s",
			req.AgentID, req.Action.ActionID, time.Since(startWait).Milliseconds(), result.Status,
		)
		m.sendForwardedLocalActionResponse(req, result)
	case <-m.stopping():
		// 本节点正在关停：连接已被断开，回包不会再来了，别让 Shutdown 干等 20s。
		// 但要先看结果是不是已经到了——关停信号与回包同时就绪时 select 会随机选，
		// 直接放弃就会把一个已经拿到的结果丢掉。
		select {
		case result := <-ch:
			m.sendForwardedLocalActionResponse(req, result)
			return
		default:
		}
		m.deletePendingLocalAction(req.Action.ActionID)
		logger.L.Warnf(
			"[file-list-diag] forward !! target shutting down agent_id=%d action_id=%s waited=%dms",
			req.AgentID, req.Action.ActionID, time.Since(startWait).Milliseconds(),
		)
		m.sendForwardedLocalActionError(req, "node_shutting_down", "target node is shutting down")
	case <-timer.C:
		m.deletePendingLocalAction(req.Action.ActionID)
		logger.L.Warnf(
			"[file-list-diag] forward !! target timeout agent_id=%d action_id=%s waited=%dms",
			req.AgentID, req.Action.ActionID, time.Since(startWait).Milliseconds(),
		)
		m.sendForwardedLocalActionError(req, "timeout", "forwarded local_action timed out")
	}
}

// sendForwardedLocalActionResponse publishes the result back to the originating node.
func (m *Manager) sendForwardedLocalActionResponse(req forwardedLocalActionRequest, payload protocol.LocalActionResultPayload) {
	resp := forwardedLocalActionResponse{
		CorrelationID: req.CorrelationID,
		Payload:       payload,
	}
	data, err := json.Marshal(map[string]any{
		"cmd":     redisCmdForwardLocalActionResponse,
		"payload": resp,
	})
	if err != nil {
		logger.L.Warnf("marshal forwarded local_action response failed correlation=%s err=%v", req.CorrelationID, err)
		return
	}
	if err := store.RDB.Publish(context.Background(), fmt.Sprintf("chan:%s", req.ReplyTo), data).Err(); err != nil {
		logger.L.Warnf("publish forwarded local_action response failed node=%s correlation=%s err=%v", req.ReplyTo, req.CorrelationID, err)
	}
}

func (m *Manager) sendForwardedLocalActionError(req forwardedLocalActionRequest, code, msg string) {
	m.sendForwardedLocalActionResponse(req, protocol.LocalActionResultPayload{
		ActionID:  req.Action.ActionID,
		Status:    "failed",
		ErrorCode: code,
		ErrorMsg:  msg,
	})
}

// handleForwardedLocalActionResponse is called on the ORIGINATING node.
// It receives the response from the target node and delivers it to the
// waiting pending local_action handler.
func (m *Manager) handleForwardedLocalActionResponse(resp forwardedLocalActionResponse) {
	logger.L.Infof(
		"[file-list-diag] forward << origin recv resp correlation=%s action_id=%s status=%s self_node=%s",
		resp.CorrelationID, resp.Payload.ActionID, resp.Payload.Status, m.getNodeID(),
	)
	pending := m.takePendingLocalAction(resp.Payload.ActionID)
	if pending == nil {
		logger.L.Warnf("forwarded local_action response has no pending action_id=%s", resp.Payload.ActionID)
		return
	}

	// If the pending has a forwardedResultCh, deliver there.
	if pending.forwardedResultCh != nil {
		logger.L.Infof(
			"[file-list-diag] forward -> origin deliver to forwardedResultCh action_id=%s kind=%s",
			pending.actionID, pending.kind,
		)
		pending.forwardedResultCh <- resp.Payload
		return
	}

	// Otherwise, process through the normal handler path.
	// The agent is remote so conn may be nil; pass a synthetic conn so
	// reply builders that read adapterID (e.g. session_control) don't panic.
	logger.L.Infof(
		"[file-list-diag] forward -> origin deliver via handlePendingLocalActionResult action_id=%s kind=%s",
		pending.actionID, pending.kind,
	)
	var conn *agentConn
	if mgr := globalManager; mgr != nil {
		conn = mgr.lookupConn(pending.agentID)
	}
	if conn == nil {
		conn = &agentConn{}
	}
	m.handlePendingLocalActionResult(conn, pending, resp.Payload)
}

// HandleForwardedLocalActionDispatch is the top-level entry point called from
// the Redis subscriber when a forwarded local_action command arrives.
func HandleForwardedLocalActionDispatch(cmd string, payload json.RawMessage) bool {
	switch cmd {
	case redisCmdForwardLocalAction:
		var req forwardedLocalActionRequest
		if err := json.Unmarshal(payload, &req); err != nil {
			logger.L.Warnf("decode forwarded local_action request failed: %v", err)
			return true
		}
		globalMu.RLock()
		mgr := globalManager
		globalMu.RUnlock()
		if mgr == nil {
			logger.L.Warnf("drop forwarded local_action request: manager unavailable agent=%d", req.AgentID)
			return true
		}
		// 走 Manager 的后台工作组：跨节点转发的 local_action 会读写 DB，
		// 裸 goroutine 会活过关停。
		mgr.goBackground(func() { mgr.handleForwardedLocalActionRequest(req) })
		return true

	case redisCmdForwardLocalActionResponse:
		var resp forwardedLocalActionResponse
		if err := json.Unmarshal(payload, &resp); err != nil {
			logger.L.Warnf("decode forwarded local_action response failed: %v", err)
			return true
		}
		globalMu.RLock()
		mgr := globalManager
		globalMu.RUnlock()
		if mgr == nil {
			return true
		}
		mgr.handleForwardedLocalActionResponse(resp)
		return true
	}

	return false
}
