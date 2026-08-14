package agentapi

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/askie/grix/backend/internal/ws/protocol"
	"github.com/stretchr/testify/require"
)

func requireNoStopProtocolError(t *testing.T, conn *agentConn) {
	t.Helper()
	select {
	case raw := <-conn.send:
		var pkt protocol.Packet
		require.NoError(t, json.Unmarshal(raw, &pkt))
		t.Fatalf("unexpected response cmd=%q payload=%s", pkt.Cmd, string(pkt.Payload))
	default:
	}
}

func TestHandleEventStopSessionScopeAcceptsEmptyEventWithoutRunTransition(t *testing.T) {
	mgr := NewManager("", 30*time.Second, nil, nil, nil, nil)
	defer mgr.Shutdown()

	conn := &agentConn{
		agentID:  701,
		ownerID:  801,
		clientID: "session-stop-test",
		send:     make(chan []byte, 8),
	}
	const (
		eventID   = "evt-live-during-session-stop"
		sessionID = "session-composing"
	)
	mgr.registerActiveRun(DelegateEventPayload{
		EventID:   eventID,
		EventType: "user_chat",
		AgentID:   conn.agentID,
		OwnerID:   conn.ownerID,
		SessionID: sessionID,
		MsgID:     901,
		SenderID:  conn.ownerID,
	})
	before := mgr.LookupActiveRun(eventID)
	require.NotNil(t, before)

	mgr.handleEventStopAck(conn, makePacket(t, protocol.CmdEventStopAck, 1, protocol.AgentEventStopAckPayload{
		StopID:    "stop-session-empty-ack",
		SessionID: sessionID,
		Scope:     protocol.AgentEventStopScopeSession,
		Accepted:  true,
	}))
	requireNoStopProtocolError(t, conn)

	// When the connector can resolve exactly one active event it echoes that
	// event ID, but session-scoped stop is still a command outcome rather than
	// terminal evidence for the tracked event.
	mgr.handleEventStopResult(conn, makePacket(t, protocol.CmdEventStopResult, 2, protocol.AgentEventStopResultPayload{
		StopID:    "stop-session-resolved",
		EventID:   eventID,
		SessionID: sessionID,
		Scope:     protocol.AgentEventStopScopeSession,
		Status:    "stopped",
	}))
	requireNoStopProtocolError(t, conn)

	after := mgr.LookupActiveRun(eventID)
	require.NotNil(t, after, "session-scoped stop result must not remove a tracked run")
	require.Equal(t, before.State, after.State, "session-scoped stop result must not terminalize a tracked run")

	// The idle/no-active connector response legitimately has no event ID.
	mgr.handleEventStopResult(conn, makePacket(t, protocol.CmdEventStopResult, 3, protocol.AgentEventStopResultPayload{
		StopID:    "stop-session-idle",
		SessionID: sessionID,
		Scope:     protocol.AgentEventStopScopeSession,
		Status:    "already_finished",
	}))
	requireNoStopProtocolError(t, conn)
	require.NotNil(t, mgr.LookupActiveRun(eventID))
}

func TestHandleEventStopEventScopeStillRequiresEventID(t *testing.T) {
	mgr := NewManager("", 30*time.Second, nil, nil, nil, nil)
	defer mgr.Shutdown()
	conn := &agentConn{agentID: 702, ownerID: 802, send: make(chan []byte, 2)}

	mgr.handleEventStopAck(conn, makePacket(t, protocol.CmdEventStopAck, 4, protocol.AgentEventStopAckPayload{
		StopID:   "stop-event-empty",
		Accepted: true,
	}))

	select {
	case raw := <-conn.send:
		var pkt protocol.Packet
		require.NoError(t, json.Unmarshal(raw, &pkt))
		require.Equal(t, "error", pkt.Cmd)
	default:
		t.Fatal("event-scoped stop ACK without event_id must still be rejected")
	}
}
