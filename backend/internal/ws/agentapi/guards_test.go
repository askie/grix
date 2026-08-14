package agentapi

import (
	"testing"
	"time"

	"github.com/askie/grix/backend/internal/ws/protocol"
)

// TestEnsureEventOwnedBy_BlocksCrossAgent 验证 Agent A 持有的 event_id
// 不允许 Agent B 引用,符合 Phase 1.1 主轴。
func TestEnsureEventOwnedBy_BlocksCrossAgent(t *testing.T) {
	withoutDurableStores(t)
	mgr := NewManager("", 30*time.Second, nil, nil, nil, nil)
	defer mgr.Shutdown()

	mgr.registerPendingEventAck(DelegateEventPayload{
		EventID:   "evt-owned-by-A",
		AgentID:   100,
		OwnerID:   200,
		SessionID: "sess-A",
	}, 1)

	if err := mgr.ensureEventOwnedBy("evt-owned-by-A", 100); err != nil {
		t.Fatalf("agent A should own its event, got=%v", err)
	}

	// Agent B 引用 Agent A 的 event_id → 必须 4003
	err := mgr.ensureEventOwnedBy("evt-owned-by-A", 999)
	if err == nil {
		t.Fatal("expected cross-agent event reference to fail")
	}
	if err.Code != 4003 {
		t.Fatalf("expected 4003, got=%d", err.Code)
	}

	// 不存在的 event_id → 也必须 4003（不暴露内部状态）
	err = mgr.ensureEventOwnedBy("evt-not-exist", 100)
	if err == nil {
		t.Fatal("expected unknown event_id to fail")
	}
	if err.Code != 4003 {
		t.Fatalf("expected 4003, got=%d", err.Code)
	}

	// 空 eventID → 视为合法 (例如 binding card 等无事件路径)
	if err := mgr.ensureEventOwnedBy("", 100); err != nil {
		t.Fatalf("empty event_id should pass, got=%v", err)
	}
}

// TestEnsureEventOwnedBy_NotFoundDiscriminator 锁定 NotFound 标志的语义：
// stream_chunk 的"event_id 找不到时降级为 session 投递"逻辑完全依赖它区分两种 4003——
//   - event 存在但归属其他 Agent → 真越权，NotFound=false，必须硬拒；
//   - event 完全不存在（很可能 Agent 传错 id）→ NotFound=true，允许降级投递。
//
// 一旦这两者被混淆，要么内容继续丢失，要么越权写入被放行。
func TestEnsureEventOwnedBy_NotFoundDiscriminator(t *testing.T) {
	withoutDurableStores(t)
	mgr := NewManager("", 30*time.Second, nil, nil, nil, nil)
	defer mgr.Shutdown()

	mgr.registerPendingEventAck(DelegateEventPayload{
		EventID:   "evt-owned-by-A",
		AgentID:   100,
		OwnerID:   200,
		SessionID: "sess-A",
	}, 1)

	// 跨 Agent 引用：event 存在，只是不归当前 Agent → NotFound 必须为 false。
	crossAgent := mgr.ensureEventOwnedBy("evt-owned-by-A", 999)
	if crossAgent == nil {
		t.Fatal("expected cross-agent reference to fail")
	}
	if crossAgent.NotFound {
		t.Fatal("cross-agent event must NOT be flagged NotFound (keep it a hard reject)")
	}

	// event 完全不存在 → NotFound 必须为 true，才能触发 session 降级投递。
	missing := mgr.ensureEventOwnedBy("evt-not-exist", 100)
	if missing == nil {
		t.Fatal("expected unknown event_id to fail")
	}
	if !missing.NotFound {
		t.Fatal("unknown event_id must be flagged NotFound to enable session fallback")
	}
}

