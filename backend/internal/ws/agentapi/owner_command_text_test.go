package agentapi

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	hermesadapter "github.com/askie/grix/backend/internal/agentadapter/hermes"
	"github.com/askie/grix/backend/internal/pkg/testutil"
	"github.com/askie/grix/backend/internal/store"
	"github.com/askie/grix/backend/internal/ws/protocol"
)

// TestDispatchOwnerCommandText_LocalDeliversStopWithoutTracking 验证：本地连接在线时，
// /stop 以 event_msg 投递给连接器，但不注册 active run、不登记 pending ack（命令式）。
func TestDispatchOwnerCommandText_LocalDeliversStopWithoutTracking(t *testing.T) {
	mgr := NewManager("", 30*time.Second, nil, nil, nil, nil)
	defer mgr.Shutdown()

	const agentID, ownerID = int64(9001), int64(1001)
	const sessionID = "sess-stop"
	conn := &agentConn{
		agentID:   agentID,
		ownerID:   ownerID,
		clientID:  "hermes-agent",
		send:      make(chan []byte, 8),
		adapter:   hermesadapter.NewAdapter(),
		adapterID: hermesadapter.AdapterID,
	}
	mgr.putConnForTest(conn)

	if ok := mgr.DispatchOwnerCommandText(agentID, ownerID, sessionID, "/stop"); !ok {
		t.Fatal("DispatchOwnerCommandText should succeed for online local conn")
	}

	select {
	case data := <-conn.send:
		var pkt protocol.Packet
		if err := json.Unmarshal(data, &pkt); err != nil {
			t.Fatalf("unmarshal packet: %v", err)
		}
		if pkt.Cmd != "event_msg" {
			t.Fatalf("cmd = %q, want event_msg", pkt.Cmd)
		}
		var payload DelegateEventPayload
		if err := json.Unmarshal(pkt.Payload, &payload); err != nil {
			t.Fatalf("unmarshal event payload: %v", err)
		}
		if payload.Content != "/stop" {
			t.Fatalf("payload content=%q want exact /stop", payload.Content)
		}
		var extra map[string]any
		if err := json.Unmarshal(payload.Extra, &extra); err != nil {
			t.Fatalf("unmarshal connector extra: %v raw=%s", err, payload.Extra)
		}
		connector, _ := extra["connector"].(map[string]any)
		if connector["response_delivery"] != "single_message" ||
			connector["tool_events"] != "drop" ||
			connector["thinking_events"] != "drop" {
			t.Fatalf("connector safe config missing: %v", connector)
		}
	default:
		t.Fatal("expected an event_msg to be sent to the connector")
	}

	// 不注册 active run。
	if run := mgr.LookupActiveRunBySessionOwner(ownerID, sessionID); run != nil {
		t.Fatalf("command event must not register an active run, got %+v", run)
	}
	// 不登记 pending ack。
	mgr.acksMu.Lock()
	pendingCount := len(mgr.pending)
	mgr.acksMu.Unlock()
	if pendingCount != 0 {
		t.Fatalf("command event must not register pending ack, got %d", pendingCount)
	}
}

// TestDispatchOwnerCommandText_OfflineReturnsFalse 验证：无本地连接且无路由（离线）时返回 false，
// 不入队（避免投递过期的停止命令）。
func TestDispatchOwnerCommandText_OfflineReturnsFalse(t *testing.T) {
	mgr := NewManager("", 30*time.Second, nil, nil, nil, nil)
	defer mgr.Shutdown()
	if ok := mgr.DispatchOwnerCommandText(9002, 1002, "sess-x", "/stop"); ok {
		t.Fatal("DispatchOwnerCommandText should return false when agent is offline")
	}
}

func TestCommandDelegateEventSkipsInterceptorsAndQueue(t *testing.T) {
	store.RDB = testutil.NewMockRedis()
	t.Cleanup(func() { store.RDB = nil })

	mgr := NewManager("", 30*time.Second, nil, nil, nil, nil)
	defer mgr.Shutdown()
	mgr.delegateEventInterceptors = []delegateEventInterceptor{
		{
			name: "test_visible_message_interceptor",
			handle: func(evt DelegateEventPayload) bool {
				return true
			},
		},
	}

	const agentID, ownerID = int64(9101), int64(1101)
	conn := &agentConn{
		agentID:  agentID,
		ownerID:  ownerID,
		clientID: "command-agent",
		send:     make(chan []byte, 1),
	}
	mgr.putConnForTest(conn)

	if ok := mgr.PushDelegateEvent(DelegateEventPayload{
		EventID:   "internal-command-online",
		EventType: "customer_coach_snapshot",
		AgentID:   agentID,
		OwnerID:   ownerID,
		SenderID:  ownerID,
		SessionID: "customer-session",
		MsgType:   1,
		Content:   "internal snapshot",
		Command:   true,
		CreatedAt: time.Now().UnixMilli(),
	}); !ok {
		t.Fatal("command event should dispatch to online connector")
	}

	select {
	case data := <-conn.send:
		var pkt protocol.Packet
		if err := json.Unmarshal(data, &pkt); err != nil {
			t.Fatalf("unmarshal packet: %v", err)
		}
		if pkt.Cmd != "event_msg" {
			t.Fatalf("cmd=%q want event_msg", pkt.Cmd)
		}
	default:
		t.Fatal("expected command event to reach connector")
	}

	if ok := enqueueDelegateEvent(context.Background(), DelegateEventPayload{
		EventID:   "internal-command-offline",
		EventType: "customer_coach_snapshot",
		AgentID:   agentID,
		OwnerID:   ownerID,
		SessionID: "customer-session",
		Command:   true,
	}); ok {
		t.Fatal("command event must not enter durable delegate queue")
	}
}

func TestDispatchCommandDelegateEventWithContextHonorsCanceledContext(t *testing.T) {
	store.RDB = testutil.NewMockRedis()
	t.Cleanup(func() { store.RDB = nil })

	previous := GetGlobalManager()
	mgr := NewManager("ctx-node", 30*time.Second, nil, nil, nil, nil)
	SetGlobal(mgr)
	t.Cleanup(func() {
		SetGlobal(previous)
		mgr.Shutdown()
	})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if ok := DispatchCommandDelegateEventWithContext(ctx, DelegateEventPayload{
		EventID:   "internal-command-canceled",
		EventType: "customer_coach_snapshot",
		AgentID:   9201,
		OwnerID:   1201,
		SessionID: "customer-session",
		Command:   true,
	}); ok {
		t.Fatal("canceled context must stop command delegate dispatch")
	}
}
