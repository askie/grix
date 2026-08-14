package agentapi

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/askie/grix/backend/internal/model"
	pkgagentapi "github.com/askie/grix/backend/internal/pkg/agentapi"
	"github.com/askie/grix/backend/internal/pkg/testutil"
	"github.com/askie/grix/backend/internal/store"
)

const (
	shareTestAgentID  = int64(91001)
	shareTestOwnerA   = int64(82001)
	shareTestUserB    = int64(82002)
	shareTestUserC    = int64(82003)
	shareTestPlainKey = "ak-share-test-key"
)

func setupShareAuthDB(t *testing.T) func() {
	t.Helper()
	testDB := testutil.NewTestDB()
	originalDB := store.DB
	store.DB = testDB.DB
	cleanup := func() {
		store.DB = originalDB
		testDB.Close()
	}
	agent := model.Agent{
		ID:           shareTestAgentID,
		AgentName:    "share-auth-agent",
		OwnerID:      shareTestOwnerA,
		ProviderType: model.AgentProviderAPI,
		Status:       model.AgentStatusActive,
		APIKeyHash:   pkgagentapi.HashAPIKey(shareTestPlainKey),
	}
	if err := store.DB.Create(&agent).Error; err != nil {
		cleanup()
		t.Fatalf("create agent: %v", err)
	}
	// 共享授权校验依赖 sharedTo 用户活跃，预置主人/B/C 三个 active 账户。
	users := []model.User{
		{ID: shareTestOwnerA, Username: "owner-a", Email: "owner-a@test.local", Status: model.UserStatusActive},
		{ID: shareTestUserB, Username: "user-b", Email: "user-b@test.local", Status: model.UserStatusActive},
		{ID: shareTestUserC, Username: "user-c", Email: "user-c@test.local", Status: model.UserStatusActive},
	}
	for i := range users {
		if err := store.DB.Create(&users[i]).Error; err != nil {
			cleanup()
			t.Fatalf("create user %d: %v", users[i].ID, err)
		}
	}
	return cleanup
}

func TestAuthenticateAgent_ShareIdentity(t *testing.T) {
	defer setupShareAuthDB(t)()
	m := NewManager("", time.Second, nil, nil, nil, nil)
	defer m.Shutdown()

	// 主连接：主人 api_key，无 sharedOwnerID → 身份=主人，isPrimary=true
	owner, primary, err := m.authenticateAgent(shareTestAgentID, shareTestPlainKey, 0)
	if err != nil || owner != shareTestOwnerA || !primary {
		t.Fatalf("primary auth: owner=%d primary=%v err=%v", owner, primary, err)
	}

	// 主人传自己的 id 当 sharedOwnerID 也算主连接
	owner, primary, err = m.authenticateAgent(shareTestAgentID, shareTestPlainKey, shareTestOwnerA)
	if err != nil || owner != shareTestOwnerA || !primary {
		t.Fatalf("self shared auth: owner=%d primary=%v err=%v", owner, primary, err)
	}

	// 无共享授权时，带 sharedOwnerID=B 应被拒
	if _, _, err := m.authenticateAgent(shareTestAgentID, shareTestPlainKey, shareTestUserB); err == nil {
		t.Fatal("share auth without grant must be rejected")
	}

	// 共享给 B 后：主人 api_key + sharedOwnerID=B → 身份=B，isPrimary=false
	if err := store.DB.Create(&model.AgentShare{
		ID: 1, AgentID: shareTestAgentID, OwnerID: shareTestOwnerA,
		SharedTo: shareTestUserB, Status: model.AgentShareStatusActive,
	}).Error; err != nil {
		t.Fatalf("create share: %v", err)
	}
	owner, primary, err = m.authenticateAgent(shareTestAgentID, shareTestPlainKey, shareTestUserB)
	if err != nil || owner != shareTestUserB || primary {
		t.Fatalf("shared auth: owner=%d primary=%v err=%v", owner, primary, err)
	}

	// 未被授权的 C 仍被拒
	if _, _, err := m.authenticateAgent(shareTestAgentID, shareTestPlainKey, shareTestUserC); err == nil {
		t.Fatal("unauthorized shared owner must be rejected")
	}

	// 错误 api_key 一律拒（即便有共享授权）
	if _, _, err := m.authenticateAgent(shareTestAgentID, "wrong-key", 0); err == nil {
		t.Fatal("wrong api key must be rejected")
	}
	if _, _, err := m.authenticateAgent(shareTestAgentID, "wrong-key", shareTestUserB); err == nil {
		t.Fatal("wrong api key must be rejected even with share grant")
	}
}

