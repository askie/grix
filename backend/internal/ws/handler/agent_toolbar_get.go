package handler

import (
	"context"
	"encoding/json"

	agenttoolbar "github.com/askie/grix/backend/internal/agenttoolbar"
	toolprotocol "github.com/askie/grix/backend/internal/agenttoolbar/protocol"
	wsprotocol "github.com/askie/grix/backend/internal/ws/protocol"
)

func HandleAgentToolbarGet(_ HubInterface, conn ConnInterface, pkt *wsprotocol.Packet) {
	var payload wsprotocol.AgentToolbarGetPayload
	if err := json.Unmarshal(pkt.Payload, &payload); err != nil {
		conn.SendPayload(wsprotocol.CmdAgentToolbarGetResp, pkt.Seq, wsprotocol.AgentToolbarGetRespPayload{
			Code:     400,
			Msg:      "invalid payload",
			Snapshot: agenttoolbar.ToWireSnapshot(toolprotocol.Snapshot{}),
		})
		return
	}
	if payload.TargetAgentID == 0 {
		widgetSession, ok, err := loadOwnedWidgetSession(conn.GetUserID(), payload.SessionID)
		if err != nil {
			conn.SendPayload(wsprotocol.CmdAgentToolbarGetResp, pkt.Seq, wsprotocol.AgentToolbarGetRespPayload{
				Code:     500,
				Msg:      err.Error(),
				Snapshot: wsprotocol.AgentToolbarSnapshotPayload{SessionID: payload.SessionID, Items: []wsprotocol.AgentToolbarItemPayload{}},
			})
			return
		}
		if ok {
			conn.SendPayload(wsprotocol.CmdAgentToolbarGetResp, pkt.Seq, wsprotocol.AgentToolbarGetRespPayload{
				Code:     0,
				Snapshot: buildVisitorToolbarSnapshot(widgetSession),
			})
			return
		}
	}
	svc := agenttoolbar.GetGlobal()
	if svc == nil {
		conn.SendPayload(wsprotocol.CmdAgentToolbarGetResp, pkt.Seq, wsprotocol.AgentToolbarGetRespPayload{
			Code:     503,
			Msg:      "toolbar service unavailable",
			Snapshot: agenttoolbar.ToWireSnapshot(toolprotocol.Snapshot{SessionID: payload.SessionID, Items: []toolprotocol.Item{}}),
		})
		return
	}
	snapshot, err := svc.GetSnapshot(context.Background(), conn.GetUserID(), payload.SessionID, payload.TargetAgentID)
	if err != nil {
		conn.SendPayload(wsprotocol.CmdAgentToolbarGetResp, pkt.Seq, wsprotocol.AgentToolbarGetRespPayload{
			Code:     403,
			Msg:      err.Error(),
			Snapshot: agenttoolbar.ToWireSnapshot(toolprotocol.Snapshot{SessionID: payload.SessionID, Items: []toolprotocol.Item{}}),
		})
		return
	}
	conn.SendPayload(wsprotocol.CmdAgentToolbarGetResp, pkt.Seq, wsprotocol.AgentToolbarGetRespPayload{
		Code:     0,
		Snapshot: agenttoolbar.ToWireSnapshot(snapshot),
	})
}
