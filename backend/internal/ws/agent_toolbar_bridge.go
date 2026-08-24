package ws

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	agenttoolbar "github.com/askie/grix/backend/internal/agenttoolbar"
	"github.com/askie/grix/backend/internal/agenttoolbar/core"
	toolruntime "github.com/askie/grix/backend/internal/agenttoolbar/runtime"
	"github.com/askie/grix/backend/internal/pkg/logger"
	"github.com/askie/grix/backend/internal/pkg/snowflake"
	"github.com/askie/grix/backend/internal/store"
	wsagentapi "github.com/askie/grix/backend/internal/ws/agentapi"
	"github.com/askie/grix/backend/internal/ws/handler"
	"github.com/askie/grix/backend/internal/ws/protocol"
)

type agentToolbarActiveRunProvider struct {
	mgr *wsagentapi.Manager
}

func (p agentToolbarActiveRunProvider) LoadRunState(ctx context.Context, ownerID int64, sessionID string, agentID int64) toolruntime.RunState {
	if p.mgr == nil {
		return toolruntime.RunState{}
	}
	snapshot := p.mgr.LookupActiveRunBySessionOwner(ownerID, sessionID)
	if snapshot != nil {
		logger.L.Debugf(
			"[queue-debug] LoadRunState owner=%d session=%s agent=%d source=active_run has_active=true event=%s state=%s can_stop=%v",
			ownerID, sessionID, agentID, snapshot.EventID, snapshot.State, snapshot.CanStop,
		)
		return toolruntime.RunState{
			HasActiveRun: true,
			RunID:        snapshot.EventID,
			State:        snapshot.State,
			CanStop:      snapshot.CanStop,
			StreamMsgID:  snapshot.StreamMsgID,
			TriggerMsgID: snapshot.TriggerMsgID,
			AgentID:      snapshot.AgentID,
			UpdatedAt:    snapshot.UpdatedAt,
		}
	}
	// 本节点无 in-memory run：跨节点/重启场景下从 Redis durable 只读出真实 run
	// （含 event_id），使前端在非 agent 节点也能解析出可停止的 run，避免降级成
	// 无 event_id 的 composing-only 停止。run 权威留在 agent 节点，这里只读不重建。
	if durableSnap := p.mgr.LookupDurableRunBySession(ownerID, sessionID, agentID); durableSnap != nil {
		runState := durableSnap.State
		canStop := durableSnap.CanStop
		// 跨节点停止被接受后，durable 快照的 State 是硬编码的 streaming，无法反映
		// 停止请求。检查本节点的跨节点停止标记，若匹配则覆盖为 stopping，使
		// RefreshSession 看到状态变化并立即推工具栏快照，清除前端 loading。
		if p.mgr.IsCrossNodeStopPending(ownerID, sessionID, durableSnap.EventID) {
			runState = protocol.AgentOutputStateStopping
			canStop = false
			logger.L.Infof(
				"[queue-debug] LoadRunState owner=%d session=%s agent=%d source=durable+cross_node_stop has_active=true event=%s state=stopping",
				ownerID, sessionID, agentID, durableSnap.EventID,
			)
		} else {
			logger.L.Infof(
				"[queue-debug] LoadRunState owner=%d session=%s agent=%d source=durable has_active=true event=%s",
				ownerID, sessionID, agentID, durableSnap.EventID,
			)
		}
		return toolruntime.RunState{
			HasActiveRun: true,
			RunID:        durableSnap.EventID,
			State:        runState,
			CanStop:      canStop,
			StreamMsgID:  durableSnap.StreamMsgID,
			TriggerMsgID: durableSnap.TriggerMsgID,
			AgentID:      durableSnap.AgentID,
			UpdatedAt:    durableSnap.UpdatedAt,
		}
	}
	// Fallback: when no active run is tracked but the agent has an active
	// composing activity, synthesize a RunState so the toolbar shows the
	// stop button.  This covers agents (like Kiro) that continue sending
	// tool-call messages after event_result has cleared the tracked run.
	hasComposing := agentID > 0 && handler.HasAgentComposingActivity(ctx, sessionID, agentID)
	if hasComposing {
		logger.L.Debugf(
			"[queue-debug] LoadRunState owner=%d session=%s agent=%d source=composing has_active=true",
			ownerID, sessionID, agentID,
		)
		return toolruntime.RunState{
			HasActiveRun: true,
			State:        "composing",
			CanStop:      true,
			AgentID:      agentID,
			UpdatedAt:    time.Now().UnixMilli(),
		}
	}
	logger.L.Debugf(
		"[queue-debug] LoadRunState owner=%d session=%s agent=%d source=none has_active=false (active_run=nil composing=false)",
		ownerID, sessionID, agentID,
	)
	return toolruntime.RunState{}
}

