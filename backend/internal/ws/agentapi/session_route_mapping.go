package agentapi

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/logger"
	"github.com/askie/grix/backend/internal/store"
	"github.com/askie/grix/backend/internal/ws/agentmsg"
	"github.com/askie/grix/backend/internal/ws/protocol"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	cmdSessionRouteBind    = "session_route_bind"
	cmdSessionRouteResolve = "session_route_resolve"
)

type SessionRouteBindPayload struct {
	Channel         string `json:"channel"`
	AccountID       string `json:"account_id"`
	RouteSessionKey string `json:"route_session_key"`
	SessionID       string `json:"session_id"`
}

type SessionRouteResolvePayload struct {
	Channel         string `json:"channel"`
	AccountID       string `json:"account_id"`
	RouteSessionKey string `json:"route_session_key"`
}

func normalizeRouteChannel(channel string) string {
	return strings.ToLower(strings.TrimSpace(channel))
}

func normalizeRouteSessionAccount(accountID string) string {
	return strings.TrimSpace(accountID)
}

func normalizeRouteSessionKey(routeSessionKey string) string {
	return strings.TrimSpace(routeSessionKey)
}

func validateRouteScope(channel, accountID, routeSessionKey string) error {
	if channel == "" {
		return errors.New("channel required")
	}
	if len(channel) > 32 {
		return errors.New("channel too long")
	}
	if accountID == "" {
		return errors.New("account_id required")
	}
	if len(accountID) > 64 {
		return errors.New("account_id too long")
	}
	if routeSessionKey == "" {
		return errors.New("route_session_key required")
	}
	if len(routeSessionKey) > 191 {
		return errors.New("route_session_key too long")
	}
	return nil
}

func validateAgentRouteSessionAccess(ctx context.Context, agentID, ownerID int64, sessionID string) *SendError {
	_, err := agentmsg.ResolveIdentity(ctx, agentmsg.IdentityParams{
		Mode:      agentmsg.ModeAgentAPI,
		SessionID: sessionID,
		OwnerID:   ownerID,
		AgentID:   agentID,
	})
	if err == nil {
		return nil
	}
	if errors.Is(err, agentmsg.ErrPermissionDenied) {
		return &SendError{Code: 4003, Msg: "permission denied"}
	}
	return &SendError{Code: 5001, Msg: "session identity resolve failed"}
}

