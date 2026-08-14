package agentapi

import (
	"context"
	"testing"
	"time"

	"github.com/askie/grix/backend/internal/pkg/testutil"
	"github.com/askie/grix/backend/internal/store"
)

// TestTimeoutPendingEvent_CrossNodeAckSuppression 锁定跨节点 ack 误判修复：
//
// 在多 ws 节点下，派发事件、持有本地 ack 计时器的节点未必是 agent 连接所在的节点。
// agent 的 event_ack 只在它自己的节点解析、并把共享 durable 记录从 "ack" 推进到
// "result"，却清不掉派发节点的计时器。修复前派发节点会在 ack 超时后误判 failed，
// 而事件其实已在另一节点 ack 并正常 streaming（线上实测：先 failed 再自愈成 completed）。
//
// 统一状态策略：无论 durable 仍在 ack，还是已由另一节点推进到 result，
// “本节点没观察到 ack”都不是 connector 执行失败的可靠证据，不落失败终态。
func TestTimeoutPendingEvent_CrossNodeAckSuppression(t *testing.T) {
	origRDB, origDB := store.RDB, store.DB
	store.RDB = testutil.NewMockRedis()
	store.DB = testutil.NewTestDB().DB
	t.Cleanup(func() { store.RDB, store.DB = origRDB, origDB })

	// newConnectedManager 构造一个本地已持有该 agent 连接的 manager，以绕过
	// timeoutPendingEvent 顶部"无本地连接则进入断连宽限"的分支，复现线上场景：
	// 派发节点持有（可能已过期的）agent 连接表项，因此不走宽限、直接走超时收尾。
	newConnectedManager := func(agentID int64) *Manager {
		m := NewManager("", 30*time.Second, nil, nil, nil, nil)
		t.Cleanup(m.Shutdown) // 工厂函数：不能 defer，否则 manager 一造出来就被关停
		m.putConnForTest(&agentConn{agentID: agentID, send: make(chan []byte, 8)})
		return m
	}

	t.Run("durable 已推进到 result（已在另一节点 ack）→ 不判 failed", func(t *testing.T) {
		mgr := newConnectedManager(100)
		evt := DelegateEventPayload{
			EventID: "evt-xnode-result", AgentID: 100, OwnerID: 200, SenderID: 200,
			SessionID: "sess-xnode-result", SessionType: 1, MsgID: 300,
		}
		mgr.registerActiveRun(evt)
		// attempt 拉满 → 超时不再重试，直接进入收尾判定。
		mgr.registerPendingEventAck(evt, agentAPIDeliveryMaxAttempts)
		// 模拟 agent 的 ack 落在另一节点：共享 durable 记录被推进到 result。
		persistDurablePendingDelegate(context.Background(), durablePendingDelegateRecord{
			Event:      evt,
			Attempt:    agentAPIDeliveryMaxAttempts,
			Stage:      durablePendingDelegateStageResult,
			ReceivedAt: time.Now().UnixMilli(),
		})

		mgr.timeoutPendingEvent(evt.EventID)

		if mgr.LookupActiveRun(evt.EventID) == nil {
			t.Fatal("run 不应被判 failed/移除：durable 处于 result 说明 agent 已在另一节点回执")
		}
		// durable 记录应保留给真正负责该 run 的节点驱动终态，不能被这里删掉。
		if _, ok := loadDurablePendingDelegate(context.Background(), evt.EventID); !ok {
			t.Fatal("durable 记录不应被误删：另一节点仍需用它驱动 result")
		}
	})

	t.Run("durable 仍停在 ack（任何节点都没回执）→ 保留 queued", func(t *testing.T) {
		mgr := newConnectedManager(101)
		evt := DelegateEventPayload{
			EventID: "evt-xnode-ack", AgentID: 101, OwnerID: 201, SenderID: 201,
			SessionID: "sess-xnode-ack", SessionType: 1, MsgID: 301,
		}
		mgr.registerActiveRun(evt)
		// registerPendingEventAck 已把 durable 写为 stage=ack，且不再推进。
		mgr.registerPendingEventAck(evt, agentAPIDeliveryMaxAttempts)

		mgr.timeoutPendingEvent(evt.EventID)

		if mgr.LookupActiveRun(evt.EventID) == nil {
			t.Fatal("run 不应因未观察到 ack 被判 failed/移除")
		}
		if _, ok := loadDurablePendingDelegate(context.Background(), evt.EventID); !ok {
			t.Fatal("durable ack 记录应保留以接受迟到回执或输出")
		}
	})
}
