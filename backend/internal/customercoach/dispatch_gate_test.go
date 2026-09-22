package customercoach

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/askie/grix/backend/internal/model"
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
		// The voice step only applies to users who own a voice-capable agent,
		// so keep one here to exercise it.
		s.Agents = []AgentSnapshot{
			{ProviderType: model.AgentProviderAPI},
			{ProviderType: model.AgentProviderVoice},
		}
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

func acquire(ctx context.Context, userID int64, snapshot Snapshot) bool {
	return acquireCoachDispatch(ctx, userID, snapshot, nextCoachStep(snapshot))
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
	if !acquire(ctx, 1001, dispatchGateSnapshot(coachStepVoice)) {
		t.Fatal("first acquire (no state) must be granted")
	}
}

func TestDispatchGateSkipsSameMissingWithinCooldown(t *testing.T) {
	ctx := setupDispatchGateTest(t)
	userID := int64(1002)
	snapshot := dispatchGateSnapshot(coachStepVoice)

	if !acquire(ctx, userID, snapshot) {
		t.Fatal("first acquire must be granted")
	}
	if acquire(ctx, userID, snapshot) {
		t.Fatal("same missing steps within cooldown must be skipped")
	}
}

// Regression for the 2026-09-06 report: a user who completed a step was nudged
// again 87 seconds later because the changed missing set bypassed the cooldown.
func TestDispatchGateSkipsProgressWithinMinInterval(t *testing.T) {
	ctx := setupDispatchGateTest(t)
	userID := int64(1003)

	if !acquire(ctx, userID, dispatchGateSnapshot(coachStepMultiAgentGroup, coachStepVoice)) {
		t.Fatal("first acquire must be granted")
	}
	// The user acts on the nudge: the missing set shrinks, but only seconds later.
	if acquire(ctx, userID, dispatchGateSnapshot(coachStepVoice)) {
		t.Fatal("progress within coachProgressMinInterval must be skipped")
	}
}

func TestDispatchGateAllowsProgressAfterMinInterval(t *testing.T) {
	ctx := setupDispatchGateTest(t)
	userID := int64(1011)
	missing := fmt.Sprintf("%s,%s", coachStepMultiAgentGroup, coachStepVoice)

	seedDispatchState(t, ctx, userID, time.Now().Add(-coachProgressMinInterval-time.Minute), missing)
	if !acquire(ctx, userID, dispatchGateSnapshot(coachStepVoice)) {
		t.Fatal("progress past coachProgressMinInterval must be granted")
	}
}

func TestDispatchGateAllowsAfterCooldown(t *testing.T) {
	ctx := setupDispatchGateTest(t)
	userID := int64(1004)
	snapshot := dispatchGateSnapshot(coachStepVoice)

	seedDispatchState(t, ctx, userID, time.Now().Add(-coachDispatchCooldown-time.Minute), coachStepVoice)
	if !acquire(ctx, userID, snapshot) {
		t.Fatal("same missing steps after cooldown must be allowed")
	}
}

