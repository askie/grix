package service

import (
	"strconv"
	"testing"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/snowflake"
	"github.com/askie/grix/backend/internal/pkg/testutil"
	"github.com/askie/grix/backend/internal/store"
)

func setupAgentClientTypeServiceTest(t *testing.T) func() {
	t.Helper()

	testDB := testutil.NewTestDB()
	store.DB = testDB.DB
	store.RDB = testutil.NewMockRedis()
	if err := snowflake.Init(1); err != nil {
		t.Fatalf("init snowflake error: %v", err)
	}

	return func() {
		testDB.Close()
	}
}

func seedAgentClientTypeOwner(t *testing.T, ownerID int64) {
	t.Helper()

	if err := store.DB.Create(&model.User{
		ID:           ownerID,
		Username:     "client_type_owner_" + strconv.FormatInt(ownerID, 10),
		Email:        "client_type_owner_" + strconv.FormatInt(ownerID, 10) + "@example.com",
		PasswordHash: "x",
		AuthProvider: "local",
		Nickname:     "Owner",
		Status:       model.UserStatusActive,
	}).Error; err != nil {
		t.Fatalf("seed owner error: %v", err)
	}
}

func TestAgentCreate_PersistsHermesClientType(t *testing.T) {
	cleanup := setupAgentClientTypeServiceTest(t)
	defer cleanup()

	const ownerID = int64(94101)
	seedAgentClientTypeOwner(t, ownerID)

	resp, ec := AgentCreate(ownerID, AgentCreateReq{
		AgentName:       "hermes-api-agent",
		ProviderType:    model.AgentProviderAPI,
		AgentClientType: " Hermes ",
	})
	if ec != nil {
		t.Fatalf("AgentCreate error: %+v", ec)
	}
	if resp.AgentClientType != model.AgentClientTypeHermes {
		t.Fatalf("expected response agent_client_type=%q, got %q", model.AgentClientTypeHermes, resp.AgentClientType)
	}

	var agent model.Agent
	if err := store.DB.First(&agent, resp.ID).Error; err != nil {
		t.Fatalf("query agent error: %v", err)
	}
	if agent.AgentClientType != model.AgentClientTypeHermes {
		t.Fatalf("expected stored agent_client_type=%q, got %q", model.AgentClientTypeHermes, agent.AgentClientType)
	}
}

func TestAgentCreate_PersistsGeminiClientType(t *testing.T) {
	cleanup := setupAgentClientTypeServiceTest(t)
	defer cleanup()

	const ownerID = int64(94105)
	seedAgentClientTypeOwner(t, ownerID)

	resp, ec := AgentCreate(ownerID, AgentCreateReq{
		AgentName:       "gemini-api-agent",
		ProviderType:    model.AgentProviderAPI,
		AgentClientType: " Gemini ",
	})
	if ec != nil {
		t.Fatalf("AgentCreate error: %+v", ec)
	}
	if resp.AgentClientType != model.AgentClientTypeGemini {
		t.Fatalf("expected response agent_client_type=%q, got %q", model.AgentClientTypeGemini, resp.AgentClientType)
	}

	var agent model.Agent
	if err := store.DB.First(&agent, resp.ID).Error; err != nil {
		t.Fatalf("query agent error: %v", err)
	}
	if agent.AgentClientType != model.AgentClientTypeGemini {
		t.Fatalf("expected stored agent_client_type=%q, got %q", model.AgentClientTypeGemini, agent.AgentClientType)
	}
}

