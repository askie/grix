package handler

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/store"
	wsagentapi "github.com/askie/grix/backend/internal/ws/agentapi"
	"github.com/askie/grix/backend/internal/ws/protocol"
)

func makeAgentOutputStopPacket(t *testing.T, payload protocol.AgentOutputStopPayload) *protocol.Packet {
	t.Helper()
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal agent_output_stop payload error: %v", err)
	}
	return &protocol.Packet{
		Cmd:     protocol.CmdAgentOutputStop,
		Seq:     103,
		Payload: raw,
	}
}

func findAgentOutputStopAck(sent []sentPayload) (protocol.AgentOutputStopAckPayload, bool) {
	for _, item := range sent {
		if item.cmd != protocol.CmdAgentOutputStopAck {
			continue
		}
		ack, ok := item.payload.(protocol.AgentOutputStopAckPayload)
		if ok {
			return ack, true
		}
	}
	return protocol.AgentOutputStopAckPayload{}, false
}

func TestHandleAgentOutputStopRejectsNonMember(t *testing.T) {
	cleanup := setupSendMsgTest(t)
	defer cleanup()

	const (
		ownerID   = int64(9301)
		sessionID = "agent-output-stop-non-member"
	)
	now := time.Now().UTC()

	prevManager := wsagentapi.GetGlobal()
	wsagentapi.SetGlobal(wsagentapi.NewManager("", time.Second, nil, nil, nil, nil))
	defer wsagentapi.SetGlobal(prevManager)

	if err := store.DB.Create(&model.Session{
		SessionID:   sessionID,
		OwnerID:     ownerID,
		SessionType: model.SessionTypeGroup,
		GroupName:   "Stop Guard Group",
		CreatedAt:   now,
		UpdatedAt:   now,
	}).Error; err != nil {
		t.Fatalf("create session error: %v", err)
	}

	conn := &sendMsgMockConn{userID: ownerID, deviceID: "device-stop-1"}
	HandleAgentOutputStop(nil, conn, makeAgentOutputStopPacket(t, protocol.AgentOutputStopPayload{
		SessionID: sessionID,
		RunID:     "run-stop-guard-1",
	}))

	ack, ok := findAgentOutputStopAck(conn.sent)
	if !ok {
		t.Fatalf("agent_output_stop should send stop ack")
	}
	if ack.Accepted {
		t.Fatalf("stop ack accepted=%t want=false", ack.Accepted)
	}
	if ack.Msg != "permission denied" {
		t.Fatalf("stop ack msg=%q want=%q", ack.Msg, "permission denied")
	}
}

func TestHandleAgentOutputStopRejectsBannedGroup(t *testing.T) {
	cleanup := setupSendMsgTest(t)
	defer cleanup()

	const (
		ownerID   = int64(9302)
		sessionID = "agent-output-stop-banned"
	)
	now := time.Now().UTC()

	prevManager := wsagentapi.GetGlobal()
	wsagentapi.SetGlobal(wsagentapi.NewManager("", time.Second, nil, nil, nil, nil))
	defer wsagentapi.SetGlobal(prevManager)

	if err := store.DB.Create(&model.Session{
		SessionID:        sessionID,
		OwnerID:          ownerID,
		SessionType:      model.SessionTypeGroup,
		GroupName:        "Banned Stop Group",
		ModerationStatus: model.SessionModerationStatusBanned,
		CreatedAt:        now,
		UpdatedAt:        now,
	}).Error; err != nil {
		t.Fatalf("create session error: %v", err)
	}
	if err := store.DB.Create(&model.SessionMember{
		SessionID:    sessionID,
		MemberID:     ownerID,
		MemberType:   1,
		Role:         3,
		JoinedAt:     now,
		LastActiveAt: now,
	}).Error; err != nil {
		t.Fatalf("create session member error: %v", err)
	}

	conn := &sendMsgMockConn{userID: ownerID, deviceID: "device-stop-2"}
	HandleAgentOutputStop(nil, conn, makeAgentOutputStopPacket(t, protocol.AgentOutputStopPayload{
		SessionID: sessionID,
		RunID:     "run-stop-guard-2",
	}))

	ack, ok := findAgentOutputStopAck(conn.sent)
	if !ok {
		t.Fatalf("agent_output_stop should send stop ack")
	}
	if ack.Accepted {
		t.Fatalf("stop ack accepted=%t want=false", ack.Accepted)
	}
	if ack.Msg != "group banned" {
		t.Fatalf("stop ack msg=%q want=%q", ack.Msg, "group banned")
	}
}
