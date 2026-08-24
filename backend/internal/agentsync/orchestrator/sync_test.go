package orchestrator

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/askie/grix/backend/internal/agentsync"
	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/snowflake"
	"github.com/askie/grix/backend/internal/pkg/testutil"
	"github.com/askie/grix/backend/internal/store"
	wsagentapi "github.com/askie/grix/backend/internal/ws/agentapi"
	"golang.org/x/sync/singleflight"
)

type fakeHistorySyncClient struct {
	cursors   []string
	responses []*wsagentapi.SessionHistorySyncResponse
	errs      []error
}

func (f *fakeHistorySyncClient) SendSessionHistorySyncActionAndWait(
	_, _ int64,
	_ string,
	_ string,
	_ string,
	_ string,
	_ string,
	cursor string,
	_ int,
	_ string,
) (*wsagentapi.SessionHistorySyncResponse, error) {
	f.cursors = append(f.cursors, cursor)
	resp := f.responses[0]
	f.responses = f.responses[1:]
	var err error
	if len(f.errs) > 0 {
		err = f.errs[0]
		f.errs = f.errs[1:]
	}
	return resp, err
}

func TestSyncBoundSessionHistoryPersistsAndReusesFinalCursor(t *testing.T) {
	testDB := testutil.NewTestDB()
	defer testDB.Close()
	store.DB = testDB.DB
	store.RDB = nil
	if err := snowflake.Init(1); err != nil {
		t.Fatalf("init snowflake: %v", err)
	}

	ownerID := int64(7201)
	agentID := int64(8201)
	sessionID := "history-orchestrator-session"
	now := time.Now().UTC()
	if err := testDB.DB.Create(&model.Session{
		SessionID: sessionID, OwnerID: ownerID, SessionType: model.SessionTypeDirect,
	}).Error; err != nil {
		t.Fatalf("create session: %v", err)
	}
	if err := testDB.DB.Create(&[]model.SessionMember{
		{SessionID: sessionID, MemberID: ownerID, MemberType: 1, Role: 3, JoinedAt: now, LastActiveAt: now},
		{SessionID: sessionID, MemberID: agentID, MemberType: 2, Role: 1, JoinedAt: now, LastActiveAt: now},
	}).Error; err != nil {
		t.Fatalf("create session members: %v", err)
	}
	if err := testDB.DB.Create(&model.AgentSessionBinding{
		AgentID: agentID, SessionID: sessionID, ProviderKey: "claude", BindingID: "native-1", Cwd: "/workspace", Status: "active",
	}).Error; err != nil {
		t.Fatalf("create binding: %v", err)
	}
	ident := agentsync.SyncIdentity{
		AgentID: agentID, OwnerID: ownerID, SessionID: sessionID,
		ProviderKey: "claude", BindingID: "native-1", SyncRunID: agentsync.NewSyncRunID(),
	}
	// 绑定导入时显式登记导入意图;没有这行状态,orchestrator 必须直接跳过。
	if err := agentsync.Queue(context.Background(), ident); err != nil {
		t.Fatalf("queue import intent: %v", err)
	}

	client := &fakeHistorySyncClient{responses: []*wsagentapi.SessionHistorySyncResponse{
		{
			Messages: []agentsync.NativeMessage{{NativeMessageID: "m1", Role: "user", Content: "one", CreatedAt: now.Add(-time.Minute)}},
			HasMore:  true, NextCursor: "cursor-1",
		},
		{
			Messages: []agentsync.NativeMessage{{NativeMessageID: "m2", Role: "assistant", Content: "two", CreatedAt: now}},
			HasMore:  false, NextCursor: "cursor-final",
		},
	}}
	previousProvider := historySyncClientProvider
	historySyncClientProvider = func() historySyncClient { return client }
	historySyncGroup = singleflight.Group{}
	t.Cleanup(func() {
		historySyncClientProvider = previousProvider
		historySyncGroup = singleflight.Group{}
	})

	imported, err := SyncBoundSessionHistory(context.Background(), ownerID, sessionID)
	if err != nil {
		t.Fatalf("first sync: %v", err)
	}
	if imported != 2 {
		t.Fatalf("imported=%d want=2", imported)
	}
	if len(client.cursors) != 2 || client.cursors[0] != "" || client.cursors[1] != "cursor-1" {
		t.Fatalf("first sync cursors=%v", client.cursors)
	}

	// 一次性导入完成后必须永久停止:若这里再次请求连接器,首次导入之后经 live
	// 通道产生的轮次会被当成"历史"重复导入(live 消息没有 native id,去重表
	// 拦不住)。这是防重复消息的核心守卫。
	client.responses = []*wsagentapi.SessionHistorySyncResponse{{HasMore: false, NextCursor: "cursor-final"}}
	client.cursors = nil
	imported, err = SyncBoundSessionHistory(context.Background(), ownerID, sessionID)
	if err != nil {
		t.Fatalf("second sync: %v", err)
	}
	if imported != 0 {
		t.Fatalf("second imported=%d want=0", imported)
	}
	if len(client.cursors) != 0 {
		t.Fatalf("completed import must not re-sync, got connector calls with cursors=%v", client.cursors)
	}

	var state model.AgentSessionSyncState
	if err := testDB.DB.Where("agent_id = ? AND session_id = ?", agentID, sessionID).First(&state).Error; err != nil {
		t.Fatalf("load sync state: %v", err)
	}
	if state.Status != model.AgentSessionSyncStatusCompleted || state.Cursor != "cursor-final" {
		t.Fatalf("sync state=%+v", state)
	}

	// 后续小节验证游标失效恢复/翻页上限,需要一个未完成的导入;显式重新排队
	// 并保留游标,模拟一次尚在进行中的导入。
	if err := agentsync.QueueAtCursor(context.Background(), ident, "cursor-final"); err != nil {
		t.Fatalf("requeue at cursor-final: %v", err)
	}
	client.responses = []*wsagentapi.SessionHistorySyncResponse{
		{ErrorCode: wsagentapi.SessionHistorySyncErrorInvalidCursor, ErrorMsg: "stale cursor"},
		{HasMore: false, NextCursor: "cursor-after-reset"},
	}
	client.errs = []error{
		&wsagentapi.SessionHistorySyncError{Code: wsagentapi.SessionHistorySyncErrorInvalidCursor, Message: "stale cursor"},
		nil,
	}
	client.cursors = nil
	if _, err := SyncBoundSessionHistory(context.Background(), ownerID, sessionID); err != nil {
		t.Fatalf("invalid cursor recovery: %v", err)
	}
	if len(client.cursors) != 2 || client.cursors[0] != "cursor-final" || client.cursors[1] != "" {
		t.Fatalf("recovery cursors=%v want=[cursor-final empty]", client.cursors)
	}
	if err := testDB.DB.Where("agent_id = ? AND session_id = ?", agentID, sessionID).First(&state).Error; err != nil {
		t.Fatalf("reload recovered sync state: %v", err)
	}
	if state.Status != model.AgentSessionSyncStatusCompleted || state.Cursor != "cursor-after-reset" {
		t.Fatalf("recovered sync state=%+v", state)
	}

	if err := agentsync.QueueAtCursor(context.Background(), ident, "cursor-after-reset"); err != nil {
		t.Fatalf("requeue at cursor-after-reset: %v", err)
	}
	client.responses = []*wsagentapi.SessionHistorySyncResponse{
		{ErrorCode: wsagentapi.SessionHistorySyncErrorInvalidCursor, ErrorMsg: "stale cursor"},
		{ErrorCode: wsagentapi.SessionHistorySyncErrorInvalidCursor, ErrorMsg: "reset cursor rejected"},
	}
	client.errs = []error{
		&wsagentapi.SessionHistorySyncError{Code: wsagentapi.SessionHistorySyncErrorInvalidCursor, Message: "stale cursor"},
		&wsagentapi.SessionHistorySyncError{Code: wsagentapi.SessionHistorySyncErrorInvalidCursor, Message: "reset cursor rejected"},
	}
	client.cursors = nil
	if _, err := SyncBoundSessionHistory(context.Background(), ownerID, sessionID); err == nil {
		t.Fatal("repeated invalid cursor should fail after one reset")
	}
	if len(client.cursors) != 2 || client.cursors[0] != "cursor-after-reset" || client.cursors[1] != "" {
		t.Fatalf("failed recovery cursors=%v want=[cursor-after-reset empty]", client.cursors)
	}

	client.responses = make([]*wsagentapi.SessionHistorySyncResponse, historySyncMaxPages)
	for i := range client.responses {
		client.responses[i] = &wsagentapi.SessionHistorySyncResponse{
			HasMore: true, NextCursor: fmt.Sprintf("cursor-page-%d", i+1),
		}
	}
	client.cursors = nil
	if _, err := SyncBoundSessionHistory(context.Background(), ownerID, sessionID); err != nil {
		t.Fatalf("page-limited sync: %v", err)
	}
	if len(client.cursors) != historySyncMaxPages {
		t.Fatalf("page-limited calls=%d want=%d", len(client.cursors), historySyncMaxPages)
	}
	if err := testDB.DB.Where("agent_id = ? AND session_id = ?", agentID, sessionID).First(&state).Error; err != nil {
		t.Fatalf("reload partial sync state: %v", err)
	}
	if state.Status != model.AgentSessionSyncStatusPartial || state.Cursor != "cursor-page-20" {
		t.Fatalf("partial sync state=%+v", state)
	}
}

