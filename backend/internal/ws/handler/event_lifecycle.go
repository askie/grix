package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/askie/grix/backend/internal/api/service"
	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/logger"
	"github.com/askie/grix/backend/internal/store"
	wsagentapi "github.com/askie/grix/backend/internal/ws/agentapi"
	"github.com/askie/grix/backend/internal/ws/protocol"
)

func HandleEventCancel(_ HubInterface, conn ConnInterface, pkt *protocol.Packet) {
	var payload protocol.EventCancelPayload
	if err := json.Unmarshal(pkt.Payload, &payload); err != nil {
		logger.L.Warnf("event_cancel payload error: %v", err)
		return
	}
	payload.SessionID = strings.TrimSpace(payload.SessionID)
	payload.EventID = strings.TrimSpace(payload.EventID)
	if payload.SessionID == "" || payload.EventID == "" {
		conn.SendPayload(protocol.CmdEventCancelResult, pkt.Seq, map[string]any{
			"event_id": payload.EventID,
			"accepted": false,
			"reason":   "invalid payload",
		})
		return
	}
	if err := service.EnsureHumanSessionAccessible(context.Background(), conn.GetUserID(), payload.SessionID); err != nil {
		conn.SendPayload(protocol.CmdEventCancelResult, pkt.Seq, map[string]any{
			"event_id": payload.EventID,
			"accepted": false,
			"reason":   resolveAgentOutputStopDeniedMessage(err),
		})
		return
	}

	agentID := resolveEventLifecycleAgentID(conn.GetUserID(), payload.SessionID)
	if agentID <= 0 {
		conn.SendPayload(protocol.CmdEventCancelResult, pkt.Seq, map[string]any{
			"event_id": payload.EventID,
			"accepted": false,
			"reason":   "delegate agent not found",
		})
		return
	}

	if !wsagentapi.DispatchEventLifecycleCommand(agentID, conn.GetUserID(), protocol.CmdEventCancel, payload) {
		conn.SendPayload(protocol.CmdEventCancelResult, pkt.Seq, map[string]any{
			"event_id": payload.EventID,
			"accepted": false,
			"reason":   "agent channel unavailable",
		})
		return
	}
}

func HandleQueueClear(_ HubInterface, conn ConnInterface, pkt *protocol.Packet) {
	var payload protocol.QueueClearPayload
	if err := json.Unmarshal(pkt.Payload, &payload); err != nil {
		logger.L.Warnf("queue_clear payload error: %v", err)
		return
	}
	payload.SessionID = strings.TrimSpace(payload.SessionID)
	if payload.SessionID == "" {
		conn.SendPayload(protocol.CmdQueueClearResult, pkt.Seq, map[string]any{
			"session_id": payload.SessionID,
			"success":    false,
			"msg":        "invalid payload",
		})
		return
	}
	if err := service.EnsureHumanSessionAccessible(context.Background(), conn.GetUserID(), payload.SessionID); err != nil {
		conn.SendPayload(protocol.CmdQueueClearResult, pkt.Seq, map[string]any{
			"session_id": payload.SessionID,
			"success":    false,
			"msg":        resolveAgentOutputStopDeniedMessage(err),
		})
		return
	}

	agentID := resolveEventLifecycleAgentID(conn.GetUserID(), payload.SessionID)
	if agentID <= 0 {
		conn.SendPayload(protocol.CmdQueueClearResult, pkt.Seq, map[string]any{
			"session_id": payload.SessionID,
			"success":    false,
			"msg":        "delegate agent not found",
		})
		return
	}

	if !wsagentapi.DispatchEventLifecycleCommand(agentID, conn.GetUserID(), protocol.CmdQueueClear, payload) {
		conn.SendPayload(protocol.CmdQueueClearResult, pkt.Seq, map[string]any{
			"session_id": payload.SessionID,
			"success":    false,
			"msg":        "agent channel unavailable",
		})
		return
	}
}

