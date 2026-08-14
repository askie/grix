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

// HandleAgentSkillEnable 处理工具栏"技能库启用"（方案 v2）：把点击转发给目标 agent
// 的 connector，在给定 scope（global/project）下把技能库里的一项技能链接进当前生效的
// 技能目录，同步等待结果后回执给发起客户端。
func HandleAgentSkillEnable(_ HubInterface, conn ConnInterface, pkt *protocol.Packet) {
	startedAt := time.Now()
	userID := conn.GetUserID()

	var payload protocol.AgentSkillEnablePayload
	if err := json.Unmarshal(pkt.Payload, &payload); err != nil {
		conn.SendPayload(protocol.CmdAgentSkillEnableResp, pkt.Seq, protocol.AgentSkillEnableRespPayload{
			Error: "invalid payload",
		})
		return
	}

	name := strings.TrimSpace(payload.Name)
	scope := strings.TrimSpace(payload.Scope)
	if payload.AgentID == 0 || strings.TrimSpace(payload.SessionID) == "" || name == "" || scope == "" {
		conn.SendPayload(protocol.CmdAgentSkillEnableResp, pkt.Seq, protocol.AgentSkillEnableRespPayload{
			Name: name, Scope: scope,
			Error: "agent_id, session_id, name and scope are required",
		})
		return
	}

	// 授权：请求者必须是 agent 主人或有效被共享者，否则拒绝（agent 共享多连接物理隔离）。
	if ok, err := apiservice.CanUseAgent(userID, payload.AgentID); err != nil || !ok {
		conn.SendPayload(protocol.CmdAgentSkillEnableResp, pkt.Seq, protocol.AgentSkillEnableRespPayload{
			Name: name, Scope: scope,
			Error: "forbidden",
		})
		return
	}

	mgr := wsagentapi.GetGlobal()
	if mgr == nil {
		conn.SendPayload(protocol.CmdAgentSkillEnableResp, pkt.Seq, protocol.AgentSkillEnableRespPayload{
			Name: name, Scope: scope,
			Error: "service unavailable",
		})
		return
	}

	actorID := strconv.FormatInt(userID, 10)
	seq := pkt.Seq
	agentID := payload.AgentID
	sessionID := payload.SessionID
	force := strings.TrimSpace(payload.Force)

	// SendSkillEnableActionAndWait blocks up to 15s waiting for the connector's reply;
	// run off the read pump like the skill_upload command does.
	go func() {
		result, err := mgr.SendSkillEnableActionAndWait(agentID, userID, sessionID, name, scope, actorID, force)
		resp := protocol.AgentSkillEnableRespPayload{Name: name, Scope: scope}
		if err != nil {
			resp.Error = err.Error()
			if result != nil {
				resp.ConflictKind = result.ConflictKind
			}
		} else if result != nil {
			resp.EnableState = result.EnableState
			resp.Uninstallable = result.Uninstallable
		}
		conn.SendPayload(protocol.CmdAgentSkillEnableResp, seq, resp)
		logger.L.Infof(
			"[skill-enable] user=%d agent=%d session=%s name=%s scope=%s waited=%dms err=%q",
			userID, agentID, sessionID, name, scope, time.Since(startedAt).Milliseconds(), resp.Error,
		)
	}()
}

// HandleAgentSkillDisable 处理工具栏"技能库停用"（方案 v2）：把点击转发给目标 agent
// 的 connector，在给定 scope 下解除技能库里某项技能的启用状态，同步等待结果后回执给
// 发起客户端。与 HandleAgentSkillEnable 是一对镜像动作。
func HandleAgentSkillDisable(_ HubInterface, conn ConnInterface, pkt *protocol.Packet) {
	startedAt := time.Now()
	userID := conn.GetUserID()

	var payload protocol.AgentSkillDisablePayload
	if err := json.Unmarshal(pkt.Payload, &payload); err != nil {
		conn.SendPayload(protocol.CmdAgentSkillDisableResp, pkt.Seq, protocol.AgentSkillDisableRespPayload{
			Error: "invalid payload",
		})
		return
	}

	name := strings.TrimSpace(payload.Name)
	scope := strings.TrimSpace(payload.Scope)
	if payload.AgentID == 0 || strings.TrimSpace(payload.SessionID) == "" || name == "" || scope == "" {
		conn.SendPayload(protocol.CmdAgentSkillDisableResp, pkt.Seq, protocol.AgentSkillDisableRespPayload{
			Name: name, Scope: scope,
			Error: "agent_id, session_id, name and scope are required",
		})
		return
	}

	if ok, err := apiservice.CanUseAgent(userID, payload.AgentID); err != nil || !ok {
		conn.SendPayload(protocol.CmdAgentSkillDisableResp, pkt.Seq, protocol.AgentSkillDisableRespPayload{
			Name: name, Scope: scope,
			Error: "forbidden",
		})
		return
	}

	mgr := wsagentapi.GetGlobal()
	if mgr == nil {
		conn.SendPayload(protocol.CmdAgentSkillDisableResp, pkt.Seq, protocol.AgentSkillDisableRespPayload{
			Name: name, Scope: scope,
			Error: "service unavailable",
		})
		return
	}

	actorID := strconv.FormatInt(userID, 10)
	seq := pkt.Seq
	agentID := payload.AgentID
	sessionID := payload.SessionID

	go func() {
		result, err := mgr.SendSkillDisableActionAndWait(agentID, userID, sessionID, name, scope, actorID)
		resp := protocol.AgentSkillDisableRespPayload{Name: name, Scope: scope}
		if err != nil {
			resp.Error = err.Error()
			if result != nil {
				resp.ConflictKind = result.ConflictKind
			}
		} else if result != nil {
			resp.EnableState = result.EnableState
			resp.Uninstallable = result.Uninstallable
		}
		conn.SendPayload(protocol.CmdAgentSkillDisableResp, seq, resp)
		logger.L.Infof(
			"[skill-disable] user=%d agent=%d session=%s name=%s scope=%s waited=%dms err=%q",
			userID, agentID, sessionID, name, scope, time.Since(startedAt).Milliseconds(), resp.Error,
		)
	}()
}
