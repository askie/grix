package agentapi

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/askie/grix/backend/internal/agentadapter/hermes"
	"github.com/askie/grix/backend/internal/ws/protocol"
)

func TestHermesContract_ValidateAuthPayload(t *testing.T) {
	validPayload := AuthPayload{
		ClientType:      hermes.Family,
		HostType:        hermes.Family,
		ProtocolVersion: agentAPIProtocolVersion,
		ContractVersion: 1,
		Capabilities:    []string{"stream_chunk", "session_route", "local_action_v1"},
	}
	if msg := validateHermesAuthPayload(validPayload); msg != "" {
		t.Fatalf("validateHermesAuthPayload(valid)=%q want empty", msg)
	}

	tests := []struct {
		name    string
		payload AuthPayload
		wantMsg string
	}{
		{
			name: "missing protocol version",
			payload: AuthPayload{
				ClientType:      hermes.Family,
				ContractVersion: 1,
				Capabilities:    []string{"local_action_v1"},
			},
			wantMsg: "protocol_version must be aibot-agent-api-v1",
		},
		{
			name: "bad contract version",
			payload: AuthPayload{
				ClientType:      hermes.Family,
				ProtocolVersion: agentAPIProtocolVersion,
				ContractVersion: 0,
				Capabilities:    []string{"local_action_v1"},
			},
			wantMsg: "contract_version must be 1",
		},
		{
			name: "missing local action capability",
			payload: AuthPayload{
				ClientType:      hermes.Family,
				ProtocolVersion: agentAPIProtocolVersion,
				ContractVersion: 1,
				Capabilities:    []string{"session_route"},
			},
			wantMsg: "local_action_v1 capability required for hermes",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := validateHermesAuthPayload(tt.payload); got != tt.wantMsg {
				t.Fatalf("validateHermesAuthPayload()=%q want=%q", got, tt.wantMsg)
			}
		})
	}
}

func TestHermesContract_AllowedClientCommandsMatchBaseline(t *testing.T) {
	allowed := []string{
		protocol.CmdPing,
		protocol.CmdPong,
		protocol.CmdEventAck,
		protocol.CmdEventResult,
		protocol.CmdEventStopAck,
		protocol.CmdEventStopResult,
		protocol.CmdEventState,
		protocol.CmdAuditState,
		protocol.CmdEventCancelResult,
		protocol.CmdQueueClearResult,
		protocol.CmdQueueReorderResult,
		protocol.CmdEventHoldResult,
		protocol.CmdQueueEditResult,
		protocol.CmdQueueSnapshot,
		protocol.CmdSendMsg,
		protocol.CmdClientStreamChunk,
		protocol.CmdEditMsg,
		protocol.CmdUpdateBindingCard,
		protocol.CmdSessionActivitySet,
		protocol.CmdLocalActionResult,
		cmdSessionRouteBind,
		cmdSessionRouteResolve,
		protocol.CmdAgentInvoke,
		"error",
	}
	for _, cmd := range allowed {
		if !isHermesAllowedClientCommand(cmd) {
			t.Fatalf("expected Hermes to allow cmd=%s", cmd)
		}
	}

	disallowed := []string{
		"delete_msg",
		"react_msg",
		"kicked",
		"local_action_ack",
		"agent_state_sync",
	}
	for _, cmd := range disallowed {
		if isHermesAllowedClientCommand(cmd) {
			t.Fatalf("expected Hermes to reject cmd=%s", cmd)
		}
	}
}

func TestHermesContract_AllowedStatusesMatchBaseline(t *testing.T) {
	for _, status := range []string{protocol.AgentEventResultResponded, protocol.AgentEventResultFailed, protocol.AgentEventResultCanceled} {
		if !isHermesAllowedEventResultStatus(status) {
			t.Fatalf("expected Hermes event_result status=%s to be allowed", status)
		}
	}
	for _, status := range []string{"", "timeout"} {
		if isHermesAllowedEventResultStatus(status) {
			t.Fatalf("expected Hermes event_result status=%s to be rejected", status)
		}
	}

	for _, status := range []string{"ok", "failed", "unsupported"} {
		if !isHermesAllowedLocalActionResultStatus(status) {
			t.Fatalf("expected Hermes local_action_result status=%s to be allowed", status)
		}
	}
	for _, status := range []string{"timeout", "", "canceled"} {
		if isHermesAllowedLocalActionResultStatus(status) {
			t.Fatalf("expected Hermes local_action_result status=%s to be rejected", status)
		}
	}
}

// 回归：hermes 连接上报 event_result(canceled) 不得再被 4001 硬拒，
// 必须走正常结算（run 清除 + canceled 终态投递），否则工具栏停止按钮残留。
func TestHermesContract_EventResultCanceledSettlesRun(t *testing.T) {
	withoutDurableStores(t)
	mgr := NewManager("", 30*time.Second, nil, nil, nil, nil)
	defer mgr.Shutdown()

	event := DelegateEventPayload{
		EventID:   "evt-hermes-canceled",
		AgentID:   100,
		OwnerID:   200,
		SessionID: "sess-hermes-canceled",
		MsgID:     300,
	}
	mgr.registerPendingEventAck(event, 1)
	mgr.registerActiveRun(event)

	conn := &agentConn{
		agentID:   event.AgentID,
		ownerID:   event.OwnerID,
		clientID:  "hermes-agent",
		send:      make(chan []byte, 64),
		adapter:   hermes.NewAdapter(),
		adapterID: hermes.AdapterID,
	}

	var deliveryStatuses []string
	mgr.SetDeliveryStatusHandler(func(payload protocol.AgentDeliveryStatusPayload) {
		deliveryStatuses = append(deliveryStatuses, payload.Status)
	})

	mgr.handleEventResult(conn, makePacket(t, protocol.CmdEventResult, 2, EventResultPayload{
		EventID: event.EventID,
		Status:  protocol.AgentEventResultCanceled,
	}))

drainLoop:
	for {
		select {
		case raw := <-conn.send:
			var out protocol.Packet
			if err := json.Unmarshal(raw, &out); err != nil {
				t.Fatalf("unmarshal outbound packet: %v", err)
			}
			if out.Cmd == "error" {
				t.Fatalf("hermes event_result(canceled) got error nack: %s", raw)
			}
		default:
			break drainLoop
		}
	}

	if mgr.hasActiveRunForSessionOwner(event.SessionID, event.OwnerID) {
		t.Fatalf("active run should be settled after event_result(canceled)")
	}

	settled := false
	for _, status := range deliveryStatuses {
		if status == protocol.AgentDeliveryStatusCanceled {
			settled = true
		}
	}
	if !settled {
		t.Fatalf("delivery statuses=%v want canceled terminal", deliveryStatuses)
	}
}
