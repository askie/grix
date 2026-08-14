package handler

import (
	"encoding/json"
	"strconv"

	"github.com/askie/grix/backend/internal/pkg/logger"
	wsagentapi "github.com/askie/grix/backend/internal/ws/agentapi"
	"github.com/askie/grix/backend/internal/ws/protocol"
)

func HandleAgentSessionBindingsList(_ HubInterface, conn ConnInterface, pkt *protocol.Packet) {
	userID := conn.GetUserID()

	var payload protocol.AgentSessionBindingsListPayload
	if err := json.Unmarshal(pkt.Payload, &payload); err != nil {
		conn.SendPayload(protocol.CmdAgentSessionBindingsListResp, pkt.Seq, protocol.AgentSessionBindingsListRespPayload{
			Error: "invalid payload",
		})
		return
	}

	if payload.AgentID == 0 {
		conn.SendPayload(protocol.CmdAgentSessionBindingsListResp, pkt.Seq, protocol.AgentSessionBindingsListRespPayload{
			Error: "agent_id is required",
		})
		return
	}

	mgr := wsagentapi.GetGlobal()
	if mgr == nil {
		conn.SendPayload(protocol.CmdAgentSessionBindingsListResp, pkt.Seq, protocol.AgentSessionBindingsListRespPayload{
			Error: "service unavailable",
		})
		return
	}

	actorID := strconv.FormatInt(userID, 10)
	seq := pkt.Seq
	agentID := payload.AgentID
	sessionID := payload.SessionID

	go func() {
		// 按请求者 userID 作为 owner 精确路由（agent 共享多连接物理隔离）。
		sessions, err := mgr.SendSessionListActionAndWait(agentID, userID, sessionID, actorID)
		resp := protocol.AgentSessionBindingsListRespPayload{}
		if err != nil {
			logger.L.Warnf("agent_session_bindings_list: failed user_id=%d agent_id=%d err=%v waited=%dms",
				userID, agentID, err, 0)
			resp.Error = err.Error()
		} else {
			resp.Bindings = sessions
		}
		conn.SendPayload(protocol.CmdAgentSessionBindingsListResp, seq, resp)
	}()
}
