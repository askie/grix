package agentapi

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	geminiadapter "github.com/askie/grix/backend/internal/agentadapter/gemini"
	"github.com/askie/grix/backend/internal/gateway/provisioning"
	"github.com/askie/grix/backend/internal/grixactions"
	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/testutil"
	"github.com/askie/grix/backend/internal/store"
	"github.com/askie/grix/backend/internal/ws/protocol"
)

// 本文件的用例全部针对「agent 共享多连接串线事故」的回归防护：
// 同一 agent 的主连接（owner=A）与共享连接（owner=B）并存时，任何一个 owner 的
// 流量都绝不能落到另一个 owner 的连接上；owner 缺失（=0）一律 fail-closed。

// 事故核心场景：本节点只持有共享连接 connB，owner A 的 local_action 到达本节点时，
// 旧代码会经 primaryConnLocked「任一连接」回退把动作投给 connB（串线）。
// 修复后：不得写入 connB，必须按 (agent, A) 路由表转发到 A 所在节点。
func TestSendLocalActionForOwner_NeverFallsBackToOtherOwnersConn(t *testing.T) {
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

	const agentID int64 = 7300
	const ownerA int64 = 87301
	const ownerB int64 = 87302

	connB := &agentConn{
		agentID:      agentID,
		ownerID:      ownerB,
		isPrimary:    false,
		send:         make(chan []byte, 4),
		capabilities: []string{"local_action_v1"},
		localActions: []string{"file_list"},
	}
	mgr.putConnForTest(connB)

	// A 的连接在 node-b 上。
	if err := store.RDB.Set(ctx, agentRouteKeyForOwner(agentID, ownerA), "node-b", time.Minute).Err(); err != nil {
		t.Fatalf("seed owner route: %v", err)
	}
	pubsub := store.RDB.Subscribe(ctx, "chan:node-b")
	defer pubsub.Close()

	action := protocol.LocalActionPayload{
		ActionID:   "file_list:7300:1",
		ActionType: "file_list",
		Params:     map[string]any{"session_id": "sess-x"},
	}
	if ok := mgr.sendLocalActionWithPendingForOwner(agentID, ownerA, action, nil); !ok {
		t.Fatal("expected action to be forwarded to node-b (owner A's node)")
	}

	// 必须转发到 node-b，且转发请求携带 owner A。
	select {
	case msg := <-pubsub.Channel():
		var envelope struct {
			Cmd     string                      `json:"cmd"`
			Payload forwardedLocalActionRequest `json:"payload"`
		}
		if err := json.Unmarshal([]byte(msg.Payload), &envelope); err != nil {
			t.Fatalf("unmarshal forwarded payload: %v", err)
		}
		if envelope.Payload.OwnerID != ownerA {
			t.Fatalf("forwarded owner_id=%d want=%d", envelope.Payload.OwnerID, ownerA)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("expected forwarded local_action on chan:node-b")
	}

	// 绝不允许写入共享连接 connB。
	select {
	case <-connB.send:
		t.Fatal("action MUST NOT be written into another owner's conn (cross-owner leak)")
	default:
	}
}

// lookupConnForOwner / primaryConnLocked 的严格性：
// 只有别的 owner 的连接时，查 owner A 或 owner=0 都必须返回 nil。
func TestLookupConnStrictness_NoCrossOwnerFallback(t *testing.T) {
	mgr := NewManager("", 30*time.Second, nil, nil, nil, nil)
	defer mgr.Shutdown()

	const agentID int64 = 7301
	const ownerA int64 = 87311
	const ownerB int64 = 87312

	connB := &agentConn{
		agentID:   agentID,
		ownerID:   ownerB,
		isPrimary: false,
		send:      make(chan []byte, 4),
	}
	mgr.putConnForTest(connB)

	if got := mgr.lookupConnForOwner(agentID, ownerA); got != nil {
		t.Fatal("lookupConnForOwner(A) with only B's conn present MUST return nil")
	}
	if got := mgr.lookupConnForOwner(agentID, 0); got != nil {
		t.Fatal("lookupConnForOwner(0) MUST return nil (fail-closed)")
	}
	if got := mgr.lookupConnForOwner(agentID, ownerB); got != connB {
		t.Fatal("lookupConnForOwner(B) should return B's conn")
	}
	// 仅有共享连接时，主连接查询不得返回「任一」连接。
	if got := mgr.lookupConn(agentID); got != nil {
		t.Fatal("lookupConn with only a shared conn present MUST return nil (no any-conn fallback)")
	}
	if got := mgr.connByOwnerLocked(agentID, ownerA); got != nil {
		t.Fatal("connByOwnerLocked(A) with only B's conn present MUST return nil")
	}
}

// owner=0 拒绝：各路由入口 fail-closed。
func TestOwnerZeroRejected_FailClosed(t *testing.T) {
	mgr := NewManager("", 30*time.Second, nil, nil, nil, nil)
	defer mgr.Shutdown()

	const agentID int64 = 7302
	primary := &agentConn{
		agentID:      agentID,
		ownerID:      87321,
		isPrimary:    true,
		send:         make(chan []byte, 4),
		capabilities: []string{"local_action_v1"},
		localActions: []string{"file_list"},
	}
	mgr.putConnForTest(primary)

	if ok := mgr.SendLocalActionForOwner(agentID, 0, protocol.LocalActionPayload{
		ActionID: "a1", ActionType: "file_list",
	}); ok {
		t.Fatal("SendLocalActionForOwner(owner=0) MUST be rejected")
	}
	if ok := mgr.PushToAgent(agentID, 0, "event_revoke", map[string]any{"msg_id": "1"}); ok {
		t.Fatal("PushToAgent(owner=0) MUST be rejected")
	}
	if ok := mgr.PushDelegateEvent(DelegateEventPayload{
		EventID: "evt-owner-zero", EventType: "user_chat",
		AgentID: agentID, OwnerID: 0, SessionID: "s1", SenderID: 1,
	}); ok {
		t.Fatal("PushDelegateEvent(OwnerID=0) MUST be rejected")
	}
	if ok := mgr.dispatchAgentCommand(agentID, 0, protocol.CmdEventStop, map[string]any{"reason": "x"}); ok {
		t.Fatal("dispatchAgentCommand(owner=0) MUST be rejected")
	}

	// 被拒绝的流量不得落到任何连接（含主连接）。
	select {
	case <-primary.send:
		t.Fatal("rejected owner=0 traffic MUST NOT reach any conn")
	default:
	}
}

// gemini open-session submit 的 session_control 必须按事件发起者 owner 精确路由：
// 共享用户 B 提交工作区时，绑定动作与重放事件都只能落到 B 的连接，不落 A 的。
func TestGeminiOpenSessionSubmit_RoutesByEventOwner(t *testing.T) {
	testDB := testutil.NewTestDB()
	defer testDB.Close()
	originalDB := store.DB
	store.DB = testDB.DB
	defer func() { store.DB = originalDB }()

	originalRDB := store.RDB
	mockRedis := testutil.NewMockRedis()
	store.RDB = mockRedis
	defer func() {
		_ = mockRedis.Close()
		store.RDB = originalRDB
	}()

	handler := &mockSendMessageHandler{result: &SendMessageResult{MsgID: 9001, InboxSeq: 77, CreatedAt: 1704067205000}}
	mgr := NewManager("", 30*time.Second, handler.handle, nil, nil, nil)
	defer mgr.Shutdown()

	const agentID int64 = 7303
	const ownerA int64 = 87331
	const ownerB int64 = 87332

	newGeminiConn := func(ownerID int64, primary bool) *agentConn {
		return &agentConn{
			agentID:      agentID,
			ownerID:      ownerID,
			isPrimary:    primary,
			clientID:     "grix-gemini",
			clientType:   model.AgentClientTypeGemini,
			capabilities: []string{"local_action_v1"},
			localActions: []string{"session_control"},
			send:         make(chan []byte, 8),
			adapter:      geminiadapter.NewAdapter(),
			adapterID:    geminiadapter.AdapterID,
		}
	}
	connA := newGeminiConn(ownerA, true)
	connB := newGeminiConn(ownerB, false)
	mgr.putConnForTest(connA)
	mgr.putConnForTest(connB)

	// B 先发消息：事件只落到 B 的连接。
	evt := DelegateEventPayload{
		EventID: "evt-gemini-b-1", EventType: "user_chat",
		AgentID: agentID, OwnerID: ownerB,
		SessionID: "chat-gemini-b", ThreadID: "chat-gemini-b",
		MsgID: 18889999001, SenderID: ownerB, Content: "请分析这个项目",
	}
	if ok := mgr.PushDelegateEvent(evt); !ok {
		t.Fatal("PushDelegateEvent should dispatch B's event to B's conn")
	}
	if pkt := mustReadAgentPacket(t, connB.send); pkt.Cmd != "event_msg" {
		t.Fatalf("B packet cmd=%q want=event_msg", pkt.Cmd)
	}

	// B 的 connector 回报 session_binding_missing，服务端存起待重放事件。
	mgr.handleEventResult(connB, makePacket(t, protocol.CmdEventResult, 101, EventResultPayload{
		EventID: evt.EventID,
		Status:  protocol.AgentEventResultFailed,
		Code:    "session_binding_missing",
		Msg:     "session workspace binding is missing",
	}))
	if _, ok := loadGeminiPendingWorkspace(context.Background(), agentID, evt.SessionID); !ok {
		t.Fatal("expected pending Gemini workspace")
	}

	// B 提交工作区：session_control 与重放事件都必须只落到 B 的连接。
	submit := DelegateEventPayload{
		EventID: "evt-gemini-b-submit", EventType: "user_chat",
		AgentID: agentID, OwnerID: ownerB,
		SessionID: evt.SessionID, ThreadID: evt.ThreadID,
		MsgID: 18889999002, SenderID: ownerB,
		Content: grixactions.BuildOpenSessionSubmitURI(grixactions.OpenSessionSubmit{Cwd: "/workspace/b-demo"}),
	}
	if ok := mgr.PushDelegateEvent(submit); !ok {
		t.Fatal("B's workspace submit should be handled")
	}
	if pkt := mustReadAgentPacket(t, connB.send); pkt.Cmd != "local_action" {
		t.Fatalf("B packet cmd=%q want=local_action (session_control)", pkt.Cmd)
	}
	if pkt := mustReadAgentPacket(t, connB.send); pkt.Cmd != "event_msg" {
		t.Fatalf("B replay packet cmd=%q want=event_msg", pkt.Cmd)
	}

	// 全程 A 的连接不得收到任何东西。
	select {
	case data := <-connA.send:
		t.Fatalf("B's traffic MUST NOT reach A's conn, got=%s", string(data))
	default:
	}
}

// configure_gateway_provider 的 payload 含 API key，只能投到主实例连接，
// 绝不允许落到共享连接。
func TestConfigureGatewayProvider_OnlyReachesPrimaryConn(t *testing.T) {
	testDB := testutil.NewTestDB()
	defer testDB.Close()
	originalDB := store.DB
	store.DB = testDB.DB
	defer func() { store.DB = originalDB }()

	const agentID int64 = 7304
	const ownerA int64 = 87341
	const ownerB int64 = 87342

	if err := store.DB.Create(&model.Agent{
		ID:        agentID,
		OwnerID:   ownerA,
		AgentName: "gw-test",
		Status:    1,
	}).Error; err != nil {
		t.Fatalf("seed agent: %v", err)
	}

	mgr := NewManager("", 30*time.Second, nil, nil, nil, nil)
	defer mgr.Shutdown()
	SetGlobal(mgr)
	defer SetGlobal(nil)

	newConn := func(ownerID int64, primary bool) *agentConn {
		return &agentConn{
			agentID:      agentID,
			ownerID:      ownerID,
			isPrimary:    primary,
			capabilities: []string{"local_action_v1"},
			localActions: []string{"configure_gateway_provider"},
			send:         make(chan []byte, 4),
		}
	}
	connA := newConn(ownerA, true)
	connB := newConn(ownerB, false)
	mgr.putConnForTest(connA)
	mgr.putConnForTest(connB)

	handleBroadcastConfigureGatewayProvider(provisioning.GatewayProviderConfig{
		AgentID: agentID,
		APIKey:  "secret-api-key",
		Model:   "test-model",
	})

	// 主连接收到配置；共享连接绝收不到（否则密钥泄漏给被共享者）。
	select {
	case raw := <-connA.send:
		var pkt protocol.Packet
		if err := json.Unmarshal(raw, &pkt); err != nil {
			t.Fatalf("unmarshal packet: %v", err)
		}
		if pkt.Cmd != protocol.CmdLocalAction {
			t.Fatalf("packet cmd=%q want=%q", pkt.Cmd, protocol.CmdLocalAction)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("expected configure_gateway_provider on primary conn")
	}
	select {
	case data := <-connB.send:
		t.Fatalf("gateway provider config (with API key) MUST NOT reach shared conn, got=%s", string(data))
	default:
	}
}

// 遗留 owner=0 的离线 delegate 事件只允许主连接 drain：
// 共享连接 drain 时既不得取走 owner=0 遗留事件，也不得取走其他 owner 的事件——
// 否则主人的事件会泄漏到被共享者的 connector（review 发现的同类漏洞）。
func TestDelegateQueueDrain_LegacyOwnerZeroOnlyPrimary(t *testing.T) {
	originalRDB := store.RDB
	store.RDB = testutil.NewMockRedis()
	t.Cleanup(func() { store.RDB = originalRDB })

	ctx := context.Background()
	const agentID int64 = 7305
	const ownerA int64 = 87351
	const ownerB int64 = 87352

	enqueueDelegateEvent(ctx, DelegateEventPayload{
		AgentID: agentID, OwnerID: 0,
		EventID: "evt-legacy-0", EventType: "user_chat", SessionID: "s-legacy", Content: "legacy",
	})
	enqueueDelegateEvent(ctx, DelegateEventPayload{
		AgentID: agentID, OwnerID: ownerA,
		EventID: "evt-a-1", EventType: "user_chat", SessionID: "s-a", Content: "a",
	})

	mgr := NewManager("", time.Second, nil, nil, nil, nil)
	defer mgr.Shutdown()
	key := queuedDelegateEventListKey(agentID)

	// 共享连接 B drain：owner=0 遗留事件与 A 的事件都必须留在队列，B 什么都收不到。
	connB := &agentConn{agentID: agentID, ownerID: ownerB, isPrimary: false, send: make(chan []byte, 8)}
	mgr.drainQueuedDelegateEvents(connB, 128)
	if n, _ := store.RDB.LLen(ctx, key).Result(); n != 2 {
		t.Fatalf("shared conn drain must not take legacy owner=0 event nor A's event, remaining=%d", n)
	}
	select {
	case data := <-connB.send:
		t.Fatalf("legacy owner=0 / A's event MUST NOT leak to shared conn, got=%s", string(data))
	default:
	}

	// 主连接 drain：owner=0 遗留事件与自己的事件都补发给 A。
	connA := &agentConn{agentID: agentID, ownerID: ownerA, isPrimary: true, send: make(chan []byte, 8)}
	mgr.drainQueuedDelegateEvents(connA, 128)
	if n, _ := store.RDB.LLen(ctx, key).Result(); n != 0 {
		t.Fatalf("primary drain should take both legacy and own events, remaining=%d", n)
	}
	for i := 0; i < 2; i++ {
		select {
		case <-connA.send:
		case <-time.After(2 * time.Second):
			t.Fatal("expected drained event packet on primary conn")
		}
	}
}