// TestEnsureSessionConsistentWithEvent_RejectsMismatch 验证带 event_id 的上行
// 必须使用 event 上登记的 session_id, 否则视为越权。
func TestEnsureSessionConsistentWithEvent_RejectsMismatch(t *testing.T) {
	withoutDurableStores(t)
	mgr := NewManager("", 30*time.Second, nil, nil, nil, nil)
	defer mgr.Shutdown()

	mgr.registerPendingEventAck(DelegateEventPayload{
		EventID:   "evt-bound-session",
		AgentID:   100,
		OwnerID:   200,
		SessionID: "sess-correct",
	}, 1)

	if err := mgr.ensureSessionConsistentWithEvent("evt-bound-session", "sess-correct"); err != nil {
		t.Fatalf("matching session should pass, got=%v", err)
	}

	err := mgr.ensureSessionConsistentWithEvent("evt-bound-session", "sess-WRONG")
	if err == nil {
		t.Fatal("expected mismatched session_id to fail")
	}
	if err.Code != 4003 {
		t.Fatalf("expected 4003, got=%d", err.Code)
	}
}

// TestChunkTrackers_ToleratesNonMonotonic 验证顺序问题（重复/回退）不再致命，
// 交由服务端重排层处理。
func TestChunkTrackers_ToleratesNonMonotonic(t *testing.T) {
	var tr chunkTrackers

	if _, crossed := tr.observe("evt-1", 1); crossed {
		t.Fatal("seq=1 should not cross the warning threshold")
	}
	if _, crossed := tr.observe("evt-1", 2); crossed {
		t.Fatal("seq=2 should not cross the warning threshold")
	}
	// 重复 seq（重发）→ 放行，由重排层去重
	if _, crossed := tr.observe("evt-1", 2); crossed {
		t.Fatal("duplicate seq=2 should not cross the warning threshold")
	}
	// 回退 seq（滞后到达）→ 放行，由重排层丢弃 stale
	if _, crossed := tr.observe("evt-1", 1); crossed {
		t.Fatal("regressing seq=1 should not cross the warning threshold")
	}
}

// TestChunkTrackers_ToleratesLargeGap 验证跳序（长停顿后前跳）不再致命，
// 交由重排层重锚 expected_seq。
func TestChunkTrackers_ToleratesLargeGap(t *testing.T) {
	var tr chunkTrackers
	if _, crossed := tr.observe("evt-2", 1); crossed {
		t.Fatal("seq=1 should not cross the warning threshold")
	}
	// gap = 100 > MaxChunkSeqGap=16 → 放行
	if _, crossed := tr.observe("evt-2", 101); crossed {
		t.Fatal("large gap should not cross the warning threshold")
	}
}

// TestChunkTrackers_ReportsSoftThresholdOnce 验证分片数超过观测阈值时只上报一次，
// 后续分片仍继续计数，不产生拒绝语义。
func TestChunkTrackers_ReportsSoftThresholdOnce(t *testing.T) {
	var tr chunkTrackers
	var crossedCount int
	var lastCount int64
	for i := int64(1); i <= int64(protocol.StreamChunkCountWarnThreshold)+2; i++ {
		count, crossed := tr.observe("evt-3", i)
		lastCount = count
		if crossed {
			crossedCount++
		}
	}
	if crossedCount != 1 {
		t.Fatalf("warning threshold crossed count=%d want=1", crossedCount)
	}
	if want := int64(protocol.StreamChunkCountWarnThreshold) + 2; lastCount != want {
		t.Fatalf("last count=%d want=%d", lastCount, want)
	}
}

// TestRecordViolation_KicksAfterThreshold 验证累计违规会触发 close。
func TestRecordViolation_KicksAfterThreshold(t *testing.T) {
	conn := &agentConn{
		agentID: 1,
		ownerID: 1,
		send:    make(chan []byte, 256),
	}
	for i := 0; i < violationKickThreshold-1; i++ {
		conn.recordViolation()
	}
	if conn.closed.Load() {
		t.Fatal("should not be closed before threshold")
	}
	conn.recordViolation() // 触发 close
	// close 是 go close,等待短时
	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		if conn.closed.Load() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("violation kick did not close conn within deadline")
}
