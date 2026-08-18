package handler

import (
	"encoding/json"
	"strconv"
	"strings"
	"time"

	apiservice "github.com/askie/grix/backend/internal/api/service"
	"github.com/askie/grix/backend/internal/pkg/logger"
	wsagentapi "github.com/askie/grix/backend/internal/ws/agentapi"
	"github.com/askie/grix/backend/internal/ws/protocol"
)

// HandleAgentSkillDelete 处理工具栏「删除本机技能」：
// 把点击转发给目标 agent 的 connector 删除本地非托管技能目录/文件，同步等待结果后回执。
func HandleAgentSkillDelete(_ HubInterface, conn ConnInterface, pkt *protocol.Packet) {
	startedAt := time.Now()
	userID := conn.GetUserID()

	var payload protocol.AgentSkillDeletePayload
	if err := json.Unmarshal(pkt.Payload, &payload); err != nil {
		conn.SendPayload(protocol.CmdAgentSkillDeleteResp, pkt.Seq, protocol.AgentSkillDeleteRespPayload{
			Error: "invalid payload",
		})
		return
	}

	name := strings.TrimSpace(payload.Name)
	if payload.AgentID == 0 || strings.TrimSpace(payload.SessionID) == "" || name == "" {
		conn.SendPayload(protocol.CmdAgentSkillDeleteResp, pkt.Seq, protocol.AgentSkillDeleteRespPayload{
			Error: "agent_id, session_id and name are required",
		})
		return
	}

	if ok, err := apiservice.CanUseAgent(userID, payload.AgentID); err != nil || !ok {
		conn.SendPayload(protocol.CmdAgentSkillDeleteResp, pkt.Seq, protocol.AgentSkillDeleteRespPayload{
			Error: "forbidden",
		})
		return
	}

	mgr := wsagentapi.GetGlobal()
	if mgr == nil {
		conn.SendPayload(protocol.CmdAgentSkillDeleteResp, pkt.Seq, protocol.AgentSkillDeleteRespPayload{
			Error: "service unavailable",
		})
		return
	}

	actorID := strconv.FormatInt(userID, 10)
	seq := pkt.Seq
	agentID := payload.AgentID
	sessionID := payload.SessionID

	go func() {
		err := mgr.SendSkillDeleteActionAndWait(agentID, userID, sessionID, name, actorID)
		resp := protocol.AgentSkillDeleteRespPayload{Name: name}
		if err != nil {
			resp.Error = err.Error()
		}
		conn.SendPayload(protocol.CmdAgentSkillDeleteResp, seq, resp)
		logger.L.Infof(
			"[skill-delete] user=%d agent=%d session=%s name=%s waited=%dms err=%q",
			userID, agentID, sessionID, name, time.Since(startedAt).Milliseconds(), resp.Error,
		)
	}()
}
