package agentapi

import (
	"context"
	"encoding/json"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	toolruntime "github.com/askie/grix/backend/internal/agenttoolbar/runtime"
	pkgagentapi "github.com/askie/grix/backend/internal/pkg/agentapi"
	"github.com/askie/grix/backend/internal/pkg/testutil"
	"github.com/askie/grix/backend/internal/store"
	"github.com/askie/grix/backend/internal/ws/protocol"
)

func authorityTestConn(agentID, ownerID, epoch, logID int64, clientType string, actions ...string) *agentConn {
	return &agentConn{
		agentID:         agentID,
		ownerID:         ownerID,
		isPrimary:       true,
		clientType:      clientType,
		capabilities:    []string{"stream_chunk", "local_action_v1"},
		localActions:    append([]string(nil), actions...),
		connectedAt:     time.Now(),
		connectionEpoch: epoch,
		connLogID:       logID,
		send:            make(chan []byte, 16),
		done:            make(chan struct{}),
	}
}

func TestConnectionAuthorityCrossNodeFencesHeartbeatDeliveryAndCleanup(t *testing.T) {
	previousRDB := store.RDB
	previousDB := store.DB
	store.RDB = testutil.NewMockRedis()
	store.DB = nil

	oldManager := NewManager("", 30*time.Second, nil, nil, nil, nil)
	newManager := NewManager("", 30*time.Second, nil, nil, nil, nil)
	oldManager.SetNodeID("authority-node-old")
	newManager.SetNodeID("authority-node-new")
	defer func() {
		oldManager.Shutdown()
		newManager.Shutdown()
		_ = store.RDB.Close()
		store.RDB = previousRDB
		store.DB = previousDB
	}()

	const (
		agentID = int64(77101)
		ownerID = int64(88101)
	)
	oldConn := authorityTestConn(agentID, ownerID, 11, 101, "old-client", "old_action")
	if !oldManager.attachConn(oldConn) {
		t.Fatal("old connection should claim initial authority")
	}
	if !oldManager.refreshAgentLease(oldConn) {
		t.Fatal("old connection should refresh its initial lease")
	}

	newConn := authorityTestConn(agentID, ownerID, 12, 202, "new-client", "new_action")
	if !newManager.attachConn(newConn) {
		t.Fatal("new connection should replace cross-node authority")
	}
	if !newManager.refreshAgentLease(newConn) {
		t.Fatal("new connection should refresh its lease")
	}

	ctx := context.Background()
	assertNewGenerationResources := func() {
		t.Helper()
		if got := loadAgentRouteForOwner(ctx, agentID, ownerID); got != "authority-node-new" {
			t.Fatalf("owner route=%q want authority-node-new", got)
		}
		if got := loadAgentRoute(ctx, agentID); got != "authority-node-new" {
			t.Fatalf("primary route=%q want authority-node-new", got)
		}
		actions := loadAgentCapabilitiesForOwner(ctx, agentID, ownerID)
		if len(actions) != 1 || actions[0] != "new_action" {
			t.Fatalf("owner capabilities=%v want [new_action]", actions)
		}
		profile, ok, err := toolruntime.LoadProfile(ctx, agentID)
		if err != nil || !ok {
			t.Fatalf("load runtime profile ok=%t err=%v", ok, err)
		}
		if profile.ClientType != "new-client" || len(profile.LocalActions) != 1 || profile.LocalActions[0] != "new_action" {
			t.Fatalf("runtime profile=%+v want new generation", profile)
		}
		ownerProfile, ok, err := toolruntime.LoadProfileForOwner(ctx, agentID, ownerID)
		if err != nil || !ok {
			t.Fatalf("load owner runtime profile ok=%t err=%v", ok, err)
		}
		if ownerProfile.ClientType != "new-client" || len(ownerProfile.LocalActions) != 1 || ownerProfile.LocalActions[0] != "new_action" {
			t.Fatalf("owner runtime profile=%+v want new generation", ownerProfile)
		}
		rawInfo, err := store.RDB.Get(ctx, pkgagentapi.ConnInfoKey(agentID, ownerID)).Result()
		if err != nil {
			t.Fatalf("load connection info: %v", err)
		}
		if !strings.Contains(rawInfo, `"log_id":"202"`) {
			t.Fatalf("connection info=%s want new log_id", rawInfo)
		}
		authority, ok, err := loadAgentConnectionAuthority(ctx, agentID, ownerID)
		if err != nil || !ok {
			t.Fatalf("load authority ok=%t err=%v", ok, err)
		}
		if !authority.Active || authority.NodeID != "authority-node-new" || authority.ConnectionEpoch != 12 {
			t.Fatalf("authority=%+v want active new epoch", authority)
		}
	}
	assertNewGenerationResources()

	// Local delivery must consult node+epoch, not merely the presence of an
	// in-memory connection. The stale manager forwards to the current node and
	// never writes the packet into its old connector send queue.
	pubsub := store.RDB.Subscribe(ctx, "chan:authority-node-new")
	defer pubsub.Close()
	if !oldManager.DispatchOwnerCommandText(agentID, ownerID, "authority-session", "status") {
		t.Fatal("stale-node command should be forwarded to the authoritative node")
	}
	select {
	case message := <-pubsub.Channel():
		var envelope struct {
			Cmd string `json:"cmd"`
		}
		if err := json.Unmarshal([]byte(message.Payload), &envelope); err != nil {
			t.Fatalf("decode forwarded command: %v", err)
		}
		if envelope.Cmd != redisCmdForwardDelegateEvent {
			t.Fatalf("forwarded cmd=%q want=%q", envelope.Cmd, redisCmdForwardDelegateEvent)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("expected stale-node command to be forwarded")
	}
	select {
	case packet := <-oldConn.send:
		t.Fatalf("stale connector received outbound packet: %s", packet)
	default:
	}
	if !oldConn.closed.Load() {
		t.Fatal("authority rejection should close the stale connector")
	}

	// Its later heartbeat cannot take back any leased resource.
	if oldManager.refreshAgentLease(oldConn) {
		t.Fatal("stale heartbeat must be rejected")
	}
	assertNewGenerationResources()

	var disconnectCalls atomic.Int64
	oldManager.SetStreamDisconnectHandler(func(context.Context, int64, int64) {
		disconnectCalls.Add(1)
	})
	oldManager.unregister(oldConn)
	if disconnectCalls.Load() != 0 {
		t.Fatalf("stale unregister invoked stream cleanup %d time(s)", disconnectCalls.Load())
	}
	assertNewGenerationResources()
}

func TestAttachRejectsDelayedLowerEpochWithoutReplacingSuccessor(t *testing.T) {
	previousRDB := store.RDB
	previousDB := store.DB
	store.RDB = testutil.NewMockRedis()
	store.DB = nil

	manager := NewManager("", 30*time.Second, nil, nil, nil, nil)
	manager.SetNodeID("authority-same-node")
	defer func() {
		manager.Shutdown()
		_ = store.RDB.Close()
		store.RDB = previousRDB
		store.DB = previousDB
	}()

	const (
		agentID = int64(77102)
		ownerID = int64(88102)
	)
	successor := authorityTestConn(agentID, ownerID, 33, 303, "successor", "new_action")
	manager.register(successor)
	if successor.closed.Load() {
		t.Fatal("higher epoch should attach")
	}
	if !manager.refreshAgentLease(successor) {
		t.Fatal("higher epoch should own the lease")
	}

	// Simulate epoch 32 reserving first but completing authentication after
	// epoch 33 has already attached.
	delayed := authorityTestConn(agentID, ownerID, 32, 302, "delayed", "old_action")
	manager.register(delayed)
	if !delayed.closed.Load() {
		t.Fatal("delayed lower epoch handshake must be closed")
	}
	if successor.closed.Load() {
		t.Fatal("delayed lower epoch must not kick its successor")
	}
	if got := manager.lookupConnByOwner(agentID, ownerID); got != successor {
		t.Fatalf("local connection table points to %p want successor %p", got, successor)
	}
	authority, ok, err := loadAgentConnectionAuthority(context.Background(), agentID, ownerID)
	if err != nil || !ok {
		t.Fatalf("load authority ok=%t err=%v", ok, err)
	}
	if authority.ConnectionEpoch != 33 || authority.NodeID != "authority-same-node" || !authority.Active {
		t.Fatalf("authority=%+v want epoch 33", authority)
	}

	if !manager.DispatchOwnerCommandText(agentID, ownerID, "same-node-session", "status") {
		t.Fatal("current successor should accept local delegate command")
	}
	select {
	case raw := <-successor.send:
		var packet protocol.Packet
		if err := json.Unmarshal(raw, &packet); err != nil {
			t.Fatalf("decode successor packet: %v", err)
		}
		if packet.Cmd != "event_msg" {
			t.Fatalf("successor cmd=%q want event_msg", packet.Cmd)
		}
	case <-time.After(time.Second):
		t.Fatal("successor did not receive delegate command")
	}
	select {
	case raw := <-delayed.send:
		t.Fatalf("delayed connection received packet: %s", raw)
	default:
	}
}
