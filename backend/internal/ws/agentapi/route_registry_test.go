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

func TestPushDelegateEvent_ForwardsToRemoteNodeRoute(t *testing.T) {
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

	if err := store.RDB.Set(ctx, agentRouteKey(9992), "node-b", time.Minute).Err(); err != nil {
		t.Fatalf("seed agent route: %v", err)
	}
	pubsub := store.RDB.Subscribe(ctx, "chan:node-b")
	defer pubsub.Close()

	evt := DelegateEventPayload{
		EventID:   "evt-forward-1",
		EventType: "user_chat",
		AgentID:   9992,
		OwnerID:   1001,
		SessionID: "sess-forward",
		MsgID:     2001,
		SenderID:  3001,
		Content:   "hello",
	}
	if ok := mgr.PushDelegateEvent(evt); !ok {
		t.Fatal("expected delegate event to be forwarded to remote node")
	}

	select {
	case msg := <-pubsub.Channel():
		var envelope struct {
			Cmd     string          `json:"cmd"`
			Payload json.RawMessage `json:"payload"`
		}
		if err := json.Unmarshal([]byte(msg.Payload), &envelope); err != nil {
			t.Fatalf("unmarshal forwarded envelope: %v", err)
		}
		if envelope.Cmd != redisCmdForwardDelegateEvent {
			t.Fatalf("forwarded cmd=%s want=%s", envelope.Cmd, redisCmdForwardDelegateEvent)
		}

		var forwarded DelegateEventPayload
		if err := json.Unmarshal(envelope.Payload, &forwarded); err != nil {
			t.Fatalf("unmarshal forwarded payload: %v", err)
		}
		if forwarded.EventID != evt.EventID || forwarded.AgentID != evt.AgentID {
			t.Fatalf("forwarded event=%#v want=%#v", forwarded, evt)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("expected forwarded delegate event on remote node channel")
	}
}

func TestHandleRedisDispatch_DeliversForwardedEventLocally(t *testing.T) {
	mgr := NewManager("", 30*time.Second, nil, nil, nil, nil)
	defer mgr.Shutdown()
	mgr.eventAckWait = time.Second

	conn := &agentConn{
		agentID:  9992,
		ownerID:  1001,
		clientID: "agent-local",
		send:     make(chan []byte, 8),
	}
	mgr.putConnForTest(conn)

	var statuses []protocol.AgentDeliveryStatusPayload
	mgr.SetDeliveryStatusHandler(func(payload protocol.AgentDeliveryStatusPayload) {
		statuses = append(statuses, payload)
	})

	previous := GetGlobal()
	SetGlobal(mgr)
	defer SetGlobal(previous)

	raw, err := json.Marshal(DelegateEventPayload{
		EventID:   "evt-forward-2",
		EventType: "user_chat",
		AgentID:   9992,
		OwnerID:   1001,
		SessionID: "sess-local",
		MsgID:     2002,
		SenderID:  3002,
		Content:   "ping",
	})
	if err != nil {
		t.Fatalf("marshal forwarded payload: %v", err)
	}

	if handled := HandleRedisDispatch(redisCmdForwardDelegateEvent, raw); !handled {
		t.Fatal("expected internal redis dispatch to be handled")
	}

	select {
	case data := <-conn.send:
		var packet protocol.Packet
		if err := json.Unmarshal(data, &packet); err != nil {
			t.Fatalf("unmarshal local event packet: %v", err)
		}
		if packet.Cmd != "event_msg" {
			t.Fatalf("packet cmd=%s want=event_msg", packet.Cmd)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("expected forwarded delegate event to be delivered to local agent conn")
	}

	if len(statuses) != 1 || statuses[0].Status != protocol.AgentDeliveryStatusQueued {
		t.Fatalf("queued status=%#v", statuses)
	}
	mgr.resolvePendingEventAck("evt-forward-2", time.Now().UnixMilli())
}

func TestClearAgentRouteDoesNotDeleteDifferentNodeOwner(t *testing.T) {
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

	if err := store.RDB.Set(ctx, agentRouteKey(9992), "node-b", time.Minute).Err(); err != nil {
		t.Fatalf("seed agent route: %v", err)
	}

	mgr.clearAgentRoute(9992)

	got, err := store.RDB.Get(ctx, agentRouteKey(9992)).Result()
	if err != nil {
		t.Fatalf("get agent route after clear: %v", err)
	}
	if got != "node-b" {
		t.Fatalf("agent route=%s want=node-b", got)
	}
}
