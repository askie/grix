package handler

import (
	"context"

	wsagentapi "github.com/askie/grix/backend/internal/ws/agentapi"
)

func ApplyAgentOutputStopLocalEffects(ctx context.Context, run *wsagentapi.ActiveRunSnapshot) {
	if run == nil {
		return
	}
	if run.StreamMsgID > 0 {
		fenceStoppedAgentStream(ctx, run.AgentID, run.StreamMsgID)
		revokeStoppedAgentStream(ctx, run.SessionID, run.StreamMsgID)
	}
}
