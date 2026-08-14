package store

import (
	"context"
	"testing"
	"time"

	toolprotocol "github.com/askie/grix/backend/internal/agenttoolbar/protocol"
	"github.com/askie/grix/backend/internal/pkg/testutil"
	appstore "github.com/askie/grix/backend/internal/store"
)

func TestCacheStoreSaveSnapshotSameStateKeepsRevision(t *testing.T) {
	oldRDB := appstore.RDB
	appstore.RDB = testutil.NewMockRedis()
	defer func() {
		_ = appstore.RDB.Close()
		appstore.RDB = oldRDB
	}()

	cache := NewCacheStore()
	first, changed, err := cache.SaveSnapshot(context.Background(), 1001, toolprotocol.Snapshot{
		SessionID: "sess-1",
		AgentID:   9001,
		ToolbarID: "agent-toolbar:test:v1",
		Visible:   true,
		Items: []toolprotocol.Item{{
			ItemID:   "stop_output",
			Kind:     toolprotocol.ItemKindButton,
			ActionID: "stop_output",
			Label:    "Stop",
		}},
	})
	if err != nil {
		t.Fatalf("first SaveSnapshot() err = %v", err)
	}
	if !changed {
		t.Fatalf("first changed = false, want true")
	}
	second, changed, err := cache.SaveSnapshot(context.Background(), 1001, toolprotocol.Snapshot{
		SessionID: "sess-1",
		AgentID:   9001,
		ToolbarID: "agent-toolbar:test:v1",
		Visible:   true,
		Items: []toolprotocol.Item{{
			ItemID:   "stop_output",
			Kind:     toolprotocol.ItemKindButton,
			ActionID: "stop_output",
			Label:    "Stop",
		}},
	})
	if err != nil {
		t.Fatalf("second SaveSnapshot() err = %v", err)
	}
	if changed {
		t.Fatalf("second changed = true, want false")
	}
	if second.Revision != first.Revision {
		t.Fatalf("second revision = %d, want %d", second.Revision, first.Revision)
	}
}

func TestCacheStoreSaveSnapshotIndexesOnlyVisibleSnapshots(t *testing.T) {
	oldRDB := appstore.RDB
	appstore.RDB = testutil.NewMockRedis()
	defer func() {
		_ = appstore.RDB.Close()
		appstore.RDB = oldRDB
	}()

	cache := NewCacheStore()
	if _, _, err := cache.SaveSnapshot(context.Background(), 1001, toolprotocol.Snapshot{
		SessionID: "hidden-first",
		AgentID:   9001,
		ToolbarID: "agent-toolbar:none:v1",
		Visible:   false,
	}); err != nil {
		t.Fatalf("SaveSnapshot(hidden-first) err = %v", err)
	}
	sessions, err := cache.ListIndexedSessions(context.Background(), 1001, 9001)
	if err != nil {
		t.Fatalf("ListIndexedSessions() err = %v", err)
	}
	if len(sessions) != 0 {
		t.Fatalf("indexed sessions = %v, want none", sessions)
	}

	if _, _, err := cache.SaveSnapshot(context.Background(), 1001, toolprotocol.Snapshot{
		SessionID: "sess-1",
		AgentID:   9001,
		ToolbarID: "agent-toolbar:test:v1",
		Visible:   true,
	}); err != nil {
		t.Fatalf("SaveSnapshot(visible) err = %v", err)
	}
	sessions, err = cache.ListIndexedSessions(context.Background(), 1001, 9001)
	if err != nil {
		t.Fatalf("ListIndexedSessions() err = %v", err)
	}
	if len(sessions) != 1 || sessions[0] != "sess-1" {
		t.Fatalf("indexed sessions = %v, want [sess-1]", sessions)
	}

	if _, _, err := cache.SaveSnapshot(context.Background(), 1001, toolprotocol.Snapshot{
		SessionID: "sess-1",
		AgentID:   9001,
		ToolbarID: "agent-toolbar:none:v1",
		Visible:   false,
	}); err != nil {
		t.Fatalf("SaveSnapshot(hidden) err = %v", err)
	}
	sessions, err = cache.ListIndexedSessions(context.Background(), 1001, 9001)
	if err != nil {
		t.Fatalf("ListIndexedSessions() err = %v", err)
	}
	if len(sessions) != 0 {
		t.Fatalf("indexed sessions = %v, want none", sessions)
	}
}

