package agentapi

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/askie/grix/backend/internal/ws/protocol"
)

func TestHandleAgentInvoke_RejectsInvalidPayload(t *testing.T) {
	mgr := NewManager("", 30*time.Second, nil, nil, nil, nil)
	defer mgr.Shutdown()
	conn := &agentConn{
		agentID:  1001,
		ownerID:  2001,
		clientID: "agent-invoke-invalid",
		send:     make(chan []byte, 1),
	}

	mgr.handleAgentInvoke(conn, &protocol.Packet{
		Cmd:     protocol.CmdAgentInvoke,
		Seq:     1,
		Payload: json.RawMessage(`{"invoke_id":`),
	})

	select {
	case data := <-conn.send:
		var pkt protocol.Packet
		if err := json.Unmarshal(data, &pkt); err != nil {
			t.Fatalf("unmarshal packet: %v", err)
		}
		if pkt.Cmd != protocol.CmdAgentInvokeResult {
			t.Fatalf("packet cmd=%s want=%s", pkt.Cmd, protocol.CmdAgentInvokeResult)
		}
		var payload protocol.AgentInvokeResultPayload
		if err := json.Unmarshal(pkt.Payload, &payload); err != nil {
			t.Fatalf("unmarshal payload: %v", err)
		}
		if payload.Code != 4001 || payload.Msg != "invalid agent_invoke payload" {
			t.Fatalf("payload=%+v", payload)
		}
	default:
		t.Fatal("expected agent_invoke_result packet")
	}
}

func TestHandleAgentInvoke_RejectsMissingAction(t *testing.T) {
	mgr := NewManager("", 30*time.Second, nil, nil, nil, nil)
	defer mgr.Shutdown()
	conn := &agentConn{
		agentID:  1002,
		ownerID:  2002,
		clientID: "agent-invoke-missing-action",
		send:     make(chan []byte, 1),
	}

	mgr.handleAgentInvoke(conn, makePacket(t, protocol.CmdAgentInvoke, 1, protocol.AgentInvokePayload{
		InvokeID: "invoke-1",
		Action:   "",
	}))

	select {
	case data := <-conn.send:
		var pkt protocol.Packet
		if err := json.Unmarshal(data, &pkt); err != nil {
			t.Fatalf("unmarshal packet: %v", err)
		}
		var payload protocol.AgentInvokeResultPayload
		if err := json.Unmarshal(pkt.Payload, &payload); err != nil {
			t.Fatalf("unmarshal payload: %v", err)
		}
		if payload.InvokeID != "invoke-1" || payload.Code != 4001 || payload.Msg != "action required" {
			t.Fatalf("payload=%+v", payload)
		}
	default:
		t.Fatal("expected agent_invoke_result packet")
	}
}

func TestHandleAgentInvoke_RejectsUnknownAction(t *testing.T) {
	mgr := NewManager("", 30*time.Second, nil, nil, nil, nil)
	defer mgr.Shutdown()
	conn := &agentConn{
		agentID:  1003,
		ownerID:  2003,
		clientID: "agent-invoke-unknown-action",
		send:     make(chan []byte, 1),
	}

	mgr.handleAgentInvoke(conn, makePacket(t, protocol.CmdAgentInvoke, 1, protocol.AgentInvokePayload{
		InvokeID: "invoke-unknown",
		Action:   "not_registered",
	}))

	select {
	case data := <-conn.send:
		var pkt protocol.Packet
		if err := json.Unmarshal(data, &pkt); err != nil {
			t.Fatalf("unmarshal packet: %v", err)
		}
		var payload protocol.AgentInvokeResultPayload
		if err := json.Unmarshal(pkt.Payload, &payload); err != nil {
			t.Fatalf("unmarshal payload: %v", err)
		}
		if payload.InvokeID != "invoke-unknown" || payload.Code != 4004 || payload.Msg != "unknown action: not_registered" {
			t.Fatalf("payload=%+v", payload)
		}
	default:
		t.Fatal("expected agent_invoke_result packet")
	}
}
