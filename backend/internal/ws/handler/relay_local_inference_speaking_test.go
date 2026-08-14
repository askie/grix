package handler

import (
	"testing"
	"time"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/store"
	"github.com/askie/grix/backend/internal/ws/protocol"
)

func seedRelayLocalSpeakingFixture(
	t *testing.T,
	session model.Session,
	members []model.SessionMember,
	agent model.Agent,
	trigger model.Message,
) {
	t.Helper()

	records := []any{&session}
	for i := range members {
		member := members[i]
		records = append(records, &member)
	}
	if agent.ID > 0 {
		records = append(records, &agent)
	}
	if trigger.MsgID > 0 {
		records = append(records, &trigger)
	}
	for _, record := range records {
		if err := store.DB.Create(record).Error; err != nil {
			t.Fatalf("create fixture error: %v", err)
		}
	}
}

func TestHandleRelayLocalStreamStartRejectsMutedAgent(t *testing.T) {
	cleanup := setupSendMsgTest(t)
	defer cleanup()

	const (
		sessionID    = "session-local-speaking-1"
		ownerID      = int64(66101)
		agentID      = int64(66102)
		triggerMsgID = int64(66103)
	)

	now := time.Now()
	seedRelayLocalSpeakingFixture(
		t,
		model.Session{
			SessionID:      sessionID,
			SessionType:    model.SessionTypeGroup,
			OwnerID:        ownerID,
			LastMsgSummary: "group",
			CreatedAt:      now,
			UpdatedAt:      now,
		},
		[]model.SessionMember{
			{
				SessionID:    sessionID,
				MemberID:     ownerID,
				MemberType:   1,
				Role:         3,
				LastActiveAt: now,
				JoinedAt:     now,
			},
			{
				SessionID:    sessionID,
				MemberID:     agentID,
				MemberType:   2,
				Role:         1,
				IsSpeakMuted: true,
				LastActiveAt: now,
				JoinedAt:     now,
			},
		},
		model.Agent{
			ID:            agentID,
			OwnerID:       ownerID,
			AgentName:     "local-muted-agent",
			ProviderType:  model.AgentProviderLocal,
			LocalEndpoint: "http://127.0.0.1:11434",
			Status:        model.AgentStatusActive,
			CreatedAt:     now,
			UpdatedAt:     now,
		},
		model.Message{
			MsgID:      triggerMsgID,
			SessionID:  sessionID,
			SenderID:   ownerID,
			SenderType: 1,
			MsgType:    1,
			Content:    "trigger",
			CreatedAt:  now,
		},
	)

	conn := &sendMsgMockConn{userID: ownerID, deviceID: "dev-owner"}
	hub := &sendMsgMockHub{nodeID: "node-a", conns: map[int64][]ConnInterface{
		ownerID: {conn},
	}}

	HandleRelayLocalStreamStart(
		hub,
		conn,
		makeRelayLocalStreamPacket(t, protocol.CmdRelayLocalStreamStart, protocol.RelayLocalStreamStartPayload{
			SessionID:    sessionID,
			AgentID:      agentID,
			TriggerMsgID: triggerMsgID,
		}),
	)

	if len(conn.sent) != 1 || conn.sent[0].cmd != protocol.CmdRelayLocalStreamStartAck {
		t.Fatalf("expected start ack, got=%#v", conn.sent)
	}
	ack := conn.sent[0].payload.(protocol.RelayLocalStreamStartAckPayload)
	if ack.Code != 403 || ack.Msg != "member is muted" {
		t.Fatalf("unexpected ack payload=%#v", ack)
	}
}

