package agentapi

import (
	"errors"
	"strings"

	"github.com/askie/grix/backend/internal/conversationaudit"
)

func dispatchAuditReplayAction(ownerID int64, action string, params map[string]interface{}) (interface{}, int, string) {
	auditID, ok := paramString(params, "audit_id")
	auditID = strings.TrimSpace(auditID)
	if !ok || auditID == "" {
		return nil, 4001, "audit_id required"
	}

	turn, err := conversationaudit.LookupTurnByAuditID(ownerID, auditID)
	if err != nil {
		if errors.Is(err, conversationaudit.ErrInvalidRequest) {
			return nil, 4001, "audit_id invalid"
		}
		if errors.Is(err, conversationaudit.ErrNotFound) {
			return nil, 4004, "audit turn not found"
		}
		return nil, 5001, err.Error()
	}
	if strings.TrimSpace(turn.AuditID) == "" || strings.TrimSpace(turn.TurnID) == "" || strings.TrimSpace(turn.SessionID) == "" {
		return nil, 4002, "audit replay is not ready"
	}

	forwardParams := map[string]interface{}{
		"session_id": turn.SessionID,
		"audit_id":   turn.AuditID,
		"turn_id":    turn.TurnID,
	}
	if revision, ok := paramInt(params, "revision"); ok {
		forwardParams["revision"] = revision
	}

	switch action {
	case "audit_list_spans":
		if cursor, ok := paramString(params, "cursor"); ok && strings.TrimSpace(cursor) != "" {
			forwardParams["cursor"] = strings.TrimSpace(cursor)
		}
		if limit, ok := paramInt(params, "limit"); ok && limit > 0 {
			forwardParams["limit"] = limit
		}
	case "audit_get_content_chunk":
		contentID, ok := paramString(params, "content_id")
		contentID = strings.TrimSpace(contentID)
		if !ok || contentID == "" {
			return nil, 4001, "content_id required"
		}
		forwardParams["content_id"] = contentID
		if cursor, ok := paramString(params, "cursor"); ok && strings.TrimSpace(cursor) != "" {
			forwardParams["cursor"] = strings.TrimSpace(cursor)
		}
		if maxBytes, ok := paramInt(params, "max_bytes"); ok && maxBytes > 0 {
			forwardParams["max_bytes"] = maxBytes
		}
	}

	mgr := GetGlobal()
	if mgr == nil {
		return nil, 5001, "audit service is unavailable"
	}
	result, err := mgr.SendAuditActionAndWait(turn.AgentID, ownerID, turn.SessionID, action, forwardParams)
	if err != nil {
		if errors.Is(err, ErrAuditNotSupported) {
			return nil, 4003, "audit connector does not support replay"
		}
		return nil, 5001, err.Error()
	}
	if result.Status != "ok" {
		msg := strings.TrimSpace(result.ErrorMsg)
		if msg == "" {
			msg = strings.TrimSpace(result.ErrorCode)
		}
		if msg == "" {
			msg = "audit query failed"
		}
		return nil, 5001, msg
	}

	return map[string]interface{}{
		"state":    turn.State,
		"audit_id": turn.AuditID,
		"turn_id":  turn.TurnID,
		"revision": turn.Revision,
		"result":   result.Result,
	}, 0, ""
}
