package service

import (
	"testing"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/store"
)

func TestAgentAPISystemPromptLifecycle(t *testing.T) {
	cleanup := setupAgentClientTypeServiceTest(t)
	defer cleanup()

	const ownerID = int64(94201)
	seedAgentClientTypeOwner(t, ownerID)

	created, ec := AgentCreate(ownerID, AgentCreateReq{
		AgentName:       "deepseek-prompt-agent",
		ProviderType:    model.AgentProviderAPI,
		AgentClientType: model.AgentClientTypeDeepSeek,
		SystemPrompt:    "business prompt for lifecycle test",
	})
	if ec != nil {
		t.Fatalf("AgentCreate() error=%+v", ec)
	}
	if created.SystemPrompt != "business prompt for lifecycle test" {
		t.Fatalf("created system prompt=%q", created.SystemPrompt)
	}

	remote, ec := AgentCreate(ownerID, AgentCreateReq{
		AgentName:    "convert-to-deepseek",
		ProviderType: model.AgentProviderRemote,
		SystemPrompt: "keep this prompt",
	})
	if ec != nil {
		t.Fatalf("create remote error=%+v", ec)
	}
	providerAPI := model.AgentProviderAPI
	clientType := model.AgentClientTypeDeepSeek
	converted, ec := AgentUpdate(ownerID, remote.ID, AgentUpdateReq{
		ProviderType: &providerAPI, AgentClientType: &clientType,
	})
	if ec != nil {
		t.Fatalf("convert error=%+v", ec)
	}
	if converted.SystemPrompt != "keep this prompt" || converted.AgentClientType != model.AgentClientTypeDeepSeek {
		t.Fatalf("converted prompt=%q client_type=%q", converted.SystemPrompt, converted.AgentClientType)
	}

	empty := ""
	cleared, ec := AgentUpdate(ownerID, remote.ID, AgentUpdateReq{SystemPrompt: &empty})
	if ec != nil {
		t.Fatalf("clear error=%+v", ec)
	}
	if cleared.SystemPrompt != "" {
		t.Fatalf("cleared system prompt=%q", cleared.SystemPrompt)
	}

	var stored model.Agent
	if err := store.DB.First(&stored, remote.ID).Error; err != nil {
		t.Fatalf("load converted agent: %v", err)
	}
	if stored.SystemPrompt != "" {
		t.Fatalf("stored system prompt=%q", stored.SystemPrompt)
	}
}
