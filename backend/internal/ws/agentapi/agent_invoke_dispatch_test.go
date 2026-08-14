package agentapi

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/askie/grix/backend/internal/api/service"
	"github.com/askie/grix/backend/internal/claudeaccess"
	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/agentapi"
	"github.com/askie/grix/backend/internal/pkg/agentscope"
	"github.com/askie/grix/backend/internal/pkg/snowflake"
	"github.com/askie/grix/backend/internal/pkg/testutil"
	"github.com/askie/grix/backend/internal/store"
)

func setupAgentInvokeDispatchTest(t *testing.T) (*testutil.TestDB, func()) {
	t.Helper()

	previousDB, previousRDB := store.DB, store.RDB
	testDB := testutil.NewTestDB()
	store.DB = testDB.DB
	store.RDB = testutil.NewMockRedis()
	_ = snowflake.Init(1)

	return testDB, func() {
		_ = store.RDB.Close()
		testDB.Close()
		store.DB, store.RDB = previousDB, previousRDB
	}
}

func seedAgentInvokeDispatchActor(
	t *testing.T,
	db *testutil.TestDB,
	ownerID, agentID int64,
	apiKey string,
) {
	t.Helper()

	owner := model.User{
		ID:           ownerID,
		Username:     "dispatch_owner_" + strings.ReplaceAll(apiKey, "-", "_"),
		Email:        "dispatch_owner_" + strings.ReplaceAll(apiKey, "-", "_") + "@example.com",
		PasswordHash: "x",
		AuthProvider: "local",
		Nickname:     "Dispatch Owner",
		Status:       model.UserStatusActive,
	}
	if err := db.DB.Create(&owner).Error; err != nil {
		t.Fatalf("seed owner error: %v", err)
	}

	actor := model.Agent{
		ID:              agentID,
		AgentName:       "dispatch_actor_" + strings.ReplaceAll(apiKey, "-", "_"),
		OwnerID:         ownerID,
		ProviderType:    model.AgentProviderAPI,
		AgentClientType: model.AgentClientTypeClaude,
		Status:          model.AgentStatusActive,
		APIKeyHash:      agentapi.HashAPIKey(apiKey),
		APIKeyHint:      agentapi.APIKeyHint(apiKey),
	}
	if err := db.DB.Create(&actor).Error; err != nil {
		t.Fatalf("seed actor agent error: %v", err)
	}
}

func seedAgentInvokeDispatchScope(t *testing.T, agentID int64, scope string) {
	t.Helper()

	if err := store.DB.Create(&model.AgentAPIScope{
		AgentID: agentID,
		Scope:   scope,
	}).Error; err != nil {
		t.Fatalf("seed scope error: %v", err)
	}
}

func TestDispatchAgentInvokeConversationAuditRequiresScope(t *testing.T) {
	_, cleanup := setupAgentInvokeDispatchTest(t)
	defer cleanup()

	_, code, msg := dispatchAgentInvokeWithHooks(42120, 42121, "audit_get_manifest", map[string]interface{}{
		"audit_id": "audit-1",
	}, agentInvokeHooks{})
	if code != 4003 {
		t.Fatalf("audit_get_manifest without scope code=%d msg=%q, want 4003", code, msg)
	}
	if !strings.Contains(msg, agentscope.ScopeConversationAuditRead) {
		t.Fatalf("msg=%q does not mention scope %q", msg, agentscope.ScopeConversationAuditRead)
	}
}

func TestDispatchAgentInvokeAuditContentAllowsSameOwnerCrossAgentLookup(t *testing.T) {
	_, cleanup := setupAgentInvokeDispatchTest(t)
	defer cleanup()

	const (
		ownerID      = int64(42131)
		callerID     = int64(42132)
		auditAgentID = int64(42135)
	)
	seedAgentInvokeDispatchScope(t, callerID, agentscope.ScopeConversationAuditRead)
	if err := store.DB.Create(&model.ConversationAuditTurn{
		ID:        42133,
		OwnerID:   ownerID,
		AgentID:   auditAgentID,
		SessionID: "audit-session",
		MsgID:     42134,
		EventID:   "audit-event",
		AuditID:   "audit-1",
		TurnID:    "turn-1",
		State:     "ready",
		Revision:  1,
	}).Error; err != nil {
		t.Fatalf("seed audit turn error: %v", err)
	}

	_, code, msg := dispatchAgentInvokeWithHooks(callerID, ownerID, "audit_get_content_chunk", map[string]interface{}{
		"audit_id": "audit-1",
	}, agentInvokeHooks{})
	if code != 4001 || !strings.Contains(msg, "content_id") {
		t.Fatalf("audit_get_content_chunk without content_id code=%d msg=%q, want 4001 content_id", code, msg)
	}
}

