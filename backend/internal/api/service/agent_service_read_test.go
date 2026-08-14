package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/testutil"
	"github.com/askie/grix/backend/internal/store"
	"github.com/askie/grix/backend/internal/ws/protocol"
)

type stubAgentChannelBridge struct {
	onlineByAgentID map[int64]bool
}

func (s stubAgentChannelBridge) PushAgentEvent(agentID, ownerID int64, cmd string, payload interface{}) bool {
	return false
}

func (s stubAgentChannelBridge) PushDelegateEvent(event AgentDelegateEvent) bool {
	return false
}

func (s stubAgentChannelBridge) IsAgentChannelAvailable(agentID int64) bool {
	return s.onlineByAgentID[agentID]
}

func (s stubAgentChannelBridge) GetAgentClientType(agentID int64) string {
	return ""
}

func TestAgentReadResponses_ExposeAgentClientType(t *testing.T) {
	testDB := testutil.NewTestDB()
	defer testDB.Close()

	originalDB := store.DB
	store.DB = testDB.DB
	originalRDB := store.RDB
	store.RDB = testutil.NewMockRedis()
	t.Cleanup(func() {
		store.DB = originalDB
		store.RDB = originalRDB
	})
	SetAgentChannelBridge(stubAgentChannelBridge{
		onlineByAgentID: map[int64]bool{91001: true},
	})
	t.Cleanup(func() {
		SetAgentChannelBridge(nil)
	})

	agent := model.Agent{
		ID:              91001,
		AgentName:       "openclaw-agent",
		Introduction:    "integration-ready api agent",
		OwnerID:         82001,
		ProviderType:    model.AgentProviderAPI,
		AgentClientType: model.AgentClientTypeOpenClaw,
		Status:          model.AgentStatusActive,
	}
	if err := store.DB.Create(&agent).Error; err != nil {
		t.Fatalf("create agent: %v", err)
	}
	nowMs := time.Now().UnixMilli()
	payload, err := json.Marshal(protocol.AgentStateSyncPayload{
		AgentID: agent.ID,
		State:   protocol.AgentStateOnline,
		Extra:   json.RawMessage(fmt.Sprintf(`{"connected":true,"lease_until":%d}`, nowMs+60_000)),
	})
	if err != nil {
		t.Fatalf("marshal agent state: %v", err)
	}
	if err := store.RDB.HSet(context.Background(), fmt.Sprintf("im:agent_state:%d", agent.OwnerID), fmt.Sprintf("%d", agent.ID), payload).Err(); err != nil {
		t.Fatalf("seed agent online state: %v", err)
	}

	list, err := AgentList(agent.OwnerID, nil)
	if err != nil {
		t.Fatalf("AgentList error: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("expected 1 agent, got %d", len(list))
	}
	if list[0].AgentClientType != model.AgentClientTypeOpenClaw {
		t.Fatalf("expected list agent_client_type=%q, got %q", model.AgentClientTypeOpenClaw, list[0].AgentClientType)
	}
	if !list[0].Online {
		t.Fatal("expected list online=true")
	}
	if list[0].Introduction != agent.Introduction {
		t.Fatalf("expected list introduction=%q, got %q", agent.Introduction, list[0].Introduction)
	}
	if !list[0].Online {
		t.Fatal("expected list online=true")
	}

	got, ec := AgentGet(agent.OwnerID, agent.ID)
	if ec != nil {
		t.Fatalf("AgentGet errcode: %+v", ec)
	}
	if got.AgentClientType != model.AgentClientTypeOpenClaw {
		t.Fatalf("expected detail agent_client_type=%q, got %q", model.AgentClientTypeOpenClaw, got.AgentClientType)
	}
	if !got.Online {
		t.Fatal("expected detail online=true")
	}
	if got.Introduction != agent.Introduction {
		t.Fatalf("expected detail introduction=%q, got %q", agent.Introduction, got.Introduction)
	}
	if got.Profile.Introduction != agent.Introduction {
		t.Fatalf("expected profile introduction=%q, got %q", agent.Introduction, got.Profile.Introduction)
	}
	if !got.Online {
		t.Fatal("expected detail online=true")
	}
}

func TestAgentListWithContextHonorsCanceledContext(t *testing.T) {
	testDB := testutil.NewTestDB()
	defer testDB.Close()

	originalDB := store.DB
	store.DB = testDB.DB
	t.Cleanup(func() {
		store.DB = originalDB
	})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := AgentListWithContext(ctx, 82002, nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("AgentListWithContext error = %v, want context canceled", err)
	}
}
