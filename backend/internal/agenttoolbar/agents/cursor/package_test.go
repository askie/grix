package cursor

import (
	"context"
	"testing"

	"github.com/askie/grix/backend/internal/agenttoolbar/core"
	toolprotocol "github.com/askie/grix/backend/internal/agenttoolbar/protocol"
	toolruntime "github.com/askie/grix/backend/internal/agenttoolbar/runtime"
)

type testExecutor struct{ localActions []core.LocalActionRequest }

func (e *testExecutor) DispatchLocalAction(_ context.Context, req core.LocalActionRequest) error {
	e.localActions = append(e.localActions, req)
	return nil
}
func (e *testExecutor) StopOutput(context.Context, core.StopOutputRequest) error   { return nil }
func (e *testExecutor) SendStopText(context.Context, core.StopOutputRequest) error { return nil }

func cursorInput(online bool, actions []string, meta map[string]any) core.BuildInput {
	return core.BuildInput{
		Session: core.SessionInfo{SessionID: "sess-1"},
		Agent:   core.AgentInfo{AgentID: 11, ClientType: "cursor"},
		Runtime: toolruntime.Profile{ClientType: "cursor", Online: online, LocalActions: actions},
		Binding: core.BindingInfo{ProviderKey: "cursor", BindingID: "b-1", Cwd: "/work", Status: "bound", Meta: meta},
	}
}

func selectModel(t *testing.T, bi core.BuildInput, optionID string) (toolprotocol.ActionResult, *testExecutor) {
	t.Helper()
	exec := &testExecutor{}
	result, err := New().HandleAction(context.Background(), core.ActionInput{
		BuildInput: bi,
		Request:    toolprotocol.ActionRequest{SessionID: "sess-1", ItemID: "select_model", ActionID: "select_model", Event: "select", OptionID: optionID},
		Executor:   exec,
	})
	if err != nil {
		t.Fatal(err)
	}
	return result, exec
}

// select_model 的 display_label 取清单显示名，清单外的 id 原样回落。
func TestCursorSelectModelDisplayLabel(t *testing.T) {
	meta := map[string]any{"available_models": []any{map[string]any{"id": "c-1", "displayName": "Cursor One"}}}

	result, _ := selectModel(t, cursorInput(false, []string{"set_model"}, meta), "c-1")
	if result.Outcome != toolprotocol.ActionOutcomeRejected || result.Code != "agent_offline" {
		t.Fatalf("offline: %+v", result)
	}
	result, _ = selectModel(t, cursorInput(true, []string{"set_model"}, meta), "")
	if result.Code != "invalid_option" {
		t.Fatalf("empty option: %+v", result)
	}

	result, exec := selectModel(t, cursorInput(true, []string{"set_model"}, meta), "c-1")
	if result.Outcome == toolprotocol.ActionOutcomeRejected {
		t.Fatalf("ready rejected: %+v", result)
	}
	if len(exec.localActions) != 1 || exec.localActions[0].ActionType != "set_model" || exec.localActions[0].Params["display_label"] != "Cursor One" {
		t.Fatalf("dispatch: %+v", exec.localActions)
	}
	_, exec = selectModel(t, cursorInput(true, []string{"set_model"}, meta), "ghost")
	if len(exec.localActions) != 1 || exec.localActions[0].Params["display_label"] != "ghost" {
		t.Fatalf("unknown id fallback: %+v", exec.localActions)
	}
}
