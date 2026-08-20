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

func TestTryForwardLocalAction_ReturnsFalseWhenNoRoute(t *testing.T) {
	previous := store.RDB
	store.RDB = testutil.NewMockRedis()
	defer func() {
		_ = store.RDB.Close()
		store.RDB = previous
	}()

	mgr := NewManager("", 30*time.Second, nil, nil, nil, nil)
	defer mgr.Shutdown()
	mgr.SetNodeID("node-a")

	action := protocol.LocalActionPayload{
		ActionID:   "act-no-route",
		ActionType: "file_list",
	}

	if mgr.tryForwardLocalActionForOwner(99999, 88001, action, nil) {
		t.Fatal("expected false when no route exists")
	}
}

func TestTryForwardLocalAction_ReturnsFalseWhenNoRedis(t *testing.T) {
	previous := store.RDB
	store.RDB = nil
	defer func() { store.RDB = previous }()

	mgr := NewManager("", 30*time.Second, nil, nil, nil, nil)
	defer mgr.Shutdown()
	mgr.SetNodeID("node-a")

	action := protocol.LocalActionPayload{
		ActionID:   "act-no-redis",
		ActionType: "file_list",
	}

	if mgr.tryForwardLocalActionForOwner(99999, 88001, action, nil) {
		t.Fatal("expected false when redis is nil")
	}
}

func TestTryForwardLocalAction_ReturnsFalseWhenAgentIsLocal(t *testing.T) {
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

	// Agent route points to the local node, so forwarding should be skipped.
	if err := store.RDB.Set(ctx, agentRouteKey(99001), "node-a", time.Minute).Err(); err != nil {
		t.Fatalf("seed agent route: %v", err)
	}

	action := protocol.LocalActionPayload{
		ActionID:   "act-local",
		ActionType: "file_list",
	}

	if mgr.tryForwardLocalActionForOwner(99001, 88001, action, nil) {
		t.Fatal("expected false when agent is on the local node")
	}
}