func TestCacheStoreReserveContextWarmDedupesWithinTTL(t *testing.T) {
	oldRDB := appstore.RDB
	appstore.RDB = testutil.NewMockRedis()
	defer func() {
		_ = appstore.RDB.Close()
		appstore.RDB = oldRDB
	}()

	cache := NewCacheStore()
	reserved, err := cache.ReserveContextWarm(context.Background(), 1001, 9001, "sess-1", time.Minute)
	if err != nil {
		t.Fatalf("first ReserveContextWarm() err = %v", err)
	}
	if !reserved {
		t.Fatalf("first reserved = false, want true")
	}
	reserved, err = cache.ReserveContextWarm(context.Background(), 1001, 9001, "sess-1", time.Minute)
	if err != nil {
		t.Fatalf("second ReserveContextWarm() err = %v", err)
	}
	if reserved {
		t.Fatalf("second reserved = true, want false")
	}
}

func TestCacheStoreReserveRateLimitFetchDedupesWithinTTL(t *testing.T) {
	oldRDB := appstore.RDB
	appstore.RDB = testutil.NewMockRedis()
	defer func() {
		_ = appstore.RDB.Close()
		appstore.RDB = oldRDB
	}()

	cache := NewCacheStore()
	reserved, err := cache.ReserveRateLimitFetch(context.Background(), 1001, "claude:binding-1", time.Minute)
	if err != nil {
		t.Fatalf("first ReserveRateLimitFetch() err = %v", err)
	}
	if !reserved {
		t.Fatalf("first reserved = false, want true")
	}
	reserved, err = cache.ReserveRateLimitFetch(context.Background(), 1001, "claude:binding-1", time.Minute)
	if err != nil {
		t.Fatalf("second ReserveRateLimitFetch() err = %v", err)
	}
	if reserved {
		t.Fatalf("second reserved = true, want false")
	}
}

func TestCacheStoreReserveActionScopedByAgentID(t *testing.T) {
	oldRDB := appstore.RDB
	appstore.RDB = testutil.NewMockRedis()
	defer func() {
		_ = appstore.RDB.Close()
		appstore.RDB = oldRDB
	}()

	cache := NewCacheStore()
	const ownerID int64 = 1001
	const sessionID = "sess-1"
	const clientActionID = "action-1"

	ok, _, err := cache.ReserveAction(context.Background(), ownerID, sessionID, 9001, clientActionID)
	if err != nil {
		t.Fatalf("ReserveAction(agent=9001) err = %v", err)
	}
	if !ok {
		t.Fatalf("ReserveAction(agent=9001) = false, want true")
	}
	ok, _, err = cache.ReserveAction(context.Background(), ownerID, sessionID, 9002, clientActionID)
	if err != nil {
		t.Fatalf("ReserveAction(agent=9002) err = %v", err)
	}
	if !ok {
		t.Fatalf("ReserveAction(agent=9002) = false, want true")
	}
}

func TestCacheStoreLoadSnapshotScopedByAgentID(t *testing.T) {
	oldRDB := appstore.RDB
	appstore.RDB = testutil.NewMockRedis()
	defer func() {
		_ = appstore.RDB.Close()
		appstore.RDB = oldRDB
	}()

	cache := NewCacheStore()
	if _, _, err := cache.SaveSnapshot(context.Background(), 1001, toolprotocol.Snapshot{
		SessionID: "sess-1",
		AgentID:   9001,
		ToolbarID: "agent-toolbar:test:v1",
		Visible:   true,
	}); err != nil {
		t.Fatalf("SaveSnapshot(agent=9001) err = %v", err)
	}
	if _, _, err := cache.SaveSnapshot(context.Background(), 1001, toolprotocol.Snapshot{
		SessionID: "sess-1",
		AgentID:   9002,
		ToolbarID: "agent-toolbar:test:v1",
		Visible:   true,
	}); err != nil {
		t.Fatalf("SaveSnapshot(agent=9002) err = %v", err)
	}

	s1, ok, err := cache.LoadSnapshot(context.Background(), 1001, "sess-1", 9001)
	if err != nil || !ok {
		t.Fatalf("LoadSnapshot(agent=9001) err=%v ok=%v", err, ok)
	}
	if s1.AgentID != 9001 {
		t.Fatalf("LoadSnapshot(agent=9001).AgentID=%d want=9001", s1.AgentID)
	}

	s2, ok, err := cache.LoadSnapshot(context.Background(), 1001, "sess-1", 9002)
	if err != nil || !ok {
		t.Fatalf("LoadSnapshot(agent=9002) err=%v ok=%v", err, ok)
	}
	if s2.AgentID != 9002 {
		t.Fatalf("LoadSnapshot(agent=9002).AgentID=%d want=9002", s2.AgentID)
	}
}
