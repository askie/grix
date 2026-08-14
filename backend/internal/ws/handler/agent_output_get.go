package handler

import (
	"encoding/json"
	"strings"
	"time"

	wsagentapi "github.com/askie/grix/backend/internal/ws/agentapi"
	"github.com/askie/grix/backend/internal/ws/protocol"
)

func HandleAgentOutputGet(_ HubInterface, conn ConnInterface, pkt *protocol.Packet) {
	var payload protocol.AgentOutputGetPayload
	if err := json.Unmarshal(pkt.Payload, &payload); err != nil {
		return
	}

	sessionID := strings.TrimSpace(payload.SessionID)
	resp := protocol.AgentOutputGetRespPayload{
		SessionID:  sessionID,
		Active:     false,
		ResolvedAt: time.Now().UnixMilli(),
	}

	mgr := wsagentapi.GetGlobal()
	if mgr == nil || sessionID == "" {
		conn.SendPayload(protocol.CmdAgentOutputGetResp, pkt.Seq, resp)
		return
	}

	status, ok := mgr.LookupActiveRunStatusBySessionOwner(conn.GetUserID(), sessionID)
	if ok {
		resp.Active = true
		resp.Status = &status
		if status.SessionID != "" {
			resp.SessionID = status.SessionID
		}
		if status.UpdatedAt > resp.ResolvedAt {
			resp.ResolvedAt = status.UpdatedAt
		}
	}

	conn.SendPayload(protocol.CmdAgentOutputGetResp, pkt.Seq, resp)
}
