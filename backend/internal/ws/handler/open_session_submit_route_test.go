package handler

import (
	"context"
	"testing"
	"time"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/store"
	wsagentapi "github.com/askie/grix/backend/internal/ws/agentapi"
)

func TestResolveGroupOpenSessionSubmitTargetPrefersQuotedCard(t *testing.T) {
	cleanup := setupSendMsgTest(t)
	defer cleanup()

	const (
		sessionID = "session-open-session-route"
		ownerID   = int64(92001)
		agentAID  = int64(92011)
		agentBID  = int64(92012)
		cardAMsg  = int64(18889996001)
		cardBMsg  = int64(18889996002)
	)

	if err := store.DB.Create(&model.Session{
		SessionID:   sessionID,
		OwnerID:     ownerID,
		SessionType: 2,
	}).Error; err != nil {
		t.Fatalf("create session error: %v", err)
	}
	if err := store.DB.Create(&[]model.Agent{
		{
			ID:           agentAID,
			OwnerID:      ownerID,
			AgentName:    "agent-a",
			ProviderType: model.AgentProviderAPI,
			Status:       1,
		},
		{
			ID:           agentBID,
			OwnerID:      ownerID,
			AgentName:    "agent-b",
			ProviderType: model.AgentProviderAPI,
			Status:       1,
		},
	}).Error; err != nil {
		t.Fatalf("create agents error: %v", err)
	}
	if err := store.DB.Create(&[]model.SessionMember{
		{SessionID: sessionID, MemberID: ownerID, MemberType: 1},
		{SessionID: sessionID, MemberID: agentAID, MemberType: 2},
		{SessionID: sessionID, MemberID: agentBID, MemberType: 2},
	}).Error; err != nil {
		t.Fatalf("create session members error: %v", err)
	}

	now := time.Now().UTC()
	if err := store.DB.Create(&[]model.Message{
		{
			MsgID:      cardAMsg,
			SessionID:  sessionID,
			SenderID:   agentAID,
			SenderType: 2,
			MsgType:    1,
			Content:    "[Open Workspace](grix://card/agent_open_session?summary_text=agent-a)",
			CreatedAt:  now.Add(-time.Minute),
		},
		{
			MsgID:      cardBMsg,
			SessionID:  sessionID,
			SenderID:   agentBID,
			SenderType: 2,
			MsgType:    1,
			Content:    "[Open Workspace](grix://card/agent_open_session?summary_text=agent-b)",
			CreatedAt:  now,
		},
	}).Error; err != nil {
		t.Fatalf("create card messages error: %v", err)
	}

	ctx := context.Background()
	wsagentapi.SaveBindingCardMsgID(ctx, agentAID, sessionID, cardAMsg)
	wsagentapi.SaveBindingCardMsgID(ctx, agentBID, sessionID, cardBMsg)

	got := resolveGroupOpenSessionSubmitTarget(ctx, sessionID, cardAMsg)
	if got != agentAID {
		t.Fatalf("quoted target agent=%d want=%d", got, agentAID)
	}

	got = resolveGroupOpenSessionSubmitTarget(ctx, sessionID, 0)
	if got != agentBID {
		t.Fatalf("fallback target agent=%d want=%d", got, agentBID)
	}
}