func TestConnRouting_MultiOwnerIsolation(t *testing.T) {
	m := NewManager("", time.Second, nil, nil, nil, nil)
	defer m.Shutdown()
	primary := &agentConn{agentID: shareTestAgentID, ownerID: shareTestOwnerA, isPrimary: true, send: make(chan []byte, 8)}
	sharedB := &agentConn{agentID: shareTestAgentID, ownerID: shareTestUserB, isPrimary: false, send: make(chan []byte, 8)}
	m.putConnForTest(primary)
	m.putConnForTest(sharedB)

	// 按 owner 精确路由
	if got := m.lookupConnByOwner(shareTestAgentID, shareTestOwnerA); got != primary {
		t.Fatal("lookupConnByOwner(A) should return primary conn")
	}
	if got := m.lookupConnByOwner(shareTestAgentID, shareTestUserB); got != sharedB {
		t.Fatal("lookupConnByOwner(B) should return shared conn")
	}
	if got := m.lookupConnByOwner(shareTestAgentID, shareTestUserC); got != nil {
		t.Fatal("lookupConnByOwner(C) should be nil")
	}

	// lookupConn 优先主连接
	if got := m.lookupConn(shareTestAgentID); got != primary {
		t.Fatal("lookupConn should prefer primary conn")
	}

	// 按事件 OwnerID 严格路由(不 fallback):
	// - B 的事件 → B 共享连接
	// - A 的事件 → 主连接
	// - OwnerID=0 → 非法,返回 nil(fail-closed,绝不回退主连接)
	if got := m.lookupConnForDelegate(DelegateEventPayload{AgentID: shareTestAgentID, OwnerID: shareTestUserB}); got != sharedB {
		t.Fatal("delegate for B should route to shared conn")
	}
	if got := m.lookupConnForDelegate(DelegateEventPayload{AgentID: shareTestAgentID, OwnerID: shareTestOwnerA}); got != primary {
		t.Fatal("delegate for A should route to primary conn")
	}
	if got := m.lookupConnForDelegate(DelegateEventPayload{AgentID: shareTestAgentID, OwnerID: 0}); got != nil {
		t.Fatal("delegate without owner MUST return nil (fail-closed, no fallback to primary)")
	}
	// 隔离安全守卫:OwnerID=unknown user(非主人非被共享者) 必须返回 nil,
	// 严禁 fallback 到主连接 —— 否则被共享者 B 的事件在 B 连接暂断时会被误投到主人 A,
	// 隔离破坏。事件应入离线队列(由 enqueueDelegateEvent 兜底),等对应连接重连后 drain。
	if got := m.lookupConnForDelegate(DelegateEventPayload{AgentID: shareTestAgentID, OwnerID: shareTestUserC}); got != nil {
		t.Fatal("delegate for unknown owner MUST return nil (no fallback to primary); 否则隔离会被打破")
	}
}

// 离线队列单键、drain 按 owner 过滤：主人连接 drain 只取走自己的事件，被共享者的留在队列。
func TestDelegateQueue_DrainOwnerFiltered(t *testing.T) {
	originalRDB := store.RDB
	store.RDB = testutil.NewMockRedis()
	t.Cleanup(func() { store.RDB = originalRDB })
	ctx := context.Background()

	enqueueDelegateEvent(ctx, DelegateEventPayload{
		AgentID: shareTestAgentID, OwnerID: shareTestOwnerA,
		EventID: "evt-a", EventType: "user_chat", SessionID: "s-a", Content: "a",
	})
	enqueueDelegateEvent(ctx, DelegateEventPayload{
		AgentID: shareTestAgentID, OwnerID: shareTestUserB,
		EventID: "evt-b", EventType: "user_chat", SessionID: "s-b", Content: "b",
	})

	// 单一队列键（离线即入队的既有契约不变），两条事件都在里面。
	key := queuedDelegateEventListKey(shareTestAgentID)
	if n, _ := store.RDB.LLen(ctx, key).Result(); n != 2 {
		t.Fatalf("queue should hold 2 events, got %d", n)
	}

	// 主人连接 drain：只投递主人自己的事件，被共享者 B 的留回队列。
	m := NewManager("", time.Second, nil, nil, nil, nil)
	defer m.Shutdown()
	connA := &agentConn{agentID: shareTestAgentID, ownerID: shareTestOwnerA, isPrimary: true, send: make(chan []byte, 8)}
	m.drainQueuedDelegateEvents(connA, 128)
	if n, _ := store.RDB.LLen(ctx, key).Result(); n != 1 {
		t.Fatalf("after A drain, only B's event should remain, got %d", n)
	}
	// 残留的必须是 B 的事件。
	raw, _ := store.RDB.LRange(ctx, key, 0, -1).Result()
	if len(raw) != 1 || !strings.Contains(raw[0], "evt-b") {
		t.Fatalf("remaining event must be B's, got %v", raw)
	}

	// 被共享者连接 drain：取走 B 的，队列清空。
	connB := &agentConn{agentID: shareTestAgentID, ownerID: shareTestUserB, send: make(chan []byte, 8)}
	m.drainQueuedDelegateEvents(connB, 128)
	if n, _ := store.RDB.LLen(ctx, key).Result(); n != 0 {
		t.Fatalf("after B drain, queue should be empty, got %d", n)
	}
}

