package agentapi

import (
	"strings"
	"time"

	"github.com/askie/grix/backend/internal/api/service"
	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/logger"
	"github.com/askie/grix/backend/internal/store"
	"github.com/askie/grix/backend/internal/ws/protocol"
)

// validChatStates is the set of states accepted for manual update.
var validChatStates = map[string]struct{}{
	model.SessionAgentStateRunning:         {},
	model.SessionAgentStateWaitingApproval: {},
	model.SessionAgentStateWaitingQuestion: {},
	model.SessionAgentStateCompleted:       {},
	model.SessionAgentStateFailed:          {},
	model.SessionAgentStateIdle:            {},
}

// dispatchChatStateUpdate handles the chat_state_update invoke action.
// Params: session_id (required), state (required enum), reason (optional → stop_reason).
func dispatchChatStateUpdate(ownerID int64, params map[string]interface{}) (interface{}, int, string) {
	sessionID, _ := paramString(params, "session_id")
	if strings.TrimSpace(sessionID) == "" {
		return nil, 4001, "session_id required"
	}
	state, _ := paramString(params, "state")
	if _, ok := validChatStates[state]; !ok {
		return nil, 4001, "state must be one of: running, waiting_approval, waiting_question, completed, failed, idle"
	}
	reason, _ := paramString(params, "reason")
	found, err := store.ManualUpdateChatState(sessionID, ownerID, state, reason)
	if err != nil {
		return nil, 5000, "update chat state failed"
	}
	if !found {
		return nil, 4004, "chat state record not found for this session"
	}
	return map[string]string{"session_id": sessionID, "state": state}, 0, ""
}

// dispatchChatStateQuery handles the chat_state_query invoke action.
// Params: page (default 1), page_size (default 10, max 100), state (optional filter).
// Each non-terminal row is self-healed before returning.
// Returns (data, errorCode, errorMsg); errorCode==0 means success.
func dispatchChatStateQuery(ownerID, agentID int64, params map[string]interface{}) (interface{}, int, string) {
	sessionID, _ := paramString(params, "session_id")
	page, _ := paramInt(params, "page")
	pageSize, _ := paramInt(params, "page_size")
	state, _ := paramString(params, "state")
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 10
	}
	rows, total, err := store.ListSessionAgentStatesByOwner(ownerID, sessionID, state, page, pageSize)
	if err != nil {
		return nil, 5000, "query session agent states failed"
	}
	m := GetGlobalManager()
	items := make([]protocol.AgentTaskItem, 0, len(rows))
	for _, r := range rows {
		if m != nil {
			r = m.reconcileSessionState(r)
		}
		item := sessionAgentStateToItem(r)
		if strings.TrimSpace(sessionID) != "" && r.State == model.SessionAgentStateCompleted {
			result, resultErr := service.ChatTaskResult(ownerID, r)
			if resultErr != nil {
				return nil, 5000, "query chat task final result failed"
			}
			item.FinalResult = &protocol.AgentTaskFinalResult{Found: result != nil}
			if result != nil {
				item.FinalResult.MsgID = result.MsgID
				item.FinalResult.Content = result.Content
				item.FinalResult.CreatedAt = result.CreatedAt.UnixMilli()
			}
		}
		items = append(items, item)
	}
	return protocol.AgentTaskQueryRespPayload{
		Tasks:    items,
		Total:    total,
		Page:     page,
		PageSize: pageSize,
	}, 0, ""
}

func sessionAgentStateToItem(r model.SessionAgentState) protocol.AgentTaskItem {
	item := protocol.AgentTaskItem{
		SessionID:   r.SessionID,
		AgentID:     r.AgentID,
		State:       r.State,
		TaskTitle:   r.TaskTitle,
		LastRunID:   r.LastRunID,
		StopReason:  r.StopReason,
		UpdatedAtMs: r.UpdatedAt.UnixMilli(),
	}
	if r.StartedAt != nil {
		ms := r.StartedAt.UnixMilli()
		item.StartedAtMs = &ms
	}
	if r.CompletedAt != nil {
		ms := r.CompletedAt.UnixMilli()
		item.CompletedAtMs = &ms
	}
	return item
}

// isNonTerminalSessionState reports whether a state implies a run is supposed to
// be alive — running, or blocked waiting on the owner.
func isNonTerminalSessionState(state string) bool {
	switch state {
	case model.SessionAgentStateRunning,
		model.SessionAgentStateWaitingApproval,
		model.SessionAgentStateWaitingQuestion:
		return true
	}
	return false
}

// sessionRunAlive reports whether a run for this session is still alive anywhere
// — in this node's in-memory tracker, or in the cross-node durable record
// (Redis). A normally-finished run deletes its durable record, so "not alive"
// reliably means the run has ended even if the persisted state still says
// running. The durable record's 48h TTL also reaps a dead agent's residue.
func (m *Manager) sessionRunAlive(ownerID int64, sessionID string, agentID int64) bool {
	if m == nil {
		return false
	}
	if snap := m.LookupActiveRunBySessionOwner(ownerID, sessionID); snap != nil {
		return true
	}
	if agentID > 0 {
		if snap := m.LookupDurableRunBySession(ownerID, sessionID, agentID); snap != nil {
			return true
		}
	}
	return false
}

// reconcileSessionState preserves non-terminal state unless the connector has
// supplied explicit terminal evidence. Missing in-memory/Redis tracking can
// mean bounded metadata retention, a restart, or a cross-node visibility gap;
// none of those proves that execution completed successfully.
func (m *Manager) reconcileSessionState(r model.SessionAgentState) model.SessionAgentState {
	if m == nil || !isNonTerminalSessionState(r.State) {
		return r
	}
	if m.sessionRunAlive(r.OwnerID, r.SessionID, r.AgentID) {
		return r
	}
	return r
}

// ReconcileLeakedSessionStatesOnStartup observes non-terminal rows whose
// ephemeral tracking is absent. It intentionally does not invent a completed
// state: event_result/event_stop_result remain the only terminal authority.
func (m *Manager) ReconcileLeakedSessionStatesOnStartup() {
	if m == nil {
		return
	}
	rows, err := store.ListSessionAgentStatesByState(
		model.SessionAgentStateRunning,
		model.SessionAgentStateWaitingApproval,
		model.SessionAgentStateWaitingQuestion,
	)
	if err != nil {
		logger.L.Warnf("session_agent_state startup reconcile: list failed err=%v", err)
		return
	}
	untracked := 0
	for _, r := range rows {
		if isNonTerminalSessionState(r.State) && !m.sessionRunAlive(r.OwnerID, r.SessionID, r.AgentID) {
			untracked++
		}
	}
	if untracked > 0 {
		logger.L.Warnf(
			"session_agent_state startup reconcile: preserving %d untracked non-terminal row(s) of %d until explicit connector result",
			untracked,
			len(rows),
		)
	}
}

// nowPtr returns a pointer to the current UTC time.
func nowPtr() *time.Time {
	t := time.Now().UTC()
	return &t
}
