package customercoach

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/askie/grix/backend/internal/featuregate"
	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/agentscope"
	"github.com/askie/grix/backend/internal/pkg/logger"
	"github.com/askie/grix/backend/internal/pkg/testutil"
	"github.com/askie/grix/backend/internal/store"
	"github.com/askie/grix/backend/internal/systemsetting"
	wsagentapi "github.com/askie/grix/backend/internal/ws/agentapi"
)

func setupCustomerCoachTest(t *testing.T) {
	t.Helper()
	logger.Init()
	testDB := testutil.NewTestDB()
	t.Cleanup(testDB.Close)
	store.DB = testDB.DB
	store.RDB = testutil.NewMockRedis()
	systemsetting.InvalidateAuthSettingsCache()
	featuregate.InvalidateCache()
	t.Cleanup(systemsetting.InvalidateAuthSettingsCache)
	t.Cleanup(featuregate.InvalidateCache)
}

func allowCoachTriggerUsers(t *testing.T, userIDs ...int64) {
	t.Helper()
	featuregate.InvalidateCache()
	if _, err := featuregate.GetGate(FeatureGateKey); err != nil {
		if _, err := featuregate.CreateGate(FeatureGateKey, "客服主动引导", model.FeatureStatusWhitelist); err != nil {
			t.Fatalf("create customer_coach gate: %v", err)
		}
	} else if err := featuregate.UpdateGateStatus(FeatureGateKey, model.FeatureStatusWhitelist); err != nil {
		t.Fatalf("set customer_coach whitelist: %v", err)
	}
	if err := featuregate.AddUsersToWhitelist(FeatureGateKey, userIDs); err != nil {
		t.Fatalf("whitelist customer_coach users: %v", err)
	}
	featuregate.InvalidateCache()
	t.Cleanup(func() {
		_ = featuregate.RemoveUsersFromWhitelist(FeatureGateKey, userIDs)
		featuregate.InvalidateCache()
	})
}

func TestBuildSnapshotRendersMainAgentByCompleteScopes(t *testing.T) {
	setupCustomerCoachTest(t)
	ctx := context.Background()
	userID := int64(101)
	agentID := int64(201)
	now := time.Now().UTC()

	mustCreateUser(t, userID, "owner", "zh", "cn")
	mustCreateAgent(t, model.Agent{
		ID:              agentID,
		AgentName:       "主控 Agent",
		OwnerID:         userID,
		ProviderType:    model.AgentProviderAPI,
		AgentClientType: "openclaw",
		Introduction:    "负责创建其他 Agent、建群和协调任务。",
		Status:          model.AgentStatusActive,
		MediaCapability: model.AgentMediaCapabilityVoice,
		CreatedAt:       now,
		UpdatedAt:       now,
	})
	for _, scope := range agentscope.AllowedScopes() {
		if err := store.DB.Create(&model.AgentAPIScope{
			AgentID: agentID,
			Scope:   scope,
		}).Error; err != nil {
			t.Fatalf("create scope %s: %v", scope, err)
		}
	}

	snapshot, err := BuildSnapshot(ctx, userID, "unit_test", "client_open")
	if err != nil {
		t.Fatalf("BuildSnapshot error: %v", err)
	}
	if snapshot.MainAgent == nil || snapshot.MainAgent.ID != agentID {
		t.Fatalf("expected main agent %d, got %#v", agentID, snapshot.MainAgent)
	}
	if got, want := len(snapshot.MainAgent.ScopeMissing), 0; got != want {
		t.Fatalf("missing scope count=%d want=%d", got, want)
	}

	markdown := RenderMarkdown(snapshot)
	for _, expected := range []string{
		"# Grix 用户状态快照",
		"判定规则：拥有全部允许的 Scope 权限，才视为主 Agent。",
		"当前主 Agent：主控 Agent",
		"介绍：负责创建其他 Agent、建群和协调任务。",
		"类型：openclaw",
	} {
		if !strings.Contains(markdown, expected) {
			t.Fatalf("markdown missing %q:\n%s", expected, markdown)
		}
	}
	for _, unexpected := range []string{"是否已有 Claude", "是否已有 Codex", "是否已有多个 Agent"} {
		if strings.Contains(markdown, unexpected) {
			t.Fatalf("markdown should not contain provider-specific overview %q:\n%s", unexpected, markdown)
		}
	}
}

