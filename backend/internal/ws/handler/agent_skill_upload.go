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

// HandleAgentSkillUpload 处理工具栏"一键上传技能"：
// 把点击转发给目标 agent 的 connector 执行真正的上传，同步等待结果后回执给发起客户端。
func HandleAgentSkillUpload(_ HubInterface, conn ConnInterface, pkt *protocol.Packet) {
	startedAt := time.Now()
	userID := conn.GetUserID()

	var payload protocol.AgentSkillUploadPayload
	if err := json.Unmarshal(pkt.Payload, &payload); err != nil {
		conn.SendPayload(protocol.CmdAgentSkillUploadResp, pkt.Seq, protocol.AgentSkillUploadRespPayload{
			Error: "invalid payload",
		})
		return
	}

	name := strings.TrimSpace(payload.Name)
	if payload.AgentID == 0 || strings.TrimSpace(payload.SessionID) == "" || name == "" {
		conn.SendPayload(protocol.CmdAgentSkillUploadResp, pkt.Seq, protocol.AgentSkillUploadRespPayload{
			Error: "agent_id, session_id and name are required",
		})
		return
	}

	// 授权：请求者必须是 agent 主人或有效被共享者，否则拒绝（agent 共享多连接物理隔离）。
	if ok, err := apiservice.CanUseAgent(userID, payload.AgentID); err != nil || !ok {
		conn.SendPayload(protocol.CmdAgentSkillUploadResp, pkt.Seq, protocol.AgentSkillUploadRespPayload{
			Error: "forbidden",
		})
		return
	}

	mgr := wsagentapi.GetGlobal()
	if mgr == nil {
		conn.SendPayload(protocol.CmdAgentSkillUploadResp, pkt.Seq, protocol.AgentSkillUploadRespPayload{
			Error: "service unavailable",
		})
		return
	}

	actorID := strconv.FormatInt(userID, 10)
	seq := pkt.Seq
	agentID := payload.AgentID
	sessionID := payload.SessionID

	// SendSkillUploadActionAndWait blocks up to 15s waiting for the connector's reply;
	// run off the read pump like the file_list command does.
	go func() {
		err := mgr.SendSkillUploadActionAndWait(agentID, userID, sessionID, name, actorID)
		resp := protocol.AgentSkillUploadRespPayload{Name: name}
		if err != nil {
			resp.Error = err.Error()
		} else {
			resp.SyncState = "synced"
		}
		conn.SendPayload(protocol.CmdAgentSkillUploadResp, seq, resp)
		logger.L.Infof(
			"[skill-upload] user=%d agent=%d session=%s name=%s waited=%dms err=%q",
			userID, agentID, sessionID, name, time.Since(startedAt).Milliseconds(), resp.Error,
		)
	}()
}
