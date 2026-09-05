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
			name: "missing voice with a voice-capable agent",
			snapshot: Snapshot{
				Agents:   []AgentSnapshot{{ProviderType: model.AgentProviderAPI}, {ProviderType: model.AgentProviderVoice}},
				Usage:    UsageSnapshot{HasSentAgentMessage: true},
				Overview: OverviewSnapshot{AgentTotal: 2, HasMultiAgentGroup: true},
				Sessions: SessionsSnapshot{MultiAgentGroups: 1},
			},
			wantSkip: false,
		},
		{
			name: "complete path without any voice-capable agent",
			snapshot: Snapshot{
				Agents: []AgentSnapshot{
					{ProviderType: model.AgentProviderRemote},
					{ProviderType: model.AgentProviderLocal},
					{ProviderType: model.AgentProviderAPI},
				},
				Usage:    UsageSnapshot{HasSentAgentMessage: true},
				Overview: OverviewSnapshot{AgentTotal: 3, HasMultiAgentGroup: true},
				Sessions: SessionsSnapshot{MultiAgentGroups: 1},
			},
			wantSkip: true,
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
	got := missingCoachSteps(Snapshot{
		Agents: []AgentSnapshot{{ProviderType: model.AgentProviderVoice}},
	})
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

func TestMissingCoachStepsVoiceRequiresVoiceCapableAgent(t *testing.T) {
	base := func(agents []AgentSnapshot, hasVoiceCall bool) Snapshot {
		return Snapshot{
			Agents:   agents,
			Usage:    UsageSnapshot{HasSentAgentMessage: true, HasVoiceCall: hasVoiceCall},
			Overview: OverviewSnapshot{AgentTotal: len(agents), HasMultiAgentGroup: true},
			Sessions: SessionsSnapshot{MultiAgentGroups: 1},
		}
	}
	nonVoiceAgents := []AgentSnapshot{
		{ProviderType: model.AgentProviderRemote},
		{ProviderType: model.AgentProviderLocal},
		{ProviderType: model.AgentProviderAPI},
	}
	voiceAgents := []AgentSnapshot{
		{ProviderType: model.AgentProviderAPI},
		{ProviderType: model.AgentProviderVoice},
	}

	tests := []struct {
		name      string
		snapshot  Snapshot
		wantVoice bool
	}{
		{
			name:      "agents but none voice-capable",
			snapshot:  base(nonVoiceAgents, false),
			wantVoice: false,
		},
		{
			name:      "no agents at all",
			snapshot:  base(nil, false),
			wantVoice: false,
		},
		{
			name:      "voice-capable agent without a call yet",
			snapshot:  base(voiceAgents, false),
			wantVoice: true,
		},
		{
			name:      "voice-capable agent that already called",
			snapshot:  base(voiceAgents, true),
			wantVoice: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := missingCoachSteps(tc.snapshot)
			hasVoice := false
			for _, step := range got {
				if step == coachStepVoice {
					hasVoice = true
				}
			}
			if hasVoice != tc.wantVoice {
				t.Fatalf("missingCoachSteps()=%v voice=%v want voice=%v", got, hasVoice, tc.wantVoice)
			}
		})
	}
}

// The regression this gate exists for: a user who finished agent, message and
// multi-agent group but owns no voice-capable agent must be left alone instead
// of being nudged every day toward a call button their client never renders.
func TestShouldSkipCoachDispatchWithoutVoiceCapableAgent(t *testing.T) {
	snapshot := Snapshot{
		Agents: []AgentSnapshot{
			{ID: 1, ClientType: "codex", ProviderType: model.AgentProviderAPI},
			{ID: 2, ClientType: "deepseek", ProviderType: model.AgentProviderRemote},
		},
		Usage:    UsageSnapshot{HasSentAgentMessage: true, HasVoiceCall: false},
		Overview: OverviewSnapshot{AgentTotal: 2, HasMultiAgentGroup: true},
		Sessions: SessionsSnapshot{MultiAgentGroups: 1},
	}
	if steps := missingCoachSteps(snapshot); len(steps) != 0 {
		t.Fatalf("missingCoachSteps()=%v want empty", steps)
	}
	if nextCoachStep(snapshot) != "" {
		t.Fatalf("nextCoachStep()=%q want empty", nextCoachStep(snapshot))
	}
	if !ShouldSkipCoachDispatch(snapshot) {
		t.Fatal("user without a voice-capable agent must not be coached forever")
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
