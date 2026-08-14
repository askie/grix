package agentapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/askie/grix/backend/internal/conversationaudit"
	"github.com/askie/grix/backend/internal/pkg/logger"
	"github.com/askie/grix/backend/internal/pkg/snowflake"
	"github.com/askie/grix/backend/internal/ws/protocol"
)

const auditLocalActionTimeoutMs = 12_000

var auditReplayLocalActions = []string{
	"audit_get_manifest",
	"audit_list_spans",
	"audit_get_content_chunk",
}

var (
	ErrAuditAgentOffline = errors.New("audit connector is unavailable")
	ErrAuditNotSupported = errors.New("audit replay action is not supported")
	ErrAuditTimeout      = errors.New("audit connector timed out")
)

// handleAuditState accepts lifecycle metadata only. The database service
// verifies the event/message/user correlation before persisting it.
func (m *Manager) handleAuditState(conn *agentConn, pkt *protocol.Packet) {
	if conn == nil || pkt == nil {
		return
	}
	var payload protocol.AuditStatePayload
	if err := json.Unmarshal(pkt.Payload, &payload); err != nil {
		conn.sendPayload(protocol.CmdError, pkt.Seq, SendNackPayload{Code: 4001, Msg: "invalid audit_state payload"})
		return
	}
	_, err := conversationaudit.RecordState(conn.agentID, conn.ownerID, conversationaudit.StatePayload{
		EventID: payload.EventID, SessionID: payload.SessionID, MsgID: payload.MsgID,
		AuditID: payload.AuditID, TurnID: payload.TurnID, State: payload.State,
		Revision: payload.Revision, Quality: payload.Quality, Truncated: payload.Truncated,
		ErrorCode: payload.ErrorCode, ErrorMessage: payload.ErrorMessage,
	})
	if err != nil {
		logger.L.Warnf("audit_state rejected agent=%d owner=%d event=%s session=%s msg=%d err=%v",
			conn.agentID, conn.ownerID, payload.EventID, payload.SessionID, payload.MsgID, err)
		conn.sendPayload(protocol.CmdError, pkt.Seq, SendNackPayload{Code: 4003, Msg: "audit_state rejected"})
		return
	}
	conn.sendPayload(protocol.CmdAuditStateAck, pkt.Seq, map[string]any{"event_id": payload.EventID, "received": true})
}

// CanServeConversationAudit verifies the complete replay surface before a
// user-facing query is sent. Local connections additionally require the v2
// capability; remote nodes persist their declared Local Action set in Redis,
// where the complete three-action surface is the v2 compatibility marker.
func (m *Manager) CanServeConversationAudit(agentID, ownerID int64) bool {
	if m == nil || agentID <= 0 || ownerID <= 0 {
		return false
	}
	if conn := m.lookupConnForOwner(agentID, ownerID); conn != nil {
		if !hasDeclaredName(conn.capabilities, "audit_replay_v2") ||
			!hasDeclaredName(conn.capabilities, "local_action_v1") {
			return false
		}
		return hasAllDeclaredNames(conn.localActions, auditReplayLocalActions)
	}
	return hasAllDeclaredNames(loadAgentCapabilitiesForOwner(context.Background(), agentID, ownerID), auditReplayLocalActions)
}

// SendAuditActionAndWait forwards one read-only replay action to the connector
// and returns its raw, bounded response. It works for both local and
// cross-node agent connections through the existing pending-action path.
func (m *Manager) SendAuditActionAndWait(
	agentID, ownerID int64,
	sessionID, actionType string,
	params map[string]interface{},
) (protocol.LocalActionResultPayload, error) {
	if m == nil || agentID <= 0 || ownerID <= 0 || strings.TrimSpace(sessionID) == "" {
		return protocol.LocalActionResultPayload{}, ErrAuditAgentOffline
	}
	actionType = strings.TrimSpace(actionType)
	switch actionType {
	case "audit_get_manifest", "audit_list_spans", "audit_get_content_chunk":
	default:
		return protocol.LocalActionResultPayload{}, ErrAuditNotSupported
	}
	if !m.CanServeConversationAudit(agentID, ownerID) {
		return protocol.LocalActionResultPayload{}, ErrAuditNotSupported
	}
	actionID := fmt.Sprintf("audit:%s:%d:%d", actionType, agentID, snowflake.GenID())
	resultCh := make(chan protocol.LocalActionResultPayload, 1)
	pending := &pendingLocalAction{
		actionID: actionID, kind: "forwarded_local_action", agentID: agentID,
		ownerID: ownerID, sessionID: sessionID, actionType: actionType,
		forwardedResultCh: resultCh,
	}
	action := protocol.LocalActionPayload{
		ActionID: actionID, ActionType: actionType, Params: params, TimeoutMs: auditLocalActionTimeoutMs,
	}
	if !m.sendLocalActionWithPendingForOwner(agentID, ownerID, action, pending) {
		return protocol.LocalActionResultPayload{}, ErrAuditAgentOffline
	}
	timer := time.NewTimer(time.Duration(auditLocalActionTimeoutMs) * time.Millisecond)
	defer timer.Stop()
	select {
	case result := <-resultCh:
		return result, nil
	case <-m.stopping():
		m.deletePendingLocalAction(actionID)
		return protocol.LocalActionResultPayload{}, ErrAuditTimeout
	case <-timer.C:
		m.deletePendingLocalAction(actionID)
		return protocol.LocalActionResultPayload{}, ErrAuditTimeout
	}
}

func hasAllDeclaredNames(values, required []string) bool {
	for _, name := range required {
		if !hasDeclaredName(values, name) {
			return false
		}
	}
	return true
}