func TestDispatchAgentInvokeAgentAPICreate(t *testing.T) {
	const (
		ownerID = int64(42101)
		agentID = int64(42102)
	)

	t.Run("creates provider_type=3 agent through ws invoke when scope granted", func(t *testing.T) {
		testDB, cleanup := setupAgentInvokeDispatchTest(t)
		defer cleanup()

		seedAgentInvokeDispatchActor(t, testDB, ownerID, agentID, "ak_ws_create_allowed")
		seedAgentInvokeDispatchScope(t, agentID, agentscope.ScopeAgentAPICreate)

		data, code, msg := dispatchAgentInvoke(agentID, ownerID, "agent_api_create", map[string]interface{}{
			"agent_name":        "ws-child-agent",
			"introduction":      "created over ws invoke",
			"system_prompt":     "ws business prompt",
			"agent_client_type": model.AgentClientTypeDeepSeek,
		})
		if code != 0 || msg != "" {
			t.Fatalf("dispatchAgentInvoke returned code=%d msg=%q", code, msg)
		}

		resp, ok := data.(*service.AgentResp)
		if !ok {
			t.Fatalf("expected *service.AgentResp, got %T", data)
		}
		if resp.ProviderType != model.AgentProviderAPI {
			t.Fatalf("provider_type=%d want=%d", resp.ProviderType, model.AgentProviderAPI)
		}
		if resp.APIKey == "" || resp.APIEndpoint == "" {
			t.Fatalf("expected api credentials in response, got %#v", resp)
		}
		if resp.OwnerID != ownerID {
			t.Fatalf("owner_id=%d want=%d", resp.OwnerID, ownerID)
		}

		var created model.Agent
		if err := store.DB.First(&created, resp.ID).Error; err != nil {
			t.Fatalf("query created agent error: %v", err)
		}
		if created.OwnerID != ownerID {
			t.Fatalf("persisted owner_id=%d want=%d", created.OwnerID, ownerID)
		}
		if created.ProviderType != model.AgentProviderAPI {
			t.Fatalf("persisted provider_type=%d want=%d", created.ProviderType, model.AgentProviderAPI)
		}
		if created.SystemPrompt != "ws business prompt" || resp.SystemPrompt != "ws business prompt" {
			t.Fatalf("system_prompt response=%q stored=%q", resp.SystemPrompt, created.SystemPrompt)
		}
		if created.AgentClientType != model.AgentClientTypeDeepSeek {
			t.Fatalf("agent_client_type=%q", created.AgentClientType)
		}
	})

	t.Run("rejects create when scope missing", func(t *testing.T) {
		testDB, cleanup := setupAgentInvokeDispatchTest(t)
		defer cleanup()

		seedAgentInvokeDispatchActor(t, testDB, ownerID+10, agentID+10, "ak_ws_create_denied")

		data, code, msg := dispatchAgentInvoke(agentID+10, ownerID+10, "agent_api_create", map[string]interface{}{
			"agent_name": "ws-child-denied",
		})
		if data != nil {
			t.Fatalf("expected nil data, got %T", data)
		}
		if code != 4003 {
			t.Fatalf("code=%d want=4003", code)
		}
		if !strings.Contains(msg, agentscope.ScopeAgentAPICreate) {
			t.Fatalf("msg=%q does not mention scope %q", msg, agentscope.ScopeAgentAPICreate)
		}
	})
}