// HandleQueueReorder 接收前端的队列重排请求，校验会话权限后原样透传给
// 会话对应的 agent。队列权威在 agent（connector）侧：agent 按愿望清单语义
// 应用重排后回 queue_reorder_result + 权威 queue_snapshot，走既有转发链路
// 下发给 owner 的所有前端连接。
func HandleQueueReorder(_ HubInterface, conn ConnInterface, pkt *protocol.Packet) {
	var payload protocol.QueueReorderPayload
	if err := json.Unmarshal(pkt.Payload, &payload); err != nil {
		logger.L.Warnf("queue_reorder payload error: %v", err)
		return
	}
	payload.SessionID = strings.TrimSpace(payload.SessionID)
	orderedIDs := make([]string, 0, len(payload.OrderedEventIDs))
	for _, id := range payload.OrderedEventIDs {
		if trimmed := strings.TrimSpace(id); trimmed != "" {
			orderedIDs = append(orderedIDs, trimmed)
		}
	}
	payload.OrderedEventIDs = orderedIDs
	if payload.SessionID == "" || len(payload.OrderedEventIDs) == 0 {
		conn.SendPayload(protocol.CmdQueueReorderResult, pkt.Seq, map[string]any{
			"session_id": payload.SessionID,
			"success":    false,
			"msg":        "invalid payload",
		})
		return
	}
	if err := service.EnsureHumanSessionAccessible(context.Background(), conn.GetUserID(), payload.SessionID); err != nil {
		conn.SendPayload(protocol.CmdQueueReorderResult, pkt.Seq, map[string]any{
			"session_id": payload.SessionID,
			"success":    false,
			"msg":        resolveAgentOutputStopDeniedMessage(err),
		})
		return
	}

	agentID := resolveEventLifecycleAgentID(conn.GetUserID(), payload.SessionID)
	if agentID <= 0 {
		conn.SendPayload(protocol.CmdQueueReorderResult, pkt.Seq, map[string]any{
			"session_id": payload.SessionID,
			"success":    false,
			"msg":        "delegate agent not found",
		})
		return
	}

	if !wsagentapi.DispatchEventLifecycleCommand(agentID, conn.GetUserID(), protocol.CmdQueueReorder, payload) {
		conn.SendPayload(protocol.CmdQueueReorderResult, pkt.Seq, map[string]any{
			"session_id": payload.SessionID,
			"success":    false,
			"msg":        "agent channel unavailable",
		})
		return
	}
}

// HandleEventHold 接收前端的排队任务暂停/恢复请求，校验会话权限后原样透传给
// 会话对应的 agent。hold 权威在 agent（connector）侧：agent 应用后回
// event_hold_result（ok/held/error）走既有转发链路下发给 owner 的所有前端连接。
// 失败在本层直接回 event_hold_result 错误包（ok=false）。
func HandleEventHold(_ HubInterface, conn ConnInterface, pkt *protocol.Packet) {
	var payload protocol.EventHoldPayload
	if err := json.Unmarshal(pkt.Payload, &payload); err != nil {
		logger.L.Warnf("event_hold payload error: %v", err)
		return
	}
	payload.SessionID = strings.TrimSpace(payload.SessionID)
	payload.EventID = strings.TrimSpace(payload.EventID)
	sendErr := func(errMsg string) {
		conn.SendPayload(protocol.CmdEventHoldResult, pkt.Seq, map[string]any{
			"session_id": payload.SessionID,
			"event_id":   payload.EventID,
			"ok":         false,
			"held":       false,
			"error":      errMsg,
		})
	}
	if payload.SessionID == "" || payload.EventID == "" {
		sendErr("bad_request")
		return
	}
	if err := service.EnsureHumanSessionAccessible(context.Background(), conn.GetUserID(), payload.SessionID); err != nil {
		sendErr(resolveAgentOutputStopDeniedMessage(err))
		return
	}

	agentID := resolveEventLifecycleAgentID(conn.GetUserID(), payload.SessionID)
	if agentID <= 0 {
		sendErr("delegate agent not found")
		return
	}

	if !wsagentapi.DispatchEventLifecycleCommand(agentID, conn.GetUserID(), protocol.CmdEventHold, payload) {
		sendErr("agent channel unavailable")
		return
	}
}

// HandleQueueEdit 接收前端的排队任务文本改写请求，校验会话权限后原样透传给
// 会话对应的 agent。agent 仅命中 queued[] 中的项：改写全文、重建预览、自动解除
// 该任务的 hold，回 queue_edit_result 并紧跟推权威 queue_snapshot。
// 失败在本层直接回 queue_edit_result 错误包（ok=false）。
func HandleQueueEdit(_ HubInterface, conn ConnInterface, pkt *protocol.Packet) {
	var payload protocol.QueueEditPayload
	if err := json.Unmarshal(pkt.Payload, &payload); err != nil {
		logger.L.Warnf("queue_edit payload error: %v", err)
		return
	}
	payload.SessionID = strings.TrimSpace(payload.SessionID)
	payload.EventID = strings.TrimSpace(payload.EventID)
	sendErr := func(errMsg string) {
		conn.SendPayload(protocol.CmdQueueEditResult, pkt.Seq, map[string]any{
			"session_id": payload.SessionID,
			"event_id":   payload.EventID,
			"ok":         false,
			"error":      errMsg,
		})
	}
	if payload.SessionID == "" || payload.EventID == "" {
		sendErr("bad_request")
		return
	}
	if strings.TrimSpace(payload.Content) == "" {
		sendErr("empty_content")
		return
	}
	if err := service.EnsureHumanSessionAccessible(context.Background(), conn.GetUserID(), payload.SessionID); err != nil {
		sendErr(resolveAgentOutputStopDeniedMessage(err))
		return
	}

	agentID := resolveEventLifecycleAgentID(conn.GetUserID(), payload.SessionID)
	if agentID <= 0 {
		sendErr("delegate agent not found")
		return
	}

	if !wsagentapi.DispatchEventLifecycleCommand(agentID, conn.GetUserID(), protocol.CmdQueueEdit, payload) {
		sendErr("agent channel unavailable")
		return
	}
}

