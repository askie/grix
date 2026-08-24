package agentapi

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/askie/grix/backend/internal/pkg/testutil"
	"github.com/askie/grix/backend/internal/store"
)

// startFakeWsNode subscribes to the node channel like a ws node's Redis
// subscriber and feeds every envelope into HandleRedisDispatch.
func startFakeWsNode(t *testing.T, node string) func() {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	sub := store.RDB.Subscribe(ctx, "chan:"+node)
	if _, err := sub.Receive(ctx); err != nil {
		t.Fatalf("subscribe node channel: %v", err)
	}
	go func() {
		for msg := range sub.Channel() {
			var envelope struct {
				Cmd     string          `json:"cmd"`
				Payload json.RawMessage `json:"payload"`
			}
			if err := json.Unmarshal([]byte(msg.Payload), &envelope); err != nil {
				continue
			}
			HandleRedisDispatch(envelope.Cmd, envelope.Payload)
		}
	}()
	return func() {
		cancel()
		_ = sub.Close()
	}
}

func withRemoteSyncFixture(t *testing.T) (ownerID, agentID int64, sessionID string) {
	t.Helper()
	previousRDB := store.RDB
	store.RDB = testutil.NewMockRedis()
	previousTimeout := remoteSessionHistorySyncTimeout
	previousManager := GetGlobalManager()
	previousHandler := getSessionHistorySyncHandler()
	t.Cleanup(func() {
		SetSessionHistorySyncHandler(previousHandler)
		SetGlobal(previousManager)
		remoteSessionHistorySyncTimeout = previousTimeout
		_ = store.RDB.Close()
		store.RDB = previousRDB
	})
	remoteSessionHistorySyncTimeout = 3 * time.Second
	return 7301, 8301, "remote-history-session"
}

func TestRemoteSyncBoundSessionHistory_RunsOnRoutedNodeAndReturnsImported(t *testing.T) {
	ownerID, agentID, sessionID := withRemoteSyncFixture(t)
	const node = "ws-node-a"
	if err := store.RDB.Set(context.Background(), agentRouteKeyForOwner(agentID, ownerID), node, 0).Err(); err != nil {
		t.Fatalf("seed route: %v", err)
	}
	stop := startFakeWsNode(t, node)
	defer stop()

	mgr := NewManager("", 30*time.Second, nil, nil, nil, nil)
	SetGlobal(mgr)
	var gotOwner int64
	var gotSession string
	SetSessionHistorySyncHandler(func(_ context.Context, owner int64, session string) (int, error) {
		gotOwner, gotSession = owner, session
		return 5, nil
	})

	imported, err := RemoteSyncBoundSessionHistory(context.Background(), ownerID, sessionID, []int64{agentID})
	if err != nil {
		t.Fatalf("remote sync: %v", err)
	}
	if imported != 5 {
		t.Fatalf("imported = %d, want 5", imported)
	}
	if gotOwner != ownerID || gotSession != sessionID {
		t.Fatalf("handler got owner=%d session=%q", gotOwner, gotSession)
	}
}

func TestRemoteSyncBoundSessionHistory_PropagatesRemoteError(t *testing.T) {
	ownerID, agentID, sessionID := withRemoteSyncFixture(t)
	const node = "ws-node-b"
	// Owner route missing, primary route present: fallback must still pick the node.
	if err := store.RDB.Set(context.Background(), agentRouteKey(agentID), node, 0).Err(); err != nil {
		t.Fatalf("seed route: %v", err)
	}
	stop := startFakeWsNode(t, node)
	defer stop()

	SetGlobal(NewManager("", 30*time.Second, nil, nil, nil, nil))
	SetSessionHistorySyncHandler(func(context.Context, int64, string) (int, error) {
		return 2, errors.New("connector rejected cursor")
	})

	imported, err := RemoteSyncBoundSessionHistory(context.Background(), ownerID, sessionID, []int64{agentID})
	if err == nil || err.Error() != "connector rejected cursor" {
		t.Fatalf("err = %v, want remote error", err)
	}
	if imported != 2 {
		t.Fatalf("imported = %d, want 2 (partial import before failure)", imported)
	}
}

