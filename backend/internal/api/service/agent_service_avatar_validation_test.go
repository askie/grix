package service

import (
	"testing"
	"time"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/testutil"
	"github.com/askie/grix/backend/internal/store"
)

func TestAgentCreate_RejectsDirectAvatarURLInput(t *testing.T) {
	testDB := testutil.NewTestDB()
	defer testDB.Close()
	store.DB = testDB.DB

	fixture := testutil.NewFixtureBuilder(testDB.DB)
	owner := fixture.CreateUser(func(u *model.User) {
		u.ID = 41001
		u.Username = "agent_create_owner"
	})

	_, ec := AgentCreate(owner.ID, AgentCreateReq{
		AgentName:    "avatar_direct_input",
		AvatarURL:    "https://example.com/agent.png",
		ProviderType: model.AgentProviderRemote,
	})
	if ec == nil {
		t.Fatal("expected validation error")
	}
	if ec.BizCode != 10003 {
		t.Fatalf("expected biz code 10003, got %d", ec.BizCode)
	}
	if ec.Msg != "头像必须通过上传接口设置" {
		t.Fatalf("expected avatar validation msg, got %q", ec.Msg)
	}
}

func TestAgentUpdate_RejectsDirectAvatarURLInput(t *testing.T) {
	testDB := testutil.NewTestDB()
	defer testDB.Close()
	store.DB = testDB.DB

	fixture := testutil.NewFixtureBuilder(testDB.DB)
	owner := fixture.CreateUser(func(u *model.User) {
		u.ID = 42001
		u.Username = "agent_update_owner"
	})
	agent := &model.Agent{
		ID:           52001,
		AgentName:    "agent_to_update",
		OwnerID:      owner.ID,
		ProviderType: model.AgentProviderRemote,
		Status:       model.AgentStatusActive,
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}
	if err := testDB.DB.Create(agent).Error; err != nil {
		t.Fatalf("create agent failed: %v", err)
	}

	avatarURL := "https://example.com/agent.png"
	_, ec := AgentUpdate(owner.ID, agent.ID, AgentUpdateReq{
		AvatarURL: &avatarURL,
	})
	if ec == nil {
		t.Fatal("expected validation error")
	}
	if ec.BizCode != 10003 {
		t.Fatalf("expected biz code 10003, got %d", ec.BizCode)
	}
	if ec.Msg != "头像必须通过上传接口设置" {
		t.Fatalf("expected avatar validation msg, got %q", ec.Msg)
	}
}