// HandleQueueSnapshotQuery 接收前端主动发起的"拉一次队列快照"请求，
// 把这条 query 透传给会话对应的 agent；agent 端会回一条 queue_snapshot
// 走原有的 queue_snapshot 转发链路下发给所有该 owner 的前端连接。
//
// 设计意图：push 通道（onStateChange / 事件结束时主动 push snapshot）
// 在 connector 进程重启、idle evict、客户端短暂离线等边界场景会丢消息，
// 导致前端本地队列状态与 agent 实际状态去同步化。query/pull 是这条
// 通道的兜底——前端在合适时机主动问一次，服务端回当前真实状态。
// agent 不在线或 session 没有 slot 时，agent 仍然会回空 snapshot，
// 让前端能据此清掉本地残留。
func HandleQueueSnapshotQuery(_ HubInterface, conn ConnInterface, pkt *protocol.Packet) {
	var payload struct {
		SessionID string `json:"session_id"`
	}
	if err := json.Unmarshal(pkt.Payload, &payload); err != nil {
		logger.L.Warnf("queue_snapshot_query payload error: %v", err)
		return
	}
	payload.SessionID = strings.TrimSpace(payload.SessionID)
	if payload.SessionID == "" {
		return
	}
	if err := service.EnsureHumanSessionAccessible(context.Background(), conn.GetUserID(), payload.SessionID); err != nil {
		logger.L.Warnf("queue_snapshot_query access denied user=%d session=%s: %v", conn.GetUserID(), payload.SessionID, err)
		return
	}

	agentID := resolveEventLifecycleAgentID(conn.GetUserID(), payload.SessionID)
	if agentID <= 0 {
		// 没有可路由的 agent —— 直接回空 snapshot，让前端清掉本地残留
		conn.SendPayload(protocol.CmdQueueSnapshot, pkt.Seq, map[string]any{
			"session_id":    payload.SessionID,
			"running":       []string{},
			"running_items": []any{},
			"queued":        []any{},
		})
		return
	}

	if !wsagentapi.DispatchEventLifecycleCommand(agentID, conn.GetUserID(), protocol.CmdQueueSnapshotQuery, payload) {
		// agent 不在线 —— 同样回空 snapshot
		conn.SendPayload(protocol.CmdQueueSnapshot, pkt.Seq, map[string]any{
			"session_id":    payload.SessionID,
			"running":       []string{},
			"running_items": []any{},
			"queued":        []any{},
		})
		return
	}
}

func resolveEventLifecycleAgentID(ownerID int64, sessionID string) int64 {
	sid := strings.TrimSpace(sessionID)
	if ownerID <= 0 || sid == "" {
		return 0
	}

	if aid := resolveDelegateAgentID(ownerID, sid); aid > 0 {
		return aid
	}
	if mgr := wsagentapi.GetGlobal(); mgr != nil {
		if run := mgr.LookupActiveRunBySessionOwner(ownerID, sid); run != nil && run.AgentID > 0 {
			return run.AgentID
		}
	}
	if store.DB == nil {
		return 0
	}

	var member model.SessionMember
	if err := store.DB.
		Where("session_id = ? AND member_type = 2", sid).
		Order("joined_at ASC").
		Take(&member).Error; err == nil && member.MemberID > 0 {
		return member.MemberID
	}
	return 0
}

func resolveDelegateAgentID(ownerID int64, sessionID string) int64 {
	if store.RDB == nil {
		return 0
	}
	key := fmt.Sprintf("im:delegate:%s:%d", sessionID, ownerID)
	raw, err := store.RDB.HGet(context.Background(), key, "agent_id").Result()
	if err != nil {
		return 0
	}
	agentID, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
	if err != nil {
		return 0
	}
	return agentID
}