// agent_queued_events（revoke/edit 离线表）按 (agent_id, owner_id) 过滤 drain：
// 主人连接只 drain owner=自己 + owner=0 的老记录；被共享者连接只 drain 自己 owner 的事件，
// 严禁拿到其他 owner 的撤回/编辑（共享场景下跨 owner 串数据的根因修复守卫）。
func TestQueuedAgentEvents_DrainOwnerFiltered(t *testing.T) {
	testDB := testutil.NewTestDB()
	originalDB := store.DB
	store.DB = testDB.DB
	t.Cleanup(func() {
		store.DB = originalDB
		testDB.Close()
	})

	mgr := NewManager("", 30*time.Second, nil, nil, nil, nil)
	defer mgr.Shutdown()
	// 入队：A 撤回一条、B 撤回一条；owner_id=0 兼容老路径事件一条。
	if ok := mgr.PushToAgent(shareTestAgentID, shareTestOwnerA, "event_revoke", map[string]any{
		"msg_id": "10001", "session_id": "s-a", "session_type": 1, "sender_id": "9001",
	}); !ok {
		t.Fatal("queue A revoke")
	}
	if ok := mgr.PushToAgent(shareTestAgentID, shareTestUserB, "event_revoke", map[string]any{
		"msg_id": "10002", "session_id": "s-b", "session_type": 1, "sender_id": "9002",
	}); !ok {
		t.Fatal("queue B revoke")
	}
	// owner_id=0 的老记录模拟升级前残留数据：PushToAgent 已对 owner=0 fail-closed，
	// 直接入队一条 legacy 记录，验证主连接 drain 仍能带走它。
	legacyEvt, ok := buildQueuedAgentEvent(shareTestAgentID, 0, "event_revoke", map[string]any{
		"msg_id": "10003", "session_id": "s-legacy", "session_type": 1, "sender_id": "9003",
	})
	if !ok {
		t.Fatal("build legacy revoke event")
	}
	if !enqueueQueuedAgentEvent(context.Background(), *legacyEvt) {
		t.Fatal("queue legacy revoke")
	}
	var total int64
	store.DB.Model(&model.AgentQueuedEvent{}).Where("agent_id = ?", shareTestAgentID).Count(&total)
	if total != 3 {
		t.Fatalf("expected 3 queued events, got %d", total)
	}

	// 被共享者 B 连接 drain：只能拿到自己的 1 条，A 和 owner=0 的都不动。
	connB := &agentConn{agentID: shareTestAgentID, ownerID: shareTestUserB, isPrimary: false, send: make(chan []byte, 8)}
	mgr.drainQueuedAgentEvents(connB, 128)
	select {
	case <-connB.send:
		// expected
	case <-time.After(time.Second):
		t.Fatal("B should receive its own revoke event on drain")
	}
	// B 的连接不该再多收别人的事件。
	select {
	case data := <-connB.send:
		t.Fatalf("B drained extra event from other owner: %s", string(data))
	case <-time.After(50 * time.Millisecond):
	}
	// 拿到对应的 record 标 ack 让它被删除，然后核对剩余只有 A 和 legacy 两条。
	for _, evt := range loadQueuedEvents(t, shareTestAgentID, shareTestUserB) {
		mgr.resolvePendingEventAck(evt.EventKey, time.Now().UnixMilli())
	}
	store.DB.Model(&model.AgentQueuedEvent{}).Where("agent_id = ?", shareTestAgentID).Count(&total)
	if total != 2 {
		t.Fatalf("after B drain ack, expected 2 left (A + legacy), got %d", total)
	}

	// 主连接（A）drain：拿到自己 + legacy 共 2 条。
	connA := &agentConn{agentID: shareTestAgentID, ownerID: shareTestOwnerA, isPrimary: true, send: make(chan []byte, 8)}
	mgr.drainQueuedAgentEvents(connA, 128)
	got := 0
	for got < 2 {
		select {
		case <-connA.send:
			got++
		case <-time.After(time.Second):
			t.Fatalf("A should drain 2 events (own + legacy), got %d", got)
		}
	}
}

