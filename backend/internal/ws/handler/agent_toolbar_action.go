package handler

import (
	"context"
	"encoding/json"

	agenttoolbar "github.com/askie/grix/backend/internal/agenttoolbar"
	toolprotocol "github.com/askie/grix/backend/internal/agenttoolbar/protocol"
	"github.com/askie/grix/backend/internal/pkg/logger"
	wsprotocol "github.com/askie/grix/backend/internal/ws/protocol"
)

func HandleAgentToolbarAction(hub HubInterface, conn ConnInterface, pkt *wsprotocol.Packet) {
	var payload wsprotocol.AgentToolbarActionPayload
	if err := json.Unmarshal(pkt.Payload, &payload); err != nil {
		conn.SendPayload(wsprotocol.CmdAgentToolbarActionAck, pkt.Seq, wsprotocol.AgentToolbarActionAckPayload{
			SessionID:      payload.SessionID,
			ToolbarID:      payload.ToolbarID,
			ClientActionID: payload.ClientActionID,
			Accepted:       false,
			Code:           "invalid_payload",
			Msg:            "invalid payload",
			UpdatedAt:      nowUnixMilli(),
		})
		return
	}
	if ack, snapshot, visitorNotify, handled := handleVisitorToolbarAction(conn.GetUserID(), payload); handled {
		conn.SendPayload(wsprotocol.CmdAgentToolbarActionAck, pkt.Seq, ack)
		if ack.Accepted && snapshot != nil {
			conn.SendPayload(wsprotocol.CmdAgentToolbarSync, conn.NextSeq(), *snapshot)
		}
		if visitorNotify != nil {
			notifyWidgetSessionClosed(hub, visitorNotify.VisitorID, visitorNotify.SessionID, visitorNotify.Reason)
		}
		return
	}
	logger.L.Infof("[toolbar-action] client request: user=%d session_id=%s action_id=%s item_id=%s action_id_field=%s event=%s",
		conn.GetUserID(), payload.SessionID, payload.ClientActionID, payload.ItemID, payload.ActionID, payload.Event)
	svc := agenttoolbar.GetGlobal()
	if svc == nil {
		conn.SendPayload(wsprotocol.CmdAgentToolbarActionAck, pkt.Seq, wsprotocol.AgentToolbarActionAckPayload{
			SessionID:      payload.SessionID,
			ToolbarID:      payload.ToolbarID,
			ClientActionID: payload.ClientActionID,
			Accepted:       false,
			Code:           "service_unavailable",
			Msg:            "toolbar service unavailable",
			UpdatedAt:      nowUnixMilli(),
		})
		return
	}
	ack, err := svc.HandleAction(context.Background(), conn.GetUserID(), toolprotocol.ActionRequest{
		SessionID:      payload.SessionID,
		TargetAgentID:  payload.TargetAgentID,
		ToolbarID:      payload.ToolbarID,
		Revision:       payload.Revision,
		ItemID:         payload.ItemID,
		ActionID:       payload.ActionID,
		ClientActionID: payload.ClientActionID,
		Event:          payload.Event,
		OptionID:       payload.OptionID,
	})
	if err != nil {
		conn.SendPayload(wsprotocol.CmdAgentToolbarActionAck, pkt.Seq, wsprotocol.AgentToolbarActionAckPayload{
			SessionID:      payload.SessionID,
			ToolbarID:      payload.ToolbarID,
			ClientActionID: payload.ClientActionID,
			Accepted:       false,
			Code:           "action_failed",
			Msg:            err.Error(),
			UpdatedAt:      nowUnixMilli(),
		})
		return
	}
	conn.SendPayload(wsprotocol.CmdAgentToolbarActionAck, pkt.Seq, agenttoolbar.ToWireActionAck(ack))
}

// notifyWidgetSessionClosed pushes widget_session_closed to the visitor's local
// connections via the hub and to remote nodes via Redis pub/sub.
func notifyWidgetSessionClosed(hub HubInterface, visitorID int64, sessionID, reason string) {
	if hub == nil || visitorID <= 0 {
		return
	}
	pl := wsprotocol.WidgetSessionClosedPayload{
		SessionID: sessionID,
		Reason:    reason,
	}
	for _, c := range hub.GetUserConns(visitorID) {
		c.SendPayload(wsprotocol.CmdWidgetSessionClosed, c.NextSeq(), pl)
	}
	ctx := context.Background()
	nodes, _, ok := collectLiveRouteNodes(ctx, visitorID, hub.GetNodeID(), "")
	if !ok || len(nodes) == 0 {
		return
	}
	publishToRouteNodes(ctx, visitorID, wsprotocol.CmdWidgetSessionClosed, pl, "", nodes)
}