func TestDispatchAgentInvokeAgentCategoryActions(t *testing.T) {
	testDB, cleanup := setupAgentInvokeDispatchTest(t)
	defer cleanup()

	const (
		ownerID      = int64(42111)
		actorAgentID = int64(42112)
		targetAgent  = int64(42113)
	)

	seedAgentInvokeDispatchActor(t, testDB, ownerID, actorAgentID, "ak_ws_agent_category")
	for _, scope := range []string{
		agentscope.ScopeAgentCategoryList,
		agentscope.ScopeAgentCategoryCreate,
		agentscope.ScopeAgentCategoryUpdate,
		agentscope.ScopeAgentCategoryAssign,
	} {
		seedAgentInvokeDispatchScope(t, actorAgentID, scope)
	}
	if err := store.DB.Create(&model.Agent{
		ID:           targetAgent,
		AgentName:    "ws-category-target-agent",
		OwnerID:      ownerID,
		ProviderType: model.AgentProviderRemote,
		Status:       model.AgentStatusActive,
	}).Error; err != nil {
		t.Fatalf("seed target agent error: %v", err)
	}

	createData, code, msg := dispatchAgentInvoke(actorAgentID, ownerID, "agent_category_create", map[string]interface{}{
		"name": "Workspace",
	})
	if code != 0 || msg != "" {
		t.Fatalf("create returned code=%d msg=%q", code, msg)
	}
	createResp, ok := createData.(*service.AgentCategoryResp)
	if !ok {
		t.Fatalf("expected *service.AgentCategoryResp, got %T", createData)
	}
	if createResp.Name != "Workspace" {
		t.Fatalf("unexpected create resp: %#v", createResp)
	}

	listData, code, msg := dispatchAgentInvoke(actorAgentID, ownerID, "agent_category_list", map[string]interface{}{})
	if code != 0 || msg != "" {
		t.Fatalf("list returned code=%d msg=%q", code, msg)
	}
	listResp, ok := listData.([]service.AgentCategoryResp)
	if !ok {
		t.Fatalf("expected []service.AgentCategoryResp, got %T", listData)
	}
	if len(listResp) != 1 || listResp[0].Name != "Workspace" {
		t.Fatalf("unexpected list resp: %#v", listResp)
	}

	updateData, code, msg := dispatchAgentInvoke(actorAgentID, ownerID, "agent_category_update", map[string]interface{}{
		"category_id": createResp.ID,
		"name":        "Workspace Updated",
	})
	if code != 0 || msg != "" {
		t.Fatalf("update returned code=%d msg=%q", code, msg)
	}
	updateResp, ok := updateData.(*service.AgentCategoryResp)
	if !ok {
		t.Fatalf("expected *service.AgentCategoryResp, got %T", updateData)
	}
	if updateResp.Name != "Workspace Updated" {
		t.Fatalf("unexpected update resp: %#v", updateResp)
	}

	assignData, code, msg := dispatchAgentInvoke(actorAgentID, ownerID, "agent_category_assign", map[string]interface{}{
		"agent_id":    targetAgent,
		"category_id": createResp.ID,
	})
	if code != 0 || msg != "" {
		t.Fatalf("assign returned code=%d msg=%q", code, msg)
	}
	assignResp, ok := assignData.(*service.AgentResp)
	if !ok {
		t.Fatalf("expected *service.AgentResp, got %T", assignData)
	}
	if assignResp.CategoryID != createResp.ID {
		t.Fatalf("unexpected assign resp: %#v", assignResp)
	}
}