func (m *Manager) handleSessionRouteBind(conn *agentConn, pkt *protocol.Packet) {
	var payload SessionRouteBindPayload
	if err := json.Unmarshal(pkt.Payload, &payload); err != nil {
		logger.L.Warnf("agent_api session_route_bind invalid payload agent=%d owner=%d err=%v", conn.agentID, conn.ownerID, err)
		conn.sendPayload("send_nack", pkt.Seq, SendNackPayload{
			Code: 4001,
			Msg:  "invalid session_route_bind payload",
		})
		return
	}

	channel := normalizeRouteChannel(payload.Channel)
	accountID := normalizeRouteSessionAccount(payload.AccountID)
	routeSessionKey := normalizeRouteSessionKey(payload.RouteSessionKey)
	sessionID := strings.TrimSpace(payload.SessionID)
	logger.L.Infof(
		"agent_api session_route_bind request agent=%d owner=%d channel=%s account_id=%s route_session_key=%s session_id=%s",
		conn.agentID,
		conn.ownerID,
		channel,
		accountID,
		routeSessionKey,
		sessionID,
	)
	if sessionID == "" {
		logger.L.Warnf("agent_api session_route_bind rejected agent=%d owner=%d reason=session_id_required", conn.agentID, conn.ownerID)
		conn.sendPayload("send_nack", pkt.Seq, SendNackPayload{
			Code: 4001,
			Msg:  "session_id required",
		})
		return
	}
	if err := validateRouteScope(channel, accountID, routeSessionKey); err != nil {
		logger.L.Warnf(
			"agent_api session_route_bind rejected agent=%d owner=%d channel=%s account_id=%s route_session_key=%s reason=%v",
			conn.agentID,
			conn.ownerID,
			channel,
			accountID,
			routeSessionKey,
			err,
		)
		conn.sendPayload("send_nack", pkt.Seq, SendNackPayload{
			Code: 4001,
			Msg:  err.Error(),
		})
		return
	}

	if sendErr := validateAgentRouteSessionAccess(context.Background(), conn.agentID, conn.ownerID, sessionID); sendErr != nil {
		logger.L.Warnf(
			"agent_api session_route_bind denied agent=%d owner=%d channel=%s account_id=%s route_session_key=%s session_id=%s code=%d msg=%s",
			conn.agentID,
			conn.ownerID,
			channel,
			accountID,
			routeSessionKey,
			sessionID,
			sendErr.Code,
			sendErr.Msg,
		)
		conn.sendPayload("send_nack", pkt.Seq, SendNackPayload{
			Code: sendErr.Code,
			Msg:  sendErr.Msg,
		})
		return
	}

	if err := upsertSessionRouteMapping(context.Background(), upsertSessionRouteMappingParams{
		AgentID:         conn.agentID,
		OwnerID:         conn.ownerID,
		Channel:         channel,
		AccountID:       accountID,
		RouteSessionKey: routeSessionKey,
		SessionID:       sessionID,
	}); err != nil {
		logger.L.Errorf(
			"agent_api session_route_bind failed agent=%d owner=%d channel=%s account_id=%s route_session_key=%s session_id=%s err=%v",
			conn.agentID,
			conn.ownerID,
			channel,
			accountID,
			routeSessionKey,
			sessionID,
			err,
		)
		conn.sendPayload("send_nack", pkt.Seq, SendNackPayload{
			Code: 5001,
			Msg:  "session route bind failed",
		})
		return
	}
	logger.L.Infof(
		"agent_api session_route_bind success agent=%d owner=%d channel=%s account_id=%s route_session_key=%s session_id=%s",
		conn.agentID,
		conn.ownerID,
		channel,
		accountID,
		routeSessionKey,
		sessionID,
	)

	conn.sendPayload("send_ack", pkt.Seq, map[string]any{
		"channel":           channel,
		"account_id":        accountID,
		"route_session_key": routeSessionKey,
		"session_id":        sessionID,
		"updated_at":        time.Now().UnixMilli(),
	})
}

