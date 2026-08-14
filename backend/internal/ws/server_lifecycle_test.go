package ws

import (
	"testing"
	"time"

	"github.com/askie/grix/backend/internal/pkg/logger"
	"github.com/askie/grix/backend/internal/pkg/testutil"
	"github.com/askie/grix/backend/internal/store"
	wsagentapi "github.com/askie/grix/backend/internal/ws/agentapi"
)

func TestServerShutdownWithoutStart(t *testing.T) {
	logger.Init()

	srv := NewServer(0, "node-shutdown-empty", "", "", 0, "", false)
	srv.Shutdown()
}

// stopAgentAPIStreamFinishTimers 必须让已排定但还没触发的定时器立刻归零计数，
// 否则 waitAgentAPIStreamFinalizers 会盯着一个永远不会有人减掉的计数傻等到
// 超时——只要关停时有一条流式消息的宽限期还没到，就要白等满 10 秒才肯放行。
//
// 计数是在 scheduleAgentAPIStreamFinalize「排定」这一刻就 +1 的（不是等定时器
// 触发才加，见该函数的注释），所以停定时器这一步必须配对 -1；这里用一个足够长
// 的宽限期（确保测试期间不会意外触发），验证 Stop() 之后计数立刻清零、
// waitAgentAPIStreamFinalizers 立刻返回，而不是等到超时。
func TestStopAgentAPIStreamFinishTimersDecrementsScheduledCount(t *testing.T) {
	s := &Server{agentAPIStreamFinishGrace: 5 * time.Second}

	if err := s.scheduleAgentAPIStreamFinalize("sess-stop-timer", 1, 100, "cmsg-stop-timer", "evt-stop-timer"); err != nil {
		t.Fatalf("schedule: %v", err)
	}

	s.agentAPIStreamFinishMu.Lock()
	active := s.agentAPIStreamFinishActive
	s.agentAPIStreamFinishMu.Unlock()
	if active != 1 {
		t.Fatalf("scheduling must count immediately, got active=%d want=1", active)
	}

	s.stopAgentAPIStreamFinishTimers()

	s.agentAPIStreamFinishMu.Lock()
	active = s.agentAPIStreamFinishActive
	s.agentAPIStreamFinishMu.Unlock()
	if active != 0 {
		t.Fatalf("stopping an un-fired timer must decrement the count, got active=%d want=0 — "+
			"waitAgentAPIStreamFinalizers would otherwise wait the full 10s timeout on every "+
			"shutdown that has any grace-period timer still pending", active)
	}

	start := time.Now()
	s.waitAgentAPIStreamFinalizers()
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("waitAgentAPIStreamFinalizers took %s, want near-instant (count should already be 0)", elapsed)
	}
}

func TestServerCleanupRuntimeStopsRedisAndClearsGlobalManager(t *testing.T) {
	logger.Init()

	previousGlobal := wsagentapi.GetGlobal()
	t.Cleanup(func() {
		wsagentapi.SetGlobal(previousGlobal)
	})

	previousRedis := store.RDB
	store.RDB = testutil.NewMockRedis()
	t.Cleanup(func() {
		_ = store.RDB.Close()
		store.RDB = previousRedis
	})

	manager := wsagentapi.NewManager("", time.Second, nil, nil, nil, nil)
	wsagentapi.SetGlobal(manager)

	stopCalls := 0
	srv := &Server{
		agentAPIMgr:  manager,
		stopRedisSub: func() { stopCalls++ },
	}

	srv.cleanupRuntime()
	srv.cleanupRuntime()

	if stopCalls != 1 {
		t.Fatalf("stopRedisSub called %d times, want 1", stopCalls)
	}
	if wsagentapi.GetGlobal() != nil {
		t.Fatal("global agentapi manager should be cleared")
	}
}

func TestServerStartAndShutdownClearsGlobalManager(t *testing.T) {
	logger.Init()

	previousGlobal := wsagentapi.GetGlobal()
	t.Cleanup(func() {
		wsagentapi.SetGlobal(previousGlobal)
	})

	previousRedis := store.RDB
	store.RDB = testutil.NewMockRedis()
	t.Cleanup(func() {
		_ = store.RDB.Close()
		store.RDB = previousRedis
	})

	srv := NewServer(0, "node-lifecycle", "", "", 0, "", false)
	done := make(chan error, 1)
	go func() {
		done <- srv.Start()
	}()

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if wsagentapi.GetGlobal() != nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if wsagentapi.GetGlobal() == nil {
		t.Fatal("global agentapi manager was not initialized by Start")
	}

	srv.Shutdown()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Start returned error: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Start did not exit after Shutdown")
	}

	if wsagentapi.GetGlobal() != nil {
		t.Fatal("global agentapi manager should be cleared after Shutdown")
	}
}
