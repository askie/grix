package agenttoolbar_test

import (
	"context"
	"testing"

	"github.com/askie/grix/backend/internal/agenttoolbar/agents/pi"
	"github.com/askie/grix/backend/internal/agenttoolbar/core"
	toolprotocol "github.com/askie/grix/backend/internal/agenttoolbar/protocol"
	toolruntime "github.com/askie/grix/backend/internal/agenttoolbar/runtime"
	"github.com/askie/grix/backend/internal/model"
)

func TestPiPackageBuild_UsesRestartSessionControl(t *testing.T) {
	snapshot, err := pi.New().Build(context.Background(), core.BuildInput{
		OwnerID: 1001,
		Session: core.SessionInfo{SessionID: "sess-pi"},
		Agent: core.AgentInfo{
			AgentID:      9001,
			OwnerID:      1001,
			ProviderType: model.AgentProviderAPI,
			ClientType:   model.AgentClientTypePi,
		},
		Runtime: toolruntime.Profile{
			Online:       true,
			LocalActions: []string{"session_control", "get_session_usage"},
		},
		Binding: core.BindingInfo{
			Cwd: "/workspace/pi-demo",
		},
	})
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}

	sessionItem, ok := snapshot.FindItem("session_control")
	if !ok {
		t.Fatal("session_control item not found")
	}
	if len(sessionItem.Options) != 4 {
		t.Fatalf("session_control options=%d want=4", len(sessionItem.Options))
	}
	if sessionItem.Options[0].OptionID != "status" || sessionItem.Options[1].OptionID != "restart" || sessionItem.Options[2].OptionID != "unbind" || sessionItem.Options[3].OptionID != "usage" {
		t.Fatalf("session_control options=%v want [status, restart, unbind, usage]", sessionItem.Options)
	}
	if sessionItem.Options[1].Label != "重启会话" {
		t.Fatalf("restart label=%q want=重启会话", sessionItem.Options[1].Label)
	}
}

func TestPiPackageHandleAction_RestartDispatchesSessionControl(t *testing.T) {
	executor := &packageTestExecutor{}
	result, err := pi.New().HandleAction(context.Background(), core.ActionInput{
		BuildInput: core.BuildInput{
			OwnerID: 1001,
			Session: core.SessionInfo{SessionID: "sess-pi"},
			Agent: core.AgentInfo{
				AgentID:      9001,
				OwnerID:      1001,
				ProviderType: model.AgentProviderAPI,
				ClientType:   model.AgentClientTypePi,
			},
			Runtime: toolruntime.Profile{
				Online:       true,
				LocalActions: []string{"session_control", "get_session_usage"},
			},
		},
		Request: toolprotocol.ActionRequest{
			SessionID: "sess-pi",
			ActionID:  "session_control",
			Event:     "select",
			OptionID:  "restart",
		},
		Executor: executor,
	})
	if err != nil {
		t.Fatalf("HandleAction(restart) error = %v", err)
	}
	if result.Outcome != toolprotocol.ActionOutcomeAcceptedNoStateChange {
		t.Fatalf("restart outcome=%q want=%q", result.Outcome, toolprotocol.ActionOutcomeAcceptedNoStateChange)
	}
	if len(executor.localActions) != 1 {
		t.Fatalf("local actions=%d want=1", len(executor.localActions))
	}
	if got := executor.localActions[0].Params["verb"]; got != "restart" {
		t.Fatalf("verb=%v want=restart", got)
	}
}