func TestTriggerOnUserOpenDispatchesInternalMarkdownTask(t *testing.T) {
	setupCustomerCoachTest(t)
	ctx := context.Background()
	userID := int64(301)
	allowCoachTriggerUsers(t, userID)
	customerUserID := int64(302)
	customerAgentID := int64(303)
	sessionID := "customer-session"
	now := time.Now().UTC()

	mustCreateUser(t, userID, "target", "zh", "cn")
	mustCreateUser(t, customerUserID, "customer", "zh", "cn")
	mustCreateAgent(t, model.Agent{
		ID:              customerAgentID,
		AgentName:       "官方客服 Agent",
		OwnerID:         customerUserID,
		ProviderType:    model.AgentProviderAPI,
		AgentClientType: "openclaw",
		Status:          model.AgentStatusActive,
		CreatedAt:       now,
		UpdatedAt:       now,
	})

	settings := systemsetting.DefaultAuthSettings()
	settings.AutoAddCustomerUserID = customerUserID
	if err := systemsetting.SaveAuthSettings(settings, nil); err != nil {
		t.Fatalf("SaveAuthSettings error: %v", err)
	}

	directKey := buildDirectKey(userID, customerUserID, 1)
	if err := store.DB.Create(&model.Session{
		SessionID:   sessionID,
		DirectKey:   &directKey,
		OwnerID:     customerUserID,
		SessionType: model.SessionTypeDirect,
		CreatedAt:   now,
		UpdatedAt:   now,
	}).Error; err != nil {
		t.Fatalf("create customer session: %v", err)
	}
	if err := store.DB.Model(&model.UserSetting{}).
		Where("user_id = ?", customerUserID).
		Update("auto_delegate_agent_id", &customerAgentID).Error; err != nil {
		t.Fatalf("update customer setting: %v", err)
	}

	var dispatched wsagentapi.DelegateEventPayload
	originalDispatch := dispatchCommandDelegateEvent
	dispatchCommandDelegateEvent = func(ctx context.Context, evt wsagentapi.DelegateEventPayload) bool {
		if ctx == nil {
			t.Fatal("dispatch context must not be nil")
		}
		dispatched = evt
		return true
	}
	t.Cleanup(func() {
		dispatchCommandDelegateEvent = originalDispatch
	})

	if err := TriggerOnUserOpen(ctx, userID, "unit_test"); err != nil {
		t.Fatalf("TriggerOnUserOpen error: %v", err)
	}

	if dispatched.EventType != EventTypeCustomerCoachSnapshot {
		t.Fatalf("event_type=%q want %q", dispatched.EventType, EventTypeCustomerCoachSnapshot)
	}
	if dispatched.AgentID != customerAgentID || dispatched.OwnerID != customerUserID || dispatched.SessionID != sessionID {
		t.Fatalf("wrong dispatch target: %#v", dispatched)
	}
	if !dispatched.Command {
		t.Fatalf("internal coach event must be command=true")
	}
	if !strings.Contains(dispatched.Content, "<snapshot_markdown>") ||
		!strings.Contains(dispatched.Content, "# Grix 用户状态快照") ||
		!strings.Contains(dispatched.Content, "这不是用户消息") {
		t.Fatalf("dispatch content missing markdown task:\n%s", dispatched.Content)
	}

	var messageCount int64
	if err := store.DB.Model(&model.Message{}).Count(&messageCount).Error; err != nil {
		t.Fatalf("count messages: %v", err)
	}
	if messageCount != 0 {
		t.Fatalf("internal trigger must not create visible messages, got %d", messageCount)
	}

	delegateKey := fmt.Sprintf("im:delegate:%s:%d", sessionID, customerUserID)
	agentIDRaw, err := store.RDB.HGet(ctx, delegateKey, "agent_id").Result()
	if err != nil {
		t.Fatalf("expected auto-delegate redis key after trigger: %v", err)
	}
	if agentIDRaw != fmt.Sprintf("%d", customerAgentID) {
		t.Fatalf("auto-delegate agent_id=%q want %d", agentIDRaw, customerAgentID)
	}
}