func TestTryForwardLocalAction_PublishesToTargetNode(t *testing.T) {
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
	SetGlobal(mgr)
	defer SetGlobal(nil)

	// Agent 99002 is on node-b.
	if err := store.RDB.Set(ctx, agentRouteKey(99002), "node-b", time.Minute).Err(); err != nil {
		t.Fatalf("seed agent route: %v", err)
	}

	pubsub := store.RDB.Subscribe(ctx, "chan:node-b")
	defer pubsub.Close()

	action := protocol.LocalActionPayload{
		ActionID:   "act-fwd-2",
		ActionType: "file_list",
	}

	if !mgr.tryForwardLocalActionForOwner(99002, 88002, action, nil) {
		t.Fatal("expected tryForwardLocalActionForOwner to return true")
	}

	select {
	case msg := <-pubsub.Channel():
		var envelope struct {
			Cmd     string                      `json:"cmd"`
			Payload forwardedLocalActionRequest `json:"payload"`
		}
		if err := json.Unmarshal([]byte(msg.Payload), &envelope); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if envelope.Cmd != redisCmdForwardLocalAction {
			t.Fatalf("cmd=%s want=%s", envelope.Cmd, redisCmdForwardLocalAction)
		}
		if envelope.Payload.AgentID != 99002 {
			t.Fatalf("agent_id=%d want=99002", envelope.Payload.AgentID)
		}
		if envelope.Payload.OwnerID != 88002 {
			t.Fatalf("owner_id=%d want=88002", envelope.Payload.OwnerID)
		}
		if envelope.Payload.Action.ActionID != "act-fwd-2" {
			t.Fatalf("action_id=%s want=act-fwd-2", envelope.Payload.Action.ActionID)
		}
		if envelope.Payload.ReplyTo != "node-a" {
			t.Fatalf("reply_to=%s want=node-a", envelope.Payload.ReplyTo)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for pubsub message")
	}
}

func TestHandleForwardedLocalActionDispatch_RecognizesCommands(t *testing.T) {
	previous := store.RDB
	store.RDB = testutil.NewMockRedis()
	defer func() {
		_ = store.RDB.Close()
		store.RDB = previous
	}()

	// Non-matching command should return false.
	if HandleForwardedLocalActionDispatch("unknown_cmd", nil) {
		t.Fatal("expected false for unknown command")
	}
}

func TestHandleForwardedLocalActionRequest_ResultMatchesOwnerScopedPending(t *testing.T) {
	previous := store.RDB
	store.RDB = testutil.NewMockRedis()
	defer func() {
		_ = store.RDB.Close()
		store.RDB = previous
	}()

	// Target node: the agent is connected locally.
	mgr := NewManager("", 30*time.Second, nil, nil, nil, nil)
	defer mgr.Shutdown()
	mgr.SetNodeID("node-b")

	conn := &agentConn{
		agentID:         99004,
		ownerID:         88004,
		isPrimary:       true,
		clientType:      "codex",
		capabilities:    []string{"local_action_v1"},
		localActions:    []string{"set_model"},
		connectedAt:     time.Now(),
		connectionEpoch: 1,
		connLogID:       1,
		send:            make(chan []byte, 16),
		done:            make(chan struct{}),
	}
	if !mgr.attachConn(conn) {
		t.Fatal("attach agent connection")
	}

	ctx := context.Background()
	pubsub := store.RDB.Subscribe(ctx, "chan:node-a")
	defer pubsub.Close()

	req := forwardedLocalActionRequest{
		CorrelationID: "corr-owner-match",
		ReplyTo:       "node-a",
		AgentID:       99004,
		OwnerID:       88004,
		Action: protocol.LocalActionPayload{
			ActionID:   "toolbar:fwd-owner-match",
			ActionType: "set_model",
		},
	}
	go mgr.handleForwardedLocalActionRequest(req)

	// The target node delivers the local_action to the locally connected agent.
	select {
	case raw := <-conn.send:
		var pkt protocol.Packet
		if err := json.Unmarshal(raw, &pkt); err != nil {
			t.Fatalf("decode local_action: %v", err)
		}
		if pkt.Cmd != protocol.CmdLocalAction {
			t.Fatalf("cmd=%s want=%s", pkt.Cmd, protocol.CmdLocalAction)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for local_action send")
	}

	// The agent answers; the result must match the owner-scoped pending
	// registered by the forward handler (regression: the forwarded pending
	// used to miss owner_id, so the ok result was silently dropped and the
	// origin node observed a 20s timeout instead).
	payload, err := json.Marshal(protocol.LocalActionResultPayload{
		ActionID: "toolbar:fwd-owner-match",
		Status:   "ok",
	})
	if err != nil {
		t.Fatalf("marshal result: %v", err)
	}
	mgr.handleLocalActionResult(conn, &protocol.Packet{Cmd: protocol.CmdLocalActionResult, Seq: 1, Payload: payload})

	select {
	case msg := <-pubsub.Channel():
		var envelope struct {
			Cmd     string                       `json:"cmd"`
			Payload forwardedLocalActionResponse `json:"payload"`
		}
		if err := json.Unmarshal([]byte(msg.Payload), &envelope); err != nil {
			t.Fatalf("unmarshal response: %v", err)
		}
		if envelope.Cmd != redisCmdForwardLocalActionResponse {
			t.Fatalf("cmd=%s want=%s", envelope.Cmd, redisCmdForwardLocalActionResponse)
		}
		if envelope.Payload.Payload.Status != "ok" {
			t.Fatalf("status=%s want=ok (forwarded pending must carry owner_id)", envelope.Payload.Payload.Status)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for forwarded response (forwarded pending likely missed owner_id)")
	}
}

func TestHandleForwardedLocalActionResponse_DeliversToPending(t *testing.T) {
	previous := store.RDB
	store.RDB = testutil.NewMockRedis()
	defer func() {
		_ = store.RDB.Close()
		store.RDB = previous
	}()

	mgr := NewManager("", 30*time.Second, nil, nil, nil, nil)
	defer mgr.Shutdown()
	mgr.SetNodeID("node-a")
	SetGlobal(mgr)
	defer SetGlobal(nil)

	// Store a pending on node-a with a forwardedResultCh.
	ch := make(chan protocol.LocalActionResultPayload, 1)
	pending := &pendingLocalAction{
		actionID:          "act-resp-test",
		kind:              "forwarded_local_action",
		agentID:           99003,
		actionType:        "file_list",
		forwardedResultCh: ch,
	}
	mgr.storePendingLocalAction(pending)

	// Simulate a response arriving from the target node.
	resp := forwardedLocalActionResponse{
		CorrelationID: "corr-resp-1",
		Payload: protocol.LocalActionResultPayload{
			ActionID: "act-resp-test",
			Status:   "ok",
			Result: map[string]any{
				"files":        []any{},
				"current_path": "/tmp",
			},
		},
	}
	payload, _ := json.Marshal(resp)

	handled := HandleRedisDispatch(redisCmdForwardLocalActionResponse, payload)
	if !handled {
		t.Fatal("expected HandleRedisDispatch to handle forwarded local_action response")
	}

	select {
	case result := <-ch:
		if result.Status != "ok" {
			t.Fatalf("expected status=ok, got %s", result.Status)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for forwarded result")
	}
}