func TestDispatchAgentInvokeGroupDetailRead(t *testing.T) {
	t.Run("allows current agent member without scope", func(t *testing.T) {
		testDB, cleanup := setupAgentInvokeDispatchTest(t)
		defer cleanup()

		const (
			ownerID      = int64(42501)
			agentID      = int64(42502)
			groupOwnerID = int64(42503)
		)
		now := time.Now()
		seedAgentInvokeDispatchActor(t, testDB, ownerID, agentID, "ak_ws_group_detail_direct")
		if err := store.DB.Create(&model.User{
			ID:           groupOwnerID,
			Username:     "ws_group_owner_direct",
			Email:        "ws_group_owner_direct@example.com",
			PasswordHash: "x",
			AuthProvider: "local",
			Nickname:     "WSGroupOwnerDirect",
			Status:       model.UserStatusActive,
		}).Error; err != nil {
			t.Fatalf("seed direct group owner error: %v", err)
		}
		session := model.Session{
			SessionID:      "ws-group-detail-direct",
			OwnerID:        groupOwnerID,
			SessionType:    model.SessionTypeGroup,
			GroupName:      "ws-direct-group",
			LastMsgSummary: "latest",
		}
		if err := store.DB.Create(&session).Error; err != nil {
			t.Fatalf("create direct session error: %v", err)
		}
		members := []model.SessionMember{
			{SessionID: session.SessionID, MemberID: groupOwnerID, MemberType: 1, Role: 3, LastActiveAt: now, JoinedAt: now},
			{SessionID: session.SessionID, MemberID: agentID, MemberType: 2, Role: 1, LastActiveAt: now, JoinedAt: now},
		}
		if err := store.DB.Create(&members).Error; err != nil {
			t.Fatalf("create direct members error: %v", err)
		}

		data, code, msg := dispatchAgentInvoke(agentID, ownerID, "group_detail_read", map[string]interface{}{
			"session_id": session.SessionID,
		})
		if code != 0 || msg != "" {
			t.Fatalf("dispatchAgentInvoke direct returned code=%d msg=%q", code, msg)
		}
		resp, ok := data.(*service.SessionDetailResp)
		if !ok {
			t.Fatalf("expected *service.SessionDetailResp, got %T", data)
		}
		if resp.GroupName != "ws-direct-group" {
			t.Fatalf("expected group_name=%q, got %q", "ws-direct-group", resp.GroupName)
		}
		if resp.SessionID != session.SessionID || resp.MemberCount != 2 {
			t.Fatalf("unexpected direct group detail resp: %#v", resp)
		}
	})

	t.Run("allows delegated owner member without scope", func(t *testing.T) {
		testDB, cleanup := setupAgentInvokeDispatchTest(t)
		defer cleanup()

		const (
			ownerID      = int64(42511)
			agentID      = int64(42512)
			groupOwnerID = int64(42513)
		)
		now := time.Now()
		seedAgentInvokeDispatchActor(t, testDB, ownerID, agentID, "ak_ws_group_detail_delegate")
		if err := store.DB.Create(&model.User{
			ID:           groupOwnerID,
			Username:     "ws_group_owner_delegate",
			Email:        "ws_group_owner_delegate@example.com",
			PasswordHash: "x",
			AuthProvider: "local",
			Nickname:     "WSGroupOwnerDelegate",
			Status:       model.UserStatusActive,
		}).Error; err != nil {
			t.Fatalf("seed delegated group owner error: %v", err)
		}
		session := model.Session{
			SessionID:      "ws-group-detail-delegate",
			OwnerID:        groupOwnerID,
			SessionType:    model.SessionTypeGroup,
			GroupName:      "ws-delegate-group",
			LastMsgSummary: "latest",
		}
		if err := store.DB.Create(&session).Error; err != nil {
			t.Fatalf("create delegated session error: %v", err)
		}
		members := []model.SessionMember{
			{SessionID: session.SessionID, MemberID: groupOwnerID, MemberType: 1, Role: 3, LastActiveAt: now, JoinedAt: now},
			{SessionID: session.SessionID, MemberID: ownerID, MemberType: 1, Role: 1, LastActiveAt: now, JoinedAt: now},
		}
		if err := store.DB.Create(&members).Error; err != nil {
			t.Fatalf("create delegated members error: %v", err)
		}
		if err := store.RDB.HSet(context.Background(), "im:delegate:"+session.SessionID+":42511", "agent_id", agentID).Err(); err != nil {
			t.Fatalf("seed delegated detail key error: %v", err)
		}

		data, code, msg := dispatchAgentInvoke(agentID, ownerID, "group_detail_read", map[string]interface{}{
			"session_id": session.SessionID,
		})
		if code != 0 || msg != "" {
			t.Fatalf("dispatchAgentInvoke delegated returned code=%d msg=%q", code, msg)
		}
		resp, ok := data.(*service.SessionDetailResp)
		if !ok {
			t.Fatalf("expected *service.SessionDetailResp, got %T", data)
		}
		if resp.GroupName != "ws-delegate-group" {
			t.Fatalf("expected group_name=%q, got %q", "ws-delegate-group", resp.GroupName)
		}
		if resp.SessionID != session.SessionID || resp.MemberCount != 2 {
			t.Fatalf("unexpected delegated group detail resp: %#v", resp)
		}
	})

	t.Run("denies unrelated agent without scope", func(t *testing.T) {
		testDB, cleanup := setupAgentInvokeDispatchTest(t)
		defer cleanup()

		const (
			ownerID      = int64(42521)
			agentID      = int64(42522)
			groupOwnerID = int64(42523)
		)
		now := time.Now()
		seedAgentInvokeDispatchActor(t, testDB, ownerID, agentID, "ak_ws_group_detail_denied")
		if err := store.DB.Create(&model.User{
			ID:           groupOwnerID,
			Username:     "ws_group_owner_denied",
			Email:        "ws_group_owner_denied@example.com",
			PasswordHash: "x",
			AuthProvider: "local",
			Nickname:     "WSGroupOwnerDenied",
			Status:       model.UserStatusActive,
		}).Error; err != nil {
			t.Fatalf("seed denied group owner error: %v", err)
		}
		session := model.Session{
			SessionID:      "ws-group-detail-denied",
			OwnerID:        groupOwnerID,
			SessionType:    model.SessionTypeGroup,
			GroupName:      "ws-denied-group",
			LastMsgSummary: "latest",
		}
		if err := store.DB.Create(&session).Error; err != nil {
			t.Fatalf("create denied session error: %v", err)
		}
		members := []model.SessionMember{
			{SessionID: session.SessionID, MemberID: groupOwnerID, MemberType: 1, Role: 3, LastActiveAt: now, JoinedAt: now},
		}
		if err := store.DB.Create(&members).Error; err != nil {
			t.Fatalf("create denied members error: %v", err)
		}

		data, code, msg := dispatchAgentInvoke(agentID, ownerID, "group_detail_read", map[string]interface{}{
			"session_id": session.SessionID,
		})
		if code != 4003 {
			t.Fatalf("expected code 4003, got %d msg=%q data=%#v", code, msg, data)
		}
		if !strings.Contains(msg, service.ErrSessionPermissionDenied.Error()) {
			t.Fatalf("expected permission denied message, got %q", msg)
		}
	})
}