func TestTriggerOnUserOpenDispatchesSharedAutoDelegateAgent(t *testing.T) {
	setupCustomerCoachTest(t)
	ctx := context.Background()
	userID := int64(311)
	allowCoachTriggerUsers(t, userID)
	customerUserID := int64(312)
	agentOwnerID := int64(313)
	sharedAgentID := int64(314)
	sessionID := "customer-shared-session"
	now := time.Now().UTC()

	mustCreateUser(t, userID, "target-shared", "zh", "cn")
	mustCreateUser(t, customerUserID, "customer-shared", "zh", "cn")
	mustCreateUser(t, agentOwnerID, "agent-owner", "zh", "cn")
	mustCreateAgent(t, model.Agent{
		ID:              sharedAgentID,
		AgentName:       "共享客服 Agent",
		OwnerID:         agentOwnerID,
		ProviderType:    model.AgentProviderAPI,
		AgentClientType: "hermes",
		Status:          model.AgentStatusActive,
		CreatedAt:       now,
		UpdatedAt:       now,
	})
	if err := store.DB.Create(&model.AgentShare{
		ID:        31401,
		AgentID:   sharedAgentID,
		OwnerID:   agentOwnerID,
		SharedTo:  customerUserID,
		Status:    model.AgentShareStatusActive,
		CreatedAt: now,
		UpdatedAt: now,
	}).Error; err != nil {
		t.Fatalf("create agent share: %v", err)
	}

	settings := systemsetting.DefaultAuthSettings()
	settings.AutoAddCustomerUserID = customerUserID
	if err := systemsetting.SaveAuthSettings(settings, nil); err != nil {
		t.Fatalf("SaveAuthSettings error: %v", err)
	}

	directKey := buildDirectKey(userID, customerUserID, 1)
	if err := store.DB.Create(&model.Session{
		SessionID:   sessionID,
		DirectKey:   &directKey,
		OwnerID:     customerUserID,
		SessionType: model.SessionTypeDirect,
		CreatedAt:   now,
		UpdatedAt:   now,
	}).Error; err != nil {
		t.Fatalf("create customer session: %v", err)
	}
	if err := store.DB.Model(&model.UserSetting{}).
		Where("user_id = ?", customerUserID).
		Update("auto_delegate_agent_id", &sharedAgentID).Error; err != nil {
		t.Fatalf("update customer setting: %v", err)
	}

	var dispatched wsagentapi.DelegateEventPayload
	originalDispatch := dispatchCommandDelegateEvent
	dispatchCommandDelegateEvent = func(ctx context.Context, evt wsagentapi.DelegateEventPayload) bool {
		dispatched = evt
		return true
	}
	t.Cleanup(func() {
		dispatchCommandDelegateEvent = originalDispatch
	})

	if err := TriggerOnUserOpen(ctx, userID, "unit_test_shared"); err != nil {
		t.Fatalf("TriggerOnUserOpen error: %v", err)
	}

	if dispatched.EventType != EventTypeCustomerCoachSnapshot {
		t.Fatalf("event_type=%q want %q", dispatched.EventType, EventTypeCustomerCoachSnapshot)
	}
	if dispatched.AgentID != sharedAgentID || dispatched.OwnerID != customerUserID || dispatched.SessionID != sessionID {
		t.Fatalf("wrong shared dispatch target: %#v", dispatched)
	}
	if !dispatched.Command {
		t.Fatalf("internal coach event must be command=true")
	}

	var messageCount int64
	if err := store.DB.Model(&model.Message{}).Count(&messageCount).Error; err != nil {
		t.Fatalf("count messages: %v", err)
	}
	if messageCount != 0 {
		t.Fatalf("internal trigger must not create visible messages, got %d", messageCount)
	}
}