func TestAgentCreate_PersistsQwenClientType(t *testing.T) {
	cleanup := setupAgentClientTypeServiceTest(t)
	defer cleanup()

	const ownerID = int64(94111)
	seedAgentClientTypeOwner(t, ownerID)

	resp, ec := AgentCreate(ownerID, AgentCreateReq{
		AgentName:       "qwen-api-agent",
		ProviderType:    model.AgentProviderAPI,
		AgentClientType: " Qwen ",
	})
	if ec != nil {
		t.Fatalf("AgentCreate error: %+v", ec)
	}
	if resp.AgentClientType != model.AgentClientTypeQwen {
		t.Fatalf("expected response agent_client_type=%q, got %q", model.AgentClientTypeQwen, resp.AgentClientType)
	}

	var agent model.Agent
	if err := store.DB.First(&agent, resp.ID).Error; err != nil {
		t.Fatalf("query agent error: %v", err)
	}
	if agent.AgentClientType != model.AgentClientTypeQwen {
		t.Fatalf("expected stored agent_client_type=%q, got %q", model.AgentClientTypeQwen, agent.AgentClientType)
	}
}

func TestAgentCreate_RejectsInvalidClientType(t *testing.T) {
	cleanup := setupAgentClientTypeServiceTest(t)
	defer cleanup()

	const ownerID = int64(94102)
	seedAgentClientTypeOwner(t, ownerID)

	_, ec := AgentCreate(ownerID, AgentCreateReq{
		AgentName:       "invalid-client-type-agent",
		ProviderType:    model.AgentProviderAPI,
		AgentClientType: "unknown-client",
	})
	if ec == nil {
		t.Fatal("expected invalid client type error")
	}
	if ec.BizCode != 10003 {
		t.Fatalf("expected biz code 10003, got %d", ec.BizCode)
	}
	if ec.Msg != "agent_client_type 非法" {
		t.Fatalf("unexpected error message: %q", ec.Msg)
	}
}

func TestAgentCreate_RejectsClientTypeForNonAPIProvider(t *testing.T) {
	cleanup := setupAgentClientTypeServiceTest(t)
	defer cleanup()

	const ownerID = int64(94103)
	seedAgentClientTypeOwner(t, ownerID)

	_, ec := AgentCreate(ownerID, AgentCreateReq{
		AgentName:       "remote-client-type-agent",
		ProviderType:    model.AgentProviderRemote,
		AgentClientType: model.AgentClientTypeHermes,
	})
	if ec == nil {
		t.Fatal("expected non-api provider error")
	}
	if ec.BizCode != 10003 {
		t.Fatalf("expected biz code 10003, got %d", ec.BizCode)
	}
	if ec.Msg != "agent_client_type 仅支持 provider_type=3" {
		t.Fatalf("unexpected error message: %q", ec.Msg)
	}
}

func TestAgentUpdate_AllowsSettingHermesClientType(t *testing.T) {
	cleanup := setupAgentClientTypeServiceTest(t)
	defer cleanup()

	const (
		ownerID = int64(94104)
		agentID = int64(95104)
	)
	seedAgentClientTypeOwner(t, ownerID)
	if err := store.DB.Create(&model.Agent{
		ID:           agentID,
		AgentName:    "update-client-type-agent",
		OwnerID:      ownerID,
		ProviderType: model.AgentProviderAPI,
		Status:       model.AgentStatusActive,
	}).Error; err != nil {
		t.Fatalf("seed agent error: %v", err)
	}

	value := " Hermes "
	resp, ec := AgentUpdate(ownerID, agentID, AgentUpdateReq{
		AgentClientType: &value,
	})
	if ec != nil {
		t.Fatalf("AgentUpdate error: %+v", ec)
	}
	if resp.AgentClientType != model.AgentClientTypeHermes {
		t.Fatalf("expected response agent_client_type=%q, got %q", model.AgentClientTypeHermes, resp.AgentClientType)
	}

	var agent model.Agent
	if err := store.DB.First(&agent, agentID).Error; err != nil {
		t.Fatalf("query agent error: %v", err)
	}
	if agent.AgentClientType != model.AgentClientTypeHermes {
		t.Fatalf("expected stored agent_client_type=%q, got %q", model.AgentClientTypeHermes, agent.AgentClientType)
	}
}
