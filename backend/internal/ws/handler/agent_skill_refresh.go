package handler

import (
	"context"
	"encoding/json"
	"strconv"
	"time"

	"github.com/askie/grix/backend/internal/agenttoolbar"
	toolprotocol "github.com/askie/grix/backend/internal/agenttoolbar/protocol"
	apiservice "github.com/askie/grix/backend/internal/api/service"
	"github.com/askie/grix/backend/internal/pkg/logger"
	wsagentapi "github.com/askie/grix/backend/internal/ws/agentapi"
	"github.com/askie/grix/backend/internal/ws/protocol"
)

// HandleAgentSkillRefresh 处理技能弹窗的「下拉刷新」：把刷新请求转成 skill_refresh
// local_action 转发给目标 agent 的 connector/插件，插件重扫本地 skills + 技能库并
// 重新上报（agent_skills_update 先于 local_action_result，同连接保序），后端同步等到
// 结果后重建工具栏快照，随回执一次性带给发起客户端，并通过常规 agent_toolbar_sync
// 推给该 owner 的其它在线客户端。
func HandleAgentSkillRefresh(_ HubInterface, conn ConnInterface, pkt *protocol.Packet) {
	startedAt := time.Now()
	userID := conn.GetUserID()

	var payload protocol.AgentSkillRefreshPayload
	if err := json.Unmarshal(pkt.Payload, &payload); err != nil {
		conn.SendPayload(protocol.CmdAgentSkillRefreshResp, pkt.Seq, protocol.AgentSkillRefreshRespPayload{
			Error: "invalid payload",
		})
		return
	}

	if payload.AgentID == 0 || payload.SessionID == "" {
		conn.SendPayload(protocol.CmdAgentSkillRefreshResp, pkt.Seq, protocol.AgentSkillRefreshRespPayload{
			Error: "agent_id and session_id are required",
		})
		return
	}

	// 授权：与 skill_enable/skill_disable 一致，请求者必须是 agent 主人或有效被共享者。
	if ok, err := apiservice.CanUseAgent(userID, payload.AgentID); err != nil || !ok {
		conn.SendPayload(protocol.CmdAgentSkillRefreshResp, pkt.Seq, protocol.AgentSkillRefreshRespPayload{
			Error: "forbidden",
		})
		return
	}

	mgr := wsagentapi.GetGlobal()
	if mgr == nil {
		conn.SendPayload(protocol.CmdAgentSkillRefreshResp, pkt.Seq, protocol.AgentSkillRefreshRespPayload{
			Error: "service unavailable",
		})
		return
	}

	actorID := strconv.FormatInt(userID, 10)
	seq := pkt.Seq
	agentID := payload.AgentID
	sessionID := payload.SessionID

	// SendSkillRefreshActionAndWait 最长阻塞 15s 等插件回执；与 skill_upload 等命令
	// 一样移出读泵 goroutine 执行。
	go func() {
		resp := protocol.AgentSkillRefreshRespPayload{}
		if err := mgr.SendSkillRefreshActionAndWait(agentID, userID, sessionID, actorID); err != nil {
			resp.Error = err.Error()
		} else if svc := agenttoolbar.GetGlobal(); svc == nil {
			resp.Error = "toolbar service unavailable"
		} else {
			// 插件已先推 agent_skills_update，这里基于最新 runtime profile 重建快照：
			// 一份随回执返回，一份通过 sync 推给其它客户端。
			if err := svc.RefreshSessionForAgent(context.Background(), userID, sessionID, agentID, "skill_refresh"); err != nil {
				resp.Error = err.Error()
			} else if snapshot, err := svc.GetSnapshot(context.Background(), userID, sessionID, agentID); err != nil {
				resp.Error = err.Error()
			} else {
				resp.Snapshot = agenttoolbar.ToWireSnapshot(snapshot)
			}
		}
		if resp.Error != "" && resp.Snapshot.SessionID == "" {
			resp.Snapshot = agenttoolbar.ToWireSnapshot(toolprotocol.Snapshot{
				SessionID: sessionID,
				Items:     []toolprotocol.Item{},
			})
		}
		conn.SendPayload(protocol.CmdAgentSkillRefreshResp, seq, resp)
		logger.L.Infof(
			"[skill-refresh] user=%d agent=%d session=%s waited=%dms err=%q",
			userID, agentID, sessionID, time.Since(startedAt).Milliseconds(), resp.Error,
		)
	}()
}
