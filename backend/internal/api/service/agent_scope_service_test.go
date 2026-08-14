package service

import (
	"strconv"
	"testing"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/agentscope"
	"github.com/askie/grix/backend/internal/pkg/testutil"
	"github.com/askie/grix/backend/internal/store"
)

func setupAgentScopeServiceTest(t *testing.T) (*testutil.TestDB, func()) {
	t.Helper()

	testDB := testutil.NewTestDB()
	store.DB = testDB.DB
	store.RDB = testutil.NewMockRedis()
	return testDB, func() { testDB.Close() }
}

func seedAgentScopeOwner(t *testing.T, ownerID int64) {
	t.Helper()

	if err := store.DB.Create(&model.User{
		ID:           ownerID,
		Username:     "owner_scope_" + strconv.FormatInt(ownerID, 10),
		Email:        "owner_scope_" + strconv.FormatInt(ownerID, 10) + "@example.com",
		PasswordHash: "x",
		AuthProvider: "local",
		Nickname:     "Owner",
		Status:       model.UserStatusActive,
	}).Error; err != nil {
		t.Fatalf("seed owner error: %v", err)
	}
}

func seedAgentScopeAgent(t *testing.T, agent model.Agent) {
	t.Helper()
	if err := store.DB.Create(&agent).Error; err != nil {
		t.Fatalf("seed agent error: %v", err)
	}
}

func TestAgentScopeReplaceAndGet(t *testing.T) {
	_, cleanup := setupAgentScopeServiceTest(t)
	defer cleanup()

	const (
		ownerID = int64(80101)
		agentID = int64(81101)
	)
	seedAgentScopeOwner(t, ownerID)
	seedAgentScopeAgent(t, model.Agent{
		ID:           agentID,
		AgentName:    "scope_api_agent",
		OwnerID:      ownerID,
		ProviderType: model.AgentProviderAPI,
		Status:       model.AgentStatusActive,
	})

	replaceResp, ec := AgentScopeReplace(ownerID, agentID, []string{
		" " + agentscope.ScopeGroupMemberAdd + " ",
		agentscope.ScopeGroupCreate,
		agentscope.ScopeGroupMemberAdd,
	})
	if ec != nil {
		t.Fatalf("AgentScopeReplace error: %+v", ec)
	}
	if len(replaceResp.Scopes) != 2 {
		t.Fatalf("expected 2 scopes, got %d", len(replaceResp.Scopes))
	}
	if replaceResp.Scopes[0] != agentscope.ScopeGroupMemberAdd || replaceResp.Scopes[1] != agentscope.ScopeGroupCreate {
		t.Fatalf("unexpected replace scopes: %#v", replaceResp.Scopes)
	}
	if len(replaceResp.AvailableScopes) != len(agentscope.AllowedScopes()) {
		t.Fatalf("expected available scopes count %d, got %d", len(agentscope.AllowedScopes()), len(replaceResp.AvailableScopes))
	}
	foundSessionSearch := false
	foundContactSearch := false
	foundCategoryList := false
	foundCategoryCreate := false
	foundCategoryUpdate := false
	foundCategoryAssign := false
	foundConversationAuditRead := false
	for _, scope := range replaceResp.AvailableScopes {
		if scope == agentscope.ScopeSessionSearch {
			foundSessionSearch = true
		}
		if scope == agentscope.ScopeContactSearch {
			foundContactSearch = true
		}
		if scope == agentscope.ScopeAgentCategoryList {
			foundCategoryList = true
		}
		if scope == agentscope.ScopeAgentCategoryCreate {
			foundCategoryCreate = true
		}
		if scope == agentscope.ScopeAgentCategoryUpdate {
			foundCategoryUpdate = true
		}
		if scope == agentscope.ScopeAgentCategoryAssign {
			foundCategoryAssign = true
		}
		if scope == agentscope.ScopeConversationAuditRead {
			foundConversationAuditRead = true
		}
	}
	if !foundSessionSearch {
		t.Fatalf("expected available scopes to include %q, got %#v", agentscope.ScopeSessionSearch, replaceResp.AvailableScopes)
	}
	if !foundContactSearch {
		t.Fatalf("expected available scopes to include %q, got %#v", agentscope.ScopeContactSearch, replaceResp.AvailableScopes)
	}
	if !foundCategoryList {
		t.Fatalf("expected available scopes to include %q, got %#v", agentscope.ScopeAgentCategoryList, replaceResp.AvailableScopes)
	}
	if !foundCategoryCreate {
		t.Fatalf("expected available scopes to include %q, got %#v", agentscope.ScopeAgentCategoryCreate, replaceResp.AvailableScopes)
	}
	if !foundCategoryUpdate {
		t.Fatalf("expected available scopes to include %q, got %#v", agentscope.ScopeAgentCategoryUpdate, replaceResp.AvailableScopes)
	}
	if !foundCategoryAssign {
		t.Fatalf("expected available scopes to include %q, got %#v", agentscope.ScopeAgentCategoryAssign, replaceResp.AvailableScopes)
	}
	if !foundConversationAuditRead {
		t.Fatalf("expected available scopes to include %q, got %#v", agentscope.ScopeConversationAuditRead, replaceResp.AvailableScopes)
	}

	getResp, ec := AgentScopeGet(ownerID, agentID)
	if ec != nil {
		t.Fatalf("AgentScopeGet error: %+v", ec)
	}
	if len(getResp.Scopes) != 2 {
		t.Fatalf("expected 2 scopes from get, got %d", len(getResp.Scopes))
	}
	if getResp.Scopes[0] != agentscope.ScopeGroupCreate || getResp.Scopes[1] != agentscope.ScopeGroupMemberAdd {
		t.Fatalf("unexpected get scopes order: %#v", getResp.Scopes)
	}
	if len(getResp.AvailableScopes) != len(agentscope.AllowedScopes()) {
		t.Fatalf("expected available scopes count %d, got %d", len(agentscope.AllowedScopes()), len(getResp.AvailableScopes))
	}
	if len(getResp.AvailableScopeItems) != len(agentscope.AllowedScopes()) {
		t.Fatalf("expected available scope items count %d, got %d", len(agentscope.AllowedScopes()), len(getResp.AvailableScopeItems))
	}
	if getResp.AvailableScopeItems[0].Scope == "" || getResp.AvailableScopeItems[0].Label == "" || getResp.AvailableScopeItems[0].Description == "" {
		t.Fatalf("expected available scope item text, got %#v", getResp.AvailableScopeItems[0])
	}

	clearResp, ec := AgentScopeReplace(ownerID, agentID, []string{})
	if ec != nil {
		t.Fatalf("AgentScopeReplace clear error: %+v", ec)
	}
	if len(clearResp.Scopes) != 0 {
		t.Fatalf("expected cleared scopes, got %#v", clearResp.Scopes)
	}

	var auditCount int64
	if err := store.DB.Model(&model.AuditLog{}).
		Where("event_type = ? AND user_id = ?", "agent_scope_replace", ownerID).
		Count(&auditCount).Error; err != nil {
		t.Fatalf("count audit log error: %v", err)
	}
	if auditCount != 2 {
		t.Fatalf("expected 2 scope replace audit logs, got %d", auditCount)
	}
}