func TestDispatchAgentInvokeGroupCreateRequiresScope(t *testing.T) {
	testDB, cleanup := setupAgentInvokeDispatchTest(t)
	defer cleanup()

	const (
		ownerID = int64(42201)
		agentID = int64(42202)
	)
	seedAgentInvokeDispatchActor(t, testDB, ownerID, agentID, "ak_ws_group_denied")

	data, code, msg := dispatchAgentInvoke(agentID, ownerID, "group_create", map[string]interface{}{
		"name": "scope-check-room",
	})
	if data != nil {
		t.Fatalf("expected nil data, got %T", data)
	}
	if code != 4003 {
		t.Fatalf("code=%d want=4003", code)
	}
	if !strings.Contains(msg, agentscope.ScopeGroupCreate) {
		t.Fatalf("msg=%q does not mention scope %q", msg, agentscope.ScopeGroupCreate)
	}
}

func TestDispatchAgentInvokeClaudeAccessActions(t *testing.T) {
	testDB, cleanup := setupAgentInvokeDispatchTest(t)
	defer cleanup()

	const (
		ownerID = int64(42301)
		agentID = int64(42302)
	)
	seedAgentInvokeDispatchActor(t, testDB, ownerID, agentID, "ak_ws_claude_access")

	statusData, code, msg := dispatchAgentInvoke(agentID, ownerID, "claude_access_control", map[string]interface{}{
		"verb":    "status_read",
		"payload": map[string]interface{}{},
	})
	if code != 0 || msg != "ok" {
		t.Fatalf("status read returned code=%d msg=%q", code, msg)
	}
	status, ok := statusData.(claudeaccess.Status)
	if !ok {
		t.Fatalf("status read data type = %T", statusData)
	}
	if status.Policy != claudeaccess.PolicyAllowlist {
		t.Fatalf("status policy = %q want %q", status.Policy, claudeaccess.PolicyAllowlist)
	}

	allowedData, code, msg := dispatchAgentInvoke(agentID, ownerID, "claude_access_control", map[string]interface{}{
		"verb": "sender_allow",
		"payload": map[string]interface{}{
			"sender_id": "sender-alpha",
		},
	})
	if code != 0 || msg != "ok" {
		t.Fatalf("allow sender returned code=%d msg=%q", code, msg)
	}
	allowed, ok := allowedData.(claudeaccess.SenderResult)
	if !ok || allowed.SenderID != "sender-alpha" {
		t.Fatalf("allow sender result = %#v", allowedData)
	}

	setPolicyData, code, msg := dispatchAgentInvoke(agentID, ownerID, "claude_access_control", map[string]interface{}{
		"verb": "policy_set",
		"payload": map[string]interface{}{
			"policy": claudeaccess.PolicyDisabled,
		},
	})
	if code != 0 || msg != "ok" {
		t.Fatalf("policy set returned code=%d msg=%q", code, msg)
	}
	updatedStatus, ok := setPolicyData.(claudeaccess.Status)
	if !ok || updatedStatus.Policy != claudeaccess.PolicyDisabled {
		t.Fatalf("policy set result = %#v", setPolicyData)
	}

	if _, code, _ = dispatchAgentInvoke(agentID, ownerID, "claude_access_control", map[string]interface{}{
		"verb": "pair_approve",
		"payload": map[string]interface{}{
			"code": "MISSING",
		},
	}); code != 4004 {
		t.Fatalf("missing pairing code should return 4004, got=%d", code)
	}
}

