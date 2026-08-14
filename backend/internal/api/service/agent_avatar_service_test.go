package service

import (
	"testing"

	"github.com/askie/grix/backend/config"
	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/testutil"
	"github.com/askie/grix/backend/internal/store"
)

func TestAgentToResp_PopulatesProfileAvatar(t *testing.T) {
	testDB := testutil.NewTestDB()
	defer testDB.Close()

	originalDB := store.DB
	store.DB = testDB.DB
	t.Cleanup(func() {
		store.DB = originalDB
	})

	agent := &model.Agent{
		ID:        1001,
		AgentName: "agent_alpha",
		AvatarURL: "https://cdn.example.com/agent/avatar.jpg",
		OwnerID:   2001,
	}

	resp := agentToResp(agent, agent.OwnerID)

	if resp.AvatarURL != agent.AvatarURL {
		t.Fatalf("expected avatar_url %q, got %q", agent.AvatarURL, resp.AvatarURL)
	}
	if resp.Profile.AvatarURL != agent.AvatarURL {
		t.Fatalf("expected profile.avatar_url %q, got %q", agent.AvatarURL, resp.Profile.AvatarURL)
	}
}

func TestIsAgentAvatarObjectKey_WithStorageDir(t *testing.T) {
	originalConfig := config.C
	t.Cleanup(func() {
		config.C = originalConfig
	})
	config.C.OSS.Avatar.StorageDir = "prod"

	if !isAgentAvatarObjectKey(8, 9, "prod/avatars/agent_9.jpg") {
		t.Fatal("expected object key to match agent avatar prefix")
	}
	if !isAgentAvatarObjectKey(8, 9, "prod/agent/8/9/avatar/10.jpg") {
		t.Fatal("expected legacy object key to match agent avatar prefix")
	}
	if isAgentAvatarObjectKey(8, 9, "prod/avatars/agent_10.jpg") {
		t.Fatal("expected object key to reject another agent avatar path")
	}
}

func TestIsAgentAvatarObjectKey_WithoutStorageDir(t *testing.T) {
	originalConfig := config.C
	t.Cleanup(func() {
		config.C = originalConfig
	})
	config.C.OSS.Avatar.StorageDir = ""

	if !isAgentAvatarObjectKey(8, 9, "avatars/agent_9.jpg") {
		t.Fatal("expected object key to match agent avatar prefix")
	}
	if !isAgentAvatarObjectKey(8, 9, "agent/8/9/avatar/10.jpg") {
		t.Fatal("expected legacy object key to match agent avatar prefix")
	}
	if isAgentAvatarObjectKey(8, 9, "avatars/agent_10.jpg") {
		t.Fatal("expected object key to reject non-avatar path")
	}
}

func TestBuildAgentAvatarObjectKey_UsesStableAvatarPath(t *testing.T) {
	originalConfig := config.C
	t.Cleanup(func() {
		config.C = originalConfig
	})
	config.C.OSS.Avatar.StorageDir = "prod"

	if got := buildAgentAvatarObjectKey(9); got != "prod/avatars/agent_9.jpg" {
		t.Fatalf("expected prod/avatars/agent_9.jpg, got %q", got)
	}
}

func TestBuildAgentAvatarVersionedObjectKey(t *testing.T) {
	originalConfig := config.C
	t.Cleanup(func() {
		config.C = originalConfig
	})
	config.C.OSS.Avatar.StorageDir = "prod"

	got := buildAgentAvatarVersionedObjectKey(9, 1700000000)
	want := "prod/avatars/agent_9_v1700000000.jpg"
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}
}

func TestIsAgentAvatarObjectKey_VersionedKey(t *testing.T) {
	originalConfig := config.C
	t.Cleanup(func() {
		config.C = originalConfig
	})
	config.C.OSS.Avatar.StorageDir = "prod"

	if !isAgentAvatarObjectKey(8, 9, "prod/avatars/agent_9_v1700000000.jpg") {
		t.Fatal("expected versioned key to match agent avatar")
	}
	if isAgentAvatarObjectKey(8, 9, "prod/avatars/agent_10_v1700000000.jpg") {
		t.Fatal("expected versioned key for different agent to not match")
	}
}
