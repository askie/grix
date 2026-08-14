package service

import (
	"strconv"
	"testing"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/agentscope"
	"github.com/askie/grix/backend/internal/pkg/snowflake"
	"github.com/askie/grix/backend/internal/pkg/testutil"
	"github.com/askie/grix/backend/internal/store"
)

func seedAgentCreateMainOwner(t *testing.T, ownerID int64) {
	t.Helper()
	if err := store.DB.Create(&model.User{
		ID:           ownerID,
		Username:     "main_owner_" + strconv.FormatInt(ownerID, 10),
		Email:        "main_owner_" + strconv.FormatInt(ownerID, 10) + "@example.com",
		PasswordHash: "x",
		AuthProvider: "local",
		Nickname:     "MainOwner",
		Status:       model.UserStatusActive,
	}).Error; err != nil {
		t.Fatalf("seed owner error: %v", err)
	}
}

func TestAgentCreate_MainAPIAgentGetsAllScopes(t *testing.T) {
	testDB := testutil.NewTestDB()
	defer testDB.Close()
	store.DB = testDB.DB
	store.RDB = testutil.NewMockRedis()
	if err := snowflake.Init(1); err != nil {
		t.Fatalf("init snowflake error: %v", err)
	}

	const ownerID = int64(93101)
	seedAgentCreateMainOwner(t, ownerID)

	resp, ec := AgentCreate(ownerID, AgentCreateReq{
		AgentName:    "main_api_agent",
		ProviderType: model.AgentProviderAPI,
		IsMain:       true,
	})
	if ec != nil {
		t.Fatalf("AgentCreate error: %+v", ec)
	}
	if resp == nil {
		t.Fatal("expected non-nil response")
	}

	var rows []model.AgentAPIScope
	if err := store.DB.
		Where("agent_id = ?", resp.ID).
		Order("scope ASC").
		Find(&rows).Error; err != nil {
		t.Fatalf("query scopes error: %v", err)
	}
	allowed := agentscope.AllowedScopes()
	if len(rows) != len(allowed) {
		t.Fatalf("expected %d scopes, got %d", len(allowed), len(rows))
	}
	gotScopeSet := make(map[string]struct{}, len(rows))
	for _, row := range rows {
		gotScopeSet[row.Scope] = struct{}{}
	}
	for _, expectedScope := range allowed {
		if _, ok := gotScopeSet[expectedScope]; !ok {
			t.Fatalf("expected scope %q to exist, got %#v", expectedScope, rows)
		}
	}
}

func TestAgentCreate_RejectsIsMainForNonAPIProvider(t *testing.T) {
	testDB := testutil.NewTestDB()
	defer testDB.Close()
	store.DB = testDB.DB
	store.RDB = testutil.NewMockRedis()
	if err := snowflake.Init(1); err != nil {
		t.Fatalf("init snowflake error: %v", err)
	}

	const ownerID = int64(93102)
	seedAgentCreateMainOwner(t, ownerID)

	_, ec := AgentCreate(ownerID, AgentCreateReq{
		AgentName:    "remote_agent_with_is_main",
		ProviderType: model.AgentProviderRemote,
		IsMain:       true,
	})
	if ec == nil {
		t.Fatal("expected error for non-api provider with is_main")
	}
	if ec.BizCode != 10003 {
		t.Fatalf("expected biz code 10003, got %d", ec.BizCode)
	}
	if ec.Msg != "is_main 仅支持 provider_type=3" {
		t.Fatalf("unexpected error message: %q", ec.Msg)
	}
}
