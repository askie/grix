package service

import (
	"context"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/store"
)

// chatStateListLimit bounds one watch poll. The companion shows an inbox and an
// agent list, not history, so a full page of the newest rows is always enough.
const chatStateListLimit = 200

// ChatStateResp is one chat_states row as the watch companion consumes it:
// the durable run state plus the agent identity needed to render it without a
// second call.
type ChatStateResp struct {
	SessionID string `json:"session_id"`
	AgentID   int64  `json:"agent_id,string"`
	AgentName string `json:"agent_name"`
	// AgentOnline is reported by the connector only. Remote-model agents
	// (provider_type=1) never report presence and are always false, so the
	// client must read it together with AgentProviderType before telling a
	// user their agent is offline.
	AgentOnline       bool   `json:"agent_online"`
	AgentProviderType int16  `json:"agent_provider_type"`
	State             string `json:"state"`
	TaskTitle         string `json:"task_title"`
	LastRunID         string `json:"last_run_id"`
	UpdatedAt         int64  `json:"updated_at"`
}

// WaitingSessionAgentStates are the states in which a run is alive but blocked
// on the owner — the watch inbox.
func WaitingSessionAgentStates() []string {
	return []string{
		model.SessionAgentStateWaitingApproval,
		model.SessionAgentStateWaitingQuestion,
	}
}

// ChatStateList returns the caller's chat_states rows as owner. waitingOnly
// restricts the result to the two waiting states.
func ChatStateList(ctx context.Context, userID int64, waitingOnly bool) ([]ChatStateResp, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	var states []string
	if waitingOnly {
		states = WaitingSessionAgentStates()
	}
	rows, err := store.ListSessionAgentStatesByOwnerStates(userID, states, chatStateListLimit)
	if err != nil {
		return nil, err
	}
	list := make([]ChatStateResp, 0, len(rows))
	if len(rows) == 0 {
		return list, nil
	}

	agentIDs := make([]int64, 0, len(rows))
	seen := make(map[int64]struct{}, len(rows))
	for _, r := range rows {
		if r.AgentID <= 0 {
			continue
		}
		if _, ok := seen[r.AgentID]; ok {
			continue
		}
		seen[r.AgentID] = struct{}{}
		agentIDs = append(agentIDs, r.AgentID)
	}
	agents := make(map[int64]model.Agent, len(agentIDs))
	if len(agentIDs) > 0 {
		var found []model.Agent
		if err := store.DB.WithContext(ctx).
			Where("id IN ? AND owner_id = ? AND status != 3", agentIDs, userID).
			Find(&found).Error; err != nil {
			return nil, err
		}
		for _, a := range found {
			agents[a.ID] = a
		}
	}
	onlineByID := loadAgentOnlineMap(ctx, userID)

	for _, r := range rows {
		a, ok := agents[r.AgentID]
		if !ok {
			// Agent deleted (or never the caller's): no owner action on this
			// session can succeed, so it is not worth a row on the watch.
			continue
		}
		item := ChatStateResp{
			SessionID: r.SessionID,
			AgentID:   r.AgentID,
			State:     r.State,
			TaskTitle: r.TaskTitle,
			LastRunID: r.LastRunID,
			UpdatedAt: r.UpdatedAt.UnixMilli(),
		}
		item.AgentName = a.AgentName
		item.AgentProviderType = a.ProviderType
		item.AgentOnline = onlineByID[r.AgentID]
		list = append(list, item)
	}
	return list, nil
}
