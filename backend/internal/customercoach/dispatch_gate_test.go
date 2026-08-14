package customercoach

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/askie/grix/backend/internal/pkg/logger"
	"github.com/askie/grix/backend/internal/pkg/testutil"
	"github.com/askie/grix/backend/internal/store"
)

func setupDispatchGateTest(t *testing.T) context.Context {
	t.Helper()
	logger.Init()
	store.RDB = testutil.NewMockRedis()
	return context.Background()
}

func dispatchGateSnapshot(missing ...string) Snapshot {
	var s Snapshot
	all := map[string]bool{
		coachStepAgent:           true,
		coachStepAgentMessage:    true,
		coachStepMultiAgentGroup: true,
		coachStepVoice:           true,
	}
	for _, step := range missing {
		delete(all, step)
	}
	if all[coachStepAgent] {
		s.Overview.AgentTotal = 2
	}
	if all[coachStepAgentMessage] {
		s.Usage.HasSentAgentMessage = true
	}
	if all[coachStepMultiAgentGroup] {
		s.Overview.HasMultiAgentGroup = true
		s.Sessions.MultiAgentGroups = 1
	}
	if all[coachStepVoice] {
		s.Usage.HasVoiceCall = true
	}
	return s
}

func seedDispatchState(t *testing.T, ctx context.Context, userID int64, lastAt time.Time, missing string) {
	t.Helper()
	raw, err := json.Marshal(coachDispatchState{LastAt: lastAt.Unix(), Missing: missing})
	if err != nil {
		t.Fatalf("marshal state: %v", err)
	}
	if err := store.RDB.Set(ctx, coachDispatchKey(userID), raw, coachDispatchStateTTL).Err(); err != nil {
		t.Fatalf("seed state: %v", err)
	}
}

func TestAcquireCoachDispatchFirstTimeGranted(t *testing.T) {
	ctx := setupDispatchGateTest(t)
	if !acquireCoachDispatch(ctx, 1001, dispatchGateSnapshot(coachStepVoice)) {
		t.Fatal("first acquire (no state) must be granted")
	}
}

func TestDispatchGateSkipsSameMissingWithinCooldown(t *testing.T) {
	ctx := setupDispatchGateTest(t)
	userID := int64(1002)
	snapshot := dispatchGateSnapshot(coachStepVoice)

	if !acquireCoachDispatch(ctx, userID, snapshot) {
		t.Fatal("first acquire must be granted")
	}
	if acquireCoachDispatch(ctx, userID, snapshot) {
		t.Fatal("same missing steps within cooldown must be skipped")
	}
}

func TestDispatchGateAllowsWhenMissingStepsChange(t *testing.T) {
	ctx := setupDispatchGateTest(t)
	userID := int64(1003)

	if !acquireCoachDispatch(ctx, userID, dispatchGateSnapshot(coachStepMultiAgentGroup, coachStepVoice)) {
		t.Fatal("first acquire must be granted")
	}
	if !acquireCoachDispatch(ctx, userID, dispatchGateSnapshot(coachStepVoice)) {
		t.Fatal("changed missing steps must re-allow dispatch even within cooldown")
	}
}

func TestDispatchGateAllowsAfterCooldown(t *testing.T) {
	ctx := setupDispatchGateTest(t)
	userID := int64(1004)
	snapshot := dispatchGateSnapshot(coachStepVoice)

	seedDispatchState(t, ctx, userID, time.Now().Add(-coachDispatchCooldown-time.Minute), coachStepVoice)
	if !acquireCoachDispatch(ctx, userID, snapshot) {
		t.Fatal("same missing steps after cooldown must be allowed")
	}
}

func TestDispatchGateFailOpenOnCorruptState(t *testing.T) {
	ctx := setupDispatchGateTest(t)
	userID := int64(1005)
	if err := store.RDB.Set(ctx, coachDispatchKey(userID), "not-json", coachDispatchStateTTL).Err(); err != nil {
		t.Fatalf("seed corrupt state: %v", err)
	}
	if !acquireCoachDispatch(ctx, userID, dispatchGateSnapshot(coachStepVoice)) {
		t.Fatal("corrupt state must fail-open")
	}
	// The grant also rewrites the corrupt state, so the next acquire is gated.
	if acquireCoachDispatch(ctx, userID, dispatchGateSnapshot(coachStepVoice)) {
		t.Fatal("after a fail-open grant the rewritten state must gate the next acquire")
	}
}

func TestAcquireCoachDispatchGrantsOnlyOnceUnderConcurrency(t *testing.T) {
	ctx := setupDispatchGateTest(t)
	userID := int64(1006)
	snapshot := dispatchGateSnapshot(coachStepVoice)

	const workers = 16
	var wg sync.WaitGroup
	grants := make(chan bool, workers)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			grants <- acquireCoachDispatch(ctx, userID, snapshot)
		}()
	}
	wg.Wait()
	close(grants)

	granted := 0
	for ok := range grants {
		if ok {
			granted++
		}
	}
	if granted != 1 {
		t.Fatalf("concurrent acquires must grant exactly once, got %d", granted)
	}
}

func TestAcquireCoachDispatchWritesState(t *testing.T) {
	ctx := setupDispatchGateTest(t)
	userID := int64(1007)
	if !acquireCoachDispatch(ctx, userID, dispatchGateSnapshot(coachStepAgentMessage, coachStepVoice)) {
		t.Fatal("first acquire must be granted")
	}

	raw, err := store.RDB.Get(ctx, coachDispatchKey(userID)).Result()
	if err != nil {
		t.Fatalf("read state: %v", err)
	}
	var state coachDispatchState
	if err := json.Unmarshal([]byte(raw), &state); err != nil {
		t.Fatalf("unmarshal state: %v", err)
	}
	wantMissing := fmt.Sprintf("%s,%s", coachStepAgentMessage, coachStepVoice)
	if state.Missing != wantMissing {
		t.Fatalf("missing=%q want %q", state.Missing, wantMissing)
	}
	if state.LastAt <= 0 {
		t.Fatalf("last_at must be set, got %d", state.LastAt)
	}
}