func loadQueuedEvents(t *testing.T, agentID, ownerID int64) []model.AgentQueuedEvent {
	t.Helper()
	var evts []model.AgentQueuedEvent
	if err := store.DB.Where("agent_id = ? AND owner_id = ?", agentID, ownerID).Find(&evts).Error; err != nil {
		t.Fatalf("load queued events: %v", err)
	}
	return evts
}

// 连接表：同 agent 多 owner 连接并存；同一身份重连踢旧；撤销/断开一个不影响其它。
func TestConnTable_RegisterUnregisterMultiOwner(t *testing.T) {
	testDB := testutil.NewTestDB()
	originalDB := store.DB
	store.DB = testDB.DB
	originalRDB := store.RDB
	store.RDB = testutil.NewMockRedis()
	t.Cleanup(func() {
		store.DB = originalDB
		store.RDB = originalRDB
		testDB.Close()
	})
	m := NewManager("", time.Second, nil, nil, nil, nil)
	defer m.Shutdown()

	connA1 := &agentConn{agentID: shareTestAgentID, ownerID: shareTestOwnerA, isPrimary: true, send: make(chan []byte, 8)}
	connB := &agentConn{agentID: shareTestAgentID, ownerID: shareTestUserB, isPrimary: false, send: make(chan []byte, 8)}
	m.register(connA1)
	m.register(connB)

	// 主人与被共享者连接并存，互不踢线。
	if m.lookupConnByOwner(shareTestAgentID, shareTestOwnerA) != connA1 {
		t.Fatal("owner A conn should be registered")
	}
	if m.lookupConnByOwner(shareTestAgentID, shareTestUserB) != connB {
		t.Fatal("owner B conn should coexist")
	}
	if connA1.closed.Load() {
		t.Fatal("registering B must NOT kick A")
	}

	// 同一身份(A)重连：踢掉旧 A 连接，新 A 生效，B 不受影响。
	connA2 := &agentConn{agentID: shareTestAgentID, ownerID: shareTestOwnerA, isPrimary: true, send: make(chan []byte, 8)}
	m.register(connA2)
	if !connA1.closed.Load() {
		t.Fatal("same-owner reconnect must kick old A conn")
	}
	if m.lookupConnByOwner(shareTestAgentID, shareTestOwnerA) != connA2 {
		t.Fatal("new A conn should replace old")
	}
	if m.lookupConnByOwner(shareTestAgentID, shareTestUserB) != connB {
		t.Fatal("B conn must survive A's reconnect")
	}

	// 断开 B：A 仍在。
	m.unregister(connB)
	if m.lookupConnByOwner(shareTestAgentID, shareTestUserB) != nil {
		t.Fatal("B conn should be removed after unregister")
	}
	if m.lookupConnByOwner(shareTestAgentID, shareTestOwnerA) != connA2 {
		t.Fatal("A conn must survive B's unregister")
	}

	// 断开最后一个：该 agent 整体下线。
	m.unregister(connA2)
	if m.lookupConn(shareTestAgentID) != nil {
		t.Fatal("agent should be fully offline after last conn unregister")
	}
}

