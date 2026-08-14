package agentapi

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/askie/grix/backend/internal/pkg/testutil"
	"github.com/askie/grix/backend/internal/store"
	"github.com/askie/grix/backend/internal/ws/protocol"
)

func TestLocalConnIsAuthoritative(t *testing.T) {
	previous := store.RDB
	store.RDB = testutil.NewMockRedis()
	defer func() {
		_ = store.RDB.Close()
		store.RDB = previous
	}()

	ctx := context.Background()
	mgr := NewManager("", 30*time.Second, nil, nil, nil, nil)
	defer mgr.Shutdown()
	mgr.SetNodeID("node-a")

	const ownerID int64 = 8801

	// No route stored — fall back to trusting the local conn.
	if !mgr.localConnIsAuthoritativeForOwner(7001, ownerID) {
		t.Fatal("empty route should be treated as authoritative (fallback)")
	}

	// Route points to this node — authoritative.
	if err := store.RDB.Set(ctx, agentRouteKey(7002), "node-a", time.Minute).Err(); err != nil {
		t.Fatalf("seed route: %v", err)
	}
	if !mgr.localConnIsAuthoritativeForOwner(7002, ownerID) {
		t.Fatal("route == self should be authoritative")
	}

	// Route points to another node — local conn is stale, not authoritative.
	if err := store.RDB.Set(ctx, agentRouteKey(7003), "node-b", time.Minute).Err(); err != nil {
		t.Fatalf("seed route: %v", err)
	}
	if mgr.localConnIsAuthoritativeForOwner(7003, ownerID) {
		t.Fatal("route == other node should NOT be authoritative")
	}

	// ownerID<=0 非法：一律非权威（fail-closed）。
	if mgr.localConnIsAuthoritativeForOwner(7001, 0) {
		t.Fatal("ownerID=0 must NOT be authoritative")
	}
}

// When the agent has reconnected to another node, the route points there while
// this node still holds a now-dead local conn. The action must be forwarded to
// the owning node instead of being written into the stale local conn.
func TestSendLocalActionWithPending_BypassesStaleLocalConn(t *testing.T) {
	previous := store.RDB
	store.RDB = testutil.NewMockRedis()
	defer func() {
		_ = store.RDB.Close()
		store.RDB = previous
	}()

	ctx := context.Background()
	mgr := NewManager("", 30*time.Second, nil, nil, nil, nil)
	defer mgr.Shutdown()
	mgr.SetNodeID("node-a")

	const agentID int64 = 7100
	const ownerID int64 = 87100

	// Stale local conn left over from before the agent moved to node-b.
	staleConn := &agentConn{
		agentID:      agentID,
		ownerID:      ownerID,
		send:         make(chan []byte, 4),
		capabilities: []string{"local_action_v1"},
		localActions: []string{"file_list"},
	}
	mgr.putConnForTest(staleConn)

	// Authoritative route says the agent is now on node-b.
	if err := store.RDB.Set(ctx, agentRouteKey(agentID), "node-b", time.Minute).Err(); err != nil {
		t.Fatalf("seed route: %v", err)
	}
	pubsub := store.RDB.Subscribe(ctx, "chan:node-b")
	defer pubsub.Close()

	action := protocol.LocalActionPayload{
		ActionID:   "file_list:7100:1",
		ActionType: "file_list",
		Params:     map[string]any{"session_id": "sess-1"},
	}
	if ok := mgr.sendLocalActionWithPendingForOwner(agentID, ownerID, action, nil); !ok {
		t.Fatal("expected action to be forwarded to the owning node")
	}

	// Must be forwarded over Redis to node-b, NOT written into the stale conn.
	select {
	case <-pubsub.Channel():
		// forwarded as expected
	case <-time.After(2 * time.Second):
		t.Fatal("expected forwarded local_action on chan:node-b")
	}

	select {
	case <-staleConn.send:
		t.Fatal("action must NOT be written into the stale local conn")
	default:
	}
}

// Sanity: when the route agrees the agent is local, the action goes to the
// local conn (no regression of the normal same-node path).
func TestSendLocalActionWithPending_UsesLocalConnWhenAuthoritative(t *testing.T) {
	previous := store.RDB
	store.RDB = testutil.NewMockRedis()
	defer func() {
		_ = store.RDB.Close()
		store.RDB = previous
	}()

	ctx := context.Background()
	mgr := NewManager("", 30*time.Second, nil, nil, nil, nil)
	defer mgr.Shutdown()
	mgr.SetNodeID("node-a")

	const agentID int64 = 7200
	const ownerID int64 = 87200

	conn := &agentConn{
		agentID:      agentID,
		ownerID:      ownerID,
		send:         make(chan []byte, 4),
		capabilities: []string{"local_action_v1"},
		localActions: []string{"file_list"},
	}
	mgr.putConnForTest(conn)

	if err := store.RDB.Set(ctx, agentRouteKey(agentID), "node-a", time.Minute).Err(); err != nil {
		t.Fatalf("seed route: %v", err)
	}

	action := protocol.LocalActionPayload{
		ActionID:   "file_list:7200:1",
		ActionType: "file_list",
		Params:     map[string]any{"session_id": "sess-2"},
	}
	if ok := mgr.sendLocalActionWithPendingForOwner(agentID, ownerID, action, nil); !ok {
		t.Fatal("expected action to be delivered to the local conn")
	}

	select {
	case data := <-conn.send:
		var packet protocol.Packet
		if err := json.Unmarshal(data, &packet); err != nil {
			t.Fatalf("unmarshal local packet: %v", err)
		}
		if packet.Cmd != protocol.CmdLocalAction {
			t.Fatalf("packet cmd=%s want=%s", packet.Cmd, protocol.CmdLocalAction)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("expected local_action delivered to local conn")
	}
}