func TestTriggerOnUserOpenSkipsUnsharedForeignAutoDelegateAgent(t *testing.T) {
	setupCustomerCoachTest(t)
	ctx := context.Background()
	userID := int64(321)
	allowCoachTriggerUsers(t, userID)
	customerUserID := int64(322)
	agentOwnerID := int64(323)
	foreignAgentID := int64(324)
	sessionID := "customer-foreign-session"
	now := time.Now().UTC()

	mustCreateUser(t, userID, "target-foreign", "zh", "cn")
	mustCreateUser(t, customerUserID, "customer-foreign", "zh", "cn")
	mustCreateUser(t, agentOwnerID, "foreign-owner", "zh", "cn")
	mustCreateAgent(t, model.Agent{
		ID:           foreignAgentID,
		AgentName:    "未共享 Agent",
		OwnerID:      agentOwnerID,
		ProviderType: model.AgentProviderAPI,
		Status:       model.AgentStatusActive,
		CreatedAt:    now,
		UpdatedAt:    now,
	})

	settings := systemsetting.DefaultAuthSettings()
	settings.AutoAddCustomerUserID = customerUserID
	if err := systemsetting.SaveAuthSettings(settings, nil); err != nil {
		t.Fatalf("SaveAuthSettings error: %v", err)
	}

	directKey := buildDirectKey(userID, customerUserID, 1)
	if err := store.DB.Create(&model.Session{
		SessionID:   sessionID,
		DirectKey:   &directKey,
		OwnerID:     customerUserID,
		SessionType: model.SessionTypeDirect,
		CreatedAt:   now,
		UpdatedAt:   now,
	}).Error; err != nil {
		t.Fatalf("create customer session: %v", err)
	}
	if err := store.DB.Model(&model.UserSetting{}).
		Where("user_id = ?", customerUserID).
		Update("auto_delegate_agent_id", &foreignAgentID).Error; err != nil {
		t.Fatalf("update customer setting: %v", err)
	}

	dispatched := false
	originalDispatch := dispatchCommandDelegateEvent
	dispatchCommandDelegateEvent = func(ctx context.Context, evt wsagentapi.DelegateEventPayload) bool {
		dispatched = true
		return true
	}
	t.Cleanup(func() {
		dispatchCommandDelegateEvent = originalDispatch
	})

	if err := TriggerOnUserOpen(ctx, userID, "unit_test_foreign"); err != nil {
		t.Fatalf("TriggerOnUserOpen error: %v", err)
	}
	if dispatched {
		t.Fatal("unshared foreign auto-delegate must not dispatch")
	}
}

func TestBuildSnapshotCountsGroupUserMessagesOnceWithMultipleAgents(t *testing.T) {
	setupCustomerCoachTest(t)
	ctx := context.Background()
	userID := int64(401)
	agentAID := int64(501)
	agentBID := int64(502)
	sessionID := "multi-agent-group"
	now := time.Now().UTC()

	mustCreateUser(t, userID, "group-user", "zh", "cn")
	mustCreateAgent(t, model.Agent{
		ID:           agentAID,
		AgentName:    "Agent A",
		OwnerID:      userID,
		ProviderType: model.AgentProviderAPI,
		Status:       model.AgentStatusActive,
		CreatedAt:    now,
		UpdatedAt:    now,
	})
	mustCreateAgent(t, model.Agent{
		ID:           agentBID,
		AgentName:    "Agent B",
		OwnerID:      userID,
		ProviderType: model.AgentProviderAPI,
		Status:       model.AgentStatusActive,
		CreatedAt:    now,
		UpdatedAt:    now,
	})
	if err := store.DB.Create(&model.Session{
		SessionID:   sessionID,
		OwnerID:     userID,
		SessionType: model.SessionTypeGroup,
		GroupName:   "协作群",
		CreatedAt:   now,
		UpdatedAt:   now,
	}).Error; err != nil {
		t.Fatalf("create group session: %v", err)
	}
	for _, member := range []model.SessionMember{
		{SessionID: sessionID, MemberID: userID, MemberType: 1, JoinedAt: now, LastActiveAt: now},
		{SessionID: sessionID, MemberID: agentAID, MemberType: 2, JoinedAt: now, LastActiveAt: now},
		{SessionID: sessionID, MemberID: agentBID, MemberType: 2, JoinedAt: now, LastActiveAt: now},
	} {
		if err := store.DB.Create(&member).Error; err != nil {
			t.Fatalf("create session member: %v", err)
		}
	}
	if err := store.DB.Create(&model.Message{
		MsgID:      9001,
		SessionID:  sessionID,
		SenderID:   userID,
		SenderType: 1,
		MsgType:    1,
		Content:    "请两个 Agent 协作",
		CreatedAt:  now,
	}).Error; err != nil {
		t.Fatalf("create message: %v", err)
	}

	snapshot, err := BuildSnapshot(ctx, userID, "unit_test", "client_open")
	if err != nil {
		t.Fatalf("BuildSnapshot error: %v", err)
	}
	if snapshot.Usage.AgentMessageCount != 1 {
		t.Fatalf("agent message count=%d want 1", snapshot.Usage.AgentMessageCount)
	}
	if snapshot.Sessions.MultiAgentGroups != 1 {
		t.Fatalf("multi-agent group count=%d want 1", snapshot.Sessions.MultiAgentGroups)
	}
}

