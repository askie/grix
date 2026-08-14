package handler

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/askie/grix/backend/internal/agenttoolbar"
	bindingstore "github.com/askie/grix/backend/internal/agenttoolbar/store"
	apiservice "github.com/askie/grix/backend/internal/api/service"
	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/logger"
	appstore "github.com/askie/grix/backend/internal/store"
	wsagentapi "github.com/askie/grix/backend/internal/ws/agentapi"
	"github.com/askie/grix/backend/internal/ws/protocol"
)

func HandleAgentSessionBind(_ HubInterface, conn ConnInterface, pkt *protocol.Packet) {
	userID := conn.GetUserID()
	var payload protocol.AgentSessionBindPayload
	if err := json.Unmarshal(pkt.Payload, &payload); err != nil {
		conn.SendPayload(protocol.CmdAgentSessionBindResp, pkt.Seq, protocol.AgentSessionBindRespPayload{
			Error: "invalid payload",
			Code:  "invalid_payload",
		})
		return
	}

	payload.Cwd = strings.TrimSpace(payload.Cwd)
	payload.AgentSessionID = strings.TrimSpace(payload.AgentSessionID)
	payload.ProviderKey = normalizeAgentSessionProviderKey(payload.ProviderKey)
	if payload.AgentID == 0 || payload.Cwd == "" {
		conn.SendPayload(protocol.CmdAgentSessionBindResp, pkt.Seq, protocol.AgentSessionBindRespPayload{
			Error: "agent_id and cwd are required",
			Code:  "invalid_payload",
		})
		return
	}
	mgr := wsagentapi.GetGlobal()
	if mgr == nil {
		conn.SendPayload(protocol.CmdAgentSessionBindResp, pkt.Seq, protocol.AgentSessionBindRespPayload{
			Error: "service unavailable",
			Code:  "agent_offline",
		})
		return
	}

	seq := pkt.Seq
	go func() {
		resp := handleAgentSessionBindAsync(userID, payload, mgr)
		conn.SendPayload(protocol.CmdAgentSessionBindResp, seq, resp)
	}()
}

func handleAgentSessionBindAsync(userID int64, payload protocol.AgentSessionBindPayload, mgr *wsagentapi.Manager) protocol.AgentSessionBindRespPayload {
	ctx := context.Background()
	agent, err := loadOwnedAgentForSessionBind(payload.AgentID, userID)
	if err != nil {
		return bindError(classifySessionBindCreateError(err), err)
	}
	providerKey := normalizeAgentSessionProviderKey(payload.ProviderKey)
	if providerKey == "" {
		providerKey = normalizeAgentSessionProviderKey(agent.AgentClientType)
	}
	if providerKey == "" {
		providerKey = "acp"
	}

	var sessionID string
	isNew := false
	if payload.AgentSessionID != "" {
		record, found, err := bindingstore.LoadBindingByProvider(ctx, payload.AgentID, providerKey, payload.AgentSessionID)
		if err != nil {
			return bindError("session_create_failed", err)
		}
		if found && strings.TrimSpace(record.SessionID) != "" {
			sessionID = strings.TrimSpace(record.SessionID)
		}
	}

	if sessionID == "" {
		directSuffix := providerKey + ":new:" + strconv.FormatInt(time.Now().UnixNano(), 10)
		if payload.AgentSessionID != "" {
			directSuffix = providerKey + ":" + payload.AgentSessionID
		}
		created, err := apiservice.SessionCreateForAgentBinding(userID, payload.AgentID, directSuffix, payload.Title)
		if err != nil {
			return bindError(classifySessionBindCreateError(err), err)
		}
		sessionID = created.SessionID
		isNew = created.IsNew
	}

	pending := bindingstore.BindingRecord{
		AgentID:      payload.AgentID,
		SessionID:    sessionID,
		ProviderKey:  providerKey,
		BindingID:    payload.AgentSessionID,
		Cwd:          payload.Cwd,
		Status:       "pending",
		WorkerStatus: "pending",
		Meta: map[string]any{
			"title":            strings.TrimSpace(payload.Title),
			"requested_cwd":    payload.Cwd,
			"agent_session_id": payload.AgentSessionID,
		},
	}
	if err := bindingstore.UpsertBinding(ctx, pending); err != nil {
		logger.L.Warnf("agent_session_bind: pending upsert failed agent=%d session=%s provider=%s binding=%s err=%v",
			payload.AgentID, sessionID, providerKey, payload.AgentSessionID, err)
		if payload.AgentSessionID != "" {
			if record, found, loadErr := bindingstore.LoadBindingByProvider(ctx, payload.AgentID, providerKey, payload.AgentSessionID); loadErr == nil && found {
				sessionID = record.SessionID
			} else {
				return bindError("session_create_failed", err)
			}
		} else {
			return bindError("session_create_failed", err)
		}
	}

	actorID := strconv.FormatInt(userID, 10)
	// 按请求者 userID 作为 owner 精确路由（agent 共享多连接物理隔离）。
	bindResp, err := mgr.SendSessionBindActionAndWait(payload.AgentID, userID, sessionID, actorID, payload.Cwd, providerKey, payload.AgentSessionID)
	if err != nil {
		code := classifySessionBindActionError(err)
		status := "failed"
		if code == "binding_pending" || code == "timeout" {
			status = "pending"
		}
		_ = bindingstore.UpsertBinding(ctx, bindingstore.BindingRecord{
			AgentID:      payload.AgentID,
			SessionID:    sessionID,
			ProviderKey:  providerKey,
			BindingID:    payload.AgentSessionID,
			Cwd:          payload.Cwd,
			Status:       status,
			WorkerStatus: status,
			Meta: map[string]any{
				"title":            strings.TrimSpace(payload.Title),
				"error":            err.Error(),
				"agent_session_id": payload.AgentSessionID,
			},
		})
		if status == "pending" {
			refreshToolbarAfterBind(ctx, userID, sessionID)
		}
		return protocol.AgentSessionBindRespPayload{
			Error:     err.Error(),
			Code:      code,
			SessionID: sessionID,
			Status:    status,
		}
	}

	bindingID := firstTrimmed(bindResp.BindingID, bindResp.AgentSessionID, payload.AgentSessionID)
	if payload.AgentSessionID != "" && bindingID != payload.AgentSessionID {
		err := errors.New("connector did not confirm requested agent session")
		_ = bindingstore.UpsertBinding(ctx, bindingstore.BindingRecord{
			AgentID:      payload.AgentID,
			SessionID:    sessionID,
			ProviderKey:  providerKey,
			BindingID:    payload.AgentSessionID,
			Cwd:          payload.Cwd,
			Status:       "failed",
			WorkerStatus: "failed",
			Meta: map[string]any{
				"title":            strings.TrimSpace(payload.Title),
				"error":            err.Error(),
				"agent_session_id": payload.AgentSessionID,
			},
		})
		return bindError("unsupported", err)
	}
	providerKey = firstTrimmed(bindResp.ProviderKey, providerKey)
	cwd := firstTrimmed(bindResp.Cwd, payload.Cwd)
	workerStatus := firstTrimmed(bindResp.WorkerStatus, "ready")
	binding := map[string]interface{}{
		"providerKey":    providerKey,
		"bindingId":      bindingID,
		"agentSessionId": bindingID,
		"cwd":            cwd,
		"workerStatus":   workerStatus,
	}
	if bindResp.Binding != nil {
		for k, v := range bindResp.Binding {
			binding[k] = v
		}
	}
	_ = bindingstore.UpsertBinding(ctx, bindingstore.BindingRecord{
		AgentID:      payload.AgentID,
		SessionID:    sessionID,
		ProviderKey:  providerKey,
		BindingID:    bindingID,
		Cwd:          cwd,
		Status:       "active",
		WorkerStatus: workerStatus,
		Meta: map[string]any{
			"title":            strings.TrimSpace(payload.Title),
			"agent_session_id": bindingID,
		},
	})
	refreshToolbarAfterBind(ctx, userID, sessionID)
	return protocol.AgentSessionBindRespPayload{
		SessionID: sessionID,
		IsNew:     isNew,
		Status:    "active",
		Binding:   binding,
	}
}