type agentToolbarExecutor struct {
	mgr *wsagentapi.Manager
}

func (e agentToolbarExecutor) DispatchLocalAction(_ context.Context, req core.LocalActionRequest) error {
	if e.mgr == nil {
		return errors.New("agent api manager unavailable")
	}
	actionID := fmt.Sprintf("toolbar:%d", snowflake.GenID())
	referenceID := extractToolbarReferenceID(req.ActionType, req.Params)
	displayLabel := extractToolbarDisplayLabel(req.Params)
	if !e.mgr.DispatchToolbarLocalAction(wsagentapi.ToolbarLocalActionRequest{
		ActionID:     actionID,
		ActionType:   req.ActionType,
		OwnerID:      req.OwnerID,
		AgentID:      req.AgentID,
		SessionID:    req.SessionID,
		ReferenceID:  referenceID,
		DisplayLabel: displayLabel,
		Params:       req.Params,
		TimeoutMs:    req.TimeoutMs,
	}) {
		return errors.New("agent channel unavailable")
	}
	return nil
}

func extractToolbarReferenceID(actionType string, params map[string]any) string {
	switch actionType {
	case "session_control":
		if verb, ok := params["verb"].(string); ok {
			return verb
		}
	case "set_model":
		if modelID, ok := params["model_id"].(string); ok {
			return modelID
		}
	case "set_mode":
		if modeID, ok := params["mode_id"].(string); ok {
			return modeID
		}
	case "set_preset":
		if presetID, ok := params["agent_preset_id"].(string); ok && strings.TrimSpace(presetID) != "" {
			return presetID
		}
		if presetID, ok := params["preset_id"].(string); ok {
			return presetID
		}
	case "set_reasoning_effort":
		// Claude uses effort as the canonical parameter; keep the legacy
		// reasoning_effort spelling for older callers/connectors.
		if effort, ok := params["effort"].(string); ok && strings.TrimSpace(effort) != "" {
			return effort
		}
		if effort, ok := params["reasoning_effort"].(string); ok {
			return effort
		}
	case "set_service_tier":
		if tier, ok := params["service_tier"].(string); ok {
			return tier
		}
	case "set_profile", "create_profile":
		if profileID, ok := params["profile_id"].(string); ok {
			return profileID
		}
	}
	return ""
}

func extractToolbarDisplayLabel(params map[string]any) string {
	if params == nil {
		return ""
	}
	label, _ := params["display_label"].(string)
	return label
}

// clearComposingStateImmediately clears every active agent-API sourced composing
// activity for the session as soon as the user presses the toolbar stop button.
// This is the single, backend-side mechanism shared by all agents; it runs
// before any agent-specific stop dispatch so the composing indicator disappears
// immediately even if the downstream stop request fails or is slow.
func (e agentToolbarExecutor) clearComposingStateImmediately(ctx context.Context, sessionID string) {
	if ctx == nil {
		ctx = context.Background()
	}
	_ = handler.ClearAgentComposingActivityBySession(ctx, nil, sessionID)
}

func (e agentToolbarExecutor) ClearComposingState(ctx context.Context, req core.StopOutputRequest) error {
	if ctx == nil {
		ctx = context.Background()
	}
	return handler.ClearAgentComposingActivityBySession(ctx, nil, req.SessionID)
}