func TestDispatchAgentInvokeClaudePermissionRequestCreate(t *testing.T) {
	testDB, cleanup := setupAgentInvokeDispatchTest(t)
	defer cleanup()

	const (
		ownerID = int64(42401)
		agentID = int64(42402)
	)
	seedAgentInvokeDispatchActor(t, testDB, ownerID, agentID, "ak_ws_claude_permission")

	sent := make([]SendMessageReq, 0, 1)
	data, code, msg := dispatchAgentInvokeWithHooks(agentID, ownerID, "claude_interaction_request_create", map[string]interface{}{
		"kind":       "permission",
		"request_id": "req-approval-1",
		"session_id": "sess-approval-1",
		"message_id": "12345",
		"payload": map[string]interface{}{
			"tool_name":     "Bash",
			"description":   "Run pwd",
			"input_preview": "{\"command\":\"pwd\"}",
		},
	}, agentInvokeHooks{
		sendMessage: func(req SendMessageReq) (*SendMessageResult, error) {
			sent = append(sent, req)
			return nil, nil
		},
	})
	if code != 0 || msg != "ok" {
		t.Fatalf("permission request create returned code=%d msg=%q", code, msg)
	}
	result, ok := data.(claudeInteractionRequestResult)
	if !ok {
		t.Fatalf("permission request create result type=%T", data)
	}
	if !result.NoticeSent || result.RequestID != "req-approval-1" || result.Kind != "permission" {
		t.Fatalf("permission request create result=%#v", result)
	}
	if len(sent) != 1 {
		t.Fatalf("send calls=%d want=1", len(sent))
	}
	if sent[0].SessionID != "sess-approval-1" || sent[0].QuotedMessageID != 12345 {
		t.Fatalf("send req=%#v", sent[0])
	}
	if got := sent[0].VisibleTo; len(got) != 1 || got[0] != ownerID {
		t.Fatalf("visible_to=%v want=[%d]", got, ownerID)
	}
	if !strings.Contains(sent[0].Content, "grix://card/exec_approval") {
		t.Fatalf("content=%q should contain exec approval card", sent[0].Content)
	}
	if !strings.Contains(sent[0].Content, "req-approval-1") {
		t.Fatalf("content=%q should mention request id", sent[0].Content)
	}
	if !strings.Contains(string(sent[0].Extra), "\"exec_approval\"") {
		t.Fatalf("extra=%s should contain exec_approval envelope", string(sent[0].Extra))
	}
}