func TestTriggerOnUserOpenSkipsWhenOnboardingComplete(t *testing.T) {
	setupCustomerCoachTest(t)
	ctx := context.Background()
	userID := int64(601)
	allowCoachTriggerUsers(t, userID)
	customerUserID := int64(602)
	customerAgentID := int64(603)
	mainAgentID := int64(604)
	secondAgentID := int64(605)
	customerSessionID := "customer-complete-session"
	groupSessionID := "user-complete-group"
	now := time.Now().UTC()

	mustCreateUser(t, userID, "complete-user", "zh", "cn")
	mustCreateUser(t, customerUserID, "complete-customer", "zh", "cn")
	mustCreateAgent(t, model.Agent{
		ID:           customerAgentID,
		AgentName:    "客服托管",
		OwnerID:      customerUserID,
		ProviderType: model.AgentProviderAPI,
		Status:       model.AgentStatusActive,
		CreatedAt:    now,
		UpdatedAt:    now,
	})
	mustCreateAgent(t, model.Agent{
		ID:              mainAgentID,
		AgentName:       "主控",
		OwnerID:         userID,
		ProviderType:    model.AgentProviderAPI,
		AgentClientType: "openclaw",
		Status:          model.AgentStatusActive,
		CreatedAt:       now,
		UpdatedAt:       now,
	})
	mustCreateAgent(t, model.Agent{
		ID:           secondAgentID,
		AgentName:    "协作者",
		OwnerID:      userID,
		ProviderType: model.AgentProviderAPI,
		Status:       model.AgentStatusActive,
		CreatedAt:    now,
		UpdatedAt:    now,
	})
	for _, scope := range agentscope.AllowedScopes() {
		if err := store.DB.Create(&model.AgentAPIScope{
			AgentID: mainAgentID,
			Scope:   scope,
		}).Error; err != nil {
			t.Fatalf("create scope %s: %v", scope, err)
		}
	}

	settings := systemsetting.DefaultAuthSettings()
	settings.AutoAddCustomerUserID = customerUserID
	if err := systemsetting.SaveAuthSettings(settings, nil); err != nil {
		t.Fatalf("SaveAuthSettings error: %v", err)
	}

	directKey := buildDirectKey(userID, customerUserID, 1)
	if err := store.DB.Create(&model.Session{
		SessionID:   customerSessionID,
		DirectKey:   &directKey,
		OwnerID:     customerUserID,
		SessionType: model.SessionTypeDirect,
		CreatedAt:   now,
		UpdatedAt:   now,
	}).Error; err != nil {
		t.Fatalf("create customer session: %v", err)
	}
	if err := store.DB.Model(&model.UserSetting{}).
		Where("user_id = ?", customerUserID).
		Update("auto_delegate_agent_id", &customerAgentID).Error; err != nil {
		t.Fatalf("update customer setting: %v", err)
	}

	if err := store.DB.Create(&model.Session{
		SessionID:   groupSessionID,
		OwnerID:     userID,
		SessionType: model.SessionTypeGroup,
		GroupName:   "完整协作群",
		CreatedAt:   now,
		UpdatedAt:   now,
	}).Error; err != nil {
		t.Fatalf("create group session: %v", err)
	}
	for _, member := range []model.SessionMember{
		{SessionID: groupSessionID, MemberID: userID, MemberType: 1, JoinedAt: now, LastActiveAt: now},
		{SessionID: groupSessionID, MemberID: mainAgentID, MemberType: 2, JoinedAt: now, LastActiveAt: now},
		{SessionID: groupSessionID, MemberID: secondAgentID, MemberType: 2, JoinedAt: now, LastActiveAt: now},
	} {
		if err := store.DB.Create(&member).Error; err != nil {
			t.Fatalf("create session member: %v", err)
		}
	}
	if err := store.DB.Create(&model.Message{
		MsgID:      9601,
		SessionID:  groupSessionID,
		SenderID:   userID,
		SenderType: 1,
		MsgType:    1,
		Content:    "开始协作",
		CreatedAt:  now,
	}).Error; err != nil {
		t.Fatalf("create message: %v", err)
	}
	if err := store.DB.Create(&model.CallRecord{
		ID:        9701,
		SessionID: groupSessionID,
		CallerID:  userID,
		CalleeID:  mainAgentID,
		CallMode:  1,
		State:     1,
		CreatedAt: now,
	}).Error; err != nil {
		t.Fatalf("create call record: %v", err)
	}

	dispatched := false
	originalDispatch := dispatchCommandDelegateEvent
	dispatchCommandDelegateEvent = func(ctx context.Context, evt wsagentapi.DelegateEventPayload) bool {
		dispatched = true
		return true
	}
	t.Cleanup(func() {
		dispatchCommandDelegateEvent = originalDispatch
	})

	if err := TriggerOnUserOpen(ctx, userID, "unit_test_complete"); err != nil {
		t.Fatalf("TriggerOnUserOpen error: %v", err)
	}
	if dispatched {
		t.Fatal("fully onboarded user must not dispatch coach snapshot")
	}
}