func (e agentToolbarExecutor) StopOutput(ctx context.Context, req core.StopOutputRequest) error {
	if e.mgr == nil {
		return errors.New("agent api manager unavailable")
	}
	logger.L.Infof(
		"[stop-trace] toolbar StopOutput enter owner=%d session=%s agent=%d run_id=%s",
		req.OwnerID, req.SessionID, req.AgentID, strings.TrimSpace(req.RunID),
	)
	// 用户点击停止按钮后，第一时间在 backend 清除 composing 状态，对所有 agent 统一处理。
	e.clearComposingStateImmediately(ctx, req.SessionID)
	// Composing-only stop: no real run tracked, but agent has composing activity.
	if strings.TrimSpace(req.RunID) == "" {
		logger.L.Infof(
			"[stop-trace] toolbar StopOutput composing-only session=%s agent=%d (no tracked run)",
			req.SessionID, req.AgentID,
		)
		return e.stopComposingOnly(ctx, req)
	}
	ack, run, err := e.mgr.RequestOutputStop(req.OwnerID, req.SessionID, req.RunID)
	if err != nil {
		logger.L.Warnf(
			"[stop-trace] toolbar StopOutput RequestOutputStop failed session=%s run_id=%s accepted=%t msg=%s err=%v",
			req.SessionID, strings.TrimSpace(req.RunID), ack.Accepted, ack.Msg, err,
		)
		if ack.Msg != "" {
			return errors.New(ack.Msg)
		}
		return err
	}
	if run == nil {
		logger.L.Warnf(
			"[stop-trace] toolbar StopOutput active run not found session=%s run_id=%s",
			req.SessionID, strings.TrimSpace(req.RunID),
		)
		return errors.New("active run not found")
	}
	logger.L.Infof(
		"[stop-trace] toolbar StopOutput accepted session=%s run_id=%s stop_id=%s agent=%d -> dispatch event_stop",
		req.SessionID, run.EventID, strings.TrimSpace(ack.StopID), run.AgentID,
	)
	handler.ApplyAgentOutputStopLocalEffects(context.Background(), run)
	if err := e.mgr.DispatchOutputStop(ack, run); err != nil {
		logger.L.Warnf(
			"[stop-trace] toolbar StopOutput DispatchOutputStop failed session=%s run_id=%s stop_id=%s err=%v",
			req.SessionID, run.EventID, strings.TrimSpace(ack.StopID), err,
		)
		if ack.Msg != "" {
			return errors.New(ack.Msg)
		}
		return err
	}
	logger.L.Infof(
		"[stop-trace] toolbar StopOutput dispatched event_stop session=%s run_id=%s stop_id=%s agent=%d",
		req.SessionID, run.EventID, strings.TrimSpace(ack.StopID), run.AgentID,
	)
	return nil
}

func (e agentToolbarExecutor) stopComposingOnly(ctx context.Context, req core.StopOutputRequest) error {
	if ctx == nil {
		ctx = context.Background()
	}
	agentID := req.AgentID
	if agentID <= 0 {
		return errors.New("agent not found")
	}
	logger.L.Infof(
		"toolbar composing-only stop session=%s owner=%d agent=%d",
		req.SessionID, req.OwnerID, agentID,
	)
	// Best-effort: send event_stop to the agent so it can interrupt work.
	e.mgr.DispatchComposingStop(agentID, buildComposingStopPayload(req, agentID, time.Now()))
	return nil
}

func buildComposingStopPayload(req core.StopOutputRequest, agentID int64, now time.Time) protocol.AgentEventStopPayload {
	return protocol.AgentEventStopPayload{
		StopID:      fmt.Sprintf("composing_stop_%d", now.UnixNano()),
		SessionID:   req.SessionID,
		Scope:       protocol.AgentEventStopScopeSession,
		OwnerID:     req.OwnerID,
		AgentID:     agentID,
		Reason:      "owner_requested_stop",
		RequestedAt: now.UnixMilli(),
	}
}

