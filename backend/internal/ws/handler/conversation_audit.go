package handler

import (
	"encoding/json"
	"errors"
	"strings"

	"github.com/askie/grix/backend/internal/conversationaudit"
	"github.com/askie/grix/backend/internal/pkg/logger"
	wsagentapi "github.com/askie/grix/backend/internal/ws/agentapi"
	"github.com/askie/grix/backend/internal/ws/protocol"
)

func HandleAuditGetManifest(_ HubInterface, conn ConnInterface, pkt *protocol.Packet) {
	handleAuditQuery(conn, pkt, protocol.CmdAuditGetManifestResp, "audit_get_manifest")
}

func HandleAuditListSpans(_ HubInterface, conn ConnInterface, pkt *protocol.Packet) {
	handleAuditQuery(conn, pkt, protocol.CmdAuditListSpansResp, "audit_list_spans")
}

func HandleAuditGetContentChunk(_ HubInterface, conn ConnInterface, pkt *protocol.Packet) {
	handleAuditQuery(conn, pkt, protocol.CmdAuditGetContentChunkResp, "audit_get_content_chunk")
}

func handleAuditQuery(conn ConnInterface, pkt *protocol.Packet, responseCmd, actionType string) {
	var request protocol.AuditTurnRequest
	if err := json.Unmarshal(pkt.Payload, &request); err != nil {
		sendAuditResponse(conn, responseCmd, pkt.Seq, protocol.AuditTurnResponse{ErrorCode: "AUDIT_INVALID_PARAMS", ErrorMessage: "invalid audit request"})
		return
	}
	request.SessionID = strings.TrimSpace(request.SessionID)
	if request.SessionID == "" || request.MsgID <= 0 || request.AgentID < 0 {
		sendAuditResponse(conn, responseCmd, pkt.Seq, protocol.AuditTurnResponse{ErrorCode: "AUDIT_INVALID_PARAMS", ErrorMessage: "session_id and msg_id are required"})
		return
	}
	if !conversationaudit.FeatureEnabled(conn.GetUserID()) {
		sendAuditResponse(conn, responseCmd, pkt.Seq, protocol.AuditTurnResponse{ErrorCode: "AUDIT_FORBIDDEN", ErrorMessage: "conversation audit is unavailable"})
		return
	}

	turns, err := conversationaudit.ListTurns(conn.GetUserID(), request.SessionID, request.MsgID)
	if err != nil {
		sendAuditResponse(conn, responseCmd, pkt.Seq, protocol.AuditTurnResponse{ErrorCode: "AUDIT_NOT_FOUND", ErrorMessage: "audit turn not found"})
		return
	}
	if request.AgentID == 0 && len(turns) > 1 {
		sendAuditResponse(conn, responseCmd, pkt.Seq, protocol.AuditTurnResponse{
			State:   "selection_required",
			Targets: toAuditTurnTargets(conversationaudit.Targets(turns)),
		})
		return
	}
	agentID := request.AgentID
	if agentID == 0 {
		agentID = turns[0].AgentID
	}
	turn, err := conversationaudit.LookupTurn(conn.GetUserID(), request.SessionID, request.MsgID, agentID)
	if err != nil {
		sendAuditResponse(conn, responseCmd, pkt.Seq, protocol.AuditTurnResponse{ErrorCode: "AUDIT_NOT_FOUND", ErrorMessage: "audit turn not found"})
		return
	}
	base := protocol.AuditTurnResponse{State: turn.State, AuditID: turn.AuditID, TurnID: turn.TurnID, Revision: turn.Revision}
	if turn.State == "failed" {
		base.ErrorCode = firstNonEmpty(turn.ErrorCode, "AUDIT_FAILED")
		base.ErrorMessage = firstNonEmpty(turn.ErrorMessage, "audit capture failed")
		sendAuditResponse(conn, responseCmd, pkt.Seq, base)
		return
	}
	if turn.AuditID == "" || turn.TurnID == "" {
		base.ErrorCode = "AUDIT_NOT_READY"
		base.ErrorMessage = "audit replay is not ready"
		sendAuditResponse(conn, responseCmd, pkt.Seq, base)
		return
	}

	params := map[string]interface{}{
		"session_id": request.SessionID,
		"audit_id":   turn.AuditID,
		"turn_id":    turn.TurnID,
	}
	if request.Revision != nil {
		params["revision"] = *request.Revision
	}
	switch actionType {
	case "audit_list_spans":
		if request.Cursor != "" {
			params["cursor"] = request.Cursor
		}
		if request.Limit > 0 {
			params["limit"] = request.Limit
		}
	case "audit_get_content_chunk":
		if strings.TrimSpace(request.ContentID) == "" {
			base.ErrorCode = "AUDIT_INVALID_PARAMS"
			base.ErrorMessage = "content_id is required"
			sendAuditResponse(conn, responseCmd, pkt.Seq, base)
			return
		}
		params["content_id"] = request.ContentID
		if request.Cursor != "" {
			params["cursor"] = request.Cursor
		}
		if request.MaxBytes > 0 {
			params["max_bytes"] = request.MaxBytes
		}
	}

	mgr := wsagentapi.GetGlobal()
	if mgr == nil {
		base.ErrorCode = "AUDIT_UNAVAILABLE"
		base.ErrorMessage = "audit service is unavailable"
		sendAuditResponse(conn, responseCmd, pkt.Seq, base)
		return
	}
	seq := pkt.Seq
	userID := conn.GetUserID()
	go func() {
		result, err := mgr.SendAuditActionAndWait(turn.AgentID, userID, turn.SessionID, actionType, params)
		if err != nil {
			logger.L.Warnf("audit local action failed user=%d agent=%d action=%s session=%s msg=%d err=%v", userID, turn.AgentID, actionType, turn.SessionID, turn.MsgID, err)
			if errors.Is(err, wsagentapi.ErrAuditNotSupported) {
				base.ErrorCode = "AUDIT_NOT_SUPPORTED"
				base.ErrorMessage = "audit connector does not support replay"
			} else {
				base.ErrorCode = "AUDIT_UNAVAILABLE"
				base.ErrorMessage = "audit connector is unavailable"
			}
			sendAuditResponse(conn, responseCmd, seq, base)
			return
		}
		if result.Status != "ok" {
			base.ErrorCode = firstNonEmpty(result.ErrorCode, "AUDIT_INTERNAL")
			base.ErrorMessage = firstNonEmpty(result.ErrorMsg, "audit query failed")
			sendAuditResponse(conn, responseCmd, seq, base)
			return
		}
		base.Result = result.Result
		sendAuditResponse(conn, responseCmd, seq, base)
	}()
}

func toAuditTurnTargets(targets []conversationaudit.TurnTarget) []protocol.AuditTurnTarget {
	result := make([]protocol.AuditTurnTarget, 0, len(targets))
	for _, target := range targets {
		result = append(result, protocol.AuditTurnTarget{AgentID: target.AgentID, State: target.State, Revision: target.Revision})
	}
	return result
}

func sendAuditResponse(conn ConnInterface, cmd string, seq int64, payload protocol.AuditTurnResponse) {
	conn.SendPayload(cmd, seq, payload)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
