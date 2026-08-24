package handler

import (
	"context"
	"testing"

	"github.com/askie/grix/backend/internal/agentsync"
	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/snowflake"
	"github.com/askie/grix/backend/internal/pkg/testutil"
	"github.com/askie/grix/backend/internal/store"
)

// 守卫:绑定复用前必须确认会话存活。已删除/他人所有的会话一旦被复用,
// 用户会被带进一个打不开的死会话,对应的 provider 会话也永远无法重新导入。
func TestBindSessionAliveRejectsDeletedOrForeignSession(t *testing.T) {
	testDB := testutil.NewTestDB()
	defer testDB.Close()
	store.DB = testDB.DB
	store.RDB = nil

	ownerID := int64(9101)
	if err := testDB.DB.Create(&model.Session{
		SessionID: "bind-alive", OwnerID: ownerID, SessionType: model.SessionTypeDirect,
	}).Error; err != nil {
		t.Fatalf("create session: %v", err)
	}
	if err := testDB.DB.Create(&model.Session{
		SessionID: "bind-deleted", OwnerID: ownerID, SessionType: model.SessionTypeDirect, IsDeleted: true,
	}).Error; err != nil {
		t.Fatalf("create deleted session: %v", err)
	}

	if !bindSessionAlive("bind-alive", ownerID) {
		t.Fatal("alive owned session should be reusable")
	}
	if bindSessionAlive("bind-deleted", ownerID) {
		t.Fatal("deleted session must not be reused")
	}
	if bindSessionAlive("bind-alive", ownerID+1) {
		t.Fatal("session owned by someone else must not be reused")
	}
	if bindSessionAlive("bind-missing", ownerID) {
		t.Fatal("missing session must not be reused")
	}
}

// 守卫:导入意图只在状态行缺失时播种。绑定重试若把已有状态重置回
// queued/cursor 0,重导区间会包含首次导入后新增的 live 轮次,产生重复消息。
func TestSeedImportIntentIfAbsentDoesNotResetExistingState(t *testing.T) {
	testDB := testutil.NewTestDB()
	defer testDB.Close()
	store.DB = testDB.DB
	store.RDB = nil
	if err := snowflake.Init(1); err != nil {
		t.Fatalf("init snowflake: %v", err)
	}

	ctx := context.Background()
	ident := agentsync.SyncIdentity{
		AgentID: 9201, OwnerID: 9202, SessionID: "seed-intent-session",
		ProviderKey: "codex", BindingID: "thread-1", SyncRunID: agentsync.NewSyncRunID(),
	}

	seedImportIntentIfAbsent(ctx, ident)
	state, found, err := agentsync.LoadState(ctx, ident)
	if err != nil || !found {
		t.Fatalf("state after first seed: found=%v err=%v", found, err)
	}
	if state.Status != model.AgentSessionSyncStatusQueued {
		t.Fatalf("first seed status=%s want=queued", state.Status)
	}

	// 模拟导入已完成后的一次绑定重试:状态必须保持 completed,不得回退。
	if err := agentsync.MarkCompleted(ctx, ident, "cursor-end", 5); err != nil {
		t.Fatalf("mark completed: %v", err)
	}
	seedImportIntentIfAbsent(ctx, ident)
	state, found, err = agentsync.LoadState(ctx, ident)
	if err != nil || !found {
		t.Fatalf("state after retry seed: found=%v err=%v", found, err)
	}
	if state.Status != model.AgentSessionSyncStatusCompleted || state.Cursor != "cursor-end" {
		t.Fatalf("retry seed must not reset state, got status=%s cursor=%s", state.Status, state.Cursor)
	}
}
