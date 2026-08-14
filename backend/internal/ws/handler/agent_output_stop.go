package handler

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/askie/grix/backend/internal/api/service"
	"github.com/askie/grix/backend/internal/pkg/logger"
	wsagentapi "github.com/askie/grix/backend/internal/ws/agentapi"
	"github.com/askie/grix/backend/internal/ws/agentstream"
	"github.com/askie/grix/backend/internal/ws/protocol"
)

func HandleAgentOutputStop(_ HubInterface, conn ConnInterface, pkt *protocol.Packet) {
	var payload protocol.AgentOutputStopPayload
	if err := json.Unmarshal(pkt.Payload, &payload); err != nil {
		logger.L.Warnf("agent_output_stop payload error: %v", err)
		return
	}
	logger.L.Infof(
		"agent_output_stop received owner=%d session=%s run_id=%s seq=%d",
		conn.GetUserID(),
		strings.TrimSpace(payload.SessionID),
		strings.TrimSpace(payload.RunID),
		pkt.Seq,
	)
	if err := service.EnsureHumanSessionAccessible(
		context.Background(),
		conn.GetUserID(),
		payload.SessionID,
	); err != nil {
		logger.L.Warnf(
			"agent_output_stop permission denied owner=%d session=%s run_id=%s err=%v",
			conn.GetUserID(),
			strings.TrimSpace(payload.SessionID),
			strings.TrimSpace(payload.RunID),
			err,
		)
		conn.SendPayload(protocol.CmdAgentOutputStopAck, pkt.Seq, protocol.AgentOutputStopAckPayload{
			SessionID: strings.TrimSpace(payload.SessionID),
			RunID:     strings.TrimSpace(payload.RunID),
			Accepted:  false,
			Msg:       resolveAgentOutputStopDeniedMessage(err),
			UpdatedAt: nowUnixMilli(),
		})
		return
	}

	mgr := wsagentapi.GetGlobal()
	if mgr == nil {
		logger.L.Warnf(
			"agent_output_stop manager unavailable owner=%d session=%s run_id=%s",
			conn.GetUserID(),
			strings.TrimSpace(payload.SessionID),
			strings.TrimSpace(payload.RunID),
		)
		conn.SendPayload(protocol.CmdAgentOutputStopAck, pkt.Seq, protocol.AgentOutputStopAckPayload{
			SessionID: strings.TrimSpace(payload.SessionID),
			RunID:     strings.TrimSpace(payload.RunID),
			Accepted:  false,
			Msg:       "agent api manager unavailable",
			UpdatedAt: nowUnixMilli(),
		})
		return
	}

	ack, run, err := mgr.RequestOutputStop(conn.GetUserID(), payload.SessionID, payload.RunID)
	if err == nil && run != nil && run.StreamMsgID > 0 {
		ApplyAgentOutputStopLocalEffects(context.Background(), run)
		logger.L.Infof(
			"agent_output_stop applied local fence+revoke owner=%d session=%s run_id=%s agent=%d stream_msg_id=%d",
			conn.GetUserID(),
			strings.TrimSpace(run.SessionID),
			strings.TrimSpace(run.EventID),
			run.AgentID,
			run.StreamMsgID,
		)
	} else if err == nil && run != nil {
		logger.L.Infof(
			"agent_output_stop skip local fence+revoke owner=%d session=%s run_id=%s agent=%d stream_msg_id=%d",
			conn.GetUserID(),
			strings.TrimSpace(run.SessionID),
			strings.TrimSpace(run.EventID),
			run.AgentID,
			run.StreamMsgID,
		)
	}
	if err == nil && run != nil {
		if dispatchErr := mgr.DispatchOutputStop(ack, run); dispatchErr != nil && ack.Msg == "" {
			ack.Msg = dispatchErr.Error()
		}
	}
	logger.L.Infof(
		"agent_output_stop ack owner=%d session=%s requested_run=%s resolved_run=%s accepted=%t stop_id=%s stream_msg_id=%d err=%v msg=%s",
		conn.GetUserID(),
		strings.TrimSpace(payload.SessionID),
		strings.TrimSpace(payload.RunID),
		strings.TrimSpace(ack.RunID),
		ack.Accepted,
		strings.TrimSpace(ack.StopID),
		func() int64 {
			if run == nil {
				return 0
			}
			return run.StreamMsgID
		}(),
		err,
		strings.TrimSpace(ack.Msg),
	)
	conn.SendPayload(protocol.CmdAgentOutputStopAck, pkt.Seq, ack)
}

func fenceStoppedAgentStream(ctx context.Context, agentID, msgID int64) {
	if ctx == nil {
		ctx = context.Background()
	}
	if agentID <= 0 || msgID <= 0 {
		return
	}
	if err := agentstream.FenceStreamsByMsgID(ctx, agentID, msgID, agentstream.DefaultStoppedFenceTTL); err != nil {
		logger.L.Warnf(
			"agent_output_stop: fence stream failed agent=%d msg_id=%d err=%v",
			agentID,
			msgID,
			err,
		)
		return
	}
	logger.L.Infof("agent_output_stop: fence stream applied agent=%d msg_id=%d", agentID, msgID)
}

func revokeStoppedAgentStream(ctx context.Context, sessionID string, msgID int64) {
	if ctx == nil {
		ctx = context.Background()
	}
	if strings.TrimSpace(sessionID) == "" || msgID <= 0 {
		return
	}
	if err := service.RevokeMessageForStop(ctx, sessionID, msgID); err != nil {
		logger.L.Warnf(
			"agent_output_stop: revoke stream failed session=%s msg_id=%d err=%v",
			sessionID,
			msgID,
			err,
		)
		return
	}
	logger.L.Infof("agent_output_stop: revoke stream applied session=%s msg_id=%d", strings.TrimSpace(sessionID), msgID)
}

func resolveAgentOutputStopDeniedMessage(err error) string {
	switch {
	case errors.Is(err, service.ErrSessionGroupBanned):
		return service.ErrSessionGroupBanned.Error()
	case errors.Is(err, service.ErrSessionNotFound):
		return service.ErrSessionNotFound.Error()
	case errors.Is(err, service.ErrSessionPermissionDenied):
		return service.ErrSessionPermissionDenied.Error()
	default:
		return service.ErrSessionPermissionDenied.Error()
	}
}