func TestTriggerOnUserOpenSkipsRecentCustomerSessionActivity(t *testing.T) {
	setupCustomerCoachTest(t)
	ctx := context.Background()
	userID := int64(651)
	allowCoachTriggerUsers(t, userID)
	customerUserID := int64(652)
	customerAgentID := int64(653)
	sessionID := "customer-recent-active-session"
	now := time.Now().UTC()

	mustCreateUser(t, userID, "recent-active-user", "zh", "cn")
	mustCreateUser(t, customerUserID, "recent-active-customer", "zh", "cn")
	mustCreateAgent(t, model.Agent{
		ID:           customerAgentID,
		AgentName:    "客服托管",
		OwnerID:      customerUserID,
		ProviderType: model.AgentProviderAPI,
		Status:       model.AgentStatusActive,
		CreatedAt:    now,
		UpdatedAt:    now,
	})

	settings := systemsetting.DefaultAuthSettings()
	settings.AutoAddCustomerUserID = customerUserID
	if err := systemsetting.SaveAuthSettings(settings, nil); err != nil {
		t.Fatalf("SaveAuthSettings error: %v", err)
	}

	directKey := buildDirectKey(userID, customerUserID, 1)
	if err := store.DB.Create(&model.Session{
		SessionID:   sessionID,
		DirectKey:   &directKey,
		OwnerID:     customerUserID,
		SessionType: model.SessionTypeDirect,
		CreatedAt:   now,
		UpdatedAt:   now,
	}).Error; err != nil {
		t.Fatalf("create customer session: %v", err)
	}
	if err := store.DB.Model(&model.UserSetting{}).
		Where("user_id = ?", customerUserID).
		Update("auto_delegate_agent_id", &customerAgentID).Error; err != nil {
		t.Fatalf("update customer setting: %v", err)
	}
	if err := store.DB.Create(&model.Message{
		MsgID:      9651,
		SessionID:  sessionID,
		SenderID:   userID,
		SenderType: 1,
		MsgType:    1,
		Content:    "我刚刚还在排查 Claude 上线问题",
		CreatedAt:  now.Add(-5 * time.Minute),
	}).Error; err != nil {
		t.Fatalf("create recent customer message: %v", err)
	}

	dispatched := false
	originalDispatch := dispatchCommandDelegateEvent
	dispatchCommandDelegateEvent = func(ctx context.Context, evt wsagentapi.DelegateEventPayload) bool {
		dispatched = true
		return true
	}
	t.Cleanup(func() {
		dispatchCommandDelegateEvent = originalDispatch
	})

	if err := TriggerOnUserOpen(ctx, userID, "unit_test_recent_active"); err != nil {
		t.Fatalf("TriggerOnUserOpen error: %v", err)
	}
	if dispatched {
		t.Fatal("recent visible customer session activity must not dispatch coach snapshot")
	}
	delegateKey := fmt.Sprintf("im:delegate:%s:%d", sessionID, customerUserID)
	exists, err := store.RDB.Exists(ctx, delegateKey).Result()
	if err != nil {
		t.Fatalf("check auto-delegate key: %v", err)
	}
	if exists != 0 {
		t.Fatal("recent activity skip must happen before auto-delegate activation")
	}
}