// 守卫:没有导入意图(无 sync state 行)的绑定绝不触发历史同步。普通聊天的
// 绑定都落在这里;一旦回归,每次打开会话都会把 provider 本地滚动日志整个
// 导进来,live 消息全部翻倍。
func TestSyncSkipsBindingWithoutImportIntent(t *testing.T) {
	testDB := testutil.NewTestDB()
	defer testDB.Close()
	store.DB = testDB.DB
	store.RDB = nil
	if err := snowflake.Init(1); err != nil {
		t.Fatalf("init snowflake: %v", err)
	}

	ownerID := int64(7301)
	agentID := int64(8301)
	sessionID := "history-orchestrator-no-intent"
	now := time.Now().UTC()
	if err := testDB.DB.Create(&model.Session{
		SessionID: sessionID, OwnerID: ownerID, SessionType: model.SessionTypeDirect,
	}).Error; err != nil {
		t.Fatalf("create session: %v", err)
	}
	if err := testDB.DB.Create(&[]model.SessionMember{
		{SessionID: sessionID, MemberID: ownerID, MemberType: 1, Role: 3, JoinedAt: now, LastActiveAt: now},
		{SessionID: sessionID, MemberID: agentID, MemberType: 2, Role: 1, JoinedAt: now, LastActiveAt: now},
	}).Error; err != nil {
		t.Fatalf("create session members: %v", err)
	}
	if err := testDB.DB.Create(&model.AgentSessionBinding{
		AgentID: agentID, SessionID: sessionID, ProviderKey: "codex", BindingID: "thread-live", Cwd: "/workspace", Status: "active",
	}).Error; err != nil {
		t.Fatalf("create binding: %v", err)
	}

	client := &fakeHistorySyncClient{}
	previousProvider := historySyncClientProvider
	historySyncClientProvider = func() historySyncClient { return client }
	historySyncGroup = singleflight.Group{}
	t.Cleanup(func() {
		historySyncClientProvider = previousProvider
		historySyncGroup = singleflight.Group{}
	})

	imported, err := SyncBoundSessionHistory(context.Background(), ownerID, sessionID)
	if err != nil {
		t.Fatalf("sync without intent: %v", err)
	}
	if imported != 0 {
		t.Fatalf("imported=%d want=0", imported)
	}
	if len(client.cursors) != 0 {
		t.Fatalf("binding without import intent must not call connector, cursors=%v", client.cursors)
	}
	var stateCount int64
	if err := testDB.DB.Model(&model.AgentSessionSyncState{}).
		Where("agent_id = ? AND session_id = ?", agentID, sessionID).
		Count(&stateCount).Error; err != nil {
		t.Fatalf("count sync state: %v", err)
	}
	if stateCount != 0 {
		t.Fatalf("sync must not create state rows on its own, count=%d", stateCount)
	}
}

