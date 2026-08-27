package agentapi

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/snowflake"
	"github.com/askie/grix/backend/internal/pkg/testutil"
	"github.com/askie/grix/backend/internal/store"
	"github.com/askie/grix/backend/internal/ws/protocol"
)

func setupConnectorRollbackBroadcastTest(t *testing.T) {
	t.Helper()
	testDB := testutil.NewTestDB()
	t.Cleanup(testDB.Close)
	originalDB, originalRDB := store.DB, store.RDB
	store.DB = testDB.DB
	store.RDB = testutil.NewMockRedis()
	t.Cleanup(func() { store.DB, store.RDB = originalDB, originalRDB })
	if err := snowflake.Init(1); err != nil {
		t.Fatalf("init snowflake: %v", err)
	}
}

func newConnectorRollbackConn(agentID, ownerID int64, localActions []string) *agentConn {
	return &agentConn{
		agentID:      agentID,
		ownerID:      ownerID,
		capabilities: []string{"local_action_v1"},
		localActions: localActions,
		send:         make(chan []byte, 8),
		done:         make(chan struct{}),
	}
}

func useGlobalManagerForTest(t *testing.T, mgr *Manager) {
	t.Helper()
	globalMu.Lock()
	prev := globalManager
	globalManager = mgr
	globalMu.Unlock()
	t.Cleanup(func() {
		globalMu.Lock()
		globalManager = prev
		globalMu.Unlock()
	})
}

// 下发契约：只打到名单内、且声明了 connector_rollback 的连接；载荷必须带
// target_version，否则客户端会直接以 MISSING_TARGET_VERSION 回绝。
// 送达的 agent 要写进 dispatch 集合，admin 侧靠它判断"谁真收到了"。
func TestHandleBroadcastConnectorRollbackPush_SendsToDeclaredTargetsOnly(t *testing.T) {
	setupConnectorRollbackBroadcastTest(t)
	for _, a := range []model.Agent{
		{ID: 9101, AgentName: "target", OwnerID: 9100, AgentClientType: model.AgentClientTypeClaude},
		{ID: 9102, AgentName: "not-in-list", OwnerID: 9100, AgentClientType: model.AgentClientTypeClaude},
		{ID: 9103, AgentName: "no-declare", OwnerID: 9100, AgentClientType: model.AgentClientTypeClaude},
	} {
		if err := store.DB.Create(&a).Error; err != nil {
			t.Fatalf("create agent %d: %v", a.ID, err)
		}
	}

	mgr := NewManager("", 30*time.Second, nil, nil, nil, nil)
	defer mgr.Shutdown()
	target := newConnectorRollbackConn(9101, 9100, []string{"connector_rollback"})
	outOfList := newConnectorRollbackConn(9102, 9100, []string{"connector_rollback"})
	// 老客户端不声明 connector_rollback，必须被 SendLocalActionForOwner 挡下。
	noDeclare := newConnectorRollbackConn(9103, 9100, []string{"exec_approve"})
	mgr.putConnForTest(target)
	mgr.putConnForTest(outOfList)
	mgr.putConnForTest(noDeclare)
	useGlobalManagerForTest(t, mgr)

	handleBroadcastConnectorRollbackPush(broadcastConnectorRollbackPushPayload{
		PushID:        "push-1",
		TargetVersion: "4.3.5",
		AgentIDs:      []int64{9101, 9103},
	})

	select {
	case raw := <-target.send:
		var pkt protocol.Packet
		if err := json.Unmarshal(raw, &pkt); err != nil {
			t.Fatalf("unmarshal packet: %v", err)
		}
		if pkt.Cmd != protocol.CmdLocalAction {
			t.Fatalf("expected cmd=%s, got %s", protocol.CmdLocalAction, pkt.Cmd)
		}
		var action protocol.LocalActionPayload
		if err := json.Unmarshal(pkt.Payload, &action); err != nil {
			t.Fatalf("unmarshal local_action: %v", err)
		}
		if action.ActionType != "connector_rollback" {
			t.Fatalf("expected action_type=connector_rollback, got %s", action.ActionType)
		}
		if got := action.Params["target_version"]; got != "4.3.5" {
			t.Fatalf("expected target_version=4.3.5, got %v", got)
		}
	default:
		t.Fatal("名单内且声明了能力的连接必须收到 connector_rollback")
	}

	select {
	case <-outOfList.send:
		t.Fatal("名单外的 agent 不应收到下发")
	default:
	}
	select {
	case <-noDeclare.send:
		t.Fatal("未声明 connector_rollback 的老客户端不应收到下发")
	default:
	}

	dispatched, err := ConnectorRollbackDispatched(context.Background(), "push-1")
	if err != nil {
		t.Fatalf("read dispatched: %v", err)
	}
	if len(dispatched) != 1 || dispatched[0] != 9101 {
		t.Fatalf("送达集合应只含 9101，实际 %v", dispatched)
	}

	// 冷却必须在派发侧就地打上，不能依赖 admin 侧回收回执：SAdd 失败、回执晚于
	// 轮询窗口、admin 客户端断连都会让回执路径漏打，进而重复推同一台机器。
	cooling := ConnectorRollbackInCooldown(context.Background(), []int64{9101, 9103})
	if !cooling[9101] {
		t.Fatal("发出去的 agent 必须立刻进入冷却")
	}
	if cooling[9103] {
		t.Fatal("被能力位挡下、没真发出去的 agent 不应占冷却")
	}
}