// SendStopText 是 Hermes 工具栏停止的分叉实现：
// 1. 先发 /stop 文本命令（让 Hermes 框架优雅停：清理工具状态、中断 AI 生成）
// 2. 再发 event_stop 协议命令（闭环生命周期：ack + stop_result → MarkRunStopped）
// 两条命令互补：/stop 负责业务层停止，event_stop 负责协议层闭环。
func (e agentToolbarExecutor) SendStopText(ctx context.Context, req core.StopOutputRequest) error {
	if e.mgr == nil {
		return errors.New("agent api manager unavailable")
	}
	if ctx == nil {
		ctx = context.Background()
	}

	logger.L.Infof(
		"[toolbar-stop] SendStopText start owner=%d session=%s agent=%d run_id=%s",
		req.OwnerID, req.SessionID, req.AgentID, req.RunID,
	)

	// 用户点击停止按钮后，第一时间在 backend 清除 composing 状态，对所有 agent 统一处理。
	e.clearComposingStateImmediately(ctx, req.SessionID)

	tracked := strings.TrimSpace(req.RunID) != ""
	var ack protocol.AgentOutputStopAckPayload
	var run *wsagentapi.ActiveRunSnapshot
	if tracked {
		// RequestOutputStop 失败（如 run 已不存在）不阻断后续 /stop 下发。
		var err error
		ack, run, err = e.mgr.RequestOutputStop(req.OwnerID, req.SessionID, req.RunID)
		if err != nil {
			logger.L.Warnf(
				"[toolbar-stop] RequestOutputStop failed owner=%d session=%s run_id=%s err=%v",
				req.OwnerID, req.SessionID, req.RunID, err,
			)
		} else if run != nil {
			handler.ApplyAgentOutputStopLocalEffects(ctx, run)
			logger.L.Infof(
				"[toolbar-stop] RequestOutputStop ok run_id=%s accepted=%t stop_id=%s stream_msg_id=%d",
				run.EventID, ack.Accepted, ack.StopID, run.StreamMsgID,
			)
		}
	}

	// 第一步：发 /stop 文本，让 Hermes 框架走 _dispatch_active_session_command 优雅停止。
	if !e.mgr.DispatchOwnerCommandText(req.AgentID, req.OwnerID, req.SessionID, "/stop") {
		logger.L.Warnf(
			"[toolbar-stop] DispatchOwnerCommandText(/stop) failed owner=%d session=%s agent=%d",
			req.OwnerID, req.SessionID, req.AgentID,
		)
		if tracked {
			e.mgr.MarkRunFailed(req.RunID, protocol.AgentDeliveryCodeChannelUnavailable)
		}
		return errors.New("agent channel unavailable")
	}
	logger.L.Infof(
		"[toolbar-stop] /stop text dispatched owner=%d session=%s agent=%d",
		req.OwnerID, req.SessionID, req.AgentID,
	)

	// 第二步：发 event_stop，让插件回 event_stop_ack + event_stop_result，闭环 run 生命周期。
	if tracked && run != nil {
		if err := e.mgr.DispatchOutputStop(ack, run); err != nil {
			// event_stop 发送失败不影响已下发的 /stop，但 run 生命周期可能靠超时关闭。
			logger.L.Warnf("[toolbar-stop] event_stop dispatch failed run_id=%s: %v", req.RunID, err)
		} else {
			logger.L.Infof(
				"[toolbar-stop] event_stop dispatched run_id=%s stop_id=%s agent=%d",
				run.EventID, ack.StopID, run.AgentID,
			)
		}
	} else if tracked {
		logger.L.Warnf(
			"[toolbar-stop] skipping event_stop dispatch: run=nil tracked=%t owner=%d session=%s run_id=%s",
			tracked, req.OwnerID, req.SessionID, req.RunID,
		)
	}

	return nil
}

func (s *Server) initAgentToolbarService() {
	if s == nil {
		return
	}
	agenttoolbar.SetGlobal(agenttoolbar.NewService(agenttoolbar.Dependencies{
		Fanout:            s.fanoutUserPacket,
		Executor:          agentToolbarExecutor{mgr: s.agentAPIMgr},
		ActiveRunProvider: agentToolbarActiveRunProvider{mgr: s.agentAPIMgr},
	}))
}

func (s *Server) fanoutUserPacket(ctx context.Context, ownerID int64, cmd string, payload any) {
	if s == nil || ownerID <= 0 {
		return
	}
	for _, conn := range s.hub.GetUserConns(ownerID) {
		conn.SendPayload(cmd, conn.NextSeq(), payload)
	}
	if store.RDB == nil {
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}
	data, err := json.Marshal(map[string]any{
		"user_id": ownerID,
		"cmd":     cmd,
		"payload": payload,
	})
	if err != nil {
		return
	}
	routeKey := fmt.Sprintf("im:ws:route:%d", ownerID)
	devices, err := store.RDB.HGetAll(ctx, routeKey).Result()
	if err != nil {
		return
	}
	publishedNodes := make(map[string]bool)
	for _, nodeID := range devices {
		if nodeID == s.nodeID || publishedNodes[nodeID] {
			continue
		}
		publishedNodes[nodeID] = true
		if err := store.RDB.Publish(ctx, fmt.Sprintf("chan:%s", nodeID), string(data)).Err(); err != nil {
			logger.L.Warnf("publish cross-node event to %s failed: %v", nodeID, err)
		}
	}
}