func TestDispatchGateFailOpenOnCorruptState(t *testing.T) {
	ctx := setupDispatchGateTest(t)
	userID := int64(1005)
	if err := store.RDB.Set(ctx, coachDispatchKey(userID), "not-json", coachDispatchStateTTL).Err(); err != nil {
		t.Fatalf("seed corrupt state: %v", err)
	}
	if !acquire(ctx, userID, dispatchGateSnapshot(coachStepVoice)) {
		t.Fatal("corrupt state must fail-open")
	}
	// The grant also rewrites the corrupt state, so the next acquire is gated.
	if acquire(ctx, userID, dispatchGateSnapshot(coachStepVoice)) {
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
			grants <- acquire(ctx, userID, snapshot)
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

func TestDispatchGateCapsNudgesPerStep(t *testing.T) {
	ctx := setupDispatchGateTest(t)
	userID := int64(1008)
	snapshot := dispatchGateSnapshot(coachStepVoice)

	for i := 0; i < coachMaxNudgesPerStep; i++ {
		seedDispatchState(t, ctx, userID, time.Now().Add(-coachDispatchCooldown-time.Minute), coachStepVoice)
		// keep counts from the previous grant: re-read and bump last_at only
		raw, _ := store.RDB.Get(ctx, coachDispatchKey(userID)).Result()
		var state coachDispatchState
		_ = json.Unmarshal([]byte(raw), &state)
		if i > 0 {
			state.Counts = map[string]int64{coachStepVoice: int64(i)}
		}
		state.LastAt = time.Now().Add(-coachDispatchCooldown - time.Minute).Unix()
		b, _ := json.Marshal(state)
		_ = store.RDB.Set(ctx, coachDispatchKey(userID), b, coachDispatchStateTTL).Err()
		if !acquire(ctx, userID, snapshot) {
			t.Fatalf("nudge %d for the same step after cooldown must be granted", i+1)
		}
	}
	// Past the cap: even after the cooldown the same step is never nudged again.
	raw, _ := store.RDB.Get(ctx, coachDispatchKey(userID)).Result()
	var state coachDispatchState
	_ = json.Unmarshal([]byte(raw), &state)
	if state.Counts[coachStepVoice] != coachMaxNudgesPerStep {
		t.Fatalf("count=%d want=%d", state.Counts[coachStepVoice], coachMaxNudgesPerStep)
	}
	state.LastAt = time.Now().Add(-coachDispatchCooldown - time.Minute).Unix()
	b, _ := json.Marshal(state)
	_ = store.RDB.Set(ctx, coachDispatchKey(userID), b, coachDispatchStateTTL).Err()
	if acquire(ctx, userID, snapshot) {
		t.Fatal("step beyond per-step cap must be skipped even after cooldown")
	}
	// Progress to a different step is still allowed.
	if !acquire(ctx, userID, dispatchGateSnapshot(coachStepAgentMessage, coachStepVoice)) {
		t.Fatal("a different step must still be granted")
	}
}

func TestAcquireCoachDispatchFailClosedWithoutRedis(t *testing.T) {
	ctx := setupDispatchGateTest(t)
	prev := store.RDB
	store.RDB = nil
	defer func() { store.RDB = prev }()
	if acquire(ctx, 1009, dispatchGateSnapshot(coachStepVoice)) {
		t.Fatal("redis unavailable must fail-closed")
	}
}

func TestAcquireCoachDispatchWritesState(t *testing.T) {
	ctx := setupDispatchGateTest(t)
	userID := int64(1007)
	if !acquire(ctx, userID, dispatchGateSnapshot(coachStepAgentMessage, coachStepVoice)) {
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

// The progress floor must not weaken the 24h cooldown for an unchanged step set.
func TestDispatchGateUnchangedMissingStillUsesFullCooldown(t *testing.T) {
	ctx := setupDispatchGateTest(t)
	userID := int64(1012)
	snapshot := dispatchGateSnapshot(coachStepVoice)

	// Past the progress floor but well inside the 24h cooldown.
	seedDispatchState(t, ctx, userID, time.Now().Add(-coachProgressMinInterval-time.Hour), coachStepVoice)
	if acquire(ctx, userID, snapshot) {
		t.Fatal("unchanged missing steps within the 24h cooldown must still be skipped")
	}

	seedDispatchState(t, ctx, userID, time.Now().Add(-coachDispatchCooldown-time.Minute), coachStepVoice)
	if !acquire(ctx, userID, snapshot) {
		t.Fatal("unchanged missing steps past the 24h cooldown must be granted")
	}
}

// States written before the progress floor existed carry no new fields (and
// pre-cap states carry no counts at all); they must still gate correctly.
func TestDispatchGateReadsLegacyStateWithoutNewFields(t *testing.T) {
	ctx := setupDispatchGateTest(t)
	userID := int64(1013)
	legacy := fmt.Sprintf(`{"last_at":%d,"missing":%q}`,
		time.Now().Add(-time.Minute).Unix(),
		fmt.Sprintf("%s,%s", coachStepMultiAgentGroup, coachStepVoice))
	if err := store.RDB.Set(ctx, coachDispatchKey(userID), legacy, coachDispatchStateTTL).Err(); err != nil {
		t.Fatalf("seed legacy state: %v", err)
	}

	// Progress against a legacy state must respect the floor, not fall through.
	if acquire(ctx, userID, dispatchGateSnapshot(coachStepVoice)) {
		t.Fatal("legacy state must not let a progress dispatch through the floor")
	}
	// And the same legacy state must still gate an unchanged step set.
	if acquire(ctx, userID, dispatchGateSnapshot(coachStepMultiAgentGroup, coachStepVoice)) {
		t.Fatal("legacy state must still enforce the cooldown for unchanged steps")
	}
}