func TestHandleRelayLocalStreamStartRejectsMutedRequester(t *testing.T) {
	cleanup := setupSendMsgTest(t)
	defer cleanup()

	const (
		sessionID    = "session-local-speaking-2"
		ownerID      = int64(66201)
		memberID     = int64(66202)
		agentID      = int64(66203)
		triggerMsgID = int64(66204)
	)

	now := time.Now()
	seedRelayLocalSpeakingFixture(
		t,
		model.Session{
			SessionID:       sessionID,
			SessionType:     model.SessionTypeGroup,
			OwnerID:         ownerID,
			AllMembersMuted: true,
			LastMsgSummary:  "group",
			CreatedAt:       now,
			UpdatedAt:       now,
		},
		[]model.SessionMember{
			{
				SessionID:    sessionID,
				MemberID:     ownerID,
				MemberType:   1,
				Role:         3,
				LastActiveAt: now,
				JoinedAt:     now,
			},
			{
				SessionID:    sessionID,
				MemberID:     memberID,
				MemberType:   1,
				Role:         1,
				LastActiveAt: now,
				JoinedAt:     now,
			},
			{
				SessionID:    sessionID,
				MemberID:     agentID,
				MemberType:   2,
				Role:         1,
				LastActiveAt: now,
				JoinedAt:     now,
			},
		},
		model.Agent{
			ID:            agentID,
			OwnerID:       ownerID,
			AgentName:     "local-agent",
			ProviderType:  model.AgentProviderLocal,
			LocalEndpoint: "http://127.0.0.1:11434",
			Status:        model.AgentStatusActive,
			CreatedAt:     now,
			UpdatedAt:     now,
		},
		model.Message{
			MsgID:      triggerMsgID,
			SessionID:  sessionID,
			SenderID:   memberID,
			SenderType: 1,
			MsgType:    1,
			Content:    "trigger",
			CreatedAt:  now,
		},
	)

	conn := &sendMsgMockConn{userID: memberID, deviceID: "dev-member"}
	hub := &sendMsgMockHub{nodeID: "node-a", conns: map[int64][]ConnInterface{
		memberID: {conn},
	}}

	HandleRelayLocalStreamStart(
		hub,
		conn,
		makeRelayLocalStreamPacket(t, protocol.CmdRelayLocalStreamStart, protocol.RelayLocalStreamStartPayload{
			SessionID:    sessionID,
			AgentID:      agentID,
			TriggerMsgID: triggerMsgID,
		}),
	)

	if len(conn.sent) != 1 || conn.sent[0].cmd != protocol.CmdRelayLocalStreamStartAck {
		t.Fatalf("expected start ack, got=%#v", conn.sent)
	}
	ack := conn.sent[0].payload.(protocol.RelayLocalStreamStartAckPayload)
	if ack.Code != 403 || ack.Msg != "group is muted" {
		t.Fatalf("unexpected ack payload=%#v", ack)
	}
}

