package handler

import (
	"encoding/json"
	"strconv"
	"strings"

	wsagentapi "github.com/askie/grix/backend/internal/ws/agentapi"
	"github.com/askie/grix/backend/internal/ws/protocol"
)

func HandleAgentCreateFolder(_ HubInterface, conn ConnInterface, pkt *protocol.Packet) {
	var payload protocol.AgentCreateFolderPayload
	if err := json.Unmarshal(pkt.Payload, &payload); err != nil {
		conn.SendPayload(protocol.CmdAgentCreateFolderResp, pkt.Seq, protocol.AgentCreateFolderRespPayload{
			Error: "invalid payload",
		})
		return
	}

	if payload.AgentID == 0 || strings.TrimSpace(payload.SessionID) == "" {
		conn.SendPayload(protocol.CmdAgentCreateFolderResp, pkt.Seq, protocol.AgentCreateFolderRespPayload{
			Error: "agent_id and session_id are required",
		})
		return
	}

	name := strings.TrimSpace(payload.Name)
	if name == "" {
		conn.SendPayload(protocol.CmdAgentCreateFolderResp, pkt.Seq, protocol.AgentCreateFolderRespPayload{
			Error: "folder name is required",
		})
		return
	}

	mgr := wsagentapi.GetGlobal()
	if mgr == nil {
		conn.SendPayload(protocol.CmdAgentCreateFolderResp, pkt.Seq, protocol.AgentCreateFolderRespPayload{
			Error: "service unavailable",
		})
		return
	}

	userID := conn.GetUserID()
	actorID := strconv.FormatInt(userID, 10)

	seq := pkt.Seq
	go func() {
		// 按请求者 userID 作为 owner 精确路由（agent 共享多连接物理隔离）。
		folder, err := mgr.SendCreateFolderActionAndWait(payload.AgentID, userID, payload.SessionID, payload.ParentID, name, actorID)
		resp := protocol.AgentCreateFolderRespPayload{}
		if err != nil {
			resp.Error = err.Error()
		} else if folder != nil {
			resp.Folder = map[string]interface{}{
				"id":           folder.ID,
				"name":         folder.Name,
				"is_directory": true,
			}
		}
		conn.SendPayload(protocol.CmdAgentCreateFolderResp, seq, resp)
	}()
}