func TestTriggerOnUserOpenDispatchesAfterRecentActivityWindow(t *testing.T) {
	setupCustomerCoachTest(t)
	ctx := context.Background()
	userID := int64(661)
	allowCoachTriggerUsers(t, userID)
	customerUserID := int64(662)
	customerAgentID := int64(663)
	sessionID := "customer-old-active-session"
	now := time.Now().UTC()

	mustCreateUser(t, userID, "old-active-user", "zh", "cn")
	mustCreateUser(t, customerUserID, "old-active-customer", "zh", "cn")
	mustCreateAgent(t, model.Agent{
		ID:           customerAgentID,
		AgentName:    "客服托管",
		OwnerID:      customerUserID,
		ProviderType: model.AgentProviderAPI,
		Status:       model.AgentStatusActive,
		CreatedAt:    now,
		UpdatedAt:    now,
	})

	settings := systemsetting.DefaultAuthSettings()
	settings.AutoAddCustomerUserID = customerUserID
	if err := systemsetting.SaveAuthSettings(settings, nil); err != nil {
		t.Fatalf("SaveAuthSettings error: %v", err)
	}

	directKey := buildDirectKey(userID, customerUserID, 1)
	if err := store.DB.Create(&model.Session{
		SessionID:   sessionID,
		DirectKey:   &directKey,
		OwnerID:     customerUserID,
		SessionType: model.SessionTypeDirect,
		CreatedAt:   now,
		UpdatedAt:   now,
	}).Error; err != nil {
		t.Fatalf("create customer session: %v", err)
	}
	if err := store.DB.Model(&model.UserSetting{}).
		Where("user_id = ?", customerUserID).
		Update("auto_delegate_agent_id", &customerAgentID).Error; err != nil {
		t.Fatalf("update customer setting: %v", err)
	}
	if err := store.DB.Create(&model.Message{
		MsgID:      9661,
		SessionID:  sessionID,
		SenderID:   userID,
		SenderType: 1,
		MsgType:    1,
		Content:    "两个多小时前的排查消息",
		CreatedAt:  now.Add(-coachRecentActivityWindow - time.Minute),
	}).Error; err != nil {
		t.Fatalf("create old customer message: %v", err)
	}
	if err := store.DB.Create(&model.Message{
		MsgID:      9662,
		SessionID:  sessionID,
		SenderID:   customerUserID,
		SenderType: 1,
		MsgType:    1,
		Content:    "近期客服欢迎语不应压掉用户再次打开后的引导",
		CreatedAt:  now.Add(-5 * time.Minute),
	}).Error; err != nil {
		t.Fatalf("create recent customer welcome message: %v", err)
	}

	var dispatched wsagentapi.DelegateEventPayload
	originalDispatch := dispatchCommandDelegateEvent
	dispatchCommandDelegateEvent = func(ctx context.Context, evt wsagentapi.DelegateEventPayload) bool {
		dispatched = evt
		return true
	}
	t.Cleanup(func() {
		dispatchCommandDelegateEvent = originalDispatch
	})

	if err := TriggerOnUserOpen(ctx, userID, "unit_test_old_active"); err != nil {
		t.Fatalf("TriggerOnUserOpen error: %v", err)
	}
	if dispatched.EventType != EventTypeCustomerCoachSnapshot {
		t.Fatalf("old customer session activity should not block dispatch, got %#v", dispatched)
	}
}

