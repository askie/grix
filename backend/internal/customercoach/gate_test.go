package customercoach

import (
	"testing"

	"github.com/askie/grix/backend/internal/featuregate"
	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/pkg/logger"
	"github.com/askie/grix/backend/internal/pkg/testutil"
	"github.com/askie/grix/backend/internal/store"
)

func TestShouldSkipCoachDispatch(t *testing.T) {
	tests := []struct {
		name     string
		snapshot Snapshot
		wantSkip bool
	}{
		{
			name:     "empty",
			snapshot: Snapshot{},
			wantSkip: false,
		},
		{
			name: "missing any agent",
			snapshot: Snapshot{
				Usage:    UsageSnapshot{HasSentAgentMessage: true, HasVoiceCall: true},
				Overview: OverviewSnapshot{HasMultiAgentGroup: true},
				Sessions: SessionsSnapshot{MultiAgentGroups: 1},
			},
			wantSkip: false,
		},
		{
			name: "missing agent message",
			snapshot: Snapshot{
				Usage:    UsageSnapshot{HasVoiceCall: true},
				Overview: OverviewSnapshot{AgentTotal: 2, HasMultiAgentGroup: true},
				Sessions: SessionsSnapshot{MultiAgentGroups: 1},
			},
			wantSkip: false,
		},
		{
			name: "missing multi-agent group",
			snapshot: Snapshot{
				Usage:    UsageSnapshot{HasSentAgentMessage: true, HasVoiceCall: true},
				Overview: OverviewSnapshot{AgentTotal: 2},
			},
			wantSkip: false,
		},
		{
			name: "missing voice",
			snapshot: Snapshot{
				Usage:    UsageSnapshot{HasSentAgentMessage: true},
				Overview: OverviewSnapshot{AgentTotal: 2, HasMultiAgentGroup: true},
				Sessions: SessionsSnapshot{MultiAgentGroups: 1},
			},
			wantSkip: false,
		},
		{
			name: "complete path via overview flag",
			snapshot: Snapshot{
				Usage:    UsageSnapshot{HasSentAgentMessage: true, HasVoiceCall: true},
				Overview: OverviewSnapshot{AgentTotal: 2, HasMultiAgentGroup: true},
			},
			wantSkip: true,
		},
		{
			name: "complete path via sessions count even if overview false",
			snapshot: Snapshot{
				Usage:    UsageSnapshot{HasSentAgentMessage: true, HasVoiceCall: true},
				Overview: OverviewSnapshot{AgentTotal: 2},
				Sessions: SessionsSnapshot{MultiAgentGroups: 2},
			},
			wantSkip: true,
		},
		{
			name: "complete without scope-full main agent",
			snapshot: Snapshot{
				MainAgent: nil,
				Usage:     UsageSnapshot{HasSentAgentMessage: true, HasVoiceCall: true, VoiceCallCount: 3},
				Overview:  OverviewSnapshot{AgentTotal: 134, HasMultiAgentGroup: true},
				Sessions:  SessionsSnapshot{MultiAgentGroups: 1},
			},
			wantSkip: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := ShouldSkipCoachDispatch(tc.snapshot); got != tc.wantSkip {
				t.Fatalf("ShouldSkipCoachDispatch()=%v want %v", got, tc.wantSkip)
			}
		})
	}
}

func TestMissingCoachStepsStableOrder(t *testing.T) {
	got := missingCoachSteps(Snapshot{})
	want := []string{coachStepAgent, coachStepAgentMessage, coachStepMultiAgentGroup, coachStepVoice}
	if len(got) != len(want) {
		t.Fatalf("missingCoachSteps()=%v want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("missingCoachSteps()[%d]=%q want %q", i, got[i], want[i])
		}
	}
}

func TestIsCoachTriggerAllowedUsesFeatureGate(t *testing.T) {
	logger.Init()
	testDB := testutil.NewTestDB()
	t.Cleanup(testDB.Close)
	store.DB = testDB.DB
	featuregate.InvalidateCache()
	t.Cleanup(featuregate.InvalidateCache)

	if IsCoachTriggerAllowed(1001) {
		t.Fatal("missing gate must deny")
	}
	if IsCoachTriggerAllowed(0) {
		t.Fatal("invalid user must be denied")
	}

	if _, err := featuregate.CreateGate(FeatureGateKey, "客服主动引导", model.FeatureStatusWhitelist); err != nil {
		t.Fatalf("create gate: %v", err)
	}
	if err := featuregate.AddUsersToWhitelist(FeatureGateKey, []int64{1001}); err != nil {
		t.Fatalf("whitelist: %v", err)
	}
	featuregate.InvalidateCache()

	if !IsCoachTriggerAllowed(1001) {
		t.Fatal("whitelisted user must be allowed")
	}
	if IsCoachTriggerAllowed(1002) {
		t.Fatal("non-whitelisted user must be denied")
	}

	if err := featuregate.UpdateGateStatus(FeatureGateKey, model.FeatureStatusEnabled); err != nil {
		t.Fatalf("enable gate: %v", err)
	}
	featuregate.InvalidateCache()
	if !IsCoachTriggerAllowed(1002) {
		t.Fatal("enabled gate must allow any user")
	}

	if err := featuregate.UpdateGateStatus(FeatureGateKey, model.FeatureStatusDisabled); err != nil {
		t.Fatalf("disable gate: %v", err)
	}
	featuregate.InvalidateCache()
	if IsCoachTriggerAllowed(1001) {
		t.Fatal("disabled gate must deny")
	}
}