func TestRemoteSyncBoundSessionHistory_OfflineWhenNoRoute(t *testing.T) {
	ownerID, agentID, sessionID := withRemoteSyncFixture(t)
	if _, err := RemoteSyncBoundSessionHistory(context.Background(), ownerID, sessionID, []int64{agentID}); !errors.Is(err, ErrSessionHistorySyncAgentOffline) {
		t.Fatalf("err = %v, want ErrSessionHistorySyncAgentOffline", err)
	}
}

func TestRemoteSyncBoundSessionHistory_OfflineWhenNoRedis(t *testing.T) {
	previous := store.RDB
	store.RDB = nil
	defer func() { store.RDB = previous }()
	if _, err := RemoteSyncBoundSessionHistory(context.Background(), 1, "s", []int64{2}); !errors.Is(err, ErrSessionHistorySyncAgentOffline) {
		t.Fatalf("err = %v, want ErrSessionHistorySyncAgentOffline", err)
	}
}

func TestRemoteSyncBoundSessionHistory_NodeWithoutManagerAnswersOffline(t *testing.T) {
	ownerID, agentID, sessionID := withRemoteSyncFixture(t)
	const node = "ws-node-c"
	if err := store.RDB.Set(context.Background(), agentRouteKeyForOwner(agentID, ownerID), node, 0).Err(); err != nil {
		t.Fatalf("seed route: %v", err)
	}
	stop := startFakeWsNode(t, node)
	defer stop()
	SetGlobal(nil)

	_, err := RemoteSyncBoundSessionHistory(context.Background(), ownerID, sessionID, []int64{agentID})
	if err == nil || err.Error() != ErrSessionHistorySyncAgentOffline.Error() {
		t.Fatalf("err = %v, want offline reply from node without manager", err)
	}
}

func TestRemoteSyncBoundSessionHistory_NodeWithoutHandlerAnswersUnavailable(t *testing.T) {
	ownerID, agentID, sessionID := withRemoteSyncFixture(t)
	const node = "ws-node-d"
	if err := store.RDB.Set(context.Background(), agentRouteKeyForOwner(agentID, ownerID), node, 0).Err(); err != nil {
		t.Fatalf("seed route: %v", err)
	}
	stop := startFakeWsNode(t, node)
	defer stop()
	SetGlobal(NewManager("", 30*time.Second, nil, nil, nil, nil))
	SetSessionHistorySyncHandler(nil)

	_, err := RemoteSyncBoundSessionHistory(context.Background(), ownerID, sessionID, []int64{agentID})
	if err == nil || err.Error() != ErrSessionHistorySyncHandlerUnavailable.Error() {
		t.Fatalf("err = %v, want handler unavailable", err)
	}
}

func TestRemoteSyncBoundSessionHistory_TimesOutWhenNodeSilent(t *testing.T) {
	ownerID, agentID, sessionID := withRemoteSyncFixture(t)
	remoteSessionHistorySyncTimeout = 200 * time.Millisecond
	// Route points at a node nobody listens on.
	if err := store.RDB.Set(context.Background(), agentRouteKeyForOwner(agentID, ownerID), "ws-node-dead", 0).Err(); err != nil {
		t.Fatalf("seed route: %v", err)
	}
	_, err := RemoteSyncBoundSessionHistory(context.Background(), ownerID, sessionID, []int64{agentID})
	if !errors.Is(err, ErrSessionHistorySyncTimeout) {
		t.Fatalf("err = %v, want ErrSessionHistorySyncTimeout", err)
	}
}

func TestHandleSessionHistorySyncDispatch_IgnoresOtherCommands(t *testing.T) {
	if HandleSessionHistorySyncDispatch("kick_agent", json.RawMessage(`{}`)) {
		t.Fatal("unrelated command must not be claimed")
	}
}