// 空版本号绝不能下发：客户端会写 pending 再失败，白白扰动一台本来还在服务的机器。
func TestHandleBroadcastConnectorRollbackPush_IgnoresEmptyTargetVersion(t *testing.T) {
	setupConnectorRollbackBroadcastTest(t)
	if err := store.DB.Create(&model.Agent{
		ID: 9201, AgentName: "target", OwnerID: 9200, AgentClientType: model.AgentClientTypeClaude,
	}).Error; err != nil {
		t.Fatalf("create agent: %v", err)
	}
	mgr := NewManager("", 30*time.Second, nil, nil, nil, nil)
	defer mgr.Shutdown()
	conn := newConnectorRollbackConn(9201, 9200, []string{"connector_rollback"})
	mgr.putConnForTest(conn)
	useGlobalManagerForTest(t, mgr)

	handleBroadcastConnectorRollbackPush(broadcastConnectorRollbackPushPayload{
		PushID: "push-2", TargetVersion: "", AgentIDs: []int64{9201},
	})

	select {
	case <-conn.send:
		t.Fatal("空 target_version 不应下发任何东西")
	default:
	}
	if ConnectorRollbackInCooldown(context.Background(), []int64{9201})[9201] {
		t.Fatal("什么都没发出去时不应占用冷却")
	}
}

func TestConnectorRollbackCooldownRoundTrip(t *testing.T) {
	setupConnectorRollbackBroadcastTest(t)
	ctx := context.Background()

	if got := ConnectorRollbackInCooldown(ctx, []int64{1, 2}); len(got) != 0 {
		t.Fatalf("初始不应有冷却，实际 %v", got)
	}
	MarkConnectorRollbackCooldown(ctx, []int64{2})
	got := ConnectorRollbackInCooldown(ctx, []int64{1, 2})
	if len(got) != 1 || !got[2] {
		t.Fatalf("只有 2 应处于冷却，实际 %v", got)
	}
}

// Redis 分发必须认领这个 cmd，否则广播会被当成未知命令丢掉；
// 载荷解不出来时也只能吞掉并记日志，不能 panic 掉整个订阅循环。
func TestHandleRedisDispatch_ClaimsConnectorRollbackPush(t *testing.T) {
	setupConnectorRollbackBroadcastTest(t)
	if !HandleRedisDispatch(redisCmdBroadcastConnectorRollbackPush, json.RawMessage(`{"push_id":"p","target_version":"4.3.5","agent_ids":[1]}`)) {
		t.Fatal("应认领 connector_rollback_push_broadcast")
	}
	if !HandleRedisDispatch(redisCmdBroadcastConnectorRollbackPush, json.RawMessage(`{"agent_ids":"bad"}`)) {
		t.Fatal("载荷非法时仍应认领并吞掉，不能落到未知命令分支")
	}
}