func TestHandleRelayLocalStreamStartRejectsNonLocalAgent(t *testing.T) {
	cleanup := setupSendMsgTest(t)
	defer cleanup()

	const (
		sessionID    = "session-local-speaking-3"
		ownerID      = int64(66301)
		agentID      = int64(66302)
		triggerMsgID = int64(66303)
	)

	now := time.Now()
	seedRelayLocalSpeakingFixture(
		t,
		model.Session{
			SessionID:      sessionID,
			SessionType:    model.SessionTypeGroup,
			OwnerID:        ownerID,
			LastMsgSummary: "group",
			CreatedAt:      now,
			UpdatedAt:      now,
		},
		[]model.SessionMember{
			{
				SessionID:    sessionID,
				MemberID:     ownerID,
				MemberType:   1,
				Role:         3,
				LastActiveAt: now,
				JoinedAt:     now,
			},
			{
				SessionID:    sessionID,
				MemberID:     agentID,
				MemberType:   2,
				Role:         1,
				LastActiveAt: now,
				JoinedAt:     now,
			},
		},
		model.Agent{
			ID:           agentID,
			OwnerID:      ownerID,
			AgentName:    "remote-agent",
			ProviderType: model.AgentProviderRemote,
			Status:       model.AgentStatusActive,
			CreatedAt:    now,
			UpdatedAt:    now,
		},
		model.Message{
			MsgID:      triggerMsgID,
			SessionID:  sessionID,
			SenderID:   ownerID,
			SenderType: 1,
			MsgType:    1,
			Content:    "trigger",
			CreatedAt:  now,
		},
	)

	conn := &sendMsgMockConn{userID: ownerID, deviceID: "dev-owner"}
	hub := &sendMsgMockHub{nodeID: "node-a", conns: map[int64][]ConnInterface{
		ownerID: {conn},
	}}

	HandleRelayLocalStreamStart(
		hub,
		conn,
		makeRelayLocalStreamPacket(t, protocol.CmdRelayLocalStreamStart, protocol.RelayLocalStreamStartPayload{
			SessionID:    sessionID,
			AgentID:      agentID,
			TriggerMsgID: triggerMsgID,
		}),
	)

	if len(conn.sent) != 1 || conn.sent[0].cmd != protocol.CmdRelayLocalStreamStartAck {
		t.Fatalf("expected start ack, got=%#v", conn.sent)
	}
	ack := conn.sent[0].payload.(protocol.RelayLocalStreamStartAckPayload)
	if ack.Code != 403 || ack.Msg != errLocalInferenceAgentUnavailable.Error() {
		t.Fatalf("unexpected ack payload=%#v", ack)
	}
}

func TestHandleRelayLocalStreamStartRejectsInvalidTriggerMessage(t *testing.T) {
	cleanup := setupSendMsgTest(t)
	defer cleanup()

	const (
		sessionID    = "session-local-speaking-4"
		ownerID      = int64(66401)
		agentID      = int64(66402)
		triggerMsgID = int64(66403)
	)

	now := time.Now()
	seedRelayLocalSpeakingFixture(
		t,
		model.Session{
			SessionID:      sessionID,
			SessionType:    model.SessionTypeGroup,
			OwnerID:        ownerID,
			LastMsgSummary: "group",
			CreatedAt:      now,
			UpdatedAt:      now,
		},
		[]model.SessionMember{
			{
				SessionID:    sessionID,
				MemberID:     ownerID,
				MemberType:   1,
				Role:         3,
				LastActiveAt: now,
				JoinedAt:     now,
			},
			{
				SessionID:    sessionID,
				MemberID:     agentID,
				MemberType:   2,
				Role:         1,
				LastActiveAt: now,
				JoinedAt:     now,
			},
		},
		model.Agent{
			ID:            agentID,
			OwnerID:       ownerID,
			AgentName:     "local-agent",
			ProviderType:  model.AgentProviderLocal,
			LocalEndpoint: "http://127.0.0.1:11434",
			Status:        model.AgentStatusActive,
			CreatedAt:     now,
			UpdatedAt:     now,
		},
		model.Message{},
	)

	conn := &sendMsgMockConn{userID: ownerID, deviceID: "dev-owner"}
	hub := &sendMsgMockHub{nodeID: "node-a", conns: map[int64][]ConnInterface{
		ownerID: {conn},
	}}

	HandleRelayLocalStreamStart(
		hub,
		conn,
		makeRelayLocalStreamPacket(t, protocol.CmdRelayLocalStreamStart, protocol.RelayLocalStreamStartPayload{
			SessionID:    sessionID,
			AgentID:      agentID,
			TriggerMsgID: triggerMsgID,
		}),
	)

	if len(conn.sent) != 1 || conn.sent[0].cmd != protocol.CmdRelayLocalStreamStartAck {
		t.Fatalf("expected start ack, got=%#v", conn.sent)
	}
	ack := conn.sent[0].payload.(protocol.RelayLocalStreamStartAckPayload)
	if ack.Code != 403 || ack.Msg != errLocalInferenceTriggerInvalid.Error() {
		t.Fatalf("unexpected ack payload=%#v", ack)
	}
}
