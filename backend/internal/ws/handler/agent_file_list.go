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

func HandleAgentFileList(_ HubInterface, conn ConnInterface, pkt *protocol.Packet) {
	startedAt := time.Now()
	userID := conn.GetUserID()
	logger.L.Infof(
		"[file-list-diag] backend << recv user_id=%d seq=%d payload_len=%d",
		userID, pkt.Seq, len(pkt.Payload),
	)

	var payload protocol.AgentFileListPayload
	if err := json.Unmarshal(pkt.Payload, &payload); err != nil {
		logger.L.Warnf(
			"[file-list-diag] backend !! invalid payload user_id=%d seq=%d err=%v raw=%s",
			userID, pkt.Seq, err, string(pkt.Payload),
		)
		conn.SendPayload(protocol.CmdAgentFileListResp, pkt.Seq, protocol.AgentFileListRespPayload{
			Error: "invalid payload",
		})
		return
	}

	parentDesc := "<root>"
	if payload.ParentID != nil {
		parentDesc = *payload.ParentID
	}
	logger.L.Infof(
		"[file-list-diag] backend -- parsed user_id=%d seq=%d agent_id=%d session_id=%s parent_id=%s show_hidden=%v ext_count=%d",
		userID, pkt.Seq, payload.AgentID, payload.SessionID, parentDesc, payload.ShowHidden, len(payload.AllowedExtensions),
	)

	if payload.AgentID == 0 || strings.TrimSpace(payload.SessionID) == "" {
		logger.L.Warnf(
			"[file-list-diag] backend !! missing required user_id=%d seq=%d agent_id=%d session_id=%q",
			userID, pkt.Seq, payload.AgentID, payload.SessionID,
		)
		conn.SendPayload(protocol.CmdAgentFileListResp, pkt.Seq, protocol.AgentFileListRespPayload{
			Error: "agent_id and session_id are required",
		})
		return
	}

	// 授权：请求者必须是 agent 主人或有效被共享者，否则拒绝（agent 共享多连接物理隔离）。
	if ok, err := apiservice.CanUseAgent(userID, payload.AgentID); err != nil || !ok {
		logger.L.Warnf(
			"[file-list-diag] backend !! unauthorized user_id=%d seq=%d agent_id=%d ok=%v err=%v",
			userID, pkt.Seq, payload.AgentID, ok, err,
		)
		conn.SendPayload(protocol.CmdAgentFileListResp, pkt.Seq, protocol.AgentFileListRespPayload{
			Error: "forbidden",
		})
		return
	}

	mgr := wsagentapi.GetGlobal()
	if mgr == nil {
		logger.L.Warnf(
			"[file-list-diag] backend !! agentapi mgr unavailable user_id=%d seq=%d agent_id=%d",
			userID, pkt.Seq, payload.AgentID,
		)
		conn.SendPayload(protocol.CmdAgentFileListResp, pkt.Seq, protocol.AgentFileListRespPayload{
			Error: "service unavailable",
		})
		return
	}

	actorID := strconv.FormatInt(userID, 10)

	// SendFileListActionAndWait blocks up to 15s waiting for agent response.
	// Run in goroutine to avoid blocking the connection's read pump.
	seq := pkt.Seq
	agentID := payload.AgentID
	sessionID := payload.SessionID
	go func() {
		dispatchedAt := time.Now()
		logger.L.Infof(
			"[file-list-diag] backend -> dispatch user_id=%d seq=%d agent_id=%d session_id=%s queue_wait=%dms",
			userID, seq, agentID, sessionID, dispatchedAt.Sub(startedAt).Milliseconds(),
		)
		// 按请求者 userID 作为 owner 精确路由到对应连接（agent 共享多连接物理隔离）。
		files, currentPath, machineName, err := mgr.SendFileListActionAndWait(
			payload.AgentID,
			userID,
			payload.SessionID,
			payload.ParentID,
			payload.ShowHidden,
			payload.AllowedExtensions,
			actorID,
		)
		dur := time.Since(dispatchedAt)
		resp := protocol.AgentFileListRespPayload{}
		if err != nil {
			logger.L.Warnf(
				"[file-list-diag] backend !! agent err user_id=%d seq=%d agent_id=%d session_id=%s waited=%dms err=%v",
				userID, seq, agentID, sessionID, dur.Milliseconds(), err,
			)
			resp.Error = err.Error()
		} else {
			resp.CurrentPath = currentPath
			resp.MachineName = machineName
			for _, f := range files {
				m := map[string]interface{}{
					"id":           f.ID,
					"name":         f.Name,
					"is_directory": f.IsDirectory,
				}
				if f.Size != nil {
					m["size"] = *f.Size
				}
				if f.ModifiedAt != nil {
					m["modified_at"] = *f.ModifiedAt
				}
				if f.MimeType != nil {
					m["mime_type"] = *f.MimeType
				}
				resp.Files = append(resp.Files, m)
			}
			logger.L.Infof(
				"[file-list-diag] backend << agent ok user_id=%d seq=%d agent_id=%d session_id=%s waited=%dms count=%d current_path=%s machine=%s",
				userID, seq, agentID, sessionID, dur.Milliseconds(), len(resp.Files), currentPath, machineName,
			)
		}
		conn.SendPayload(protocol.CmdAgentFileListResp, seq, resp)
		logger.L.Infof(
			"[file-list-diag] backend -> sent resp user_id=%d seq=%d agent_id=%d session_id=%s total=%dms err=%q",
			userID, seq, agentID, sessionID, time.Since(startedAt).Milliseconds(), resp.Error,
		)
	}()
}