// 守卫:历史导入不依赖 binding.Status 字符串。工具栏 local action 结果会把
// status 覆盖成 "opened"/"model_set" 等任意 outcome;只要导入意图还在且未
// completed,导入就必须继续,不能被工具栏操作静默打断。
func TestSyncRunsDespiteNonActiveBindingStatus(t *testing.T) {
	testDB := testutil.NewTestDB()
	defer testDB.Close()
	store.DB = testDB.DB
	store.RDB = nil
	if err := snowflake.Init(1); err != nil {
		t.Fatalf("init snowflake: %v", err)
	}

	ownerID := int64(7401)
	agentID := int64(8401)
	sessionID := "history-orchestrator-toolbar-status"
	now := time.Now().UTC()
	if err := testDB.DB.Create(&model.Session{
		SessionID: sessionID, OwnerID: ownerID, SessionType: model.SessionTypeDirect,
	}).Error; err != nil {
		t.Fatalf("create session: %v", err)
	}
	if err := testDB.DB.Create(&[]model.SessionMember{
		{SessionID: sessionID, MemberID: ownerID, MemberType: 1, Role: 3, JoinedAt: now, LastActiveAt: now},
		{SessionID: sessionID, MemberID: agentID, MemberType: 2, Role: 1, JoinedAt: now, LastActiveAt: now},
	}).Error; err != nil {
		t.Fatalf("create session members: %v", err)
	}
	// 模拟导入进行到一半时用户点了工具栏:status 已被覆盖成 model_set。
	if err := testDB.DB.Create(&model.AgentSessionBinding{
		AgentID: agentID, SessionID: sessionID, ProviderKey: "codex", BindingID: "thread-import", Cwd: "/workspace", Status: "model_set",
	}).Error; err != nil {
		t.Fatalf("create binding: %v", err)
	}
	ident := agentsync.SyncIdentity{
		AgentID: agentID, OwnerID: ownerID, SessionID: sessionID,
		ProviderKey: "codex", BindingID: "thread-import", SyncRunID: agentsync.NewSyncRunID(),
	}
	if err := agentsync.Queue(context.Background(), ident); err != nil {
		t.Fatalf("queue import intent: %v", err)
	}

	client := &fakeHistorySyncClient{responses: []*wsagentapi.SessionHistorySyncResponse{
		{
			Messages: []agentsync.NativeMessage{{NativeMessageID: "n1", Role: "user", Content: "hello", CreatedAt: now.Add(-time.Hour)}},
			HasMore:  false, NextCursor: "cursor-done",
		},
	}}
	previousProvider := historySyncClientProvider
	historySyncClientProvider = func() historySyncClient { return client }
	historySyncGroup = singleflight.Group{}
	t.Cleanup(func() {
		historySyncClientProvider = previousProvider
		historySyncGroup = singleflight.Group{}
	})

	imported, err := SyncBoundSessionHistory(context.Background(), ownerID, sessionID)
	if err != nil {
		t.Fatalf("sync with non-active binding status: %v", err)
	}
	if imported != 1 {
		t.Fatalf("imported=%d want=1", imported)
	}
	var state model.AgentSessionSyncState
	if err := testDB.DB.Where("agent_id = ? AND session_id = ?", agentID, sessionID).First(&state).Error; err != nil {
		t.Fatalf("load sync state: %v", err)
	}
	if state.Status != model.AgentSessionSyncStatusCompleted {
		t.Fatalf("state=%+v want completed", state)
	}
}
