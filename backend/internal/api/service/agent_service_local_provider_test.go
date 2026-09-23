package service

import (
	"testing"
	"time"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/errcode"
	"github.com/askie/grix/backend/internal/pkg/testutil"
	"github.com/askie/grix/backend/internal/store"
)

func TestAgentCreate_RejectsLocalProvider(t *testing.T) {
	testDB := testutil.NewTestDB()
	defer testDB.Close()
	store.DB = testDB.DB

	fixture := testutil.NewFixtureBuilder(testDB.DB)
	owner := fixture.CreateUser(func(u *model.User) {
		u.ID = 43001
		u.Username = "local_create_owner"
	})

	_, ec := AgentCreate(owner.ID, AgentCreateReq{
		AgentName:     "local_agent",
		ProviderType:  model.AgentProviderLocal,
		LocalEndpoint: "http://127.0.0.1:11434",
	})
	if ec == nil || ec.BizCode != errcode.ErrAgentInvalidType.BizCode {
		t.Fatalf("expected invalid provider type error, got %+v", ec)
	}
}

func TestAgentUpdate_LocalProviderOnlyForExistingLocalAgents(t *testing.T) {
	testDB := testutil.NewTestDB()
	defer testDB.Close()
	store.DB = testDB.DB

	fixture := testutil.NewFixtureBuilder(testDB.DB)
	owner := fixture.CreateUser(func(u *model.User) {
		u.ID = 43002
		u.Username = "local_update_owner"
	})
	seed := func(id int64, providerType int16) {
		t.Helper()
		if err := testDB.DB.Create(&model.Agent{
			ID:           id,
			AgentName:    "agent_" + time.Now().Format("150405.000000"),
			OwnerID:      owner.ID,
			ProviderType: providerType,
			Status:       model.AgentStatusActive,
			CreatedAt:    time.Now(),
			UpdatedAt:    time.Now(),
		}).Error; err != nil {
			t.Fatalf("create agent failed: %v", err)
		}
	}
	seed(53001, model.AgentProviderRemote)
	seed(53002, model.AgentProviderLocal)

	local := model.AgentProviderLocal
	_, ec := AgentUpdate(owner.ID, 53001, AgentUpdateReq{ProviderType: &local})
	if ec == nil || ec.BizCode != errcode.ErrAgentInvalidType.BizCode {
		t.Fatalf("expected switching to local to be rejected, got %+v", ec)
	}

	name := "still_local"
	_, ec = AgentUpdate(owner.ID, 53002, AgentUpdateReq{ProviderType: &local, AgentName: &name})
	if ec != nil && ec.BizCode == errcode.ErrAgentInvalidType.BizCode {
		t.Fatalf("existing local agent should stay editable, got %+v", ec)
	}
}