func TestDispatchAgentInvokeClaudeElicitationRequestCreate(t *testing.T) {
	testDB, cleanup := setupAgentInvokeDispatchTest(t)
	defer cleanup()

	const (
		ownerID = int64(42411)
		agentID = int64(42412)
	)
	seedAgentInvokeDispatchActor(t, testDB, ownerID, agentID, "ak_ws_claude_elicitation")

	sent := make([]SendMessageReq, 0, 1)
	data, code, msg := dispatchAgentInvokeWithHooks(agentID, ownerID, "claude_interaction_request_create", map[string]interface{}{
		"kind":       "elicitation",
		"request_id": "req-question-1",
		"session_id": "sess-question-1",
		"message_id": "12346",
		"payload": map[string]interface{}{
			"message": "Choose an environment.",
			"requested_schema": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"environment": map[string]interface{}{
						"type":        "string",
						"title":       "Environment",
						"description": "Choose an environment.",
						"enum": []interface{}{
							"production",
							"staging",
						},
					},
				},
				"required": []interface{}{"environment"},
			},
		},
	}, agentInvokeHooks{
		sendMessage: func(req SendMessageReq) (*SendMessageResult, error) {
			sent = append(sent, req)
			return nil, nil
		},
	})
	if code != 0 || msg != "ok" {
		t.Fatalf("elicitation request create returned code=%d msg=%q", code, msg)
	}
	result, ok := data.(claudeInteractionRequestResult)
	if !ok {
		t.Fatalf("elicitation request create result type=%T", data)
	}
	if !result.NoticeSent || result.RequestID != "req-question-1" || result.Kind != "elicitation" {
		t.Fatalf("elicitation request create result=%#v", result)
	}
	if len(sent) != 1 {
		t.Fatalf("send calls=%d want=1", len(sent))
	}
	if sent[0].SessionID != "sess-question-1" || sent[0].QuotedMessageID != 12346 {
		t.Fatalf("send req=%#v", sent[0])
	}
	if len(sent[0].VisibleTo) != 0 {
		t.Fatalf("visible_to=%v want=nil for elicitation card", sent[0].VisibleTo)
	}
	if !strings.Contains(sent[0].Content, "grix://card/agent_question") {
		t.Fatalf("content=%q should contain agent question card", sent[0].Content)
	}
	if !strings.Contains(sent[0].Content, "?d=") {
		t.Fatalf("content=%q should encode complex question payload with d=", sent[0].Content)
	}
	if !strings.Contains(sent[0].Content, "req-question-1") {
		t.Fatalf("content=%q should mention request id", sent[0].Content)
	}
}

func TestDispatchAgentInvokeClaudeURLElicitationRequestCreate(t *testing.T) {
	testDB, cleanup := setupAgentInvokeDispatchTest(t)
	defer cleanup()

	const (
		ownerID = int64(42421)
		agentID = int64(42422)
	)
	seedAgentInvokeDispatchActor(t, testDB, ownerID, agentID, "ak_ws_claude_elicitation_url")

	sent := make([]SendMessageReq, 0, 1)
	data, code, msg := dispatchAgentInvokeWithHooks(agentID, ownerID, "claude_interaction_request_create", map[string]interface{}{
		"kind":       "elicitation",
		"request_id": "req-url-1",
		"session_id": "sess-url-1",
		"message_id": "12347",
		"payload": map[string]interface{}{
			"mode":    "url",
			"message": "Please authenticate",
			"url":     "https://auth.example.com/login",
		},
	}, agentInvokeHooks{
		sendMessage: func(req SendMessageReq) (*SendMessageResult, error) {
			sent = append(sent, req)
			return nil, nil
		},
	})
	if code != 0 || msg != "ok" {
		t.Fatalf("url elicitation request create returned code=%d msg=%q", code, msg)
	}
	result, ok := data.(claudeInteractionRequestResult)
	if !ok {
		t.Fatalf("url elicitation request create result type=%T", data)
	}
	if !result.NoticeSent || result.RequestID != "req-url-1" || result.Kind != "elicitation" {
		t.Fatalf("url elicitation request create result=%#v", result)
	}
	if len(sent) != 1 {
		t.Fatalf("send calls=%d want=1", len(sent))
	}
	if sent[0].SessionID != "sess-url-1" || sent[0].QuotedMessageID != 12347 {
		t.Fatalf("send req=%#v", sent[0])
	}
	if !strings.Contains(sent[0].Content, "grix://card/agent_question") {
		t.Fatalf("content=%q should contain agent question card", sent[0].Content)
	}
	if !strings.Contains(sent[0].Content, "?d=") {
		t.Fatalf("content=%q should encode complex url question payload with d=", sent[0].Content)
	}
	if !strings.Contains(sent[0].Content, "https%3A%2F%2Fauth.example.com%2Flogin") {
		t.Fatalf("content=%q should contain encoded auth url", sent[0].Content)
	}
	if !strings.Contains(sent[0].Content, "%22mode%22%3A%22url%22") {
		t.Fatalf("content=%q should contain url mode payload", sent[0].Content)
	}
}