func TestAgentScopeGetLocalizesAvailableScopeItems(t *testing.T) {
	_, cleanup := setupAgentScopeServiceTest(t)
	defer cleanup()

	const (
		ownerID = int64(80106)
		agentID = int64(81106)
	)
	seedAgentScopeOwner(t, ownerID)
	seedAgentScopeAgent(t, model.Agent{
		ID:           agentID,
		AgentName:    "scope_i18n_agent",
		OwnerID:      ownerID,
		ProviderType: model.AgentProviderAPI,
		Status:       model.AgentStatusActive,
	})

	zhResp, ec := AgentScopeGet(ownerID, agentID, "zh")
	if ec != nil {
		t.Fatalf("AgentScopeGet zh error: %+v", ec)
	}
	enResp, ec := AgentScopeGet(ownerID, agentID, "en")
	if ec != nil {
		t.Fatalf("AgentScopeGet en error: %+v", ec)
	}
	if len(zhResp.AvailableScopeItems) != len(zhResp.AvailableScopes) {
		t.Fatalf("zh available_scope_items len=%d available_scopes len=%d", len(zhResp.AvailableScopeItems), len(zhResp.AvailableScopes))
	}
	if len(enResp.AvailableScopeItems) != len(enResp.AvailableScopes) {
		t.Fatalf("en available_scope_items len=%d available_scopes len=%d", len(enResp.AvailableScopeItems), len(enResp.AvailableScopes))
	}

	var zhSessionSearch, enSessionSearch agentscope.ScopeItem
	for _, item := range zhResp.AvailableScopeItems {
		if item.Scope == agentscope.ScopeSessionSearch {
			zhSessionSearch = item
			break
		}
	}
	for _, item := range enResp.AvailableScopeItems {
		if item.Scope == agentscope.ScopeSessionSearch {
			enSessionSearch = item
			break
		}
	}
	if zhSessionSearch.Label != "搜索会话" || zhSessionSearch.Description != "允许搜索会话。" {
		t.Fatalf("unexpected zh session.search item: %#v", zhSessionSearch)
	}
	if enSessionSearch.Label != "Search Sessions" || enSessionSearch.Description != "Allow searching sessions." {
		t.Fatalf("unexpected en session.search item: %#v", enSessionSearch)
	}
}

