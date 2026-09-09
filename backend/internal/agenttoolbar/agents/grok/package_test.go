package grok

import (
	"context"
	"testing"

	"github.com/askie/grix/backend/internal/agenttoolbar/core"
	toolprotocol "github.com/askie/grix/backend/internal/agenttoolbar/protocol"
	toolruntime "github.com/askie/grix/backend/internal/agenttoolbar/runtime"
	"github.com/askie/grix/backend/internal/model"
)

type testExecutor struct {
	localActions []core.LocalActionRequest
}

func (e *testExecutor) DispatchLocalAction(_ context.Context, req core.LocalActionRequest) error {
	e.localActions = append(e.localActions, req)
	return nil
}

func (e *testExecutor) StopOutput(_ context.Context, _ core.StopOutputRequest) error { return nil }

func (e *testExecutor) SendStopText(_ context.Context, _ core.StopOutputRequest) error { return nil }

func buildInput(online bool, localActions []string, hasCwd bool) core.BuildInput {
	cwd := ""
	if hasCwd {
		cwd = "/workspace/project"
	}
	return core.BuildInput{
		OwnerID: 1001,
		Session: core.SessionInfo{SessionID: "sess-1"},
		Agent: core.AgentInfo{
			AgentID:    9001,
			OwnerID:    1001,
			ClientType: model.AgentClientTypeGrok,
		},
		Runtime: toolruntime.Profile{
			Online:       online,
			LocalActions: localActions,
		},
		Binding: core.BindingInfo{
			Cwd: cwd,
			Meta: map[string]any{
				"model_id": "m-1",
				"available_models": []any{
					map[string]any{"id": "m-1", "displayName": "Model One"},
				},
			},
		},
	}
}

func TestPackage_KeyAndMatch(t *testing.T) {
	p := New()
	if p.Key() != "grok" {
		t.Fatalf("Key()=%q want=%q", p.Key(), "grok")
	}
	if !p.Match(core.MatchContext{Agent: core.AgentInfo{ClientType: "grok"}}) {
		t.Fatal("expected Match to accept grok client_type")
	}
	if p.Match(core.MatchContext{Agent: core.AgentInfo{ClientType: "claude"}}) {
		t.Fatal("expected Match to reject a different client_type")
	}
}

func TestPackage_BuildHiddenWithoutBinding(t *testing.T) {
	snap, err := New().Build(context.Background(), buildInput(true, []string{"session_control"}, false))
	if err != nil {
		t.Fatal(err)
	}
	if snap.Visible {
		t.Fatal("snapshot should be hidden without a session binding")
	}
}

func TestPackage_BuildRendersModelSelectFromReportedMeta(t *testing.T) {
	snap, err := New().Build(context.Background(), buildInput(true, []string{"session_control", "set_model"}, true))
	if err != nil {
		t.Fatal(err)
	}
	if !snap.Visible {
		t.Fatal("snapshot should be visible with a session binding")
	}
	var modelItem *toolprotocol.Item
	for i := range snap.Items {
		if snap.Items[i].ItemID == "select_model" || snap.Items[i].ActionID == "select_model" {
			modelItem = &snap.Items[i]
		}
	}
	if modelItem == nil {
		t.Fatal("expected a model select item to be rendered from binding.Meta.available_models")
	}
}

func TestPackage_HandleActionDispatchesSetModel(t *testing.T) {
	exec := &testExecutor{}
	in := buildInput(true, []string{"session_control", "set_model"}, true)
	result, err := New().HandleAction(context.Background(), core.ActionInput{
		BuildInput: in,
		Request:    toolprotocol.ActionRequest{ActionID: "select_model", OptionID: "m-2"},
		Executor:   exec,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Outcome == toolprotocol.ActionOutcomeRejected {
		t.Fatalf("expected select_model to be accepted, got rejected: %+v", result)
	}
	if len(exec.localActions) != 1 || exec.localActions[0].ActionType != "set_model" {
		t.Fatalf("expected exactly one set_model local action dispatched, got %+v", exec.localActions)
	}
}