func (m *Manager) handleSessionRouteResolve(conn *agentConn, pkt *protocol.Packet) {
	var payload SessionRouteResolvePayload
	if err := json.Unmarshal(pkt.Payload, &payload); err != nil {
		logger.L.Warnf("agent_api session_route_resolve invalid payload agent=%d owner=%d err=%v", conn.agentID, conn.ownerID, err)
		conn.sendPayload("send_nack", pkt.Seq, SendNackPayload{
			Code: 4001,
			Msg:  "invalid session_route_resolve payload",
		})
		return
	}

	channel := normalizeRouteChannel(payload.Channel)
	accountID := normalizeRouteSessionAccount(payload.AccountID)
	routeSessionKey := normalizeRouteSessionKey(payload.RouteSessionKey)
	logger.L.Infof(
		"agent_api session_route_resolve request agent=%d owner=%d channel=%s account_id=%s route_session_key=%s",
		conn.agentID,
		conn.ownerID,
		channel,
		accountID,
		routeSessionKey,
	)
	if err := validateRouteScope(channel, accountID, routeSessionKey); err != nil {
		logger.L.Warnf(
			"agent_api session_route_resolve rejected agent=%d owner=%d channel=%s account_id=%s route_session_key=%s reason=%v",
			conn.agentID,
			conn.ownerID,
			channel,
			accountID,
			routeSessionKey,
			err,
		)
		conn.sendPayload("send_nack", pkt.Seq, SendNackPayload{
			Code: 4001,
			Msg:  err.Error(),
		})
		return
	}

	mapping, err := getSessionRouteMapping(context.Background(), conn.agentID, channel, accountID, routeSessionKey)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			logger.L.Warnf(
				"agent_api session_route_resolve miss agent=%d owner=%d channel=%s account_id=%s route_session_key=%s",
				conn.agentID,
				conn.ownerID,
				channel,
				accountID,
				routeSessionKey,
			)
			conn.sendPayload("send_nack", pkt.Seq, SendNackPayload{
				Code: 4044,
				Msg:  "route_session_key not found",
			})
			return
		}
		logger.L.Errorf(
			"agent_api session_route_resolve failed agent=%d owner=%d channel=%s account_id=%s route_session_key=%s err=%v",
			conn.agentID,
			conn.ownerID,
			channel,
			accountID,
			routeSessionKey,
			err,
		)
		conn.sendPayload("send_nack", pkt.Seq, SendNackPayload{
			Code: 5001,
			Msg:  "session route resolve failed",
		})
		return
	}

	if sendErr := validateAgentRouteSessionAccess(context.Background(), conn.agentID, conn.ownerID, mapping.SessionID); sendErr != nil {
		logger.L.Warnf(
			"agent_api session_route_resolve denied agent=%d owner=%d channel=%s account_id=%s route_session_key=%s session_id=%s code=%d msg=%s",
			conn.agentID,
			conn.ownerID,
			channel,
			accountID,
			routeSessionKey,
			mapping.SessionID,
			sendErr.Code,
			sendErr.Msg,
		)
		conn.sendPayload("send_nack", pkt.Seq, SendNackPayload{
			Code: sendErr.Code,
			Msg:  sendErr.Msg,
		})
		return
	}
	logger.L.Infof(
		"agent_api session_route_resolve success agent=%d owner=%d channel=%s account_id=%s route_session_key=%s session_id=%s",
		conn.agentID,
		conn.ownerID,
		channel,
		accountID,
		routeSessionKey,
		mapping.SessionID,
	)

	conn.sendPayload("send_ack", pkt.Seq, map[string]any{
		"channel":           mapping.Channel,
		"account_id":        mapping.AccountID,
		"route_session_key": mapping.RouteSessionKey,
		"session_id":        mapping.SessionID,
	})
}

type upsertSessionRouteMappingParams struct {
	AgentID         int64
	OwnerID         int64
	Channel         string
	AccountID       string
	RouteSessionKey string
	SessionID       string
}

func upsertSessionRouteMapping(ctx context.Context, params upsertSessionRouteMappingParams) error {
	now := time.Now()
	record := model.AgentSessionRouteMapping{
		AgentID:         params.AgentID,
		OwnerID:         params.OwnerID,
		Channel:         params.Channel,
		AccountID:       params.AccountID,
		RouteSessionKey: params.RouteSessionKey,
		SessionID:       params.SessionID,
		CreatedAt:       now,
		UpdatedAt:       now,
	}
	return store.DB.WithContext(ctx).
		Clauses(clause.OnConflict{
			Columns: []clause.Column{
				{Name: "agent_id"},
				{Name: "channel"},
				{Name: "account_id"},
				{Name: "route_session_key"},
			},
			DoUpdates: clause.Assignments(map[string]any{
				"owner_id":   params.OwnerID,
				"session_id": params.SessionID,
				"updated_at": now,
			}),
		}).
		Create(&record).Error
}

func getSessionRouteMapping(
	ctx context.Context,
	agentID int64,
	channel string,
	accountID string,
	routeSessionKey string,
) (*model.AgentSessionRouteMapping, error) {
	var mapping model.AgentSessionRouteMapping
	if err := store.DB.WithContext(ctx).
		Where("agent_id = ? AND channel = ? AND account_id = ? AND route_session_key = ?",
			agentID,
			channel,
			accountID,
			routeSessionKey,
		).
		First(&mapping).Error; err != nil {
		return nil, err
	}
	return &mapping, nil
}