// refreshToolbarAfterBind pushes a fresh agent toolbar snapshot to the user
// right after a session bind, so the toolbar shows up without re-entering the
// session. The other binding-write paths (update_binding_card from the
// connector, toolbar local-action results) already refresh on write; the
// user-initiated bind path was the one gap.
func refreshToolbarAfterBind(ctx context.Context, userID int64, sessionID string) {
	svc := agenttoolbar.GetGlobal()
	if svc == nil || userID <= 0 || strings.TrimSpace(sessionID) == "" {
		return
	}
	_ = svc.RefreshSession(ctx, userID, sessionID, "session_bind")
}

func bindError(code string, err error) protocol.AgentSessionBindRespPayload {
	msg := ""
	if err != nil {
		msg = err.Error()
	}
	return protocol.AgentSessionBindRespPayload{Error: msg, Code: code}
}

func classifySessionBindCreateError(err error) string {
	switch {
	case errors.Is(err, apiservice.ErrMemberAgentNotFound):
		return "agent_not_found"
	case errors.Is(err, apiservice.ErrMemberAgentNotOwned), errors.Is(err, apiservice.ErrMemberAgentUnavailable):
		return "agent_not_found"
	default:
		return "session_create_failed"
	}
}

func classifySessionBindActionError(err error) string {
	switch {
	case errors.Is(err, wsagentapi.ErrSessionBindAgentOffline):
		return "agent_offline"
	case errors.Is(err, wsagentapi.ErrSessionBindNotSupported):
		return "unsupported"
	case errors.Is(err, wsagentapi.ErrSessionBindTimeout):
		return "binding_pending"
	default:
		msg := strings.ToLower(err.Error())
		switch {
		case strings.Contains(msg, "invalid_agent_session"):
			return "invalid_agent_session"
		case strings.Contains(msg, "session_invalid_cwd"), strings.Contains(msg, "invalid cwd"), strings.Contains(msg, "path"):
			return "invalid_cwd"
		case strings.Contains(msg, "unsupported"):
			return "unsupported"
		default:
			return "session_create_failed"
		}
	}
}

func normalizeAgentSessionProviderKey(value string) string {
	switch model.NormalizeAgentClientType(value) {
	case model.AgentClientTypeClaude:
		return "claude"
	case model.AgentClientTypeCodex:
		return "codex"
	case model.AgentClientTypePi:
		return "pi"
	case model.AgentClientTypeCodeWhale:
		return "codewhale"
	default:
		return "acp"
	}
}

func loadOwnedAgentForSessionBind(agentID int64, userID int64) (model.Agent, error) {
	var agent model.Agent
	if appstore.DB == nil || agentID <= 0 {
		return agent, apiservice.ErrMemberAgentNotFound
	}
	if err := appstore.DB.Select("id", "owner_id", "status", "agent_client_type").
		Where("id = ?", agentID).
		First(&agent).Error; err != nil {
		return agent, apiservice.ErrMemberAgentNotFound
	}
	if agent.OwnerID != userID {
		return agent, apiservice.ErrMemberAgentNotOwned
	}
	if agent.Status != 1 {
		return agent, apiservice.ErrMemberAgentUnavailable
	}
	return agent, nil
}

func firstTrimmed(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}