// enforceAuthorizedShareConns 后端兜底：撤销共享时立即踢失授权连接，
// 不靠 connector 收 control_share_set 后回踢。守这一条「即时性」契约。
func TestEnforceAuthorizedShareConns_KicksRevoked(t *testing.T) {
	defer setupShareAuthDB(t)()
	m := NewManager("", time.Second, nil, nil, nil, nil)
	defer m.Shutdown()

	primary := &agentConn{agentID: shareTestAgentID, ownerID: shareTestOwnerA, isPrimary: true, send: make(chan []byte, 8)}
	sharedB := &agentConn{agentID: shareTestAgentID, ownerID: shareTestUserB, isPrimary: false, send: make(chan []byte, 8)}
	sharedC := &agentConn{agentID: shareTestAgentID, ownerID: shareTestUserC, isPrimary: false, send: make(chan []byte, 8)}
	m.putConnForTest(primary)
	m.putConnForTest(sharedB)
	m.putConnForTest(sharedC)

	for i, sharedTo := range []int64{shareTestUserB, shareTestUserC} {
		if err := store.DB.Create(&model.AgentShare{
			ID:       int64(i + 1),
			AgentID:  shareTestAgentID,
			OwnerID:  shareTestOwnerA,
			SharedTo: sharedTo,
			Status:   model.AgentShareStatusActive,
		}).Error; err != nil {
			t.Fatalf("create share %d: %v", sharedTo, err)
		}
	}

	// 撤销 B 的共享行，C 仍有效
	if err := store.DB.Model(&model.AgentShare{}).
		Where("agent_id = ? AND shared_to = ?", shareTestAgentID, shareTestUserB).
		Update("status", model.AgentShareStatusRevoked).Error; err != nil {
		t.Fatalf("revoke B share: %v", err)
	}

	m.enforceAuthorizedShareConns(shareTestAgentID)

	if got := m.lookupConnByOwner(shareTestAgentID, shareTestUserB); got != nil {
		t.Fatal("B's conn must be unregistered after revoke + enforce")
	}
	if !sharedB.closed.Load() {
		t.Fatal("B's conn must be closed after enforce")
	}
	if got := m.lookupConnByOwner(shareTestAgentID, shareTestOwnerA); got != primary {
		t.Fatal("A primary conn must survive enforce")
	}
	if got := m.lookupConnByOwner(shareTestAgentID, shareTestUserC); got != sharedC {
		t.Fatal("C shared conn must survive enforce")
	}
	if primary.closed.Load() || sharedC.closed.Load() {
		t.Fatal("A and C conns must not be closed by enforce")
	}
}

// 被共享者封号/注销时 enforce 同样踢——hasActiveAgentShare/agentShareActive/
// enforceAuthorizedShareConns 三处口径必须一致：被共享者非 active 即视为共享失效。
func TestEnforceAuthorizedShareConns_KicksBannedSharedUser(t *testing.T) {
	defer setupShareAuthDB(t)()
	m := NewManager("", time.Second, nil, nil, nil, nil)
	defer m.Shutdown()

	primary := &agentConn{agentID: shareTestAgentID, ownerID: shareTestOwnerA, isPrimary: true, send: make(chan []byte, 8)}
	sharedB := &agentConn{agentID: shareTestAgentID, ownerID: shareTestUserB, isPrimary: false, send: make(chan []byte, 8)}
	m.putConnForTest(primary)
	m.putConnForTest(sharedB)

	if err := store.DB.Create(&model.AgentShare{
		ID: 1, AgentID: shareTestAgentID, OwnerID: shareTestOwnerA,
		SharedTo: shareTestUserB, Status: model.AgentShareStatusActive,
	}).Error; err != nil {
		t.Fatalf("create share: %v", err)
	}

	// B 被封号
	if err := store.DB.Model(&model.User{}).
		Where("id = ?", shareTestUserB).
		Update("status", model.UserStatusBanned).Error; err != nil {
		t.Fatalf("ban B: %v", err)
	}

	m.enforceAuthorizedShareConns(shareTestAgentID)

	if got := m.lookupConnByOwner(shareTestAgentID, shareTestUserB); got != nil {
		t.Fatal("banned B's conn must be kicked by enforce")
	}
	if !sharedB.closed.Load() {
		t.Fatal("banned B's conn must be closed by enforce")
	}
	if primary.closed.Load() {
		t.Fatal("A primary conn must not be affected by B's ban")
	}
}

// 配合上面 enforce 的封号守卫:握手时被共享者已封号,authenticateAgent 必须拒,
// 即使 agent_shares 行仍是 status=1。守 hasActiveAgentShare/agentShareActive 同口径。
func TestAuthenticateAgent_RejectsBannedSharedUser(t *testing.T) {
	defer setupShareAuthDB(t)()
	m := NewManager("", time.Second, nil, nil, nil, nil)
	defer m.Shutdown()

	if err := store.DB.Create(&model.AgentShare{
		ID: 1, AgentID: shareTestAgentID, OwnerID: shareTestOwnerA,
		SharedTo: shareTestUserB, Status: model.AgentShareStatusActive,
	}).Error; err != nil {
		t.Fatalf("create share: %v", err)
	}
	if err := store.DB.Model(&model.User{}).
		Where("id = ?", shareTestUserB).
		Update("status", model.UserStatusBanned).Error; err != nil {
		t.Fatalf("ban B: %v", err)
	}

	if _, _, err := m.authenticateAgent(shareTestAgentID, shareTestPlainKey, shareTestUserB); err == nil {
		t.Fatal("banned B must not be able to authenticate, even with active agent_shares row")
	}
}
