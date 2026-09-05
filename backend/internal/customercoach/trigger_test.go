package customercoach

import (
	"strings"
	"testing"

	"github.com/askie/grix/backend/internal/model"
)

const (
	offlineReminderMarker = "需要连接本机才能工作的 Agent 当前都不在线"
	// The reminder must never claim every agent is down: a user can have an
	// offline connector agent next to a perfectly usable remote model agent.
	allAgentsClaim = "所有 Agent"
)

func TestBuildInternalTaskOfflineReminder(t *testing.T) {
	cases := []struct {
		name        string
		agents      []AgentSnapshot
		wantOffline bool
	}{
		{
			name:        "no agent yet",
			agents:      nil,
			wantOffline: false,
		},
		{
			name: "only remote model agents",
			agents: []AgentSnapshot{
				{ProviderType: model.AgentProviderRemote, Online: false},
				{ProviderType: model.AgentProviderVoice, Online: false},
			},
			wantOffline: false,
		},
		{
			name: "connector agents all offline",
			agents: []AgentSnapshot{
				{ProviderType: model.AgentProviderLocal, Online: false},
				{ProviderType: model.AgentProviderAPI, Online: false},
			},
			wantOffline: true,
		},
		{
			name: "one connector agent online",
			agents: []AgentSnapshot{
				{ProviderType: model.AgentProviderLocal, Online: false},
				{ProviderType: model.AgentProviderAPI, Online: true},
			},
			wantOffline: false,
		},
		{
			name: "connector agent offline alongside a remote model",
			agents: []AgentSnapshot{
				{ProviderType: model.AgentProviderAPI, Online: false},
				{ProviderType: model.AgentProviderRemote, Online: false},
			},
			wantOffline: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			snapshot := Snapshot{Agents: tc.agents}
			for _, agent := range tc.agents {
				snapshot.Overview.AgentTotal++
				if agent.Online {
					snapshot.Overview.AgentOnline++
				}
			}
			if got := agentsAllOffline(snapshot); got != tc.wantOffline {
				t.Fatalf("agentsAllOffline = %v want %v", got, tc.wantOffline)
			}
			prompt := buildInternalTask("# snapshot", coachStepMultiAgentGroup, agentsAllOffline(snapshot))
			if got := strings.Contains(prompt, offlineReminderMarker); got != tc.wantOffline {
				t.Fatalf("offline reminder present = %v want %v:\n%s", got, tc.wantOffline, prompt)
			}
			if !strings.Contains(prompt, coachStepGuidance[coachStepMultiAgentGroup]) {
				t.Fatalf("prompt lost the backend-chosen step guidance:\n%s", prompt)
			}
			if strings.Contains(prompt, allAgentsClaim) {
				t.Fatalf("prompt must not make a claim about every agent:\n%s", prompt)
			}
		})
	}
}

func TestBuildInternalTaskOfflineReminderPrecedesStep(t *testing.T) {
	prompt := buildInternalTask("# snapshot", coachStepMultiAgentGroup, true)
	reminder := strings.Index(prompt, offlineReminderMarker)
	step := strings.Index(prompt, "后端已经判定本次要引导的下一步动作是")
	if reminder < 0 || step < 0 {
		t.Fatalf("prompt missing reminder or step instruction:\n%s", prompt)
	}
	if reminder > step {
		t.Fatalf("offline reminder must come before the assigned step instruction:\n%s", prompt)
	}
}

func TestBuildInternalTaskOfflineReminderDoesNotClaimEveryAgentIsDown(t *testing.T) {
	snapshot := Snapshot{Agents: []AgentSnapshot{
		{ProviderType: model.AgentProviderAPI, Online: false},
		{ProviderType: model.AgentProviderRemote, Online: false},
	}}
	prompt := buildInternalTask("# snapshot", coachStepMultiAgentGroup, agentsAllOffline(snapshot))
	if !strings.Contains(prompt, offlineReminderMarker) {
		t.Fatalf("offline reminder missing:\n%s", prompt)
	}
	if strings.Contains(prompt, allAgentsClaim) {
		t.Fatalf("prompt must not claim every agent is offline:\n%s", prompt)
	}
	if !strings.Contains(prompt, "远程模型 Agent 正常可用") {
		t.Fatalf("prompt must tell the model not to call remote model agents offline:\n%s", prompt)
	}
}