func TestTriggerOnUserOpenSkipsNonAllowlistedUser(t *testing.T) {
	setupCustomerCoachTest(t)
	ctx := context.Background()
	userID := int64(701)
	customerUserID := int64(702)
	customerAgentID := int64(703)
	sessionID := "customer-non-allow-session"
	now := time.Now().UTC()

	// Gate missing / not whitelisted: production default until admin creates the gate.
	mustCreateUser(t, userID, "non-allow-user", "zh", "cn")
	mustCreateUser(t, customerUserID, "non-allow-customer", "zh", "cn")
	mustCreateAgent(t, model.Agent{
		ID:           customerAgentID,
		AgentName:    "客服托管",
		OwnerID:      customerUserID,
		ProviderType: model.AgentProviderAPI,
		Status:       model.AgentStatusActive,
		CreatedAt:    now,
		UpdatedAt:    now,
	})

	settings := systemsetting.DefaultAuthSettings()
	settings.AutoAddCustomerUserID = customerUserID
	if err := systemsetting.SaveAuthSettings(settings, nil); err != nil {
		t.Fatalf("SaveAuthSettings error: %v", err)
	}

	directKey := buildDirectKey(userID, customerUserID, 1)
	if err := store.DB.Create(&model.Session{
		SessionID:   sessionID,
		DirectKey:   &directKey,
		OwnerID:     customerUserID,
		SessionType: model.SessionTypeDirect,
		CreatedAt:   now,
		UpdatedAt:   now,
	}).Error; err != nil {
		t.Fatalf("create customer session: %v", err)
	}
	if err := store.DB.Model(&model.UserSetting{}).
		Where("user_id = ?", customerUserID).
		Update("auto_delegate_agent_id", &customerAgentID).Error; err != nil {
		t.Fatalf("update customer setting: %v", err)
	}

	dispatched := false
	originalDispatch := dispatchCommandDelegateEvent
	dispatchCommandDelegateEvent = func(ctx context.Context, evt wsagentapi.DelegateEventPayload) bool {
		dispatched = true
		return true
	}
	t.Cleanup(func() {
		dispatchCommandDelegateEvent = originalDispatch
	})

	if err := TriggerOnUserOpen(ctx, userID, "unit_test_non_allow"); err != nil {
		t.Fatalf("TriggerOnUserOpen error: %v", err)
	}
	if dispatched {
		t.Fatal("non-allowlisted user must not dispatch coach snapshot")
	}
}

func mustCreateUser(t *testing.T, userID int64, username, locale, region string) {
	t.Helper()
	if err := store.DB.Create(&model.User{
		ID:           userID,
		Username:     username,
		Email:        username + "@example.com",
		PasswordHash: "x",
		Nickname:     username,
		Status:       model.UserStatusActive,
		Region:       region,
	}).Error; err != nil {
		t.Fatalf("create user %d: %v", userID, err)
	}
	if err := store.DB.Create(&model.UserSetting{
		UserID:            userID,
		PreferredLanguage: locale,
	}).Error; err != nil {
		t.Fatalf("create user setting %d: %v", userID, err)
	}
}

func mustCreateAgent(t *testing.T, agent model.Agent) {
	t.Helper()
	if agent.CreatedAt.IsZero() {
		agent.CreatedAt = time.Now().UTC()
	}
	if agent.UpdatedAt.IsZero() {
		agent.UpdatedAt = agent.CreatedAt
	}
	if err := store.DB.Create(&agent).Error; err != nil {
		t.Fatalf("create agent %d: %v", agent.ID, err)
	}
}

func TestCoachPromptsForbidReasoningLeak(t *testing.T) {
	prompts := map[string]string{
		"internal_task":     buildInternalTask("# snapshot"),
		"rendered_markdown": RenderMarkdown(Snapshot{}),
	}
	for name, prompt := range prompts {
		for _, want := range []string{"自然客服口吻", "严禁", "快照显示", "/no_reply"} {
			if !strings.Contains(prompt, want) {
				t.Fatalf("%s prompt missing %q:\n%s", name, want, prompt)
			}
		}
	}
}

func TestCoachPromptsDefaultChineseLanguage(t *testing.T) {
	prompts := map[string]string{
		"internal_task":     buildInternalTask("# snapshot"),
		"rendered_markdown": RenderMarkdown(Snapshot{}),
	}
	for name, prompt := range prompts {
		for _, want := range []string{"中国区客服", "使用中文", "英文服务语境"} {
			if !strings.Contains(prompt, want) {
				t.Fatalf("%s prompt missing Chinese-default language directive %q:\n%s", name, want, prompt)
			}
		}
	}
}
