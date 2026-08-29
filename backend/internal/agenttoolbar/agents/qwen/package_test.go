package qwen

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

func qwenInput(online bool, actions []string, meta map[string]any) core.BuildInput {
	return core.BuildInput{
		Session: core.SessionInfo{SessionID: "sess-1"},
		Agent:   core.AgentInfo{AgentID: 11, ClientType: "qwen"},
		Runtime: toolruntime.Profile{ClientType: "qwen", Online: online, LocalActions: actions},
		Binding: core.BindingInfo{ProviderKey: "qwen", BindingID: "b-1", Cwd: "/work", Status: "bound", Meta: meta},
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

// select_model 的拒绝提示与工具栏置灰提示同源（shared.BuildSelect），display_label 取清单显示名。
func TestQwenSelectModelSharesToolbarAvailability(t *testing.T) {
	meta := map[string]any{"available_models": []any{map[string]any{"id": "q-1", "display_name": "Qwen One"}}}

	result, exec := selectModel(t, qwenInput(false, []string{"set_model"}, meta), "q-1")
	if result.Outcome != toolprotocol.ActionOutcomeRejected || result.Code != "action_unavailable" || result.Message != "Qwen 当前离线" {
		t.Fatalf("offline: %+v", result)
	}
	result, _ = selectModel(t, qwenInput(true, nil, meta), "q-1")
	if result.Code != "action_unavailable" || result.Message != "当前插件未声明 set_model" {
		t.Fatalf("undeclared: %+v", result)
	}
	result, _ = selectModel(t, qwenInput(true, []string{"set_model"}, nil), "q-1")
	if result.Code != "action_unavailable" || result.Message != "等待 Qwen 模型列表同步" {
		t.Fatalf("no list: %+v", result)
	}

	result, exec = selectModel(t, qwenInput(true, []string{"set_model"}, meta), "q-1")
	if result.Outcome == toolprotocol.ActionOutcomeRejected {
		t.Fatalf("ready rejected: %+v", result)
	}
	if len(exec.localActions) != 1 || exec.localActions[0].ActionType != "set_model" {
		t.Fatalf("dispatch: %+v", exec.localActions)
	}
	if got := exec.localActions[0].Params["display_label"]; got != "Qwen One" {
		t.Fatalf("display_label=%v", got)
	}
}