func TestAgentScopeReplaceRejectsInvalidScope(t *testing.T) {
	_, cleanup := setupAgentScopeServiceTest(t)
	defer cleanup()

	const (
		ownerID = int64(80102)
		agentID = int64(81102)
	)
	seedAgentScopeOwner(t, ownerID)
	seedAgentScopeAgent(t, model.Agent{
		ID:           agentID,
		AgentName:    "scope_invalid_agent",
		OwnerID:      ownerID,
		ProviderType: model.AgentProviderAPI,
		Status:       model.AgentStatusActive,
	})

	_, ec := AgentScopeReplace(ownerID, agentID, []string{"group.unknown"})
	if ec == nil {
		t.Fatal("expected invalid scope error")
	}
	if ec.BizCode != 20012 {
		t.Fatalf("expected biz code 20012, got %d", ec.BizCode)
	}
}

func TestAgentScopeReplaceRejectsNonAPIProvider(t *testing.T) {
	_, cleanup := setupAgentScopeServiceTest(t)
	defer cleanup()

	const (
		ownerID = int64(80103)
		agentID = int64(81103)
	)
	seedAgentScopeOwner(t, ownerID)
	seedAgentScopeAgent(t, model.Agent{
		ID:           agentID,
		AgentName:    "scope_non_api_agent",
		OwnerID:      ownerID,
		ProviderType: model.AgentProviderRemote,
		Status:       model.AgentStatusActive,
	})

	_, ec := AgentScopeReplace(ownerID, agentID, []string{agentscope.ScopeGroupCreate})
	if ec == nil {
		t.Fatal("expected non-api provider error")
	}
	if ec.BizCode != 20013 {
		t.Fatalf("expected biz code 20013, got %d", ec.BizCode)
	}
}

func TestAgentScopeGetFiltersBuiltInLegacyScopes(t *testing.T) {
	_, cleanup := setupAgentScopeServiceTest(t)
	defer cleanup()

	const (
		ownerID = int64(80104)
		agentID = int64(81104)
	)
	seedAgentScopeOwner(t, ownerID)
	seedAgentScopeAgent(t, model.Agent{
		ID:           agentID,
		AgentName:    "scope_legacy_filter_agent",
		OwnerID:      ownerID,
		ProviderType: model.AgentProviderAPI,
		Status:       model.AgentStatusActive,
	})
	if err := store.DB.Create(&[]model.AgentAPIScope{
		{AgentID: agentID, Scope: agentscope.ScopeGroupCreate},
		{AgentID: agentID, Scope: "group.detail.read"},
	}).Error; err != nil {
		t.Fatalf("seed legacy scope rows error: %v", err)
	}

	resp, ec := AgentScopeGet(ownerID, agentID)
	if ec != nil {
		t.Fatalf("AgentScopeGet error: %+v", ec)
	}
	if len(resp.Scopes) != 1 || resp.Scopes[0] != agentscope.ScopeGroupCreate {
		t.Fatalf("expected only configurable scopes, got %#v", resp.Scopes)
	}
	for _, scope := range resp.AvailableScopes {
		if scope == "group.detail.read" {
			t.Fatalf("unexpected built-in group detail scope in available scopes: %#v", resp.AvailableScopes)
		}
	}
}

func TestAgentScopeReplaceRejectsForeignOwner(t *testing.T) {
	_, cleanup := setupAgentScopeServiceTest(t)
	defer cleanup()

	const (
		ownerID        = int64(80104)
		anotherOwnerID = int64(80105)
		agentID        = int64(81104)
	)
	seedAgentScopeOwner(t, ownerID)
	seedAgentScopeOwner(t, anotherOwnerID)
	seedAgentScopeAgent(t, model.Agent{
		ID:           agentID,
		AgentName:    "scope_foreign_owner_agent",
		OwnerID:      ownerID,
		ProviderType: model.AgentProviderAPI,
		Status:       model.AgentStatusActive,
	})

	_, ec := AgentScopeReplace(anotherOwnerID, agentID, []string{agentscope.ScopeGroupCreate})
	if ec == nil {
		t.Fatal("expected forbidden error")
	}
	if ec.BizCode != 20003 {
		t.Fatalf("expected biz code 20003, got %d", ec.BizCode)
	}
}
