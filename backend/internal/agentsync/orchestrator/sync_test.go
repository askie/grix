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

	client.responses = []*wsagentapi.SessionHistorySyncResponse{{HasMore: false, NextCursor: "cursor-final"}}
	client.cursors = nil
	imported, err = SyncBoundSessionHistory(context.Background(), ownerID, sessionID)
	if err != nil {
		t.Fatalf("second sync: %v", err)
	}
	if imported != 0 {
		t.Fatalf("second imported=%d want=0", imported)
	}
	if len(client.cursors) != 1 || client.cursors[0] != "cursor-final" {
		t.Fatalf("second sync cursors=%v want=[cursor-final]", client.cursors)
	}

	var state model.AgentSessionSyncState
	if err := testDB.DB.Where("agent_id = ? AND session_id = ?", agentID, sessionID).First(&state).Error; err != nil {
		t.Fatalf("load sync state: %v", err)
	}
	if state.Status != model.AgentSessionSyncStatusCompleted || state.Cursor != "cursor-final" {
		t.Fatalf("sync state=%+v", state)
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
